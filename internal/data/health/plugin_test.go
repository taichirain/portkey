package health

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"
)

// === RetryPlugin ShouldRetry 测试 ===

func TestRetryPlugin_ShouldRetry_DisabledPlugin(t *testing.T) {
	p := &RetryPlugin{
		config: &RetryPluginConfig{Enabled: false},
	}

	if p.ShouldRetry("GET", 503, nil, 0) {
		t.Error("Should not retry when plugin is disabled")
	}
}

func TestRetryPlugin_ShouldRetry_ExceedsMaxRetries(t *testing.T) {
	p := &RetryPlugin{
		config: &RetryPluginConfig{
			Enabled:         true,
			MaxRetries:      2,
			RetryOnMethods:  []string{"GET"},
			RetryOnStatuses: []int{502, 503, 504},
		},
	}

	if p.ShouldRetry("GET", 503, nil, 2) {
		t.Error("Should not retry when attempt >= maxRetries")
	}
	if !p.ShouldRetry("GET", 503, nil, 1) {
		t.Error("Should retry when attempt < maxRetries")
	}
}

func TestRetryPlugin_ShouldRetry_NonRetryableMethod(t *testing.T) {
	p := &RetryPlugin{
		config: &RetryPluginConfig{
			Enabled:         true,
			MaxRetries:      3,
			RetryOnMethods:  []string{"GET", "HEAD"},
			RetryOnStatuses: []int{502, 503, 504},
		},
	}

	if p.ShouldRetry("POST", 503, nil, 0) {
		t.Error("Should not retry POST requests")
	}
	if p.ShouldRetry("PUT", 503, nil, 0) {
		t.Error("Should not retry PUT requests")
	}
	if !p.ShouldRetry("GET", 503, nil, 0) {
		t.Error("Should retry GET requests")
	}
	if !p.ShouldRetry("HEAD", 503, nil, 0) {
		t.Error("Should retry HEAD requests")
	}
}

func TestRetryPlugin_ShouldRetry_RetryableStatuses(t *testing.T) {
	p := &RetryPlugin{
		config: &RetryPluginConfig{
			Enabled:         true,
			MaxRetries:      3,
			RetryOnMethods:  []string{"GET"},
			RetryOnStatuses: []int{502, 503, 504},
		},
	}

	retryableStatuses := []int{502, 503, 504}
	for _, status := range retryableStatuses {
		if !p.ShouldRetry("GET", status, nil, 0) {
			t.Errorf("Should retry status %d", status)
		}
	}

	nonRetryableStatuses := []int{200, 400, 404, 500}
	for _, status := range nonRetryableStatuses {
		if p.ShouldRetry("GET", status, nil, 0) {
			t.Errorf("Should not retry status %d", status)
		}
	}
}

func TestRetryPlugin_ShouldRetry_OnError(t *testing.T) {
	p := &RetryPlugin{
		config: &RetryPluginConfig{
			Enabled:         true,
			MaxRetries:      3,
			RetryOnMethods:  []string{"GET"},
			RetryOnErrors:   true,
			RetryOnTimeouts: true,
		},
	}

	// Generic error
	err := fmt.Errorf("connection refused")
	if !p.ShouldRetry("GET", 0, err, 0) {
		t.Error("Should retry on generic error")
	}

	// Timeout error
	timeoutErr := &url.Error{
		Op:  "Get",
		URL: "http://example.com",
		Err: &timeoutError{},
	}
	if !p.ShouldRetry("GET", 0, timeoutErr, 0) {
		t.Error("Should retry on timeout error")
	}
}

func TestRetryPlugin_ShouldRetry_RetryOnErrorsDisabled(t *testing.T) {
	p := &RetryPlugin{
		config: &RetryPluginConfig{
			Enabled:         true,
			MaxRetries:      3,
			RetryOnMethods:  []string{"GET"},
			RetryOnStatuses: []int{502, 503, 504},
			RetryOnErrors:   false,
		},
	}

	err := fmt.Errorf("connection refused")
	if p.ShouldRetry("GET", 0, err, 0) {
		t.Error("Should not retry on error when RetryOnErrors is false")
	}

	// But should still retry on retryable status
	if !p.ShouldRetry("GET", 503, nil, 0) {
		t.Error("Should still retry on retryable status")
	}
}

