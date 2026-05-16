package plugin

import (
	"net/http"
	"sort"

	"github.com/google/uuid"
)

type PluginChain struct {
	plugins []Plugin
	configs []*PluginConfig
}

func NewPluginChain() *PluginChain {
	return &PluginChain{
		plugins: make([]Plugin, 0),
		configs: make([]*PluginConfig, 0),
	}
}

func (c *PluginChain) Add(plugin Plugin, config *PluginConfig) {
	c.plugins = append(c.plugins, plugin)
	c.configs = append(c.configs, config)
}

func (c *PluginChain) Plugins() []Plugin {
	return c.plugins
}

func (c *PluginChain) Configs() []*PluginConfig {
	return c.configs
}

func (c *PluginChain) Len() int {
	return len(c.plugins)
}

func (c *PluginChain) isPluginMatch(ctx *PluginContext, config *PluginConfig) bool {
	if !config.Enabled {
		return false
	}

	switch config.Scope {
	case ScopeGlobal:
		return true

	case ScopeService:
		if ctx.MatchedRoute == nil {
			return false
		}
		if config.ServiceID == nil {
			return false
		}
		return *config.ServiceID == ctx.MatchedRoute.ServiceID

	case ScopeRoute:
		if ctx.MatchedRoute == nil {
			return false
		}
		if config.RouteID == nil {
			return false
		}
		return *config.RouteID == ctx.MatchedRoute.RouteID

	case ScopeConsumer:
		if ctx.ConsumerID == nil {
			return false
		}
		if config.ConsumerID == nil {
			return false
		}
		return *config.ConsumerID == *ctx.ConsumerID

	default:
		return false
	}
}

func (c *PluginChain) ExecuteOnRequest(ctx *PluginContext) error {
	scopePriority := map[Scope]int{
		ScopeGlobal:   0,
		ScopeService:  1,
		ScopeRoute:    2,
		ScopeConsumer: 3,
	}

	pluginsByName := make(map[string][]*pluginEntry)
	for i, plugin := range c.plugins {
		config := c.configs[i]
		pluginsByName[config.Name] = append(pluginsByName[config.Name], &pluginEntry{
			plugin: plugin,
			config: config,
		})
	}

	executed := make(map[string]bool)

	for i := range c.plugins {
		config := c.configs[i]

		if executed[config.Name] {
			continue
		}

		entries := pluginsByName[config.Name]
		sort.SliceStable(entries, func(i, j int) bool {
			return scopePriority[entries[i].config.Scope] > scopePriority[entries[j].config.Scope]
		})

		var executedEntry *pluginEntry
		for _, entry := range entries {
			if c.isPluginMatch(ctx, entry.config) {
				if err := entry.plugin.OnRequest(ctx); err != nil {
					return NewPluginError(entry.plugin.Name(), "OnRequest failed", err)
				}
				executedEntry = entry
				break
			}
		}

		executed[config.Name] = true

		if executedEntry != nil && ctx.IsShortCircuited() {
			return nil
		}
	}
	return nil
}

type pluginEntry struct {
	plugin Plugin
	config *PluginConfig
}

func (c *PluginChain) ExecuteOnResponse(ctx *PluginContext, resp *http.Response) error {
	scopePriority := map[Scope]int{
		ScopeGlobal:   0,
		ScopeService:  1,
		ScopeRoute:    2,
		ScopeConsumer: 3,
	}

	pluginsByName := make(map[string][]*pluginEntry)
	for i, plugin := range c.plugins {
		config := c.configs[i]
		pluginsByName[config.Name] = append(pluginsByName[config.Name], &pluginEntry{
			plugin: plugin,
			config: config,
		})
	}

	executed := make(map[string]bool)

	for i := len(c.plugins) - 1; i >= 0; i-- {
		config := c.configs[i]

		if executed[config.Name] {
			continue
		}

		entries := pluginsByName[config.Name]
		sort.SliceStable(entries, func(i, j int) bool {
			return scopePriority[entries[i].config.Scope] > scopePriority[entries[j].config.Scope]
		})

		for _, entry := range entries {
			if c.isPluginMatch(ctx, entry.config) {
				if err := entry.plugin.OnResponse(ctx, resp); err != nil {
					return NewPluginError(entry.plugin.Name(), "OnResponse failed", err)
				}
				break
			}
		}

		executed[config.Name] = true
	}
	return nil
}

