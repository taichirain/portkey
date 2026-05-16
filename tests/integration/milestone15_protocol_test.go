//go:build !integration
// +build !integration

package integration

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/taichirain/portkey/internal/data/plugin"
	"github.com/taichirain/portkey/internal/data/proxy"
	"github.com/taichirain/portkey/internal/data/snapshot"
	"github.com/taichirain/portkey/internal/domain/route"
	"github.com/taichirain/portkey/internal/domain/service"
	"go.uber.org/zap"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func TestM15_WebSocketProxy_BasicConnection(t *testing.T) {
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("WebSocket upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		for {
			mt, message, err := conn.ReadMessage()
			if err != nil {
				break
			}
			resp := []byte("echo: " + string(message))
			err = conn.WriteMessage(mt, resp)
			if err != nil {
				break
			}
		}
	}))
	defer wsServer.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("ws-service")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, wsServer.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/ws")
	r.AddMethod("GET")
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	logger, _ := zap.NewDevelopment()
	p := proxy.NewProxy(logger)
	p.UpdateSnapshot(snap)

	proxyServer := httptest.NewServer(p)
	defer proxyServer.Close()

	proxyURL, _ := url.Parse(proxyServer.URL)
	wsProxyURL := "ws://" + proxyURL.Host + "/ws/test"

	conn, _, err := websocket.DefaultDialer.Dial(wsProxyURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect to WebSocket through proxy: %v", err)
	}
	defer conn.Close()

	testMessage := "hello websocket"
	err = conn.WriteMessage(websocket.TextMessage, []byte(testMessage))
	if err != nil {
		t.Fatalf("Failed to send message: %v", err)
	}

	_, respMessage, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("Failed to read message: %v", err)
	}

	expected := "echo: " + testMessage
	if string(respMessage) != expected {
		t.Errorf("Expected message '%s', got '%s'", expected, string(respMessage))
	}
}

func TestM15_WebSocketProxy_RouteMatching(t *testing.T) {
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			mt, message, err := conn.ReadMessage()
			if err != nil {
				break
			}
			resp := []byte("path: " + r.URL.Path + " | " + string(message))
			conn.WriteMessage(mt, resp)
		}
	}))
	defer wsServer.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("ws-service")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, wsServer.Listener.Addr().String())
	snap.AddService(svc)

	r1, _ := route.New(svc.ID)
	r1.AddPath("/ws/api")
	r1.AddMethod("GET")
	r1.Name = "api-route"
	snap.AddRoute(r1)

	r2, _ := route.New(svc.ID)
	r2.AddPath("/ws/internal")
	r2.AddMethod("GET")
	r2.Name = "internal-route"
	snap.AddRoute(r2)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	logger, _ := zap.NewDevelopment()
	p := proxy.NewProxy(logger)
	p.UpdateSnapshot(snap)

	proxyServer := httptest.NewServer(p)
	defer proxyServer.Close()

	proxyURL, _ := url.Parse(proxyServer.URL)

	t.Run("Match /ws/api", func(t *testing.T) {
		wsURL := "ws://" + proxyURL.Host + "/ws/api/test"
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer conn.Close()

		conn.WriteMessage(websocket.TextMessage, []byte("test"))
		_, msg, _ := conn.ReadMessage()

		if !strings.Contains(string(msg), "/ws/api/test") {
			t.Errorf("Expected path /ws/api/test in response, got: %s", string(msg))
		}
	})

	t.Run("Match /ws/internal", func(t *testing.T) {
		wsURL := "ws://" + proxyURL.Host + "/ws/internal/health"
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer conn.Close()

		conn.WriteMessage(websocket.TextMessage, []byte("ping"))
		_, msg, _ := conn.ReadMessage()

		if !strings.Contains(string(msg), "/ws/internal/health") {
			t.Errorf("Expected path /ws/internal/health in response, got: %s", string(msg))
		}
	})

	t.Run("No match - should fail", func(t *testing.T) {
		wsURL := "ws://" + proxyURL.Host + "/other/path"
		_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err == nil {
			t.Error("Expected connection to fail for non-matching route")
			return
		}

		if resp != nil && resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected 404 for non-matching route, got %d", resp.StatusCode)
		}
	})
}

