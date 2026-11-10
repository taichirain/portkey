package snapshot

import (
	"testing"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/domain/target"
	"github.com/taichirain/portkey/internal/domain/upstream"
)

func TestBuildPreservesIndexesOnFailureAndRemovesStaleBalancers(t *testing.T) {
	s := NewConfigSnapshot(uuid.New())
	u, err := upstream.New("test")
	if err != nil {
		t.Fatal(err)
	}
	target1, err := target.New(u.ID, "127.0.0.1", 8080)
	if err != nil {
		t.Fatal(err)
	}
	s.AddUpstream(u)
	s.AddTargets(u.ID, []*target.Target{target1})
	if err := s.Build(); err != nil {
		t.Fatal(err)
	}
	old, ok := s.GetBalancer(u.ID)
	if !ok {
		t.Fatal("initial balancer missing")
	}
	missingID := uuid.New()
	target2, err := target.New(missingID, "127.0.0.1", 8081)
	if err != nil {
		t.Fatal(err)
	}
	s.AddTargets(missingID, []*target.Target{target2})
	if err := s.Build(); err == nil {
		t.Fatal("expected missing upstream error")
	}
	if got, ok := s.GetBalancer(u.ID); !ok || got != old {
		t.Fatal("failed build changed live indexes")
	}
	delete(s.Targets, missingID)
	delete(s.Targets, u.ID)
	if err := s.Build(); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.GetBalancer(u.ID); ok {
		t.Fatal("removed upstream still has a balancer")
	}
}

func TestBuildOrdersEqualPriorityPoliciesByID(t *testing.T) {
	s := NewConfigSnapshot(uuid.New())
	routeID := uuid.New()
	id1 := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	id2 := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	s.TrafficPolicies = []*TrafficPolicy{{ID: id2, RouteID: routeID, Priority: 1}, {ID: id1, RouteID: routeID, Priority: 1}}
	if err := s.Build(); err != nil {
		t.Fatal(err)
	}
	if s.trafficPoliciesByRoute[routeID][0].ID != id1 {
		t.Fatal("equal priorities depend on input order")
	}
}
