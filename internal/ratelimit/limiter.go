package ratelimit

import (
	"context"
	"sync"
	"time"
)

type Limiter interface {
	Allow(ctx context.Context, key string) (bool, error)
}

type MemoryLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	clock   func() time.Time
	buckets map[string]bucket
}

type bucket struct {
	count int
	reset time.Time
}

func NewMemoryLimiter(limit int, window time.Duration) *MemoryLimiter {
	return &MemoryLimiter{
		limit:   limit,
		window:  window,
		clock:   func() time.Time { return time.Now().UTC() },
		buckets: make(map[string]bucket),
	}
}

func (l *MemoryLimiter) Allow(_ context.Context, key string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.clock()
	current := l.buckets[key]
	if current.reset.IsZero() || !now.Before(current.reset) {
		current = bucket{reset: now.Add(l.window)}
	}
	current.count++
	l.buckets[key] = current

	return current.count <= l.limit, nil
}
