package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestParseRateLimitConfig(t *testing.T) {
	t.Run("parse limit as int", func(t *testing.T) {
		config := map[string]interface{}{
			"limit_by": "route",
			"limit":    5,
			"window":   "1m",
			"policy":   "local",
		}

		cfg := parseRateLimitConfig(config)

		if cfg.Limit != 5 {
			t.Errorf("Expected limit 5, got %d", cfg.Limit)
		}

		if cfg.LimitBy != LimitByRoute {
			t.Errorf("Expected LimitByRoute, got %v", cfg.LimitBy)
		}

		if cfg.Window.Duration() != 60*time.Second {
			t.Errorf("Expected window 60s, got %v", cfg.Window.Duration())
		}

		if cfg.Policy != PolicyLocal {
			t.Errorf("Expected PolicyLocal, got %v", cfg.Policy)
		}
	})

	t.Run("parse limit as float64", func(t *testing.T) {
		config := map[string]interface{}{
			"limit_by": "ip",
			"limit":    float64(10),
			"window":   "30s",
			"policy":   "redis",
		}

		cfg := parseRateLimitConfig(config)

		if cfg.Limit != 10 {
			t.Errorf("Expected limit 10, got %d", cfg.Limit)
		}

		if cfg.LimitBy != LimitByIP {
			t.Errorf("Expected LimitByIP, got %v", cfg.LimitBy)
		}

		if cfg.Window.Duration() != 30*time.Second {
			t.Errorf("Expected window 30s, got %v", cfg.Window.Duration())
		}

		if cfg.Policy != PolicyRedis {
			t.Errorf("Expected PolicyRedis, got %v", cfg.Policy)
		}
	})

	t.Run("default values", func(t *testing.T) {
		config := map[string]interface{}{}

		cfg := parseRateLimitConfig(config)

		if cfg.Limit != 100 {
			t.Errorf("Expected default limit 100, got %d", cfg.Limit)
		}

		if cfg.LimitBy != LimitByRoute {
			t.Errorf("Expected default LimitByRoute, got %v", cfg.LimitBy)
		}

		if cfg.Window.Duration() != 60*time.Second {
			t.Errorf("Expected default window 60s, got %v", cfg.Window.Duration())
		}

		if cfg.Policy != PolicyLocal {
			t.Errorf("Expected default PolicyLocal, got %v", cfg.Policy)
		}
	})

	t.Run("parse consumer limit_by", func(t *testing.T) {
		config := map[string]interface{}{
			"limit_by": "consumer",
		}

		cfg := parseRateLimitConfig(config)

		if cfg.LimitBy != LimitByConsumer {
			t.Errorf("Expected LimitByConsumer, got %v", cfg.LimitBy)
		}
	})
}

func TestLimitKeyGenerator(t *testing.T) {
	gen := &LimitKeyGenerator{}

	t.Run("generate route key", func(t *testing.T) {
		routeID := mustParseUUID("550e8400-e29b-41d4-a716-446655440000")
		key := gen.Generate(LimitByRoute, nil, routeID, "")

		if key != "ratelimit:route:550e8400-e29b-41d4-a716-446655440000" {
			t.Errorf("Unexpected key: %s", key)
		}
	})

	t.Run("generate consumer key with consumer", func(t *testing.T) {
		consumerID := mustParseUUID("550e8400-e29b-41d4-a716-446655440001")
		key := gen.Generate(LimitByConsumer, &consumerID, uuid.Nil, "")

		if key != "ratelimit:consumer:550e8400-e29b-41d4-a716-446655440001" {
			t.Errorf("Unexpected key: %s", key)
		}
	})

	t.Run("generate consumer key without consumer", func(t *testing.T) {
		key := gen.Generate(LimitByConsumer, nil, uuid.Nil, "")

		if key != "ratelimit:consumer:anonymous" {
			t.Errorf("Unexpected key: %s", key)
		}
	})

	t.Run("generate IP key", func(t *testing.T) {
		key := gen.Generate(LimitByIP, nil, uuid.Nil, "192.168.1.100")

		if key != "ratelimit:ip:192.168.1.100" {
			t.Errorf("Unexpected key: %s", key)
		}
	})
}

func TestLocalLimiter(t *testing.T) {
	limiter := NewLocalLimiter()

	t.Run("allow within limit", func(t *testing.T) {
		key := "test-local-limit-1"
		limit := 3
		window := 1 * time.Minute

		for i := 0; i < limit; i++ {
			result, err := limiter.Allow(context.Background(), key, limit, window)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if !result.Allowed {
				t.Errorf("Request %d should be allowed", i+1)
			}

			if result.Remaining != limit-i-1 {
				t.Errorf("Expected remaining %d, got %d", limit-i-1, result.Remaining)
			}
		}
	})

	t.Run("deny when limit exceeded", func(t *testing.T) {
		key := "test-local-limit-2"
		limit := 2
		window := 1 * time.Minute

		for i := 0; i < limit; i++ {
			result, _ := limiter.Allow(context.Background(), key, limit, window)
			if !result.Allowed {
				t.Errorf("Request %d should be allowed", i+1)
			}
		}

		result, _ := limiter.Allow(context.Background(), key, limit, window)
		if result.Allowed {
			t.Error("Request should be denied when limit exceeded")
		}

		if result.Remaining != 0 {
			t.Errorf("Expected remaining 0, got %d", result.Remaining)
		}
	})

	t.Run("different keys are independent", func(t *testing.T) {
		key1 := "test-local-limit-key1"
		key2 := "test-local-limit-key2"
		limit := 2
		window := 1 * time.Minute

		for i := 0; i < limit; i++ {
			result1, _ := limiter.Allow(context.Background(), key1, limit, window)
			result2, _ := limiter.Allow(context.Background(), key2, limit, window)

			if !result1.Allowed || !result2.Allowed {
				t.Errorf("Request %d should be allowed for both keys", i+1)
			}
		}

		result1, _ := limiter.Allow(context.Background(), key1, limit, window)
		result2, _ := limiter.Allow(context.Background(), key2, limit, window)

		if result1.Allowed || result2.Allowed {
			t.Error("Both keys should be denied when limit exceeded")
		}
	})
}

func mustParseUUID(s string) uuid.UUID {
	id, err := uuid.Parse(s)
	if err != nil {
		panic(err)
	}
	return id
}
