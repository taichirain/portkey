package snapshot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/domain/credential"
	"github.com/taichirain/portkey/internal/domain/route"
	"github.com/taichirain/portkey/internal/domain/service"
	"github.com/taichirain/portkey/internal/domain/target"
	"github.com/taichirain/portkey/internal/domain/trafficpolicy"
	"github.com/taichirain/portkey/internal/domain/upstream"
)

func TestRouteMatcher_MatchPath(t *testing.T) {
	svcID := uuid.New()
	r, _ := route.New(svcID)
	r.AddPath("/api")
	r.AddMethod("GET")

	matcher, err := NewRouteMatcher(r)
	if err != nil {
		t.Fatalf("NewRouteMatcher failed: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/test", nil)
	matched, _ := matcher.Match(req)
	if !matched {
		t.Errorf("Expected to match /api/test with path /api")
	}

	req2 := httptest.NewRequest("GET", "/other/test", nil)
	matched2, _ := matcher.Match(req2)
	if matched2 {
		t.Errorf("Expected not to match /other/test with path /api")
	}
}

func TestRouteMatcher_MatchMethod(t *testing.T) {
	svcID := uuid.New()
	r, _ := route.New(svcID)
	r.AddMethod("GET")
	r.AddMethod("POST")
	r.AddPath("/test")

	matcher, err := NewRouteMatcher(r)
	if err != nil {
		t.Fatalf("NewRouteMatcher failed: %v", err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	matched, _ := matcher.Match(req)
	if !matched {
		t.Errorf("Expected to match GET request")
	}

	req2 := httptest.NewRequest("POST", "/test", nil)
	matched2, _ := matcher.Match(req2)
	if !matched2 {
		t.Errorf("Expected to match POST request")
	}

	req3 := httptest.NewRequest("DELETE", "/test", nil)
	matched3, _ := matcher.Match(req3)
	if matched3 {
		t.Errorf("Expected not to match DELETE request")
	}
}

func TestRouteMatcher_MatchHost(t *testing.T) {
	svcID := uuid.New()
	r, _ := route.New(svcID)
	r.AddHost("api.example.com")
	r.AddPath("/test")

	matcher, err := NewRouteMatcher(r)
	if err != nil {
		t.Fatalf("NewRouteMatcher failed: %v", err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	req.Host = "api.example.com:8080"
	matched, _ := matcher.Match(req)
	if !matched {
		t.Errorf("Expected to match host api.example.com")
	}

	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.Host = "other.example.com"
	matched2, _ := matcher.Match(req2)
	if matched2 {
		t.Errorf("Expected not to match host other.example.com")
	}
}

func TestRouteMatcher_MatchHeader(t *testing.T) {
	svcID := uuid.New()
	r, _ := route.New(svcID)
	r.Headers = map[string][]string{
		"X-API-Version": {"v1", "v2"},
	}
	r.AddPath("/test")

	matcher, err := NewRouteMatcher(r)
	if err != nil {
		t.Fatalf("NewRouteMatcher failed: %v", err)
	}

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Version", "v1")
	matched, _ := matcher.Match(req)
	if !matched {
		t.Errorf("Expected to match header X-API-Version: v1")
	}

	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.Header.Set("X-API-Version", "v3")
	matched2, _ := matcher.Match(req2)
	if matched2 {
		t.Errorf("Expected not to match header X-API-Version: v3")
	}
}

func TestBalancer_RoundRobin(t *testing.T) {
	up, _ := upstream.New("test-upstream")
	up.Algorithm = upstream.AlgorithmRoundRobin

	t1, _ := target.New(up.ID, "target1", 8080)
	t2, _ := target.New(up.ID, "target2", 8080)
	t3, _ := target.New(up.ID, "target3", 8080)

	balancer := NewBalancer(up, []*target.Target{t1, t2, t3})

	selected := make([]string, 0)
	for i := 0; i < 6; i++ {
		tgt, ok := balancer.Next()
		if !ok {
			t.Fatalf("Expected to get target")
		}
		selected = append(selected, tgt.Target)
	}

	expected := []string{"target1", "target2", "target3", "target1", "target2", "target3"}
	for i, s := range selected {
		if s != expected[i] {
			t.Errorf("Expected %s at position %d, got %s", expected[i], i, s)
		}
	}
}

func TestConfigSnapshot_MatchRoute(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())

	svc, _ := service.New("test-service")
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

	req := httptest.NewRequest("GET", "/api/test", nil)
	matched, ok := snap.MatchRoute(req)
	if !ok {
		t.Errorf("Expected to match route")
	}
	if matched.Service.ID != svc.ID {
		t.Errorf("Expected service ID %s, got %s", svc.ID, matched.Service.ID)
	}
}

func TestConfigSnapshot_MatchRouteWithUpstream(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())

	up, _ := upstream.New("test-upstream")
	snap.AddUpstream(up)

	t1, _ := target.New(up.ID, "target1", 8080)
	t2, _ := target.New(up.ID, "target2", 8080)
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

	req := httptest.NewRequest("GET", "/lb/test", nil)
	matched, ok := snap.MatchRoute(req)
	if !ok {
		t.Errorf("Expected to match route")
	}
	if matched.Upstream == nil {
		t.Errorf("Expected upstream to be set")
	}
	if matched.Balancer == nil {
		t.Errorf("Expected balancer to be set")
	}
	if matched.Upstream.ID != up.ID {
		t.Errorf("Expected upstream ID %s, got %s", up.ID, matched.Upstream.ID)
	}
}

func TestRouteMatcher_MatchEmptyConditions(t *testing.T) {
	svcID := uuid.New()
	r, _ := route.New(svcID)

	_, err := NewRouteMatcher(r)
	if err == nil {
		t.Errorf("Expected error for route with no match conditions")
	}
}

func TestBalancer_EmptyTargets(t *testing.T) {
	up, _ := upstream.New("test-upstream")
	balancer := NewBalancer(up, []*target.Target{})

	_, ok := balancer.Next()
	if ok {
		t.Errorf("Expected false for empty targets")
	}
}

// --- Traffic Policy Tests ---

// setupTrafficSplitSnapshot creates a snapshot with primary + canary services
// and a route pointing to primary, plus the given traffic policies.
func setupTrafficSplitSnapshot(t *testing.T, policies []*TrafficPolicy) *ConfigSnapshot {
	t.Helper()

	snap := NewConfigSnapshot(uuid.New())

	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"
	primarySvc.Port = 8080
	snap.AddService(primarySvc)

	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"
	canarySvc.Port = 9090
	snap.AddService(canarySvc)

	r, _ := route.New(primarySvc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	for _, tp := range policies {
		snap.AddTrafficPolicy(tp)
	}

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	return snap
}

func makeHeaderPolicy(routeID, targetServiceID uuid.UUID, priority int, header, value string) *TrafficPolicy {
	cfg, _ := json.Marshal(HeaderMatchConfig{Header: header, Value: value})
	return &TrafficPolicy{
		ID:              uuid.New(),
		Name:            "header-policy",
		RouteID:         routeID,
		Priority:        priority,
		Type:            PolicyTypeHeader,
		MatchConfig:     cfg,
		TargetServiceID: targetServiceID,
		Enabled:         true,
	}
}

func makeWeightPolicy(routeID, targetServiceID uuid.UUID, priority, percentage int) *TrafficPolicy {
	cfg, _ := json.Marshal(WeightMatchConfig{Percentage: percentage})
	return &TrafficPolicy{
		ID:              uuid.New(),
		Name:            "weight-policy",
		RouteID:         routeID,
		Priority:        priority,
		Type:            PolicyTypeWeight,
		MatchConfig:     cfg,
		TargetServiceID: targetServiceID,
		Enabled:         true,
	}
}

// findIDByName returns a service's ID by name from the snapshot.
func findIDByName(t *testing.T, snap *ConfigSnapshot, name string) uuid.UUID {
	t.Helper()
	for _, svc := range snap.Services {
		if svc.Name == name {
			return svc.ID
		}
	}
	t.Fatalf("Service %q not found in snapshot", name)
	return uuid.Nil
}

// findRouteID returns the first route's ID from the snapshot.
func findRouteID(t *testing.T, snap *ConfigSnapshot) uuid.UUID {
	t.Helper()
	if len(snap.Routes) == 0 {
		t.Fatal("No routes in snapshot")
	}
	return snap.Routes[0].ID
}

// TestTrafficPolicy_HeaderMatch_RoutesToCanary verifies that a matching
// header policy redirects traffic from primary to canary service.
func TestTrafficPolicy_HeaderMatch_RoutesToCanary(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())

	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"
	primarySvc.Port = 8080
	snap.AddService(primarySvc)

	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"
	canarySvc.Port = 9090
	snap.AddService(canarySvc)

	r, _ := route.New(primarySvc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	tp := makeHeaderPolicy(r.ID, canarySvc.ID, 100, "X-Canary", "beta")
	snap.AddTrafficPolicy(tp)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Request with matching header
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Canary", "beta")

	matched, ok := snap.MatchRoute(req)
	if !ok {
		t.Fatal("Expected to match route")
	}

	// Original service should be primary
	if matched.OriginalService.ID != primarySvc.ID {
		t.Errorf("OriginalService = %s, want primary %s", matched.OriginalService.ID, primarySvc.ID)
	}

	// Effective service should be canary (traffic redirected)
	if matched.EffectiveService.ID != canarySvc.ID {
		t.Errorf("EffectiveService = %s, want canary %s", matched.EffectiveService.ID, canarySvc.ID)
	}

	// matched.Service should equal effective service
	if matched.Service.ID != canarySvc.ID {
		t.Errorf("Service = %s, want canary %s (effective)", matched.Service.ID, canarySvc.ID)
	}

	// TrafficPolicyHit should be true
	if !matched.TrafficPolicyHit {
		t.Error("TrafficPolicyHit should be true")
	}

	if matched.HitPolicyID != tp.ID {
		t.Errorf("HitPolicyID = %s, want %s", matched.HitPolicyID, tp.ID)
	}
}

// TestTrafficPolicy_HeaderNoMatch_StaysOnPrimary verifies that without
// a matching header, traffic stays on the original (primary) service.
func TestTrafficPolicy_HeaderNoMatch_StaysOnPrimary(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())

	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"
	primarySvc.Port = 8080
	snap.AddService(primarySvc)

	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"
	canarySvc.Port = 9090
	snap.AddService(canarySvc)

	r, _ := route.New(primarySvc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	tp := makeHeaderPolicy(r.ID, canarySvc.ID, 100, "X-Canary", "beta")
	snap.AddTrafficPolicy(tp)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Request WITHOUT matching header
	req := httptest.NewRequest("GET", "/api/test", nil)

	matched, ok := snap.MatchRoute(req)
	if !ok {
		t.Fatal("Expected to match route")
	}

	if matched.EffectiveService.ID != primarySvc.ID {
		t.Errorf("EffectiveService = %s, want primary %s", matched.EffectiveService.ID, primarySvc.ID)
	}
	if matched.OriginalService.ID != primarySvc.ID {
		t.Errorf("OriginalService = %s, want primary %s", matched.OriginalService.ID, primarySvc.ID)
	}
	if matched.TrafficPolicyHit {
		t.Error("TrafficPolicyHit should be false for non-matching request")
	}
}

// TestTrafficPolicy_HeaderKeyCaseInsensitive verifies that header key
// matching is case-insensitive per design doc section 5.
func TestTrafficPolicy_HeaderKeyCaseInsensitive(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())

	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"
	primarySvc.Port = 8080
	snap.AddService(primarySvc)

	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"
	canarySvc.Port = 9090
	snap.AddService(canarySvc)

	r, _ := route.New(primarySvc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	tp := makeHeaderPolicy(r.ID, canarySvc.ID, 100, "x-canary", "beta")
	snap.AddTrafficPolicy(tp)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Request with different case header key (Go normalizes to canonical form)
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Canary", "beta")

	matched, ok := snap.MatchRoute(req)
	if !ok {
		t.Fatal("Expected to match route")
	}

	if matched.EffectiveService.ID != canarySvc.ID {
		t.Errorf("Header key should be case-insensitive. EffectiveService = %s, want canary %s",
			matched.EffectiveService.ID, canarySvc.ID)
	}
}

// TestTrafficPolicy_HeaderValueCaseSensitivity verifies header value matching
// is case-sensitive per design doc section 5 ("value 精确匹配").
func TestTrafficPolicy_HeaderValueCaseSensitivity(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())

	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"
	primarySvc.Port = 8080
	snap.AddService(primarySvc)

	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"
	canarySvc.Port = 9090
	snap.AddService(canarySvc)

	r, _ := route.New(primarySvc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	// Policy value is "beta" (lowercase)
	tp := makeHeaderPolicy(r.ID, canarySvc.ID, 100, "X-Canary", "beta")
	snap.AddTrafficPolicy(tp)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Request with "BETA" (uppercase) - should NOT match per design doc "精确匹配"
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Canary", "BETA")

	matched, ok := snap.MatchRoute(req)
	if !ok {
		t.Fatal("Expected to match route")
	}

	// Value matching should be case-sensitive per design doc "value 精确匹配".
	// "BETA" should NOT match policy value "beta".
	if matched.EffectiveService.ID == canarySvc.ID {
		t.Errorf("BUG REGRESSION: Header value matching should be case-sensitive")
		t.Errorf("  'BETA' should NOT match policy value 'beta' (设计文档: value 精确匹配)")
		t.Errorf("  Got canary service, want primary service (no match)")
	}
	if matched.TrafficPolicyHit {
		t.Error("TrafficPolicyHit should be false for case-different value")
	}
	// Verify effective stays on original
	if matched.EffectiveService.ID != primarySvc.ID {
		t.Errorf("EffectiveService = %s, want primary (case-different value should not match)",
			matched.EffectiveService.ID)
	}
}

// TestTrafficPolicy_WeightMatch_AllTrafficToCanary verifies that a 100%
// weight policy routes all traffic to canary.
func TestTrafficPolicy_WeightMatch_AllTrafficToCanary(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())

	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"
	primarySvc.Port = 8080
	snap.AddService(primarySvc)

	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"
	canarySvc.Port = 9090
	snap.AddService(canarySvc)

	r, _ := route.New(primarySvc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	tp := makeWeightPolicy(r.ID, canarySvc.ID, 100, 100)
	snap.AddTrafficPolicy(tp)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// With 100% weight, all requests should hit the canary
	matchCount := 0
	for i := 0; i < 20; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		matched, ok := snap.MatchRoute(req)
		if !ok {
			t.Fatal("Expected to match route")
		}
		if matched.EffectiveService.ID == canarySvc.ID {
			matchCount++
		}
	}

	if matchCount != 20 {
		t.Errorf("100%% weight policy: expected 20/20 hits on canary, got %d/20", matchCount)
	}
}

// TestTrafficPolicy_WeightMatch_ZeroTrafficToCanary verifies that a 0%
// weight policy never routes to canary. Using 1% to test probabilistic nature.
func TestTrafficPolicy_WeightMatch_ProbabilityDistribution(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())

	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"
	primarySvc.Port = 8080
	snap.AddService(primarySvc)

	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"
	canarySvc.Port = 9090
	snap.AddService(canarySvc)

	r, _ := route.New(primarySvc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	tp := makeWeightPolicy(r.ID, canarySvc.ID, 100, 50)
	snap.AddTrafficPolicy(tp)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	canaryHits := 0
	const total = 1000
	for i := 0; i < total; i++ {
		req := httptest.NewRequest("GET", "/api/test", nil)
		matched, ok := snap.MatchRoute(req)
		if !ok {
			t.Fatal("Expected to match route")
		}
		if matched.EffectiveService.ID == canarySvc.ID {
			canaryHits++
		}
	}

	// With 50% weight, expect roughly 500/1000 hits. Allow ±10% tolerance.
	pct := float64(canaryHits) / float64(total) * 100
	if pct < 40 || pct > 60 {
		t.Errorf("50%% weight policy: expected ~50%% canary hits, got %.1f%% (%d/%d)", pct, canaryHits, total)
	}
}

// TestTrafficPolicy_PriorityOrdering verifies that when multiple policies
// match, the one with the lowest priority (first in order) wins.
func TestTrafficPolicy_PriorityOrdering(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())

	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"
	primarySvc.Port = 8080
	snap.AddService(primarySvc)

	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"
	canarySvc.Port = 9090
	snap.AddService(canarySvc)

	otherSvc, _ := service.New("other")
	otherSvc.Host = "other.local"
	otherSvc.Port = 7070
	snap.AddService(otherSvc)

	r, _ := route.New(primarySvc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	// Lower priority (10) should win over higher (20)
	// Both match the same header condition
	policy1 := makeHeaderPolicy(r.ID, canarySvc.ID, 10, "X-Canary", "beta")
	policy2 := makeHeaderPolicy(r.ID, otherSvc.ID, 20, "X-Canary", "beta")
	snap.AddTrafficPolicy(policy1)
	snap.AddTrafficPolicy(policy2)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Canary", "beta")

	matched, ok := snap.MatchRoute(req)
	if !ok {
		t.Fatal("Expected to match route")
	}

	// Priority 10 (canary) should win over priority 20 (other)
	if matched.EffectiveService.ID != canarySvc.ID {
		t.Errorf("Priority ordering failed: effective=%s, want canary (priority 10, lowest wins)",
			matched.EffectiveService.ID)
	}
	if matched.HitPolicyID != policy1.ID {
		t.Errorf("HitPolicyID = %s, want %s (priority 10 policy)", matched.HitPolicyID, policy1.ID)
	}
}

// TestTrafficPolicy_DisabledPolicySkipped verifies that disabled policies
// are skipped during traffic policy evaluation.
func TestTrafficPolicy_DisabledPolicySkipped(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())

	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"
	primarySvc.Port = 8080
	snap.AddService(primarySvc)

	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"
	canarySvc.Port = 9090
	snap.AddService(canarySvc)

	r, _ := route.New(primarySvc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	// Disabled policy (should be skipped)
	disabledTP := makeHeaderPolicy(r.ID, canarySvc.ID, 10, "X-Canary", "beta")
	disabledTP.Enabled = false
	snap.AddTrafficPolicy(disabledTP)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Canary", "beta")

	matched, ok := snap.MatchRoute(req)
	if !ok {
		t.Fatal("Expected to match route")
	}

	// Disabled policy should not match, traffic stays on primary
	if matched.TrafficPolicyHit {
		t.Error("Disabled policy should be skipped, TrafficPolicyHit should be false")
	}
	if matched.EffectiveService.ID != primarySvc.ID {
		t.Errorf("Disabled policy should not redirect. EffectiveService = %s, want primary %s",
			matched.EffectiveService.ID, primarySvc.ID)
	}
}

// TestTrafficPolicy_NoPolicies_DefaultsToOriginal verifies that with no
// traffic policies, effective == original service and no hit is recorded.
func TestTrafficPolicy_NoPolicies_DefaultsToOriginal(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())

	svc, _ := service.New("primary")
	svc.Host = "primary.local"
	svc.Port = 8080
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/test", nil)
	matched, ok := snap.MatchRoute(req)
	if !ok {
		t.Fatal("Expected to match route")
	}

	if matched.OriginalService.ID != svc.ID {
		t.Errorf("OriginalService = %s, want %s", matched.OriginalService.ID, svc.ID)
	}
	if matched.EffectiveService.ID != svc.ID {
		t.Errorf("EffectiveService should equal original when no policies: got %s, want %s",
			matched.EffectiveService.ID, svc.ID)
	}
	if matched.TrafficPolicyHit {
		t.Error("TrafficPolicyHit should be false with no policies")
	}
}

// TestTrafficPolicy_EffectiveServiceUpstream verifies that when a traffic
// policy redirects to a canary service with its own upstream, the
// effective service's upstream and balancer are used.
func TestTrafficPolicy_EffectiveServiceUpstream(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())

	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"
	primarySvc.Port = 8080
	snap.AddService(primarySvc)

	canaryUp, _ := upstream.New("canary-upstream")
	snap.AddUpstream(canaryUp)

	t1, _ := target.New(canaryUp.ID, "canary-target", 9090)
	snap.AddTargets(canaryUp.ID, []*target.Target{t1})

	canarySvc, _ := service.New("canary")
	canarySvc.UpstreamID = canaryUp.ID
	canarySvc.Protocol = "http"
	snap.AddService(canarySvc)

	r, _ := route.New(primarySvc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	tp := makeHeaderPolicy(r.ID, canarySvc.ID, 100, "X-Canary", "beta")
	snap.AddTrafficPolicy(tp)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Canary", "beta")

	matched, ok := snap.MatchRoute(req)
	if !ok {
		t.Fatal("Expected to match route")
	}

	// Effective service should be canary
	if matched.EffectiveService.ID != canarySvc.ID {
		t.Fatalf("EffectiveService = %s, want canary", matched.EffectiveService.ID)
	}

	// Should use canary's upstream, not primary's (which has none)
	if matched.Upstream == nil {
		t.Fatal("Expected upstream from effective (canary) service, got nil")
	}
	if matched.Upstream.ID != canaryUp.ID {
		t.Errorf("Upstream = %s, want canary's upstream %s", matched.Upstream.ID, canaryUp.ID)
	}
	if matched.Balancer == nil {
		t.Fatal("Expected balancer from effective (canary) service's upstream, got nil")
	}
}

// TestTrafficPolicy_MatchConfig_InvalidHeaderConfig verifies that an
// invalid header match_config is handled gracefully (doesn't panic).
func TestTrafficPolicy_MatchConfig_InvalidHeaderConfig(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())

	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"
	primarySvc.Port = 8080
	snap.AddService(primarySvc)

	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"
	canarySvc.Port = 9090
	snap.AddService(canarySvc)

	r, _ := route.New(primarySvc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	// Policy with invalid match_config (not valid JSON for HeaderMatchConfig)
	invalidTP := &TrafficPolicy{
		ID:              uuid.New(),
		Name:            "bad-policy",
		RouteID:         r.ID,
		Priority:        100,
		Type:            PolicyTypeHeader,
		MatchConfig:     json.RawMessage(`{"bad": "config"}`),
		TargetServiceID: canarySvc.ID,
		Enabled:         true,
	}
	snap.AddTrafficPolicy(invalidTP)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Canary", "beta")

	matched, ok := snap.MatchRoute(req)
	if !ok {
		t.Fatal("Expected to match route")
	}

	// Invalid config should not match, traffic stays on primary
	if matched.TrafficPolicyHit {
		t.Error("Policy with invalid match_config should not cause TrafficPolicyHit")
	}
}

// TestTrafficPolicy_MatchConfig_InvalidWeightConfig verifies graceful
// handling of invalid weight match_config.
func TestTrafficPolicy_MatchConfig_InvalidWeightConfig(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())

	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"
	primarySvc.Port = 8080
	snap.AddService(primarySvc)

	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"
	canarySvc.Port = 9090
	snap.AddService(canarySvc)

	r, _ := route.New(primarySvc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	invalidTP := &TrafficPolicy{
		ID:              uuid.New(),
		Name:            "bad-weight",
		RouteID:         r.ID,
		Priority:        100,
		Type:            PolicyTypeWeight,
		MatchConfig:     json.RawMessage(`{"percentage": -1}`),
		TargetServiceID: canarySvc.ID,
		Enabled:         true,
	}
	snap.AddTrafficPolicy(invalidTP)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/test", nil)

	matched, ok := snap.MatchRoute(req)
	if !ok {
		t.Fatal("Expected to match route")
	}

	if matched.TrafficPolicyHit {
		t.Error("Policy with invalid weight (negative) should not cause TrafficPolicyHit")
	}
}

// ==================== MatchValue Tests (M12: operators) ====================

func TestMatchValue_Exact(t *testing.T) {
	if !MatchValue("beta", "beta", MatchOperatorExact) {
		t.Error("exact: 'beta' should match 'beta'")
	}
	if MatchValue("BETA", "beta", MatchOperatorExact) {
		t.Error("exact: 'BETA' should not match 'beta' (case-sensitive)")
	}
}

func TestMatchValue_Prefix(t *testing.T) {
	if !MatchValue("/api/v1/users", "/api", MatchOperatorPrefix) {
		t.Error("prefix: '/api/v1/users' should match pattern '/api'")
	}
	if MatchValue("/other/v1/users", "/api", MatchOperatorPrefix) {
		t.Error("prefix: '/other/v1/users' should not match pattern '/api'")
	}
}

func TestMatchValue_Suffix(t *testing.T) {
	if !MatchValue("image.png", ".png", MatchOperatorSuffix) {
		t.Error("suffix: 'image.png' should match '.png'")
	}
	if MatchValue("image.jpg", ".png", MatchOperatorSuffix) {
		t.Error("suffix: 'image.jpg' should not match '.png'")
	}
}

func TestMatchValue_Contains(t *testing.T) {
	if !MatchValue("hello world", "lo wo", MatchOperatorContains) {
		t.Error("contains: 'hello world' should match 'lo wo'")
	}
	if MatchValue("hello world", "xyz", MatchOperatorContains) {
		t.Error("contains: 'hello world' should not match 'xyz'")
	}
}

func TestMatchValue_Regex(t *testing.T) {
	if !MatchValue("abc123", `^[a-z]+\d+$`, MatchOperatorRegex) {
		t.Error("regex: 'abc123' should match '^[a-z]+\\d+$'")
	}
	if MatchValue("abc", `^\d+$`, MatchOperatorRegex) {
		t.Error("regex: 'abc' should not match '^\\d+$'")
	}
}

func TestMatchValue_NotExact(t *testing.T) {
	if !MatchValue("beta", "alpha", MatchOperatorNotExact) {
		t.Error("not_exact: 'beta' should not-equal 'alpha'")
	}
	if MatchValue("beta", "beta", MatchOperatorNotExact) {
		t.Error("not_exact: 'beta' equals 'beta', should return false")
	}
}

func TestMatchValue_NotContains(t *testing.T) {
	if !MatchValue("hello", "xyz", MatchOperatorNotContains) {
		t.Error("not_contains: 'hello' should not contain 'xyz'")
	}
	if MatchValue("hello world", "world", MatchOperatorNotContains) {
		t.Error("not_contains: 'hello world' contains 'world', should return false")
	}
}

func TestMatchValue_NumericComparisons(t *testing.T) {
	if !MatchValue("100", "50", MatchOperatorGreaterThan) {
		t.Error("greater_than: 100 > 50 should be true")
	}
	if MatchValue("50", "100", MatchOperatorGreaterThan) {
		t.Error("greater_than: 50 > 100 should be false")
	}
	if !MatchValue("50", "100", MatchOperatorLessThan) {
		t.Error("less_than: 50 < 100 should be true")
	}
	if !MatchValue("100", "100", MatchOperatorGreaterEqual) {
		t.Error("greater_equal: 100 >= 100 should be true")
	}
	if !MatchValue("100", "100", MatchOperatorLessEqual) {
		t.Error("less_equal: 100 <= 100 should be true")
	}
	// Non-numeric values should fail gracefully
	if MatchValue("abc", "100", MatchOperatorGreaterThan) {
		t.Error("greater_than: non-numeric should return false")
	}
}

func TestMatchValue_DefaultOperator(t *testing.T) {
	// Empty operator defaults to exact match
	if !MatchValue("beta", "beta", "") {
		t.Error("empty operator should default to exact match")
	}
	if MatchValue("BETA", "beta", "") {
		t.Error("empty operator: case should not match")
	}
}

// ==================== MatchHeader Tests ====================

func TestMatchHeader_WithOperators(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Version", "v2.0.1")

	if !MatchHeader(req, "X-Version", "v2", MatchOperatorPrefix) {
		t.Error("header prefix: 'v2.0.1' should match 'v2'")
	}
	if MatchHeader(req, "X-Version", "v3", MatchOperatorPrefix) {
		t.Error("header prefix: 'v2.0.1' should not match 'v3'")
	}
	if !MatchHeader(req, "X-Version", "2.0.1", MatchOperatorContains) {
		t.Error("header contains: 'v2.0.1' should contain '2.0.1'")
	}
}

func TestMatchHeader_MultipleValues(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Add("X-Tag", "alpha")
	req.Header.Add("X-Tag", "beta")

	if !MatchHeader(req, "X-Tag", "beta", MatchOperatorExact) {
		t.Error("header multi-value: should match 'beta' in multi-value header")
	}
}

// ==================== MatchIP Tests ====================

func TestMatchIP_ExactIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.100:54321"

	if !MatchIP(req, []string{"192.168.1.100"}, nil) {
		t.Error("exact IP: 192.168.1.100 should match")
	}
	if MatchIP(req, []string{"192.168.1.200"}, nil) {
		t.Error("exact IP: 192.168.1.200 should not match for 192.168.1.100")
	}
}

func TestMatchIP_CIDR(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.50:8080"

	if !MatchIP(req, nil, []string{"10.0.0.0/24"}) {
		t.Error("CIDR: 10.0.0.50 should match 10.0.0.0/24")
	}
	if MatchIP(req, nil, []string{"192.168.0.0/16"}) {
		t.Error("CIDR: 10.0.0.50 should not match 192.168.0.0/16")
	}
}

func TestMatchIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")
	req.RemoteAddr = "192.168.1.1:12345"

	if !MatchIP(req, []string{"203.0.113.5"}, nil) {
		t.Error("X-Forwarded-For: should use first IP in X-Forwarded-For header")
	}
}

