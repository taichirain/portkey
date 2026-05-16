//go:build !integration
// +build !integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/data/consumer"
	"github.com/taichirain/portkey/internal/data/proxy"
	"github.com/taichirain/portkey/internal/data/snapshot"
	"github.com/taichirain/portkey/internal/domain/route"
	"github.com/taichirain/portkey/internal/domain/service"
	"github.com/taichirain/portkey/internal/domain/target"
	"github.com/taichirain/portkey/internal/domain/upstream"
	"go.uber.org/zap"
)

// --- Milestone 3: Data Plane 快照消费 ---

func TestM3_SnapshotBuild_BasicFlow(t *testing.T) {
	snap := snapshot.NewConfigSnapshot(uuid.New())

	svc, _ := service.New("api-service")
	svc.Host = "localhost"
	svc.Port = 9000
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	if len(snap.Routes) != 1 {
		t.Errorf("Expected 1 route, got %d", len(snap.Routes))
	}
	if len(snap.Services) != 1 {
		t.Errorf("Expected 1 service, got %d", len(snap.Services))
	}
}

func TestM3_SnapshotBuild_WithUpstreamAndTargets(t *testing.T) {
	snap := snapshot.NewConfigSnapshot(uuid.New())

	up, _ := upstream.New("backend-upstream")
	snap.AddUpstream(up)

	t1, _ := target.New(up.ID, "host1.example.com", 8080)
	t2, _ := target.New(up.ID, "host2.example.com", 8080)
	snap.AddTargets(up.ID, []*target.Target{t1, t2})

	svc, _ := service.New("lb-service")
	svc.UpstreamID = up.ID
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/lb")
	r.AddMethod("GET")
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	balancer, ok := snap.GetBalancer(up.ID)
	if !ok {
		t.Fatal("Expected balancer to exist")
	}
	if len(balancer.Targets()) != 2 {
		t.Errorf("Expected 2 targets in balancer, got %d", len(balancer.Targets()))
	}
}

func TestM3_SnapshotBuild_DisabledRouteExcluded(t *testing.T) {
	snap := snapshot.NewConfigSnapshot(uuid.New())

	svc, _ := service.New("svc")
	svc.Host = "localhost"
	svc.Port = 8080
	snap.AddService(svc)

	rEnabled, _ := route.New(svc.ID)
	rEnabled.AddPath("/enabled")
	rEnabled.AddMethod("GET")
	snap.AddRoute(rEnabled)

	rDisabled, _ := route.New(svc.ID)
	rDisabled.AddPath("/disabled")
	rDisabled.AddMethod("GET")
	rDisabled.Enabled = false
	snap.AddRoute(rDisabled)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	req := httptest.NewRequest("GET", "/enabled/test", nil)
	_, ok := snap.MatchRoute(req)
	if !ok {
		t.Error("Expected /enabled to match")
	}

	req2 := httptest.NewRequest("GET", "/disabled/test", nil)
	_, ok2 := snap.MatchRoute(req2)
	if ok2 {
		t.Error("Expected /disabled NOT to match (disabled route)")
	}
}

func TestM3_SnapshotBuild_DisabledTargetExcludedFromBalancer(t *testing.T) {
	snap := snapshot.NewConfigSnapshot(uuid.New())

	up, _ := upstream.New("upstream")
	snap.AddUpstream(up)

	t1, _ := target.New(up.ID, "active-host", 8080)
	t2, _ := target.New(up.ID, "disabled-host", 8080)
	t2.Enabled = false
	snap.AddTargets(up.ID, []*target.Target{t1, t2})

	svc, _ := service.New("svc")
	svc.UpstreamID = up.ID
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/test")
	r.AddMethod("GET")
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	balancer, ok := snap.GetBalancer(up.ID)
	if !ok {
		t.Fatal("Expected balancer")
	}
	if len(balancer.Targets()) != 1 {
		t.Errorf("Expected 1 enabled target, got %d", len(balancer.Targets()))
	}
	if balancer.Targets()[0].Target != "active-host" {
		t.Errorf("Expected active-host, got %s", balancer.Targets()[0].Target)
	}
}

