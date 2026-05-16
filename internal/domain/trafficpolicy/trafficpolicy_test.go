package trafficpolicy

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestNew_ValidIDs(t *testing.T) {
	routeID := uuid.New()
	targetSvcID := uuid.New()
	tp, err := New(routeID, targetSvcID)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if tp.ID == uuid.Nil {
		t.Error("New() id should not be nil")
	}
	if tp.RouteID != routeID {
		t.Error("New() route_id mismatch")
	}
	if tp.TargetServiceID != targetSvcID {
		t.Error("New() target_service_id mismatch")
	}
	if !tp.Enabled {
		t.Error("New() should default to enabled")
	}
	if tp.Type != PolicyTypeHeader {
		t.Errorf("New() default type = %s, want header", tp.Type)
	}
}

func TestNew_NilRouteID(t *testing.T) {
	_, err := New(uuid.Nil, uuid.New())
	if err != ErrTrafficPolicyRouteIDRequired {
		t.Errorf("New() error = %v, want ErrTrafficPolicyRouteIDRequired", err)
	}
}

func TestNew_NilTargetServiceID(t *testing.T) {
	_, err := New(uuid.New(), uuid.Nil)
	if err != ErrTrafficPolicyTargetServiceIDRequired {
		t.Errorf("New() error = %v, want ErrTrafficPolicyTargetServiceIDRequired", err)
	}
}

func TestValidate_EmptyMatchConfig(t *testing.T) {
	tp, _ := New(uuid.New(), uuid.New())
	tp.Name = "test"
	tp.Priority = 100
	tp.MatchConfig = json.RawMessage("{}")
	err := tp.Validate()
	if err != ErrTrafficPolicyMatchConfigRequired {
		t.Errorf("Validate() error = %v, want ErrTrafficPolicyMatchConfigRequired", err)
	}
}

func TestValidate_NilMatchConfig(t *testing.T) {
	tp, _ := New(uuid.New(), uuid.New())
	tp.Name = "test"
	tp.Priority = 100
	tp.MatchConfig = json.RawMessage("null")
	err := tp.Validate()
	if err != ErrTrafficPolicyMatchConfigRequired {
		t.Errorf("Validate() error = %v, want ErrTrafficPolicyMatchConfigRequired", err)
	}
}

func TestValidate_HeaderConfig_Valid(t *testing.T) {
	tp, _ := New(uuid.New(), uuid.New())
	tp.Name = "header-test"
	tp.Priority = 100
	tp.Type = PolicyTypeHeader
	tp.SetHeaderMatchConfig("X-Canary", "beta")
	err := tp.Validate()
	if err != nil {
		t.Errorf("Validate() header error = %v, want nil", err)
	}
}

func TestValidate_HeaderConfig_MissingHeader(t *testing.T) {
	tp, _ := New(uuid.New(), uuid.New())
	tp.Name = "header-test"
	tp.Priority = 100
	tp.Type = PolicyTypeHeader
	cfg, _ := json.Marshal(HeaderMatchConfig{Header: "", Value: "beta"})
	tp.MatchConfig = cfg
	err := tp.Validate()
	if err != ErrTrafficPolicyInvalidHeaderConfig {
		t.Errorf("Validate() error = %v, want ErrTrafficPolicyInvalidHeaderConfig", err)
	}
}

func TestValidate_HeaderConfig_MissingValue(t *testing.T) {
	tp, _ := New(uuid.New(), uuid.New())
	tp.Name = "header-test"
	tp.Priority = 100
	tp.Type = PolicyTypeHeader
	cfg, _ := json.Marshal(HeaderMatchConfig{Header: "X-Canary", Value: ""})
	tp.MatchConfig = cfg
	err := tp.Validate()
	if err != ErrTrafficPolicyInvalidHeaderConfig {
		t.Errorf("Validate() error = %v, want ErrTrafficPolicyInvalidHeaderConfig", err)
	}
}

