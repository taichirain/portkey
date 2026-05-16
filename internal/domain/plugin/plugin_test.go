package plugin

import (
	"testing"

	"github.com/google/uuid"
)

func uuidNew() uuid.UUID { return uuid.New() }

func TestNew_ValidInput(t *testing.T) {
	p, err := New("rate-limit", map[string]interface{}{"rps": 100})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name != "rate-limit" {
		t.Errorf("expected name 'rate-limit', got '%s'", p.Name)
	}
	if !p.Enabled {
		t.Error("expected plugin to be enabled by default")
	}
	if p.RunOn != RunOnFirst {
		t.Errorf("expected RunOn 'first', got '%s'", p.RunOn)
	}
	if len(p.Protocols) != 2 {
		t.Errorf("expected 2 protocols, got %d", len(p.Protocols))
	}
}

func TestNew_EmptyName(t *testing.T) {
	_, err := New("", map[string]interface{}{})
	if err != ErrPluginNameRequired {
		t.Errorf("expected ErrPluginNameRequired, got %v", err)
	}
}

func TestNew_WhitespaceName(t *testing.T) {
	_, err := New("   ", map[string]interface{}{})
	if err != ErrPluginNameRequired {
		t.Errorf("expected ErrPluginNameRequired, got %v", err)
	}
}

func TestNew_NilConfig(t *testing.T) {
	_, err := New("test", nil)
	if err != ErrPluginConfigRequired {
		t.Errorf("expected ErrPluginConfigRequired, got %v", err)
	}
}

func TestNew_EmptyConfig(t *testing.T) {
	p, err := New("test", map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Config == nil {
		t.Error("expected non-nil config")
	}
}

func TestValidate_Valid(t *testing.T) {
	p, _ := New("test", map[string]interface{}{})
	if err := p.Validate(); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidate_EmptyName(t *testing.T) {
	p, _ := New("test", map[string]interface{}{})
	p.Name = ""
	if err := p.Validate(); err != ErrPluginNameRequired {
		t.Errorf("expected ErrPluginNameRequired, got %v", err)
	}
}

func TestValidate_NilConfig(t *testing.T) {
	p, _ := New("test", map[string]interface{}{})
	p.Config = nil
	if err := p.Validate(); err != ErrPluginConfigRequired {
		t.Errorf("expected ErrPluginConfigRequired, got %v", err)
	}
}

func TestValidate_InvalidRunOn(t *testing.T) {
	p, _ := New("test", map[string]interface{}{})
	p.RunOn = "invalid"
	if err := p.Validate(); err != ErrPluginInvalidRunOn {
		t.Errorf("expected ErrPluginInvalidRunOn, got %v", err)
	}
}

func TestValidate_ValidRunOnValues(t *testing.T) {
	validRunOns := []RunOn{RunOnFirst, RunOnSecond, RunOnLast, RunOnAll, ""}
	for _, ro := range validRunOns {
		p, _ := New("test", map[string]interface{}{})
		p.RunOn = ro
		if err := p.Validate(); err != nil {
			t.Errorf("RunOn '%s' should be valid, got error: %v", ro, err)
		}
	}
}

func TestEnableDisable(t *testing.T) {
	p, _ := New("test", map[string]interface{}{})
	if !p.Enabled {
		t.Fatal("expected initially enabled")
	}
	p.Disable()
	if p.Enabled {
		t.Error("expected disabled after Disable()")
	}
	p.Enable()
	if !p.Enabled {
		t.Error("expected enabled after Enable()")
	}
}

func TestAddTag_NoDuplicate(t *testing.T) {
	p, _ := New("test", map[string]interface{}{})
	p.AddTag("auth")
	p.AddTag("rate-limit")
	p.AddTag("auth") // duplicate
	if len(p.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d: %v", len(p.Tags), p.Tags)
	}
}

func TestScope_Priority(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(p *Plugin)
		expected Scope
	}{
		{"global", func(p *Plugin) {}, ScopeGlobal},
		{"service", func(p *Plugin) { sid := uuidNew(); p.ServiceID = &sid }, ScopeService},
		{"route", func(p *Plugin) { rid := uuidNew(); p.RouteID = &rid }, ScopeRoute},
		{"route over service", func(p *Plugin) { sid := uuidNew(); rid := uuidNew(); p.ServiceID = &sid; p.RouteID = &rid }, ScopeRoute},
		{"consumer", func(p *Plugin) { cid := uuidNew(); p.ConsumerID = &cid }, ScopeConsumer},
		{"consumer over route", func(p *Plugin) { rid := uuidNew(); cid := uuidNew(); p.RouteID = &rid; p.ConsumerID = &cid }, ScopeConsumer},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _ := New("test", map[string]interface{}{})
			tt.setup(p)
			if got := p.Scope(); got != tt.expected {
				t.Errorf("expected scope %d, got %d", tt.expected, got)
			}
		})
	}
}

func TestIsGlobal(t *testing.T) {
	p, _ := New("test", map[string]interface{}{})
	if !p.IsGlobal() {
		t.Error("expected IsGlobal=true with no scope IDs")
	}
	sid := uuidNew()
	p.ServiceID = &sid
	if p.IsGlobal() {
		t.Error("expected IsGlobal=false with ServiceID set")
	}
}

func TestConfigJSON_Roundtrip(t *testing.T) {
	original := map[string]interface{}{
		"rps":    100,
		"burst":  200,
		"prefix": "rate",
	}
	p, _ := New("test", original)

	data, err := p.ConfigJSON()
	if err != nil {
		t.Fatalf("ConfigJSON error: %v", err)
	}

	p2, _ := New("test2", map[string]interface{}{})
	if err := p2.SetConfigFromJSON(data); err != nil {
		t.Fatalf("SetConfigFromJSON error: %v", err)
	}

	if p2.Config["rps"] != 100.0 { // JSON numbers are float64
		t.Errorf("expected rps=100, got %v", p2.Config["rps"])
	}
}

func TestSetConfigFromJSON_Invalid(t *testing.T) {
	p, _ := New("test", map[string]interface{}{})
	err := p.SetConfigFromJSON([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
