package health

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

type HealthStatus int

const (
	StatusUnknown HealthStatus = iota
	StatusHealthy
	StatusUnhealthy
	StatusDegraded
)

func (s HealthStatus) String() string {
	switch s {
	case StatusHealthy:
		return "healthy"
	case StatusUnhealthy:
		return "unhealthy"
	case StatusDegraded:
		return "degraded"
	default:
		return "unknown"
	}
}

type CircuitBreakerState int

const (
	CircuitClosed CircuitBreakerState = iota
	CircuitOpen
	CircuitHalfOpen
)

func (s CircuitBreakerState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

type TargetKey struct {
	UpstreamID uuid.UUID
	Target     string
	Port       int
}

func NewTargetKey(upstreamID uuid.UUID, target string, port int) TargetKey {
	return TargetKey{
		UpstreamID: upstreamID,
		Target:     target,
		Port:       port,
	}
}

type TargetHealth struct {
	Key               TargetKey
	Status            HealthStatus
	LastCheckedAt     time.Time
	LastSuccessAt     time.Time
	LastFailureAt     time.Time
	ConsecutiveErrors int32
	ConsecutiveSuccesses int32
	CircuitState      CircuitBreakerState
	LastStateChangeAt time.Time
	Failures          int64
	Successes         int64
	Timeouts          int64
}

func NewTargetHealth(key TargetKey) *TargetHealth {
	return &TargetHealth{
		Key:               key,
		Status:            StatusHealthy,
		CircuitState:      CircuitClosed,
		ConsecutiveSuccesses: 0,
		ConsecutiveErrors: 0,
	}
}

func (h *TargetHealth) RecordSuccess() {
	atomic.StoreInt32(&h.ConsecutiveErrors, 0)
	atomic.AddInt32(&h.ConsecutiveSuccesses, 1)
	atomic.AddInt64(&h.Successes, 1)
	h.LastSuccessAt = time.Now()
	h.LastCheckedAt = time.Now()
}

func (h *TargetHealth) RecordError(isTimeout bool) {
	atomic.StoreInt32(&h.ConsecutiveSuccesses, 0)
	atomic.AddInt32(&h.ConsecutiveErrors, 1)
	atomic.AddInt64(&h.Failures, 1)
	if isTimeout {
		atomic.AddInt64(&h.Timeouts, 1)
	}
	h.LastFailureAt = time.Now()
	h.LastCheckedAt = time.Now()
}

func (h *TargetHealth) SetStatus(status HealthStatus) {
	if h.Status != status {
		h.Status = status
		h.LastStateChangeAt = time.Now()
	}
}

func (h *TargetHealth) SetCircuitState(state CircuitBreakerState) {
	if h.CircuitState != state {
		h.CircuitState = state
		h.LastStateChangeAt = time.Now()
	}
}

func (h *TargetHealth) GetMetrics() TargetHealthMetrics {
	return TargetHealthMetrics{
		Status:            h.Status,
		CircuitState:      h.CircuitState,
		LastCheckedAt:     h.LastCheckedAt,
		LastSuccessAt:     h.LastSuccessAt,
		LastFailureAt:     h.LastFailureAt,
		ConsecutiveErrors: atomic.LoadInt32(&h.ConsecutiveErrors),
		Failures:          atomic.LoadInt64(&h.Failures),
		Successes:         atomic.LoadInt64(&h.Successes),
		Timeouts:          atomic.LoadInt64(&h.Timeouts),
	}
}

type TargetHealthMetrics struct {
	Status            HealthStatus
	CircuitState      CircuitBreakerState
	LastCheckedAt     time.Time
	LastSuccessAt     time.Time
	LastFailureAt     time.Time
	ConsecutiveErrors int32
	Failures          int64
	Successes         int64
	Timeouts          int64
}

type PassiveHealthCheckConfig struct {
	Enabled            bool
	UnhealthyThreshold int32
	HealthyThreshold   int32
	FailureTimeouts    []int
	FailureStatuses    []int
	CheckInterval      time.Duration
}

func DefaultPassiveHealthCheckConfig() *PassiveHealthCheckConfig {
	return &PassiveHealthCheckConfig{
		Enabled:            true,
		UnhealthyThreshold: 5,
		HealthyThreshold:   3,
		FailureStatuses:    []int{500, 502, 503, 504},
		CheckInterval:      time.Second,
	}
}

type ActiveHealthCheckConfig struct {
	Enabled               bool
	Interval              time.Duration
	Timeout               time.Duration
	HTTPPath              string
	HTTPMethod            string
	UnhealthyThreshold    int
	HealthyThreshold      int
	HTTPSServerName       string
	HTTPSVerifyCertificate bool
	Headers               map[string][]string
	Port                  int
}

func DefaultActiveHealthCheckConfig() *ActiveHealthCheckConfig {
	return &ActiveHealthCheckConfig{
		Enabled:               false,
		Interval:              10 * time.Second,
		Timeout:               1 * time.Second,
		HTTPPath:              "/",
		HTTPMethod:            "GET",
		UnhealthyThreshold:    3,
		HealthyThreshold:      2,
		HTTPSVerifyCertificate: true,
	}
}

type CircuitBreakerConfig struct {
	Enabled              bool
	FailureThreshold     int32
	RecoveryTime         time.Duration
	HalfOpenRequests     int
	HalfOpenSuccessThreshold int
}

func DefaultCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		Enabled:                  true,
		FailureThreshold:         10,
		RecoveryTime:             30 * time.Second,
		HalfOpenRequests:         5,
		HalfOpenSuccessThreshold: 3,
	}
}

