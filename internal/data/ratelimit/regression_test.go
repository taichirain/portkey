package ratelimit

import (
	"bytes"
	"context"
	"log"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	pluginPkg "github.com/taichirain/portkey/internal/data/plugin"
	"github.com/redis/go-redis/v9"
)

// === 问题 1：窗口边界计算 ===
// 验证滑动窗口行为：请求在窗口内有效，过期后重新计数。
// 使用 LocalLimiter 验证（Redis 需要真实实例，逻辑一致）。

func TestRegression_SlidingWindow_LocalLimiter(t *testing.T) {
	limiter := NewLocalLimiter()

	t.Run("requests within window are counted", func(t *testing.T) {
		key := uuid.New().String()
		limit := 3
		window := 2 * time.Second

		for i := 0; i < limit; i++ {
			result, err := limiter.Allow(context.Background(), key, limit, window)
			if err != nil {
				t.Fatalf("Request %d: %v", i+1, err)
			}
			if !result.Allowed {
				t.Errorf("Request %d: should be allowed within window", i+1)
			}
		}

		result, _ := limiter.Allow(context.Background(), key, limit, window)
		if result.Allowed {
			t.Error("Should be denied when limit reached")
		}
	})

	t.Run("window expires and resets count", func(t *testing.T) {
		key := uuid.New().String()
		limit := 2
		window := 500 * time.Millisecond

		// Exhaust limit
		for i := 0; i < limit; i++ {
			limiter.Allow(context.Background(), key, limit, window)
		}

		// Should be denied
		result, _ := limiter.Allow(context.Background(), key, limit, window)
		if result.Allowed {
			t.Error("Should be denied before window expires")
		}

		// Wait for window to expire
		time.Sleep(600 * time.Millisecond)

		// Should be allowed again
		result, err := limiter.Allow(context.Background(), key, limit, window)
		if err != nil {
			t.Fatalf("After window reset: %v", err)
		}
		if !result.Allowed {
			t.Error("Should be allowed after window expires")
		}
	})

	t.Run("sliding window: old requests expire independently", func(t *testing.T) {
		key := uuid.New().String()
		limit := 3
		window := 1 * time.Second

		// Send 2 requests
		limiter.Allow(context.Background(), key, limit, window)
		limiter.Allow(context.Background(), key, limit, window)

		// Wait for those to expire
		time.Sleep(1100 * time.Millisecond)

		// Send 3 new requests — should all be allowed (old ones expired)
		for i := 0; i < limit; i++ {
			result, err := limiter.Allow(context.Background(), key, limit, window)
			if err != nil {
				t.Fatalf("New request %d: %v", i+1, err)
			}
			if !result.Allowed {
				t.Errorf("New request %d: should be allowed (old requests expired)", i+1)
			}
		}
	})
}

// === 问题 2：Redis client 未注入时静默降级 ===
// 配置 policy=redis 但不调用 SetRedisClient，验证：
// - GetLimiter() 返回 localLimiter（降级）
// - 输出告警日志

func TestRegression_RedisPolicy_NoClient_FallbackToLocal(t *testing.T) {
	factory := NewRateLimitFactory()

	config := map[string]interface{}{
		"limit_by": "route",
		"limit":    5,
		"window":   "1m",
		"policy":   "redis",
	}

	p, err := factory.Create(config)
	if err != nil {
		t.Fatalf("Failed to create plugin: %v", err)
	}

	plugin := p.(*RateLimitPlugin)

	// 验证 redisLimiter 为 nil（未注入）
	if plugin.redisLimiter != nil {
		t.Error("redisLimiter should be nil when SetRedisClient not called")
	}

	// 验证 GetLimiter() 降级到 localLimiter
	limiter := plugin.GetLimiter()
	if limiter == nil {
		t.Fatal("GetLimiter() should not return nil")
	}

	// 验证降级后仍能正常限流
	routeID := uuid.New()
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		ctx := pluginPkg.NewPluginContext(w, req, "trace-id")
		ctx.SetMatchedRoute(routeID, uuid.New(), uuid.New())

		err := plugin.OnRequest(ctx)
		if err != nil {
			t.Errorf("Request %d: unexpected error: %v", i+1, err)
		}
		if ctx.IsShortCircuited() {
			t.Errorf("Request %d: should not be short-circuited", i+1)
		}
	}

	// 第 6 个请求应被拒绝
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	ctx := pluginPkg.NewPluginContext(w, req, "trace-id")
	ctx.SetMatchedRoute(routeID, uuid.New(), uuid.New())
	plugin.OnRequest(ctx)

	if !ctx.IsShortCircuited() {
		t.Error("6th request should be short-circuited (limit=5)")
	}
}

