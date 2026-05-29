package messaging

import (
	"context"
	"fmt"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type OutboxSource interface {
	PendingOutboxEvents(ctx context.Context, limit int) ([]commerce.OutboxEvent, error)
	MarkOutboxPublished(ctx context.Context, messageID string, publishedAt time.Time) error
}

type Publisher interface {
	Publish(ctx context.Context, event commerce.OutboxEvent) error
}

type Relay struct {
	Source    OutboxSource
	Publisher Publisher
	Clock     func() time.Time
	Tracer    trace.Tracer
}

func (r Relay) RunOnce(ctx context.Context, limit int) (int, error) {
	if r.Clock == nil {
		r.Clock = func() time.Time { return time.Now().UTC() }
	}
	tracer := r.Tracer
	if tracer == nil {
		tracer = messagingTracer()
	}
	ctx, span := tracer.Start(ctx, "outbox.relay.run_once", trace.WithAttributes(
		attribute.Int("fulfillhub.outbox.limit", limit),
	))
	defer span.End()

	events, err := r.Source.PendingOutboxEvents(ctx, limit)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "load pending outbox events")
		return 0, fmt.Errorf("load pending outbox events: %w", err)
	}
	span.SetAttributes(attribute.Int("fulfillhub.outbox.pending_count", len(events)))

	published := 0
	for _, event := range events {
		publishCtx, publishSpan := tracer.Start(ctx, "outbox.publish", trace.WithAttributes(
			attribute.String("messaging.message.id", event.MessageID),
			attribute.String("fulfillhub.event_type", event.EventType),
			attribute.String("fulfillhub.correlation_id", event.CorrelationID),
			attribute.String("fulfillhub.order_id", event.OrderID),
			attribute.String("fulfillhub.merchant_id", event.MerchantID),
		))
		if err := r.Publisher.Publish(publishCtx, event); err != nil {
			publishSpan.RecordError(err)
			publishSpan.SetStatus(codes.Error, "publish outbox event")
			publishSpan.End()
			span.RecordError(err)
			span.SetStatus(codes.Error, "publish outbox event")
			return published, fmt.Errorf("publish %s: %w", event.MessageID, err)
		}
		if err := r.Source.MarkOutboxPublished(publishCtx, event.MessageID, r.Clock()); err != nil {
			publishSpan.RecordError(err)
			publishSpan.SetStatus(codes.Error, "mark outbox event published")
			publishSpan.End()
			span.RecordError(err)
			span.SetStatus(codes.Error, "mark outbox event published")
			return published, fmt.Errorf("mark %s published: %w", event.MessageID, err)
		}
		publishSpan.End()
		published++
	}
	span.SetAttributes(attribute.Int("fulfillhub.outbox.published_count", published))
	return published, nil
}
