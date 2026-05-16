package ratelimit

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Limiter interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) (*LimitResult, error)
}

type LimitResult struct {
	Allowed     bool
	Remaining   int
	Reset       time.Time
	Limit       int
	Window      time.Duration
}

type RateLimitConfig struct {
	LimitBy      LimitByType `json:"limit_by"`
	Limit        int         `json:"limit"`
	Window       Duration    `json:"window"`
	Policy       PolicyType  `json:"policy"`
	RetryAfter   bool        `json:"retry_after"`
	IncludeHeaders bool      `json:"include_headers"`
}

type LimitByType string

const (
	LimitByConsumer LimitByType = "consumer"
	LimitByRoute    LimitByType = "route"
	LimitByIP       LimitByType = "ip"
)

type PolicyType string

const (
	PolicyLocal PolicyType = "local"
	PolicyRedis PolicyType = "redis"
)

type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%q", time.Duration(d).String())), nil
}

func (d *Duration) UnmarshalJSON(data []byte) error {
	str := strings.Trim(string(data), `"`)
	if str == "" {
		*d = Duration(60 * time.Second)
		return nil
	}
	dur, err := time.ParseDuration(str)
	if err != nil {
		return err
	}
	*d = Duration(dur)
	return nil
}

func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

type LimitKeyGenerator struct{}

func (g *LimitKeyGenerator) Generate(
	limitBy LimitByType,
	consumerID *uuid.UUID,
	routeID uuid.UUID,
	ip string,
) string {
	switch limitBy {
	case LimitByConsumer:
		if consumerID != nil {
			return "ratelimit:consumer:" + consumerID.String()
		}
		return "ratelimit:consumer:anonymous"
	case LimitByRoute:
		return "ratelimit:route:" + routeID.String()
	case LimitByIP:
		return "ratelimit:ip:" + ip
	default:
		return "ratelimit:global"
	}
}

func ExtractIP(r *http.Request) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return xri
	}

	remoteAddr := r.RemoteAddr
	if idx := strings.LastIndex(remoteAddr, ":"); idx != -1 {
		return remoteAddr[:idx]
	}
	return remoteAddr
}

type RateLimitHeaders struct{}

func (h *RateLimitHeaders) SetHeaders(w http.ResponseWriter, result *LimitResult) {
	if result == nil {
		return
	}

	w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", result.Limit))
	w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", result.Remaining))
	w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", result.Reset.Unix()))
	w.Header().Set("X-RateLimit-Policy", fmt.Sprintf("%d;w=%d", result.Limit, int(result.Window.Seconds())))

	if !result.Allowed {
		retryAfter := int(time.Until(result.Reset).Seconds())
		if retryAfter < 1 {
			retryAfter = 1
		}
		w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
	}
}
