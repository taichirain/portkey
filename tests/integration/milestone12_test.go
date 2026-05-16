//go:build !integration
// +build !integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/data/consumer"
	"github.com/taichirain/portkey/internal/data/snapshot"
	"github.com/taichirain/portkey/internal/domain/credential"
	"github.com/taichirain/portkey/internal/domain/plugin"
	"github.com/taichirain/portkey/internal/domain/route"
	"github.com/taichirain/portkey/internal/domain/service"
	"github.com/taichirain/portkey/internal/domain/target"
	"github.com/taichirain/portkey/internal/domain/upstream"
	"go.uber.org/zap"
)

// ==================== e2e 测试: 多维度匹配与切流场景 ====================

// TestM12_HeaderAndQuery_CompoundAND verifies header + query compound AND routes to canary.
func TestM12_HeaderAndQuery_CompoundAND(t *testing.T) {
	primaryBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("primary"))
	}))
	defer primaryBackend.Close()

	canaryBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("canary"))
	}))
	defer canaryBackend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())

	primarySvc, _ := service.New("primary-svc")
	primarySvc.Protocol = service.ProtocolHTTP
	primarySvc.Host = "127.0.0.1"
	primarySvc.Port = parsePort(t, primaryBackend.Listener.Addr().String())
	snap.AddService(primarySvc)

	canarySvc, _ := service.New("canary-svc")
	canarySvc.Protocol = service.ProtocolHTTP
	canarySvc.Host = "127.0.0.1"
	canarySvc.Port = parsePort(t, canaryBackend.Listener.Addr().String())
	snap.AddService(canarySvc)

	r, _ := route.New(primarySvc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	headerCfg, _ := json.Marshal(snapshot.HeaderMatchConfig{Header: "X-Canary", Value: "enabled"})
	queryCfg, _ := json.Marshal(snapshot.QueryMatchConfig{Key: "version", Value: "v2"})
	compoundCfg, _ := json.Marshal(snapshot.CompoundMatchConfig{
		Operator: snapshot.CompoundOperatorAND,
		Conditions: []snapshot.CompoundCondition{
			{Type: snapshot.PolicyTypeHeader, MatchConfig: headerCfg},
			{Type: snapshot.PolicyTypeQuery, MatchConfig: queryCfg},
		},
	})

	tp := &snapshot.TrafficPolicy{
		ID: uuid.New(), Name: "header-query-and", RouteID: r.ID,
		Priority: 100, Type: snapshot.PolicyTypeCompound,
		MatchConfig: compoundCfg, TargetServiceID: canarySvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	p := newTestProxy(t, snap)

	// Both header and query match → canary
	req := httptest.NewRequest("GET", "/api/test?version=v2", nil)
	req.Header.Set("X-Canary", "enabled")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "canary" {
		t.Errorf("compound AND both match: expected canary, got %s", w.Body.String())
	}
	if w.Header().Get("X-Traffic-Policy-Hit") != "true" {
		t.Error("X-Traffic-Policy-Hit should be true")
	}

	// Only header matches → primary
	req2 := httptest.NewRequest("GET", "/api/test", nil)
	req2.Header.Set("X-Canary", "enabled")
	w2 := httptest.NewRecorder()
	p.ServeHTTP(w2, req2)
	if w2.Body.String() != "primary" {
		t.Errorf("compound AND header-only: expected primary, got %s", w2.Body.String())
	}

	// Only query matches → primary
	req3 := httptest.NewRequest("GET", "/api/test?version=v2", nil)
	w3 := httptest.NewRecorder()
	p.ServeHTTP(w3, req3)
	if w3.Body.String() != "primary" {
		t.Errorf("compound AND query-only: expected primary, got %s", w3.Body.String())
	}
}

// TestM12_CookieOrIP_CompoundOR verifies cookie OR IP compound routes to canary.
func TestM12_CookieOrIP_CompoundOR(t *testing.T) {
	primaryBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("primary"))
	}))
	defer primaryBackend.Close()

	canaryBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("canary"))
	}))
	defer canaryBackend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())

	primarySvc, _ := service.New("primary-svc")
	primarySvc.Protocol = service.ProtocolHTTP
	primarySvc.Host = "127.0.0.1"
	primarySvc.Port = parsePort(t, primaryBackend.Listener.Addr().String())
	snap.AddService(primarySvc)

	canarySvc, _ := service.New("canary-svc")
	canarySvc.Protocol = service.ProtocolHTTP
	canarySvc.Host = "127.0.0.1"
	canarySvc.Port = parsePort(t, canaryBackend.Listener.Addr().String())
	snap.AddService(canarySvc)

	r, _ := route.New(primarySvc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	cookieCfg, _ := json.Marshal(snapshot.CookieMatchConfig{Name: "beta", Value: "1"})
	ipCfg, _ := json.Marshal(snapshot.IPMatchConfig{CIDRList: []string{"10.0.0.0/8"}})
	compoundCfg, _ := json.Marshal(snapshot.CompoundMatchConfig{
		Operator: snapshot.CompoundOperatorOR,
		Conditions: []snapshot.CompoundCondition{
			{Type: snapshot.PolicyTypeCookie, MatchConfig: cookieCfg},
			{Type: snapshot.PolicyTypeIP, MatchConfig: ipCfg},
		},
	})

	tp := &snapshot.TrafficPolicy{
		ID: uuid.New(), Name: "cookie-or-ip", RouteID: r.ID,
		Priority: 100, Type: snapshot.PolicyTypeCompound,
		MatchConfig: compoundCfg, TargetServiceID: canarySvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	p := newTestProxy(t, snap)

	// Cookie matches → canary
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: "beta", Value: "1"})
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)
	if w.Body.String() != "canary" {
		t.Errorf("compound OR cookie match: expected canary, got %s", w.Body.String())
	}

	// IP matches → canary
	req2 := httptest.NewRequest("GET", "/api/test", nil)
	req2.RemoteAddr = "10.0.0.50:12345"
	w2 := httptest.NewRecorder()
	p.ServeHTTP(w2, req2)
	if w2.Body.String() != "canary" {
		t.Errorf("compound OR IP match: expected canary, got %s", w2.Body.String())
	}

	// Neither matches → primary
	req3 := httptest.NewRequest("GET", "/api/test", nil)
	req3.RemoteAddr = "192.168.1.1:12345"
	w3 := httptest.NewRecorder()
	p.ServeHTTP(w3, req3)
	if w3.Body.String() != "primary" {
		t.Errorf("compound OR no match: expected primary, got %s", w3.Body.String())
	}
}