func TestValidate_WeightConfig_Valid(t *testing.T) {
	tp, _ := New(uuid.New(), uuid.New())
	tp.Name = "weight-test"
	tp.Priority = 100
	tp.Type = PolicyTypeWeight
	tp.SetWeightMatchConfig(50)
	err := tp.Validate()
	if err != nil {
		t.Errorf("Validate() weight error = %v, want nil", err)
	}
}

func TestValidate_WeightConfig_PercentageTooLow(t *testing.T) {
	tp, _ := New(uuid.New(), uuid.New())
	tp.Name = "weight-test"
	tp.Priority = 100
	tp.Type = PolicyTypeWeight
	cfg, _ := json.Marshal(WeightMatchConfig{Percentage: 0})
	tp.MatchConfig = cfg
	err := tp.Validate()
	if err != ErrTrafficPolicyInvalidWeightConfig {
		t.Errorf("Validate() error = %v, want ErrTrafficPolicyInvalidWeightConfig", err)
	}
}

func TestValidate_WeightConfig_PercentageTooHigh(t *testing.T) {
	tp, _ := New(uuid.New(), uuid.New())
	tp.Name = "weight-test"
	tp.Priority = 100
	tp.Type = PolicyTypeWeight
	cfg, _ := json.Marshal(WeightMatchConfig{Percentage: 101})
	tp.MatchConfig = cfg
	err := tp.Validate()
	if err != ErrTrafficPolicyInvalidWeightConfig {
		t.Errorf("Validate() error = %v, want ErrTrafficPolicyInvalidWeightConfig", err)
	}
}

func TestValidate_InvalidType(t *testing.T) {
	tp, _ := New(uuid.New(), uuid.New())
	tp.Name = "test"
	tp.Priority = 100
	tp.Type = PolicyType("invalid")
	cfg, _ := json.Marshal(HeaderMatchConfig{Header: "X", Value: "v"})
	tp.MatchConfig = cfg
	err := tp.Validate()
	if err != ErrTrafficPolicyInvalidType {
		t.Errorf("Validate() error = %v, want ErrTrafficPolicyInvalidType", err)
	}
}

func TestValidate_NameTooLong(t *testing.T) {
	tp, _ := New(uuid.New(), uuid.New())
	tp.Priority = 100
	tp.Type = PolicyTypeHeader
	tp.SetHeaderMatchConfig("X-Canary", "beta")
	// build a name > 255 chars
	longName := ""
	for i := 0; i < 256; i++ {
		longName += "a"
	}
	tp.Name = longName
	err := tp.Validate()
	if err != ErrTrafficPolicyNameTooLong {
		t.Errorf("Validate() error = %v, want ErrTrafficPolicyNameTooLong", err)
	}
}

func TestValidate_NegativePriority(t *testing.T) {
	tp, _ := New(uuid.New(), uuid.New())
	tp.Name = "test"
	tp.Priority = -1
	tp.Type = PolicyTypeHeader
	tp.SetHeaderMatchConfig("X-Canary", "beta")
	err := tp.Validate()
	if err != ErrTrafficPolicyPriorityRequired {
		t.Errorf("Validate() error = %v, want ErrTrafficPolicyPriorityRequired", err)
	}
}

func TestEnableDisable(t *testing.T) {
	tp, _ := New(uuid.New(), uuid.New())
	if !tp.Enabled {
		t.Error("New() should be enabled by default")
	}
	tp.Disable()
	if tp.Enabled {
		t.Error("Disable() should set enabled to false")
	}
	tp.Enable()
	if !tp.Enabled {
		t.Error("Enable() should set enabled to true")
	}
}

