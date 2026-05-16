package health

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// === HealthManager 单元测试 ===

func TestHealthManager_RecordSuccess(t *testing.T) {
	m := NewHealthManager()
	upstreamID := uuid.New()
	m.RegisterUpstream(upstreamID, nil)

	key := NewTargetKey(upstreamID, "10.0.0.1", 8080)

	m.RecordSuccess(key)
	m.RecordSuccess(key)
	m.RecordSuccess(key)

	h, ok := m.GetTargetHealth(key)
	if !ok {
		t.Fatal("Target health not found")
	}

	if h.ConsecutiveSuccesses != 3 {
		t.Errorf("Expected 3 consecutive successes, got %d", h.ConsecutiveSuccesses)
	}
	if h.Successes != 3 {
		t.Errorf("Expected 3 total successes, got %d", h.Successes)
	}
	if h.Status != StatusHealthy {
		t.Errorf("Expected healthy, got %v", h.Status)
	}
}

func TestHealthManager_RecordError(t *testing.T) {
	m := NewHealthManager()
	upstreamID := uuid.New()
	config := NewUpstreamHealthConfig(upstreamID)
	config.PassiveCheck.UnhealthyThreshold = 3
	m.RegisterUpstream(upstreamID, config)

	key := NewTargetKey(upstreamID, "10.0.0.1", 8080)

	m.RecordError(key, false)
	m.RecordError(key, false)

	h, _ := m.GetTargetHealth(key)
	if h.Status != StatusHealthy {
		t.Errorf("Should still be healthy after 2 errors, got %v", h.Status)
	}

	m.RecordError(key, false)

	h, _ = m.GetTargetHealth(key)
	if h.Status != StatusUnhealthy {
		t.Errorf("Expected unhealthy after 3 errors, got %v", h.Status)
	}
	if h.ConsecutiveErrors != 3 {
		t.Errorf("Expected 3 consecutive errors, got %d", h.ConsecutiveErrors)
	}
}

func TestHealthManager_RecordError_Timeout(t *testing.T) {
	m := NewHealthManager()
	upstreamID := uuid.New()
	m.RegisterUpstream(upstreamID, nil)

	key := NewTargetKey(upstreamID, "10.0.0.1", 8080)

	m.RecordError(key, true)

	h, _ := m.GetTargetHealth(key)
	if h.Timeouts != 1 {
		t.Errorf("Expected 1 timeout, got %d", h.Timeouts)
	}
	if h.Failures != 1 {
		t.Errorf("Expected 1 failure, got %d", h.Failures)
	}
}

func TestHealthManager_RecordError_ResetsConsecutiveSuccesses(t *testing.T) {
	m := NewHealthManager()
	upstreamID := uuid.New()
	m.RegisterUpstream(upstreamID, nil)

	key := NewTargetKey(upstreamID, "10.0.0.1", 8080)

	m.RecordSuccess(key)
	m.RecordSuccess(key)
	m.RecordSuccess(key)

	h, _ := m.GetTargetHealth(key)
	if h.ConsecutiveSuccesses != 3 {
		t.Errorf("Expected 3 consecutive successes, got %d", h.ConsecutiveSuccesses)
	}

	m.RecordError(key, false)

	h, _ = m.GetTargetHealth(key)
	if h.ConsecutiveSuccesses != 0 {
		t.Errorf("Consecutive successes should reset to 0, got %d", h.ConsecutiveSuccesses)
	}
	if h.ConsecutiveErrors != 1 {
		t.Errorf("Expected 1 consecutive error, got %d", h.ConsecutiveErrors)
	}
}

func TestHealthManager_RecordSuccess_ResetsConsecutiveErrors(t *testing.T) {
	m := NewHealthManager()
	upstreamID := uuid.New()
	m.RegisterUpstream(upstreamID, nil)

	key := NewTargetKey(upstreamID, "10.0.0.1", 8080)

	m.RecordError(key, false)
	m.RecordError(key, false)

	h, _ := m.GetTargetHealth(key)
	if h.ConsecutiveErrors != 2 {
		t.Errorf("Expected 2 consecutive errors, got %d", h.ConsecutiveErrors)
	}

	m.RecordSuccess(key)

	h, _ = m.GetTargetHealth(key)
	if h.ConsecutiveErrors != 0 {
		t.Errorf("Consecutive errors should reset to 0, got %d", h.ConsecutiveErrors)
	}
}