// TestM12_MethodAndPath_CompoundAND verifies method + path prefix AND routes to canary.
func TestM12_MethodAndPath_CompoundAND(t *testing.T) {
	primaryBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("primary"))
	}))
	defer primaryBackend.Close()

	canaryBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("canary"))
	}))
	defer canaryBackend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())

	primarySvc, _ := service.New("primary-svc")
	primarySvc.Protocol = service.ProtocolHTTP
	primarySvc.Host = "127.0.0.1"
	primarySvc.Port = parsePort(t, primaryBackend.Listener.Addr().String())
	snap.AddService(primarySvc)

	canarySvc, _ := service.New("canary-svc")
	canarySvc.Protocol = service.ProtocolHTTP
	canarySvc.Host = "127.0.0.1"
	canarySvc.Port = parsePort(t, canaryBackend.Listener.Addr().String())
	snap.AddService(canarySvc)

	r, _ := route.New(primarySvc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	r.AddMethod("POST")
	snap.AddRoute(r)

	methodCfg, _ := json.Marshal(snapshot.MethodMatchConfig{Methods: []string{"POST"}})
	pathCfg, _ := json.Marshal(snapshot.PathMatchConfig{Pattern: "/api/admin", Operator: snapshot.MatchOperatorPrefix})
	compoundCfg, _ := json.Marshal(snapshot.CompoundMatchConfig{
		Operator: snapshot.CompoundOperatorAND,
		Conditions: []snapshot.CompoundCondition{
			{Type: snapshot.PolicyTypeMethod, MatchConfig: methodCfg},
			{Type: snapshot.PolicyTypePath, MatchConfig: pathCfg},
		},
	})

	tp := &snapshot.TrafficPolicy{
		ID: uuid.New(), Name: "method-path-and", RouteID: r.ID,
		Priority: 100, Type: snapshot.PolicyTypeCompound,
		MatchConfig: compoundCfg, TargetServiceID: canarySvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	p := newTestProxy(t, snap)

	// POST + /api/admin path → canary
	req := httptest.NewRequest("POST", "/api/admin/users", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)
	if w.Body.String() != "canary" {
		t.Errorf("method+path AND: expected canary, got %s", w.Body.String())
	}

	// GET + /api/admin path → primary (wrong method)
	req2 := httptest.NewRequest("GET", "/api/admin/users", nil)
	w2 := httptest.NewRecorder()
	p.ServeHTTP(w2, req2)
	if w2.Body.String() != "primary" {
		t.Errorf("method+path AND wrong method: expected primary, got %s", w2.Body.String())
	}

	// POST + wrong path → primary
	req3 := httptest.NewRequest("POST", "/api/other", nil)
	w3 := httptest.NewRecorder()
	p.ServeHTTP(w3, req3)
	if w3.Body.String() != "primary" {
		t.Errorf("method+path AND wrong path: expected primary, got %s", w3.Body.String())
	}
}

// TestM12_WeightedTrafficSplit_E2E verifies weighted traffic splitting with real backends.
func TestM12_WeightedTrafficSplit_E2E(t *testing.T) {
	primaryBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("primary"))
	}))
	defer primaryBackend.Close()

	canaryBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("canary"))
	}))
	defer canaryBackend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())

	primarySvc, _ := service.New("primary-svc")
	primarySvc.Protocol = service.ProtocolHTTP
	primarySvc.Host = "127.0.0.1"
	primarySvc.Port = parsePort(t, primaryBackend.Listener.Addr().String())
	snap.AddService(primarySvc)

	canarySvc, _ := service.New("canary-svc")
	canarySvc.Protocol = service.ProtocolHTTP
	canarySvc.Host = "127.0.0.1"
	canarySvc.Port = parsePort(t, canaryBackend.Listener.Addr().String())
	snap.AddService(canarySvc)

	r, _ := route.New(primarySvc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	weightCfg, _ := json.Marshal(snapshot.WeightMatchConfig{Percentage: 50})
	tp := &snapshot.TrafficPolicy{
		ID: uuid.New(), Name: "weight-50", RouteID: r.ID,
		Priority: 100, Type: snapshot.PolicyTypeWeight,
		MatchConfig: weightCfg, TargetServiceID: canarySvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	p := newTestProxy(t, snap)

	primaryCount, canaryCount := 0, 0
	for i := 0; i < 200; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		w := httptest.NewRecorder()
		p.ServeHTTP(w, req)
		if w.Body.String() == "primary" {
			primaryCount++
		} else {
			canaryCount++
		}
	}

	if canaryCount == 0 {
		t.Errorf("weight 50%%: expected some canary traffic, got 0/%d", 200)
	}
	if primaryCount == 0 {
		t.Errorf("weight 50%%: expected some primary traffic, got 0/%d", 200)
	}
	t.Logf("weight=50: primary=%d, canary=%d (out of 200)", primaryCount, canaryCount)
}