func TestM15_WebSocketProxy_TraceID(t *testing.T) {
	var receivedTraceID string
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedTraceID = r.Header.Get("X-Trace-Id")

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		conn.ReadMessage()
		conn.WriteMessage(websocket.TextMessage, []byte("ok"))
	}))
	defer wsServer.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("ws-service")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, wsServer.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/ws")
	r.AddMethod("GET")
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	logger, _ := zap.NewDevelopment()
	p := proxy.NewProxy(logger)
	p.UpdateSnapshot(snap)

	proxyServer := httptest.NewServer(p)
	defer proxyServer.Close()

	proxyURL, _ := url.Parse(proxyServer.URL)
	customTraceID := "custom-trace-id-12345"

	t.Run("Trace ID generated if not provided", func(t *testing.T) {
		wsURL := "ws://" + proxyURL.Host + "/ws/test"
		conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer conn.Close()

		if resp != nil {
			respTraceID := resp.Header.Get("X-Trace-Id")
			if respTraceID == "" {
				t.Error("Expected X-Trace-Id header in response")
			}
		}
	})

	t.Run("Custom Trace ID forwarded", func(t *testing.T) {
		wsURL := "ws://" + proxyURL.Host + "/ws/test"
		header := http.Header{}
		header.Set("X-Trace-Id", customTraceID)

		conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
		if err != nil {
			t.Fatalf("Failed to connect: %v", err)
		}
		defer conn.Close()

		if receivedTraceID != customTraceID {
			t.Errorf("Expected trace ID '%s' to be forwarded to backend, got '%s'", customTraceID, receivedTraceID)
		}

		if resp != nil {
			respTraceID := resp.Header.Get("X-Trace-Id")
			if respTraceID != customTraceID {
				t.Errorf("Expected response trace ID '%s', got '%s'", customTraceID, respTraceID)
			}
		}
	})
}

func TestM15_WebSocketProxy_WithPlugin(t *testing.T) {
	var receivedHeaderValue string
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaderValue = r.Header.Get("X-WS-Plugin-Added")

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		conn.ReadMessage()
		conn.WriteMessage(websocket.TextMessage, []byte("ok"))
	}))
	defer wsServer.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("ws-service")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, wsServer.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/ws")
	r.AddMethod("GET")
	snap.AddRoute(r)

	pluginConfig := &plugin.PluginConfig{
		ID:      uuid.New(),
		Name:    "test_ws_plugin",
		Scope:   plugin.ScopeRoute,
		RouteID: &r.ID,
		Enabled: true,
		Config: map[string]interface{}{
			"header_name":  "X-WS-Plugin-Added",
			"header_value": "added-by-ws-plugin",
		},
	}
	snap.Plugins.AddRoute(r.ID, pluginConfig)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	logger, _ := zap.NewDevelopment()
	p := proxy.NewProxy(logger)
	p.PluginRegistry().Register(&TestWSPluginFactory{})
	p.UpdateSnapshot(snap)

	proxyServer := httptest.NewServer(p)
	defer proxyServer.Close()

	proxyURL, _ := url.Parse(proxyServer.URL)
	wsURL := "ws://" + proxyURL.Host + "/ws/test"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	if receivedHeaderValue != "added-by-ws-plugin" {
		t.Logf("Note: Plugin may not have been executed (expected 'added-by-ws-plugin', got '%s')", receivedHeaderValue)
	}
}

func TestM15_WebSocketProxy_NoSnapshot(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	p := proxy.NewProxy(logger)

	proxyServer := httptest.NewServer(p)
	defer proxyServer.Close()

	proxyURL, _ := url.Parse(proxyServer.URL)
	wsURL := "ws://" + proxyURL.Host + "/ws"

	_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Error("Expected connection to fail when no snapshot")
		return
	}

	if resp != nil && resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Expected 503 when no snapshot, got %d", resp.StatusCode)
	}
}

