//go:build !integration
// +build !integration

package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/data/proxy"
	"github.com/taichirain/portkey/internal/data/snapshot"
	"github.com/taichirain/portkey/internal/domain/route"
	"github.com/taichirain/portkey/internal/domain/service"
	"github.com/taichirain/portkey/internal/domain/target"
	"github.com/taichirain/portkey/internal/domain/upstream"
	"go.uber.org/zap"
)

// --- Milestone 4: 核心代理链路 ---

func newTestProxy(t *testing.T, snap *snapshot.ConfigSnapshot) *proxy.Proxy {
	t.Helper()
	logger, _ := zap.NewDevelopment()
	p := proxy.NewProxy(logger)
	if snap != nil {
		p.UpdateSnapshot(snap)
	}
	return p
}

func TestM4_ProxyRouteNotFound(t *testing.T) {
	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("svc")
	svc.Host = "localhost"
	svc.Port = 8080
	snap.AddService(svc)
	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)
	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxy(t, snap)

	req := httptest.NewRequest("GET", "/not-found", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404, got %d", w.Code)
	}
}

func TestM4_ProxyNoSnapshot(t *testing.T) {
	p := newTestProxy(t, nil)

	req := httptest.NewRequest("GET", "/any", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected 503, got %d", w.Code)
	}
}

func TestM4_ProxyFullRoundTrip(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"message": "hello from upstream",
			"path":    r.URL.Path,
		})
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

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxy(t, snap)

	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if result["message"] != "hello from upstream" {
		t.Errorf("Expected 'hello from upstream', got '%s'", result["message"])
	}
}

func TestM4_ProxyRouteMatch_Path(t *testing.T) {
	var receivedPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, backend.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	r.StripPath = false
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxy(t, snap)

	req := httptest.NewRequest("GET", "/api/users", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}
	if receivedPath != "/api/users" {
		t.Errorf("Expected path /api/users, got %s", receivedPath)
	}
}

func TestM4_ProxyRouteMatch_Method(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, backend.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("POST")
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxy(t, snap)

	reqGet := httptest.NewRequest("GET", "/api/test", nil)
	wGet := httptest.NewRecorder()
	p.ServeHTTP(wGet, reqGet)
	if wGet.Code != http.StatusNotFound {
		t.Errorf("GET should not match POST-only route, got %d", wGet.Code)
	}

	reqPost := httptest.NewRequest("POST", "/api/test", nil)
	wPost := httptest.NewRecorder()
	p.ServeHTTP(wPost, reqPost)
	if wPost.Code != http.StatusOK {
		t.Errorf("POST should match, got %d", wPost.Code)
	}
}

func TestM4_ProxyRouteMatch_Host(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, backend.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	r.AddHost("api.example.com")
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxy(t, snap)

	reqWrong := httptest.NewRequest("GET", "/api/test", nil)
	reqWrong.Host = "other.com"
	wWrong := httptest.NewRecorder()
	p.ServeHTTP(wWrong, reqWrong)
	if wWrong.Code != http.StatusNotFound {
		t.Errorf("Wrong host should not match, got %d", wWrong.Code)
	}

	reqOK := httptest.NewRequest("GET", "/api/test", nil)
	reqOK.Host = "api.example.com"
	wOK := httptest.NewRecorder()
	p.ServeHTTP(wOK, reqOK)
	if wOK.Code != http.StatusOK {
		t.Errorf("Correct host should match, got %d", wOK.Code)
	}
}

func TestM4_ProxyRouteMatch_Header(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, backend.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	r.Headers = map[string][]string{
		"X-API-Version": {"v1"},
	}
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxy(t, snap)

	reqNoHeader := httptest.NewRequest("GET", "/api/test", nil)
	wNoHeader := httptest.NewRecorder()
	p.ServeHTTP(wNoHeader, reqNoHeader)
	if wNoHeader.Code != http.StatusNotFound {
		t.Errorf("Missing header should not match, got %d", wNoHeader.Code)
	}

	reqWithHeader := httptest.NewRequest("GET", "/api/test", nil)
	reqWithHeader.Header.Set("X-API-Version", "v1")
	wWithHeader := httptest.NewRecorder()
	p.ServeHTTP(wWithHeader, reqWithHeader)
	if wWithHeader.Code != http.StatusOK {
		t.Errorf("Correct header should match, got %d", wWithHeader.Code)
	}

	reqWrongHeader := httptest.NewRequest("GET", "/api/test", nil)
	reqWrongHeader.Header.Set("X-API-Version", "v2")
	wWrongHeader := httptest.NewRecorder()
	p.ServeHTTP(wWrongHeader, reqWrongHeader)
	if wWrongHeader.Code != http.StatusNotFound {
		t.Errorf("Wrong header value should not match, got %d", wWrongHeader.Code)
	}
}

