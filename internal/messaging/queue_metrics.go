package messaging

import (
	"context"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

type QueueDepth struct {
	Queue         string
	MessagesReady int
	Consumers     int
}

type QueueMetricsProvider interface {
	QueueDepths(ctx context.Context) ([]QueueDepth, error)
}

type UnavailableQueueMetrics struct {
	Err error
}

func (m UnavailableQueueMetrics) QueueDepths(context.Context) ([]QueueDepth, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return nil, fmt.Errorf("rabbitmq queue metrics unavailable")
}

type QueueInspector struct {
	conn    *amqp.Connection
	channel *amqp.Channel
	queues  []string
}

func NewQueueInspector(url string, queues []string) (*QueueInspector, error) {
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
	if len(queues) == 0 {
		queues = QueueNames()
	}
	return &QueueInspector{conn: conn, channel: channel, queues: append([]string(nil), queues...)}, nil
}

func (i *QueueInspector) Close() error {
	channelErr := i.channel.Close()
	connErr := i.conn.Close()
	if channelErr != nil {
		return channelErr
	}
	return connErr
}

func (i *QueueInspector) QueueDepths(ctx context.Context) ([]QueueDepth, error) {
	depths := make([]QueueDepth, 0, len(i.queues))
	for _, queueName := range i.queues {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		queue, err := i.channel.QueueInspect(queueName)
		if err != nil {
			return nil, fmt.Errorf("inspect queue %s: %w", queueName, err)
		}
		depths = append(depths, QueueDepth{
			Queue:         queue.Name,
			MessagesReady: queue.Messages,
			Consumers:     queue.Consumers,
		})
	}
	return depths, nil
}