type RetryConfig struct {
	Enabled          bool
	MaxRetries       int
	RetryOnStatuses  []int
	RetryOnMethods   []string
	RetryOnErrors    bool
	RetryOnTimeouts  bool
	BackoffFactor    time.Duration
	MaxBackoff       time.Duration
}

func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		Enabled:         false,
		MaxRetries:      3,
		RetryOnStatuses: []int{502, 503, 504},
		RetryOnMethods:  []string{"GET", "HEAD"},
		RetryOnErrors:   true,
		RetryOnTimeouts: true,
		BackoffFactor:   100 * time.Millisecond,
		MaxBackoff:      1 * time.Second,
	}
}

type UpstreamHealthConfig struct {
	UpstreamID        uuid.UUID
	PassiveCheck      *PassiveHealthCheckConfig
	ActiveCheck       *ActiveHealthCheckConfig
	CircuitBreaker    *CircuitBreakerConfig
	Retry             *RetryConfig
}

func NewUpstreamHealthConfig(upstreamID uuid.UUID) *UpstreamHealthConfig {
	return &UpstreamHealthConfig{
		UpstreamID:     upstreamID,
		PassiveCheck:   DefaultPassiveHealthCheckConfig(),
		ActiveCheck:    DefaultActiveHealthCheckConfig(),
		CircuitBreaker: DefaultCircuitBreakerConfig(),
		Retry:          DefaultRetryConfig(),
	}
}

type HealthManager struct {
	mu             sync.RWMutex
	targets        map[TargetKey]*TargetHealth
	upstreamConfigs map[uuid.UUID]*UpstreamHealthConfig
}

func NewHealthManager() *HealthManager {
	return &HealthManager{
		targets:        make(map[TargetKey]*TargetHealth),
		upstreamConfigs: make(map[uuid.UUID]*UpstreamHealthConfig),
	}
}

func (m *HealthManager) RegisterUpstream(upstreamID uuid.UUID, config *UpstreamHealthConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if config == nil {
		config = NewUpstreamHealthConfig(upstreamID)
	}
	m.upstreamConfigs[upstreamID] = config
}

func (m *HealthManager) UnregisterUpstream(upstreamID uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.upstreamConfigs, upstreamID)
	for key := range m.targets {
		if key.UpstreamID == upstreamID {
			delete(m.targets, key)
		}
	}
}

func (m *HealthManager) GetUpstreamConfig(upstreamID uuid.UUID) (*UpstreamHealthConfig, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	config, ok := m.upstreamConfigs[upstreamID]
	return config, ok
}

func (m *HealthManager) GetOrCreateTargetHealth(key TargetKey) *TargetHealth {
	m.mu.Lock()
	defer m.mu.Unlock()
	if h, ok := m.targets[key]; ok {
		return h
	}
	h := NewTargetHealth(key)
	m.targets[key] = h
	return h
}

func (m *HealthManager) GetTargetHealth(key TargetKey) (*TargetHealth, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	h, ok := m.targets[key]
	return h, ok
}