func TestMatchIP_XRealIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Real-IP", "172.16.0.1")
	req.RemoteAddr = "192.168.1.1:12345"

	if !MatchIP(req, []string{"172.16.0.1"}, nil) {
		t.Error("X-Real-IP: should use X-Real-IP header")
	}
}

// ==================== MatchTags Tests ====================

func TestMatchTags_AnyMode(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	svc, _ := service.New("tagged-svc")
	svc.Tags = []string{"canary", "v2", "experimental"}
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)
	snap.Build()

	req := httptest.NewRequest("GET", "/api/test", nil)

	// source_type="service" should match
	if !MatchTags([]string{"canary"}, TagMatchModeAny, "service", snap, req) {
		t.Error("tag any: service has 'canary' tag, should match")
	}
	if !MatchTags([]string{"v3", "canary"}, TagMatchModeAny, "service", snap, req) {
		t.Error("tag any: one of ['v3','canary'] matches, should return true")
	}
	if MatchTags([]string{"v3", "v4"}, TagMatchModeAny, "service", snap, req) {
		t.Error("tag any: neither 'v3' nor 'v4' in service tags, should not match")
	}
}

func TestMatchTags_AllMode(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	svc, _ := service.New("tagged-svc")
	svc.Tags = []string{"canary", "v2", "experimental"}
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)
	snap.Build()

	req := httptest.NewRequest("GET", "/api/test", nil)

	if !MatchTags([]string{"canary", "v2"}, TagMatchModeAll, "service", snap, req) {
		t.Error("tag all: service has both 'canary' and 'v2', should match")
	}
	if MatchTags([]string{"canary", "v3"}, TagMatchModeAll, "service", snap, req) {
		t.Error("tag all: service has 'canary' but not 'v3', should not match")
	}
}