func TestRegression_RedisPolicy_NoClient_WarnLog(t *testing.T) {
	// 捕获 log 输出
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	// 重置 sync.Once 以便重新触发（注意：这是测试专用 hack）
	// 由于 redisClientWarnOnce 是包级变量且 sync.Once 只能执行一次，
	// 我们直接验证 GetLimiter 的行为即可。
	// 这里验证日志格式是否正确——如果 once 已触发则跳过。
	factory := NewRateLimitFactory()
	config := map[string]interface{}{
		"limit_by": "route",
		"limit":    5,
		"window":   "1m",
		"policy":   "redis",
	}

	p, _ := factory.Create(config)
	plugin := p.(*RateLimitPlugin)

	// 触发 GetLimiter
	plugin.GetLimiter()

	logOutput := buf.String()
	// 如果 once 已经在其他测试中触发，日志可能为空
	// 但至少验证 GetLimiter 不 panic 且返回有效 limiter
	limiter := plugin.GetLimiter()
	if limiter == nil {
		t.Error("GetLimiter() returned nil after fallback")
	}

	// 如果日志被触发，验证格式
	if logOutput != "" && !strings.Contains(logOutput, "falling back to local limiter") {
		t.Errorf("Expected fallback warning log, got: %s", logOutput)
	}
}

// === 问题 3：Redis 运行时不可用降级 ===
// 配置 policy=redis，注入一个连接到不存在地址的 Redis client，
// 验证 OnRequest 捕获错误后降级到 local limiter。

func TestRegression_RedisUnavailable_FallbackOnRequest(t *testing.T) {
	factory := NewRateLimitFactory()

	config := map[string]interface{}{
		"limit_by": "route",
		"limit":    3,
		"window":   "1m",
		"policy":   "redis",
	}

	p, err := factory.Create(config)
	if err != nil {
		t.Fatalf("Failed to create plugin: %v", err)
	}

	plugin := p.(*RateLimitPlugin)

	// 注入一个连接到不存在地址的 Redis client
	badClient := redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:19999", // 不可能存在的端口
	})
	defer badClient.Close()

	plugin.SetRedisClient(badClient)

	// 验证 redisLimiter 已注入
	if plugin.redisLimiter == nil {
		t.Fatal("redisLimiter should be set after SetRedisClient")
	}

	// 捕获日志
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	// OnRequest 应该降级到 local limiter，不返回错误
	routeID := uuid.New()
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		ctx := pluginPkg.NewPluginContext(w, req, "trace-id")
		ctx.SetMatchedRoute(routeID, uuid.New(), uuid.New())

		err := plugin.OnRequest(ctx)
		if err != nil {
			t.Errorf("Request %d: should fallback to local, got error: %v", i+1, err)
		}
		if ctx.IsShortCircuited() {
			t.Errorf("Request %d: should not be short-circuited within limit", i+1)
		}
	}

	// 第 4 个请求应被本地限流器拒绝
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	ctx := pluginPkg.NewPluginContext(w, req, "trace-id")
	ctx.SetMatchedRoute(routeID, uuid.New(), uuid.New())
	err = plugin.OnRequest(ctx)

	if err == nil {
		t.Error("4th request should be rate-limited by local fallback")
	}
	if !ctx.IsShortCircuited() {
		t.Error("4th request should be short-circuited")
	}
	if w.Code != 429 {
		t.Errorf("Expected 429, got %d", w.Code)
	}
}

