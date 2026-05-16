//go:build !integration
// +build !integration

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	pluginPkg "github.com/taichirain/portkey/internal/data/plugin"
	"github.com/taichirain/portkey/internal/data/proxy"
	"github.com/taichirain/portkey/internal/data/snapshot"
	"github.com/taichirain/portkey/internal/domain/plugin"
	"github.com/taichirain/portkey/internal/domain/route"
	"github.com/taichirain/portkey/internal/domain/service"
	"go.uber.org/zap"
)

type TestPlugin struct {
	name       string
	onRequest  func(ctx *pluginPkg.PluginContext) error
	onResponse func(ctx *pluginPkg.PluginContext, resp *http.Response) error
	onError    func(ctx *pluginPkg.PluginContext, err error) error
}

func (p *TestPlugin) Name() string {
	return p.name
}

func (p *TestPlugin) OnRequest(ctx *pluginPkg.PluginContext) error {
	if p.onRequest != nil {
		return p.onRequest(ctx)
	}
	return nil
}

func (p *TestPlugin) OnResponse(ctx *pluginPkg.PluginContext, resp *http.Response) error {
	if p.onResponse != nil {
		return p.onResponse(ctx, resp)
	}
	return nil
}

func (p *TestPlugin) OnError(ctx *pluginPkg.PluginContext, err error) error {
	if p.onError != nil {
		return p.onError(ctx, err)
	}
	return nil
}

type TestPluginFactory struct {
	name       string
	createFunc func(config map[string]interface{}) (pluginPkg.Plugin, error)
}

func (f *TestPluginFactory) Name() string {
	return f.name
}

func (f *TestPluginFactory) Create(config map[string]interface{}) (pluginPkg.Plugin, error) {
	if f.createFunc != nil {
		return f.createFunc(config)
	}
	return &TestPlugin{name: f.name}, nil
}

func newTestProxyWithPlugins(t *testing.T, snap *snapshot.ConfigSnapshot, factories ...pluginPkg.PluginFactory) *proxy.Proxy {
	t.Helper()
	logger, _ := zap.NewDevelopment()
	p := proxy.NewProxy(logger)

	for _, factory := range factories {
		p.PluginRegistry().Register(factory)
	}

	if snap != nil {
		p.UpdateSnapshot(snap)
	}
	return p
}

func TestM5_Plugin_OnRequest_Executed(t *testing.T) {
	var onRequestCalled int32

	testFactory := &TestPluginFactory{
		name: "test-plugin",
		createFunc: func(config map[string]interface{}) (pluginPkg.Plugin, error) {
			return &TestPlugin{
				name: "test-plugin",
				onRequest: func(ctx *pluginPkg.PluginContext) error {
					atomic.AddInt32(&onRequestCalled, 1)
					return nil
				},
			}, nil
		},
	}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
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

	testPlugin, _ := plugin.New("test-plugin", map[string]interface{}{"key": "value"})
	snap.AddPlugin(testPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithPlugins(t, snap, testFactory)

	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if atomic.LoadInt32(&onRequestCalled) != 1 {
		t.Errorf("Expected OnRequest to be called once, got %d", onRequestCalled)
	}
}

func TestM5_Plugin_OnResponse_Executed(t *testing.T) {
	var onResponseCalled int32

	testFactory := &TestPluginFactory{
		name: "response-plugin",
		createFunc: func(config map[string]interface{}) (pluginPkg.Plugin, error) {
			return &TestPlugin{
				name: "response-plugin",
				onResponse: func(ctx *pluginPkg.PluginContext, resp *http.Response) error {
					atomic.AddInt32(&onResponseCalled, 1)
					resp.Header.Set("X-Plugin-Modified", "true")
					return nil
				},
			}, nil
		},
	}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"message": "hello"})
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

	testPlugin, _ := plugin.New("response-plugin", map[string]interface{}{})
	snap.AddPlugin(testPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithPlugins(t, snap, testFactory)

	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if atomic.LoadInt32(&onResponseCalled) != 1 {
		t.Errorf("Expected OnResponse to be called once, got %d", onResponseCalled)
	}

	modifiedHeader := w.Header().Get("X-Plugin-Modified")
	if modifiedHeader != "true" {
		t.Errorf("Expected X-Plugin-Modified header to be 'true', got '%s'", modifiedHeader)
	}
}

