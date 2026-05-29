package messaging

import (
	"context"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

type DLQReplayConfig struct {
	Queue      string
	Exchange   string
	RoutingKey string
	Limit      int
}

func ReplayDLQ(ctx context.Context, channel *amqp.Channel, config DLQReplayConfig) (int, error) {
	if config.Queue == "" || config.Exchange == "" || config.RoutingKey == "" {
		return 0, fmt.Errorf("queue, exchange, and routing key are required")
	}
	if config.Limit <= 0 {
		config.Limit = 100
	}

	replayed := 0
	for replayed < config.Limit {
		delivery, ok, err := channel.Get(config.Queue, false)
		if err != nil {
			return replayed, fmt.Errorf("get dlq message: %w", err)
		}
		if !ok {
			return replayed, nil
		}

		if err := channel.PublishWithContext(ctx, config.Exchange, config.RoutingKey, false, false, amqp.Publishing{
			ContentType:   delivery.ContentType,
			DeliveryMode:  amqp.Persistent,
			MessageId:     delivery.MessageId,
			CorrelationId: delivery.CorrelationId,
			Timestamp:     delivery.Timestamp,
			Type:          delivery.Type,
			Headers:       delivery.Headers,
			Body:          delivery.Body,
		}); err != nil {
			_ = delivery.Nack(false, true)
			return replayed, fmt.Errorf("republish dlq message: %w", err)
		}
		if err := delivery.Ack(false); err != nil {
			return replayed, fmt.Errorf("ack dlq message: %w", err)
		}
		replayed++
	}
	return replayed, nil
}