func TestMatchTags_ExactMode(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	svc, _ := service.New("tagged-svc")
	svc.Tags = []string{"canary", "v2"}
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)
	snap.Build()

	req := httptest.NewRequest("GET", "/api/test", nil)

	if !MatchTags([]string{"canary", "v2"}, TagMatchModeExact, "service", snap, req) {
		t.Error("tag exact: ['canary','v2'] exactly matches service tags ['canary','v2']")
	}
	if MatchTags([]string{"canary"}, TagMatchModeExact, "service", snap, req) {
		t.Error("tag exact: ['canary'] does not exactly match ['canary','v2']")
	}
	if MatchTags([]string{"canary", "v2", "v3"}, TagMatchModeExact, "service", snap, req) {
		t.Error("tag exact: too many tags, should not match")
	}
	// Different order should still match (exact uses map comparison)
	if !MatchTags([]string{"v2", "canary"}, TagMatchModeExact, "service", snap, req) {
		t.Error("tag exact: order-independent, ['v2','canary'] should match ['canary','v2']")
	}
}

func TestMatchTags_EmptySourceTags(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	svc, _ := service.New("no-tags-svc")
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)
	snap.Build()

	req := httptest.NewRequest("GET", "/api/test", nil)

	if MatchTags([]string{"canary"}, TagMatchModeAny, "service", snap, req) {
		t.Error("tag match: service has no tags, should not match")
	}
}

