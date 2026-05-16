package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	pluginPkg "github.com/taichirain/portkey/internal/data/plugin"
	"github.com/taichirain/portkey/internal/data/plugin/auth"
	"github.com/taichirain/portkey/internal/data/health"
	"github.com/taichirain/portkey/internal/data/ratelimit"
	"github.com/taichirain/portkey/internal/data/snapshot"
	"github.com/taichirain/portkey/internal/domain/target"
	"go.uber.org/zap"
)

const (
	TraceIDHeader = "X-Trace-Id"
)

type ctxKey string

const (
	ctxKeyPluginContext ctxKey = "plugin_context"
	ctxKeyPluginChain   ctxKey = "plugin_chain"
	ctxKeyEffectivePlugins ctxKey = "effective_plugins"
	ctxKeyStartTime     ctxKey = "start_time"
	ctxKeySelectedTarget ctxKey = "selected_target"
	ctxKeyRetryAttempt  ctxKey = "retry_attempt"
	ctxKeyRequestBody   ctxKey = "request_body"
)

type Proxy struct {
	logger               *zap.Logger
	reverseProxy         *httputil.ReverseProxy
	currentSnapshot      atomic.Pointer[snapshot.ConfigSnapshot]
	metrics              *Metrics
	pluginRegistry       *pluginPkg.PluginRegistry
	pluginChainBuilder   *pluginPkg.PluginChainBuilder
	healthManager        *health.HealthManager
	activeChecker        *health.ActiveHealthChecker
	websocketProxy       *WebSocketProxy
	grpcProxy            *GRPCProxy
	dynamicPluginManager *pluginPkg.DynamicPluginManager
}

const (
	metricsWindowBuckets = 12
	metricsBucketSeconds = 5
)

type Metrics struct {
	requestsTotal   int64
	requestsActive  int64
	responseLatency int64
	errorsTotal     int64

	requestBuckets    [metricsWindowBuckets]int64
	errorBuckets      [metricsWindowBuckets]int64
	latencyBuckets    [metricsWindowBuckets]int64
	bucketTimestamps  [metricsWindowBuckets]int64
	currentBucket     int32
	bucketMu          sync.RWMutex

	status2xx int64
	status3xx int64
	status4xx int64
	status5xx int64

	rateLimitedTotal int64
	policyHitTotal   int64
	startedAt        time.Time
}

func NewMetrics() *Metrics {
	return &Metrics{
		startedAt: time.Now(),
	}
}

func (m *Metrics) recordRequest(statusCode int, latency time.Duration, rateLimited, policyHit bool) {
	now := time.Now().Unix()
	bucket := int32((now / metricsBucketSeconds) % metricsWindowBuckets)

	m.bucketMu.RLock()
	currentBkt := atomic.LoadInt32(&m.currentBucket)
	m.bucketMu.RUnlock()

	if currentBkt != bucket {
		m.bucketMu.Lock()
		if atomic.LoadInt32(&m.currentBucket) != bucket {
			atomic.StoreInt32(&m.currentBucket, bucket)
			atomic.StoreInt64(&m.requestBuckets[bucket], 0)
			atomic.StoreInt64(&m.errorBuckets[bucket], 0)
			atomic.StoreInt64(&m.latencyBuckets[bucket], 0)
			atomic.StoreInt64(&m.bucketTimestamps[bucket], now)
		}
		m.bucketMu.Unlock()
	}

	atomic.AddInt64(&m.requestBuckets[bucket], 1)
	atomic.AddInt64(&m.latencyBuckets[bucket], latency.Microseconds())

	if statusCode >= 500 {
		atomic.AddInt64(&m.errorBuckets[bucket], 1)
	}

	switch {
	case statusCode >= 200 && statusCode < 300:
		atomic.AddInt64(&m.status2xx, 1)
	case statusCode >= 300 && statusCode < 400:
		atomic.AddInt64(&m.status3xx, 1)
	case statusCode >= 400 && statusCode < 500:
		atomic.AddInt64(&m.status4xx, 1)
	default:
		atomic.AddInt64(&m.status5xx, 1)
	}

	if rateLimited {
		atomic.AddInt64(&m.rateLimitedTotal, 1)
	}
	if policyHit {
		atomic.AddInt64(&m.policyHitTotal, 1)
	}
}

func (m *Metrics) isRateLimitError(err error) bool {
	var pErr *pluginPkg.PluginError
	if errors.As(err, &pErr) {
		return pErr.Plugin == ratelimit.RateLimitPluginName
	}
	return false
}