func (c *PluginChain) ExecuteOnError(ctx *PluginContext, err error) error {
	scopePriority := map[Scope]int{
		ScopeGlobal:   0,
		ScopeService:  1,
		ScopeRoute:    2,
		ScopeConsumer: 3,
	}

	pluginsByName := make(map[string][]*pluginEntry)
	for i, plugin := range c.plugins {
		config := c.configs[i]
		pluginsByName[config.Name] = append(pluginsByName[config.Name], &pluginEntry{
			plugin: plugin,
			config: config,
		})
	}

	executed := make(map[string]bool)

	for i := len(c.plugins) - 1; i >= 0; i-- {
		config := c.configs[i]

		if executed[config.Name] {
			continue
		}

		entries := pluginsByName[config.Name]
		sort.SliceStable(entries, func(i, j int) bool {
			return scopePriority[entries[i].config.Scope] > scopePriority[entries[j].config.Scope]
		})

		for _, entry := range entries {
			if c.isPluginMatch(ctx, entry.config) {
				if pluginErr := entry.plugin.OnError(ctx, err); pluginErr != nil {
					return NewPluginError(entry.plugin.Name(), "OnError failed", pluginErr)
				}
				break
			}
		}

		executed[config.Name] = true
	}
	return nil
}

type EffectivePlugin struct {
	PluginConfig
	PluginInstance Plugin `json:"-"`
	SourceScope    Scope
}

func (ep *EffectivePlugin) Name() string {
	return ep.PluginConfig.Name
}

type PluginChainBuilder struct {
	registry *PluginRegistry
}

func NewPluginChainBuilder(registry *PluginRegistry) *PluginChainBuilder {
	return &PluginChainBuilder{
		registry: registry,
	}
}

func (b *PluginChainBuilder) BuildForRequest(
	globalPlugins []*PluginConfig,
	servicePlugins []*PluginConfig,
	routePlugins []*PluginConfig,
	consumerPlugins []*PluginConfig,
) (*PluginChain, []*EffectivePlugin, error) {
	scopeOrder := []struct {
		scope   Scope
		plugins []*PluginConfig
	}{
		{ScopeGlobal, globalPlugins},
		{ScopeService, servicePlugins},
		{ScopeRoute, routePlugins},
		{ScopeConsumer, consumerPlugins},
	}

	pluginMap := make(map[string]*EffectivePlugin)
	effectiveList := make([]*EffectivePlugin, 0)

	for _, scopeData := range scopeOrder {
		for _, config := range scopeData.plugins {
			if _, exists := pluginMap[config.Name]; exists {
				pluginMap[config.Name].PluginConfig = *config
				pluginMap[config.Name].SourceScope = scopeData.scope
				continue
			}

			pluginInstance, err := b.registry.CreateInstance(config.Name, config.Config)
			if err != nil {
				return nil, nil, err
			}

			effective := &EffectivePlugin{
				PluginConfig:   *config,
				PluginInstance: pluginInstance,
				SourceScope:    scopeData.scope,
			}

			pluginMap[config.Name] = effective
			effectiveList = append(effectiveList, effective)
		}
	}

	sort.Slice(effectiveList, func(i, j int) bool {
		return effectiveList[i].SourceScope < effectiveList[j].SourceScope
	})

	chain := NewPluginChain()
	for _, ep := range effectiveList {
		chain.Add(ep.PluginInstance, &ep.PluginConfig)
	}

	return chain, effectiveList, nil
}

