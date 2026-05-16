package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func getRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	// 使用独立 DB 15 避免污染其他环境，连接后自动清空
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 15})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available, skipping test: %v", err)
	}
	if err := client.FlushDB(ctx).Err(); err != nil {
		t.Logf("Warning: failed to flush redis test db: %v", err)
	}
	t.Cleanup(func() {
		_ = client.FlushDB(context.Background()).Err()
		client.Close()
	})
	return client
}

func uniqueKey(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func TestRedisLimiter_Allow_WithinLimit(t *testing.T) {
	client := getRedisClient(t)
	limiter := NewRedisLimiter(client)

	key := uniqueKey("allow")
	limit := 5
	window := 1 * time.Minute

	for i := 0; i < limit; i++ {
		result, err := limiter.Allow(context.Background(), key, limit, window)
		if err != nil {
			t.Fatalf("Request %d: unexpected error: %v", i+1, err)
		}
		if !result.Allowed {
			t.Errorf("Request %d: should be allowed", i+1)
		}
		if result.Remaining != limit-i-1 {
			t.Errorf("Request %d: expected remaining %d, got %d", i+1, limit-i-1, result.Remaining)
		}
	}
}

func TestRedisLimiter_Deny_ExceedLimit(t *testing.T) {
	client := getRedisClient(t)
	limiter := NewRedisLimiter(client)

	key := uniqueKey("deny")
	limit := 3
	window := 1 * time.Minute

	for i := 0; i < limit; i++ {
		result, err := limiter.Allow(context.Background(), key, limit, window)
		if err != nil {
			t.Fatalf("Request %d: unexpected error: %v", i+1, err)
		}
		if !result.Allowed {
			t.Errorf("Request %d: should be allowed", i+1)
		}
	}

	result, err := limiter.Allow(context.Background(), key, limit, window)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result.Allowed {
		t.Error("Should be denied when limit exceeded")
	}
	if result.Remaining != 0 {
		t.Errorf("Expected remaining 0, got %d", result.Remaining)
	}
}

func TestRedisLimiter_DifferentKeys_Independent(t *testing.T) {
	client := getRedisClient(t)
	limiter := NewRedisLimiter(client)

	keyA := uniqueKey("ind-a")
	keyB := uniqueKey("ind-b")
	limit := 3
	window := 1 * time.Minute

	// Exhaust key-a
	for i := 0; i < limit; i++ {
		limiter.Allow(context.Background(), keyA, limit, window)
	}

	// key-a should be denied
	resultA, _ := limiter.Allow(context.Background(), keyA, limit, window)
	if resultA.Allowed {
		t.Error("key-a should be denied when limit exceeded")
	}

	// key-b should still be allowed
	resultB, _ := limiter.Allow(context.Background(), keyB, limit, window)
	if !resultB.Allowed {
		t.Error("key-b should be allowed (independent of key-a)")
	}
}

func TestRedisLimiter_Concurrent(t *testing.T) {
	client := getRedisClient(t)
	limiter := NewRedisLimiter(client)

	key := uniqueKey("concurrent")
	limit := 10
	window := 1 * time.Minute
	concurrent := 20

	var allowed int64
	var denied int64
	var wg sync.WaitGroup

	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := limiter.Allow(context.Background(), key, limit, window)
			if err != nil {
				return
			}
			if result.Allowed {
				atomic.AddInt64(&allowed, 1)
			} else {
				atomic.AddInt64(&denied, 1)
			}
		}()
	}

	wg.Wait()

	if allowed != int64(limit) {
		t.Errorf("Expected %d allowed, got %d", limit, allowed)
	}
	if denied != int64(concurrent-limit) {
		t.Errorf("Expected %d denied, got %d", concurrent-limit, denied)
	}
}