func TestHealthManager_RecordSuccess_RecoversUnhealthyTarget(t *testing.T) {
	m := NewHealthManager()
	upstreamID := uuid.New()
	config := NewUpstreamHealthConfig(upstreamID)
	config.PassiveCheck.UnhealthyThreshold = 2
	config.PassiveCheck.HealthyThreshold = 3
	m.RegisterUpstream(upstreamID, config)

	key := NewTargetKey(upstreamID, "10.0.0.1", 8080)

	// Make unhealthy
	m.RecordError(key, false)
	m.RecordError(key, false)

	h, _ := m.GetTargetHealth(key)
	if h.Status != StatusUnhealthy {
		t.Errorf("Expected unhealthy, got %v", h.Status)
	}

	// Recover
	m.RecordSuccess(key)
	m.RecordSuccess(key)
	m.RecordSuccess(key)

	h, _ = m.GetTargetHealth(key)
	if h.Status != StatusHealthy {
		t.Errorf("Expected healthy after recovery, got %v", h.Status)
	}
}

// === 熔断器单元测试 ===

func TestCircuitBreaker_ClosedToOpen(t *testing.T) {
	m := NewHealthManager()
	upstreamID := uuid.New()
	config := NewUpstreamHealthConfig(upstreamID)
	config.CircuitBreaker.FailureThreshold = 5
	config.CircuitBreaker.RecoveryTime = 1 * time.Second
	m.RegisterUpstream(upstreamID, config)

	key := NewTargetKey(upstreamID, "10.0.0.1", 8080)

	for i := 0; i < 5; i++ {
		m.RecordError(key, false)
	}

	h, _ := m.GetTargetHealth(key)
	if h.CircuitState != CircuitOpen {
		t.Errorf("Expected circuit open after 5 failures, got %v", h.CircuitState)
	}
}

func TestCircuitBreaker_OpenBlocksRequests(t *testing.T) {
	m := NewHealthManager()
	upstreamID := uuid.New()
	config := NewUpstreamHealthConfig(upstreamID)
	config.CircuitBreaker.FailureThreshold = 3
	config.CircuitBreaker.RecoveryTime = 10 * time.Second // long recovery
	m.RegisterUpstream(upstreamID, config)

	key := NewTargetKey(upstreamID, "10.0.0.1", 8080)

	for i := 0; i < 3; i++ {
		m.RecordError(key, false)
	}

	if m.ShouldAllowRequest(key) {
		t.Error("Should not allow request when circuit is open")
	}
}

func TestCircuitBreaker_OpenToHalfOpen_AfterRecoveryTime(t *testing.T) {
	m := NewHealthManager()
	upstreamID := uuid.New()
	config := NewUpstreamHealthConfig(upstreamID)
	config.CircuitBreaker.FailureThreshold = 3
	config.CircuitBreaker.RecoveryTime = 100 * time.Millisecond
	m.RegisterUpstream(upstreamID, config)

	key := NewTargetKey(upstreamID, "10.0.0.1", 8080)

	for i := 0; i < 3; i++ {
		m.RecordError(key, false)
	}

	h, _ := m.GetTargetHealth(key)
	if h.CircuitState != CircuitOpen {
		t.Fatalf("Expected circuit open, got %v", h.CircuitState)
	}

	// Before recovery time — should still block
	if m.ShouldAllowRequest(key) {
		t.Error("Should not allow request before recovery time")
	}

	time.Sleep(150 * time.Millisecond)

	// After recovery time — should allow (half-open)
	if !m.ShouldAllowRequest(key) {
		t.Error("Should allow request after recovery time (half-open)")
	}

	h, _ = m.GetTargetHealth(key)
	if h.CircuitState != CircuitHalfOpen {
		t.Errorf("Expected half-open after recovery, got %v", h.CircuitState)
	}
}

func TestCircuitBreaker_HalfOpen_Success_ClosesCircuit(t *testing.T) {
	m := NewHealthManager()
	upstreamID := uuid.New()
	config := NewUpstreamHealthConfig(upstreamID)
	config.CircuitBreaker.FailureThreshold = 3
	config.CircuitBreaker.RecoveryTime = 50 * time.Millisecond
	config.CircuitBreaker.HalfOpenSuccessThreshold = 2
	m.RegisterUpstream(upstreamID, config)

	key := NewTargetKey(upstreamID, "10.0.0.1", 8080)

	// Trip the circuit
	for i := 0; i < 3; i++ {
		m.RecordError(key, false)
	}

	time.Sleep(60 * time.Millisecond)
	m.ShouldAllowRequest(key) // triggers half-open

	h, _ := m.GetTargetHealth(key)
	if h.CircuitState != CircuitHalfOpen {
		t.Fatalf("Expected half-open, got %v", h.CircuitState)
	}

	// Succeed enough to close
	m.RecordSuccess(key)
	m.RecordSuccess(key)

	h, _ = m.GetTargetHealth(key)
	if h.CircuitState != CircuitClosed {
		t.Errorf("Expected circuit closed after half-open successes, got %v", h.CircuitState)
	}
	if h.Status != StatusHealthy {
		t.Errorf("Expected healthy after circuit closes, got %v", h.Status)
	}
}