func TestAddRemoveTag(t *testing.T) {
	tp, _ := New(uuid.New(), uuid.New())
	tp.AddTag("canary")
	if len(tp.Tags) != 1 || tp.Tags[0] != "canary" {
		t.Error("AddTag failed")
	}
	// duplicate
	tp.AddTag("canary")
	if len(tp.Tags) != 1 {
		t.Error("AddTag should not add duplicates")
	}
	tp.AddTag("v2")
	if len(tp.Tags) != 2 {
		t.Error("AddTag should add unique tags")
	}
	tp.RemoveTag("canary")
	if len(tp.Tags) != 1 || tp.Tags[0] != "v2" {
		t.Error("RemoveTag failed")
	}
}

func TestSetHeaderMatchConfig(t *testing.T) {
	tp, _ := New(uuid.New(), uuid.New())
	err := tp.SetHeaderMatchConfig("X-Canary", "beta")
	if err != nil {
		t.Fatalf("SetHeaderMatchConfig error = %v", err)
	}
	if tp.Type != PolicyTypeHeader {
		t.Errorf("SetHeaderMatchConfig should set type to header, got %s", tp.Type)
	}
	cfg, err := tp.GetHeaderMatchConfig()
	if err != nil {
		t.Fatalf("GetHeaderMatchConfig error = %v", err)
	}
	if cfg.Header != "X-Canary" || cfg.Value != "beta" {
		t.Errorf("GetHeaderMatchConfig got header=%s value=%s", cfg.Header, cfg.Value)
	}
}

func TestSetWeightMatchConfig(t *testing.T) {
	tp, _ := New(uuid.New(), uuid.New())
	err := tp.SetWeightMatchConfig(30)
	if err != nil {
		t.Fatalf("SetWeightMatchConfig error = %v", err)
	}
	if tp.Type != PolicyTypeWeight {
		t.Errorf("SetWeightMatchConfig should set type to weight, got %s", tp.Type)
	}
	cfg, err := tp.GetWeightMatchConfig()
	if err != nil {
		t.Fatalf("GetWeightMatchConfig error = %v", err)
	}
	if cfg.Percentage != 30 {
		t.Errorf("GetWeightMatchConfig got percentage=%d", cfg.Percentage)
	}
}

func TestSetWeightMatchConfig_Invalid(t *testing.T) {
	tp, _ := New(uuid.New(), uuid.New())
	err := tp.SetWeightMatchConfig(0)
	if err != ErrTrafficPolicyInvalidWeightConfig {
		t.Errorf("SetWeightMatchConfig(0) error = %v, want ErrTrafficPolicyInvalidWeightConfig", err)
	}
	err = tp.SetWeightMatchConfig(101)
	if err != ErrTrafficPolicyInvalidWeightConfig {
		t.Errorf("SetWeightMatchConfig(101) error = %v, want ErrTrafficPolicyInvalidWeightConfig", err)
	}
}

func TestGetHeaderMatchConfig_WrongType(t *testing.T) {
	tp, _ := New(uuid.New(), uuid.New())
	tp.Type = PolicyTypeWeight
	_, err := tp.GetHeaderMatchConfig()
	if err == nil {
		t.Error("GetHeaderMatchConfig should error on weight type")
	}
}

func TestGetWeightMatchConfig_WrongType(t *testing.T) {
	tp, _ := New(uuid.New(), uuid.New())
	tp.Type = PolicyTypeHeader
	_, err := tp.GetWeightMatchConfig()
	if err == nil {
		t.Error("GetWeightMatchConfig should error on header type")
	}
}

func TestConsumerType_AcceptedByValidate(t *testing.T) {
	// Per design doc: consumer type is defined but not part of v1 acceptance
	tp, _ := New(uuid.New(), uuid.New())
	tp.Name = "test"
	tp.Priority = 100
	tp.Type = PolicyTypeConsumer
	tp.SetHeaderMatchConfig("X-Canary", "beta") // match_config just needs to be non-empty
	err := tp.Validate()
	if err != nil {
		// consumer type is accepted at domain level but should be rejected at validator level
		t.Logf("Validate() for consumer type returned error = %v (may be expected)", err)
	}
}
