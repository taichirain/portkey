package proxy

import (
	"net/http/httptest"
	"testing"

	pluginPkg "github.com/taichirain/portkey/internal/data/plugin"
	"go.uber.org/zap"
)

func TestWebSocketProxy_isUpgradeRequest(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	registry := newTestPluginRegistry(t)
	builder := newTestPluginChainBuilder(t, registry)
	proxy := NewWebSocketProxy(logger, registry, builder)

	tests := []struct {
		name           string
		connection     string
		upgrade        string
		expectedResult bool
	}{
		{
			name:           "纯 upgrade 值",
			connection:     "upgrade",
			upgrade:        "websocket",
			expectedResult: true,
		},
		{
			name:           "纯 Upgrade 值（大写）",
			connection:     "Upgrade",
			upgrade:        "websocket",
			expectedResult: true,
		},
		{
			name:           "多值格式：keep-alive, Upgrade",
			connection:     "keep-alive, Upgrade",
			upgrade:        "websocket",
			expectedResult: true,
		},
		{
			name:           "多值格式：upgrade, keep-alive",
			connection:     "upgrade, keep-alive",
			upgrade:        "websocket",
			expectedResult: true,
		},
		{
			name:           "多值格式带空格：keep-alive , Upgrade , close",
			connection:     "keep-alive , Upgrade , close",
			upgrade:        "websocket",
			expectedResult: true,
		},
		{
			name:           "大小写混合：uPgRaDe",
			connection:     "uPgRaDe",
			upgrade:        "websocket",
			expectedResult: true,
		},
		{
			name:           "空 Connection 头",
			connection:     "",
			upgrade:        "websocket",
			expectedResult: false,
		},
		{
			name:           "Connection 头但没有 upgrade",
			connection:     "keep-alive",
			upgrade:        "websocket",
			expectedResult: false,
		},
		{
			name:           "Connection 头有 upgrade 但没有 Upgrade 头",
			connection:     "upgrade",
			upgrade:        "",
			expectedResult: false,
		},
		{
			name:           "多值格式但没有 upgrade",
			connection:     "keep-alive, close",
			upgrade:        "websocket",
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ws", nil)
			if tt.connection != "" {
				req.Header.Set("Connection", tt.connection)
			}
			if tt.upgrade != "" {
				req.Header.Set("Upgrade", tt.upgrade)
			}

			result := proxy.isUpgradeRequest(req)
			if result != tt.expectedResult {
				t.Errorf("isUpgradeRequest() 期望 %v，实际 %v", tt.expectedResult, result)
			}
		})
	}
}

func TestWebSocketProxy_IsWebSocketUpgrade(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	registry := newTestPluginRegistry(t)
	builder := newTestPluginChainBuilder(t, registry)
	proxy := NewWebSocketProxy(logger, registry, builder)

	tests := []struct {
		name           string
		connection     string
		upgrade        string
		expectedResult bool
	}{
		{
			name:           "标准 WebSocket 升级请求",
			connection:     "Upgrade",
			upgrade:        "websocket",
			expectedResult: true,
		},
		{
			name:           "浏览器多值格式：keep-alive, Upgrade",
			connection:     "keep-alive, Upgrade",
			upgrade:        "websocket",
			expectedResult: true,
		},
		{
			name:           "Upgrade 头大写：WebSocket",
			connection:     "upgrade",
			upgrade:        "WebSocket",
			expectedResult: true,
		},
		{
			name:           "Upgrade 头混合大小写：webSocket",
			connection:     "upgrade",
			upgrade:        "webSocket",
			expectedResult: true,
		},
		{
			name:           "非 WebSocket Upgrade（如 HTTP/2）",
			connection:     "upgrade",
			upgrade:        "h2c",
			expectedResult: false,
		},
		{
			name:           "没有 Upgrade 头",
			connection:     "upgrade",
			upgrade:        "",
			expectedResult: false,
		},
		{
			name:           "没有 Connection 头",
			connection:     "",
			upgrade:        "websocket",
			expectedResult: false,
		},
		{
			name:           "普通 HTTP 请求",
			connection:     "keep-alive",
			upgrade:        "",
			expectedResult: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/ws", nil)
			if tt.connection != "" {
				req.Header.Set("Connection", tt.connection)
			}
			if tt.upgrade != "" {
				req.Header.Set("Upgrade", tt.upgrade)
			}

			result := proxy.IsWebSocketUpgrade(req)
			if result != tt.expectedResult {
				t.Errorf("IsWebSocketUpgrade() 期望 %v，实际 %v", tt.expectedResult, result)
			}
		})
	}
}

func TestWebSocketProxy_UpdateSnapshot(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	registry := newTestPluginRegistry(t)
	builder := newTestPluginChainBuilder(t, registry)
	proxy := NewWebSocketProxy(logger, registry, builder)

	req := httptest.NewRequest("GET", "/ws", nil)
	req.Header.Set("Connection", "upgrade")
	req.Header.Set("Upgrade", "websocket")

	result := proxy.IsWebSocketUpgrade(req)
	if !result {
		t.Error("期望 IsWebSocketUpgrade 返回 true")
	}
}

func TestWebSocketProxy_Initialization(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	registry := newTestPluginRegistry(t)
	builder := newTestPluginChainBuilder(t, registry)
	proxy := NewWebSocketProxy(logger, registry, builder)

	if proxy == nil {
		t.Error("期望 NewWebSocketProxy 返回非空值")
	}
}

func newTestPluginRegistry(t *testing.T) *pluginPkg.PluginRegistry {
	t.Helper()
	return pluginPkg.NewPluginRegistry()
}

func newTestPluginChainBuilder(t *testing.T, registry *pluginPkg.PluginRegistry) *pluginPkg.PluginChainBuilder {
	t.Helper()
	return pluginPkg.NewPluginChainBuilder(registry)
}
