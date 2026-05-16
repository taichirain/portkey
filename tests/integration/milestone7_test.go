//go:build !integration
// +build !integration

package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/data/proxy"
	"github.com/taichirain/portkey/internal/data/snapshot"
	"github.com/taichirain/portkey/internal/domain/credential"
	"github.com/taichirain/portkey/internal/domain/plugin"
	"github.com/taichirain/portkey/internal/domain/route"
	"github.com/taichirain/portkey/internal/domain/service"
	"go.uber.org/zap"
)

func newTestProxyWithRatelimit(t *testing.T, snap *snapshot.ConfigSnapshot) *proxy.Proxy {
	t.Helper()
	logger, _ := zap.NewDevelopment()
	p := proxy.NewProxy(logger)
	if snap != nil {
		p.UpdateSnapshot(snap)
	}
	return p
}

// === Local Rate Limit Tests ===

func TestM7_LocalRateLimit_RouteScoped_Success(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer backend.Close()

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
		"limit_by": "route",
		"limit":    5,
		"window":   "1m",
		"policy":   "local",
	})
	snap.AddPlugin(rateLimitPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithRatelimit(t, snap)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test/hello", nil)
		w := httptest.NewRecorder()
		p.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d: expected 200, got %d", i+1, w.Code)
		}

		limitHeader := w.Header().Get("X-RateLimit-Limit")
		if limitHeader != "5" {
			t.Errorf("Expected X-RateLimit-Limit: 5, got: %s", limitHeader)
		}
	}
}

func TestM7_LocalRateLimit_RouteScoped_ExceedLimit_429(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

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
		"limit_by": "route",
		"limit":    3,
		"window":   "1m",
		"policy":   "local",
	})
	snap.AddPlugin(rateLimitPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithRatelimit(t, snap)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/test/hello", nil)
		w := httptest.NewRecorder()
		p.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Expected 429, got %d", w.Code)
	}

	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	if !strings.Contains(body["message"].(string), "Rate limit") {
		t.Errorf("Expected 'Rate limit' message, got: %v", body["message"])
	}

	retryAfter := w.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Error("Expected Retry-After header")
	}
}

func TestM7_LocalRateLimit_IPScoped(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

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
		"limit_by": "ip",
		"limit":    2,
		"window":   "1m",
		"policy":   "local",
	})
	snap.AddPlugin(rateLimitPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithRatelimit(t, snap)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test/hello", nil)
		req.Header.Set("X-Forwarded-For", "192.168.1.100")
		w := httptest.NewRecorder()
		p.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	req1 := httptest.NewRequest("GET", "/test/hello", nil)
	req1.Header.Set("X-Forwarded-For", "192.168.1.100")
	w1 := httptest.NewRecorder()
	p.ServeHTTP(w1, req1)

	if w1.Code != http.StatusTooManyRequests {
		t.Errorf("Expected 429 for same IP, got %d", w1.Code)
	}

	req2 := httptest.NewRequest("GET", "/test/hello", nil)
	req2.Header.Set("X-Forwarded-For", "192.168.1.200")
	w2 := httptest.NewRecorder()
	p.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("Expected 200 for different IP, got %d", w2.Code)
	}
}

func TestM7_LocalRateLimit_ConsumerScoped(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

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

	consumerID := uuid.New()
	cred := &credential.Credential{
		ID: uuid.New(), ConsumerID: consumerID,
		Type: credential.TypeKeyAuth, Key: "consumer-key", Enabled: true,
	}
	snap.AddCredential(cred)

	consumer2ID := uuid.New()
	cred2 := &credential.Credential{
		ID: uuid.New(), ConsumerID: consumer2ID,
		Type: credential.TypeKeyAuth, Key: "consumer2-key", Enabled: true,
	}
	snap.AddCredential(cred2)

	keyAuthPlugin, _ := plugin.New("key-auth", map[string]interface{}{})
	snap.AddPlugin(keyAuthPlugin)

	rateLimitPlugin, _ := plugin.New("rate-limit", map[string]interface{}{
		"limit_by": "consumer",
		"limit":    2,
		"window":   "1m",
		"policy":   "local",
	})
	snap.AddPlugin(rateLimitPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithRatelimit(t, snap)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test/hello", nil)
		req.Header.Set("apikey", "consumer-key")
		w := httptest.NewRecorder()
		p.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Consumer 1 Request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	req1 := httptest.NewRequest("GET", "/test/hello", nil)
	req1.Header.Set("apikey", "consumer-key")
	w1 := httptest.NewRecorder()
	p.ServeHTTP(w1, req1)

	if w1.Code != http.StatusTooManyRequests {
		t.Errorf("Expected 429 for consumer 1, got %d", w1.Code)
	}

	req2 := httptest.NewRequest("GET", "/test/hello", nil)
	req2.Header.Set("apikey", "consumer2-key")
	w2 := httptest.NewRecorder()
	p.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("Expected 200 for consumer 2, got %d", w2.Code)
	}
}