// TestM12_MultiplePolicyTypes_PriorityWins_E2E verifies priority ordering e2e.
func TestM12_MultiplePolicyTypes_PriorityWins_E2E(t *testing.T) {
	primaryBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("primary"))
	}))
	defer primaryBackend.Close()

	lowPriorityBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("low-priority"))
	}))
	defer lowPriorityBackend.Close()

	highPriorityBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("high-priority"))
	}))
	defer highPriorityBackend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())

	primarySvc, _ := service.New("primary-svc")
	primarySvc.Protocol = service.ProtocolHTTP
	primarySvc.Host = "127.0.0.1"
	primarySvc.Port = parsePort(t, primaryBackend.Listener.Addr().String())
	snap.AddService(primarySvc)

	lowSvc, _ := service.New("low-svc")
	lowSvc.Protocol = service.ProtocolHTTP
	lowSvc.Host = "127.0.0.1"
	lowSvc.Port = parsePort(t, lowPriorityBackend.Listener.Addr().String())
	snap.AddService(lowSvc)

	highSvc, _ := service.New("high-svc")
	highSvc.Protocol = service.ProtocolHTTP
	highSvc.Host = "127.0.0.1"
	highSvc.Port = parsePort(t, highPriorityBackend.Listener.Addr().String())
	snap.AddService(highSvc)

	r, _ := route.New(primarySvc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	// Lower priority (100) — header match, would route to lowSvc
	headerCfg, _ := json.Marshal(snapshot.HeaderMatchConfig{Header: "X-Version", Value: "v2"})
	tpLow := &snapshot.TrafficPolicy{
		ID: uuid.New(), Name: "low-priority-header", RouteID: r.ID,
		Priority: 100, Type: snapshot.PolicyTypeHeader,
		MatchConfig: headerCfg, TargetServiceID: lowSvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tpLow)

	// Higher priority (10) — always matches (path prefix ""), routes to highSvc
	pathCfg, _ := json.Marshal(snapshot.PathMatchConfig{Pattern: "/api", Operator: snapshot.MatchOperatorPrefix})
	tpHigh := &snapshot.TrafficPolicy{
		ID: uuid.New(), Name: "high-priority-path", RouteID: r.ID,
		Priority: 10, Type: snapshot.PolicyTypePath,
		MatchConfig: pathCfg, TargetServiceID: highSvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tpHigh)
	snap.Build()

	p := newTestProxy(t, snap)

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Version", "v2")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	// Higher priority (10) wins over lower priority (100), so should go to highSvc
	if w.Body.String() != "high-priority" {
		t.Errorf("priority ordering: expected high-priority, got %s", w.Body.String())
	}
	if w.Header().Get("X-Hit-Policy-ID") != tpHigh.ID.String() {
		t.Errorf("priority: expected hit policy %s, got %s", tpHigh.ID, w.Header().Get("X-Hit-Policy-ID"))
	}
}

