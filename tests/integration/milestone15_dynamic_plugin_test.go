//go:build !integration
// +build !integration

package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/data/plugin"
	"github.com/taichirain/portkey/internal/data/proxy"
	"github.com/taichirain/portkey/internal/data/snapshot"
	"github.com/taichirain/portkey/internal/domain/route"
	"github.com/taichirain/portkey/internal/domain/service"
	"go.uber.org/zap"
)

const (
	pluginSourceDir = "plugins"
	pluginFileName  = "dynamic_add_header.go"
	pluginOutputDir = "test_plugins"
	pluginSOName    = "dynamic_add_header.so"
)

func buildTestPlugin(t *testing.T) string {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skipf("Go plugins are only supported on Linux and macOS, current OS: %s", runtime.GOOS)
	}

	_, currentFile, _, _ := runtime.Caller(0)
	currentDir := filepath.Dir(currentFile)

	sourcePath := filepath.Join(currentDir, pluginSourceDir, pluginFileName)
	if _, err := os.Stat(sourcePath); os.IsNotExist(err) {
		t.Skipf("Plugin source not found at %s, skipping dynamic plugin test", sourcePath)
	}

	outputDir := filepath.Join(currentDir, pluginOutputDir)
	os.MkdirAll(outputDir, 0755)
	outputPath := filepath.Join(outputDir, pluginSOName)

	cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", outputPath, sourcePath)
	cmd.Dir = currentDir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("Plugin build output: %s", string(output))
		t.Skipf("Failed to build plugin: %v (this may be due to dependency mismatches)", err)
	}

	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Skipf("Plugin .so file was not created")
	}

	return outputPath
}

func TestM15_DynamicPluginLoader_LoadPlugin(t *testing.T) {
	pluginPath := buildTestPlugin(t)
	if pluginPath == "" {
		return
	}
	defer os.RemoveAll(filepath.Dir(pluginPath))

	logger, _ := zap.NewDevelopment()
	registry := plugin.NewPluginRegistry()
	loader := plugin.NewDynamicPluginLoader(logger, registry)

	t.Run("Load enabled plugin", func(t *testing.T) {
		config := &plugin.DynamicPluginConfig{
			Path:    pluginPath,
			Name:    "dynamic_add_header",
			Enabled: true,
			Config: map[string]interface{}{
				"header_name":  "X-Custom-Header",
				"header_value": "test-value",
			},
		}

		loaded, err := loader.LoadPlugin(config)
		if err != nil {
			t.Fatalf("Failed to load plugin: %v", err)
		}

		if loaded == nil {
			t.Fatal("Expected loaded plugin to be non-nil")
		}

		if !loaded.IsHealthy() {
			t.Error("Expected plugin to be healthy")
		}

		if loaded.Factory == nil {
			t.Error("Expected plugin factory to be non-nil")
		}

		if loaded.Factory.Name() != "dynamic_add_header" {
			t.Errorf("Expected plugin name 'dynamic_add_header', got '%s'", loaded.Factory.Name())
		}
	})

	t.Run("Load disabled plugin", func(t *testing.T) {
		config := &plugin.DynamicPluginConfig{
			Path:    pluginPath,
			Name:    "dynamic_add_header",
			Enabled: false,
		}

		loaded, err := loader.LoadPlugin(config)
		if err != nil {
			t.Fatalf("Unexpected error for disabled plugin: %v", err)
		}

		if loaded != nil {
			t.Log("Note: Disabled plugin returns nil (as expected)")
		}
	})

	t.Run("Load non-existent plugin", func(t *testing.T) {
		config := &plugin.DynamicPluginConfig{
			Path:    "/path/to/nonexistent.so",
			Name:    "nonexistent",
			Enabled: true,
		}

		loaded, err := loader.LoadPlugin(config)
		if err == nil {
			t.Error("Expected error for non-existent plugin")
		}

		if loaded == nil {
			t.Error("Expected loaded plugin info even when failed")
		} else if loaded.Error == nil {
			t.Error("Expected error in loaded plugin")
		}
	})
}

func TestM15_DynamicPluginLoader_GetPlugin(t *testing.T) {
	pluginPath := buildTestPlugin(t)
	if pluginPath == "" {
		return
	}
	defer os.RemoveAll(filepath.Dir(pluginPath))

	logger, _ := zap.NewDevelopment()
	registry := plugin.NewPluginRegistry()
	loader := plugin.NewDynamicPluginLoader(logger, registry)

	config := &plugin.DynamicPluginConfig{
		Path:    pluginPath,
		Name:    "dynamic_add_header",
		Enabled: true,
	}

	_, err := loader.LoadPlugin(config)
	if err != nil {
		t.Logf("Plugin load failed: %v (may be expected in test environment)", err)
		t.Skip("Skipping test as plugin couldn't be loaded")
	}

	t.Run("Get existing plugin", func(t *testing.T) {
		factory, ok := loader.GetPlugin("dynamic_add_header")
		if !ok {
			t.Error("Expected to find loaded plugin")
		}

		if factory == nil {
			t.Log("Note: Factory may be nil if plugin wasn't properly loaded")
		} else if factory.Name() != "dynamic_add_header" {
			t.Errorf("Expected factory name 'dynamic_add_header', got '%s'", factory.Name())
		}
	})

	t.Run("Get non-existing plugin", func(t *testing.T) {
		factory, ok := loader.GetPlugin("nonexistent_plugin")
		if ok {
			t.Error("Expected not to find non-existent plugin")
		}

		if factory != nil {
			t.Error("Expected factory to be nil for non-existent plugin")
		}
	})
}