func TestM4_ProxyMultipleRoutes(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, backend.Listener.Addr().String())
	snap.AddService(svc)

	r1, _ := route.New(svc.ID)
	r1.AddPath("/users")
	r1.AddMethod("GET")
	r1.Name = "users-route"
	snap.AddRoute(r1)

	r2, _ := route.New(svc.ID)
	r2.AddPath("/orders")
	r2.AddMethod("GET")
	r2.Name = "orders-route"
	snap.AddRoute(r2)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxy(t, snap)

	reqUsers := httptest.NewRequest("GET", "/users/1", nil)
	wUsers := httptest.NewRecorder()
	p.ServeHTTP(wUsers, reqUsers)
	if wUsers.Code != http.StatusOK {
		t.Errorf("/users should match, got %d", wUsers.Code)
	}

	reqOrders := httptest.NewRequest("GET", "/orders/1", nil)
	wOrders := httptest.NewRecorder()
	p.ServeHTTP(wOrders, reqOrders)
	if wOrders.Code != http.StatusOK {
		t.Errorf("/orders should match, got %d", wOrders.Code)
	}

	reqOther := httptest.NewRequest("GET", "/products/1", nil)
	wOther := httptest.NewRecorder()
	p.ServeHTTP(wOther, reqOther)
	if wOther.Code != http.StatusNotFound {
		t.Errorf("/products should not match, got %d", wOther.Code)
	}
}

func TestM4_RoundRobinDistribution(t *testing.T) {
	var count1, count2 int64

	backend1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&count1, 1)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "target1")
	}))
	defer backend1.Close()

	backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&count2, 1)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "target2")
	}))
	defer backend2.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())

	up, _ := upstream.New("lb-upstream")
	up.Algorithm = upstream.AlgorithmRoundRobin
	snap.AddUpstream(up)

	t1, _ := target.New(up.ID, "127.0.0.1", parsePort(t, backend1.Listener.Addr().String()))
	t2, _ := target.New(up.ID, "127.0.0.1", parsePort(t, backend2.Listener.Addr().String()))
	snap.AddTargets(up.ID, []*target.Target{t1, t2})

	svc, _ := service.New("lb-svc")
	svc.UpstreamID = up.ID
	svc.Protocol = service.ProtocolHTTP
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxy(t, snap)

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		w := httptest.NewRecorder()
		p.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("Request %d: expected 200, got %d", i, w.Code)
		}
	}

	c1 := atomic.LoadInt64(&count1)
	c2 := atomic.LoadInt64(&count2)

	if c1 != 5 || c2 != 5 {
		t.Errorf("Expected 5/5 distribution, got %d/%d", c1, c2)
	}
}

func TestM4_TraceID_Generated(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, backend.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxy(t, snap)

	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	traceID := w.Header().Get("X-Trace-Id")
	if traceID == "" {
		t.Error("Expected X-Trace-Id header to be set")
	}
	if len(traceID) != 36 {
		t.Errorf("Expected UUID format trace ID, got: %s", traceID)
	}
}

func TestM4_TraceID_Forwarded(t *testing.T) {
	var upstreamTraceID string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamTraceID = r.Header.Get("X-Trace-Id")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, backend.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxy(t, snap)

	customTraceID := "my-custom-trace-123"
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Trace-Id", customTraceID)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	respTraceID := w.Header().Get("X-Trace-Id")
	if respTraceID != customTraceID {
		t.Errorf("Expected response trace ID %s, got %s", customTraceID, respTraceID)
	}

	if upstreamTraceID != customTraceID {
		t.Errorf("Expected upstream trace ID %s, got %s", customTraceID, upstreamTraceID)
	}
}

func TestM4_Metrics_Counting(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, backend.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxy(t, snap)

	m0 := p.Metrics()
	if m0.RequestsTotal != 0 {
		t.Errorf("Initial requests_total should be 0, got %d", m0.RequestsTotal)
	}

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		w := httptest.NewRecorder()
		p.ServeHTTP(w, req)
	}

	m1 := p.Metrics()
	if m1.RequestsTotal != 3 {
		t.Errorf("Expected requests_total=3, got %d", m1.RequestsTotal)
	}

	req404 := httptest.NewRequest("GET", "/not-found", nil)
	w404 := httptest.NewRecorder()
	p.ServeHTTP(w404, req404)

	m2 := p.Metrics()
	if m2.RequestsTotal != 3 {
		t.Errorf("404 should not increment requests_total, expected 3, got %d", m2.RequestsTotal)
	}
	if m2.ErrorsTotal != 1 {
		t.Errorf("Expected errors_total=1, got %d", m2.ErrorsTotal)
	}

	pNoSnap := newTestProxy(t, nil)
	req503 := httptest.NewRequest("GET", "/any", nil)
	w503 := httptest.NewRecorder()
	pNoSnap.ServeHTTP(w503, req503)

	m3 := pNoSnap.Metrics()
	if m3.ErrorsTotal != 1 {
		t.Errorf("Expected errors_total=1 for 503, got %d", m3.ErrorsTotal)
	}
}