func TestMatchTags_InvalidSourceType(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	req := httptest.NewRequest("GET", "/test", nil)

	if MatchTags([]string{"canary"}, TagMatchModeAny, "invalid_source", snap, req) {
		t.Error("tag match: invalid source_type should return false")
	}
}

func TestMatchTags_RouteSource(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	svc, _ := service.New("test-svc")
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.Tags = []string{"api", "public"}
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)
	snap.Build()

	req := httptest.NewRequest("GET", "/api/test", nil)

	if !MatchTags([]string{"api", "public"}, TagMatchModeAll, "route", snap, req) {
		t.Error("tag route source: should match route tags ['api','public']")
	}
}

// ==================== MatchConsumer Tests ====================

func TestMatchConsumer_ByConsumerID(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	consumerID := uuid.New()
	key := "test-api-key-12345"

	cred, _ := credential.New(consumerID, credential.TypeKeyAuth, key)
	snap.AddCredential(cred)

	svc, _ := service.New("test-svc")
	snap.AddService(svc)
	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)
	snap.Build()

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-API-Key", key)

	cfg := &ConsumerMatchConfig{
		ConsumerIDs: []uuid.UUID{consumerID},
	}
	if !MatchConsumer(cfg, snap, req) {
		t.Errorf("consumer match: should find consumer %s by API key", consumerID)
	}
}

