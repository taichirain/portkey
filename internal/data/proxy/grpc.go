package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	pluginPkg "github.com/taichirain/portkey/internal/data/plugin"
	"github.com/taichirain/portkey/internal/data/snapshot"
	"go.uber.org/zap"
	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type GRPCProxy struct {
	logger            *zap.Logger
	currentSnapshot   atomic.Pointer[snapshot.ConfigSnapshot]
	pluginRegistry    *pluginPkg.PluginRegistry
	pluginChainBuilder *pluginPkg.PluginChainBuilder
	http2Transport     *http2.Transport
}

func NewGRPCProxy(
	logger *zap.Logger,
	pluginRegistry *pluginPkg.PluginRegistry,
	pluginChainBuilder *pluginPkg.PluginChainBuilder,
) *GRPCProxy {
	return &GRPCProxy{
		logger:            logger,
		pluginRegistry:    pluginRegistry,
		pluginChainBuilder: pluginChainBuilder,
		http2Transport: &http2.Transport{
			AllowHTTP: true,
		},
	}
}

func (p *GRPCProxy) UpdateSnapshot(snap *snapshot.ConfigSnapshot) {
	p.currentSnapshot.Store(snap)
}

func (p *GRPCProxy) IsGRPCRequest(r *http.Request) bool {
	contentType := r.Header.Get("Content-Type")
	return contentType == "application/grpc" || 
		contentType == "application/grpc+proto" ||
		contentType == "application/grpc+json"
}

func (p *GRPCProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		p.writeGRPCError(w, codes.Unavailable, "Service Unavailable")
		return
	}

	matched, ok := snap.MatchRoute(r)
	if !ok {
		p.logger.Warn("未匹配到路由",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("trace_id", traceID),
		)
		p.writeGRPCError(w, codes.NotFound, "Not Found")
		p.logGRPCAccess(r, int(codes.NotFound), time.Since(start), traceID, "route_not_found")
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
		p.writeGRPCError(w, codes.Internal, "Internal Server Error")
		p.logGRPCAccess(r, int(codes.Internal), time.Since(start), traceID, "plugin_chain_error")
		return
	}

	p.setEffectivePluginsHeader(w, effectivePlugins)

	if err := chain.ExecuteOnRequest(pluginCtx); err != nil {
		p.logger.Error("OnRequest 插件执行失败",
			zap.Error(err),
			zap.String("trace_id", traceID),
		)
		_ = chain.ExecuteOnError(pluginCtx, err)
		p.writeGRPCError(w, codes.Internal, "Internal Server Error")
		p.logGRPCAccess(r, int(codes.Internal), time.Since(start), traceID, "plugin_error")
		return
	}

	if pluginCtx.IsShortCircuited() {
		p.logger.Debug("请求被插件短路",
			zap.String("trace_id", traceID),
		)
		p.logGRPCAccess(r, int(codes.OK), time.Since(start), traceID, "short_circuited")
		return
	}

	var targetAddr string
	if matched.Balancer != nil {
		target, ok := matched.Balancer.Next()
		if !ok {
			p.logger.Error("没有可用的目标", zap.String("trace_id", traceID))
			p.writeGRPCError(w, codes.Unavailable, "Service Unavailable")
			p.logGRPCAccess(r, int(codes.Unavailable), time.Since(start), traceID, "no_target")
			return
		}
		targetAddr = fmt.Sprintf("%s:%d", target.Target, target.Port)
	} else if matched.Service.Host != "" {
		targetAddr = fmt.Sprintf("%s:%d", matched.Service.Host, matched.Service.Port)
	} else {
		p.logger.Error("没有可用的目标配置", zap.String("trace_id", traceID))
		p.writeGRPCError(w, codes.Unavailable, "Service Unavailable")
		p.logGRPCAccess(r, int(codes.Unavailable), time.Since(start), traceID, "invalid_target")
		return
	}

	p.logger.Debug("gRPC 代理目标",
		zap.String("target", targetAddr),
		zap.String("trace_id", traceID),
		zap.String("method", r.URL.Path),
	)

	err = p.proxyGRPC(w, r, targetAddr, traceID, chain, pluginCtx)
	if err != nil {
		p.logger.Error("gRPC 代理失败",
			zap.Error(err),
			zap.String("trace_id", traceID),
		)
		_ = chain.ExecuteOnError(pluginCtx, err)
		p.logGRPCAccess(r, int(codes.Internal), time.Since(start), traceID, "proxy_error")
		return
	}

	latency := time.Since(start)
	p.logGRPCAccess(r, int(codes.OK), latency, traceID, "success")
}

