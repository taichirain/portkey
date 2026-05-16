//go:build !integration
// +build !integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	pluginPkg "github.com/taichirain/portkey/internal/data/plugin"
	"github.com/taichirain/portkey/internal/data/proxy"
	"github.com/taichirain/portkey/internal/data/ratelimit"
	"github.com/taichirain/portkey/internal/data/snapshot"
	"github.com/taichirain/portkey/internal/domain/plugin"
	"github.com/taichirain/portkey/internal/domain/route"
	"github.com/taichirain/portkey/internal/domain/service"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// redisRateLimitFactory creates RateLimitPlugin instances with a Redis client pre-injected.
// This is needed because the proxy creates new plugin instances per-request via the factory.
type redisRateLimitFactory struct {
	inner  pluginPkg.PluginFactory
	client *redis.Client
}

func (f *redisRateLimitFactory) Name() string {
	return f.inner.Name()
}

func (f *redisRateLimitFactory) Create(config map[string]interface{}) (pluginPkg.Plugin, error) {
	p, err := f.inner.Create(config)
	if err != nil {
		return nil, err
	}
	if rl, ok := p.(interface{ SetRedisClient(*redis.Client) }); ok {
		rl.SetRedisClient(f.client)
	}
	return p, nil
}

// getTestRedisClient returns a Redis client or skips the test if Redis is unavailable.
func getTestRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available, skipping test: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

// flushTestKeys removes test keys from Redis after each test.
func flushTestKeys(t *testing.T, client *redis.Client, prefix string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		iter := client.Scan(ctx, 0, prefix+"*", 100).Iterator()
		for iter.Next(ctx) {
			client.Del(ctx, iter.Val())
		}
	})
}

// setupRedisDP creates DP proxies sharing the same Redis client, simulating multiple data planes.
func setupRedisDP(t *testing.T, limit int, window string, limitBy string) (
	func() *proxy.Proxy, *redis.Client,
) {
	t.Helper()
	client := getTestRedisClient(t)

	// Use unique key prefix per test to avoid cross-test interference
	testPrefix := uuid.New().String()[:8]
	flushTestKeys(t, client, "ratelimit:"+testPrefix)

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	t.Cleanup(backend.Close)

	newDP := func() *proxy.Proxy {
		snap := snapshot.NewConfigSnapshot(uuid.New())
		svc, _ := service.New("test-svc")
		svc.Protocol = service.ProtocolHTTP
		svc.Host = "127.0.0.1"
		svc.Port = parsePort(t, backend.Listener.Addr().String())
		snap.AddService(svc)

		r, _ := route.New(svc.ID)
		r.AddPath("/test")
		r.AddMethod("GET")
		snap.AddRoute(r)

		rateLimitPlugin, _ := plugin.New("rate-limit", map[string]interface{}{
			"limit_by": limitBy,
			"limit":    limit,
			"window":   window,
			"policy":   "redis",
		})
		snap.AddPlugin(rateLimitPlugin)

		if err := snap.Build(); err != nil {
			t.Fatalf("Build failed: %v", err)
		}

		logger, _ := zap.NewDevelopment()
		p := proxy.NewProxy(logger)

		// Override the rate-limit factory to inject Redis client into each new instance
		registry := p.PluginRegistry()
		originalFactory, _ := registry.Get("rate-limit")
		registry.Register(&redisRateLimitFactory{
			inner:  originalFactory,
			client: client,
		})

		p.UpdateSnapshot(snap)
		return p
	}

	return newDP, client
}

// TestRedisRateLimit_RouteScoped_SingleDP verifies basic Redis rate limiting with one DP.
func TestRedisRateLimit_RouteScoped_SingleDP(t *testing.T) {
	newDP, _ := setupRedisDP(t, 5, "1m", "route")
	dp := newDP()

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test/hello", nil)
		w := httptest.NewRecorder()
		dp.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("Request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	dp.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Expected 429, got %d", w.Code)
	}
}

