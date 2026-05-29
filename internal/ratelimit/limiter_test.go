package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestMemoryLimiterAllowsWithinWindowLimit(t *testing.T) {
	limiter := NewMemoryLimiter(2, time.Minute)

	for i := 0; i < 2; i++ {
		allowed, err := limiter.Allow(context.Background(), "merchant:create-order")
		if err != nil {
			t.Fatalf("Allow returned error: %v", err)
		}
		if !allowed {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	allowed, err := limiter.Allow(context.Background(), "merchant:create-order")
	if err != nil {
		t.Fatalf("Allow returned error: %v", err)
	}
	if allowed {
		t.Fatal("third request in same window must be rate limited")
	}
}

func TestMemoryLimiterResetsAfterWindow(t *testing.T) {
	limiter := NewMemoryLimiter(1, time.Minute)
	now := time.Date(2026, 5, 28, 20, 0, 0, 0, time.UTC)
	limiter.clock = func() time.Time { return now }

	if allowed, _ := limiter.Allow(context.Background(), "merchant:create-order"); !allowed {
		t.Fatal("first request should be allowed")
	}
	if allowed, _ := limiter.Allow(context.Background(), "merchant:create-order"); allowed {
		t.Fatal("second request should be limited")
	}

	now = now.Add(time.Minute + time.Second)
	if allowed, _ := limiter.Allow(context.Background(), "merchant:create-order"); !allowed {
		t.Fatal("request after window reset should be allowed")
	}
}

func TestRedisLimiterResetsAfterOriginalWindowUnderContinuousTraffic(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	limiter := NewRedisLimiter(client, 2, time.Minute)
	ctx := context.Background()
	key := "merchant:create-order"

	if allowed, err := limiter.Allow(ctx, key); err != nil || !allowed {
		t.Fatalf("first request allowed=%v err=%v", allowed, err)
	}

	server.FastForward(30 * time.Second)
	if allowed, err := limiter.Allow(ctx, key); err != nil || !allowed {
		t.Fatalf("second request allowed=%v err=%v", allowed, err)
	}

	server.FastForward(31 * time.Second)
	if allowed, err := limiter.Allow(ctx, key); err != nil || !allowed {
		t.Fatalf("request after original window allowed=%v err=%v", allowed, err)
	}
}

func TestRedisLimiterRejectsWithinWindowLimit(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	limiter := NewRedisLimiter(client, 1, time.Minute)
	ctx := context.Background()
	key := "merchant:create-order"

	if allowed, err := limiter.Allow(ctx, key); err != nil || !allowed {
		t.Fatalf("first request allowed=%v err=%v", allowed, err)
	}
	if allowed, err := limiter.Allow(ctx, key); err != nil || allowed {
		t.Fatalf("second request allowed=%v err=%v", allowed, err)
	}
}
