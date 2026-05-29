package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/fulfillment"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/messaging"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/observability"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/postgres"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/providers"
)

type settings struct {
	databaseURL  string
	rabbitURL    string
	queue        string
	consumerName string
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	shutdownTracing, err := observability.ConfigureTracing(context.Background(), "fulfillhub-worker", os.Getenv, logger)
	if err != nil {
		fatal(logger, "configure tracing", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTracing(ctx); err != nil {
			logger.Error("shutdown tracing", "error", err)
		}
	}()

	cfg, err := loadSettings()
	if err != nil {
		fatal(logger, "load settings", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := postgres.Open(ctx, cfg.databaseURL)
	if err != nil {
		fatal(logger, "open postgres", err)
	}
	defer store.Close()
	if err := postgres.RunMigrations(ctx, store.DB()); err != nil {
		fatal(logger, "run postgres migrations", err)
	}

	rabbitConsumer, err := newRabbitConsumerWithRetry(ctx, cfg.rabbitURL, logger)
	if err != nil {
		fatal(logger, "create rabbit consumer", err)
	}
	defer rabbitConsumer.Close()

	handler, err := fulfillment.HandlerForQueue(cfg.queue, fulfillment.Dependencies{
		Projector: store,
		Orders:    store,
		PaymentAuthorizer: fulfillment.ProviderPaymentAuthorizer{
			Orders:   store,
			Provider: providers.FakePaymentProvider{},
		},
		ShipmentCreator: fulfillment.ProviderShipmentCreator{
			Orders:   store,
			Provider: providers.FakeShipmentProvider{},
		},
	})
	if err != nil {
		fatal(logger, "create worker handler", err)
	}

	deliveries, err := rabbitConsumer.Deliveries(cfg.queue, cfg.consumerName)
	if err != nil {
		fatal(logger, "start queue consumer", err)
	}

	consumer := messaging.Consumer{
		Queue:        cfg.queue,
		ConsumerName: cfg.consumerName,
		Inbox:        messaging.PersistentInbox{Recorder: store},
		Handler:      handler,
		Retry:        rabbitConsumer,
	}

	logger.Info("starting fulfillhub worker", "queue", cfg.queue, "consumer_name", cfg.consumerName)
	for {
		select {
		case <-ctx.Done():
			return
		case delivery, ok := <-deliveries:
			if !ok {
				logger.Error("rabbitmq deliveries channel closed", "queue", cfg.queue)
				return
			}
			if err := consumer.ProcessDelivery(ctx, delivery); err != nil {
				logger.Error(
					"process rabbitmq delivery",
					"error", err,
					"queue", cfg.queue,
					"message_id", delivery.MessageId,
					"routing_key", delivery.RoutingKey,
				)
			}
		}
	}
}

func newRabbitConsumerWithRetry(ctx context.Context, rabbitURL string, logger *slog.Logger) (*messaging.RabbitConsumer, error) {
	deadline := time.NewTimer(60 * time.Second)
	defer deadline.Stop()

	var lastErr error
	for {
		consumer, err := messaging.NewRabbitConsumer(rabbitURL)
		if err == nil {
			return consumer, nil
		}
		lastErr = err
		logger.Warn("rabbitmq consumer unavailable, retrying", "error", err)

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, lastErr
		case <-time.After(2 * time.Second):
		}
	}
}

func loadSettings() (settings, error) {
	cfg := settings{
		databaseURL:  os.Getenv("DATABASE_URL"),
		rabbitURL:    os.Getenv("RABBITMQ_URL"),
		queue:        os.Getenv("WORKER_QUEUE"),
		consumerName: os.Getenv("CONSUMER_NAME"),
	}
	if cfg.databaseURL == "" {
		return settings{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.rabbitURL == "" {
		return settings{}, fmt.Errorf("RABBITMQ_URL is required")
	}
	if cfg.queue == "" {
		cfg.queue = messaging.InventoryReserveQueue
	}
	if cfg.consumerName == "" {
		cfg.consumerName = cfg.queue
	}
	return cfg, nil
}

func fatal(logger *slog.Logger, message string, err error) {
	if err != nil {
		logger.Error(message, "error", err)
	} else {
		logger.Error(message)
	}
	os.Exit(1)
}
