package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type RabbitPublisher struct {
	conn    *amqp.Connection
	channel *amqp.Channel
}

func NewRabbitPublisher(url string) (*RabbitPublisher, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, fmt.Errorf("dial rabbitmq: %w", err)
	}
	channel, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("open rabbitmq channel: %w", err)
	}
	if err := DeclareTopology(channel); err != nil {
		_ = channel.Close()
		_ = conn.Close()
		return nil, err
	}
	return &RabbitPublisher{conn: conn, channel: channel}, nil
}

func (p *RabbitPublisher) Close() error {
	channelErr := p.channel.Close()
	connErr := p.conn.Close()
	if channelErr != nil {
		return channelErr
	}
	return connErr
}

func (p *RabbitPublisher) Publish(ctx context.Context, event commerce.OutboxEvent) error {
	event = event.WithDefaultCausation()
	ctx, span := messagingTracer().Start(ctx, "rabbitmq.publish", trace.WithAttributes(
		attribute.String("messaging.system", "rabbitmq"),
		attribute.String("messaging.destination.name", DomainExchange),
		attribute.String("messaging.rabbitmq.routing_key", RoutingKey(event.EventType)),
		attribute.String("messaging.message.id", event.MessageID),
		attribute.String("fulfillhub.event_type", event.EventType),
		attribute.String("fulfillhub.correlation_id", event.CorrelationID),
		attribute.String("fulfillhub.causation_id", event.CausationID),
		attribute.String("fulfillhub.order_id", event.OrderID),
		attribute.String("fulfillhub.merchant_id", event.MerchantID),
	))
	defer span.End()

	body, err := json.Marshal(event)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "marshal event")
		return fmt.Errorf("marshal event: %w", err)
	}
	headers := amqp.Table{
		"causation_id": event.CausationID,
		"merchant_id":  event.MerchantID,
		"order_id":     event.OrderID,
	}
	injectTraceHeaders(ctx, headers)
	if err := p.channel.PublishWithContext(ctx, DomainExchange, RoutingKey(event.EventType), false, false, amqp.Publishing{
		ContentType:   "application/json",
		DeliveryMode:  amqp.Persistent,
		MessageId:     event.MessageID,
		CorrelationId: event.CorrelationID,
		Timestamp:     time.Now().UTC(),
		Type:          event.EventType,
		Headers:       headers,
		Body:          body,
	}); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "publish rabbitmq message")
		return err
	}
	return nil
}

func injectTraceHeaders(ctx context.Context, headers amqp.Table) {
	propagation.TraceContext{}.Inject(ctx, amqpHeaderCarrier{headers: headers})
}

type amqpHeaderCarrier struct {
	headers amqp.Table
}

func (c amqpHeaderCarrier) Get(key string) string {
	value, ok := c.headers[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return ""
	}
}

func (c amqpHeaderCarrier) Set(key, value string) {
	c.headers[key] = value
}

func (c amqpHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c.headers))
	for key := range c.headers {
		keys = append(keys, key)
	}
	return keys
}

func DeclareTopology(channel *amqp.Channel) error {
	for _, exchange := range []string{DomainExchange, RetryExchange, DLXExchange} {
		if err := channel.ExchangeDeclare(exchange, "topic", true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare exchange %s: %w", exchange, err)
		}
	}

	for _, topology := range QueueTopologies() {
		if _, err := channel.QueueDeclare(topology.Queue, true, false, false, false, amqp.Table{
			"x-dead-letter-exchange": DLXExchange,
		}); err != nil {
			return fmt.Errorf("declare queue %s: %w", topology.Queue, err)
		}
		if _, err := channel.QueueDeclare(topology.RetryQueue, true, false, false, false, amqp.Table{
			"x-message-ttl":          int32(topology.RetryTTL / time.Millisecond),
			"x-dead-letter-exchange": DomainExchange,
		}); err != nil {
			return fmt.Errorf("declare retry queue %s: %w", topology.RetryQueue, err)
		}
		if _, err := channel.QueueDeclare(topology.DLQ, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare dlq %s: %w", topology.DLQ, err)
		}
		for _, key := range topology.RoutingKeys {
			if err := channel.QueueBind(topology.Queue, key, DomainExchange, false, nil); err != nil {
				return fmt.Errorf("bind queue %s to %s: %w", topology.Queue, key, err)
			}
			if err := channel.QueueBind(topology.RetryQueue, key, RetryExchange, false, nil); err != nil {
				return fmt.Errorf("bind retry queue %s to %s: %w", topology.RetryQueue, key, err)
			}
			if err := channel.QueueBind(topology.DLQ, key, DLXExchange, false, nil); err != nil {
				return fmt.Errorf("bind dlq %s to %s: %w", topology.DLQ, key, err)
			}
		}
	}
	return nil
}

func messagingTracer() trace.Tracer {
	return otel.Tracer("github.com/Defyland/fulfillhub-go-commerce-platform/internal/messaging")
}
