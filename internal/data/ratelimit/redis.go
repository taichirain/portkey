package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisLimiter struct {
	client *redis.Client
}

func NewRedisLimiter(client *redis.Client) *RedisLimiter {
	return &RedisLimiter{
		client: client,
	}
}

func (r *RedisLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (*LimitResult, error) {
	now := time.Now()
	windowStart := now.Add(-window)
	uniqueID := now.UnixNano()

	script := redis.NewScript(`
		local key = KEYS[1]
		local limit = tonumber(ARGV[1])
		local window_ms = tonumber(ARGV[2])
		local now_ms = tonumber(ARGV[3])
		local window_start_ms = tonumber(ARGV[4])
		local unique_id = ARGV[5]
		
		local window_end_ms = now_ms + window_ms
		
		redis.call('ZREMRANGEBYSCORE', key, '-inf', window_start_ms)
		
		local count = redis.call('ZCARD', key)
		
		if count >= limit then
			local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
			if #oldest > 0 then
				return {0, count, tonumber(oldest[2]) + window_ms}
			else
				return {0, count, window_end_ms}
			end
		end
		
		redis.call('ZADD', key, now_ms, unique_id)
		redis.call('PEXPIREAT', key, window_end_ms)
		
		return {1, count + 1, window_end_ms}
	`)

	result, err := script.Run(ctx, r.client, []string{key},
		limit,
		window.Milliseconds(),
		now.UnixMilli(),
		windowStart.UnixMilli(),
		fmt.Sprintf("%d", uniqueID),
	).Result()

	if err != nil {
		return nil, fmt.Errorf("redis rate limit script failed: %w", err)
	}

	results, ok := result.([]interface{})
	if !ok || len(results) < 3 {
		return nil, fmt.Errorf("unexpected redis result format")
	}

	allowed, _ := results[0].(int64)
	count, _ := results[1].(int64)
	resetMs, _ := results[2].(int64)
	resetTime := time.UnixMilli(resetMs)

	remaining := limit - int(count)
	if remaining < 0 {
		remaining = 0
	}

	return &LimitResult{
		Allowed:   allowed == 1,
		Remaining: remaining,
		Reset:     resetTime,
		Limit:     limit,
		Window:    window,
	}, nil
}

type RedisConfig struct {
	Host     string
	Port     int
	Password string
	DB       int
}

func NewRedisClient(cfg *RedisConfig) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	return client, nil
}