func (b *PluginChainBuilder) BuildForRequestWithAllConsumers(
	globalPlugins []*PluginConfig,
	servicePlugins []*PluginConfig,
	routePlugins []*PluginConfig,
	allConsumerPlugins []*PluginConfig,
) (*PluginChain, []*EffectivePlugin, error) {
	allPlugins := make([]*PluginConfig, 0)
	allPlugins = append(allPlugins, globalPlugins...)
	allPlugins = append(allPlugins, servicePlugins...)
	allPlugins = append(allPlugins, routePlugins...)
	allPlugins = append(allPlugins, allConsumerPlugins...)

	return b.BuildFromConfigsWithScope(allPlugins)
}

func (b *PluginChainBuilder) BuildFromConfigsWithScope(configs []*PluginConfig) (*PluginChain, []*EffectivePlugin, error) {
	scopePriority := map[Scope]int{
		ScopeGlobal:   0,
		ScopeService:  1,
		ScopeRoute:    2,
		ScopeConsumer: 3,
	}

	allPlugins := make([]*EffectivePlugin, 0)

	for _, config := range configs {
		pluginInstance, err := b.registry.CreateInstance(config.Name, config.Config)
		if err != nil {
			return nil, nil, err
		}

		effective := &EffectivePlugin{
			PluginConfig:   *config,
			PluginInstance: pluginInstance,
			SourceScope:    config.Scope,
		}
		allPlugins = append(allPlugins, effective)
	}

	sort.SliceStable(allPlugins, func(i, j int) bool {
		return scopePriority[allPlugins[i].SourceScope] < scopePriority[allPlugins[j].SourceScope]
	})

	chain := NewPluginChain()
	for _, ep := range allPlugins {
		chain.Add(ep.PluginInstance, &ep.PluginConfig)
	}

	effectiveMap := make(map[string]*EffectivePlugin)
	for _, p := range allPlugins {
		existing, exists := effectiveMap[p.PluginConfig.Name]
		if !exists || scopePriority[p.SourceScope] > scopePriority[existing.SourceScope] {
			effectiveMap[p.PluginConfig.Name] = p
		}
	}

	effectiveList := make([]*EffectivePlugin, 0, len(effectiveMap))
	added := make(map[string]bool)
	for _, p := range allPlugins {
		if added[p.PluginConfig.Name] {
			continue
		}
		if effectiveMap[p.PluginConfig.Name] == p {
			effectiveList = append(effectiveList, p)
			added[p.PluginConfig.Name] = true
		}
	}

	return chain, effectiveList, nil
}

func (b *PluginChainBuilder) BuildFromConfigs(configs []*PluginConfig) (*PluginChain, error) {
	chain := NewPluginChain()

	for _, config := range configs {
		pluginInstance, err := b.registry.CreateInstance(config.Name, config.Config)
		if err != nil {
			return nil, err
		}
		chain.Add(pluginInstance, config)
	}

	return chain, nil
}

type SnapshotPluginStore struct {
	globalPlugins      map[string]*PluginConfig
	globalPluginOrder  []string
	servicePlugins     map[uuid.UUID]map[string]*PluginConfig
	servicePluginOrder map[uuid.UUID][]string
	routePlugins       map[uuid.UUID]map[string]*PluginConfig
	routePluginOrder   map[uuid.UUID][]string
	consumerPlugins    map[uuid.UUID]map[string]*PluginConfig
}

func NewSnapshotPluginStore() *SnapshotPluginStore {
	return &SnapshotPluginStore{
		globalPlugins:      make(map[string]*PluginConfig),
		globalPluginOrder:  make([]string, 0),
		servicePlugins:     make(map[uuid.UUID]map[string]*PluginConfig),
		servicePluginOrder: make(map[uuid.UUID][]string),
		routePlugins:       make(map[uuid.UUID]map[string]*PluginConfig),
		routePluginOrder:   make(map[uuid.UUID][]string),
		consumerPlugins:    make(map[uuid.UUID]map[string]*PluginConfig),
	}
}

