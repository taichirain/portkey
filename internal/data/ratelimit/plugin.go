package ratelimit

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	pluginPkg "github.com/taichirain/portkey/internal/data/plugin"
	"github.com/redis/go-redis/v9"
)

const (
	RateLimitPluginName = "rate-limit"
)

var (
	globalLocalLimiter      Limiter
	localLimiterOnce        sync.Once
	redisClientWarnOnce     sync.Once
	redisUnavailableWarnOnce sync.Once
)

func getGlobalLocalLimiter() Limiter {
	localLimiterOnce.Do(func() {
		globalLocalLimiter = NewLocalLimiter()
	})
	return globalLocalLimiter
}

type RateLimitPlugin struct {
	config          *RateLimitConfig
	localLimiter    Limiter
	redisLimiter    Limiter
	keyGenerator    *LimitKeyGenerator
	headers         *RateLimitHeaders
	blockedRequests int64
}

func NewRateLimitFactory() pluginPkg.PluginFactory {
	return &rateLimitFactory{}
}

type rateLimitFactory struct{}

func (f *rateLimitFactory) Name() string {
	return RateLimitPluginName
}

func (f *rateLimitFactory) Create(config map[string]interface{}) (pluginPkg.Plugin, error) {
	cfg := parseRateLimitConfig(config)
	return &RateLimitPlugin{
		config:       cfg,
		localLimiter: getGlobalLocalLimiter(),
		keyGenerator: &LimitKeyGenerator{},
		headers:      &RateLimitHeaders{},
	}, nil
}

func parseRateLimitConfig(config map[string]interface{}) *RateLimitConfig {
	cfg := &RateLimitConfig{
		LimitBy:        LimitByRoute,
		Limit:          100,
		Window:         Duration(60 * time.Second),
		Policy:         PolicyLocal,
		RetryAfter:     true,
		IncludeHeaders: true,
	}

	if limitBy, ok := config["limit_by"].(string); ok {
		switch limitBy {
		case "consumer":
			cfg.LimitBy = LimitByConsumer
		case "route":
			cfg.LimitBy = LimitByRoute
		case "ip":
			cfg.LimitBy = LimitByIP
		}
	}

	if limit, ok := config["limit"]; ok {
		switch v := limit.(type) {
		case float64:
			if v > 0 {
				cfg.Limit = int(v)
			}
		case int:
			if v > 0 {
				cfg.Limit = v
			}
		case int64:
			if v > 0 {
				cfg.Limit = int(v)
			}
		}
	}

	if window, ok := config["window"].(string); ok {
		if dur, err := time.ParseDuration(window); err == nil {
			cfg.Window = Duration(dur)
		}
	}

	if policy, ok := config["policy"].(string); ok {
		switch policy {
		case "local":
			cfg.Policy = PolicyLocal
		case "redis":
			cfg.Policy = PolicyRedis
		}
	}

	if retryAfter, ok := config["retry_after"].(bool); ok {
		cfg.RetryAfter = retryAfter
	}

	if includeHeaders, ok := config["include_headers"].(bool); ok {
		cfg.IncludeHeaders = includeHeaders
	}

	return cfg
}

func (p *RateLimitPlugin) Name() string {
	return RateLimitPluginName
}

func (p *RateLimitPlugin) SetRedisClient(client *redis.Client) {
	p.redisLimiter = NewRedisLimiter(client)
}

func (p *RateLimitPlugin) GetLimiter() Limiter {
	if p.config.Policy == PolicyRedis {
		if p.redisLimiter != nil {
			return p.redisLimiter
		}
		redisClientWarnOnce.Do(func() {
			log.Printf("[WARN] rate-limit: policy configured as 'redis' but Redis client not injected, falling back to local limiter. This may cause inconsistent rate limiting across multiple DP instances.")
		})
	}
	return p.localLimiter
}

func (p *RateLimitPlugin) OnRequest(ctx *pluginPkg.PluginContext) error {
	var key string

	switch p.config.LimitBy {
	case LimitByConsumer:
		key = p.keyGenerator.Generate(LimitByConsumer, ctx.ConsumerID, uuidFromMatchedRoute(ctx.MatchedRoute), "")
	case LimitByRoute:
		key = p.keyGenerator.Generate(LimitByRoute, nil, uuidFromMatchedRoute(ctx.MatchedRoute), "")
	case LimitByIP:
		ip := ExtractIP(ctx.Request)
		key = p.keyGenerator.Generate(LimitByIP, nil, uuid.Nil, ip)
	}

	limiter := p.GetLimiter()
	result, err := limiter.Allow(context.Background(), key, p.config.Limit, p.config.Window.Duration())

	if err != nil {
		if p.config.Policy == PolicyRedis && p.redisLimiter != nil {
			redisUnavailableWarnOnce.Do(func() {
				log.Printf("[WARN] rate-limit: Redis is unavailable, falling back to local limiter. Error: %v", err)
			})
			result, err = p.localLimiter.Allow(context.Background(), key, p.config.Limit, p.config.Window.Duration())
			if err != nil {
				return pluginPkg.NewPluginError(p.Name(), "rate limit check failed (both redis and local limiter failed)", err)
			}
		} else {
			return pluginPkg.NewPluginError(p.Name(), "rate limit check failed", err)
		}
	}

	if p.config.IncludeHeaders {
		p.headers.SetHeaders(ctx.ResponseWriter, result)
	}

	if !result.Allowed {
		atomic.AddInt64(&p.blockedRequests, 1)
		return p.tooManyRequests(ctx)
	}

	return nil
}

func (p *RateLimitPlugin) tooManyRequests(ctx *pluginPkg.PluginContext) error {
	ctx.ResponseWriter.Header().Set("Content-Type", "application/json")
	ctx.ResponseWriter.WriteHeader(http.StatusTooManyRequests)

	response := map[string]interface{}{
		"message": "Rate limit exceeded",
		"limit":   p.config.Limit,
		"window":  p.config.Window.Duration().String(),
	}

	body, _ := json.Marshal(response)
	ctx.ResponseWriter.Write(body)
	ctx.ShortCircuit()

	return pluginPkg.NewPluginError(p.Name(), "rate limit exceeded", nil)
}

func (p *RateLimitPlugin) OnResponse(ctx *pluginPkg.PluginContext, resp *http.Response) error {
	return nil
}

func (p *RateLimitPlugin) OnError(ctx *pluginPkg.PluginContext, err error) error {
	return nil
}

func (p *RateLimitPlugin) BlockedRequests() int64 {
	return atomic.LoadInt64(&p.blockedRequests)
}

func uuidFromMatchedRoute(route *pluginPkg.MatchedRouteInfo) uuid.UUID {
	if route == nil {
		return uuid.Nil
	}
	return route.RouteID
}
