package proxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	pluginPkg "github.com/taichirain/portkey/internal/data/plugin"
	"github.com/taichirain/portkey/internal/data/snapshot"
	"go.uber.org/zap"
)

type WebSocketProxy struct {
	logger             *zap.Logger
	currentSnapshot    atomic.Pointer[snapshot.ConfigSnapshot]
	pluginRegistry     *pluginPkg.PluginRegistry
	pluginChainBuilder *pluginPkg.PluginChainBuilder
}

func NewWebSocketProxy(
	logger *zap.Logger,
	pluginRegistry *pluginPkg.PluginRegistry,
	pluginChainBuilder *pluginPkg.PluginChainBuilder,
) *WebSocketProxy {
	return &WebSocketProxy{
		logger:             logger,
		pluginRegistry:     pluginRegistry,
		pluginChainBuilder: pluginChainBuilder,
	}
}

func (p *WebSocketProxy) UpdateSnapshot(snap *snapshot.ConfigSnapshot) {
	p.currentSnapshot.Store(snap)
}

func (p *WebSocketProxy) IsWebSocketUpgrade(r *http.Request) bool {
	return p.isUpgradeRequest(r) &&
		strings.ToLower(r.Header.Get("Upgrade")) == "websocket"
}

func (p *WebSocketProxy) isUpgradeRequest(r *http.Request) bool {
	connectionHeader := r.Header.Get("Connection")
	if connectionHeader == "" {
		return false
	}

	parts := strings.Split(connectionHeader, ",")
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if strings.ToLower(trimmed) == "upgrade" {
			return r.Header.Get("Upgrade") != ""
		}
	}
	return false
}

