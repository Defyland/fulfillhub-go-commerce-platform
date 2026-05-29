package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/api"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/messaging"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/postgres"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/ratelimit"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	shutdownTracing, err := configureTracing(logger)
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

	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	store := commerce.Store(commerce.NewMemoryStore())
	if databaseURL := os.Getenv("DATABASE_URL"); databaseURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		postgresStore, err := postgres.Open(ctx, databaseURL)
		if err != nil {
			fatal(logger, "open postgres", err)
		}
		defer postgresStore.Close()
		if err := postgres.RunMigrations(ctx, postgresStore.DB()); err != nil {
			fatal(logger, "run postgres migrations", err)
		}
		store = postgresStore
	}

	options := api.Options{
		Logger:       logger,
		OpsJWTSecret: os.Getenv("OPS_JWT_SECRET"),
	}
	if rabbitURL := os.Getenv("RABBITMQ_URL"); rabbitURL != "" {
		inspector, err := messaging.NewQueueInspector(rabbitURL, nil)
		if err != nil {
			logger.Error("create rabbitmq queue metrics inspector", "error", err)
			options.QueueMetrics = messaging.UnavailableQueueMetrics{Err: err}
		} else {
			defer inspector.Close()
			options.QueueMetrics = inspector
		}
	}
	if redisURL := os.Getenv("REDIS_URL"); redisURL != "" {
		client, err := ratelimit.NewRedisClient(redisURL)
		if err != nil {
			fatal(logger, "create redis client", err)
		}
		defer client.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := client.Ping(ctx).Err(); err != nil {
			cancel()
			fatal(logger, "ping redis", err)
		}
		cancel()
		options.RateLimiter = ratelimit.NewRedisLimiter(client, 120, time.Minute)
	}

	service := commerce.NewService(store)
	server := api.NewServerWithOptions(service, options)

	logger.Info("starting fulfillhub api", "addr", addr)
	if err := http.ListenAndServe(addr, server); err != nil {
		fatal(logger, "api server stopped", err)
	}
}

func configureTracing(logger *slog.Logger) (func(context.Context) error, error) {
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
	logger.Info("otel tracing enabled", "exporter", "stdout")
	return provider.Shutdown, nil
}

func fatal(logger *slog.Logger, message string, err error) {
	if err != nil {
		logger.Error(message, "error", err)
	} else {
		logger.Error(message)
	}
	os.Exit(1)
}
