package plugin

import (
	"net/http"
	"testing"

	"go.uber.org/zap"
)

type testPluginFactory struct {
	name string
}

func (f *testPluginFactory) Name() string {
	return f.name
}

func (f *testPluginFactory) Create(config map[string]interface{}) (Plugin, error) {
	return &testPlugin{name: f.name}, nil
}

type testPlugin struct {
	name string
}

func (p *testPlugin) Name() string {
	return p.name
}

func (p *testPlugin) OnRequest(ctx *PluginContext) error {
	return nil
}

func (p *testPlugin) OnResponse(ctx *PluginContext, resp *http.Response) error {
	return nil
}

func (p *testPlugin) OnError(ctx *PluginContext, err error) error {
	return nil
}

func TestDynamicPluginLoader_LoadPlugin_Disabled(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	registry := NewPluginRegistry()
	loader := NewDynamicPluginLoader(logger, registry)

	config := &DynamicPluginConfig{
		Path:    "/nonexistent/path/plugin.so",
		Name:    "test-plugin",
		Enabled: false,
	}

	loaded, err := loader.LoadPlugin(config)
	if err != nil {
		t.Errorf("Expected no error for disabled plugin, got %v", err)
	}
	if loaded != nil {
		t.Errorf("Expected nil for disabled plugin, got %v", loaded)
	}
}

func TestDynamicPluginLoader_LoadPlugin_FileNotFound(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	registry := NewPluginRegistry()
	loader := NewDynamicPluginLoader(logger, registry)

	config := &DynamicPluginConfig{
		Path:    "/nonexistent/path/plugin.so",
		Name:    "test-plugin",
		Enabled: true,
	}

	loaded, err := loader.LoadPlugin(config)
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
	if loaded == nil {
		t.Error("Expected loaded plugin to be non-nil even when file not found")
	}
	if loaded.Error == nil {
		t.Error("Expected loaded plugin to have error")
	}
	if loaded.IsHealthy() {
		t.Error("Expected loaded plugin to be unhealthy")
	}
}

func TestDynamicPluginLoader_GetErrors(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	registry := NewPluginRegistry()
	loader := NewDynamicPluginLoader(logger, registry)

	config := &DynamicPluginConfig{
		Path:    "/nonexistent/path/plugin.so",
		Name:    "test-plugin",
		Enabled: true,
	}

	loader.LoadPlugin(config)

	if !loader.HasErrors() {
		t.Error("Expected loader to have errors")
	}

	errors := loader.GetErrors()
	if len(errors) != 1 {
		t.Errorf("Expected 1 error, got %d", len(errors))
	}
}

func TestDynamicPluginManager_Initialize(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	registry := NewPluginRegistry()
	manager := NewDynamicPluginManager(logger, registry)

	configs := []*DynamicPluginConfig{
		{
			Path:    "/nonexistent/path/plugin1.so",
			Name:    "plugin1",
			Enabled: true,
		},
		{
			Path:    "/nonexistent/path/plugin2.so",
			Name:    "plugin2",
			Enabled: false,
		},
	}

	err := manager.Initialize(configs)
	if err != nil {
		t.Errorf("Expected Initialize to succeed, got %v", err)
	}

	plugins := manager.GetLoader().GetLoadedPlugins()
	if len(plugins) != 1 {
		t.Errorf("Expected 1 loaded plugin (only enabled), got %d", len(plugins))
	}
}

func TestDynamicPluginLoader_GetPlugin_NotFound(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	registry := NewPluginRegistry()
	loader := NewDynamicPluginLoader(logger, registry)

	_, found := loader.GetPlugin("nonexistent")
	if found {
		t.Error("Expected plugin not to be found")
	}
}

func TestLoadedDynamicPlugin_IsHealthy(t *testing.T) {
	healthy := &LoadedDynamicPlugin{
		Path:    "/path/to/plugin.so",
		Factory: &testPluginFactory{name: "test"},
		Enabled: true,
		Error:   nil,
	}

	if !healthy.IsHealthy() {
		t.Error("Expected healthy plugin to be healthy")
	}

	unhealthyError := &LoadedDynamicPlugin{
		Path:    "/path/to/plugin.so",
		Factory: &testPluginFactory{name: "test"},
		Enabled: true,
		Error:   ErrDynamicPluginLoadFailed,
	}

	if unhealthyError.IsHealthy() {
		t.Error("Expected plugin with error to be unhealthy")
	}

	unhealthyDisabled := &LoadedDynamicPlugin{
		Path:    "/path/to/plugin.so",
		Factory: &testPluginFactory{name: "test"},
		Enabled: false,
		Error:   nil,
	}

	if unhealthyDisabled.IsHealthy() {
		t.Error("Expected disabled plugin to be unhealthy")
	}

	unhealthyNoFactory := &LoadedDynamicPlugin{
		Path:    "/path/to/plugin.so",
		Factory: nil,
		Enabled: true,
		Error:   nil,
	}

	if unhealthyNoFactory.IsHealthy() {
		t.Error("Expected plugin without factory to be unhealthy")
	}
}

