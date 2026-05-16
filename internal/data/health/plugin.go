package health

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	pluginPkg "github.com/taichirain/portkey/internal/data/plugin"
	"github.com/taichirain/portkey/internal/data/snapshot"
	"github.com/taichirain/portkey/internal/domain/target"
	"go.uber.org/zap"
)

const (
	HealthCheckPluginName = "health-check"
	RetryPluginName       = "retry"
)

var (
	globalHealthManager *HealthManager
	healthManagerOnce   sync.Once
)

func GetGlobalHealthManager() *HealthManager {
	healthManagerOnce.Do(func() {
		globalHealthManager = NewHealthManager()
	})
	return globalHealthManager
}

type HealthCheckPlugin struct {
	config        *HealthCheckPluginConfig
	healthManager *HealthManager
	logger        *zap.Logger
}

type HealthCheckPluginConfig struct {
	PassiveCheckEnabled bool
	UnhealthyThreshold   int32
	HealthyThreshold     int32
	CircuitBreakerEnabled bool
	FailureThreshold     int32
	RecoveryTime         time.Duration
}

func parseHealthCheckConfig(config map[string]interface{}) *HealthCheckPluginConfig {
	cfg := &HealthCheckPluginConfig{
		PassiveCheckEnabled:   true,
		UnhealthyThreshold:    5,
		HealthyThreshold:      3,
		CircuitBreakerEnabled: true,
		FailureThreshold:      10,
		RecoveryTime:          30 * time.Second,
	}

	if v, ok := config["passive_check_enabled"].(bool); ok {
		cfg.PassiveCheckEnabled = v
	}

	if v, ok := config["unhealthy_threshold"].(float64); ok && v > 0 {
		cfg.UnhealthyThreshold = int32(v)
	}

	if v, ok := config["healthy_threshold"].(float64); ok && v > 0 {
		cfg.HealthyThreshold = int32(v)
	}

	if v, ok := config["circuit_breaker_enabled"].(bool); ok {
		cfg.CircuitBreakerEnabled = v
	}

	if v, ok := config["failure_threshold"].(float64); ok && v > 0 {
		cfg.FailureThreshold = int32(v)
	}

	if v, ok := config["recovery_time"].(string); ok {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.RecoveryTime = d
		}
	}

	return cfg
}

func NewHealthCheckFactory() pluginPkg.PluginFactory {
	return &healthCheckFactory{}
}

type healthCheckFactory struct{}

func (f *healthCheckFactory) Name() string {
	return HealthCheckPluginName
}

func (f *healthCheckFactory) Create(config map[string]interface{}) (pluginPkg.Plugin, error) {
	cfg := parseHealthCheckConfig(config)
	logger, _ := zap.NewDevelopment()

	return &HealthCheckPlugin{
		config:        cfg,
		healthManager: GetGlobalHealthManager(),
		logger:        logger,
	}, nil
}

func (p *HealthCheckPlugin) Name() string {
	return HealthCheckPluginName
}

func (p *HealthCheckPlugin) OnRequest(ctx *pluginPkg.PluginContext) error {
	return nil
}

func (p *HealthCheckPlugin) OnResponse(ctx *pluginPkg.PluginContext, resp *http.Response) error {
	key, ok := p.extractTargetKey(ctx)
	if !ok {
		return nil
	}

	if p.isFailureStatus(resp.StatusCode) {
		p.healthManager.RecordError(key, resp.StatusCode == http.StatusGatewayTimeout)
		p.logger.Debug("Recorded error for target",
			zap.String("target", key.Target),
			zap.Int("port", key.Port),
			zap.Int("status", resp.StatusCode),
		)
	} else {
		p.healthManager.RecordSuccess(key)
	}

	return nil
}

func (p *HealthCheckPlugin) OnError(ctx *pluginPkg.PluginContext, err error) error {
	key, ok := p.extractTargetKey(ctx)
	if !ok {
		return nil
	}

	isTimeout := p.isTimeoutError(err)
	p.healthManager.RecordError(key, isTimeout)

	p.logger.Debug("Recorded error for target",
		zap.String("target", key.Target),
		zap.Int("port", key.Port),
		zap.Error(err),
	)

	return nil
}

func (p *HealthCheckPlugin) extractTargetKey(ctx *pluginPkg.PluginContext) (TargetKey, bool) {
	if ctx.MatchedRoute == nil {
		return TargetKey{}, false
	}

	targetHost := ctx.Request.Header.Get("X-Selected-Target-Host")
	targetPort := ctx.Request.Header.Get("X-Selected-Target-Port")

	if targetHost == "" || targetPort == "" {
		return TargetKey{}, false
	}

	port := 80
	fmt.Sscanf(targetPort, "%d", &port)

	return NewTargetKey(ctx.MatchedRoute.UpstreamID, targetHost, port), true
}