// TestRedisRateLimit_MultiDP_SharedLimit is the core multi-DP test.
// Two independent DP instances share the same Redis. The combined request count
// should enforce a single global limit across both DPs.
func TestRedisRateLimit_MultiDP_SharedLimit(t *testing.T) {
	newDP, _ := setupRedisDP(t, 10, "1m", "route")
	dp1 := newDP()
	dp2 := newDP()

	// Send 6 requests through DP1
	for i := 0; i < 6; i++ {
		req := httptest.NewRequest("GET", "/test/hello", nil)
		w := httptest.NewRecorder()
		dp1.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("DP1 request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	// Send 4 requests through DP2 (should hit the shared limit of 10)
	for i := 0; i < 4; i++ {
		req := httptest.NewRequest("GET", "/test/hello", nil)
		w := httptest.NewRecorder()
		dp2.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("DP2 request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	// 11th request through DP1 should be blocked (shared limit reached)
	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	dp1.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("DP1 11th request: expected 429, got %d", w.Code)
	}

	// 11th request through DP2 should also be blocked
	req2 := httptest.NewRequest("GET", "/test/hello", nil)
	w2 := httptest.NewRecorder()
	dp2.ServeHTTP(w2, req2)
	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("DP2 11th request: expected 429, got %d", w2.Code)
	}
}

// TestRedisRateLimit_MultiDP_Concurrent verifies that concurrent requests across
// multiple DPs are correctly limited by the shared Redis counter.
func TestRedisRateLimit_MultiDP_Concurrent(t *testing.T) {
	limit := 10
	newDP, _ := setupRedisDP(t, limit, "1m", "route")
	dp1 := newDP()
	dp2 := newDP()

	concurrent := 30
	var successCount int64
	var blockedCount int64
	var wg sync.WaitGroup

	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		dp := dp1
		if i%2 == 1 {
			dp = dp2
		}
		go func(p *proxy.Proxy) {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/test/hello", nil)
			w := httptest.NewRecorder()
			p.ServeHTTP(w, req)
			if w.Code == http.StatusOK {
				atomic.AddInt64(&successCount, 1)
			} else if w.Code == http.StatusTooManyRequests {
				atomic.AddInt64(&blockedCount, 1)
			}
		}(dp)
	}

	wg.Wait()

	if successCount != int64(limit) {
		t.Errorf("Expected %d successful requests across both DPs, got %d", limit, successCount)
	}
	expectedBlocked := int64(concurrent - limit)
	if blockedCount != expectedBlocked {
		t.Errorf("Expected %d blocked requests, got %d", expectedBlocked, blockedCount)
	}
}

// TestRedisRateLimit_MultiDP_WindowReset verifies that the shared window resets
// correctly across DPs.
func TestRedisRateLimit_MultiDP_WindowReset(t *testing.T) {
	newDP, _ := setupRedisDP(t, 3, "2s", "route")
	dp1 := newDP()
	dp2 := newDP()

	// Exhaust the limit across two DPs
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test/hello", nil)
		w := httptest.NewRecorder()
		dp1.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("DP1 request %d: expected 200, got %d", i+1, w.Code)
		}
	}
	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	dp2.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("DP2 request: expected 200, got %d", w.Code)
	}

	// Both DPs should be blocked now
	req1 := httptest.NewRequest("GET", "/test/hello", nil)
	w1 := httptest.NewRecorder()
	dp1.ServeHTTP(w1, req1)
	if w1.Code != http.StatusTooManyRequests {
		t.Errorf("DP1 should be blocked: expected 429, got %d", w1.Code)
	}

	// Wait for window to expire
	time.Sleep(2500 * time.Millisecond)

	// Both DPs should allow requests again
	req2 := httptest.NewRequest("GET", "/test/hello", nil)
	w2 := httptest.NewRecorder()
	dp1.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("DP1 after reset: expected 200, got %d", w2.Code)
	}

	req3 := httptest.NewRequest("GET", "/test/hello", nil)
	w3 := httptest.NewRecorder()
	dp2.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Errorf("DP2 after reset: expected 200, got %d", w3.Code)
	}
}

// TestRedisRateLimit_MultiDP_IPScoped verifies IP-based rate limiting is shared
// across multiple DPs via Redis.
func TestRedisRateLimit_MultiDP_IPScoped(t *testing.T) {
	newDP, _ := setupRedisDP(t, 3, "1m", "ip")
	dp1 := newDP()
	dp2 := newDP()

	clientIP := "10.0.0.50"

	// Send 2 requests through DP1
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test/hello", nil)
		req.Header.Set("X-Forwarded-For", clientIP)
		w := httptest.NewRecorder()
		dp1.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("DP1 request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	// Send 1 request through DP2 (same IP, should hit shared limit)
	req := httptest.NewRequest("GET", "/test/hello", nil)
	req.Header.Set("X-Forwarded-For", clientIP)
	w := httptest.NewRecorder()
	dp2.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("DP2 request: expected 200, got %d", w.Code)
	}

	// 4th request from same IP via DP1 should be blocked
	req1 := httptest.NewRequest("GET", "/test/hello", nil)
	req1.Header.Set("X-Forwarded-For", clientIP)
	w1 := httptest.NewRecorder()
	dp1.ServeHTTP(w1, req1)
	if w1.Code != http.StatusTooManyRequests {
		t.Errorf("DP1 4th request: expected 429, got %d", w1.Code)
	}

	// Different IP should still be allowed
	req2 := httptest.NewRequest("GET", "/test/hello", nil)
	req2.Header.Set("X-Forwarded-For", "10.0.0.99")
	w2 := httptest.NewRecorder()
	dp2.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("Different IP: expected 200, got %d", w2.Code)
	}
}