func (m *HealthManager) IsTargetHealthy(key TargetKey) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	h, ok := m.targets[key]
	if !ok {
		return true
	}
	if h.Status != StatusHealthy {
		return false
	}
	if h.CircuitState == CircuitOpen {
		return false
	}
	return true
}

func (m *HealthManager) GetAllTargetHealth() map[TargetKey]TargetHealthMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[TargetKey]TargetHealthMetrics)
	for key, h := range m.targets {
		result[key] = h.GetMetrics()
	}
	return result
}

func (m *HealthManager) GetUpstreamTargets(upstreamID uuid.UUID) []*TargetHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*TargetHealth
	for key, h := range m.targets {
		if key.UpstreamID == upstreamID {
			result = append(result, h)
		}
	}
	return result
}

func (m *HealthManager) RecordSuccess(key TargetKey) {
	h := m.GetOrCreateTargetHealth(key)
	h.RecordSuccess()

	m.mu.RLock()
	config, ok := m.upstreamConfigs[key.UpstreamID]
	m.mu.RUnlock()

	if ok && config.PassiveCheck != nil && config.PassiveCheck.Enabled {
		if atomic.LoadInt32(&h.ConsecutiveSuccesses) >= config.PassiveCheck.HealthyThreshold {
			if h.Status == StatusUnhealthy {
				h.SetStatus(StatusHealthy)
			}
		}
	}

	if ok && config.CircuitBreaker != nil && config.CircuitBreaker.Enabled {
		if h.CircuitState == CircuitHalfOpen {
			if atomic.LoadInt32(&h.ConsecutiveSuccesses) >= int32(config.CircuitBreaker.HalfOpenSuccessThreshold) {
				h.SetCircuitState(CircuitClosed)
				h.SetStatus(StatusHealthy)
			}
		}
	}
}

func (m *HealthManager) RecordError(key TargetKey, isTimeout bool) {
	h := m.GetOrCreateTargetHealth(key)
	h.RecordError(isTimeout)

	m.mu.RLock()
	config, ok := m.upstreamConfigs[key.UpstreamID]
	m.mu.RUnlock()

	if ok && config.PassiveCheck != nil && config.PassiveCheck.Enabled {
		if atomic.LoadInt32(&h.ConsecutiveErrors) >= config.PassiveCheck.UnhealthyThreshold {
			h.SetStatus(StatusUnhealthy)
		}
	}

	if ok && config.CircuitBreaker != nil && config.CircuitBreaker.Enabled {
		if h.CircuitState == CircuitHalfOpen {
			h.SetCircuitState(CircuitOpen)
		} else if h.CircuitState == CircuitClosed {
			if atomic.LoadInt32(&h.ConsecutiveErrors) >= config.CircuitBreaker.FailureThreshold {
				h.SetCircuitState(CircuitOpen)
			}
		}
	}
}

func (m *HealthManager) CheckCircuitRecovery(key TargetKey) bool {
	m.mu.RLock()
	config, ok := m.upstreamConfigs[key.UpstreamID]
	m.mu.RUnlock()

	if !ok || config.CircuitBreaker == nil || !config.CircuitBreaker.Enabled {
		return false
	}

	h := m.GetOrCreateTargetHealth(key)
	if h.CircuitState != CircuitOpen {
		return false
	}

	if time.Since(h.LastStateChangeAt) >= config.CircuitBreaker.RecoveryTime {
		h.SetCircuitState(CircuitHalfOpen)
		atomic.StoreInt32(&h.ConsecutiveErrors, 0)
		atomic.StoreInt32(&h.ConsecutiveSuccesses, 0)
		return true
	}

	return false
}

func (m *HealthManager) ShouldAllowRequest(key TargetKey) bool {
	h := m.GetOrCreateTargetHealth(key)

	if h.CircuitState == CircuitOpen {
		return m.CheckCircuitRecovery(key)
	}

	if h.Status == StatusUnhealthy {
		return false
	}

	return true
}

func (m *HealthManager) ForceHealthy(key TargetKey) {
	h := m.GetOrCreateTargetHealth(key)
	h.SetStatus(StatusHealthy)
	h.SetCircuitState(CircuitClosed)
	atomic.StoreInt32(&h.ConsecutiveErrors, 0)
	atomic.StoreInt32(&h.ConsecutiveSuccesses, 0)
}

func (m *HealthManager) ForceUnhealthy(key TargetKey) {
	h := m.GetOrCreateTargetHealth(key)
	h.SetStatus(StatusUnhealthy)
}