func (m *Metrics) calculateWindowMetrics() (qps, errorRate, avgLatencyMs float64, requestsInWindow int64) {
	now := time.Now().Unix()
	windowCutoff := now - int64(metricsWindowBuckets*metricsBucketSeconds)

	var totalRequests int64
	var totalErrors int64
	var totalLatencyUs int64

	for i := 0; i < metricsWindowBuckets; i++ {
		ts := atomic.LoadInt64(&m.bucketTimestamps[i])
		if ts == 0 || ts <= windowCutoff {
			continue
		}
		reqs := atomic.LoadInt64(&m.requestBuckets[i])
		errs := atomic.LoadInt64(&m.errorBuckets[i])
		latency := atomic.LoadInt64(&m.latencyBuckets[i])

		totalRequests += reqs
		totalErrors += errs
		totalLatencyUs += latency
	}

	requestsInWindow = totalRequests
	windowSeconds := float64(metricsWindowBuckets * metricsBucketSeconds)

	if totalRequests > 0 {
		qps = float64(totalRequests) / windowSeconds
		errorRate = float64(totalErrors) / float64(totalRequests)
		avgLatencyMs = float64(totalLatencyUs) / float64(totalRequests) / 1000.0
	}

	return qps, errorRate, avgLatencyMs, totalRequests
}

func (m *Metrics) getUptimeSeconds() float64 {
	return time.Since(m.startedAt).Seconds()
}

func (m *Metrics) getStatusDistribution() StatusDistribution {
	return StatusDistribution{
		Status2xx: atomic.LoadInt64(&m.status2xx),
		Status3xx: atomic.LoadInt64(&m.status3xx),
		Status4xx: atomic.LoadInt64(&m.status4xx),
		Status5xx: atomic.LoadInt64(&m.status5xx),
	}
}

type StatusDistribution struct {
	Status2xx int64 `json:"2xx"`
	Status3xx int64 `json:"3xx"`
	Status4xx int64 `json:"4xx"`
	Status5xx int64 `json:"5xx"`
}

type EnhancedMetricsSnapshot struct {
	RequestsTotal   int64              `json:"requests_total"`
	RequestsActive  int64              `json:"requests_active"`
	ResponseLatency int64              `json:"response_latency_us"`
	ErrorsTotal     int64              `json:"errors_total"`
	QPS1m           float64            `json:"qps_1m"`
	ErrorRate1m     float64            `json:"error_rate_1m"`
	AvgLatencyMs1m  float64            `json:"avg_latency_ms_1m"`
	RateLimitedTotal int64             `json:"rate_limited_total"`
	PolicyHitTotal   int64             `json:"policy_hit_total"`
	UptimeSeconds    float64           `json:"uptime_seconds"`
	StatusDistribution StatusDistribution `json:"status_distribution"`
}

func NewProxy(logger *zap.Logger) *Proxy {
	registry := pluginPkg.NewPluginRegistry()
	builder := pluginPkg.NewPluginChainBuilder(registry)
	healthManager := health.GetGlobalHealthManager()

	auth.RegisterAuthPlugins(registry)
	ratelimit.RegisterRateLimitPlugin(registry)
	health.Register(registry)

	activeChecker := health.NewActiveHealthChecker(healthManager, logger)
	activeChecker.Start()

	websocketProxy := NewWebSocketProxy(logger, registry, builder)
	grpcProxy := NewGRPCProxy(logger, registry, builder)
	dynamicPluginManager := pluginPkg.NewDynamicPluginManager(logger, registry)

	p := &Proxy{
		logger:               logger,
		metrics:              NewMetrics(),
		pluginRegistry:       registry,
		pluginChainBuilder:   builder,
		healthManager:        healthManager,
		activeChecker:        activeChecker,
		websocketProxy:       websocketProxy,
		grpcProxy:            grpcProxy,
		dynamicPluginManager: dynamicPluginManager,
	}

	p.reverseProxy = &httputil.ReverseProxy{
		Director:       p.director,
		ModifyResponse: p.modifyResponse,
		ErrorHandler:   p.errorHandler,
	}

	return p
}

func (p *Proxy) HealthManager() *health.HealthManager {
	return p.healthManager
}

func (p *Proxy) Stop() {
	if p.activeChecker != nil {
		p.activeChecker.Stop()
	}
}

func (p *Proxy) PluginRegistry() *pluginPkg.PluginRegistry {
	return p.pluginRegistry
}

