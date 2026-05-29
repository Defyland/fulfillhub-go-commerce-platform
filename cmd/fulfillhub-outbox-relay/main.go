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
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func main() {
	shutdownTracing, err := configureTracing()
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

func configureTracing() (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	if os.Getenv("OTEL_TRACES_EXPORTER") != "stdout" {
		return func(context.Context) error { return nil }, nil
	}
	exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		return nil, err
	}
	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter))
	otel.SetTracerProvider(provider)
	return provider.Shutdown, nil
}