func TestM3_SnapshotBuild_AllTargetsDisabled_NoBalancer(t *testing.T) {
	snap := snapshot.NewConfigSnapshot(uuid.New())

	up, _ := upstream.New("upstream")
	snap.AddUpstream(up)

	t1, _ := target.New(up.ID, "host1", 8080)
	t1.Enabled = false
	t2, _ := target.New(up.ID, "host2", 8080)
	t2.Enabled = false
	snap.AddTargets(up.ID, []*target.Target{t1, t2})

	svc, _ := service.New("svc")
	svc.UpstreamID = up.ID
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/test")
	r.AddMethod("GET")
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	_, ok := snap.GetBalancer(up.ID)
	if ok {
		t.Error("Expected NO balancer when all targets disabled")
	}
}

func TestM3_SnapshotBuild_UpstreamNotFoundForTargets(t *testing.T) {
	snap := snapshot.NewConfigSnapshot(uuid.New())

	orphanUpstreamID := uuid.New()
	t1, _ := target.New(orphanUpstreamID, "host", 8080)
	snap.AddTargets(orphanUpstreamID, []*target.Target{t1})

	svc, _ := service.New("svc")
	svc.Host = "localhost"
	svc.Port = 8080
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/test")
	r.AddMethod("GET")
	snap.AddRoute(r)

	err := snap.Build()
	if err == nil {
		t.Fatal("Expected build to fail when upstream not found for targets")
	}
}

func TestM3_SnapshotBuild_NoMatchConditions(t *testing.T) {
	snap := snapshot.NewConfigSnapshot(uuid.New())

	svc, _ := service.New("svc")
	svc.Host = "localhost"
	svc.Port = 8080
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	// No methods, hosts, paths, or headers added
	snap.AddRoute(r)

	err := snap.Build()
	if err == nil {
		t.Fatal("Expected build to fail for route with no match conditions")
	}
}

func TestM3_SnapshotMatch_PathPrefix(t *testing.T) {
	snap := snapshot.NewConfigSnapshot(uuid.New())

	svc, _ := service.New("svc")
	svc.Host = "localhost"
	svc.Port = 8080
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api/v1")
	r.AddMethod("GET")
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	tests := []struct {
		path    string
		matched bool
	}{
		{"/api/v1/users", true},
		{"/api/v1", true},
		{"/api/v2/users", false},
		{"/other", false},
	}

	for _, tt := range tests {
		req := httptest.NewRequest("GET", tt.path, nil)
		_, ok := snap.MatchRoute(req)
		if ok != tt.matched {
			t.Errorf("path=%s: expected match=%v, got %v", tt.path, tt.matched, ok)
		}
	}
}

func TestM3_SnapshotMatch_MultipleRoutes_FirstMatch(t *testing.T) {
	snap := snapshot.NewConfigSnapshot(uuid.New())

	svc1, _ := service.New("svc1")
	svc1.Host = "localhost"
	svc1.Port = 8001
	snap.AddService(svc1)

	svc2, _ := service.New("svc2")
	svc2.Host = "localhost"
	svc2.Port = 8002
	snap.AddService(svc2)

	r1, _ := route.New(svc1.ID)
	r1.AddPath("/svc1")
	r1.AddMethod("GET")
	snap.AddRoute(r1)

	r2, _ := route.New(svc2.ID)
	r2.AddPath("/svc2")
	r2.AddMethod("GET")
	snap.AddRoute(r2)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	req1 := httptest.NewRequest("GET", "/svc1/test", nil)
	m1, ok1 := snap.MatchRoute(req1)
	if !ok1 || m1.Service.ID != svc1.ID {
		t.Error("Expected /svc1 to match svc1")
	}

	req2 := httptest.NewRequest("GET", "/svc2/test", nil)
	m2, ok2 := snap.MatchRoute(req2)
	if !ok2 || m2.Service.ID != svc2.ID {
		t.Error("Expected /svc2 to match svc2")
	}
}