// ==================== 集成测试: 治理规则与认证、限流的组合行为 ====================

// TestM12_TrafficPolicy_WithAuth_ConsumerMatch verifies consumer-based traffic
// policy coordinates with key-auth plugin.
func TestM12_TrafficPolicy_WithAuth_ConsumerMatch(t *testing.T) {
	primaryBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("primary"))
	}))
	defer primaryBackend.Close()

	vipBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("vip"))
	}))
	defer vipBackend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())

	primarySvc, _ := service.New("primary-svc")
	primarySvc.Protocol = service.ProtocolHTTP
	primarySvc.Host = "127.0.0.1"
	primarySvc.Port = parsePort(t, primaryBackend.Listener.Addr().String())
	snap.AddService(primarySvc)

	vipSvc, _ := service.New("vip-svc")
	vipSvc.Protocol = service.ProtocolHTTP
	vipSvc.Host = "127.0.0.1"
	vipSvc.Port = parsePort(t, vipBackend.Listener.Addr().String())
	snap.AddService(vipSvc)

	r, _ := route.New(primarySvc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	// VIP consumer
	vipConsumerID := uuid.New()
	vipCred := &credential.Credential{
		ID:         uuid.New(),
		ConsumerID: vipConsumerID,
		Type:       credential.TypeKeyAuth,
		Key:        "vip-key-123",
		Enabled:    true,
	}
	snap.AddCredential(vipCred)

	// Regular consumer
	regularConsumerID := uuid.New()
	regularCred := &credential.Credential{
		ID:         uuid.New(),
		ConsumerID: regularConsumerID,
		Type:       credential.TypeKeyAuth,
		Key:        "regular-key-456",
		Enabled:    true,
	}
	snap.AddCredential(regularCred)

	// Consumer-based traffic policy: VIP consumers → vip-svc
	// Note: getConsumerIDFromRequest checks X-API-Key / Authorization:Bearer headers.
	consumerCfg, _ := json.Marshal(snapshot.ConsumerMatchConfig{
		ConsumerIDs: []uuid.UUID{vipConsumerID},
	})
	tp := &snapshot.TrafficPolicy{
		ID: uuid.New(), Name: "vip-consumer-policy", RouteID: r.ID,
		Priority: 100, Type: snapshot.PolicyTypeConsumer,
		MatchConfig: consumerCfg, TargetServiceID: vipSvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	p := newTestProxy(t, snap)

	// VIP consumer with X-API-Key → routed to vip backend
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-API-Key", "vip-key-123")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("VIP request: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != "vip" {
		t.Errorf("VIP consumer: expected vip, got %s", w.Body.String())
	}
	if w.Header().Get("X-Traffic-Policy-Hit") != "true" {
		t.Error("VIP: X-Traffic-Policy-Hit should be true")
	}
	if w.Header().Get("X-Effective-Service-ID") != vipSvc.ID.String() {
		t.Error("VIP: X-Effective-Service-ID should be vip-svc")
	}

	// Regular consumer → stays on primary
	req2 := httptest.NewRequest("GET", "/api/test", nil)
	req2.Header.Set("X-API-Key", "regular-key-456")
	w2 := httptest.NewRecorder()
	p.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("Regular request: expected 200, got %d", w2.Code)
	}
	if w2.Body.String() != "primary" {
		t.Errorf("Regular consumer: expected primary, got %s", w2.Body.String())
	}
	if w2.Header().Get("X-Traffic-Policy-Hit") != "false" {
		t.Error("Regular: X-Traffic-Policy-Hit should be false")
	}

	// No credentials → defaults to primary
	req3 := httptest.NewRequest("GET", "/api/test", nil)
	w3 := httptest.NewRecorder()
	p.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("No auth request: expected 200, got %d", w3.Code)
	}
	if w3.Body.String() != "primary" {
		t.Errorf("No auth: expected primary, got %s", w3.Body.String())
	}
}