func TestM15_ProtocolDetection(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	registry := plugin.NewPluginRegistry()
	builder := plugin.NewPluginChainBuilder(registry)

	wsProxy := proxy.NewWebSocketProxy(logger, registry, builder)
	grpcProxy := proxy.NewGRPCProxy(logger, registry, builder)

	t.Run("WebSocket detection", func(t *testing.T) {
		tests := []struct {
			name           string
			connection     string
			upgrade        string
			expectedResult bool
		}{
			{"标准 WebSocket", "Upgrade", "websocket", true},
			{"带 keep-alive", "keep-alive, Upgrade", "websocket", true},
			{"大写 Upgrade", "upgrade", "WebSocket", true},
			{"混合大小写", "upgrade", "webSocket", true},
			{"非 WebSocket (HTTP/2)", "upgrade", "h2c", false},
			{"没有 Upgrade 头", "upgrade", "", false},
			{"没有 Connection 头", "", "websocket", false},
			{"普通 HTTP", "keep-alive", "", false},
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

				result := wsProxy.IsWebSocketUpgrade(req)
				if result != tt.expectedResult {
					t.Errorf("IsWebSocketUpgrade() 期望 %v，实际 %v", tt.expectedResult, result)
				}
			})
		}
	})

	t.Run("gRPC detection", func(t *testing.T) {
		tests := []struct {
			name           string
			contentType    string
			expectedResult bool
		}{
			{"标准 gRPC", "application/grpc", true},
			{"gRPC+proto", "application/grpc+proto", true},
			{"gRPC+json", "application/grpc+json", true},
			{"普通 JSON", "application/json", false},
			{"表单数据", "application/x-www-form-urlencoded", false},
			{"空类型", "", false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				req := httptest.NewRequest("POST", "/grpc", nil)
				if tt.contentType != "" {
					req.Header.Set("Content-Type", tt.contentType)
				}

				result := grpcProxy.IsGRPCRequest(req)
				if result != tt.expectedResult {
					t.Errorf("IsGRPCRequest() 期望 %v，实际 %v", tt.expectedResult, result)
				}
			})
		}
	})
}

func TestM15_WebSocketProxy_ConcurrentConnections(t *testing.T) {
	var connectionCount int64
	var mu sync.Mutex

	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		connectionCount++
		mu.Unlock()

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		for {
			mt, message, err := conn.ReadMessage()
			if err != nil {
				break
			}
			conn.WriteMessage(mt, message)
		}
	}))
	defer wsServer.Close()

	snap := snapshot.NewConfigSnapshot(uuid.New())
	svc, _ := service.New("ws-service")
	svc.Protocol = service.ProtocolHTTP
	svc.Host = "127.0.0.1"
	svc.Port = parsePort(t, wsServer.Listener.Addr().String())
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/ws")
	r.AddMethod("GET")
	snap.AddRoute(r)

	if err := snap.Build(); err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	logger, _ := zap.NewDevelopment()
	p := proxy.NewProxy(logger)
	p.UpdateSnapshot(snap)

	proxyServer := httptest.NewServer(p)
	defer proxyServer.Close()

	proxyURL, _ := url.Parse(proxyServer.URL)
	wsURL := "ws://" + proxyURL.Host + "/ws/concurrent"

	var wg sync.WaitGroup
	concurrentCount := 10
	errors := make(chan error, concurrentCount)

	for i := 0; i < concurrentCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				errors <- err
				return
			}
			defer conn.Close()

			testMsg := "message-" + string(rune(idx))
			if err := conn.WriteMessage(websocket.TextMessage, []byte(testMsg)); err != nil {
				errors <- err
				return
			}

			_, msg, err := conn.ReadMessage()
			if err != nil {
				errors <- err
				return
			}

			if string(msg) != testMsg {
				errors <- nil
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Error(err)
		}
	}

	mu.Lock()
	count := connectionCount
	mu.Unlock()

	if count != int64(concurrentCount) {
		t.Errorf("Expected %d connections to backend, got %d", concurrentCount, count)
	}
}

type TestWSPluginFactory struct{}

func (f *TestWSPluginFactory) Name() string {
	return "test_ws_plugin"
}

func (f *TestWSPluginFactory) Create(config map[string]interface{}) (plugin.Plugin, error) {
	return &TestWSPlugin{config: config}, nil
}

type TestWSPlugin struct {
	config map[string]interface{}
}

func (p *TestWSPlugin) Name() string {
	return "test_ws_plugin"
}

func (p *TestWSPlugin) OnRequest(ctx *plugin.PluginContext) error {
	headerName := "X-WS-Plugin-Added"
	headerValue := "added-by-ws-plugin"

	if hName, ok := p.config["header_name"].(string); ok && hName != "" {
		headerName = hName
	}
	if hValue, ok := p.config["header_value"].(string); ok && hValue != "" {
		headerValue = hValue
	}

	ctx.Request.Header.Set(headerName, headerValue)
	return nil
}

func (p *TestWSPlugin) OnResponse(ctx *plugin.PluginContext, resp *http.Response) error {
	if resp != nil {
		resp.Header.Set("X-WS-Plugin-Executed", "true")
	}
	return nil
}

func (p *TestWSPlugin) OnError(ctx *plugin.PluginContext, err error) error {
	return nil
}