func TestM5_Plugin_OnRequest_ShortCircuit(t *testing.T) {
	var backendCalled int32

	testFactory := &TestPluginFactory{
		name: "short-circuit-plugin",
		createFunc: func(config map[string]interface{}) (pluginPkg.Plugin, error) {
			return &TestPlugin{
				name: "short-circuit-plugin",
				onRequest: func(ctx *pluginPkg.PluginContext) error {
					ctx.ResponseWriter.WriteHeader(http.StatusForbidden)
					ctx.ResponseWriter.Write([]byte("Access Denied"))
					ctx.ShortCircuit()
					return nil
				},
			}, nil
		},
	}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&backendCalled, 1)
		w.WriteHeader(http.StatusOK)
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

	testPlugin, _ := plugin.New("short-circuit-plugin", map[string]interface{}{})
	snap.AddPlugin(testPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithPlugins(t, snap, testFactory)

	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden, got %d", w.Code)
	}

	if w.Body.String() != "Access Denied" {
		t.Errorf("Expected 'Access Denied', got '%s'", w.Body.String())
	}

	if atomic.LoadInt32(&backendCalled) != 0 {
		t.Errorf("Backend should not be called when plugin short-circuits, but was called %d times", backendCalled)
	}
}

func TestM5_Plugin_ScopeOverride_GlobalToService(t *testing.T) {
	testFactory := &TestPluginFactory{
		name: "override-plugin",
		createFunc: func(config map[string]interface{}) (pluginPkg.Plugin, error) {
			return &TestPlugin{name: "override-plugin"}, nil
		},
	}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
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

	globalPlugin, _ := plugin.New("override-plugin", map[string]interface{}{"value": "global"})
	snap.AddPlugin(globalPlugin)

	servicePlugin, _ := plugin.New("override-plugin", map[string]interface{}{"value": "service"})
	servicePlugin.ServiceID = &svc.ID
	snap.AddPlugin(servicePlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithPlugins(t, snap, testFactory)

	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	effectivePlugins := w.Header().Get("X-Effective-Plugins")
	if effectivePlugins != "override-plugin:service" {
		t.Errorf("Expected effective plugins 'override-plugin:service', got '%s'", effectivePlugins)
	}
}

func TestM5_Plugin_ScopeOverride_RouteToConsumer(t *testing.T) {
	testFactory := &TestPluginFactory{
		name: "multi-scope-plugin",
		createFunc: func(config map[string]interface{}) (pluginPkg.Plugin, error) {
			return &TestPlugin{name: "multi-scope-plugin"}, nil
		},
	}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
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

	globalPlugin, _ := plugin.New("multi-scope-plugin", map[string]interface{}{"value": "global"})
	snap.AddPlugin(globalPlugin)

	servicePlugin, _ := plugin.New("multi-scope-plugin", map[string]interface{}{"value": "service"})
	servicePlugin.ServiceID = &svc.ID
	snap.AddPlugin(servicePlugin)

	routePlugin, _ := plugin.New("multi-scope-plugin", map[string]interface{}{"value": "route"})
	routePlugin.RouteID = &r.ID
	snap.AddPlugin(routePlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithPlugins(t, snap, testFactory)

	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	effectivePlugins := w.Header().Get("X-Effective-Plugins")
	if effectivePlugins != "multi-scope-plugin:route" {
		t.Errorf("Expected effective plugins 'multi-scope-plugin:route', got '%s'", effectivePlugins)
	}
}

func TestM5_Plugin_MultiplePlugins_ExecutionOrder(t *testing.T) {
	var executionOrder []string

	createFactory := func(name string) *TestPluginFactory {
		return &TestPluginFactory{
			name: name,
			createFunc: func(config map[string]interface{}) (pluginPkg.Plugin, error) {
				return &TestPlugin{
					name: name,
					onRequest: func(ctx *pluginPkg.PluginContext) error {
						executionOrder = append(executionOrder, name+"-request")
						return nil
					},
					onResponse: func(ctx *pluginPkg.PluginContext, resp *http.Response) error {
						executionOrder = append(executionOrder, name+"-response")
						return nil
					},
				}, nil
			},
		}
	}

	factory1 := createFactory("plugin-a")
	factory2 := createFactory("plugin-b")

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
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

	pluginA, _ := plugin.New("plugin-a", map[string]interface{}{})
	snap.AddPlugin(pluginA)

	pluginB, _ := plugin.New("plugin-b", map[string]interface{}{})
	snap.AddPlugin(pluginB)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	executionOrder = make([]string, 0)
	p := newTestProxyWithPlugins(t, snap, factory1, factory2)

	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	expectedOrder := []string{"plugin-a-request", "plugin-b-request", "plugin-b-response", "plugin-a-response"}
	if len(executionOrder) != len(expectedOrder) {
		t.Errorf("Expected %d executions, got %d: %v", len(expectedOrder), len(executionOrder), executionOrder)
	} else {
		for i, expected := range expectedOrder {
			if executionOrder[i] != expected {
				t.Errorf("Expected execution[%d] = '%s', got '%s'", i, expected, executionOrder[i])
			}
		}
	}
}

func TestM5_Plugin_OnError_Executed(t *testing.T) {
	var onErrorCalled int32
	var capturedError error

	testFactory := &TestPluginFactory{
		name: "error-plugin",
		createFunc: func(config map[string]interface{}) (pluginPkg.Plugin, error) {
			return &TestPlugin{
				name: "error-plugin",
				onError: func(ctx *pluginPkg.PluginContext, err error) error {
					atomic.AddInt32(&onErrorCalled, 1)
					capturedError = err
					return nil
				},
			}, nil
		},
	}

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("test-svc")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = 19999
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/test")
	r.AddMethod("GET")
	snap.AddRoute(r)

	testPlugin, _ := plugin.New("error-plugin", map[string]interface{}{})
	snap.AddPlugin(testPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithPlugins(t, snap, testFactory)

	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("Expected 502 BadGateway, got %d", w.Code)
	}

	if atomic.LoadInt32(&onErrorCalled) != 1 {
		t.Errorf("Expected OnError to be called once, got %d", onErrorCalled)
	}

	if capturedError == nil {
		t.Error("Expected capturedError to be set, but it was nil")
	}
}

func TestM5_EffectivePlugins_Header(t *testing.T) {
	testFactory := &TestPluginFactory{
		name: "test-plugin",
		createFunc: func(config map[string]interface{}) (pluginPkg.Plugin, error) {
			return &TestPlugin{name: "test-plugin"}, nil
		},
	}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
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

	plugin1, _ := plugin.New("test-plugin", map[string]interface{}{})
	snap.AddPlugin(plugin1)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithPlugins(t, snap, testFactory)

	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	effectivePlugins := w.Header().Get("X-Effective-Plugins")
	if effectivePlugins != "test-plugin:global" {
		t.Errorf("Expected 'test-plugin:global', got '%s'", effectivePlugins)
	}
}

func TestM5_Plugin_Disabled_NotExecuted(t *testing.T) {
	var onRequestCalled int32

	testFactory := &TestPluginFactory{
		name: "disabled-plugin",
		createFunc: func(config map[string]interface{}) (pluginPkg.Plugin, error) {
			return &TestPlugin{
				name: "disabled-plugin",
				onRequest: func(ctx *pluginPkg.PluginContext) error {
					atomic.AddInt32(&onRequestCalled, 1)
					return nil
				},
			}, nil
		},
	}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
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

	disabledPlugin, _ := plugin.New("disabled-plugin", map[string]interface{}{})
	disabledPlugin.Enabled = false
	snap.AddPlugin(disabledPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p := newTestProxyWithPlugins(t, snap, testFactory)

	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	if atomic.LoadInt32(&onRequestCalled) != 0 {
		t.Errorf("Disabled plugin should not be executed, but OnRequest was called %d times", onRequestCalled)
	}
}

// --- Supplementary tests ---

func TestM5_Plugin_OnRequest_Error_Triggers500(t *testing.T) {
	var onErrorCalled int32

	testFactory := &TestPluginFactory{
		name: "error-on-request",
		createFunc: func(config map[string]interface{}) (pluginPkg.Plugin, error) {
			return &TestPlugin{
				name: "error-on-request",
				onRequest: func(ctx *pluginPkg.PluginContext) error {
					return fmt.Errorf("auth failed")
				},
				onError: func(ctx *pluginPkg.PluginContext, err error) error {
					atomic.AddInt32(&onErrorCalled, 1)
					return nil
				},
			}, nil
		},
	}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
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

	p, _ := plugin.New("error-on-request", map[string]interface{}{})
	snap.AddPlugin(p)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	proxy := newTestProxyWithPlugins(t, snap, testFactory)

	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 when OnRequest returns error, got %d", w.Code)
	}
	if atomic.LoadInt32(&onErrorCalled) != 1 {
		t.Errorf("Expected OnError to be called once, got %d", onErrorCalled)
	}
}

func TestM5_Plugin_ConfigPassedToFactory(t *testing.T) {
	var receivedConfig map[string]interface{}

	testFactory := &TestPluginFactory{
		name: "config-plugin",
		createFunc: func(config map[string]interface{}) (pluginPkg.Plugin, error) {
			receivedConfig = config
			return &TestPlugin{name: "config-plugin"}, nil
		},
	}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
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

	cfg := map[string]interface{}{"rps": 100, "burst": 200}
	p, _ := plugin.New("config-plugin", cfg)
	snap.AddPlugin(p)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	proxy := newTestProxyWithPlugins(t, snap, testFactory)

	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	if receivedConfig == nil {
		t.Fatal("Expected factory to receive config, got nil")
	}
	if receivedConfig["rps"] != 100 {
		t.Errorf("Expected rps=100, got %v", receivedConfig["rps"])
	}
}

func TestM5_Plugin_MixedEnabled_DisabledExecutionOrder(t *testing.T) {
	var executionOrder []string

	createFactory := func(name string, enabled bool) *TestPluginFactory {
		return &TestPluginFactory{
			name: name,
			createFunc: func(config map[string]interface{}) (pluginPkg.Plugin, error) {
				return &TestPlugin{
					name: name,
					onRequest: func(ctx *pluginPkg.PluginContext) error {
						executionOrder = append(executionOrder, name)
						return nil
					},
				}, nil
			},
		}
	}

	factoryA := createFactory("plugin-a", true)
	factoryB := createFactory("plugin-b", true)

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
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

	pA, _ := plugin.New("plugin-a", map[string]interface{}{})
	snap.AddPlugin(pA)

	// plugin-b is disabled
	pB, _ := plugin.New("plugin-b", map[string]interface{}{})
	pB.Enabled = false
	snap.AddPlugin(pB)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	executionOrder = make([]string, 0)
	proxy := newTestProxyWithPlugins(t, snap, factoryA, factoryB)

	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	if len(executionOrder) != 1 || executionOrder[0] != "plugin-a" {
		t.Errorf("Expected only plugin-a to execute, got %v", executionOrder)
	}
}

func TestM5_Plugin_NoPlugins_Success(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
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

	p := newTestProxyWithPlugins(t, snap)

	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 with no plugins, got %d", w.Code)
	}
	if w.Header().Get("X-Effective-Plugins") != "" {
		t.Errorf("Expected no X-Effective-Plugins header, got '%s'", w.Header().Get("X-Effective-Plugins"))
	}
}

func TestM5_Plugin_OnRequest_ModifiesRequestHeaders(t *testing.T) {
	var receivedAuth string

	testFactory := &TestPluginFactory{
		name: "header-plugin",
		createFunc: func(config map[string]interface{}) (pluginPkg.Plugin, error) {
			return &TestPlugin{
				name: "header-plugin",
				onRequest: func(ctx *pluginPkg.PluginContext) error {
					ctx.Request.Header.Set("X-Injected-By", "plugin")
					return nil
				},
			}, nil
		},
	}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("X-Injected-By")
		w.WriteHeader(http.StatusOK)
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

	p, _ := plugin.New("header-plugin", map[string]interface{}{})
	snap.AddPlugin(p)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	proxy := newTestProxyWithPlugins(t, snap, testFactory)

	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	proxy.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}
	if receivedAuth != "plugin" {
		t.Errorf("Expected backend to receive X-Injected-By=plugin, got '%s'", receivedAuth)
	}
}