func (p *WebSocketProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	traceID := r.Header.Get(TraceIDHeader)
	if traceID == "" {
		traceID = uuid.New().String()
		r.Header.Set(TraceIDHeader, traceID)
	}
	w.Header().Set(TraceIDHeader, traceID)

	snap := p.currentSnapshot.Load()
	if snap == nil {
		p.logger.Warn("没有可用的配置快照", zap.String("trace_id", traceID))
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}

	matched, ok := snap.MatchRoute(r)
	if !ok {
		p.logger.Warn("未匹配到路由",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("trace_id", traceID),
		)
		http.Error(w, "Not Found", http.StatusNotFound)
		p.logWebSocketAccess(r, http.StatusNotFound, time.Since(start), traceID, "route_not_found")
		return
	}

	pluginCtx := pluginPkg.NewPluginContext(w, r, traceID)
	pluginCtx.SetMatchedRoute(matched.Route.ID, matched.Service.ID, matched.Service.UpstreamID)

	credFetcher := snapshot.NewSnapshotCredentialFetcher(snap)
	pluginCtx.SetAttribute("credential_fetcher", credFetcher)

	chain, effectivePlugins, err := snap.Plugins.BuildChainForRequest(
		p.pluginChainBuilder,
		matched.Service.ID,
		matched.Route.ID,
	)
	if err != nil {
		p.logger.Error("构建插件链失败",
			zap.Error(err),
			zap.String("trace_id", traceID),
		)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		p.logWebSocketAccess(r, http.StatusInternalServerError, time.Since(start), traceID, "plugin_chain_error")
		return
	}

	p.setEffectivePluginsHeader(w, effectivePlugins)

	if err := chain.ExecuteOnRequest(pluginCtx); err != nil {
		p.logger.Error("OnRequest 插件执行失败",
			zap.Error(err),
			zap.String("trace_id", traceID),
		)
		_ = chain.ExecuteOnError(pluginCtx, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		p.logWebSocketAccess(r, http.StatusInternalServerError, time.Since(start), traceID, "plugin_error")
		return
	}

	if pluginCtx.IsShortCircuited() {
		p.logger.Debug("请求被插件短路",
			zap.String("trace_id", traceID),
		)
		p.logWebSocketAccess(r, http.StatusOK, time.Since(start), traceID, "short_circuited")
		return
	}

	var targetAddr string
	if matched.Balancer != nil {
		target, ok := matched.Balancer.Next()
		if !ok {
			p.logger.Error("没有可用的目标", zap.String("trace_id", traceID))
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
			p.logWebSocketAccess(r, http.StatusServiceUnavailable, time.Since(start), traceID, "no_target")
			return
		}
		targetAddr = fmt.Sprintf("%s:%d", target.Target, target.Port)
	} else if matched.Service.Host != "" {
		targetAddr = fmt.Sprintf("%s:%d", matched.Service.Host, matched.Service.Port)
	} else {
		p.logger.Error("没有可用的目标配置", zap.String("trace_id", traceID))
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		p.logWebSocketAccess(r, http.StatusServiceUnavailable, time.Since(start), traceID, "invalid_target")
		return
	}

	p.logger.Debug("WebSocket 代理目标",
		zap.String("target", targetAddr),
		zap.String("trace_id", traceID),
	)

	err = p.proxyWebSocket(w, r, targetAddr, traceID, chain, pluginCtx)
	if err != nil {
		p.logger.Error("WebSocket 代理失败",
			zap.Error(err),
			zap.String("trace_id", traceID),
		)
		_ = chain.ExecuteOnError(pluginCtx, err)
		p.logWebSocketAccess(r, http.StatusBadGateway, time.Since(start), traceID, "proxy_error")
		return
	}

	latency := time.Since(start)
	p.logWebSocketAccess(r, http.StatusSwitchingProtocols, latency, traceID, "success")
}

func (p *WebSocketProxy) proxyWebSocket(
	w http.ResponseWriter,
	r *http.Request,
	targetAddr string,
	traceID string,
	chain *pluginPkg.PluginChain,
	pluginCtx *pluginPkg.PluginContext,
) error {
	h, ok := w.(http.Hijacker)
	if !ok {
		return fmt.Errorf("不支持 WebSocket hijacking")
	}

	clientConn, _, err := h.Hijack()
	if err != nil {
		return fmt.Errorf("hijack 失败: %w", err)
	}
	defer clientConn.Close()

	var targetConn net.Conn
	targetURL, err := url.Parse(fmt.Sprintf("ws://%s", targetAddr))
	if err != nil {
		return fmt.Errorf("解析目标地址失败: %w", err)
	}

	targetConn, err = net.Dial("tcp", targetURL.Host)
	if err != nil {
		return fmt.Errorf("连接目标失败: %w", err)
	}
	defer targetConn.Close()

	err = p.writeWebSocketRequest(targetConn, r, targetAddr)
	if err != nil {
		return fmt.Errorf("发送 WebSocket 请求失败: %w", err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(targetConn), r)
	if err != nil {
		return fmt.Errorf("读取 WebSocket 响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		return fmt.Errorf("WebSocket 升级失败，状态码: %d", resp.StatusCode)
	}

	resp.Header.Set(TraceIDHeader, traceID)
	err = p.writeWebSocketResponse(clientConn, resp)
	if err != nil {
		return fmt.Errorf("发送 WebSocket 响应失败: %w", err)
	}

	p.logger.Info("WebSocket 连接已建立",
		zap.String("trace_id", traceID),
		zap.String("target", targetAddr),
	)

	errChan := make(chan error, 2)

	go func() {
		_, err := io.Copy(targetConn, clientConn)
		errChan <- err
	}()

	go func() {
		_, err := io.Copy(clientConn, targetConn)
		errChan <- err
	}()

	select {
	case err := <-errChan:
		if err != nil && err != io.EOF {
			p.logger.Warn("WebSocket 连接断开",
				zap.Error(err),
				zap.String("trace_id", traceID),
			)
		}
	case <-r.Context().Done():
	}

	p.logger.Info("WebSocket 连接已关闭",
		zap.String("trace_id", traceID),
	)

	return nil
}

func (p *WebSocketProxy) writeWebSocketRequest(conn net.Conn, r *http.Request, targetAddr string) error {
	reqLine := fmt.Sprintf("%s %s HTTP/1.1\r\n", r.Method, r.URL.RequestURI())
	if _, err := conn.Write([]byte(reqLine)); err != nil {
		return err
	}

	hostHeader := fmt.Sprintf("Host: %s\r\n", targetAddr)
	if _, err := conn.Write([]byte(hostHeader)); err != nil {
		return err
	}

	for name, values := range r.Header {
		if strings.EqualFold(name, "Host") {
			continue
		}
		for _, value := range values {
			headerLine := fmt.Sprintf("%s: %s\r\n", name, value)
			if _, err := conn.Write([]byte(headerLine)); err != nil {
				return err
			}
		}
	}

	if _, err := conn.Write([]byte("\r\n")); err != nil {
		return err
	}

	return nil
}

func (p *WebSocketProxy) writeWebSocketResponse(conn net.Conn, resp *http.Response) error {
	statusLine := fmt.Sprintf("HTTP/1.1 %d %s\r\n", resp.StatusCode, resp.Status)
	if _, err := conn.Write([]byte(statusLine)); err != nil {
		return err
	}

	for name, values := range resp.Header {
		for _, value := range values {
			headerLine := fmt.Sprintf("%s: %s\r\n", name, value)
			if _, err := conn.Write([]byte(headerLine)); err != nil {
				return err
			}
		}
	}

	if _, err := conn.Write([]byte("\r\n")); err != nil {
		return err
	}

	return nil
}

func (p *WebSocketProxy) logWebSocketAccess(
	r *http.Request,
	statusCode int,
	latency time.Duration,
	traceID string,
	status string,
) {
	fields := []zap.Field{
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
		zap.String("host", r.Host),
		zap.String("remote_addr", r.RemoteAddr),
		zap.Int("status", statusCode),
		zap.Duration("latency", latency),
		zap.String("trace_id", traceID),
		zap.String("websocket_status", status),
		zap.String("user_agent", r.UserAgent()),
	}

	p.logger.Info("websocket_access", fields...)
}

func (p *WebSocketProxy) ServeWithContext(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	p.ServeHTTP(w, r.WithContext(ctx))
}

func (p *WebSocketProxy) setEffectivePluginsHeader(w http.ResponseWriter, plugins []*pluginPkg.EffectivePlugin) {
	if len(plugins) == 0 {
		return
	}

	pluginNames := make([]string, len(plugins))
	for i, ep := range plugins {
		pluginNames[i] = ep.Name() + ":" + ep.SourceScope.String()
	}

	w.Header().Set("X-Effective-Plugins", strings.Join(pluginNames, ","))
}