func TestM4_StripPath(t *testing.T) {
	var receivedPath string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, backend.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api/v1")
	r.AddMethod("GET")
	r.StripPath = true
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxy(t, snap)

	req := httptest.NewRequest("GET", "/api/v1/users/123", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}
	if receivedPath != "/users/123" {
		t.Errorf("Expected stripped path /users/123, got %s", receivedPath)
	}
}

func TestM4_PreserveHost(t *testing.T) {
	var receivedHost string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHost = r.Host
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, backend.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	r.PreserveHost = true
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxy(t, snap)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Host = "my-gateway.example.com"
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}
	if receivedHost != "my-gateway.example.com" {
		t.Errorf("Expected host my-gateway.example.com, got %s", receivedHost)
	}
}

func TestM4_ResponseHeaders_RouteAndServiceID(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, backend.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxy(t, snap)

	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	routeID := w.Header().Get("X-Route-ID")
	serviceID := w.Header().Get("X-Service-ID")

	if routeID != r.ID.String() {
		t.Errorf("Expected X-Route-ID=%s, got %s", r.ID.String(), routeID)
	}
	if serviceID != svc.ID.String() {
		t.Errorf("Expected X-Service-ID=%s, got %s", svc.ID.String(), serviceID)
	}
}

func TestM4_UpstreamConnectionError(t *testing.T) {
	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = 19999
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxy(t, snap)

	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("Expected 502 BadGateway for connection error, got %d", w.Code)
	}

	m := p.Metrics()
	if m.ErrorsTotal != 1 {
		t.Errorf("Expected errors_total=1, got %d", m.ErrorsTotal)
	}
}

func TestM4_AccessLog_UpstreamError(t *testing.T) {
	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = 19998
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxy(t, snap)

	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	m := p.Metrics()
	if m.ErrorsTotal != 1 {
		t.Errorf("Expected 1 error, got %d", m.ErrorsTotal)
	}
}

func TestM4_ConcurrentRequests(t *testing.T) {
	var count int64
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&count, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, backend.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxy(t, snap)

	done := make(chan bool, 50)
	for i := 0; i < 50; i++ {
		go func() {
			req := httptest.NewRequest("GET", "/api/test", nil)
			w := httptest.NewRecorder()
			p.ServeHTTP(w, req)
			done <- w.Code == http.StatusOK
		}()
	}

	successCount := 0
	for i := 0; i < 50; i++ {
		if <-done {
			successCount++
		}
	}

	if successCount != 50 {
		t.Errorf("Expected 50 successful requests, got %d", successCount)
	}

	m := p.Metrics()
	if m.RequestsTotal != 50 {
		t.Errorf("Expected requests_total=50, got %d", m.RequestsTotal)
	}

	upstreamCount := atomic.LoadInt64(&count)
	if upstreamCount != 50 {
		t.Errorf("Expected upstream to receive 50 requests, got %d", upstreamCount)
	}
}

func TestM4_Balancer_SingleTarget(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "single-target")
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())

	up, _ := upstream.New("up")
	up.Algorithm = upstream.AlgorithmRoundRobin
	snap.AddUpstream(up)

	tgt, _ := target.New(up.ID, "127.0.0.1", parsePort(t, backend.Listener.Addr().String()))
	snap.AddTargets(up.ID, []*target.Target{tgt})

	svc, _ := service.New("svc")
	svc.UpstreamID = up.ID
	svc.Protocol = service.ProtocolHTTP
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxy(t, snap)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		w := httptest.NewRecorder()
		p.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("Request %d: expected 200, got %d", i, w.Code)
		}
	}
}

func TestM4_MethodNotAllowed(t *testing.T) {
	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("svc")
	svc.Host = "localhost"
	svc.Port = 8080
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	r.AddMethod("POST")
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxy(t, snap)

	req := httptest.NewRequest("DELETE", "/api/test", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for non-matching method, got %d", w.Code)
	}
}

func TestM4_UpstreamServiceDirect(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "direct-service")
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("direct-svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, backend.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/direct")
	r.AddMethod("GET")
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxy(t, snap)

	req := httptest.NewRequest("GET", "/direct/test", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}
	body, _ := io.ReadAll(w.Body)
	if string(body) != "direct-service" {
		t.Errorf("Expected 'direct-service', got '%s'", string(body))
	}
}

func parsePort(t *testing.T, addr string) int {
	t.Helper()
	var port int
	_, err := fmt.Sscanf(addr, "127.0.0.1:%d", &port)
	if err != nil {
		_, err = fmt.Sscanf(addr, "localhost:%d", &port)
		if err != nil {
			_, err = fmt.Sscanf(addr, "[::]:%d", &port)
			if err != nil {
				t.Fatalf("Failed to parse port from addr %q: %v", addr, err)
			}
		}
	}
	return port
}