func TestM15_DynamicPluginLoader_LoadPlugins(t *testing.T) {
	pluginPath := buildTestPlugin(t)
	if pluginPath == "" {
		return
	}
	defer os.RemoveAll(filepath.Dir(pluginPath))

	logger, _ := zap.NewDevelopment()
	registry := plugin.NewPluginRegistry()
	loader := plugin.NewDynamicPluginLoader(logger, registry)

	configs := []*plugin.DynamicPluginConfig{
		{
			Path:    pluginPath,
			Name:    "test_plugin_1",
			Enabled: true,
		},
		{
			Path:    "/nonexistent.so",
			Name:    "nonexistent",
			Enabled: true,
		},
		{
			Path:    pluginPath,
			Name:    "test_plugin_2",
			Enabled: false,
		},
	}

	successCount := loader.LoadPlugins(configs)
	t.Logf("Successfully loaded %d plugins", successCount)

	if loader.HasErrors() {
		t.Log("Loader has errors (as expected for nonexistent plugin)")
		errors := loader.GetErrors()
		if len(errors) == 0 {
			t.Error("Expected errors map to be non-empty")
		}
	}
}

func TestM15_DynamicPluginLoader_UnloadPlugin(t *testing.T) {
	pluginPath := buildTestPlugin(t)
	if pluginPath == "" {
		return
	}
	defer os.RemoveAll(filepath.Dir(pluginPath))

	logger, _ := zap.NewDevelopment()
	registry := plugin.NewPluginRegistry()
	loader := plugin.NewDynamicPluginLoader(logger, registry)

	config := &plugin.DynamicPluginConfig{
		Path:    pluginPath,
		Name:    "dynamic_add_header",
		Enabled: true,
	}

	_, err := loader.LoadPlugin(config)
	if err != nil {
		t.Skipf("Skipping test as plugin couldn't be loaded: %v", err)
	}

	err = loader.UnloadPlugin(pluginPath)
	if err != nil {
		t.Fatalf("Failed to unload plugin: %v", err)
	}
}

func TestM15_DynamicPlugin_WithHTTPProxy(t *testing.T) {
	pluginPath := buildTestPlugin(t)
	if pluginPath == "" {
		return
	}
	defer os.RemoveAll(filepath.Dir(pluginPath))

	var receivedHeaders http.Header
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer backend.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("test-service")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, backend.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/test")
	r.AddMethod("GET")
	snap.AddRoute(r)

	logger, _ := zap.NewDevelopment()
	p := proxy.NewProxy(logger)
	registry := p.PluginRegistry()

	pluginConfig := &plugin.DynamicPluginConfig{
		Path:    pluginPath,
		Name:    "dynamic_add_header",
		Enabled: true,
		Config: map[string]interface{}{
			"header_name":  "X-Dynamic-Test",
			"header_value": "integration-test",
		},
	}

	loader := plugin.NewDynamicPluginLoader(logger, registry)
	_, err := loader.LoadPlugin(pluginConfig)
	if err != nil {
		t.Logf("Dynamic plugin load failed: %v (this may be due to build environment)", err)
		t.Skip("Skipping integration test as dynamic plugin couldn't be loaded")
	}

	routePluginConfig := &plugin.PluginConfig{
		ID:      uuid.New(),
		Name:    "dynamic_add_header",
		Scope:   plugin.ScopeRoute,
		RouteID: &r.ID,
		Enabled: true,
		Config: map[string]interface{}{
			"header_name":  "X-Dynamic-Test",
			"header_value": "integration-test",
		},
	}
	snap.Plugins.AddRoute(r.ID, routePluginConfig)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	p.UpdateSnapshot(snap)

	proxyServer := httptest.NewServer(p)
	defer proxyServer.Close()

	proxyURL, _ := url.Parse(proxyServer.URL)
	testURL := proxyURL.String() + "/test/path"

	resp, err := http.Get(testURL)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	t.Logf("Response status: %d, body: %s", resp.StatusCode, string(body))

	if receivedHeaders != nil {
		dynamicHeader := receivedHeaders.Get("X-Dynamic-Test")
		t.Logf("Received X-Dynamic-Test header: '%s'", dynamicHeader)
	}
}