func (p *Proxy) UpdateSnapshot(snap *snapshot.ConfigSnapshot) {
	p.currentSnapshot.Store(snap)
	if p.websocketProxy != nil {
		p.websocketProxy.UpdateSnapshot(snap)
	}
	if p.grpcProxy != nil {
		p.grpcProxy.UpdateSnapshot(snap)
	}
	p.logger.Info("配置快照已更新", zap.Stringer("revision", snap.RevisionID))
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	atomic.AddInt64(&p.metrics.requestsActive, 1)
	defer atomic.AddInt64(&p.metrics.requestsActive, -1)

	traceID := r.Header.Get(TraceIDHeader)
	if traceID == "" {
		traceID = uuid.New().String()
		r.Header.Set(TraceIDHeader, traceID)
	}
	w.Header().Set(TraceIDHeader, traceID)

	if p.websocketProxy != nil && p.websocketProxy.IsWebSocketUpgrade(r) {
		p.logger.Debug("检测到 WebSocket 请求，转发到 WebSocket 代理",
			zap.String("trace_id", traceID),
			zap.String("path", r.URL.Path),
		)
		p.websocketProxy.ServeHTTP(w, r)
		latency := time.Since(start)
		atomic.AddInt64(&p.metrics.requestsTotal, 1)
		atomic.AddInt64(&p.metrics.responseLatency, latency.Microseconds())
		p.metrics.recordRequest(http.StatusSwitchingProtocols, latency, false, false)
		return
	}

	if p.grpcProxy != nil && p.grpcProxy.IsGRPCRequest(r) {
		p.logger.Debug("检测到 gRPC 请求，转发到 gRPC 代理",
			zap.String("trace_id", traceID),
			zap.String("path", r.URL.Path),
		)
		p.grpcProxy.ServeHTTP(w, r)
		latency := time.Since(start)
		atomic.AddInt64(&p.metrics.requestsTotal, 1)
		atomic.AddInt64(&p.metrics.responseLatency, latency.Microseconds())
		p.metrics.recordRequest(http.StatusOK, latency, false, false)
		return
	}

	snap := p.currentSnapshot.Load()
	if snap == nil {
		p.logger.Warn("没有可用的配置快照", zap.String("trace_id", traceID))
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		latency := time.Since(start)
		atomic.AddInt64(&p.metrics.errorsTotal, 1)
		p.metrics.recordRequest(http.StatusServiceUnavailable, latency, false, false)
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
		latency := time.Since(start)
		atomic.AddInt64(&p.metrics.errorsTotal, 1)
		p.logAccess(r, http.StatusNotFound, latency, traceID)
		p.metrics.recordRequest(http.StatusNotFound, latency, false, false)
		return
	}

	p.logger.Debug("路由匹配成功",
		zap.String("trace_id", traceID),
		zap.Stringer("route_id", matched.Route.ID),
		zap.Stringer("original_service_id", matched.OriginalService.ID),
		zap.String("original_service_name", matched.OriginalService.Name),
		zap.Stringer("effective_service_id", matched.EffectiveService.ID),
		zap.String("effective_service_name", matched.EffectiveService.Name),
		zap.Bool("traffic_policy_hit", matched.TrafficPolicyHit),
	)

	if matched.TrafficPolicyHit {
		p.logger.Info("流量策略命中",
			zap.String("trace_id", traceID),
			zap.Stringer("hit_policy_id", matched.HitPolicyID),
			zap.String("hit_policy_type", matched.HitPolicyType),
			zap.Stringer("from_service_id", matched.OriginalService.ID),
			zap.String("from_service_name", matched.OriginalService.Name),
			zap.Stringer("to_service_id", matched.EffectiveService.ID),
			zap.String("to_service_name", matched.EffectiveService.Name),
		)
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
		latency := time.Since(start)
		atomic.AddInt64(&p.metrics.errorsTotal, 1)
		p.metrics.recordRequest(http.StatusInternalServerError, latency, false, matched.TrafficPolicyHit)
		return
	}

	var requestBody []byte
	if r.Body != nil {
		requestBody, _ = io.ReadAll(r.Body)
		r.Body.Close()
		r.Body = io.NopCloser(bytes.NewBuffer(requestBody))
	}

	ctx := r.Context()
	ctx = context.WithValue(ctx, "matched_route", matched)
	ctx = context.WithValue(ctx, "trace_id", traceID)
	ctx = context.WithValue(ctx, ctxKeyPluginContext, pluginCtx)
	ctx = context.WithValue(ctx, ctxKeyPluginChain, chain)
	ctx = context.WithValue(ctx, ctxKeyEffectivePlugins, effectivePlugins)
	ctx = context.WithValue(ctx, ctxKeyStartTime, start)
	ctx = context.WithValue(ctx, ctxKeyRequestBody, requestBody)
	r = r.WithContext(ctx)

	w.Header().Set("X-Route-ID", matched.Route.ID.String())
	w.Header().Set("X-Service-ID", matched.Service.ID.String())
	w.Header().Set("X-Original-Service-ID", matched.OriginalService.ID.String())
	w.Header().Set("X-Effective-Service-ID", matched.EffectiveService.ID.String())
	if matched.TrafficPolicyHit {
		w.Header().Set("X-Traffic-Policy-Hit", "true")
		w.Header().Set("X-Hit-Policy-ID", matched.HitPolicyID.String())
	} else {
		w.Header().Set("X-Traffic-Policy-Hit", "false")
	}
	p.setEffectivePluginsHeader(w, effectivePlugins)

	if err := chain.ExecuteOnRequest(pluginCtx); err != nil {
		p.logger.Error("OnRequest 插件执行失败",
			zap.Error(err),
			zap.String("trace_id", traceID),
		)
		_ = chain.ExecuteOnError(pluginCtx, err)
		latency := time.Since(start)
		isRateLimit := p.metrics.isRateLimitError(err)
		if isRateLimit {
			atomic.AddInt64(&p.metrics.requestsTotal, 1)
			atomic.AddInt64(&p.metrics.responseLatency, latency.Microseconds())
			p.metrics.recordRequest(http.StatusTooManyRequests, latency, true, matched.TrafficPolicyHit)
		} else {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			atomic.AddInt64(&p.metrics.errorsTotal, 1)
			p.metrics.recordRequest(http.StatusInternalServerError, latency, false, matched.TrafficPolicyHit)
		}
		return
	}

	if pluginCtx.IsShortCircuited() {
		p.logger.Debug("请求被插件短路",
			zap.String("trace_id", traceID),
		)
		latency := time.Since(start)
		atomic.AddInt64(&p.metrics.requestsTotal, 1)
		atomic.AddInt64(&p.metrics.responseLatency, latency.Microseconds())
		p.metrics.recordRequest(http.StatusOK, latency, false, matched.TrafficPolicyHit)
		return
	}

	p.serveWithRetry(w, r, chain, pluginCtx, 0)

	latency := time.Since(start)
	atomic.AddInt64(&p.metrics.requestsTotal, 1)
	atomic.AddInt64(&p.metrics.responseLatency, latency.Microseconds())
}