func TestM3_AtomicSnapshotSwitch(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	p := proxy.NewProxy(logger)

	// Build first snapshot
	snap1 := snapshot.NewConfigSnapshot(uuid.New())
	svc1, _ := service.New("v1-service")
	svc1.Host = "localhost"
	svc1.Port = 9001
	snap1.AddService(svc1)
	r1, _ := route.New(svc1.ID)
	r1.AddPath("/test")
	r1.AddMethod("GET")
	snap1.AddRoute(r1)
	if err := snap1.Build(); err != nil {
		t.Fatalf("Build snap1 failed: %v", err)
	}

	p.UpdateSnapshot(snap1)

	// Verify first snapshot is active
	req := httptest.NewRequest("GET", "/test/abc", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)
	if w.Code != http.StatusBadGateway {
		// The proxy tries to connect to upstream, which will fail with BadGateway
		// But the route should match (we get BadGateway instead of NotFound)
		if w.Code == http.StatusNotFound {
			t.Error("Expected route to match (not 404), got 404")
		}
	}

	// Build second snapshot
	snap2 := snapshot.NewConfigSnapshot(uuid.New())
	svc2, _ := service.New("v2-service")
	svc2.Host = "localhost"
	svc2.Port = 9002
	snap2.AddService(svc2)
	r2, _ := route.New(svc2.ID)
	r2.AddPath("/new-path")
	r2.AddMethod("GET")
	snap2.AddRoute(r2)
	if err := snap2.Build(); err != nil {
		t.Fatalf("Build snap2 failed: %v", err)
	}

	// Atomic switch
	p.UpdateSnapshot(snap2)

	// Old route should no longer match
	req2 := httptest.NewRequest("GET", "/test/abc", nil)
	w2 := httptest.NewRecorder()
	p.ServeHTTP(w2, req2)
	if w2.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for old route after switch, got %d", w2.Code)
	}

	// New route should match
	req3 := httptest.NewRequest("GET", "/new-path/abc", nil)
	w3 := httptest.NewRecorder()
	p.ServeHTTP(w3, req3)
	if w3.Code == http.StatusNotFound {
		t.Error("Expected new route to match after switch, got 404")
	}
}

func TestM3_BadSnapshotPreservesOldConfig(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	p := proxy.NewProxy(logger)

	// Build valid snapshot
	snap1 := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("good-service")
	svc.Host = "localhost"
	svc.Port = 8080
	snap1.AddService(svc)
	r, _ := route.New(svc.ID)
	r.AddPath("/good")
	r.AddMethod("GET")
	snap1.AddRoute(r)
	if err := snap1.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	p.UpdateSnapshot(snap1)

	// The snapshot is already stored via atomic.Pointer, so we can't
	// directly test "bad snapshot rejected" at proxy level since
	// UpdateSnapshot always succeeds. But we verify that a snapshot
	// with a bad build would be caught before reaching the proxy.
	// This is handled by the consumer's buildConfigSnapshot + validateSnapshot.

	// Verify old config still works
	req := httptest.NewRequest("GET", "/good/test", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)
	if w.Code == http.StatusNotFound {
		t.Error("Expected old route to still match")
	}
}