func TestPluginRegistry_Unregister(t *testing.T) {
	registry := NewPluginRegistry()
	factory := &testPluginFactory{name: "test-plugin"}

	registry.Register(factory)

	regFactory, found := registry.Get("test-plugin")
	if !found {
		t.Error("Expected plugin to be found after registration")
	}
	if regFactory.Name() != "test-plugin" {
		t.Errorf("Expected factory name 'test-plugin', got '%s'", regFactory.Name())
	}

	registry.Unregister("test-plugin")

	_, found = registry.Get("test-plugin")
	if found {
		t.Error("Expected plugin not to be found after unregistration")
	}
}

func TestPluginRegistry_Unregister_NonExistent(t *testing.T) {
	registry := NewPluginRegistry()

	registry.Unregister("nonexistent")

	_, found := registry.Get("nonexistent")
	if found {
		t.Error("Expected nonexistent plugin not to be found")
	}
}

func TestPluginRegistry_Register_Overwrite(t *testing.T) {
	registry := NewPluginRegistry()
	factory1 := &testPluginFactory{name: "test-plugin"}
	factory2 := &testPluginFactory{name: "test-plugin"}

	registry.Register(factory1)
	registry.Register(factory2)

	_, found := registry.Get("test-plugin")
	if !found {
		t.Error("Expected plugin to be found")
	}

	registry.Unregister("test-plugin")

	_, found = registry.Get("test-plugin")
	if found {
		t.Error("Expected plugin not to be found after unregistration")
	}
}

func TestDynamicPluginLoader_UnloadPlugin_CleansRegistry(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	registry := NewPluginRegistry()
	loader := NewDynamicPluginLoader(logger, registry)

	testPath := "/path/to/test-plugin.so"
	factory := &testPluginFactory{name: "test-plugin"}

	registry.Register(factory)

	regFactory, found := registry.Get("test-plugin")
	if !found {
		t.Error("Expected plugin to be found in registry")
	}
	if regFactory.Name() != "test-plugin" {
		t.Errorf("Expected factory name 'test-plugin', got '%s'", regFactory.Name())
	}

	loaded := &LoadedDynamicPlugin{
		Path:    testPath,
		Factory: factory,
		Enabled: true,
		Error:   nil,
	}

	loader.mu.Lock()
	loader.loadedPlugins[testPath] = loaded
	loader.mu.Unlock()

	err := loader.UnloadPlugin(testPath)
	if err != nil {
		t.Errorf("Expected UnloadPlugin to succeed, got %v", err)
	}

	_, found = registry.Get("test-plugin")
	if found {
		t.Error("Expected plugin to be removed from registry after unload")
	}

	plugins := loader.GetLoadedPlugins()
	if len(plugins) != 0 {
		t.Errorf("Expected 0 loaded plugins after unload, got %d", len(plugins))
	}
}

func TestDynamicPluginLoader_UnloadPlugin_NonExistentPath(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	registry := NewPluginRegistry()
	loader := NewDynamicPluginLoader(logger, registry)

	err := loader.UnloadPlugin("/nonexistent/path.so")
	if err != nil {
		t.Errorf("Expected UnloadPlugin to succeed for nonexistent path, got %v", err)
	}
}

func TestDynamicPluginLoader_UnloadPlugin_NoFactory(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	registry := NewPluginRegistry()
	loader := NewDynamicPluginLoader(logger, registry)

	testPath := "/path/to/test-plugin.so"

	loaded := &LoadedDynamicPlugin{
		Path:    testPath,
		Factory: nil,
		Enabled: true,
		Error:   nil,
	}

	loader.mu.Lock()
	loader.loadedPlugins[testPath] = loaded
	loader.mu.Unlock()

	err := loader.UnloadPlugin(testPath)
	if err != nil {
		t.Errorf("Expected UnloadPlugin to succeed, got %v", err)
	}

	plugins := loader.GetLoadedPlugins()
	if len(plugins) != 0 {
		t.Errorf("Expected 0 loaded plugins after unload, got %d", len(plugins))
	}
}
