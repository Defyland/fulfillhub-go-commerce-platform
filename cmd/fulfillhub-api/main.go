package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/api"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/messaging"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/observability"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/postgres"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/ratelimit"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	shutdownTracing, err := observability.ConfigureTracing(context.Background(), "fulfillhub-api", os.Getenv, logger)
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

	readinessChecks := map[string]api.ReadinessChecker{
		"store": api.ReadinessCheckFunc(func(context.Context) error { return nil }),
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
		readinessChecks["store"] = api.ReadinessCheckFunc(postgresStore.DB().PingContext)
	}

	options := api.Options{
		Logger:                logger,
		OpsJWTSecret:          os.Getenv("OPS_JWT_SECRET"),
		OpsJWTPreviousSecrets: splitCSV(os.Getenv("OPS_JWT_PREVIOUS_SECRETS")),
		OpsJWTIssuer:          os.Getenv("OPS_JWT_ISSUER"),
		OpsJWTAudience:        os.Getenv("OPS_JWT_AUDIENCE"),
		MetricsBearerToken:    os.Getenv("METRICS_BEARER_TOKEN"),
		ReadinessChecks:       readinessChecks,
	}
	if outboxBacklog, ok := store.(api.OutboxBacklogProvider); ok {
		options.OutboxBacklog = outboxBacklog
	}
	if rabbitURL := os.Getenv("RABBITMQ_URL"); rabbitURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		inspector, err := newQueueInspectorWithRetry(ctx, rabbitURL, logger)
		cancel()
		if err != nil {
			logger.Error("create rabbitmq queue metrics inspector", "error", err)
			options.QueueMetrics = messaging.UnavailableQueueMetrics{Err: err}
			options.ReadinessChecks["broker"] = api.ReadinessCheckFunc(func(context.Context) error {
				return err
			})
		} else {
			defer inspector.Close()
			options.QueueMetrics = inspector
			options.ReadinessChecks["broker"] = api.ReadinessCheckFunc(func(ctx context.Context) error {
				_, err := inspector.QueueDepths(ctx)
				return err
			})
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
		limit, err := rateLimitPerMinute(os.Getenv)
		if err != nil {
			fatal(logger, "load redis rate limit", err)
		}
		options.RateLimiter = ratelimit.NewRedisLimiter(client, limit, time.Minute)
		options.ReadinessChecks["cache"] = api.ReadinessCheckFunc(func(ctx context.Context) error {
			return client.Ping(ctx).Err()
		})
	}

	service := commerce.NewService(store)
	server := api.NewServerWithOptions(service, options)

	logger.Info("starting fulfillhub api", "addr", addr)
	if err := http.ListenAndServe(addr, server); err != nil {
		fatal(logger, "api server stopped", err)
	}
}

func newQueueInspectorWithRetry(ctx context.Context, rabbitURL string, logger *slog.Logger) (*messaging.QueueInspector, error) {
	var lastErr error
	for {
		inspector, err := messaging.NewQueueInspector(rabbitURL, nil)
		if err == nil {
			return inspector, nil
		}
		lastErr = err
		logger.Warn("rabbitmq queue metrics unavailable, retrying", "error", err)

		select {
		case <-ctx.Done():
			return nil, lastErr
		case <-time.After(2 * time.Second):
		}
	}
}

func fatal(logger *slog.Logger, message string, err error) {
	if err != nil {
		logger.Error(message, "error", err)
	} else {
		logger.Error(message)
	}
	os.Exit(1)
}

func rateLimitPerMinute(getenv func(string) string) (int64, error) {
	value := strings.TrimSpace(getenv("RATE_LIMIT_PER_MINUTE"))
	if value == "" {
		return 120, nil
	}
	limit, err := strconv.ParseInt(value, 10, 64)
	if err != nil || limit <= 0 {
		return 0, fmt.Errorf("RATE_LIMIT_PER_MINUTE must be a positive integer")
	}
	return limit, nil
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			values = append(values, part)
		}
	}
	return values
}