func TestM5_Plugin_ConsumerScope_WithAuthPlugin(t *testing.T) {
	var consumerPluginCalled int32
	var globalPluginCalled int32

	authConsumerID := uuid.New()

	authFactory := &TestPluginFactory{
		name: "auth-plugin",
		createFunc: func(config map[string]interface{}) (pluginPkg.Plugin, error) {
			return &TestPlugin{
				name: "auth-plugin",
				onRequest: func(ctx *pluginPkg.PluginContext) error {
					ctx.SetConsumerID(authConsumerID)
					return nil
				},
			}, nil
		},
	}

	testFactory := &TestPluginFactory{
		name: "test-plugin",
		createFunc: func(config map[string]interface{}) (pluginPkg.Plugin, error) {
			scope := "unknown"
			if s, ok := config["scope"].(string); ok {
				scope = s
			}
			return &TestPlugin{
				name: "test-plugin",
				onRequest: func(ctx *pluginPkg.PluginContext) error {
					if scope == "global" {
						atomic.AddInt32(&globalPluginCalled, 1)
					} else if scope == "consumer" {
						atomic.AddInt32(&consumerPluginCalled, 1)
					}
					return nil
				},
			}, nil
		},
	}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
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

	authPlugin, _ := plugin.New("auth-plugin", map[string]interface{}{})
	snap.AddPlugin(authPlugin)

	globalPlugin, _ := plugin.New("test-plugin", map[string]interface{}{"scope": "global"})
	snap.AddPlugin(globalPlugin)

	consumerPlugin, _ := plugin.New("test-plugin", map[string]interface{}{"scope": "consumer"})
	consumerPlugin.ConsumerID = &authConsumerID
	snap.AddPlugin(consumerPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	consumerPluginCalled = 0
	globalPluginCalled = 0

	p := newTestProxyWithPlugins(t, snap, authFactory, testFactory)

	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	if atomic.LoadInt32(&globalPluginCalled) != 0 {
		t.Errorf("Global plugin should NOT be called when consumer-scoped version exists and consumerID matches")
	}

	if atomic.LoadInt32(&consumerPluginCalled) != 1 {
		t.Errorf("Consumer plugin should be called once, got %d", consumerPluginCalled)
	}

	effectivePlugins := w.Header().Get("X-Effective-Plugins")
	if effectivePlugins != "auth-plugin:global,test-plugin:consumer" {
		t.Errorf("Expected 'auth-plugin:global,test-plugin:consumer', got '%s'", effectivePlugins)
	}
}

func TestM5_Plugin_ConsumerScope_NoMatch(t *testing.T) {
	var consumerPluginCalled int32
	var globalPluginCalled int32

	authConsumerID := uuid.New()
	otherConsumerID := uuid.New()

	authFactory := &TestPluginFactory{
		name: "auth-plugin",
		createFunc: func(config map[string]interface{}) (pluginPkg.Plugin, error) {
			return &TestPlugin{
				name: "auth-plugin",
				onRequest: func(ctx *pluginPkg.PluginContext) error {
					ctx.SetConsumerID(authConsumerID)
					return nil
				},
			}, nil
		},
	}

	testFactory := &TestPluginFactory{
		name: "test-plugin",
		createFunc: func(config map[string]interface{}) (pluginPkg.Plugin, error) {
			scope := "unknown"
			if s, ok := config["scope"].(string); ok {
				scope = s
			}
			return &TestPlugin{
				name: "test-plugin",
				onRequest: func(ctx *pluginPkg.PluginContext) error {
					if scope == "global" {
						atomic.AddInt32(&globalPluginCalled, 1)
					} else if scope == "consumer" {
						atomic.AddInt32(&consumerPluginCalled, 1)
					}
					return nil
				},
			}, nil
		},
	}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
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

	authPlugin, _ := plugin.New("auth-plugin", map[string]interface{}{})
	snap.AddPlugin(authPlugin)

	globalPlugin, _ := plugin.New("test-plugin", map[string]interface{}{"scope": "global"})
	snap.AddPlugin(globalPlugin)

	consumerPlugin, _ := plugin.New("test-plugin", map[string]interface{}{"scope": "consumer"})
	consumerPlugin.ConsumerID = &otherConsumerID
	snap.AddPlugin(consumerPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	consumerPluginCalled = 0
	globalPluginCalled = 0

	p := newTestProxyWithPlugins(t, snap, authFactory, testFactory)

	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	if atomic.LoadInt32(&globalPluginCalled) != 1 {
		t.Errorf("Global plugin should be called when consumerID doesn't match, got %d", globalPluginCalled)
	}

	if atomic.LoadInt32(&consumerPluginCalled) != 0 {
		t.Errorf("Consumer plugin should NOT be called when consumerID doesn't match")
	}
}

func TestM5_Plugin_ConsumerScope_NoConsumerID(t *testing.T) {
	var consumerPluginCalled int32
	var globalPluginCalled int32

	consumerID := uuid.New()

	testFactory := &TestPluginFactory{
		name: "test-plugin",
		createFunc: func(config map[string]interface{}) (pluginPkg.Plugin, error) {
			scope := "unknown"
			if s, ok := config["scope"].(string); ok {
				scope = s
			}
			return &TestPlugin{
				name: "test-plugin",
				onRequest: func(ctx *pluginPkg.PluginContext) error {
					if scope == "global" {
						atomic.AddInt32(&globalPluginCalled, 1)
					} else if scope == "consumer" {
						atomic.AddInt32(&consumerPluginCalled, 1)
					}
					return nil
				},
			}, nil
		},
	}

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
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

	globalPlugin, _ := plugin.New("test-plugin", map[string]interface{}{"scope": "global"})
	snap.AddPlugin(globalPlugin)

	consumerPlugin, _ := plugin.New("test-plugin", map[string]interface{}{"scope": "consumer"})
	consumerPlugin.ConsumerID = &consumerID
	snap.AddPlugin(consumerPlugin)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	consumerPluginCalled = 0
	globalPluginCalled = 0

	p := newTestProxyWithPlugins(t, snap, testFactory)

	req := httptest.NewRequest("GET", "/test/hello", nil)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	if atomic.LoadInt32(&globalPluginCalled) != 1 {
		t.Errorf("Global plugin should be called when no consumerID is set, got %d", globalPluginCalled)
	}

	if atomic.LoadInt32(&consumerPluginCalled) != 0 {
		t.Errorf("Consumer plugin should NOT be called when no consumerID is set")
	}
}