func (p *GRPCProxy) proxyGRPC(
	w http.ResponseWriter,
	r *http.Request,
	targetAddr string,
	traceID string,
	chain *pluginPkg.PluginChain,
	pluginCtx *pluginPkg.PluginContext,
) error {
	requestBody, err := io.ReadAll(r.Body)
	if err != nil {
		return fmt.Errorf("读取请求体失败: %w", err)
	}
	r.Body.Close()

	md := p.extractGRPCMetadata(r)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	ctx = metadata.NewOutgoingContext(ctx, md)

	conn, err := grpc.DialContext(ctx, targetAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.ForceCodec(&rawCodec{}),
		),
	)
	if err != nil {
		return fmt.Errorf("连接 gRPC 服务器失败: %w", err)
	}
	defer conn.Close()

	fullMethodName := r.URL.Path
	if len(fullMethodName) > 0 && fullMethodName[0] == '/' {
		fullMethodName = fullMethodName[1:]
	}

	var responseBytes []byte
	err = conn.Invoke(ctx, fullMethodName, requestBody, &responseBytes)
	if err != nil {
		st, ok := status.FromError(err)
		if ok {
			p.writeGRPCStatus(w, st.Code(), st.Message())
			return nil
		}
		return fmt.Errorf("gRPC 调用失败: %w", err)
	}

	w.Header().Set("Content-Type", "application/grpc")
	w.Header().Set("grpc-status", "0")
	w.Header().Set("grpc-message", "")

	compressedFlag := byte(0)
	messageLength := len(responseBytes)

	w.Write([]byte{compressedFlag})
	w.Write(encodeUint32(uint32(messageLength)))
	w.Write(responseBytes)

	return nil
}

func (p *GRPCProxy) extractGRPCMetadata(r *http.Request) metadata.MD {
	md := metadata.MD{}

	for key, values := range r.Header {
		if key == "Content-Type" || key == "Content-Length" {
			continue
		}
		
		lowerKey := strings.ToLower(key)
		
		if strings.HasPrefix(lowerKey, "grpc-") || strings.HasPrefix(lowerKey, ":") {
			continue
		}
		
		md.Set(lowerKey, values...)
	}

	return md
}

func (p *GRPCProxy) writeGRPCError(w http.ResponseWriter, code codes.Code, message string) {
	w.Header().Set("Content-Type", "application/grpc")
	w.Header().Set("grpc-status", fmt.Sprintf("%d", code))
	w.Header().Set("grpc-message", message)
	w.WriteHeader(http.StatusOK)
}

func (p *GRPCProxy) writeGRPCStatus(w http.ResponseWriter, code codes.Code, message string) {
	w.Header().Set("Content-Type", "application/grpc")
	w.Header().Set("grpc-status", fmt.Sprintf("%d", code))
	w.Header().Set("grpc-message", message)
	w.WriteHeader(http.StatusOK)
}

func (p *GRPCProxy) setEffectivePluginsHeader(w http.ResponseWriter, plugins []*pluginPkg.EffectivePlugin) {
	if len(plugins) == 0 {
		return
	}

	pluginNames := make([]string, len(plugins))
	for i, ep := range plugins {
		pluginNames[i] = ep.Name() + ":" + ep.SourceScope.String()
	}

	w.Header().Set("X-Effective-Plugins", strings.Join(pluginNames, ","))
}

func (p *GRPCProxy) logGRPCAccess(
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
		zap.Int("grpc_status", statusCode),
		zap.Duration("latency", latency),
		zap.String("trace_id", traceID),
		zap.String("grpc_status_text", status),
		zap.String("user_agent", r.UserAgent()),
	}

	p.logger.Info("grpc_access", fields...)
}

func (p *GRPCProxy) ServeWithContext(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	p.ServeHTTP(w, r.WithContext(ctx))
}

type rawCodec struct{}

func (rawCodec) Marshal(v interface{}) ([]byte, error) {
	if b, ok := v.([]byte); ok {
		return b, nil
	}
	return nil, fmt.Errorf("rawCodec: expected []byte, got %T", v)
}

func (rawCodec) Unmarshal(data []byte, v interface{}) error {
	if b, ok := v.(*[]byte); ok {
		*b = data
		return nil
	}
	return fmt.Errorf("rawCodec: expected *[]byte, got %T", v)
}

func (rawCodec) Name() string {
	return "raw"
}

func encodeUint32(v uint32) []byte {
	return []byte{
		byte(v >> 24),
		byte(v >> 16),
		byte(v >> 8),
		byte(v),
	}
}
