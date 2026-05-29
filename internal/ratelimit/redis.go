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

const redisLimiterScript = `
local current = redis.call("INCR", KEYS[1])
if current == 1 then
	redis.call("PEXPIRE", KEYS[1], ARGV[1])
end
return current
`

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
	if l.window <= 0 {
		return false, fmt.Errorf("rate limit window must be positive")
	}
	redisKey := "rate_limit:" + key
	count, err := l.client.Eval(ctx, redisLimiterScript, []string{redisKey}, l.window.Milliseconds()).Int64()
	if err != nil {
		return false, fmt.Errorf("increment rate limit: %w", err)
	}
	return count <= l.limit, nil
}
