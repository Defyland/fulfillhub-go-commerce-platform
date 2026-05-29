package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type EventHandler interface {
	HandleEvent(ctx context.Context, event commerce.OutboxEvent) error
}

type HandlerFunc func(context.Context, commerce.OutboxEvent) error

func (f HandlerFunc) HandleEvent(ctx context.Context, event commerce.OutboxEvent) error {
	return f(ctx, event)
}

type Consumer struct {
	Queue        string
	ConsumerName string
	Inbox        Inbox
	Handler      EventHandler
	Tracer       trace.Tracer
}

func (c Consumer) ProcessDelivery(ctx context.Context, delivery amqp.Delivery) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := c.validate(); err != nil {
		return err
	}

	consumerName := c.consumerName()
	ctx = extractTraceHeaders(ctx, delivery.Headers)
	tracer := c.Tracer
	if tracer == nil {
		tracer = messagingTracer()
	}
	ctx, span := tracer.Start(ctx, "rabbitmq.consume", trace.WithAttributes(
		attribute.String("messaging.system", "rabbitmq"),
		attribute.String("messaging.destination.name", c.Queue),
		attribute.String("messaging.operation", "process"),
		attribute.String("messaging.rabbitmq.routing_key", delivery.RoutingKey),
		attribute.String("messaging.message.id", delivery.MessageId),
		attribute.String("fulfillhub.consumer_name", consumerName),
	))
	defer finishConsumerSpan(span, &err)

	event, err := decodeDelivery(delivery)
	if err != nil {
		return errors.Join(fmt.Errorf("decode delivery: %w", err), nackDelivery(delivery))
	}
	span.SetAttributes(
		attribute.String("fulfillhub.event_type", event.EventType),
		attribute.String("fulfillhub.correlation_id", event.CorrelationID),
		attribute.String("fulfillhub.order_id", event.OrderID),
		attribute.String("fulfillhub.merchant_id", event.MerchantID),
	)

	recorded, err := c.Inbox.Record(ctx, consumerName, event)
	if err != nil {
		return errors.Join(fmt.Errorf("record inbox message: %w", err), nackDelivery(delivery))
	}
	span.SetAttributes(attribute.Bool("fulfillhub.inbox_recorded", recorded))
	if !recorded {
		return ackDelivery(delivery)
	}

	if err := c.Handler.HandleEvent(ctx, event); err != nil {
		return errors.Join(fmt.Errorf("handle delivery: %w", err), nackDelivery(delivery))
	}
	return ackDelivery(delivery)
}

func (c Consumer) validate() error {
	if c.Inbox == nil {
		return errors.New("consumer inbox is required")
	}
	if c.Handler == nil {
		return errors.New("consumer handler is required")
	}
	return nil
}

func (c Consumer) consumerName() string {
	if c.ConsumerName != "" {
		return c.ConsumerName
	}
	return c.Queue
}

func decodeDelivery(delivery amqp.Delivery) (commerce.OutboxEvent, error) {
	var event commerce.OutboxEvent
	if err := json.Unmarshal(delivery.Body, &event); err != nil {
		return commerce.OutboxEvent{}, err
	}
	if event.MessageID == "" {
		event.MessageID = delivery.MessageId
	}
	if event.CorrelationID == "" {
		event.CorrelationID = delivery.CorrelationId
	}
	if event.EventType == "" {
		event.EventType = delivery.Type
	}
	return event, nil
}

func ackDelivery(delivery amqp.Delivery) error {
	if err := delivery.Ack(false); err != nil {
		return fmt.Errorf("ack delivery: %w", err)
	}
	return nil
}

func nackDelivery(delivery amqp.Delivery) error {
	if err := delivery.Nack(false, false); err != nil {
		return fmt.Errorf("nack delivery: %w", err)
	}
	return nil
}

func extractTraceHeaders(ctx context.Context, headers amqp.Table) context.Context {
	return propagation.TraceContext{}.Extract(ctx, amqpHeaderCarrier{headers: headers})
}

func finishConsumerSpan(span trace.Span, err *error) {
	if err != nil && *err != nil {
		span.RecordError(*err)
		span.SetStatus(codes.Error, "process rabbitmq delivery")
	}
	span.End()
}