func (p *Proxy) serveWithRetry(w http.ResponseWriter, r *http.Request, chain *pluginPkg.PluginChain, pluginCtx *pluginPkg.PluginContext, attempt int) {
	traceID := r.Context().Value("trace_id").(string)
	matched := r.Context().Value("matched_route").(*snapshot.MatchedRoute)

	selectedTarget := p.selectHealthyTarget(r)
	if selectedTarget == nil {
		if matched.Balancer == nil && matched.Service.Host != "" {
			p.logger.Debug("服务直接使用 host/port 配置，无 balancer",
				zap.String("trace_id", traceID),
				zap.String("service", matched.Service.Name),
				zap.String("host", matched.Service.Host),
			)
		} else {
			p.logger.Error("没有可用的健康目标", zap.String("trace_id", traceID))
			atomic.AddInt64(&p.metrics.errorsTotal, 1)

			start := r.Context().Value(ctxKeyStartTime)
			var latency time.Duration
			if start != nil {
				latency = time.Since(start.(time.Time))
			}
			p.metrics.recordRequest(http.StatusServiceUnavailable, latency, false, matched.TrafficPolicyHit)

			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
			return
		}
	}

	var targetKey health.TargetKey
	if selectedTarget != nil {
		targetKey = health.NewTargetKey(
			pluginCtx.MatchedRoute.UpstreamID,
			selectedTarget.Target,
			selectedTarget.Port,
		)

		ctx := r.Context()
		ctx = context.WithValue(ctx, ctxKeySelectedTarget, selectedTarget)
		ctx = context.WithValue(ctx, ctxKeyRetryAttempt, attempt)
		r = r.WithContext(ctx)

		r.Header.Set("X-Selected-Target-Host", selectedTarget.Target)
		r.Header.Set("X-Selected-Target-Port", fmt.Sprintf("%d", selectedTarget.Port))
	}

	recoveryWriter := newResponseWriter(w)

	p.reverseProxy.ServeHTTP(recoveryWriter, r)

	statusCode := recoveryWriter.statusCode
	respErr := recoveryWriter.err

	if selectedTarget != nil {
		if respErr != nil || p.isFailureStatus(statusCode) {
			p.healthManager.RecordError(targetKey, p.isTimeoutError(respErr) || statusCode == http.StatusGatewayTimeout)
		} else {
			p.healthManager.RecordSuccess(targetKey)
		}
	}

	if respErr != nil || p.isFailureStatus(statusCode) {
		if p.shouldRetry(r, statusCode, respErr, attempt) {
			p.logger.Debug("准备重试请求",
				zap.String("trace_id", traceID),
				zap.Int("attempt", attempt+1),
				zap.Int("status", statusCode),
				zap.Error(respErr),
			)

			requestBody := r.Context().Value(ctxKeyRequestBody).([]byte)
			if len(requestBody) > 0 {
				r.Body = io.NopCloser(bytes.NewBuffer(requestBody))
			}

			backoff := p.calculateBackoff(attempt)
			if backoff > 0 {
				time.Sleep(backoff)
			}

			p.serveWithRetry(w, r, chain, pluginCtx, attempt+1)
			return
		}
	}

	if recoveryWriter.wroteHeader {
		for k, v := range recoveryWriter.Header() {
			for _, vv := range v {
				w.Header().Add(k, vv)
			}
		}
		w.WriteHeader(recoveryWriter.statusCode)
		if recoveryWriter.body != nil {
			w.Write(recoveryWriter.body.Bytes())
		}
	} else if respErr != nil {
		_ = chain.ExecuteOnError(pluginCtx, respErr)
		atomic.AddInt64(&p.metrics.errorsTotal, 1)

		start := r.Context().Value(ctxKeyStartTime)
		var latency time.Duration
		if start != nil {
			latency = time.Since(start.(time.Time))
		}
		p.metrics.recordRequest(http.StatusBadGateway, latency, false, matched.TrafficPolicyHit)

		http.Error(w, "Bad Gateway", http.StatusBadGateway)
	}
}