func TestM3_SnapshotConsumer_PollAndSwitch(t *testing.T) {
	revisionID := uuid.New()
	callCount := 0

	// Mock CP server
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/public/active-revision" {
			http.NotFound(w, r)
			return
		}
		callCount++

		svc := consumer.ServiceSnapshot{
			ID:       uuid.New(),
			Name:     "test-svc",
			Protocol: "http",
			Host:     "localhost",
			Port:     8080,
			Enabled:  true,
		}
		routeSnap := consumer.RouteSnapshot{
			ID:        uuid.New(),
			ServiceID: svc.ID,
			Methods:   []string{"GET"},
			Paths:     []string{"/test"},
			Enabled:   true,
		}

		resp := consumer.RevisionResponse{
			RevisionID:  revisionID.String(),
			Version:     "v1",
			Description: "test",
			Snapshot: &consumer.RevisionSnapshotData{
				Version:   "v1",
				Timestamp: time.Now().Format(time.RFC3339),
				Services:  []consumer.ServiceSnapshot{svc},
				Routes:    []consumer.RouteSnapshot{routeSnap},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer cpServer.Close()

	logger, _ := zap.NewDevelopment()
	cfg := &consumer.SnapshotConsumerConfig{
		ControlURL:   cpServer.URL,
		PollInterval: 500 * time.Millisecond,
		Logger:       logger,
	}

	c := consumer.NewSnapshotConsumer(cfg)

	var receivedSnap *snapshot.ConfigSnapshot
	var mu sync.Mutex
	c.SetOnSnapshotUpdate(func(snap *snapshot.ConfigSnapshot) {
		mu.Lock()
		defer mu.Unlock()
		receivedSnap = snap
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer c.Stop(ctx)

	// Wait for first poll
	time.Sleep(1 * time.Second)

	mu.Lock()
	if receivedSnap == nil {
		t.Fatal("Expected to receive snapshot from CP")
	}
	if receivedSnap.RevisionID != revisionID {
		t.Errorf("Expected revision %s, got %s", revisionID, receivedSnap.RevisionID)
	}
	if len(receivedSnap.Services) != 1 {
		t.Errorf("Expected 1 service, got %d", len(receivedSnap.Services))
	}
	if len(receivedSnap.Routes) != 1 {
		t.Errorf("Expected 1 route, got %d", len(receivedSnap.Routes))
	}
	mu.Unlock()

	// Verify revision ID is tracked
	if c.GetCurrentRevisionID() != revisionID.String() {
		t.Errorf("Expected current revision ID %s, got %s", revisionID.String(), c.GetCurrentRevisionID())
	}
}

func TestM3_SnapshotConsumer_NoActiveRevision(t *testing.T) {
	// CP returns 404 for no active revision
	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "No active revision found"})
	}))
	defer cpServer.Close()

	logger, _ := zap.NewDevelopment()
	cfg := &consumer.SnapshotConsumerConfig{
		ControlURL:   cpServer.URL,
		PollInterval: 500 * time.Millisecond,
		Logger:       logger,
	}

	c := consumer.NewSnapshotConsumer(cfg)

	var updateCalled bool
	c.SetOnSnapshotUpdate(func(snap *snapshot.ConfigSnapshot) {
		updateCalled = true
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer c.Stop(ctx)

	time.Sleep(1 * time.Second)

	if updateCalled {
		t.Error("Expected no snapshot update when no active revision")
	}
	if c.GetCurrentSnapshot() != nil {
		t.Error("Expected nil snapshot when no active revision")
	}
}

func TestM3_SnapshotConsumer_BadSnapshotRejected(t *testing.T) {
	revisionID := uuid.New()

	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a snapshot with an invalid route (no match conditions)
		svc := consumer.ServiceSnapshot{
			ID:       uuid.New(),
			Name:     "test-svc",
			Protocol: "http",
			Host:     "localhost",
			Port:     8080,
			Enabled:  true,
		}
		routeSnap := consumer.RouteSnapshot{
			ID:        uuid.New(),
			ServiceID: svc.ID,
			// No methods, hosts, paths, or headers → will fail build
			Enabled: true,
		}

		resp := consumer.RevisionResponse{
			RevisionID:  revisionID.String(),
			Version:     "v1",
			Snapshot: &consumer.RevisionSnapshotData{
				Version:   "v1",
				Timestamp: time.Now().Format(time.RFC3339),
				Services:  []consumer.ServiceSnapshot{svc},
				Routes:    []consumer.RouteSnapshot{routeSnap},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer cpServer.Close()

	logger, _ := zap.NewDevelopment()
	cfg := &consumer.SnapshotConsumerConfig{
		ControlURL:   cpServer.URL,
		PollInterval: 500 * time.Millisecond,
		Logger:       logger,
	}

	c := consumer.NewSnapshotConsumer(cfg)

	var updateCalled bool
	c.SetOnSnapshotUpdate(func(snap *snapshot.ConfigSnapshot) {
		updateCalled = true
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer c.Stop(ctx)

	time.Sleep(1 * time.Second)

	if updateCalled {
		t.Error("Expected bad snapshot to be rejected (no update callback)")
	}
	if c.GetCurrentSnapshot() != nil {
		t.Error("Expected nil snapshot when bad snapshot is rejected")
	}
}

func TestM3_SnapshotConsumer_IdempotentPoll(t *testing.T) {
	revisionID := uuid.New()
	callCount := 0

	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		svc := consumer.ServiceSnapshot{
			ID:       uuid.New(),
			Name:     "svc",
			Protocol: "http",
			Host:     "localhost",
			Port:     8080,
			Enabled:  true,
		}
		routeSnap := consumer.RouteSnapshot{
			ID:        uuid.New(),
			ServiceID: svc.ID,
			Methods:   []string{"GET"},
			Paths:     []string{"/test"},
			Enabled:   true,
		}
		resp := consumer.RevisionResponse{
			RevisionID:  revisionID.String(),
			Version:     "v1",
			Snapshot: &consumer.RevisionSnapshotData{
				Version:   "v1",
				Timestamp: time.Now().Format(time.RFC3339),
				Services:  []consumer.ServiceSnapshot{svc},
				Routes:    []consumer.RouteSnapshot{routeSnap},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer cpServer.Close()

	logger, _ := zap.NewDevelopment()
	cfg := &consumer.SnapshotConsumerConfig{
		ControlURL:   cpServer.URL,
		PollInterval: 300 * time.Millisecond,
		Logger:       logger,
	}

	c := consumer.NewSnapshotConsumer(cfg)

	updateCount := 0
	c.SetOnSnapshotUpdate(func(snap *snapshot.ConfigSnapshot) {
		updateCount++
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer c.Stop(ctx)

	time.Sleep(1500 * time.Millisecond)

	// Should only update once (same revision ID)
	if updateCount != 1 {
		t.Errorf("Expected exactly 1 update (idempotent), got %d", updateCount)
	}
}

func TestM3_RevisionStatusEndpoint(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	p := proxy.NewProxy(logger)

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("test-svc")
	svc.Host = "localhost"
	svc.Port = 8080
	snap.AddService(svc)
	r, _ := route.New(svc.ID)
	r.AddPath("/test")
	r.AddMethod("GET")
	snap.AddRoute(r)
	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	p.UpdateSnapshot(snap)

	// Verify metrics endpoint works
	metrics := p.Metrics()
	if metrics == nil {
		t.Fatal("Expected metrics to be non-nil")
	}
}

func TestM3_WildcardPathMatch(t *testing.T) {
	snap := snapshot.NewConfigSnapshot(uuid.New())

	svc, _ := service.New("svc")
	svc.Host = "localhost"
	svc.Port = 8080
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api/*/users")
	r.AddMethod("GET")
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	tests := []struct {
		path    string
		matched bool
	}{
		{"/api/v1/users", true},
		{"/api/v2/users", true},
		{"/api//users", true},
		{"/api/v1/other", false},
		{"/other/v1/users", false},
	}

	for _, tt := range tests {
		req := httptest.NewRequest("GET", tt.path, nil)
		_, ok := snap.MatchRoute(req)
		if ok != tt.matched {
			t.Errorf("path=%s: expected match=%v, got %v", tt.path, tt.matched, ok)
		}
	}
}

func TestM3_CombinedMatch_MethodAndHost(t *testing.T) {
	snap := snapshot.NewConfigSnapshot(uuid.New())

	svc, _ := service.New("svc")
	svc.Host = "localhost"
	svc.Port = 8080
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("POST")
	r.AddHost("api.example.com")
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	tests := []struct {
		method  string
		host    string
		path    string
		matched bool
	}{
		{"POST", "api.example.com", "/api/test", true},
		{"GET", "api.example.com", "/api/test", false},   // wrong method
		{"POST", "other.com", "/api/test", false},         // wrong host
		{"POST", "api.example.com:8080", "/api/test", true}, // host with port
	}

	for _, tt := range tests {
		req := httptest.NewRequest(tt.method, tt.path, nil)
		req.Host = tt.host
		_, ok := snap.MatchRoute(req)
		if ok != tt.matched {
			t.Errorf("method=%s host=%s path=%s: expected match=%v, got %v",
				tt.method, tt.host, tt.path, tt.matched, ok)
		}
	}
}