func TestRetryPlugin_ShouldRetry_RetryOnTimeoutsDisabled(t *testing.T) {
	p := &RetryPlugin{
		config: &RetryPluginConfig{
			Enabled:         true,
			MaxRetries:      3,
			RetryOnMethods:  []string{"GET"},
			RetryOnErrors:   true,
			RetryOnTimeouts: false,
		},
	}

	timeoutErr := &url.Error{
		Op:  "Get",
		URL: "http://example.com",
		Err: &timeoutError{},
	}
	if p.ShouldRetry("GET", 0, timeoutErr, 0) {
		t.Error("Should not retry on timeout when RetryOnTimeouts is false")
	}

	// But should still retry on non-timeout error
	normalErr := fmt.Errorf("connection refused")
	if !p.ShouldRetry("GET", 0, normalErr, 0) {
		t.Error("Should retry on non-timeout error")
	}
}

func TestRetryPlugin_ShouldRetry_DeadlineExceeded(t *testing.T) {
	p := &RetryPlugin{
		config: &RetryPluginConfig{
			Enabled:         true,
			MaxRetries:      3,
			RetryOnMethods:  []string{"GET"},
			RetryOnErrors:   true,
			RetryOnTimeouts: true,
		},
	}

	if !p.ShouldRetry("GET", 0, context.DeadlineExceeded, 0) {
		t.Error("Should retry on context.DeadlineExceeded")
	}
}

// === RetryPlugin CalculateBackoff 测试 ===