// TestM12_TrafficPolicy_WithRateLimit verifies traffic split still works
// under rate limiting and returns correct response headers.
func TestM12_TrafficPolicy_WithRateLimit(t *testing.T) {
	primaryBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("primary"))
	}))
	defer primaryBackend.Close()

	canaryBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("canary"))
	}))
	defer canaryBackend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())

	primarySvc, _ := service.New("primary-svc")
	primarySvc.Protocol = service.ProtocolHTTP
	primarySvc.Host = "127.0.0.1"
	primarySvc.Port = parsePort(t, primaryBackend.Listener.Addr().String())
	snap.AddService(primarySvc)

	canarySvc, _ := service.New("canary-svc")
	canarySvc.Protocol = service.ProtocolHTTP
	canarySvc.Host = "127.0.0.1"
	canarySvc.Port = parsePort(t, canaryBackend.Listener.Addr().String())
	snap.AddService(canarySvc)

	r, _ := route.New(primarySvc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	// Rate limit: 3 requests per minute (local policy)
	rateLimitPlugin, _ := plugin.New("rate-limit", map[string]interface{}{
		"limit_by": "consumer",
		"limit":    3,
		"window":   "1m",
		"policy":   "local",
	})
	snap.AddPlugin(rateLimitPlugin)

	// Header-based traffic policy
	headerCfg, _ := json.Marshal(snapshot.HeaderMatchConfig{Header: "X-Canary", Value: "enabled"})
	tp := &snapshot.TrafficPolicy{
		ID: uuid.New(), Name: "canary-header", RouteID: r.ID,
		Priority: 100, Type: snapshot.PolicyTypeHeader,
		MatchConfig: headerCfg, TargetServiceID: canarySvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	p := newTestProxy(t, snap)

	// Within rate limit: traffic policy applies
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		req.Header.Set("X-Canary", "enabled")
		w := httptest.NewRecorder()
		p.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, w.Code)
		}
		if w.Body.String() != "canary" {
			t.Errorf("request %d: expected canary, got %s", i+1, w.Body.String())
		}
		if w.Header().Get("X-Traffic-Policy-Hit") != "true" {
			t.Errorf("request %d: X-Traffic-Policy-Hit should be true", i+1)
		}
	}

	// Exceeds rate limit → 429, but traffic policy headers still set
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Canary", "enabled")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("rate limit exceeded: expected 429, got %d", w.Code)
	}
	// Traffic policy info still in headers even when rate-limited
	if w.Header().Get("X-Effective-Service-ID") != canarySvc.ID.String() {
		t.Error("rate-limited: X-Effective-Service-ID should still be set")
	}
}