func (s *SnapshotPluginStore) AddGlobal(config *PluginConfig) {
	if _, exists := s.globalPlugins[config.Name]; !exists {
		s.globalPluginOrder = append(s.globalPluginOrder, config.Name)
	}
	s.globalPlugins[config.Name] = config
}

func (s *SnapshotPluginStore) AddService(serviceID uuid.UUID, config *PluginConfig) {
	if s.servicePlugins[serviceID] == nil {
		s.servicePlugins[serviceID] = make(map[string]*PluginConfig)
		s.servicePluginOrder[serviceID] = make([]string, 0)
	}
	if _, exists := s.servicePlugins[serviceID][config.Name]; !exists {
		s.servicePluginOrder[serviceID] = append(s.servicePluginOrder[serviceID], config.Name)
	}
	s.servicePlugins[serviceID][config.Name] = config
}

func (s *SnapshotPluginStore) AddRoute(routeID uuid.UUID, config *PluginConfig) {
	if s.routePlugins[routeID] == nil {
		s.routePlugins[routeID] = make(map[string]*PluginConfig)
		s.routePluginOrder[routeID] = make([]string, 0)
	}
	if _, exists := s.routePlugins[routeID][config.Name]; !exists {
		s.routePluginOrder[routeID] = append(s.routePluginOrder[routeID], config.Name)
	}
	s.routePlugins[routeID][config.Name] = config
}

func (s *SnapshotPluginStore) AddConsumer(consumerID uuid.UUID, config *PluginConfig) {
	if s.consumerPlugins[consumerID] == nil {
		s.consumerPlugins[consumerID] = make(map[string]*PluginConfig)
	}
	s.consumerPlugins[consumerID][config.Name] = config
}

func (s *SnapshotPluginStore) GetGlobal() []*PluginConfig {
	result := make([]*PluginConfig, 0, len(s.globalPluginOrder))
	for _, name := range s.globalPluginOrder {
		if p, ok := s.globalPlugins[name]; ok {
			result = append(result, p)
		}
	}
	return result
}

func (s *SnapshotPluginStore) GetForService(serviceID uuid.UUID) []*PluginConfig {
	plugins, ok := s.servicePlugins[serviceID]
	if !ok {
		return nil
	}
	order := s.servicePluginOrder[serviceID]
	result := make([]*PluginConfig, 0, len(order))
	for _, name := range order {
		if p, ok := plugins[name]; ok {
			result = append(result, p)
		}
	}
	return result
}

func (s *SnapshotPluginStore) GetForRoute(routeID uuid.UUID) []*PluginConfig {
	plugins, ok := s.routePlugins[routeID]
	if !ok {
		return nil
	}
	order := s.routePluginOrder[routeID]
	result := make([]*PluginConfig, 0, len(order))
	for _, name := range order {
		if p, ok := plugins[name]; ok {
			result = append(result, p)
		}
	}
	return result
}

func (s *SnapshotPluginStore) GetForConsumer(consumerID uuid.UUID) []*PluginConfig {
	plugins, ok := s.consumerPlugins[consumerID]
	if !ok {
		return nil
	}
	result := make([]*PluginConfig, 0, len(plugins))
	for _, p := range plugins {
		result = append(result, p)
	}
	return result
}

func (s *SnapshotPluginStore) GetAllConsumerPlugins() []*PluginConfig {
	result := make([]*PluginConfig, 0)
	for _, plugins := range s.consumerPlugins {
		for _, p := range plugins {
			result = append(result, p)
		}
	}
	return result
}

func (s *SnapshotPluginStore) BuildChainForRequest(
	builder *PluginChainBuilder,
	serviceID uuid.UUID,
	routeID uuid.UUID,
) (*PluginChain, []*EffectivePlugin, error) {
	globalPlugins := s.GetGlobal()
	servicePlugins := s.GetForService(serviceID)
	routePlugins := s.GetForRoute(routeID)
	allConsumerPlugins := s.GetAllConsumerPlugins()

	return builder.BuildForRequestWithAllConsumers(
		globalPlugins,
		servicePlugins,
		routePlugins,
		allConsumerPlugins,
	)
}