func TestRetryPlugin_CalculateBackoff(t *testing.T) {
	p := &RetryPlugin{
		config: &RetryPluginConfig{
			BackoffFactor: 100 * time.Millisecond,
			MaxBackoff:    1 * time.Second,
		},
	}

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 100 * time.Millisecond},   // 100ms * 2^0 = 100ms
		{1, 200 * time.Millisecond},   // 100ms * 2^1 = 200ms
		{2, 400 * time.Millisecond},   // 100ms * 2^2 = 400ms
		{3, 800 * time.Millisecond},   // 100ms * 2^3 = 800ms
		{4, 1 * time.Second},          // 100ms * 2^4 = 1600ms, capped at 1s
		{5, 1 * time.Second},          // capped at 1s
		{10, 1 * time.Second},         // capped at 1s
	}

	for _, tt := range tests {
		got := p.CalculateBackoff(tt.attempt)
		if got != tt.want {
			t.Errorf("CalculateBackoff(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

// === parseHealthCheckConfig 测试 ===

func TestParseHealthCheckConfig_Defaults(t *testing.T) {
	cfg := parseHealthCheckConfig(map[string]interface{}{})

	if !cfg.PassiveCheckEnabled {
		t.Error("Default passive check should be enabled")
	}
	if cfg.UnhealthyThreshold != 5 {
		t.Errorf("Expected unhealthy threshold 5, got %d", cfg.UnhealthyThreshold)
	}
	if cfg.HealthyThreshold != 3 {
		t.Errorf("Expected healthy threshold 3, got %d", cfg.HealthyThreshold)
	}
	if !cfg.CircuitBreakerEnabled {
		t.Error("Default circuit breaker should be enabled")
	}
	if cfg.FailureThreshold != 10 {
		t.Errorf("Expected failure threshold 10, got %d", cfg.FailureThreshold)
	}
	if cfg.RecoveryTime != 30*time.Second {
		t.Errorf("Expected recovery time 30s, got %v", cfg.RecoveryTime)
	}
}

func TestParseHealthCheckConfig_Custom(t *testing.T) {
	cfg := parseHealthCheckConfig(map[string]interface{}{
		"passive_check_enabled":  false,
		"unhealthy_threshold":    float64(3),
		"healthy_threshold":      float64(5),
		"circuit_breaker_enabled": false,
		"failure_threshold":      float64(7),
		"recovery_time":          "10s",
	})

	if cfg.PassiveCheckEnabled {
		t.Error("Passive check should be disabled")
	}
	if cfg.UnhealthyThreshold != 3 {
		t.Errorf("Expected 3, got %d", cfg.UnhealthyThreshold)
	}
	if cfg.HealthyThreshold != 5 {
		t.Errorf("Expected 5, got %d", cfg.HealthyThreshold)
	}
	if cfg.CircuitBreakerEnabled {
		t.Error("Circuit breaker should be disabled")
	}
	if cfg.FailureThreshold != 7 {
		t.Errorf("Expected 7, got %d", cfg.FailureThreshold)
	}
	if cfg.RecoveryTime != 10*time.Second {
		t.Errorf("Expected 10s, got %v", cfg.RecoveryTime)
	}
}

// === parseRetryConfig 测试 ===

func TestParseRetryConfig_Defaults(t *testing.T) {
	cfg := parseRetryConfig(map[string]interface{}{})

	if cfg.Enabled {
		t.Error("Default retry should be disabled")
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("Expected 3, got %d", cfg.MaxRetries)
	}
	if !cfg.RetryOnErrors {
		t.Error("Default retry on errors should be true")
	}
	if !cfg.RetryOnTimeouts {
		t.Error("Default retry on timeouts should be true")
	}
	if cfg.BackoffFactor != 100*time.Millisecond {
		t.Errorf("Expected 100ms, got %v", cfg.BackoffFactor)
	}
	if cfg.MaxBackoff != 1*time.Second {
		t.Errorf("Expected 1s, got %v", cfg.MaxBackoff)
	}
}

func TestParseRetryConfig_Custom(t *testing.T) {
	cfg := parseRetryConfig(map[string]interface{}{
		"enabled":          true,
		"max_retries":      float64(5),
		"retry_on_errors":  false,
		"retry_on_timeouts": false,
		"backoff_factor":   "200ms",
		"max_backoff":      "5s",
	})

	if !cfg.Enabled {
		t.Error("Retry should be enabled")
	}
	if cfg.MaxRetries != 5 {
		t.Errorf("Expected 5, got %d", cfg.MaxRetries)
	}
	if cfg.RetryOnErrors {
		t.Error("Retry on errors should be false")
	}
	if cfg.RetryOnTimeouts {
		t.Error("Retry on timeouts should be false")
	}
	if cfg.BackoffFactor != 200*time.Millisecond {
		t.Errorf("Expected 200ms, got %v", cfg.BackoffFactor)
	}
	if cfg.MaxBackoff != 5*time.Second {
		t.Errorf("Expected 5s, got %v", cfg.MaxBackoff)
	}
}

// === HealthCheckPlugin isFailureStatus 测试 ===

func TestHealthCheckPlugin_IsFailureStatus(t *testing.T) {
	p := &HealthCheckPlugin{}

	failureStatuses := []int{500, 502, 503, 504}
	for _, s := range failureStatuses {
		if !p.isFailureStatus(s) {
			t.Errorf("Status %d should be failure", s)
		}
	}

	successStatuses := []int{200, 201, 301, 400, 404}
	for _, s := range successStatuses {
		if p.isFailureStatus(s) {
			t.Errorf("Status %d should not be failure", s)
		}
	}
}

// === HealthCheckPlugin isTimeoutError 测试 ===

func TestHealthCheckPlugin_IsTimeoutError(t *testing.T) {
	p := &HealthCheckPlugin{}

	if p.isTimeoutError(nil) {
		t.Error("nil error should not be timeout")
	}

	if p.isTimeoutError(fmt.Errorf("generic error")) {
		t.Error("Generic error should not be timeout")
	}

	timeoutErr := &url.Error{
		Op:  "Get",
		URL: "http://example.com",
		Err: &timeoutError{},
	}
	if !p.isTimeoutError(timeoutErr) {
		t.Error("url.Error with timeout should be detected")
	}
}

// timeoutError implements net.Error with Timeout() = true
type timeoutError struct{}

func (e *timeoutError) Error() string   { return "timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return true }
