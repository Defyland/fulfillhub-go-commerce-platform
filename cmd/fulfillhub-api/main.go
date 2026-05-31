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
	allowLocalDemoCredentials, err := boolEnv(os.Getenv, "ALLOW_LOCAL_DEMO_CREDENTIALS")
	if err != nil {
		fatal(logger, "load local demo credential setting", err)
	}
	allowLocalOpsToken, err := boolEnv(os.Getenv, "ALLOW_LOCAL_OPS_TOKEN")
	if err != nil {
		fatal(logger, "load local ops token setting", err)
	}
	databaseURL, allowInMemoryStore, err := durableStoreConfig(os.Getenv)
	if err != nil {
		fatal(logger, "load durable store setting", err)
	}
	apiKeys, err := merchantAPIKeys(os.Getenv)
	if err != nil {
		fatal(logger, "load merchant api keys", err)
	}
	if len(apiKeys) == 0 {
		if !allowLocalDemoCredentials {
			fatal(logger, "MERCHANT_API_KEYS is required unless ALLOW_LOCAL_DEMO_CREDENTIALS=true", nil)
		}
		apiKeys = api.LocalDemoAPIKeys()
	}

	readinessChecks := map[string]api.ReadinessChecker{
		"store": api.ReadinessCheckFunc(func(context.Context) error { return nil }),
	}
	var store commerce.Store
	if databaseURL != "" {
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
	} else if allowInMemoryStore {
		logger.Warn("using in-memory store; order state and outbox are not durable")
		store = commerce.NewMemoryStore()
	}

	options := api.Options{
		Logger:                logger,
		OpsJWTSecret:          os.Getenv("OPS_JWT_SECRET"),
		OpsJWTPreviousSecrets: splitCSV(os.Getenv("OPS_JWT_PREVIOUS_SECRETS")),
		OpsJWTIssuer:          os.Getenv("OPS_JWT_ISSUER"),
		OpsJWTAudience:        os.Getenv("OPS_JWT_AUDIENCE"),
		MetricsBearerToken:    os.Getenv("METRICS_BEARER_TOKEN"),
		ReadinessChecks:       readinessChecks,
		APIKeys:               apiKeys,
		AllowLocalOpsToken:    allowLocalOpsToken,
	}
	if outboxBacklog, ok := store.(api.OutboxBacklogProvider); ok {
		options.OutboxBacklog = outboxBacklog
	}
	if sagaMetrics, ok := store.(api.SagaMetricsProvider); ok {
		options.SagaMetrics = sagaMetrics
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
	handler := api.NewServerWithOptions(service, options)
	server := newHTTPServer(addr, handler)

	logger.Info("starting fulfillhub api", "addr", addr)
	if err := server.ListenAndServe(); err != nil {
		fatal(logger, "api server stopped", err)
	}
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
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

func durableStoreConfig(getenv func(string) string) (databaseURL string, allowInMemory bool, err error) {
	databaseURL = strings.TrimSpace(getenv("DATABASE_URL"))
	allowInMemory, err = boolEnv(getenv, "ALLOW_IN_MEMORY_STORE")
	if err != nil {
		return "", false, err
	}
	if databaseURL == "" && !allowInMemory {
		return "", false, fmt.Errorf("DATABASE_URL is required unless ALLOW_IN_MEMORY_STORE=true")
	}
	return databaseURL, allowInMemory, nil
}

func merchantAPIKeys(getenv func(string) string) (map[string]string, error) {
	value := strings.TrimSpace(getenv("MERCHANT_API_KEYS"))
	if value == "" {
		return nil, nil
	}
	keys := make(map[string]string)
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		apiKey, merchantID, ok := strings.Cut(part, "=")
		apiKey = strings.TrimSpace(apiKey)
		merchantID = strings.TrimSpace(merchantID)
		if !ok || apiKey == "" || merchantID == "" {
			return nil, fmt.Errorf("MERCHANT_API_KEYS must use api_key=merchant_id pairs separated by commas")
		}
		keys[apiKey] = merchantID
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("MERCHANT_API_KEYS must contain at least one api_key=merchant_id pair")
	}
	return keys, nil
}

func boolEnv(getenv func(string) string, key string) (bool, error) {
	value := strings.TrimSpace(getenv(key))
	if value == "" {
		return false, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean", key)
	}
	return parsed, nil
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