func TestCircuitBreaker_HalfOpen_Failure_ReOpensCircuit(t *testing.T) {
	m := NewHealthManager()
	upstreamID := uuid.New()
	config := NewUpstreamHealthConfig(upstreamID)
	config.CircuitBreaker.FailureThreshold = 3
	config.CircuitBreaker.RecoveryTime = 50 * time.Millisecond
	config.CircuitBreaker.HalfOpenSuccessThreshold = 2
	m.RegisterUpstream(upstreamID, config)

	key := NewTargetKey(upstreamID, "10.0.0.1", 8080)

	// Trip the circuit
	for i := 0; i < 3; i++ {
		m.RecordError(key, false)
	}

	time.Sleep(60 * time.Millisecond)
	m.ShouldAllowRequest(key) // triggers half-open

	h, _ := m.GetTargetHealth(key)
	if h.CircuitState != CircuitHalfOpen {
		t.Fatalf("Expected half-open, got %v", h.CircuitState)
	}

	// Fail again — should re-open
	m.RecordError(key, false)
	m.RecordError(key, false)
	m.RecordError(key, false)

	h, _ = m.GetTargetHealth(key)
	if h.CircuitState != CircuitOpen {
		t.Errorf("Expected circuit re-opened after half-open failures, got %v", h.CircuitState)
	}
}

// === ShouldAllowRequest 测试 ===

func TestShouldAllowRequest_UnknownTarget(t *testing.T) {
	m := NewHealthManager()
	upstreamID := uuid.New()
	m.RegisterUpstream(upstreamID, nil)

	key := NewTargetKey(upstreamID, "10.0.0.1", 8080)

	// Unknown target should be allowed (default healthy)
	if !m.ShouldAllowRequest(key) {
		t.Error("Unknown target should be allowed by default")
	}
}

func TestShouldAllowRequest_UnhealthyTarget(t *testing.T) {
	m := NewHealthManager()
	upstreamID := uuid.New()
	config := NewUpstreamHealthConfig(upstreamID)
	config.PassiveCheck.UnhealthyThreshold = 2
	m.RegisterUpstream(upstreamID, config)

	key := NewTargetKey(upstreamID, "10.0.0.1", 8080)

	m.RecordError(key, false)
	m.RecordError(key, false)

	if m.ShouldAllowRequest(key) {
		t.Error("Unhealthy target should not be allowed")
	}
}

// === ForceHealthy / ForceUnhealthy ===

func TestForceHealthy(t *testing.T) {
	m := NewHealthManager()
	upstreamID := uuid.New()
	config := NewUpstreamHealthConfig(upstreamID)
	config.PassiveCheck.UnhealthyThreshold = 2
	config.CircuitBreaker.FailureThreshold = 3
	m.RegisterUpstream(upstreamID, config)

	key := NewTargetKey(upstreamID, "10.0.0.1", 8080)

	// Make unhealthy and trip circuit
	m.RecordError(key, false)
	m.RecordError(key, false)
	m.RecordError(key, false)

	h, _ := m.GetTargetHealth(key)
	if h.Status != StatusUnhealthy {
		t.Errorf("Expected unhealthy, got %v", h.Status)
	}
	if h.CircuitState != CircuitOpen {
		t.Errorf("Expected circuit open, got %v", h.CircuitState)
	}

	m.ForceHealthy(key)

	h, _ = m.GetTargetHealth(key)
	if h.Status != StatusHealthy {
		t.Errorf("Expected healthy after force, got %v", h.Status)
	}
	if h.CircuitState != CircuitClosed {
		t.Errorf("Expected circuit closed after force, got %v", h.CircuitState)
	}
	if h.ConsecutiveErrors != 0 {
		t.Errorf("Consecutive errors should be reset, got %d", h.ConsecutiveErrors)
	}
}

