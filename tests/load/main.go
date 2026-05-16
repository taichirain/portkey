package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/data/proxy"
	"github.com/taichirain/portkey/internal/data/snapshot"
	"github.com/taichirain/portkey/internal/domain/credential"
	"github.com/taichirain/portkey/internal/domain/plugin"
	"github.com/taichirain/portkey/internal/domain/route"
	"github.com/taichirain/portkey/internal/domain/service"
	"go.uber.org/zap"
)

type TestConfig struct {
	Duration   time.Duration
	Concurrent int
	RateLimit  int
	Mode       string
}

type Metrics struct {
	TotalRequests   int64
	SuccessRequests int64
	ErrorRequests   int64
	RateLimited     int64
	Unauthorized    int64

	LatencySum      int64
	LatencyMin      int64
	LatencyMax      int64
	latencyMu       sync.Mutex

	StatusCodeCounts map[int]int64
	statusMu         sync.Mutex

	StartTime time.Time
}

func NewMetrics() *Metrics {
	return &Metrics{
		LatencyMin:       1e9,
		StatusCodeCounts: make(map[int]int64),
	}
}

func (m *Metrics) RecordLatency(latency time.Duration) {
	m.latencyMu.Lock()
	defer m.latencyMu.Unlock()

	latencyNs := latency.Nanoseconds()
	m.LatencySum += latencyNs
	if latencyNs < m.LatencyMin {
		m.LatencyMin = latencyNs
	}
	if latencyNs > m.LatencyMax {
		m.LatencyMax = latencyNs
	}
}

func (m *Metrics) RecordStatusCode(code int) {
	m.statusMu.Lock()
	defer m.statusMu.Unlock()
	m.StatusCodeCounts[code]++
}

func (m *Metrics) PrintReport(cfg TestConfig) {
	duration := time.Since(m.StartTime)
	qps := float64(m.SuccessRequests) / duration.Seconds()

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("                    压测结果报告")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("\n测试时长: %v\n", duration.Round(time.Millisecond))
	fmt.Printf("并发数: %d\n", cfg.Concurrent)
	fmt.Printf("测试模式: %s\n", cfg.Mode)
	fmt.Println()
	fmt.Println(strings.Repeat("-", 60))
	fmt.Println("请求统计:")
	fmt.Printf("  总请求数:     %d\n", m.TotalRequests)
	fmt.Printf("  成功请求:     %d (%.1f%%)\n", m.SuccessRequests, float64(m.SuccessRequests)*100/float64(max(m.TotalRequests, 1)))
	fmt.Printf("  错误请求:     %d\n", m.ErrorRequests)
	fmt.Printf("  被限流:       %d\n", m.RateLimited)
	fmt.Printf("  未授权:       %d\n", m.Unauthorized)
	fmt.Println()
	fmt.Println(strings.Repeat("-", 60))
	fmt.Println("性能指标:")
	fmt.Printf("  QPS:          %.2f req/s\n", qps)

	if m.SuccessRequests > 0 {
		avgLatency := time.Duration(m.LatencySum / m.SuccessRequests)
		fmt.Printf("  平均延迟:     %v\n", avgLatency.Round(time.Microsecond))
		fmt.Printf("  最小延迟:     %v\n", time.Duration(m.LatencyMin).Round(time.Microsecond))
		fmt.Printf("  最大延迟:     %v\n", time.Duration(m.LatencyMax).Round(time.Microsecond))
	}

	fmt.Println()
	fmt.Println(strings.Repeat("-", 60))
	fmt.Println("HTTP 状态码分布:")
	for code, count := range m.StatusCodeCounts {
		fmt.Printf("  %d: %d (%.1f%%)\n", code, count, float64(count)*100/float64(max(m.TotalRequests, 1)))
	}
	fmt.Println(strings.Repeat("=", 60))
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func createTestProxy(mode string, rateLimit int) (*proxy.Proxy, string) {
	logger, _ := zap.NewDevelopment()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))

	snap := snapshot.NewConfigSnapshot(uuid.New())

	svc, _ := service.New("test-service")
	svc.Protocol = service.ProtocolHTTP

	host, portInt := parseAddr(backend.Listener.Addr().String())
	svc.Host = host
	svc.Port = portInt
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/api")
	r.AddMethod("GET")
	snap.AddRoute(r)

	apiKey := "test-api-key"

	switch mode {
	case "auth":
		consumerID := uuid.New()
		cred := &credential.Credential{
			ID:         uuid.New(),
			ConsumerID: consumerID,
			Type:       credential.TypeKeyAuth,
			Key:        apiKey,
			Enabled:    true,
		}
		snap.AddCredential(cred)

		keyAuthPlugin, _ := plugin.New("key-auth", map[string]interface{}{})
		snap.AddPlugin(keyAuthPlugin)

	case "ratelimit":
		consumerID := uuid.New()
		cred := &credential.Credential{
			ID:         uuid.New(),
			ConsumerID: consumerID,
			Type:       credential.TypeKeyAuth,
			Key:        apiKey,
			Enabled:    true,
		}
		snap.AddCredential(cred)

		keyAuthPlugin, _ := plugin.New("key-auth", map[string]interface{}{})
		snap.AddPlugin(keyAuthPlugin)

		rateLimitPlugin, _ := plugin.New("rate-limit", map[string]interface{}{
			"limit_by": "consumer",
			"limit":    rateLimit,
			"window":   "1m",
			"policy":   "local",
		})
		snap.AddPlugin(rateLimitPlugin)
	}

	if err := snap.Build(); err != nil {
		panic(fmt.Sprintf("Failed to build snapshot: %v", err))
	}

	p := proxy.NewProxy(logger)
	p.UpdateSnapshot(snap)

	return p, apiKey
}