// TestRedisRateLimit_MultiDP_ThreeDPs extends the multi-DP test to three instances.
func TestRedisRateLimit_MultiDP_ThreeDPs(t *testing.T) {
	newDP, _ := setupRedisDP(t, 6, "1m", "route")
	dp1 := newDP()
	dp2 := newDP()
	dp3 := newDP()

	dps := []*proxy.Proxy{dp1, dp2, dp3}

	// Send 2 requests through each DP (total 6, exactly at limit)
	for idx, dp := range dps {
		for i := 0; i < 2; i++ {
			req := httptest.NewRequest("GET", "/test/hello", nil)
			w := httptest.NewRecorder()
			dp.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("DP%d request %d: expected 200, got %d", idx+1, i+1, w.Code)
			}
		}
	}

	// Any additional request through any DP should be blocked
	for idx, dp := range dps {
		req := httptest.NewRequest("GET", "/test/hello", nil)
		w := httptest.NewRecorder()
		dp.ServeHTTP(w, req)
		if w.Code != http.StatusTooManyRequests {
			t.Errorf("DP%d overflow request: expected 429, got %d", idx+1, w.Code)
		}
	}
}

// TestRedisLimiter_Direct_SharedState directly tests that two RedisLimiter instances
// pointing to the same Redis share state (lower-level unit test).
func TestRedisLimiter_Direct_SharedState(t *testing.T) {
	getTestRedisClient(t) // ensure Redis is available

	// Create two separate Redis clients to simulate two different DP instances
	client1 := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	client2 := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	t.Cleanup(func() {
		client1.Close()
		client2.Close()
	})

	limiter1 := ratelimit.NewRedisLimiter(client1)
	limiter2 := ratelimit.NewRedisLimiter(client2)

	key := uuid.New().String()
	limit := 5
	window := 1 * time.Minute

	// Send 3 through limiter1
	for i := 0; i < 3; i++ {
		result, err := limiter1.Allow(context.Background(), key, limit, window)
		if err != nil {
			t.Fatalf("Limiter1 request %d: %v", i+1, err)
		}
		if !result.Allowed {
			t.Errorf("Limiter1 request %d: should be allowed", i+1)
		}
	}

	// Send 2 through limiter2 (should see the 3 from limiter1)
	for i := 0; i < 2; i++ {
		result, err := limiter2.Allow(context.Background(), key, limit, window)
		if err != nil {
			t.Fatalf("Limiter2 request %d: %v", i+1, err)
		}
		if !result.Allowed {
			t.Errorf("Limiter2 request %d: should be allowed", i+1)
		}
	}

	// 6th request through either should be denied
	result1, _ := limiter1.Allow(context.Background(), key, limit, window)
	if result1.Allowed {
		t.Error("Limiter1 6th request: should be denied (shared limit)")
	}

	result2, _ := limiter2.Allow(context.Background(), key, limit, window)
	if result2.Allowed {
		t.Error("Limiter2 6th request: should be denied (shared limit)")
	}
}

// TestRedisRateLimit_MultiDP_RemainingHeaders verifies that X-RateLimit-Remaining
// headers reflect the shared count across DPs.
func TestRedisRateLimit_MultiDP_RemainingHeaders(t *testing.T) {
	newDP, _ := setupRedisDP(t, 5, "1m", "route")
	dp1 := newDP()
	dp2 := newDP()

	// Send 3 through DP1
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/test/hello", nil)
		w := httptest.NewRecorder()
		dp1.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("DP1 request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	// Send 1 through DP2 and check remaining header
	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	dp2.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DP2 request: expected 200, got %d", w.Code)
	}

	remaining := w.Header().Get("X-RateLimit-Remaining")
	if remaining != "1" {
		t.Errorf("Expected X-RateLimit-Remaining: 1, got: %s (DP2 should see shared count)", remaining)
	}
}
