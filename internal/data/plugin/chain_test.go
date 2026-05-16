package plugin

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// --- PluginChain basic ---

func TestPluginChain_Len(t *testing.T) {
	chain := NewPluginChain()
	if chain.Len() != 0 {
		t.Errorf("expected 0, got %d", chain.Len())
	}
	chain.Add(&stubPlugin{name: "a"}, &PluginConfig{Name: "a", Enabled: true})
	if chain.Len() != 1 {
		t.Errorf("expected 1, got %d", chain.Len())
	}
}

func TestPluginChain_ExecuteOnRequest_SkipsDisabled(t *testing.T) {
	var called int
	chain := NewPluginChain()
	chain.Add(&callbackPlugin{
		name: "a",
		onRequest: func(ctx *PluginContext) error {
			called++
			return nil
		},
	}, &PluginConfig{Name: "a", Enabled: false})

	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ctx := NewPluginContext(w, r, "t1")

	if err := chain.ExecuteOnRequest(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called != 0 {
		t.Errorf("disabled plugin should not be called, got %d", called)
	}
}

func TestPluginChain_ExecuteOnRequest_Error(t *testing.T) {
	chain := NewPluginChain()
	chain.Add(&callbackPlugin{
		name: "bad",
		onRequest: func(ctx *PluginContext) error {
			return errors.New("request failed")
		},
	}, &PluginConfig{Name: "bad", Enabled: true})

	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ctx := NewPluginContext(w, r, "t1")

	err := chain.ExecuteOnRequest(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
	var pe *PluginError
	if !errors.As(err, &pe) {
		t.Errorf("expected PluginError, got %T", err)
	}
}

func TestPluginChain_ExecuteOnRequest_ShortCircuit(t *testing.T) {
	var secondCalled int
	chain := NewPluginChain()
	chain.Add(&callbackPlugin{
		name: "first",
		onRequest: func(ctx *PluginContext) error {
			ctx.ShortCircuit()
			return nil
		},
	}, &PluginConfig{Name: "first", Enabled: true})
	chain.Add(&callbackPlugin{
		name: "second",
		onRequest: func(ctx *PluginContext) error {
			secondCalled++
			return nil
		},
	}, &PluginConfig{Name: "second", Enabled: true})

	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ctx := NewPluginContext(w, r, "t1")

	if err := chain.ExecuteOnRequest(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if secondCalled != 0 {
		t.Errorf("second plugin should not be called after short-circuit, got %d", secondCalled)
	}
}

func TestPluginChain_ExecuteOnResponse_ReverseOrder(t *testing.T) {
	var order []string
	chain := NewPluginChain()
	chain.Add(&callbackPlugin{
		name: "a",
		onResponse: func(ctx *PluginContext, resp *http.Response) error {
			order = append(order, "a")
			return nil
		},
	}, &PluginConfig{Name: "a", Enabled: true})
	chain.Add(&callbackPlugin{
		name: "b",
		onResponse: func(ctx *PluginContext, resp *http.Response) error {
			order = append(order, "b")
			return nil
		},
	}, &PluginConfig{Name: "b", Enabled: true})

	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ctx := NewPluginContext(w, r, "t1")
	resp := &http.Response{Header: http.Header{}}

	if err := chain.ExecuteOnResponse(ctx, resp); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 2 || order[0] != "b" || order[1] != "a" {
		t.Errorf("expected reverse order [b,a], got %v", order)
	}
}

func TestPluginChain_ExecuteOnError_ReverseOrder(t *testing.T) {
	var order []string
	chain := NewPluginChain()
	chain.Add(&callbackPlugin{
		name: "a",
		onError: func(ctx *PluginContext, err error) error {
			order = append(order, "a")
			return nil
		},
	}, &PluginConfig{Name: "a", Enabled: true})
	chain.Add(&callbackPlugin{
		name: "b",
		onError: func(ctx *PluginContext, err error) error {
			order = append(order, "b")
			return nil
		},
	}, &PluginConfig{Name: "b", Enabled: true})

	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	ctx := NewPluginContext(w, r, "t1")

	if err := chain.ExecuteOnError(ctx, errors.New("upstream err")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 2 || order[0] != "b" || order[1] != "a" {
		t.Errorf("expected reverse order [b,a], got %v", order)
	}
}

// --- PluginChainBuilder ---

func TestPluginChainBuilder_ScopePrecedence(t *testing.T) {
	reg := NewPluginRegistry()
	reg.Register(&stubFactory{
		name: "auth",
		createFunc: func(config map[string]interface{}) (Plugin, error) {
			return &stubPlugin{name: "auth"}, nil
		},
	})

	builder := NewPluginChainBuilder(reg)
	global := []*PluginConfig{{Name: "auth", Config: map[string]interface{}{"level": "global"}, Enabled: true, Scope: ScopeGlobal}}
	service := []*PluginConfig{{Name: "auth", Config: map[string]interface{}{"level": "service"}, Enabled: true, Scope: ScopeService}}
	route := []*PluginConfig{{Name: "auth", Config: map[string]interface{}{"level": "route"}, Enabled: true, Scope: ScopeRoute}}

	// BuildForRequest deduplicates by name, keeping the last (highest) scope
	chain, effective, err := builder.BuildForRequest(global, service, route, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chain.Len() != 1 {
		t.Fatalf("expected 1 plugin (deduplicated), got %d", chain.Len())
	}
	if effective[0].SourceScope != ScopeRoute {
		t.Errorf("expected source scope Route, got %v", effective[0].SourceScope)
	}
	if effective[0].Config["level"] != "route" {
		t.Errorf("expected level=route, got %v", effective[0].Config["level"])
	}
}

func TestPluginChainBuilder_AllConsumers_RuntimeMatching(t *testing.T) {
	reg := NewPluginRegistry()
	reg.Register(&stubFactory{
		name: "auth",
		createFunc: func(config map[string]interface{}) (Plugin, error) {
			return &stubPlugin{name: "auth"}, nil
		},
	})

	builder := NewPluginChainBuilder(reg)
	cid := uuid.New()
	consumerPlugin := &PluginConfig{
		Name: "auth", Config: map[string]interface{}{"level": "consumer"},
		Enabled: true, Scope: ScopeConsumer, ConsumerID: &cid,
	}
	globalPlugin := &PluginConfig{
		Name: "auth", Config: map[string]interface{}{"level": "global"},
		Enabled: true, Scope: ScopeGlobal,
	}

	// BuildForRequestWithAllConsumers keeps all scope variants
	chain, _, err := builder.BuildForRequestWithAllConsumers(
		[]*PluginConfig{globalPlugin}, nil, nil,
		[]*PluginConfig{consumerPlugin},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Chain contains both variants; runtime picks the right one
	if chain.Len() != 2 {
		t.Fatalf("expected 2 plugins (global + consumer), got %d", chain.Len())
	}
}

func TestPluginChainBuilder_MissingFactory(t *testing.T) {
	reg := NewPluginRegistry()
	builder := NewPluginChainBuilder(reg)
	global := []*PluginConfig{{Name: "unknown", Enabled: true}}

	_, _, err := builder.BuildForRequest(global, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing factory")
	}
}

func TestPluginChainBuilder_MultiplePlugins_OrderByScope(t *testing.T) {
	reg := NewPluginRegistry()
	reg.Register(&stubFactory{name: "global-plugin", createFunc: func(c map[string]interface{}) (Plugin, error) {
		return &stubPlugin{name: "global-plugin"}, nil
	}})
	reg.Register(&stubFactory{name: "route-plugin", createFunc: func(c map[string]interface{}) (Plugin, error) {
		return &stubPlugin{name: "route-plugin"}, nil
	}})

	builder := NewPluginChainBuilder(reg)
	global := []*PluginConfig{{Name: "global-plugin", Enabled: true}}
	route := []*PluginConfig{{Name: "route-plugin", Enabled: true}}

	chain, effective, err := builder.BuildForRequest(global, nil, route, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chain.Len() != 2 {
		t.Fatalf("expected 2 plugins, got %d", chain.Len())
	}
	// Global should come before Route
	if effective[0].SourceScope != ScopeGlobal {
		t.Errorf("expected first plugin from global scope, got %v", effective[0].SourceScope)
	}
	if effective[1].SourceScope != ScopeRoute {
		t.Errorf("expected second plugin from route scope, got %v", effective[1].SourceScope)
	}
}

func TestPluginChainBuilder_BuildFromConfigs(t *testing.T) {
	reg := NewPluginRegistry()
	reg.Register(&stubFactory{name: "a", createFunc: func(c map[string]interface{}) (Plugin, error) {
		return &stubPlugin{name: "a"}, nil
	}})
	reg.Register(&stubFactory{name: "b", createFunc: func(c map[string]interface{}) (Plugin, error) {
		return &stubPlugin{name: "b"}, nil
	}})

	builder := NewPluginChainBuilder(reg)
	configs := []*PluginConfig{
		{Name: "a", Enabled: true},
		{Name: "b", Enabled: true},
	}
	chain, err := builder.BuildFromConfigs(configs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chain.Len() != 2 {
		t.Errorf("expected 2 plugins, got %d", chain.Len())
	}
}

// --- SnapshotPluginStore ---

func TestSnapshotPluginStore_Global(t *testing.T) {
	store := NewSnapshotPluginStore()
	store.AddGlobal(&PluginConfig{Name: "auth", Enabled: true})
	store.AddGlobal(&PluginConfig{Name: "cors", Enabled: true})

	got := store.GetGlobal()
	if len(got) != 2 {
		t.Errorf("expected 2 global plugins, got %d", len(got))
	}
}

func TestSnapshotPluginStore_Global_Overwrite(t *testing.T) {
	store := NewSnapshotPluginStore()
	store.AddGlobal(&PluginConfig{Name: "auth", Enabled: true, Config: map[string]interface{}{"v": 1}})
	store.AddGlobal(&PluginConfig{Name: "auth", Enabled: false, Config: map[string]interface{}{"v": 2}})

	got := store.GetGlobal()
	if len(got) != 1 {
		t.Fatalf("expected 1 (overwritten), got %d", len(got))
	}
	if got[0].Config["v"] != 2 {
		t.Errorf("expected overwritten config v=2, got %v", got[0].Config["v"])
	}
}

func TestSnapshotPluginStore_ServiceScoped(t *testing.T) {
	store := NewSnapshotPluginStore()
	svcID := uuid.New()
	store.AddService(svcID, &PluginConfig{Name: "rate-limit", Enabled: true})

	got := store.GetForService(svcID)
	if len(got) != 1 {
		t.Errorf("expected 1, got %d", len(got))
	}

	other := uuid.New()
	if empty := store.GetForService(other); empty != nil {
		t.Errorf("expected nil for unknown service, got %v", empty)
	}
}

func TestSnapshotPluginStore_RouteScoped(t *testing.T) {
	store := NewSnapshotPluginStore()
	routeID := uuid.New()
	store.AddRoute(routeID, &PluginConfig{Name: "auth", Enabled: true})

	got := store.GetForRoute(routeID)
	if len(got) != 1 {
		t.Errorf("expected 1, got %d", len(got))
	}
}

func TestSnapshotPluginStore_ConsumerScoped(t *testing.T) {
	store := NewSnapshotPluginStore()
	cid := uuid.New()
	store.AddConsumer(cid, &PluginConfig{Name: "quota", Enabled: true})

	got := store.GetForConsumer(cid)
	if len(got) != 1 {
		t.Errorf("expected 1, got %d", len(got))
	}
}

func TestSnapshotPluginStore_BuildChainForRequest(t *testing.T) {
	reg := NewPluginRegistry()
	reg.Register(&stubFactory{name: "auth", createFunc: func(c map[string]interface{}) (Plugin, error) {
		return &stubPlugin{name: "auth"}, nil
	}})
	reg.Register(&stubFactory{name: "rate-limit", createFunc: func(c map[string]interface{}) (Plugin, error) {
		return &stubPlugin{name: "rate-limit"}, nil
	}})

	store := NewSnapshotPluginStore()
	svcID := uuid.New()
	routeID := uuid.New()
	store.AddGlobal(&PluginConfig{Name: "auth", Enabled: true})
	store.AddService(svcID, &PluginConfig{Name: "rate-limit", Enabled: true})

	builder := NewPluginChainBuilder(reg)
	chain, effective, err := store.BuildChainForRequest(builder, svcID, routeID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chain.Len() != 2 {
		t.Errorf("expected 2 plugins, got %d", chain.Len())
	}
	if len(effective) != 2 {
		t.Errorf("expected 2 effective plugins, got %d", len(effective))
	}
}

func TestSnapshotPluginStore_BuildChainForRequest_WithConsumer(t *testing.T) {
	reg := NewPluginRegistry()
	reg.Register(&stubFactory{name: "quota", createFunc: func(c map[string]interface{}) (Plugin, error) {
		return &stubPlugin{name: "quota"}, nil
	}})

	store := NewSnapshotPluginStore()
	cid := uuid.New()
	store.AddConsumer(cid, &PluginConfig{Name: "quota", Enabled: true, Scope: ScopeConsumer, ConsumerID: &cid})

	builder := NewPluginChainBuilder(reg)
	chain, _, err := store.BuildChainForRequest(builder, uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if chain.Len() != 1 {
		t.Errorf("expected 1 consumer plugin, got %d", chain.Len())
	}
}

// --- helper ---

type callbackPlugin struct {
	name       string
	onRequest  func(ctx *PluginContext) error
	onResponse func(ctx *PluginContext, resp *http.Response) error
	onError    func(ctx *PluginContext, err error) error
}

func (p *callbackPlugin) Name() string { return p.name }
func (p *callbackPlugin) OnRequest(ctx *PluginContext) error {
	if p.onRequest != nil {
		return p.onRequest(ctx)
	}
	return nil
}
func (p *callbackPlugin) OnResponse(ctx *PluginContext, resp *http.Response) error {
	if p.onResponse != nil {
		return p.onResponse(ctx, resp)
	}
	return nil
}
func (p *callbackPlugin) OnError(ctx *PluginContext, err error) error {
	if p.onError != nil {
		return p.onError(ctx, err)
	}
	return nil
}
