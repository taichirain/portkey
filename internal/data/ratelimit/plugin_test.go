package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	pluginPkg "github.com/taichirain/portkey/internal/data/plugin"
)

func TestRateLimitPlugin_ConfigPassing(t *testing.T) {
	t.Run("config with limit 5 should be parsed correctly", func(t *testing.T) {
		factory := NewRateLimitFactory()

		config := map[string]interface{}{
			"limit_by": "route",
			"limit":    5,
			"window":   "1m",
			"policy":   "local",
		}

		pluginInstance, err := factory.Create(config)
		if err != nil {
			t.Fatalf("Failed to create plugin: %v", err)
		}

		rateLimitPlugin, ok := pluginInstance.(*RateLimitPlugin)
		if !ok {
			t.Fatal("Plugin is not *RateLimitPlugin")
		}

		if rateLimitPlugin.config.Limit != 5 {
			t.Errorf("Expected limit 5, got %d", rateLimitPlugin.config.Limit)
		}

		if rateLimitPlugin.config.LimitBy != LimitByRoute {
			t.Errorf("Expected LimitByRoute, got %v", rateLimitPlugin.config.LimitBy)
		}

		if rateLimitPlugin.config.Window.Duration() != 60*time.Second {
			t.Errorf("Expected window 60s, got %v", rateLimitPlugin.config.Window.Duration())
		}
	})

	t.Run("config with float64 limit should be parsed correctly", func(t *testing.T) {
		factory := NewRateLimitFactory()

		config := map[string]interface{}{
			"limit_by": "ip",
			"limit":    float64(10),
			"window":   "30s",
			"policy":   "redis",
		}

		pluginInstance, err := factory.Create(config)
		if err != nil {
			t.Fatalf("Failed to create plugin: %v", err)
		}

		rateLimitPlugin, ok := pluginInstance.(*RateLimitPlugin)
		if !ok {
			t.Fatal("Plugin is not *RateLimitPlugin")
		}

		if rateLimitPlugin.config.Limit != 10 {
			t.Errorf("Expected limit 10, got %d", rateLimitPlugin.config.Limit)
		}

		if rateLimitPlugin.config.LimitBy != LimitByIP {
			t.Errorf("Expected LimitByIP, got %v", rateLimitPlugin.config.LimitBy)
		}

		if rateLimitPlugin.config.Window.Duration() != 30*time.Second {
			t.Errorf("Expected window 30s, got %v", rateLimitPlugin.config.Window.Duration())
		}

		if rateLimitPlugin.config.Policy != PolicyRedis {
			t.Errorf("Expected PolicyRedis, got %v", rateLimitPlugin.config.Policy)
		}
	})
}

func TestRateLimitPlugin_OnRequest(t *testing.T) {
	t.Run("should allow requests within limit", func(t *testing.T) {
		factory := NewRateLimitFactory()

		config := map[string]interface{}{
			"limit_by": "route",
			"limit":    3,
			"window":   "1m",
			"policy":   "local",
		}

		pluginInstance, err := factory.Create(config)
		if err != nil {
			t.Fatalf("Failed to create plugin: %v", err)
		}

		routeID := uuid.New()

		for i := 0; i < 3; i++ {
			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()

			ctx := pluginPkg.NewPluginContext(w, req, "trace-id")
			ctx.SetMatchedRoute(routeID, uuid.New(), uuid.New())

			err := pluginInstance.OnRequest(ctx)

			if err != nil {
				t.Errorf("Request %d: unexpected error: %v", i+1, err)
			}

			if ctx.IsShortCircuited() {
				t.Errorf("Request %d: should not be short-circuited", i+1)
			}

			limitHeader := w.Header().Get("X-RateLimit-Limit")
			if limitHeader != "3" {
				t.Errorf("Request %d: expected X-RateLimit-Limit: 3, got: %s", i+1, limitHeader)
			}
		}
	})

	t.Run("should block requests exceeding limit", func(t *testing.T) {
		factory := NewRateLimitFactory()

		config := map[string]interface{}{
			"limit_by": "route",
			"limit":    2,
			"window":   "1m",
			"policy":   "local",
		}

		pluginInstance, err := factory.Create(config)
		if err != nil {
			t.Fatalf("Failed to create plugin: %v", err)
		}

		routeID := uuid.New()

		for i := 0; i < 2; i++ {
			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()

			ctx := pluginPkg.NewPluginContext(w, req, "trace-id")
			ctx.SetMatchedRoute(routeID, uuid.New(), uuid.New())

			err := pluginInstance.OnRequest(ctx)

			if err != nil {
				t.Errorf("Request %d: unexpected error: %v", i+1, err)
			}
		}

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		ctx := pluginPkg.NewPluginContext(w, req, "trace-id")
		ctx.SetMatchedRoute(routeID, uuid.New(), uuid.New())

		err = pluginInstance.OnRequest(ctx)

		if err == nil {
			t.Error("Expected error for rate limit exceeded")
		}

		if !ctx.IsShortCircuited() {
			t.Error("Request should be short-circuited")
		}

		if w.Code != http.StatusTooManyRequests {
			t.Errorf("Expected 429, got %d", w.Code)
		}

		retryAfter := w.Header().Get("Retry-After")
		if retryAfter == "" {
			t.Error("Expected Retry-After header")
		}
	})

	t.Run("different routes should have independent limits", func(t *testing.T) {
		factory := NewRateLimitFactory()

		config := map[string]interface{}{
			"limit_by": "route",
			"limit":    2,
			"window":   "1m",
			"policy":   "local",
		}

		pluginInstance, err := factory.Create(config)
		if err != nil {
			t.Fatalf("Failed to create plugin: %v", err)
		}

		routeID1 := uuid.New()
		routeID2 := uuid.New()

		for i := 0; i < 2; i++ {
			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()

			ctx := pluginPkg.NewPluginContext(w, req, "trace-id")
			ctx.SetMatchedRoute(routeID1, uuid.New(), uuid.New())

			pluginInstance.OnRequest(ctx)

			if ctx.IsShortCircuited() {
				t.Errorf("Route 1 Request %d: should not be short-circuited", i+1)
			}
		}

		req1 := httptest.NewRequest("GET", "/test", nil)
		w1 := httptest.NewRecorder()
		ctx1 := pluginPkg.NewPluginContext(w1, req1, "trace-id")
		ctx1.SetMatchedRoute(routeID1, uuid.New(), uuid.New())
		pluginInstance.OnRequest(ctx1)

		if !ctx1.IsShortCircuited() {
			t.Error("Route 1 should be blocked")
		}

		for i := 0; i < 2; i++ {
			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()

			ctx := pluginPkg.NewPluginContext(w, req, "trace-id")
			ctx.SetMatchedRoute(routeID2, uuid.New(), uuid.New())

			pluginInstance.OnRequest(ctx)

			if ctx.IsShortCircuited() {
				t.Errorf("Route 2 Request %d: should not be short-circuited", i+1)
			}
		}
	})
}