func TestForceUnhealthy(t *testing.T) {
	m := NewHealthManager()
	upstreamID := uuid.New()
	m.RegisterUpstream(upstreamID, nil)

	key := NewTargetKey(upstreamID, "10.0.0.1", 8080)

	m.ForceUnhealthy(key)

	h, _ := m.GetTargetHealth(key)
	if h.Status != StatusUnhealthy {
		t.Errorf("Expected unhealthy after force, got %v", h.Status)
	}
	if m.ShouldAllowRequest(key) {
		t.Error("Should not allow request to forced unhealthy target")
	}
}

// === UnregisterUpstream ===

func TestUnregisterUpstream(t *testing.T) {
	m := NewHealthManager()
	upstreamID := uuid.New()
	m.RegisterUpstream(upstreamID, nil)

	key := NewTargetKey(upstreamID, "10.0.0.1", 8080)
	m.RecordSuccess(key)

	_, ok := m.GetTargetHealth(key)
	if !ok {
		t.Fatal("Target should exist before unregister")
	}

	m.UnregisterUpstream(upstreamID)

	_, ok = m.GetTargetHealth(key)
	if ok {
		t.Error("Target should not exist after unregister")
	}

	_, ok = m.GetUpstreamConfig(upstreamID)
	if ok {
		t.Error("Upstream config should not exist after unregister")
	}
}

// === GetUpstreamTargets ===

func TestGetUpstreamTargets(t *testing.T) {
	m := NewHealthManager()
	upstreamID := uuid.New()
	m.RegisterUpstream(upstreamID, nil)

	otherUpstreamID := uuid.New()
	m.RegisterUpstream(otherUpstreamID, nil)

	key1 := NewTargetKey(upstreamID, "10.0.0.1", 8080)
	key2 := NewTargetKey(upstreamID, "10.0.0.2", 8080)
	key3 := NewTargetKey(otherUpstreamID, "10.0.0.3", 8080)

	m.RecordSuccess(key1)
	m.RecordSuccess(key2)
	m.RecordSuccess(key3)

	targets := m.GetUpstreamTargets(upstreamID)
	if len(targets) != 2 {
		t.Errorf("Expected 2 targets for upstream, got %d", len(targets))
	}
}

// === GetAllTargetHealth ===

func TestGetAllTargetHealth(t *testing.T) {
	m := NewHealthManager()
	upstreamID := uuid.New()
	m.RegisterUpstream(upstreamID, nil)

	key1 := NewTargetKey(upstreamID, "10.0.0.1", 8080)
	key2 := NewTargetKey(upstreamID, "10.0.0.2", 8080)

	m.RecordSuccess(key1)
	m.RecordError(key2, false)

	all := m.GetAllTargetHealth()
	if len(all) != 2 {
		t.Errorf("Expected 2 entries, got %d", len(all))
	}

	m1, ok := all[key1]
	if !ok {
		t.Fatal("key1 not found in health metrics")
	}
	if m1.Successes != 1 {
		t.Errorf("Expected 1 success for key1, got %d", m1.Successes)
	}

	m2, ok := all[key2]
	if !ok {
		t.Fatal("key2 not found in health metrics")
	}
	if m2.Failures != 1 {
		t.Errorf("Expected 1 failure for key2, got %d", m2.Failures)
	}
}

// === RetryConfig 单元测试 ===

func TestRetryConfig_DefaultValues(t *testing.T) {
	cfg := DefaultRetryConfig()

	if cfg.Enabled {
		t.Error("Default retry should be disabled")
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("Expected max retries 3, got %d", cfg.MaxRetries)
	}
	if len(cfg.RetryOnStatuses) != 3 {
		t.Errorf("Expected 3 retry statuses, got %d", len(cfg.RetryOnStatuses))
	}
	if len(cfg.RetryOnMethods) != 2 {
		t.Errorf("Expected 2 retry methods, got %d", len(cfg.RetryOnMethods))
	}
}

// === HealthStatus String ===

func TestHealthStatus_String(t *testing.T) {
	tests := []struct {
		status HealthStatus
		want   string
	}{
		{StatusHealthy, "healthy"},
		{StatusUnhealthy, "unhealthy"},
		{StatusDegraded, "degraded"},
		{StatusUnknown, "unknown"},
	}

	for _, tt := range tests {
		if got := tt.status.String(); got != tt.want {
			t.Errorf("HealthStatus(%d).String() = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestCircuitBreakerState_String(t *testing.T) {
	tests := []struct {
		state CircuitBreakerState
		want  string
	}{
		{CircuitClosed, "closed"},
		{CircuitOpen, "open"},
		{CircuitHalfOpen, "half-open"},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("CircuitBreakerState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}