func (p *HealthCheckPlugin) isFailureStatus(statusCode int) bool {
	failureStatuses := []int{
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	}

	for _, s := range failureStatuses {
		if statusCode == s {
			return true
		}
	}
	return false
}

func (p *HealthCheckPlugin) isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if uErr, ok := err.(*url.Error); ok {
		return uErr.Timeout()
	}
	return false
}

type RetryPlugin struct {
	config        *RetryPluginConfig
	healthManager *HealthManager
	logger        *zap.Logger
}

type RetryPluginConfig struct {
	Enabled         bool
	MaxRetries      int
	RetryOnStatuses []int
	RetryOnMethods  []string
	RetryOnErrors   bool
	RetryOnTimeouts bool
	BackoffFactor   time.Duration
	MaxBackoff      time.Duration
}

func parseRetryConfig(config map[string]interface{}) *RetryPluginConfig {
	cfg := &RetryPluginConfig{
		Enabled:         false,
		MaxRetries:      3,
		RetryOnStatuses: []int{502, 503, 504},
		RetryOnMethods:  []string{"GET", "HEAD"},
		RetryOnErrors:   true,
		RetryOnTimeouts: true,
		BackoffFactor:   100 * time.Millisecond,
		MaxBackoff:      1 * time.Second,
	}

	if v, ok := config["enabled"].(bool); ok {
		cfg.Enabled = v
	}

	if v, ok := config["max_retries"].(float64); ok && v > 0 {
		cfg.MaxRetries = int(v)
	}

	if v, ok := config["retry_on_errors"].(bool); ok {
		cfg.RetryOnErrors = v
	}

	if v, ok := config["retry_on_timeouts"].(bool); ok {
		cfg.RetryOnTimeouts = v
	}

	if v, ok := config["backoff_factor"].(string); ok {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.BackoffFactor = d
		}
	}

	if v, ok := config["max_backoff"].(string); ok {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.MaxBackoff = d
		}
	}

	return cfg
}

func NewRetryFactory() pluginPkg.PluginFactory {
	return &retryFactory{}
}

type retryFactory struct{}

func (f *retryFactory) Name() string {
	return RetryPluginName
}

func (f *retryFactory) Create(config map[string]interface{}) (pluginPkg.Plugin, error) {
	cfg := parseRetryConfig(config)
	logger, _ := zap.NewDevelopment()

	return &RetryPlugin{
		config:        cfg,
		healthManager: GetGlobalHealthManager(),
		logger:        logger,
	}, nil
}

func (p *RetryPlugin) Name() string {
	return RetryPluginName
}

func (p *RetryPlugin) OnRequest(ctx *pluginPkg.PluginContext) error {
	ctx.SetAttribute("retry_config", p.config)
	return nil
}

func (p *RetryPlugin) OnResponse(ctx *pluginPkg.PluginContext, resp *http.Response) error {
	return nil
}

func (p *RetryPlugin) OnError(ctx *pluginPkg.PluginContext, err error) error {
	return nil
}

func (p *RetryPlugin) ShouldRetry(method string, statusCode int, err error, attempt int) bool {
	if !p.config.Enabled {
		return false
	}

	if attempt >= p.config.MaxRetries {
		return false
	}

	methodMatch := false
	for _, m := range p.config.RetryOnMethods {
		if method == m {
			methodMatch = true
			break
		}
	}
	if !methodMatch {
		return false
	}

	if err != nil && p.config.RetryOnErrors {
		isTimeout := p.isTimeoutError(err)
		if isTimeout && p.config.RetryOnTimeouts {
			return true
		}
		if !isTimeout {
			return true
		}
	}

	if statusCode > 0 {
		for _, s := range p.config.RetryOnStatuses {
			if statusCode == s {
				return true
			}
		}
	}

	return false
}

func (p *RetryPlugin) CalculateBackoff(attempt int) time.Duration {
	backoff := p.config.BackoffFactor * time.Duration(1<<uint(attempt))
	if backoff > p.config.MaxBackoff {
		return p.config.MaxBackoff
	}
	return backoff
}

func (p *RetryPlugin) isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if uErr, ok := err.(*url.Error); ok {
		return uErr.Timeout()
	}
	if err == context.DeadlineExceeded {
		return true
	}
	return false
}

