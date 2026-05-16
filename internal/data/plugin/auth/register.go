package auth

import (
	pluginPkg "github.com/taichirain/portkey/internal/data/plugin"
)

func RegisterAuthPlugins(registry *pluginPkg.PluginRegistry) {
	registry.Register(NewKeyAuthFactory())
	registry.Register(NewJWTAuthFactory())
}