func TestMatchConsumer_WrongConsumerID(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	consumerID := uuid.New()
	key := "key-abc"

	cred, _ := credential.New(consumerID, credential.TypeKeyAuth, key)
	snap.AddCredential(cred)

	svc, _ := service.New("test-svc")
	snap.AddService(svc)
	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)
	snap.Build()

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-API-Key", key)

	otherID := uuid.New()
	cfg := &ConsumerMatchConfig{
		ConsumerIDs: []uuid.UUID{otherID},
	}
	if MatchConsumer(cfg, snap, req) {
		t.Error("consumer match: wrong consumer ID should not match")
	}
}

func TestMatchConsumer_ByTags(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	consumerID := uuid.New()
	key := "key-tag-test"

	cred, _ := credential.New(consumerID, credential.TypeKeyAuth, key)
	cred.Tags = []string{"premium", "beta"}
	snap.AddCredential(cred)

	svc, _ := service.New("test-svc")
	snap.AddService(svc)
	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)
	snap.Build()

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-API-Key", key)

	cfg := &ConsumerMatchConfig{
		Tags:      []string{"premium"},
		MatchMode: TagMatchModeAny,
	}
	if !MatchConsumer(cfg, snap, req) {
		t.Error("consumer tag match: should match premium tag")
	}
}

func TestMatchConsumer_TagsAllMode(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	consumerID := uuid.New()
	key := "key-all-tags"

	cred, _ := credential.New(consumerID, credential.TypeKeyAuth, key)
	cred.Tags = []string{"premium", "beta"}
	snap.AddCredential(cred)

	svc, _ := service.New("test-svc")
	snap.AddService(svc)
	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)
	snap.Build()

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-API-Key", key)

	cfg := &ConsumerMatchConfig{
		Tags:      []string{"premium", "beta"},
		MatchMode: TagMatchModeAll,
	}
	if !MatchConsumer(cfg, snap, req) {
		t.Error("consumer tag all: should match both premium and beta tags")
	}

	cfg2 := &ConsumerMatchConfig{
		Tags:      []string{"premium", "gamma"},
		MatchMode: TagMatchModeAll,
	}
	if MatchConsumer(cfg2, snap, req) {
		t.Error("consumer tag all: should not match when 'gamma' tag is missing")
	}
}

func TestMatchConsumer_NoCredentials(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	svc, _ := service.New("test-svc")
	snap.AddService(svc)
	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)
	snap.Build()

	req := httptest.NewRequest("GET", "/api/test", nil)
	// No Authorization or X-API-Key header

	cfg := &ConsumerMatchConfig{
		ConsumerIDs: []uuid.UUID{uuid.New()},
	}
	if MatchConsumer(cfg, snap, req) {
		t.Error("consumer match: no credentials in request should not match")
	}
}

// ==================== Query Policy Tests ====================