func parseAddr(addr string) (string, int) {
	parts := strings.Split(addr, ":")
	if len(parts) < 2 {
		return "127.0.0.1", 8080
	}
	host := parts[0]
	if host == "" || host == "[" {
		host = "127.0.0.1"
	}
	portStr := parts[len(parts)-1]
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return host, 8080
	}
	return host, port
}

func runLoadTest(p *proxy.Proxy, apiKey string, cfg TestConfig) *Metrics {
	metrics := NewMetrics()
	metrics.StartTime = time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Duration)
	defer cancel()

	var wg sync.WaitGroup

	for i := 0; i < cfg.Concurrent; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				default:
					req := httptest.NewRequest("GET", "/api/test", nil)

					if cfg.Mode != "plain" {
						req.Header.Set("apikey", apiKey)
					}

					w := httptest.NewRecorder()

					atomic.AddInt64(&metrics.TotalRequests, 1)

					reqStart := time.Now()
					p.ServeHTTP(w, req)
					latency := time.Since(reqStart)

					metrics.RecordLatency(latency)
					metrics.RecordStatusCode(w.Code)

					switch w.Code {
					case http.StatusOK:
						atomic.AddInt64(&metrics.SuccessRequests, 1)
					case http.StatusTooManyRequests:
						atomic.AddInt64(&metrics.RateLimited, 1)
					case http.StatusUnauthorized:
						atomic.AddInt64(&metrics.Unauthorized, 1)
					default:
						atomic.AddInt64(&metrics.ErrorRequests, 1)
					}
				}
			}
		}()
	}

	<-ctx.Done()
	wg.Wait()

	return metrics
}

func main() {
	durationFlag := flag.Duration("duration", 10*time.Second, "Test duration")
	concurrentFlag := flag.Int("concurrent", 10, "Concurrent requests")
	rateLimitFlag := flag.Int("ratelimit", 10000, "Rate limit per consumer")
	modeFlag := flag.String("mode", "plain", "Test mode: plain|auth|ratelimit")

	flag.Parse()

	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("              Portkey 压测工具")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("\n配置:")
	fmt.Printf("\n  持续时间: %v", *durationFlag)
	fmt.Printf("\n  并发数:   %d", *concurrentFlag)
	fmt.Printf("\n  限流值:   %d", *rateLimitFlag)
	fmt.Printf("\n  测试模式: %s", *modeFlag)
	fmt.Println("\n")

	fmt.Println("正在初始化测试环境...")
	p, apiKey := createTestProxy(*modeFlag, *rateLimitFlag)
	fmt.Printf("API Key: %s\n", apiKey)
	fmt.Println("测试环境就绪，开始压测...\n")

	cfg := TestConfig{
		Duration:   *durationFlag,
		Concurrent: *concurrentFlag,
		RateLimit:  *rateLimitFlag,
		Mode:       *modeFlag,
	}

	metrics := runLoadTest(p, apiKey, cfg)
	metrics.PrintReport(cfg)
}
