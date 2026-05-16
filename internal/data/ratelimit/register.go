package ratelimit

import (
	pluginPkg "github.com/taichirain/portkey/internal/data/plugin"
)

func RegisterRateLimitPlugin(registry *pluginPkg.PluginRegistry) {
	registry.Register(NewRateLimitFactory())
}
