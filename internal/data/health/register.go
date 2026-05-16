package health

import (
	pluginPkg "github.com/taichirain/portkey/internal/data/plugin"
)

func Register(registry *pluginPkg.PluginRegistry) {
	registry.Register(NewHealthCheckFactory())
	registry.Register(NewRetryFactory())
}
