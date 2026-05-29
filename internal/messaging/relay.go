package messaging

import (
	"context"
	"fmt"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
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
}

func (r Relay) RunOnce(ctx context.Context, limit int) (int, error) {
	if r.Clock == nil {
		r.Clock = func() time.Time { return time.Now().UTC() }
	}

	events, err := r.Source.PendingOutboxEvents(ctx, limit)
	if err != nil {
		return 0, fmt.Errorf("load pending outbox events: %w", err)
	}

	published := 0
	for _, event := range events {
		if err := r.Publisher.Publish(ctx, event); err != nil {
			return published, fmt.Errorf("publish %s: %w", event.MessageID, err)
		}
		if err := r.Source.MarkOutboxPublished(ctx, event.MessageID, r.Clock()); err != nil {
			return published, fmt.Errorf("mark %s published: %w", event.MessageID, err)
		}
		published++
	}
	return published, nil
}
