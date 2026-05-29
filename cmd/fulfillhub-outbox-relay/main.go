package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/messaging"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/postgres"
)

func main() {
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

	publisher, err := messaging.NewRabbitPublisher(rabbitURL)
	if err != nil {
		log.Fatal(err)
	}
	defer publisher.Close()

	relay := messaging.Relay{Source: store, Publisher: publisher}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		published, err := relay.RunOnce(ctx, 50)
		if err != nil {
			log.Printf("outbox relay error: %v", err)
		}
		if published > 0 {
			log.Printf("published %d outbox events", published)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