func (p *Proxy) selectHealthyTarget(r *http.Request) *target.Target {
	matched := r.Context().Value("matched_route").(*snapshot.MatchedRoute)
	traceID := r.Header.Get(TraceIDHeader)

	if matched.Balancer == nil {
		p.logger.Warn("目标选择失败：无负载均衡器",
			zap.String("trace_id", traceID),
			zap.Stringer("service_id", matched.Service.ID),
		)
		return nil
	}

	allTargets := matched.Balancer.Targets()
	if len(allTargets) == 0 {
		p.logger.Warn("目标选择失败：无可用目标",
			zap.String("trace_id", traceID),
			zap.Stringer("service_id", matched.Service.ID),
			zap.Stringer("upstream_id", matched.Service.UpstreamID),
		)
		return nil
	}

	primaryTarget, ok := matched.Balancer.Next()
	if !ok {
		p.logger.Warn("目标选择失败：负载均衡器无可用目标",
			zap.String("trace_id", traceID),
			zap.Stringer("service_id", matched.Service.ID),
		)
		return nil
	}

	primaryKey := health.NewTargetKey(matched.Service.UpstreamID, primaryTarget.Target, primaryTarget.Port)
	primaryHealthInfo := p.getTargetHealthInfo(primaryKey)
	primaryHealthy := p.healthManager.ShouldAllowRequest(primaryKey)

	if primaryHealthy {
		p.logger.Debug("主目标健康，选择主目标",
			zap.String("trace_id", traceID),
			zap.String("target", primaryTarget.Target),
			zap.Int("port", primaryTarget.Port),
			zap.String("health_status", primaryHealthInfo.Status),
			zap.String("circuit_state", primaryHealthInfo.CircuitState),
		)
		return primaryTarget
	}

	p.logger.Warn("主目标不健康，尝试备用目标",
		zap.String("trace_id", traceID),
		zap.String("primary_target", primaryTarget.Target),
		zap.Int("primary_port", primaryTarget.Port),
		zap.String("health_status", primaryHealthInfo.Status),
		zap.String("circuit_state", primaryHealthInfo.CircuitState),
		zap.Int32("consecutive_errors", primaryHealthInfo.ConsecutiveErrors),
	)

	for _, t := range allTargets {
		if t == primaryTarget {
			continue
		}
		key := health.NewTargetKey(matched.Service.UpstreamID, t.Target, t.Port)
		healthInfo := p.getTargetHealthInfo(key)
		if p.healthManager.ShouldAllowRequest(key) {
			p.logger.Info("选择备用目标",
				zap.String("trace_id", traceID),
				zap.String("primary_target", primaryTarget.Target),
				zap.String("backup_target", t.Target),
				zap.Int("backup_port", t.Port),
				zap.String("health_status", healthInfo.Status),
				zap.String("circuit_state", healthInfo.CircuitState),
			)
			return t
		}
		p.logger.Debug("备用目标不健康，跳过",
			zap.String("trace_id", traceID),
			zap.String("target", t.Target),
			zap.Int("port", t.Port),
			zap.String("health_status", healthInfo.Status),
			zap.String("circuit_state", healthInfo.CircuitState),
		)
	}

	p.logger.Error("所有目标都不健康，降级使用主目标",
		zap.String("trace_id", traceID),
		zap.String("primary_target", primaryTarget.Target),
		zap.Int("primary_port", primaryTarget.Port),
		zap.Int("total_targets", len(allTargets)),
	)
	return primaryTarget
}