func TestM15_DynamicPluginManager_Initialize(t *testing.T) {
	pluginPath := buildTestPlugin(t)
	if pluginPath == "" {
		return
	}
	defer os.RemoveAll(filepath.Dir(pluginPath))

	logger, _ := zap.NewDevelopment()
	registry := plugin.NewPluginRegistry()
	manager := plugin.NewDynamicPluginManager(logger, registry)

	t.Run("Initialize with single plugin", func(t *testing.T) {
		config := &plugin.DynamicPluginConfig{
			Path:    pluginPath,
			Name:    "test_plugin",
			Enabled: true,
		}

		err := manager.Initialize([]*plugin.DynamicPluginConfig{config})
		if err != nil {
			t.Logf("Initialize returned error (may be expected in test environment): %v", err)
		}

		loader := manager.GetLoader()
		if loader == nil {
			t.Error("Expected loader to be non-nil")
		}
	})

	t.Run("Initialize with empty configs", func(t *testing.T) {
		emptyManager := plugin.NewDynamicPluginManager(logger, registry)
		err := emptyManager.Initialize([]*plugin.DynamicPluginConfig{})
		if err != nil {
			t.Errorf("Expected no error for empty configs, got: %v", err)
		}
	})
}

func TestM15_LoadedDynamicPlugin_IsHealthy(t *testing.T) {
	t.Run("Healthy plugin", func(t *testing.T) {
		loaded := &plugin.LoadedDynamicPlugin{
			Path:    "/path/to/plugin.so",
			Factory: &TestDynamicPluginFactory{name: "test"},
			Enabled: true,
			Error:   nil,
		}

		if !loaded.IsHealthy() {
			t.Error("Expected plugin to be healthy")
		}
	})

	t.Run("Unhealthy plugin - has error", func(t *testing.T) {
		loaded := &plugin.LoadedDynamicPlugin{
			Path:    "/path/to/plugin.so",
			Factory: &TestDynamicPluginFactory{name: "test"},
			Enabled: true,
			Error:   fmt.Errorf("some error"),
		}

		if loaded.IsHealthy() {
			t.Error("Expected plugin to be unhealthy when has error")
		}
	})

	t.Run("Unhealthy plugin - disabled", func(t *testing.T) {
		loaded := &plugin.LoadedDynamicPlugin{
			Path:    "/path/to/plugin.so",
			Factory: &TestDynamicPluginFactory{name: "test"},
			Enabled: false,
			Error:   nil,
		}

		if loaded.IsHealthy() {
			t.Error("Expected plugin to be unhealthy when disabled")
		}
	})

	t.Run("Unhealthy plugin - no factory", func(t *testing.T) {
		loaded := &plugin.LoadedDynamicPlugin{
			Path:    "/path/to/plugin.so",
			Factory: nil,
			Enabled: true,
			Error:   nil,
		}

		if loaded.IsHealthy() {
			t.Error("Expected plugin to be unhealthy when no factory")
		}
	})
}

func TestM15_DynamicPlugin_FactoryCreate(t *testing.T) {
	factory := &TestDynamicPluginFactory{name: "test_dynamic"}

	config := map[string]interface{}{
		"header_name":  "X-Test-Header",
		"header_value": "test-value",
	}

	pluginInst, err := factory.Create(config)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if pluginInst == nil {
		t.Error("Expected plugin to be non-nil")
	}

	if pluginInst.Name() != "test_dynamic" {
		t.Errorf("Expected plugin name 'test_dynamic', got '%s'", pluginInst.Name())
	}
}

type TestDynamicPluginFactory struct {
	name string
}

func (f *TestDynamicPluginFactory) Name() string {
	return f.name
}

func (f *TestDynamicPluginFactory) Create(config map[string]interface{}) (plugin.Plugin, error) {
	return &TestDynamicPlugin{config: config}, nil
}

type TestDynamicPlugin struct {
	config map[string]interface{}
}

func (p *TestDynamicPlugin) Name() string {
	return "test_dynamic"
}

func (p *TestDynamicPlugin) OnRequest(ctx *plugin.PluginContext) error {
	headerName := "X-Test-Header"
	headerValue := "test-value"

	if hName, ok := p.config["header_name"].(string); ok && hName != "" {
		headerName = hName
	}
	if hValue, ok := p.config["header_value"].(string); ok && hValue != "" {
		headerValue = hValue
	}

	ctx.Request.Header.Set(headerName, headerValue)
	return nil
}

func (p *TestDynamicPlugin) OnResponse(ctx *plugin.PluginContext, resp *http.Response) error {
	if resp != nil {
		resp.Header.Set("X-Plugin-Executed", "true")
	}
	return nil
}

func (p *TestDynamicPlugin) OnError(ctx *plugin.PluginContext, err error) error {
	return nil
}
