package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisLimiter struct {
	client *redis.Client
	limit  int64
	window time.Duration
}

func NewRedisLimiter(client *redis.Client, limit int64, window time.Duration) *RedisLimiter {
	return &RedisLimiter{client: client, limit: limit, window: window}
}

func NewRedisClient(redisURL string) (*redis.Client, error) {
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	return redis.NewClient(options), nil
}

func (l *RedisLimiter) Allow(ctx context.Context, key string) (bool, error) {
	redisKey := "rate_limit:" + key
	pipe := l.client.TxPipeline()
	count := pipe.Incr(ctx, redisKey)
	pipe.Expire(ctx, redisKey, l.window)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, fmt.Errorf("increment rate limit: %w", err)
	}
	return count.Val() <= l.limit, nil
}
