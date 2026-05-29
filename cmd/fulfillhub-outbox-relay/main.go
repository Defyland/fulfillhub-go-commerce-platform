package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/messaging"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/observability"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/postgres"
)

func main() {
	shutdownTracing, err := observability.ConfigureTracing(context.Background(), "fulfillhub-outbox-relay", os.Getenv, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracing(ctx); err != nil {
			log.Printf("shutdown tracing: %v", err)
		}
	}()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		log.Fatal("RABBITMQ_URL is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	if err := postgres.RunMigrations(ctx, store.DB()); err != nil {
		log.Fatal(err)
	}

	publisher, err := newRabbitPublisherWithRetry(ctx, rabbitURL)
	if err != nil {
		log.Fatal(err)
	}
	defer publisher.Close()

	batchSize, err := relayBatchSize(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	interval, err := relayInterval(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}

	relay := messaging.Relay{Source: store, Publisher: publisher}

	for {
		published, err := relay.RunOnce(ctx, batchSize)
		if err != nil {
			log.Printf("outbox relay error: %v", err)
		}
		if published > 0 {
			log.Printf("published %d outbox events", published)
		}
		if published == batchSize {
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
		}
	}
}

func relayBatchSize(getenv func(string) string) (int, error) {
	value := strings.TrimSpace(getenv("OUTBOX_RELAY_BATCH_SIZE"))
	if value == "" {
		return 100, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("OUTBOX_RELAY_BATCH_SIZE must be a positive integer")
	}
	return parsed, nil
}

func relayInterval(getenv func(string) string) (time.Duration, error) {
	value := strings.TrimSpace(getenv("OUTBOX_RELAY_INTERVAL"))
	if value == "" {
		return 250 * time.Millisecond, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("OUTBOX_RELAY_INTERVAL must be a positive duration")
	}
	return parsed, nil
}

func newRabbitPublisherWithRetry(ctx context.Context, rabbitURL string) (*messaging.RabbitPublisher, error) {
	deadline := time.NewTimer(60 * time.Second)
	defer deadline.Stop()

	var lastErr error
	for {
		publisher, err := messaging.NewRabbitPublisher(rabbitURL)
		if err == nil {
			return publisher, nil
		}
		lastErr = err
		log.Printf("rabbitmq publisher unavailable, retrying: %v", err)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, lastErr
		case <-time.After(2 * time.Second):
		}
	}
}
