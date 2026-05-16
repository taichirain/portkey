package consumer

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/data/snapshot"
)

// TestJSONUnmarshal_TrafficPoliciesParsed verifies that traffic_policies
// in the CP JSON response are correctly parsed into RevisionSnapshotData.
func TestJSONUnmarshal_TrafficPoliciesParsed(t *testing.T) {
	cpJSON := `{
		"revision_id": "00000000-0000-0000-0000-000000000001",
		"version": "v1.0.0",
		"description": "test revision with traffic split",
		"snapshot": {
			"version": "v1.0.0",
			"services": [
				{"id": "00000000-0000-0000-0000-000000000010", "name": "primary", "protocol": "http", "host": "primary.local", "port": 8080, "retries": 0, "connect_timeout": 0, "write_timeout": 0, "read_timeout": 0, "tags": [], "enabled": true},
				{"id": "00000000-0000-0000-0000-000000000020", "name": "canary", "protocol": "http", "host": "canary.local", "port": 9090, "retries": 0, "connect_timeout": 0, "write_timeout": 0, "read_timeout": 0, "tags": [], "enabled": true}
			],
			"routes": [
				{"id": "00000000-0000-0000-0000-000000000030", "name": "api-route", "service_id": "00000000-0000-0000-0000-000000000010", "protocols": [], "methods": ["GET"], "hosts": [], "paths": ["/api"], "headers": null, "strip_path": false, "preserve_host": false, "regex_priority": 0, "tags": [], "enabled": true}
			],
			"upstreams": [],
			"consumers": [],
			"credentials": [],
			"plugins": [],
			"traffic_policies": [
				{
					"id": "00000000-0000-0000-0000-000000000040",
					"name": "canary-header",
					"route_id": "00000000-0000-0000-0000-000000000030",
					"priority": 100,
					"type": "header",
					"match_config": {"header": "X-Canary", "value": "beta"},
					"target_service_id": "00000000-0000-0000-0000-000000000020",
					"enabled": true,
					"tags": ["canary"]
				}
			]
		}
	}`

	var resp RevisionResponse
	if err := json.Unmarshal([]byte(cpJSON), &resp); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}

	if resp.Snapshot == nil {
		t.Fatal("Snapshot is nil")
	}

	// Verify traffic_policies were parsed
	if len(resp.Snapshot.TrafficPolicies) != 1 {
		t.Fatalf("Expected 1 traffic policy, got %d", len(resp.Snapshot.TrafficPolicies))
	}

	tp := resp.Snapshot.TrafficPolicies[0]
	if tp.Name != "canary-header" {
		t.Errorf("Policy name = %s, want canary-header", tp.Name)
	}
	if tp.Type != "header" {
		t.Errorf("Policy type = %s, want header", tp.Type)
	}
	if !tp.Enabled {
		t.Error("Policy should be enabled")
	}

	// Now build the config snapshot and verify traffic split works end-to-end
	c := &SnapshotConsumer{}
	snap, err := c.buildConfigSnapshot(&resp)
	if err != nil {
		t.Fatalf("buildConfigSnapshot error = %v", err)
	}

	// Verify services and routes
	if len(snap.Services) != 2 {
		t.Errorf("expected 2 services, got %d", len(snap.Services))
	}
	if len(snap.Routes) != 1 {
		t.Errorf("expected 1 route, got %d", len(snap.Routes))
	}

	// Send request with canary header - should route to canary
	req := httptest.NewRequest("GET", "/api/test", nil)
	req.Header.Set("X-Canary", "beta")

	matched, ok := snap.MatchRoute(req)
	if !ok {
		t.Fatal("Expected to match route")
	}

	// Traffic should be redirected to canary service
	if matched.EffectiveService.Name != "canary" {
		t.Errorf("EffectiveService = %s, want canary (traffic split not applied)",
			matched.EffectiveService.Name)
	}
	if matched.OriginalService.Name != "primary" {
		t.Errorf("OriginalService = %s, want primary", matched.OriginalService.Name)
	}
	if !matched.TrafficPolicyHit {
		t.Error("TrafficPolicyHit should be true")
	}
}

// TestBuildConfigSnapshot_NoTrafficPoliciesInJSON verifies behavior when
// the snapshot JSON has no traffic_policies field.
func TestBuildConfigSnapshot_NoTrafficPoliciesInJSON(t *testing.T) {
	c := &SnapshotConsumer{}

	revID := uuid.New()
	routeID := uuid.New()
	svcID := uuid.New()

	resp := &RevisionResponse{
		RevisionID: revID.String(),
		Version:    "v1",
		Snapshot: &RevisionSnapshotData{
			Services: []ServiceSnapshot{
				{ID: svcID, Name: "svc1", Protocol: "http", Host: "localhost", Port: 8080, Enabled: true},
			},
			Routes: []RouteSnapshot{
				{ID: routeID, Name: "route1", ServiceID: svcID, Methods: []string{"GET"}, Paths: []string{"/"}, Enabled: true},
			},
		},
	}

	snap, err := c.buildConfigSnapshot(resp)
	if err != nil {
		t.Fatalf("buildConfigSnapshot error = %v", err)
	}

	if snap == nil {
		t.Fatal("buildConfigSnapshot returned nil")
	}

	// No traffic policies means effective == original
	req := httptest.NewRequest("GET", "/test", nil)
	matched, ok := snap.MatchRoute(req)
	if !ok {
		t.Fatal("Expected to match route")
	}

	if matched.TrafficPolicyHit {
		t.Error("TrafficPolicyHit should be false with no policies")
	}
	if matched.EffectiveService.ID != svcID {
		t.Errorf("EffectiveService should equal original when no policies")
	}
}

// TestRevisionSnapshotData_HasTrafficPoliciesField verifies the struct
// has the TrafficPolicies field (compile-time + runtime check).
func TestRevisionSnapshotData_HasTrafficPoliciesField(t *testing.T) {
	data := RevisionSnapshotData{
		TrafficPolicies: []TrafficPolicySnapshot{
			{
				ID:   uuid.New(),
				Name: "test-policy",
			},
		},
	}
	if len(data.TrafficPolicies) != 1 {
		t.Errorf("TrafficPolicies length = %d, want 1", len(data.TrafficPolicies))
	}
	if data.TrafficPolicies[0].Name != "test-policy" {
		t.Errorf("TrafficPolicies[0].Name = %s, want test-policy", data.TrafficPolicies[0].Name)
	}
}

// Verify the types we need exist
var _ *snapshot.ConfigSnapshot
