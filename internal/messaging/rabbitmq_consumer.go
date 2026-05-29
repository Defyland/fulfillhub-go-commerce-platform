package messaging

import (
	"fmt"

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