func TestRateLimitPlugin_LimitByIP(t *testing.T) {
	factory := NewRateLimitFactory()

	config := map[string]interface{}{
		"limit_by": "ip",
		"limit":    2,
		"window":   "1m",
		"policy":   "local",
	}

	pluginInstance, err := factory.Create(config)
	if err != nil {
		t.Fatalf("Failed to create plugin: %v", err)
	}

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Forwarded-For", "192.168.1.100")
		w := httptest.NewRecorder()

		ctx := pluginPkg.NewPluginContext(w, req, "trace-id")
		ctx.SetMatchedRoute(uuid.New(), uuid.New(), uuid.New())

		err := pluginInstance.OnRequest(ctx)

		if err != nil {
			t.Errorf("Request %d: unexpected error: %v", i+1, err)
		}
	}

	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.Header.Set("X-Forwarded-For", "192.168.1.100")
	w1 := httptest.NewRecorder()
	ctx1 := pluginPkg.NewPluginContext(w1, req1, "trace-id")
	ctx1.SetMatchedRoute(uuid.New(), uuid.New(), uuid.New())
	pluginInstance.OnRequest(ctx1)

	if !ctx1.IsShortCircuited() {
		t.Error("Same IP should be blocked")
	}

	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.Header.Set("X-Forwarded-For", "192.168.1.200")
	w2 := httptest.NewRecorder()
	ctx2 := pluginPkg.NewPluginContext(w2, req2, "trace-id")
	ctx2.SetMatchedRoute(uuid.New(), uuid.New(), uuid.New())
	pluginInstance.OnRequest(ctx2)

	if ctx2.IsShortCircuited() {
		t.Error("Different IP should not be blocked")
	}
}

func TestRateLimitPlugin_Concurrent(t *testing.T) {
	factory := NewRateLimitFactory()

	config := map[string]interface{}{
		"limit_by": "route",
		"limit":    10,
		"window":   "1m",
		"policy":   "local",
	}

	pluginInstance, err := factory.Create(config)
	if err != nil {
		t.Fatalf("Failed to create plugin: %v", err)
	}

	routeID := uuid.New()
	concurrent := 20

	var allowedCount int32
	var blockedCount int32
	var wg sync.WaitGroup

	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()

			ctx := pluginPkg.NewPluginContext(w, req, "trace-id")
			ctx.SetMatchedRoute(routeID, uuid.New(), uuid.New())

			err := pluginInstance.OnRequest(ctx)

			if err == nil && !ctx.IsShortCircuited() {
				atomic.AddInt32(&allowedCount, 1)
			} else {
				atomic.AddInt32(&blockedCount, 1)
			}
		}()
	}

	wg.Wait()

	if allowedCount != 10 {
		t.Errorf("Expected 10 allowed requests, got %d", allowedCount)
	}

	expectedBlocked := int32(concurrent - 10)
	if blockedCount != expectedBlocked {
		t.Errorf("Expected %d blocked requests, got %d", expectedBlocked, blockedCount)
	}
}