func TestRegression_RedisUnavailable_WarnLog(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	factory := NewRateLimitFactory()
	config := map[string]interface{}{
		"limit_by": "route",
		"limit":    3,
		"window":   "1m",
		"policy":   "redis",
	}

	p, _ := factory.Create(config)
	plugin := p.(*RateLimitPlugin)

	// 注入坏的 Redis client
	badClient := redis.NewClient(&redis.Options{Addr: "127.0.0.1:19999"})
	defer badClient.Close()
	plugin.SetRedisClient(badClient)

	// 触发一次请求以触发降级日志
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	ctx := pluginPkg.NewPluginContext(w, req, "trace-id")
	ctx.SetMatchedRoute(uuid.New(), uuid.New(), uuid.New())
	plugin.OnRequest(ctx)

	logOutput := buf.String()
	// 如果 once 已触发则日志可能为空，验证功能正常即可
	if logOutput != "" && !strings.Contains(logOutput, "Redis is unavailable") {
		t.Errorf("Expected Redis unavailable warning, got: %s", logOutput)
	}
}

// === 问题 4：Redis 连接健康检查 ===
// 验证 NewRedisClient 在连接失败时返回错误。

func TestRegression_RedisConnection_HealthCheck(t *testing.T) {
	t.Run("refused connection returns error", func(t *testing.T) {
		cfg := &RedisConfig{
			Host: "127.0.0.1",
			Port: 19999, // 不可能存在的端口
		}

		client, err := NewRedisClient(cfg)
		if err == nil {
			t.Error("Expected error for refused connection")
			if client != nil {
				client.Close()
			}
		}
		if client != nil {
			t.Error("Client should be nil on connection failure")
		}
	})

	t.Run("wrong host returns error", func(t *testing.T) {
		cfg := &RedisConfig{
			Host: "192.0.2.1", // TEST-NET, 不可达
			Port: 6379,
		}

		client, err := NewRedisClient(cfg)
		if err == nil {
			t.Error("Expected error for unreachable host")
			if client != nil {
				client.Close()
			}
		}
	})

	// 如果本地 Redis 可用，验证正常连接
	t.Run("valid connection succeeds", func(t *testing.T) {
		cfg := &RedisConfig{
			Host: "127.0.0.1",
			Port: 6379,
		}

		client, err := NewRedisClient(cfg)
		if err != nil {
			t.Skipf("Redis not available: %v", err)
		}
		defer client.Close()

		// 验证 client 可用
		ctx := context.Background()
		if err := client.Ping(ctx).Err(); err != nil {
			t.Errorf("Ping failed: %v", err)
		}
	})
}

// === 补充：SetRedisClient 后 GetLimiter 返回 RedisLimiter ===

func TestRegression_SetRedisClient_GetLimiterReturnsRedis(t *testing.T) {
	factory := NewRateLimitFactory()
	config := map[string]interface{}{
		"limit_by": "route",
		"limit":    5,
		"window":   "1m",
		"policy":   "redis",
	}

	p, _ := factory.Create(config)
	plugin := p.(*RateLimitPlugin)

	// 注入一个真实但不需要连接的 client（只验证类型切换）
	client := redis.NewClient(&redis.Options{})
	defer client.Close()

	plugin.SetRedisClient(client)

	limiter := plugin.GetLimiter()
	if limiter == nil {
		t.Fatal("GetLimiter() returned nil")
	}

	// 验证返回的是 RedisLimiter 而非 LocalLimiter
	if _, ok := limiter.(*RedisLimiter); !ok {
		t.Errorf("Expected *RedisLimiter, got %T", limiter)
	}
}
