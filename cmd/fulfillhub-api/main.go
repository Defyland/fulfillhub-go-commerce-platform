package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/api"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/commerce"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/postgres"
	"github.com/Defyland/fulfillhub-go-commerce-platform/internal/ratelimit"
)

func main() {
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
			log.Fatal(err)
		}
		defer postgresStore.Close()
		if err := postgres.RunMigrations(ctx, postgresStore.DB()); err != nil {
			log.Fatal(err)
		}
		store = postgresStore
	}

	options := api.Options{}
	if redisURL := os.Getenv("REDIS_URL"); redisURL != "" {
		client, err := ratelimit.NewRedisClient(redisURL)
		if err != nil {
			log.Fatal(err)
		}
		defer client.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := client.Ping(ctx).Err(); err != nil {
			cancel()
			log.Fatal(err)
		}
		cancel()
		options.RateLimiter = ratelimit.NewRedisLimiter(client, 120, time.Minute)
	}

	service := commerce.NewService(store)
	server := api.NewServerWithOptions(service, options)

	log.Printf("starting fulfillhub api on %s", addr)
	if err := http.ListenAndServe(addr, server); err != nil {
		log.Fatal(err)
	}
}
