package plugin

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// --- Scope.String() ---

func TestScope_String(t *testing.T) {
	tests := []struct {
		scope    Scope
		expected string
	}{
		{ScopeGlobal, "global"},
		{ScopeService, "service"},
		{ScopeRoute, "route"},
		{ScopeConsumer, "consumer"},
		{Scope(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.scope.String(); got != tt.expected {
			t.Errorf("Scope(%d).String() = %q, want %q", tt.scope, got, tt.expected)
		}
	}
}

// --- PluginContext ---

func TestPluginContext_Attributes(t *testing.T) {
	r := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	ctx := NewPluginContext(w, r, "trace-1")

	ctx.SetAttribute("key1", "value1")
	ctx.SetAttribute("key2", 42)

	if v := ctx.GetAttribute("key1"); v != "value1" {
		t.Errorf("expected 'value1', got %v", v)
	}
	if v := ctx.GetAttribute("key2"); v != 42 {
		t.Errorf("expected 42, got %v", v)
	}
	if v := ctx.GetAttribute("missing"); v != nil {
		t.Errorf("expected nil for missing key, got %v", v)
	}
}

func TestPluginContext_ShortCircuit(t *testing.T) {
	r := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	ctx := NewPluginContext(w, r, "trace-1")

	if ctx.IsShortCircuited() {
		t.Error("expected not short-circuited initially")
	}
	ctx.ShortCircuit()
	if !ctx.IsShortCircuited() {
		t.Error("expected short-circuited after ShortCircuit()")
	}
}

func TestPluginContext_SetMatchedRoute(t *testing.T) {
	r := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	ctx := NewPluginContext(w, r, "trace-1")

	routeID := uuid.New()
	serviceID := uuid.New()
	upstreamID := uuid.New()
	ctx.SetMatchedRoute(routeID, serviceID, upstreamID)

	if ctx.MatchedRoute == nil {
		t.Fatal("expected MatchedRoute to be set")
	}
	if ctx.MatchedRoute.RouteID != routeID {
		t.Errorf("RouteID mismatch")
	}
	if ctx.MatchedRoute.ServiceID != serviceID {
		t.Errorf("ServiceID mismatch")
	}
}

func TestPluginContext_SetConsumerID(t *testing.T) {
	r := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	ctx := NewPluginContext(w, r, "trace-1")

	if ctx.ConsumerID != nil {
		t.Error("expected nil ConsumerID initially")
	}
	cid := uuid.New()
	ctx.SetConsumerID(cid)
	if ctx.ConsumerID == nil || *ctx.ConsumerID != cid {
		t.Errorf("ConsumerID mismatch")
	}
}

// --- PluginRegistry ---

type stubFactory struct {
	name       string
	createFunc func(config map[string]interface{}) (Plugin, error)
}

func (f *stubFactory) Name() string { return f.name }
func (f *stubFactory) Create(config map[string]interface{}) (Plugin, error) {
	if f.createFunc != nil {
		return f.createFunc(config)
	}
	return nil, nil
}

type stubPlugin struct{ name string }

func (p *stubPlugin) Name() string                                                { return p.name }
func (p *stubPlugin) OnRequest(ctx *PluginContext) error                           { return nil }
func (p *stubPlugin) OnResponse(ctx *PluginContext, resp *http.Response) error     { return nil }
func (p *stubPlugin) OnError(ctx *PluginContext, err error) error                  { return nil }

func TestPluginRegistry_RegisterAndGet(t *testing.T) {
	reg := NewPluginRegistry()
	f := &stubFactory{name: "auth"}
	reg.Register(f)

	got, ok := reg.Get("auth")
	if !ok {
		t.Fatal("expected to find 'auth' factory")
	}
	if got.Name() != "auth" {
		t.Errorf("expected 'auth', got '%s'", got.Name())
	}
}

func TestPluginRegistry_GetMissing(t *testing.T) {
	reg := NewPluginRegistry()
	_, ok := reg.Get("nonexistent")
	if ok {
		t.Error("expected not found")
	}
}

func TestPluginRegistry_CreateInstance(t *testing.T) {
	reg := NewPluginRegistry()
	created := false
	reg.Register(&stubFactory{
		name: "test",
		createFunc: func(config map[string]interface{}) (Plugin, error) {
			created = true
			return &stubPlugin{name: "test"}, nil
		},
	})

	p, err := reg.CreateInstance("test", map[string]interface{}{"k": "v"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created {
		t.Error("expected factory.Create to be called")
	}
	if p.Name() != "test" {
		t.Errorf("expected plugin name 'test', got '%s'", p.Name())
	}
}

func TestPluginRegistry_CreateInstance_NotFound(t *testing.T) {
	reg := NewPluginRegistry()
	_, err := reg.CreateInstance("missing", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var pluginErr *PluginError
	if !errors.As(err, &pluginErr) {
		t.Errorf("expected *PluginError, got %T", err)
	}
}

func TestPluginRegistry_CreateInstance_FactoryError(t *testing.T) {
	reg := NewPluginRegistry()
	reg.Register(&stubFactory{
		name: "bad",
		createFunc: func(config map[string]interface{}) (Plugin, error) {
			return nil, errors.New("factory failed")
		},
	})

	_, err := reg.CreateInstance("bad", nil)
	if err == nil {
		t.Fatal("expected error from factory")
	}
	if err.Error() != "factory failed" {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- PluginError ---

func TestPluginError_WithPluginName(t *testing.T) {
	e := NewPluginError("auth", "invalid config", errors.New("bad value"))
	expected := "plugin [auth]: invalid config"
	if e.Error() != expected {
		t.Errorf("expected %q, got %q", expected, e.Error())
	}
	if e.Unwrap().Error() != "bad value" {
		t.Errorf("Unwrap mismatch")
	}
}

func TestPluginError_WithoutPluginName(t *testing.T) {
	e := &PluginError{Message: "not found"}
	if e.Error() != "not found" {
		t.Errorf("expected 'not found', got %q", e.Error())
	}
	if e.Unwrap() != nil {
		t.Error("expected nil Unwrap")
	}
}
