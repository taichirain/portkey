package plugin

import (
	"errors"
	"fmt"
	"os"
	"plugin"
	"sync"
	"time"

	"go.uber.org/zap"
)

var (
	ErrDynamicPluginLoadFailed = errors.New("dynamic plugin load failed")
	ErrDynamicPluginSymbolNotFound = errors.New("dynamic plugin symbol not found")
	ErrDynamicPluginInvalidFactory = errors.New("dynamic plugin invalid factory")
)

type DynamicPluginLoader struct {
	logger        *zap.Logger
	registry      *PluginRegistry
	loadedPlugins map[string]*LoadedDynamicPlugin
	mu            sync.RWMutex
}

type LoadedDynamicPlugin struct {
	Path       string
	Factory    PluginFactory
	Plugin     *plugin.Plugin
	LoadedAt   int64
	Enabled    bool
	Error      error
}

type DynamicPluginConfig struct {
	Path    string
	Name    string
	Enabled bool
	Config  map[string]interface{}
}

func NewDynamicPluginLoader(logger *zap.Logger, registry *PluginRegistry) *DynamicPluginLoader {
	return &DynamicPluginLoader{
		logger:        logger,
		registry:      registry,
		loadedPlugins: make(map[string]*LoadedDynamicPlugin),
	}
}

func (l *DynamicPluginLoader) LoadPlugin(config *DynamicPluginConfig) (*LoadedDynamicPlugin, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !config.Enabled {
		l.logger.Info("动态插件已禁用，跳过加载",
			zap.String("path", config.Path),
			zap.String("name", config.Name),
		)
		return nil, nil
	}

	if existing, exists := l.loadedPlugins[config.Path]; exists {
		if existing.Error == nil {
			l.logger.Debug("动态插件已加载，跳过重复加载",
				zap.String("path", config.Path),
			)
			return existing, nil
		}
	}

	if _, err := os.Stat(config.Path); os.IsNotExist(err) {
		l.logger.Warn("动态插件文件不存在",
			zap.String("path", config.Path),
			zap.Error(err),
		)
		loaded := &LoadedDynamicPlugin{
			Path:    config.Path,
			Enabled: config.Enabled,
			Error:   fmt.Errorf("plugin file not found: %s", config.Path),
		}
		l.loadedPlugins[config.Path] = loaded
		return loaded, fmt.Errorf("%w: plugin file not found: %s", ErrDynamicPluginLoadFailed, config.Path)
	}

	loadedPlugin, err := l.doLoad(config)
	if err != nil {
		l.logger.Error("动态插件加载失败",
			zap.String("path", config.Path),
			zap.Error(err),
		)
		loaded := &LoadedDynamicPlugin{
			Path:    config.Path,
			Enabled: config.Enabled,
			Error:   err,
		}
		l.loadedPlugins[config.Path] = loaded
		return loaded, fmt.Errorf("%w: %s", ErrDynamicPluginLoadFailed, err.Error())
	}

	loadedPlugin.Enabled = config.Enabled
	l.loadedPlugins[config.Path] = loadedPlugin

	l.registry.Register(loadedPlugin.Factory)

	l.logger.Info("动态插件加载成功",
		zap.String("path", config.Path),
		zap.String("name", loadedPlugin.Factory.Name()),
	)

	return loadedPlugin, nil
}

func (l *DynamicPluginLoader) doLoad(config *DynamicPluginConfig) (*LoadedDynamicPlugin, error) {
	p, err := plugin.Open(config.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to open plugin: %w", err)
	}

	sym, err := p.Lookup("PluginFactory")
	if err != nil {
		return nil, fmt.Errorf("%w: PluginFactory", ErrDynamicPluginSymbolNotFound)
	}

	factory, ok := sym.(PluginFactory)
	if !ok {
		return nil, ErrDynamicPluginInvalidFactory
	}

	return &LoadedDynamicPlugin{
		Path:     config.Path,
		Factory:  factory,
		Plugin:   p,
		LoadedAt: time.Now().Unix(),
		Enabled:  config.Enabled,
		Error:    nil,
	}, nil
}

func (l *DynamicPluginLoader) UnloadPlugin(path string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	loaded, exists := l.loadedPlugins[path]
	if !exists {
		return nil
	}

	if loaded.Factory != nil {
		l.registry.Unregister(loaded.Factory.Name())
		l.logger.Info("动态插件已卸载",
			zap.String("path", path),
			zap.String("name", loaded.Factory.Name()),
		)
	}

	delete(l.loadedPlugins, path)
	return nil
}

func (l *DynamicPluginLoader) LoadPlugins(configs []*DynamicPluginConfig) int {
	successCount := 0
	for _, config := range configs {
		_, err := l.LoadPlugin(config)
		if err == nil {
			successCount++
		} else if config.Enabled {
			l.logger.Warn("动态插件加载失败，但继续加载其他插件",
				zap.String("path", config.Path),
				zap.Error(err),
			)
		}
	}
	return successCount
}

func (l *DynamicPluginLoader) GetLoadedPlugins() []*LoadedDynamicPlugin {
	l.mu.RLock()
	defer l.mu.RUnlock()

	plugins := make([]*LoadedDynamicPlugin, 0, len(l.loadedPlugins))
	for _, p := range l.loadedPlugins {
		plugins = append(plugins, p)
	}
	return plugins
}

func (l *DynamicPluginLoader) GetPlugin(name string) (PluginFactory, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	for _, p := range l.loadedPlugins {
		if p.Factory != nil && p.Factory.Name() == name && p.Enabled {
			return p.Factory, true
		}
	}
	return nil, false
}

func (l *DynamicPluginLoader) HasErrors() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	for _, p := range l.loadedPlugins {
		if p.Error != nil {
			return true
		}
	}
	return false
}

func (l *DynamicPluginLoader) GetErrors() map[string]error {
	l.mu.RLock()
	defer l.mu.RUnlock()

	errors := make(map[string]error)
	for path, p := range l.loadedPlugins {
		if p.Error != nil {
			errors[path] = p.Error
		}
	}
	return errors
}

type DynamicPluginManager struct {
	loader   *DynamicPluginLoader
	registry *PluginRegistry
	logger   *zap.Logger
}

func NewDynamicPluginManager(
	logger *zap.Logger,
	registry *PluginRegistry,
) *DynamicPluginManager {
	return &DynamicPluginManager{
		loader:   NewDynamicPluginLoader(logger, registry),
		registry: registry,
		logger:   logger,
	}
}

func (m *DynamicPluginManager) Initialize(configs []*DynamicPluginConfig) error {
	m.logger.Info("开始初始化动态插件管理器",
		zap.Int("plugin_count", len(configs)),
	)

	successCount := m.loader.LoadPlugins(configs)
	errorCount := len(configs) - successCount

	if errorCount > 0 {
		m.logger.Warn("部分动态插件加载失败",
			zap.Int("success", successCount),
			zap.Int("failed", errorCount),
		)
	}

	m.logger.Info("动态插件管理器初始化完成",
		zap.Int("loaded_plugins", successCount),
	)

	return nil
}

func (m *DynamicPluginManager) Reload(configs []*DynamicPluginConfig) error {
	m.logger.Info("开始重新加载动态插件")

	for _, p := range m.loader.GetLoadedPlugins() {
		m.loader.UnloadPlugin(p.Path)
	}

	return m.Initialize(configs)
}

func (m *DynamicPluginManager) GetLoader() *DynamicPluginLoader {
	return m.loader
}

func (f *LoadedDynamicPlugin) IsHealthy() bool {
	return f.Error == nil && f.Enabled && f.Factory != nil
}
