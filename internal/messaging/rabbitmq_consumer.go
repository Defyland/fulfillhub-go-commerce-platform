package messaging

import (
	"context"
	"fmt"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
	amqp "github.com/rabbitmq/amqp091-go"
)

type RabbitConsumer struct {
	conn    *amqp.Connection
	channel *amqp.Channel
}

func NewRabbitConsumer(url string) (*RabbitConsumer, error) {
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
	if err := channel.Qos(1, 0, false); err != nil {
		_ = channel.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("configure consumer qos: %w", err)
	}
	return &RabbitConsumer{conn: conn, channel: channel}, nil
}

func (c *RabbitConsumer) Close() error {
	channelErr := c.channel.Close()
	connErr := c.conn.Close()
	if channelErr != nil {
		return channelErr
	}
	return connErr
}

func (c *RabbitConsumer) Deliveries(queue, consumerName string) (<-chan amqp.Delivery, error) {
	if queue == "" {
		return nil, fmt.Errorf("queue is required")
	}
	if consumerName == "" {
		consumerName = queue
	}
	deliveries, err := c.channel.Consume(queue, consumerName, false, false, false, false, nil)
	if err != nil {
		return nil, fmt.Errorf("consume queue %s: %w", queue, err)
	}
	return deliveries, nil
}

func (c *RabbitConsumer) PublishRetry(ctx context.Context, delivery amqp.Delivery, event commerce.OutboxEvent, attempt int) error {
	if ctx == nil {
		ctx = context.Background()
	}
	routingKey := delivery.RoutingKey
	if routingKey == "" {
		routingKey = RoutingKey(event.EventType)
	}
	headers := cloneHeaders(delivery.Headers)
	headers["fulfillhub_retry_attempt"] = int32(attempt)
	if err := c.channel.PublishWithContext(ctx, RetryExchange, routingKey, false, false, amqp.Publishing{
		ContentType:   delivery.ContentType,
		DeliveryMode:  amqp.Persistent,
		MessageId:     firstNonEmpty(delivery.MessageId, event.MessageID),
		CorrelationId: firstNonEmpty(delivery.CorrelationId, event.CorrelationID),
		Timestamp:     time.Now().UTC(),
		Type:          firstNonEmpty(delivery.Type, event.EventType),
		Headers:       headers,
		Body:          delivery.Body,
	}); err != nil {
		return fmt.Errorf("publish retry message: %w", err)
	}
	return nil
}

func cloneHeaders(headers amqp.Table) amqp.Table {
	clone := amqp.Table{}
	for key, value := range headers {
		clone[key] = value
	}
	return clone
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