func (p *Proxy) getTargetHealthInfo(key health.TargetKey) struct {
	Status            string
	CircuitState      string
	ConsecutiveErrors int32
	Failures          int64
} {
	h, ok := p.healthManager.GetTargetHealth(key)
	if !ok {
		return struct {
			Status            string
			CircuitState      string
			ConsecutiveErrors int32
			Failures          int64
		}{
			Status:            "unknown",
			CircuitState:      "closed",
			ConsecutiveErrors: 0,
			Failures:          0,
		}
	}
	metrics := h.GetMetrics()
	return struct {
		Status            string
		CircuitState      string
		ConsecutiveErrors int32
		Failures          int64
	}{
		Status:            metrics.Status.String(),
		CircuitState:      metrics.CircuitState.String(),
		ConsecutiveErrors: metrics.ConsecutiveErrors,
		Failures:          metrics.Failures,
	}
}

func (p *Proxy) shouldRetry(r *http.Request, statusCode int, err error, attempt int) bool {
	if attempt >= 2 {
		return false
	}

	method := r.Method
	retryableMethods := map[string]bool{
		"GET":  true,
		"HEAD": true,
	}

	if !retryableMethods[method] {
		return false
	}

	retryableStatuses := map[int]bool{
		http.StatusBadGateway:          true,
		http.StatusServiceUnavailable: true,
		http.StatusGatewayTimeout:     true,
	}

	if err != nil {
		return true
	}

	if retryableStatuses[statusCode] {
		return true
	}

	return false
}

func (p *Proxy) calculateBackoff(attempt int) time.Duration {
	base := 100 * time.Millisecond
	backoff := base * time.Duration(1<<uint(attempt))
	if backoff > time.Second {
		return time.Second
	}
	return backoff
}

func (p *Proxy) isFailureStatus(statusCode int) bool {
	failureStatuses := map[int]bool{
		http.StatusInternalServerError: true,
		http.StatusBadGateway:          true,
		http.StatusServiceUnavailable: true,
		http.StatusGatewayTimeout:     true,
	}
	return failureStatuses[statusCode]
}

func (p *Proxy) isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if uErr, ok := err.(*url.Error); ok {
		return uErr.Timeout()
	}
	return false
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
	wroteHeader bool
	body        *bytes.Buffer
	err         error
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
		body:           &bytes.Buffer{},
	}
}

func (w *responseWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.statusCode = code
		w.wroteHeader = true
	}
}

func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.body.Write(b)
}

func (p *Proxy) setEffectivePluginsHeader(w http.ResponseWriter, plugins []*pluginPkg.EffectivePlugin) {
	if len(plugins) == 0 {
		return
	}

	pluginNames := make([]string, len(plugins))
	for i, ep := range plugins {
		pluginNames[i] = ep.Name() + ":" + ep.SourceScope.String()
	}

	w.Header().Set("X-Effective-Plugins", strings.Join(pluginNames, ","))
}