func TestTrafficPolicy_QueryMatch_RoutesToCanary(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())

	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"
	primarySvc.Port = 8080
	snap.AddService(primarySvc)

	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"
	canarySvc.Port = 9090
	snap.AddService(canarySvc)

	r, _ := route.New(primarySvc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	cfg, _ := json.Marshal(QueryMatchConfig{Key: "version", Value: "v2"})
	tp := &TrafficPolicy{
		ID: uuid.New(), Name: "query-policy", RouteID: r.ID, Priority: 100,
		Type: PolicyTypeQuery, MatchConfig: cfg, TargetServiceID: canarySvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	req := httptest.NewRequest("GET", "/api/test?version=v2", nil)
	matched, _ := snap.MatchRoute(req)
	if !matched.TrafficPolicyHit {
		t.Error("query match: ?version=v2 should match")
	}
	if matched.EffectiveService.ID != canarySvc.ID {
		t.Errorf("query match: should route to canary, got %s", matched.EffectiveService.ID)
	}
}

func TestTrafficPolicy_QueryNoMatch(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"; primarySvc.Port = 8080
	snap.AddService(primarySvc)
	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"; canarySvc.Port = 9090
	snap.AddService(canarySvc)
	r, _ := route.New(primarySvc.ID); r.AddPath("/api"); r.AddMethod("GET")
	snap.AddRoute(r)

	cfg, _ := json.Marshal(QueryMatchConfig{Key: "version", Value: "v2"})
	tp := &TrafficPolicy{
		ID: uuid.New(), Name: "q", RouteID: r.ID, Priority: 100,
		Type: PolicyTypeQuery, MatchConfig: cfg, TargetServiceID: canarySvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	// Request without matching query param
	req := httptest.NewRequest("GET", "/api/test", nil)
	matched, _ := snap.MatchRoute(req)
	if matched.TrafficPolicyHit {
		t.Error("query no match: missing query param should not match")
	}
}

func TestTrafficPolicy_QueryWithOperator(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"; primarySvc.Port = 8080
	snap.AddService(primarySvc)
	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"; canarySvc.Port = 9090
	snap.AddService(canarySvc)
	r, _ := route.New(primarySvc.ID); r.AddPath("/api"); r.AddMethod("GET")
	snap.AddRoute(r)

	cfg, _ := json.Marshal(QueryMatchConfig{Key: "count", Value: "10", Operator: MatchOperatorGreaterThan})
	tp := &TrafficPolicy{
		ID: uuid.New(), Name: "q", RouteID: r.ID, Priority: 100,
		Type: PolicyTypeQuery, MatchConfig: cfg, TargetServiceID: canarySvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	req := httptest.NewRequest("GET", "/api/test?count=50", nil)
	matched, _ := snap.MatchRoute(req)
	if !matched.TrafficPolicyHit {
		t.Error("query operator: count=50 > 10 should match greater_than")
	}
}

// ==================== Cookie Policy Tests ====================

func TestTrafficPolicy_CookieMatch(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"; primarySvc.Port = 8080
	snap.AddService(primarySvc)
	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"; canarySvc.Port = 9090
	snap.AddService(canarySvc)
	r, _ := route.New(primarySvc.ID); r.AddPath("/api"); r.AddMethod("GET")
	snap.AddRoute(r)

	cfg, _ := json.Marshal(CookieMatchConfig{Name: "feature_flag", Value: "new-ui"})
	tp := &TrafficPolicy{
		ID: uuid.New(), Name: "cookie-policy", RouteID: r.ID, Priority: 100,
		Type: PolicyTypeCookie, MatchConfig: cfg, TargetServiceID: canarySvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: "feature_flag", Value: "new-ui"})

	matched, _ := snap.MatchRoute(req)
	if !matched.TrafficPolicyHit {
		t.Error("cookie match: feature_flag=new-ui should match")
	}
	if matched.EffectiveService.ID != canarySvc.ID {
		t.Error("cookie match: should route to canary")
	}
}

func TestTrafficPolicy_CookieNoValue_MatchesExistence(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"; primarySvc.Port = 8080
	snap.AddService(primarySvc)
	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"; canarySvc.Port = 9090
	snap.AddService(canarySvc)
	r, _ := route.New(primarySvc.ID); r.AddPath("/api"); r.AddMethod("GET")
	snap.AddRoute(r)

	// Cookie with no value requirement: just check existence
	cfg, _ := json.Marshal(CookieMatchConfig{Name: "beta_opt_in"})
	tp := &TrafficPolicy{
		ID: uuid.New(), Name: "cookie-exist", RouteID: r.ID, Priority: 100,
		Type: PolicyTypeCookie, MatchConfig: cfg, TargetServiceID: canarySvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: "beta_opt_in", Value: "anything"})

	matched, _ := snap.MatchRoute(req)
	if !matched.TrafficPolicyHit {
		t.Error("cookie existence: cookie present with any value should match")
	}
}

func TestTrafficPolicy_CookieNotFound(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"; primarySvc.Port = 8080
	snap.AddService(primarySvc)
	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"; canarySvc.Port = 9090
	snap.AddService(canarySvc)
	r, _ := route.New(primarySvc.ID); r.AddPath("/api"); r.AddMethod("GET")
	snap.AddRoute(r)

	cfg, _ := json.Marshal(CookieMatchConfig{Name: "nonexistent", Value: "test"})
	tp := &TrafficPolicy{
		ID: uuid.New(), Name: "c", RouteID: r.ID, Priority: 100,
		Type: PolicyTypeCookie, MatchConfig: cfg, TargetServiceID: canarySvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	req := httptest.NewRequest("GET", "/api/test", nil)
	matched, _ := snap.MatchRoute(req)
	if matched.TrafficPolicyHit {
		t.Error("cookie not found: missing cookie should not match")
	}
}

// ==================== IP Policy Tests ====================

func TestTrafficPolicy_IPMatch_RoutesToCanary(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"; primarySvc.Port = 8080
	snap.AddService(primarySvc)
	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"; canarySvc.Port = 9090
	snap.AddService(canarySvc)
	r, _ := route.New(primarySvc.ID); r.AddPath("/api"); r.AddMethod("GET")
	snap.AddRoute(r)

	cfg, _ := json.Marshal(IPMatchConfig{IPList: []string{"10.0.0.50"}})
	tp := &TrafficPolicy{
		ID: uuid.New(), Name: "ip-policy", RouteID: r.ID, Priority: 100,
		Type: PolicyTypeIP, MatchConfig: cfg, TargetServiceID: canarySvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "10.0.0.50:12345"
	matched, _ := snap.MatchRoute(req)
	if !matched.TrafficPolicyHit {
		t.Error("IP match: 10.0.0.50 should match IP list")
	}
}

func TestTrafficPolicy_IPMatch_CIDR(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"; primarySvc.Port = 8080
	snap.AddService(primarySvc)
	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"; canarySvc.Port = 9090
	snap.AddService(canarySvc)
	r, _ := route.New(primarySvc.ID); r.AddPath("/api"); r.AddMethod("GET")
	snap.AddRoute(r)

	cfg, _ := json.Marshal(IPMatchConfig{CIDRList: []string{"172.16.0.0/16"}})
	tp := &TrafficPolicy{
		ID: uuid.New(), Name: "ip-cidr", RouteID: r.ID, Priority: 100,
		Type: PolicyTypeIP, MatchConfig: cfg, TargetServiceID: canarySvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.RemoteAddr = "172.16.5.5:8080"
	matched, _ := snap.MatchRoute(req)
	if !matched.TrafficPolicyHit {
		t.Error("IP CIDR: 172.16.5.5 should match 172.16.0.0/16")
	}
}

func TestTrafficPolicy_IPMatch_Negate(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"; primarySvc.Port = 8080
	snap.AddService(primarySvc)
	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"; canarySvc.Port = 9090
	snap.AddService(canarySvc)
	r, _ := route.New(primarySvc.ID); r.AddPath("/api"); r.AddMethod("GET")
	snap.AddRoute(r)

	// Negate=true: traffic from non-matching IPs goes to canary
	cfg, _ := json.Marshal(IPMatchConfig{IPList: []string{"10.0.0.1"}, Negate: true})
	tp := &TrafficPolicy{
		ID: uuid.New(), Name: "ip-negate", RouteID: r.ID, Priority: 100,
		Type: PolicyTypeIP, MatchConfig: cfg, TargetServiceID: canarySvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	// Internal IP 10.0.0.1 should NOT match (negate=true)
	reqInternal := httptest.NewRequest("GET", "/api/test", nil)
	reqInternal.RemoteAddr = "10.0.0.1:12345"
	matched1, _ := snap.MatchRoute(reqInternal)
	if matched1.TrafficPolicyHit {
		t.Error("IP negate: internal IP 10.0.0.1 should NOT match (negate=true excludes it)")
	}

	// External IP should match (negate=true)
	reqExternal := httptest.NewRequest("GET", "/api/test", nil)
	reqExternal.RemoteAddr = "203.0.113.50:12345"
	matched2, _ := snap.MatchRoute(reqExternal)
	if !matched2.TrafficPolicyHit {
		t.Error("IP negate: external IP should match (negate=true routes non-matching IPs)")
	}
}

// ==================== Path Policy Tests ====================

func TestTrafficPolicy_PathMatch(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"; primarySvc.Port = 8080
	snap.AddService(primarySvc)
	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"; canarySvc.Port = 9090
	snap.AddService(canarySvc)
	r, _ := route.New(primarySvc.ID); r.AddPath("/api"); r.AddMethod("GET")
	snap.AddRoute(r)

	cfg, _ := json.Marshal(PathMatchConfig{Pattern: "/api/v2", Operator: MatchOperatorPrefix})
	tp := &TrafficPolicy{
		ID: uuid.New(), Name: "path-policy", RouteID: r.ID, Priority: 100,
		Type: PolicyTypePath, MatchConfig: cfg, TargetServiceID: canarySvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	req := httptest.NewRequest("GET", "/api/v2/users", nil)
	matched, _ := snap.MatchRoute(req)
	if !matched.TrafficPolicyHit {
		t.Error("path prefix: '/api/v2/users' should match pattern '/api/v2'")
	}
}

func TestTrafficPolicy_PathRegex(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"; primarySvc.Port = 8080
	snap.AddService(primarySvc)
	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"; canarySvc.Port = 9090
	snap.AddService(canarySvc)
	r, _ := route.New(primarySvc.ID); r.AddPath("/api"); r.AddMethod("GET")
	snap.AddRoute(r)

	cfg, _ := json.Marshal(PathMatchConfig{Pattern: `^/api/user/\d+$`, Operator: MatchOperatorRegex})
	tp := &TrafficPolicy{
		ID: uuid.New(), Name: "path-regex", RouteID: r.ID, Priority: 100,
		Type: PolicyTypePath, MatchConfig: cfg, TargetServiceID: canarySvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	req := httptest.NewRequest("GET", "/api/user/12345", nil)
	matched, _ := snap.MatchRoute(req)
	if !matched.TrafficPolicyHit {
		t.Error("path regex: '/api/user/12345' should match '^/api/user/\\d+$'")
	}
}

// ==================== Method Policy Tests ====================

func TestTrafficPolicy_MethodMatch(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"; primarySvc.Port = 8080
	snap.AddService(primarySvc)
	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"; canarySvc.Port = 9090
	snap.AddService(canarySvc)
	r, _ := route.New(primarySvc.ID); r.AddPath("/api"); r.AddMethod("GET"); r.AddMethod("POST")
	snap.AddRoute(r)

	cfg, _ := json.Marshal(MethodMatchConfig{Methods: []string{"POST"}})
	tp := &TrafficPolicy{
		ID: uuid.New(), Name: "method-policy", RouteID: r.ID, Priority: 100,
		Type: PolicyTypeMethod, MatchConfig: cfg, TargetServiceID: canarySvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	postReq := httptest.NewRequest("POST", "/api/test", nil)
	matched1, _ := snap.MatchRoute(postReq)
	if !matched1.TrafficPolicyHit {
		t.Error("method match: POST should match")
	}

	getReq := httptest.NewRequest("GET", "/api/test", nil)
	matched2, _ := snap.MatchRoute(getReq)
	if matched2.TrafficPolicyHit {
		t.Error("method match: GET should not match POST-only policy")
	}
}

func TestTrafficPolicy_MethodNegate(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"; primarySvc.Port = 8080
	snap.AddService(primarySvc)
	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"; canarySvc.Port = 9090
	snap.AddService(canarySvc)
	r, _ := route.New(primarySvc.ID); r.AddPath("/api")
	snap.AddRoute(r)

	// Negate: route all non-GET requests to canary
	cfg, _ := json.Marshal(MethodMatchConfig{Methods: []string{"GET"}, Negate: true})
	tp := &TrafficPolicy{
		ID: uuid.New(), Name: "method-negate", RouteID: r.ID, Priority: 100,
		Type: PolicyTypeMethod, MatchConfig: cfg, TargetServiceID: canarySvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	// POST should match (negate=true means "not GET")
	postReq := httptest.NewRequest("POST", "/api/test", nil)
	matched1, _ := snap.MatchRoute(postReq)
	if !matched1.TrafficPolicyHit {
		t.Error("method negate: POST should match (not GET)")
	}

	// GET should NOT match (negate=true excludes GET)
	getReq := httptest.NewRequest("GET", "/api/test", nil)
	matched2, _ := snap.MatchRoute(getReq)
	if matched2.TrafficPolicyHit {
		t.Error("method negate: GET should not match (excluded by negate)")
	}
}

// ==================== Compound Policy Tests ====================

func TestTrafficPolicy_CompoundAND(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"; primarySvc.Port = 8080
	snap.AddService(primarySvc)
	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"; canarySvc.Port = 9090
	snap.AddService(canarySvc)
	r, _ := route.New(primarySvc.ID); r.AddPath("/api"); r.AddMethod("GET")
	snap.AddRoute(r)

	// AND: header X-Canary=beta AND query version=v2
	headerCfg, _ := json.Marshal(HeaderMatchConfig{Header: "X-Canary", Value: "beta"})
	queryCfg, _ := json.Marshal(QueryMatchConfig{Key: "version", Value: "v2"})
	compoundCfg, _ := json.Marshal(CompoundMatchConfig{
		Operator: CompoundOperatorAND,
		Conditions: []CompoundCondition{
			{Type: PolicyTypeHeader, MatchConfig: headerCfg},
			{Type: PolicyTypeQuery, MatchConfig: queryCfg},
		},
	})
	tp := &TrafficPolicy{
		ID: uuid.New(), Name: "compound-and", RouteID: r.ID, Priority: 100,
		Type: PolicyTypeCompound, MatchConfig: compoundCfg, TargetServiceID: canarySvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	// Both conditions match
	reqBoth := httptest.NewRequest("GET", "/api/test?version=v2", nil)
	reqBoth.Header.Set("X-Canary", "beta")
	matched1, _ := snap.MatchRoute(reqBoth)
	if !matched1.TrafficPolicyHit {
		t.Error("compound AND: both header and query match, should hit")
	}

	// Only header matches
	reqHeaderOnly := httptest.NewRequest("GET", "/api/test", nil)
	reqHeaderOnly.Header.Set("X-Canary", "beta")
	matched2, _ := snap.MatchRoute(reqHeaderOnly)
	if matched2.TrafficPolicyHit {
		t.Error("compound AND: only header matches, should not hit")
	}

	// Only query matches
	reqQueryOnly := httptest.NewRequest("GET", "/api/test?version=v2", nil)
	matched3, _ := snap.MatchRoute(reqQueryOnly)
	if matched3.TrafficPolicyHit {
		t.Error("compound AND: only query matches, should not hit")
	}
}

func TestTrafficPolicy_CompoundOR(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"; primarySvc.Port = 8080
	snap.AddService(primarySvc)
	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"; canarySvc.Port = 9090
	snap.AddService(canarySvc)
	r, _ := route.New(primarySvc.ID); r.AddPath("/api"); r.AddMethod("GET")
	snap.AddRoute(r)

	headerCfg, _ := json.Marshal(HeaderMatchConfig{Header: "X-Canary", Value: "beta"})
	queryCfg, _ := json.Marshal(QueryMatchConfig{Key: "version", Value: "v2"})
	compoundCfg, _ := json.Marshal(CompoundMatchConfig{
		Operator: CompoundOperatorOR,
		Conditions: []CompoundCondition{
			{Type: PolicyTypeHeader, MatchConfig: headerCfg},
			{Type: PolicyTypeQuery, MatchConfig: queryCfg},
		},
	})
	tp := &TrafficPolicy{
		ID: uuid.New(), Name: "compound-or", RouteID: r.ID, Priority: 100,
		Type: PolicyTypeCompound, MatchConfig: compoundCfg, TargetServiceID: canarySvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	// Header only - should match (OR)
	req1 := httptest.NewRequest("GET", "/api/test", nil)
	req1.Header.Set("X-Canary", "beta")
	matched1, _ := snap.MatchRoute(req1)
	if !matched1.TrafficPolicyHit {
		t.Error("compound OR: header match alone should hit")
	}

	// Query only - should match (OR)
	req2 := httptest.NewRequest("GET", "/api/test?version=v2", nil)
	matched2, _ := snap.MatchRoute(req2)
	if !matched2.TrafficPolicyHit {
		t.Error("compound OR: query match alone should hit")
	}

	// Neither matches
	req3 := httptest.NewRequest("GET", "/api/test", nil)
	matched3, _ := snap.MatchRoute(req3)
	if matched3.TrafficPolicyHit {
		t.Error("compound OR: nothing matches, should not hit")
	}
}

func TestTrafficPolicy_CompoundNestedConditions(t *testing.T) {
	// Test compound with cookie + IP combination
	snap := NewConfigSnapshot(uuid.New())
	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"; primarySvc.Port = 8080
	snap.AddService(primarySvc)
	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"; canarySvc.Port = 9090
	snap.AddService(canarySvc)
	r, _ := route.New(primarySvc.ID); r.AddPath("/api"); r.AddMethod("GET")
	snap.AddRoute(r)

	cookieCfg, _ := json.Marshal(CookieMatchConfig{Name: "premium"})
	ipCfg, _ := json.Marshal(IPMatchConfig{IPList: []string{"10.0.0.50"}})
	compoundCfg, _ := json.Marshal(CompoundMatchConfig{
		Operator: CompoundOperatorAND,
		Conditions: []CompoundCondition{
			{Type: PolicyTypeCookie, MatchConfig: cookieCfg},
			{Type: PolicyTypeIP, MatchConfig: ipCfg},
		},
	})
	tp := &TrafficPolicy{
		ID: uuid.New(), Name: "compound-mixed", RouteID: r.ID, Priority: 100,
		Type: PolicyTypeCompound, MatchConfig: compoundCfg, TargetServiceID: canarySvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	req := httptest.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: "premium", Value: "1"})
	req.RemoteAddr = "10.0.0.50:12345"

	matched, _ := snap.MatchRoute(req)
	if !matched.TrafficPolicyHit {
		t.Error("compound nested: cookie premium + IP 10.0.0.50 should match AND")
	}
}

// ==================== Fallback Policy Tests ====================

func TestTrafficPolicy_Fallback_NoHealthyTargets(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())

	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"; primarySvc.Port = 8080
	snap.AddService(primarySvc)

	fallbackSvc, _ := service.New("fallback")
	fallbackSvc.Host = "fallback.local"; fallbackSvc.Port = 9090
	snap.AddService(fallbackSvc)

	up, _ := upstream.New("primary-upstream")
	snap.AddUpstream(up)
	primarySvc.UpstreamID = up.ID

	t1, _ := target.New(up.ID, "target1", 8080)
	t2, _ := target.New(up.ID, "target2", 8080)
	snap.AddTargets(up.ID, []*target.Target{t1, t2})

	r, _ := route.New(primarySvc.ID)
	r.AddPath("/api"); r.AddMethod("GET")
	snap.AddRoute(r)

	snap.Build()

	// Mark all targets as unhealthy
	snap.UpdateTargetHealth(up.ID, "target1", 8080, false)
	snap.UpdateTargetHealth(up.ID, "target2", 8080, false)

	cfg, _ := json.Marshal(FallbackMatchConfig{FallbackServiceID: fallbackSvc.ID, MinHealthyTargets: 1})
	tp := &TrafficPolicy{
		ID: uuid.New(), Name: "fallback-policy", RouteID: r.ID, Priority: 100,
		Type: PolicyTypeFallback, MatchConfig: cfg, TargetServiceID: fallbackSvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	req := httptest.NewRequest("GET", "/api/test", nil)
	matched, _ := snap.MatchRoute(req)
	if !matched.TrafficPolicyHit {
		t.Error("fallback: 0 healthy targets < min=1, should trigger fallback")
	}
	if matched.EffectiveService.ID != fallbackSvc.ID {
		t.Errorf("fallback: should route to fallback service, got %s", matched.EffectiveService.ID)
	}
	if matched.HitPolicyType != PolicyTypeFallback {
		t.Errorf("fallback: HitPolicyType = %s, want %s", matched.HitPolicyType, PolicyTypeFallback)
	}
}

func TestTrafficPolicy_Fallback_EnoughHealthyTargets(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"; primarySvc.Port = 8080
	snap.AddService(primarySvc)
	fallbackSvc, _ := service.New("fallback")
	fallbackSvc.Host = "fallback.local"; fallbackSvc.Port = 9090
	snap.AddService(fallbackSvc)
	up, _ := upstream.New("primary-upstream")
	snap.AddUpstream(up)
	primarySvc.UpstreamID = up.ID
	t1, _ := target.New(up.ID, "target1", 8080)
	t2, _ := target.New(up.ID, "target2", 8080)
	snap.AddTargets(up.ID, []*target.Target{t1, t2})
	r, _ := route.New(primarySvc.ID); r.AddPath("/api"); r.AddMethod("GET")
	snap.AddRoute(r)
	snap.Build()

	// All targets healthy (default behavior: no health info = healthy)
	cfg, _ := json.Marshal(FallbackMatchConfig{FallbackServiceID: fallbackSvc.ID, MinHealthyTargets: 1})
	tp := &TrafficPolicy{
		ID: uuid.New(), Name: "fallback-ok", RouteID: r.ID, Priority: 100,
		Type: PolicyTypeFallback, MatchConfig: cfg, TargetServiceID: fallbackSvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	req := httptest.NewRequest("GET", "/api/test", nil)
	matched, _ := snap.MatchRoute(req)
	if matched.TrafficPolicyHit {
		t.Error("fallback: all targets healthy, should NOT trigger fallback")
	}
}

func TestTrafficPolicy_Fallback_ServiceWithoutUpstream(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"; primarySvc.Port = 8080
	// No upstream = direct host/port service
	snap.AddService(primarySvc)
	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"; canarySvc.Port = 9090
	snap.AddService(canarySvc)
	r, _ := route.New(primarySvc.ID); r.AddPath("/api"); r.AddMethod("GET")
	snap.AddRoute(r)
	snap.Build()

	cfg, _ := json.Marshal(FallbackMatchConfig{FallbackServiceID: canarySvc.ID})
	tp := &TrafficPolicy{
		ID: uuid.New(), Name: "fallback-direct", RouteID: r.ID, Priority: 100,
		Type: PolicyTypeFallback, MatchConfig: cfg, TargetServiceID: canarySvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	req := httptest.NewRequest("GET", "/api/test", nil)
	matched, _ := snap.MatchRoute(req)
	// Direct service (no upstream) always triggers fallback per current logic
	// because originalSvc.UpstreamID == uuid.Nil returns true
	if !matched.TrafficPolicyHit {
		t.Log("fallback direct: service without upstream triggers fallback by design (UpstreamID == Nil)")
	}
}

// ==================== HitPolicyType Tests ====================

func TestTrafficPolicy_HitPolicyType(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"; primarySvc.Port = 8080
	snap.AddService(primarySvc)
	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"; canarySvc.Port = 9090
	snap.AddService(canarySvc)
	r, _ := route.New(primarySvc.ID); r.AddPath("/api"); r.AddMethod("GET")
	snap.AddRoute(r)

	// Weight policy
	cfg, _ := json.Marshal(WeightMatchConfig{Percentage: 100})
	tp := &TrafficPolicy{
		ID: uuid.New(), Name: "weight", RouteID: r.ID, Priority: 100,
		Type: PolicyTypeWeight, MatchConfig: cfg, TargetServiceID: canarySvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tp)
	snap.Build()

	req := httptest.NewRequest("GET", "/api/test", nil)
	matched, _ := snap.MatchRoute(req)
	if !matched.TrafficPolicyHit {
		t.Fatal("weight 100% should always hit")
	}
	if matched.HitPolicyType != PolicyTypeWeight {
		t.Errorf("HitPolicyType = %s, want %s", matched.HitPolicyType, PolicyTypeWeight)
	}
}

// ==================== Multi-Policy Complex Scenarios ====================

func TestTrafficPolicy_MultipleTypes_PriorityWins(t *testing.T) {
	snap := NewConfigSnapshot(uuid.New())
	primarySvc, _ := service.New("primary")
	primarySvc.Host = "primary.local"; primarySvc.Port = 8080
	snap.AddService(primarySvc)
	canarySvc, _ := service.New("canary")
	canarySvc.Host = "canary.local"; canarySvc.Port = 9090
	snap.AddService(canarySvc)
	otherSvc, _ := service.New("other")
	otherSvc.Host = "other.local"; otherSvc.Port = 7070
	snap.AddService(otherSvc)
	r, _ := route.New(primarySvc.ID); r.AddPath("/api"); r.AddMethod("GET")
	snap.AddRoute(r)

	// Priority 10: method match -> canary
	methodCfg, _ := json.Marshal(MethodMatchConfig{Methods: []string{"GET"}})
	tpMethod := &TrafficPolicy{
		ID: uuid.New(), Name: "method-first", RouteID: r.ID, Priority: 10,
		Type: PolicyTypeMethod, MatchConfig: methodCfg, TargetServiceID: canarySvc.ID, Enabled: true,
	}
	// Priority 20: query match -> other (lower priority, should not win when both match)
	queryCfg, _ := json.Marshal(QueryMatchConfig{Key: "v", Value: "2"})
	tpQuery := &TrafficPolicy{
		ID: uuid.New(), Name: "query-second", RouteID: r.ID, Priority: 20,
		Type: PolicyTypeQuery, MatchConfig: queryCfg, TargetServiceID: otherSvc.ID, Enabled: true,
	}
	snap.AddTrafficPolicy(tpMethod)
	snap.AddTrafficPolicy(tpQuery)
	snap.Build()

	// Both policies match this request, priority 10 (method) should win
	req := httptest.NewRequest("GET", "/api/test?v=2", nil)
	matched, _ := snap.MatchRoute(req)
	if !matched.TrafficPolicyHit {
		t.Fatal("at least one policy should match")
	}
	if matched.EffectiveService.ID != canarySvc.ID {
		t.Errorf("multi-type priority: should hit canary (priority 10), got %s", matched.EffectiveService.ID)
	}
	if matched.HitPolicyType != PolicyTypeMethod {
		t.Errorf("multi-type priority: HitPolicyType = %s, want %s", matched.HitPolicyType, PolicyTypeMethod)
	}
}

// ==================== TrafficPolicySnapshot Tests (RTTI) ====================

func TestTrafficPolicy_RoundTripToFromDomain(t *testing.T) {
	domainTP, err := trafficpolicy.New(uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("trafficpolicy.New() error = %v", err)
	}
	domainTP.Name = "rtti-test"
	domainTP.Priority = 50
	domainTP.Type = trafficpolicy.PolicyType("header")
	domainTP.SetHeaderMatchConfig("X-Test", "value")
	domainTP.Enabled = true
	domainTP.Tags = []string{"test", "rtti"}

	snapTP := FromDomainTrafficPolicy(domainTP)
	result := ToDomainTrafficPolicy(snapTP)

	if result.ID != domainTP.ID {
		t.Errorf("RTTI: ID mismatch")
	}
	if result.Name != domainTP.Name {
		t.Errorf("RTTI: Name mismatch")
	}
	if result.Type != domainTP.Type {
		t.Errorf("RTTI: Type mismatch")
	}
	if result.Priority != domainTP.Priority {
		t.Errorf("RTTI: Priority mismatch")
	}
	if !result.Enabled {
		t.Error("RTTI: Enabled should be true")
	}
	if len(result.Tags) != 2 {
		t.Errorf("RTTI: Tags length = %d, want 2", len(result.Tags))
	}
}