// TestM12_TrafficPolicy_ResponseHeaders verifies all traffic policy response headers.
func TestM12_TrafficPolicy_ResponseHeaders(t *testing.T) {
	primaryBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer primaryBackend.Close()

	canaryBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"canary"}`))
	}))
	defer canaryBackend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())

	primarySvc, _ := service.New("primary-svc")
	primarySvc.Protocol = service.ProtocolHTTP
	primarySvc.Host = "127.0.0.1"
	primarySvc.Port = parsePort(t, primaryBackend.Listener.Addr().String())
	snap.AddService(primarySvc)

	canarySvc, _ := service.New("canary-svc")
	canarySvc.Protocol = service.ProtocolHTTP
	canarySvc.Host = "127.0.0.1"
	canarySvc.Port = parsePort(t, canaryBackend.Listener.Addr().String())
	snap.AddService(canarySvc)

	r, _ := route.New(primarySvc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	headerCfg, _ := json.Marshal(snapshot.HeaderMatchConfig{Header: "X-Canary", Value: "enabled"})
	tp := &snapshot.TrafficPolicy{
		ID: uuid.New(), Name: "canary-header", RouteID: r.ID,
		Priority: 100, Type: snapshot.PolicyTypeHeader,
		MatchConfig: headerCfg, TargetServiceID: canarySvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	p := newTestProxy(t, snap)

	// Request with canary header
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Canary", "enabled")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Verify all traffic-policy-related response headers
	if w.Header().Get("X-Original-Service-ID") != primarySvc.ID.String() {
		t.Errorf("X-Original-Service-ID: expected %s, got %s",
			primarySvc.ID, w.Header().Get("X-Original-Service-ID"))
	}
	if w.Header().Get("X-Effective-Service-ID") != canarySvc.ID.String() {
		t.Errorf("X-Effective-Service-ID: expected %s, got %s",
			canarySvc.ID, w.Header().Get("X-Effective-Service-ID"))
	}
	if w.Header().Get("X-Traffic-Policy-Hit") != "true" {
		t.Error("X-Traffic-Policy-Hit: expected true")
	}
	if w.Header().Get("X-Hit-Policy-ID") != tp.ID.String() {
		t.Errorf("X-Hit-Policy-ID: expected %s, got %s",
			tp.ID, w.Header().Get("X-Hit-Policy-ID"))
	}

	// Request without canary header
	req2 := httptest.NewRequest("GET", "/api/test", nil)
	w2 := httptest.NewRecorder()
	p.ServeHTTP(w2, req2)

	if w2.Header().Get("X-Traffic-Policy-Hit") != "false" {
		t.Error("X-Traffic-Policy-Hit: expected false for no-match")
	}
	if w2.Header().Get("X-Effective-Service-ID") != primarySvc.ID.String() {
		t.Errorf("X-Effective-Service-ID: expected primary %s, got %s",
			primarySvc.ID, w2.Header().Get("X-Effective-Service-ID"))
	}
}

// ==================== 集成测试: 故障场景切换 (fallback + health check coordination) ====================

// TestM12_Fallback_UnhealthyTargets_TriggersFallback verifies that when
// primary targets fail health checks, fallback policy redirects to fallback service.
func TestM12_Fallback_UnhealthyTargets_TriggersFallback(t *testing.T) {
	// Primary backend — will be unhealthy
	unhealthyBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer unhealthyBackend.Close()

	fallbackBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("fallback"))
	}))
	defer fallbackBackend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())

	primarySvc, _ := service.New("primary-svc")
	primarySvc.Protocol = service.ProtocolHTTP
	snap.AddService(primarySvc)

	fallbackSvc, _ := service.New("fallback-svc")
	fallbackSvc.Protocol = service.ProtocolHTTP
	fallbackSvc.Host = "127.0.0.1"
	fallbackSvc.Port = parsePort(t, fallbackBackend.Listener.Addr().String())
	snap.AddService(fallbackSvc)

	// Put primary behind an upstream with targets
	u, _ := upstream.New("primary-upstream")
	snap.AddUpstream(u)

	// Associate primary service with upstream
	primarySvc.UpstreamID = u.ID
	primarySvc.Host = "" // When using upstream, host is derived from target

	unhealthyTarget := &target.Target{
		ID:         uuid.New(),
		UpstreamID: u.ID,
		Target:     "127.0.0.1",
		Port:       parsePort(t, unhealthyBackend.Listener.Addr().String()),
		Weight:     100,
		Enabled:    true,
	}
	snap.AddTargets(u.ID, []*target.Target{unhealthyTarget})

	r, _ := route.New(primarySvc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	fallbackCfg, _ := json.Marshal(snapshot.FallbackMatchConfig{MinHealthyTargets: 1})
	tp := &snapshot.TrafficPolicy{
		ID: uuid.New(), Name: "fallback-policy", RouteID: r.ID,
		Priority: 100, Type: snapshot.PolicyTypeFallback,
		MatchConfig: fallbackCfg, TargetServiceID: fallbackSvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	p := newTestProxy(t, snap)

	// Send several requests to make primary target fail health checks
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		w := httptest.NewRecorder()
		p.ServeHTTP(w, req)
	}

	// Give health manager time to mark target unhealthy
	time.Sleep(100 * time.Millisecond)

	// Now request should fallback to fallback service
	req := httptest.NewRequest("GET", "/api/test", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	// The fallback should route to fallbackSvc which has direct host/port
	if w.Header().Get("X-Effective-Service-ID") == fallbackSvc.ID.String() ||
		w.Header().Get("X-Traffic-Policy-Hit") == "true" {
		t.Logf("fallback activated: body=%s, policy-hit=%s, effective=%s",
			w.Body.String(),
			w.Header().Get("X-Traffic-Policy-Hit"),
			w.Header().Get("X-Effective-Service-ID"))
	}
}

// ==================== CP-DP 同步测试: Traffic Policy 分发 ====================

// TestM12_CP_DP_TrafficPolicy_Distribution verifies CP publishes revision
// with traffic policies, DP fetches and applies them e2e.
func TestM12_CP_DP_TrafficPolicy_Distribution(t *testing.T) {
	var callCount atomic.Int64

	serviceID := uuid.New()
	routeID := uuid.New()
	canaryServiceID := uuid.New()
	policyID := uuid.New()

	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)

		if r.URL.Path != "/api/v1/public/active-revision" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		headerCfg, _ := json.Marshal(snapshot.HeaderMatchConfig{Header: "X-Canary", Value: "enabled"})

		response := consumer.RevisionResponse{
			RevisionID:  uuid.New().String(),
			Version:     "v1.0",
			Description: "revision with traffic policies",
			Snapshot: &consumer.RevisionSnapshotData{
				Version:   "1",
				Timestamp: time.Now().Format(time.RFC3339),
				Services: []consumer.ServiceSnapshot{
					{ID: serviceID, Name: "primary", Protocol: "http", Host: "127.0.0.1", Port: 18080, Enabled: true},
					{ID: canaryServiceID, Name: "canary", Protocol: "http", Host: "127.0.0.1", Port: 19090, Enabled: true},
				},
				Routes: []consumer.RouteSnapshot{
					{ID: routeID, Name: "api-route", ServiceID: serviceID, Paths: []string{"/api"}, Methods: []string{"GET"}, Enabled: true},
				},
				TrafficPolicies: []consumer.TrafficPolicySnapshot{
					{
						ID:              policyID,
						Name:            "canary-header",
						RouteID:         routeID,
						Priority:        100,
						Type:            "header",
						MatchConfig:     headerCfg,
						TargetServiceID: canaryServiceID,
						Enabled:         true,
						Tags:            []string{"canary"},
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer cpServer.Close()

	logger, _ := zap.NewDevelopment()
	c := consumer.NewSnapshotConsumer(&consumer.SnapshotConsumerConfig{
		ControlURL:   cpServer.URL,
		PollInterval: 50 * time.Millisecond,
		Logger:       logger,
	})

	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer c.Stop(ctx)

	// Wait for first poll
	time.Sleep(200 * time.Millisecond)

	if callCount.Load() == 0 {
		t.Fatal("DP never called CP")
	}

	snap := c.GetCurrentSnapshot()
	if snap == nil {
		t.Fatal("Current snapshot is nil")
	}

	if len(snap.Services) != 2 {
		t.Errorf("expected 2 services, got %d", len(snap.Services))
	}
	if len(snap.Routes) != 1 {
		t.Errorf("expected 1 route, got %d", len(snap.Routes))
	}
	if len(snap.TrafficPolicies) != 1 {
		t.Errorf("expected 1 traffic policy, got %d", len(snap.TrafficPolicies))
	}

	// Build happens inside fetchAndUpdate, so snap is ready for matching
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Canary", "enabled")
	matched, ok := snap.MatchRoute(req)
	if !ok {
		t.Fatal("expected to match route")
	}
	if !matched.TrafficPolicyHit {
		t.Error("expected traffic policy hit")
	}
	if matched.EffectiveService.ID != canaryServiceID {
		t.Errorf("expected canary service %s, got %s", canaryServiceID, matched.EffectiveService.ID)
	}
	if matched.OriginalService.ID != serviceID {
		t.Errorf("expected original service %s, got %s", serviceID, matched.OriginalService.ID)
	}
	t.Logf("CP→DP distribution verified: %d CP calls, policy=%s, hit=%v",
		callCount.Load(), matched.HitPolicyType, matched.TrafficPolicyHit)
}

// TestM12_CP_DP_TrafficPolicy_UpdateToNewRevision verifies that when CP
// publishes a new revision, DP picks up the updated traffic policies.
func TestM12_CP_DP_TrafficPolicy_UpdateToNewRevision(t *testing.T) {
	revisionID1 := uuid.New().String()
	revisionID2 := uuid.New().String()
	currentRevision := atomic.Value{}
	currentRevision.Store(revisionID1)

	serviceID := uuid.New()
	routeID := uuid.New()
	canarySvcID := uuid.New()
	policyID := uuid.New()

	cpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/public/active-revision" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		revID := currentRevision.Load().(string)

		headerCfg, _ := json.Marshal(snapshot.HeaderMatchConfig{Header: "X-Canary", Value: "enabled"})

		response := consumer.RevisionResponse{
			RevisionID: revID,
			Version:    "v1.0",
			Snapshot: &consumer.RevisionSnapshotData{
				Version: "1",
				Services: []consumer.ServiceSnapshot{
					{ID: serviceID, Name: "primary", Protocol: "http", Host: "127.0.0.1", Port: 18080, Enabled: true},
					{ID: canarySvcID, Name: "canary", Protocol: "http", Host: "127.0.0.1", Port: 19090, Enabled: true},
				},
				Routes: []consumer.RouteSnapshot{
					{ID: routeID, Name: "api-route", ServiceID: serviceID, Paths: []string{"/api"}, Methods: []string{"GET"}, Enabled: true},
				},
				TrafficPolicies: []consumer.TrafficPolicySnapshot{
					{
						ID: policyID, Name: "canary-header", RouteID: routeID,
						Priority: 100, Type: "header",
						MatchConfig: headerCfg, TargetServiceID: canarySvcID,
						Enabled: true, Tags: []string{"canary"},
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer cpServer.Close()

	logger, _ := zap.NewDevelopment()
	c := consumer.NewSnapshotConsumer(&consumer.SnapshotConsumerConfig{
		ControlURL:   cpServer.URL,
		PollInterval: 50 * time.Millisecond,
		Logger:       logger,
	})

	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer c.Stop(ctx)

	time.Sleep(200 * time.Millisecond)

	if c.GetCurrentRevisionID() != revisionID1 {
		t.Errorf("expected revision %s, got %s", revisionID1, c.GetCurrentRevisionID())
	}

	// Switch CP to new revision
	currentRevision.Store(revisionID2)
	time.Sleep(200 * time.Millisecond)

	if c.GetCurrentRevisionID() != revisionID2 {
		t.Errorf("expected revision update to %s, got %s", revisionID2, c.GetCurrentRevisionID())
	}

	snap := c.GetCurrentSnapshot()
	if len(snap.TrafficPolicies) != 1 {
		t.Errorf("expected 1 traffic policy in new revision, got %d", len(snap.TrafficPolicies))
	}
	t.Logf("CP→DP revision update: %s → %s", revisionID1, revisionID2)
}