func (p *Proxy) director(r *http.Request) {
	matched := r.Context().Value("matched_route").(*snapshot.MatchedRoute)
	traceID := r.Context().Value("trace_id").(string)

	var targetURL *url.URL
	var err error
	var selectedTarget *target.Target

	if preselectedTargetVal := r.Context().Value(ctxKeySelectedTarget); preselectedTargetVal != nil {
		selectedTarget = preselectedTargetVal.(*target.Target)
	}

	if matched.Balancer != nil {
		var t *target.Target
		var ok bool

		if selectedTarget != nil {
			t = selectedTarget
			ok = true
		} else {
			t, ok = matched.Balancer.Next()
		}

		if !ok {
			p.logger.Error("没有可用的目标", zap.String("trace_id", traceID))
			return
		}
		targetURL, err = url.Parse(fmt.Sprintf("%s://%s:%d", matched.Service.Protocol, t.Target, t.Port))
		p.logger.Debug("选择目标",
			zap.String("target", t.Target),
			zap.Int("port", t.Port),
			zap.String("trace_id", traceID),
		)
	} else if matched.Service.Host != "" {
		targetURL, err = url.Parse(fmt.Sprintf("%s://%s:%d", matched.Service.Protocol, matched.Service.Host, matched.Service.Port))
	} else {
		targetURL, err = url.Parse(fmt.Sprintf("%s://localhost", matched.Service.Protocol))
	}

	if err != nil {
		p.logger.Error("解析目标URL失败", zap.Error(err), zap.String("trace_id", traceID))
		return
	}

	targetQuery := targetURL.RawQuery

	originalPath := r.URL.Path
	if matched.Route.StripPath && len(matched.Route.Paths) > 0 {
		for _, path := range matched.Route.Paths {
			if strings.HasPrefix(originalPath, path) {
				r.URL.Path = strings.TrimPrefix(originalPath, path)
				if !strings.HasPrefix(r.URL.Path, "/") {
					r.URL.Path = "/" + r.URL.Path
				}
				break
			}
		}
	}

	r.URL.Scheme = targetURL.Scheme
	r.URL.Host = targetURL.Host
	r.URL.Path = singleJoiningSlash(targetURL.Path, r.URL.Path)
	if targetQuery == "" || r.URL.RawQuery == "" {
		r.URL.RawQuery = targetQuery + r.URL.RawQuery
	} else {
		r.URL.RawQuery = targetQuery + "&" + r.URL.RawQuery
	}

	if !matched.Route.PreserveHost {
		r.Host = targetURL.Host
	}

	if _, ok := r.Header["User-Agent"]; !ok {
		r.Header.Set("User-Agent", "")
	}

	r.Header.Set(TraceIDHeader, traceID)

	p.logger.Debug("代理请求",
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
		zap.String("host", r.URL.Host),
		zap.String("trace_id", traceID),
	)
}

func (p *Proxy) modifyResponse(resp *http.Response) error {
	traceID := resp.Request.Header.Get(TraceIDHeader)
	start := resp.Request.Context().Value(ctxKeyStartTime)
	var latency time.Duration
	if start != nil {
		latency = time.Since(start.(time.Time))
	}

	pluginCtxVal := resp.Request.Context().Value(ctxKeyPluginContext)
	chainVal := resp.Request.Context().Value(ctxKeyPluginChain)

	if pluginCtxVal != nil && chainVal != nil {
		pluginCtx := pluginCtxVal.(*pluginPkg.PluginContext)
		chain := chainVal.(*pluginPkg.PluginChain)

		if err := chain.ExecuteOnResponse(pluginCtx, resp); err != nil {
			p.logger.Error("OnResponse 插件执行失败",
				zap.Error(err),
				zap.String("trace_id", traceID),
			)
			return err
		}
	}

	p.logAccess(resp.Request, resp.StatusCode, latency, traceID)

	policyHit := false
	if matchedVal := resp.Request.Context().Value("matched_route"); matchedVal != nil {
		if matched, ok := matchedVal.(*snapshot.MatchedRoute); ok {
			policyHit = matched.TrafficPolicyHit
		}
	}
	p.metrics.recordRequest(resp.StatusCode, latency, false, policyHit)

	return nil
}

func (p *Proxy) errorHandler(w http.ResponseWriter, r *http.Request, err error) {
	traceID := r.Header.Get(TraceIDHeader)
	p.logger.Error("代理错误",
		zap.Error(err),
		zap.String("trace_id", traceID),
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
	)

	pluginCtxVal := r.Context().Value(ctxKeyPluginContext)
	chainVal := r.Context().Value(ctxKeyPluginChain)

	if pluginCtxVal != nil && chainVal != nil {
		pluginCtx := pluginCtxVal.(*pluginPkg.PluginContext)
		chain := chainVal.(*pluginPkg.PluginChain)

		_ = chain.ExecuteOnError(pluginCtx, err)
	}

	atomic.AddInt64(&p.metrics.errorsTotal, 1)

	start := r.Context().Value(ctxKeyStartTime)
	var latency time.Duration
	if start != nil {
		latency = time.Since(start.(time.Time))
	}

	policyHit := false
	if matchedVal := r.Context().Value("matched_route"); matchedVal != nil {
		if matched, ok := matchedVal.(*snapshot.MatchedRoute); ok {
			policyHit = matched.TrafficPolicyHit
		}
	}

	p.metrics.recordRequest(http.StatusBadGateway, latency, false, policyHit)

	http.Error(w, "Bad Gateway", http.StatusBadGateway)
}

