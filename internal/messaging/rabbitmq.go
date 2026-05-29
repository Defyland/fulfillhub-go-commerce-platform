package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
	amqp "github.com/rabbitmq/amqp091-go"
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
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	return p.channel.PublishWithContext(ctx, DomainExchange, RoutingKey(event.EventType), false, false, amqp.Publishing{
		ContentType:   "application/json",
		DeliveryMode:  amqp.Persistent,
		MessageId:     event.MessageID,
		CorrelationId: event.CorrelationID,
		Timestamp:     time.Now().UTC(),
		Type:          event.EventType,
		Headers: amqp.Table{
			"merchant_id": event.MerchantID,
			"order_id":    event.OrderID,
		},
		Body: body,
	})
}

func DeclareTopology(channel *amqp.Channel) error {
	for _, exchange := range []string{DomainExchange, RetryExchange, DLXExchange} {
		if err := channel.ExchangeDeclare(exchange, "topic", true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare exchange %s: %w", exchange, err)
		}
	}

	bindings := map[string][]string{
		InventoryReserveQueue:   {"order.created"},
		PaymentsAuthorizeQueue:  {"inventory.reserved"},
		ShipmentsCreateQueue:    {"payment.authorized"},
		OrdersCompensateQueue:   {"inventory.rejected", "payment.failed", "shipment.failed"},
		NotificationsEmailQueue: {"order.completed", "order.cancelled"},
	}

	for queue, keys := range bindings {
		if _, err := channel.QueueDeclare(queue, true, false, false, false, amqp.Table{
			"x-dead-letter-exchange": DLXExchange,
		}); err != nil {
			return fmt.Errorf("declare queue %s: %w", queue, err)
		}
		for _, key := range keys {
			if err := channel.QueueBind(queue, key, DomainExchange, false, nil); err != nil {
				return fmt.Errorf("bind queue %s to %s: %w", queue, key, err)
			}
		}
	}
	return nil
}