func TestM7_RateLimit_Headers(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

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
		"limit_by":        "route",
		"limit":           10,
		"window":          "60s",
		"policy":          "local",
		"include_headers": true,
	})
	snap.AddPlugin(rateLimitPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithRatelimit(t, snap)

	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	limitHeader := w.Header().Get("X-RateLimit-Limit")
	if limitHeader != "10" {
		t.Errorf("Expected X-RateLimit-Limit: 10, got: %s", limitHeader)
	}

	remainingHeader := w.Header().Get("X-RateLimit-Remaining")
	remaining, _ := strconv.Atoi(remainingHeader)
	if remaining != 9 {
		t.Errorf("Expected X-RateLimit-Remaining: 9, got: %s", remainingHeader)
	}

	resetHeader := w.Header().Get("X-RateLimit-Reset")
	if resetHeader == "" {
		t.Error("Expected X-RateLimit-Reset header")
	}

	policyHeader := w.Header().Get("X-RateLimit-Policy")
	if policyHeader == "" {
		t.Error("Expected X-RateLimit-Policy header")
	}
}

func TestM7_LocalRateLimit_Concurrent(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	limit := 10
	concurrent := 20

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
		"limit_by": "route",
		"limit":    limit,
		"window":   "1m",
		"policy":   "local",
	})
	snap.AddPlugin(rateLimitPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithRatelimit(t, snap)

	var successCount int64
	var blockedCount int64
	var wg sync.WaitGroup

	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/test/hello", nil)
			w := httptest.NewRecorder()
			p.ServeHTTP(w, req)

			if w.Code == http.StatusOK {
				atomic.AddInt64(&successCount, 1)
			} else if w.Code == http.StatusTooManyRequests {
				atomic.AddInt64(&blockedCount, 1)
			}
		}()
	}

	wg.Wait()

	if successCount != int64(limit) {
		t.Errorf("Expected %d successful requests, got %d", limit, successCount)
	}

	expectedBlocked := int64(concurrent - limit)
	if blockedCount != expectedBlocked {
		t.Errorf("Expected %d blocked requests, got %d", expectedBlocked, blockedCount)
	}
}

func TestM7_RateLimit_RouteScoped_Plugin(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("test-svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, backend.Listener.Addr().String())
	snap.AddService(svc)

	protectedRoute, _ := route.New(svc.ID)
	protectedRoute.AddPath("/protected")
	protectedRoute.AddMethod("GET")
	snap.AddRoute(protectedRoute)

	publicRoute, _ := route.New(svc.ID)
	publicRoute.AddPath("/public")
	publicRoute.AddMethod("GET")
	snap.AddRoute(publicRoute)

	rateLimitPlugin, _ := plugin.New("rate-limit", map[string]interface{}{
		"limit_by": "route",
		"limit":    2,
		"window":   "1m",
		"policy":   "local",
	})
	rateLimitPlugin.RouteID = &protectedRoute.ID
	snap.AddPlugin(rateLimitPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithRatelimit(t, snap)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/public/hello", nil)
		w := httptest.NewRecorder()
		p.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Public route request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/protected/hello", nil)
		w := httptest.NewRecorder()
		p.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Protected route request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	req := httptest.NewRequest("GET", "/protected/hello", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Expected 429 for protected route, got %d", w.Code)
	}
}

func TestM7_RateLimit_WindowReset(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

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
		"limit_by": "route",
		"limit":    2,
		"window":   "1s",
		"policy":   "local",
	})
	snap.AddPlugin(rateLimitPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithRatelimit(t, snap)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test/hello", nil)
		w := httptest.NewRecorder()
		p.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	req1 := httptest.NewRequest("GET", "/test/hello", nil)
	w1 := httptest.NewRecorder()
	p.ServeHTTP(w1, req1)

	if w1.Code != http.StatusTooManyRequests {
		t.Errorf("Expected 429 before window reset, got %d", w1.Code)
	}

	time.Sleep(1100 * time.Millisecond)

	req2 := httptest.NewRequest("GET", "/test/hello", nil)
	w2 := httptest.NewRecorder()
	p.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("Expected 200 after window reset, got %d", w2.Code)
	}
}
