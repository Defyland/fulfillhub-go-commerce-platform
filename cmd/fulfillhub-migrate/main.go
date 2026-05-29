package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/postgres"
)

type settings struct {
	databaseURL string
	timeout     time.Duration
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := loadSettings(os.Getenv)
	if err != nil {
		fatal(logger, "load settings", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	store, err := postgres.Open(ctx, cfg.databaseURL)
	if err != nil {
		fatal(logger, "open postgres", err)
	}
	defer store.Close()

	if err := postgres.RunMigrations(ctx, store.DB()); err != nil {
		fatal(logger, "run postgres migrations", err)
	}
	logger.Info("postgres migrations applied")
}

func loadSettings(getenv func(string) string) (settings, error) {
	cfg := settings{
		databaseURL: strings.TrimSpace(getenv("DATABASE_URL")),
		timeout:     30 * time.Second,
	}
	if cfg.databaseURL == "" {
		return settings{}, fmt.Errorf("DATABASE_URL is required")
	}
	if raw := strings.TrimSpace(getenv("MIGRATION_TIMEOUT")); raw != "" {
		timeout, err := time.ParseDuration(raw)
		if err != nil || timeout <= 0 {
			return settings{}, fmt.Errorf("MIGRATION_TIMEOUT must be a positive duration")
		}
		cfg.timeout = timeout
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