type ActiveHealthChecker struct {
	healthManager *HealthManager
	logger        *zap.Logger
	stopChan      chan struct{}
	wg            sync.WaitGroup
}

func NewActiveHealthChecker(healthManager *HealthManager, logger *zap.Logger) *ActiveHealthChecker {
	return &ActiveHealthChecker{
		healthManager: healthManager,
		logger:        logger,
		stopChan:      make(chan struct{}),
	}
}

func (c *ActiveHealthChecker) Start() {
	c.wg.Add(1)
	go c.run()
}

func (c *ActiveHealthChecker) Stop() {
	close(c.stopChan)
	c.wg.Wait()
}

func (c *ActiveHealthChecker) run() {
	defer c.wg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopChan:
			return
		case <-ticker.C:
			c.checkAllTargets()
		}
	}
}

func (c *ActiveHealthChecker) checkAllTargets() {
	allTargets := c.healthManager.GetAllTargetHealth()

	for key := range allTargets {
		config, ok := c.healthManager.GetUpstreamConfig(key.UpstreamID)
		if !ok || config.ActiveCheck == nil || !config.ActiveCheck.Enabled {
			continue
		}

		targetHealth := c.healthManager.GetOrCreateTargetHealth(key)

		if time.Since(targetHealth.LastCheckedAt) < config.ActiveCheck.Interval {
			continue
		}

		go c.checkTarget(key, config.ActiveCheck)
	}
}

func (c *ActiveHealthChecker) checkTarget(key TargetKey, config *ActiveHealthCheckConfig) {
	client := &http.Client{
		Timeout: config.Timeout,
	}

	scheme := "http"
	if config.HTTPSVerifyCertificate || config.HTTPSServerName != "" {
		scheme = "https"
	}

	targetURL := fmt.Sprintf("%s://%s:%d%s", scheme, key.Target, key.Port, config.HTTPPath)

	req, err := http.NewRequest(config.HTTPMethod, targetURL, nil)
	if err != nil {
		c.logger.Debug("Active health check failed to create request",
			zap.String("target", key.Target),
			zap.Int("port", key.Port),
			zap.Error(err),
		)
		c.healthManager.RecordError(key, false)
		return
	}

	for k, values := range config.Headers {
		for _, v := range values {
			req.Header.Add(k, v)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		c.logger.Debug("Active health check request failed",
			zap.String("target", key.Target),
			zap.Int("port", key.Port),
			zap.Error(err),
		)
		c.healthManager.RecordError(key, c.isTimeoutError(err))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		c.healthManager.RecordSuccess(key)
		c.logger.Debug("Active health check succeeded",
			zap.String("target", key.Target),
			zap.Int("port", key.Port),
			zap.Int("status", resp.StatusCode),
		)
	} else {
		c.healthManager.RecordError(key, resp.StatusCode == http.StatusGatewayTimeout)
		c.logger.Debug("Active health check failed with status",
			zap.String("target", key.Target),
			zap.Int("port", key.Port),
			zap.Int("status", resp.StatusCode),
		)
	}
}

func (c *ActiveHealthChecker) isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	if uErr, ok := err.(*url.Error); ok {
		return uErr.Timeout()
	}
	return false
}

func RegisterHealthPlugins(registry *pluginPkg.PluginRegistry) {
	registry.Register(NewHealthCheckFactory())
	registry.Register(NewRetryFactory())
}

type HealthAwareBalancer struct {
	*snapshot.Balancer
	healthManager *HealthManager
}

func NewHealthAwareBalancer(balancer *snapshot.Balancer, healthManager *HealthManager) *HealthAwareBalancer {
	return &HealthAwareBalancer{
		Balancer:      balancer,
		healthManager: healthManager,
	}
}

func (b *HealthAwareBalancer) Next() (*target.Target, bool) {
	allTargets := b.Targets()
	if len(allTargets) == 0 {
		return nil, false
	}

	healthyTargets := make([]*target.Target, 0)
	for _, t := range allTargets {
		key := NewTargetKey(b.Upstream().ID, t.Target, t.Port)
		if b.healthManager.ShouldAllowRequest(key) {
			healthyTargets = append(healthyTargets, t)
		}
	}

	if len(healthyTargets) == 0 {
		return nil, false
	}

	idx := int(b.NextIndex()) % len(healthyTargets)
	return healthyTargets[idx], true
}

func (b *HealthAwareBalancer) NextIndex() uint32 {
	return 0
}
