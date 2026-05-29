package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/messaging"
	amqp "github.com/rabbitmq/amqp091-go"
)

func main() {
	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		log.Fatal("RABBITMQ_URL is required")
	}
	queue := os.Getenv("DLQ_QUEUE")
	routingKey := os.Getenv("TARGET_ROUTING_KEY")
	if queue == "" || routingKey == "" {
		log.Fatal("DLQ_QUEUE and TARGET_ROUTING_KEY are required")
	}
	limit := 100
	if raw := os.Getenv("REPLAY_LIMIT"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			log.Fatal(err)
		}
		limit = parsed
	}

	conn, err := amqp.Dial(rabbitURL)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	channel, err := conn.Channel()
	if err != nil {
		log.Fatal(err)
	}
	defer channel.Close()
	if err := messaging.DeclareTopology(channel); err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	replayed, err := messaging.ReplayDLQ(ctx, channel, messaging.DLQReplayConfig{
		Queue:      queue,
		Exchange:   messaging.DomainExchange,
		RoutingKey: routingKey,
		Limit:      limit,
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("replayed %d messages from %s", replayed, queue)
}