func (p *Proxy) logAccess(r *http.Request, statusCode int, latency time.Duration, traceID string) {
	fields := []zap.Field{
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
		zap.String("host", r.Host),
		zap.String("remote_addr", r.RemoteAddr),
		zap.Int("status", statusCode),
		zap.Duration("latency", latency),
		zap.String("trace_id", traceID),
		zap.String("user_agent", r.UserAgent()),
	}

	if matchedVal := r.Context().Value("matched_route"); matchedVal != nil {
		if matched, ok := matchedVal.(*snapshot.MatchedRoute); ok {
			fields = append(fields,
				zap.Stringer("route_id", matched.Route.ID),
				zap.Stringer("original_service_id", matched.OriginalService.ID),
				zap.String("original_service_name", matched.OriginalService.Name),
				zap.Stringer("effective_service_id", matched.EffectiveService.ID),
				zap.String("effective_service_name", matched.EffectiveService.Name),
				zap.Bool("traffic_policy_hit", matched.TrafficPolicyHit),
			)
			if matched.TrafficPolicyHit {
				fields = append(fields,
					zap.Stringer("hit_policy_id", matched.HitPolicyID),
					zap.String("hit_policy_type", matched.HitPolicyType),
				)
			}

			if len(matched.PolicyMatchDetails) > 0 {
				policyDetails := make([]map[string]interface{}, 0, len(matched.PolicyMatchDetails))
				for _, pd := range matched.PolicyMatchDetails {
					policyDetail := map[string]interface{}{
						"policy_id":        pd.PolicyID.String(),
						"policy_name":      pd.PolicyName,
						"policy_type":      pd.PolicyType,
						"priority":         pd.Priority,
						"enabled":          pd.Enabled,
						"matched":          pd.Matched,
						"selected":         pd.Selected,
						"target_service_id": pd.TargetServiceID.String(),
					}
					if pd.SkipReason != "" {
						policyDetail["skip_reason"] = pd.SkipReason
					}
					if len(pd.ConditionDetails) > 0 {
						condDetails := make([]map[string]interface{}, 0, len(pd.ConditionDetails))
						for _, cd := range pd.ConditionDetails {
							condDetail := map[string]interface{}{
								"condition_type": cd.ConditionType,
								"matched":        cd.Matched,
							}
							if cd.Reason != "" {
								condDetail["reason"] = cd.Reason
							}
							if cd.ActualValue != "" {
								condDetail["actual_value"] = cd.ActualValue
							}
							if cd.ExpectedValue != "" {
								condDetail["expected_value"] = cd.ExpectedValue
							}
							if cd.Operator != "" {
								condDetail["operator"] = string(cd.Operator)
							}
							condDetails = append(condDetails, condDetail)
						}
						policyDetail["conditions"] = condDetails
					}
					policyDetails = append(policyDetails, policyDetail)
				}
				fields = append(fields, zap.Any("policy_match_details", policyDetails))
			}
		}
	}

	p.logger.Info("access", fields...)
}

func (p *Proxy) Metrics() *MetricsSnapshot {
	return &MetricsSnapshot{
		RequestsTotal:   atomic.LoadInt64(&p.metrics.requestsTotal),
		RequestsActive:  atomic.LoadInt64(&p.metrics.requestsActive),
		ResponseLatency: atomic.LoadInt64(&p.metrics.responseLatency),
		ErrorsTotal:     atomic.LoadInt64(&p.metrics.errorsTotal),
	}
}

func (p *Proxy) EnhancedMetrics() *EnhancedMetricsSnapshot {
	qps, errorRate, avgLatencyMs, _ := p.metrics.calculateWindowMetrics()
	return &EnhancedMetricsSnapshot{
		RequestsTotal:      atomic.LoadInt64(&p.metrics.requestsTotal),
		RequestsActive:     atomic.LoadInt64(&p.metrics.requestsActive),
		ResponseLatency:    atomic.LoadInt64(&p.metrics.responseLatency),
		ErrorsTotal:        atomic.LoadInt64(&p.metrics.errorsTotal),
		QPS1m:              qps,
		ErrorRate1m:        errorRate,
		AvgLatencyMs1m:     avgLatencyMs,
		RateLimitedTotal:   atomic.LoadInt64(&p.metrics.rateLimitedTotal),
		PolicyHitTotal:     atomic.LoadInt64(&p.metrics.policyHitTotal),
		UptimeSeconds:      p.metrics.getUptimeSeconds(),
		StatusDistribution: p.metrics.getStatusDistribution(),
	}
}

type MetricsSnapshot struct {
	RequestsTotal   int64
	RequestsActive  int64
	ResponseLatency int64
	ErrorsTotal     int64
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}
