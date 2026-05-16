package upstream

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrUpstreamNameRequired     = errors.New("upstream name is required")
	ErrUpstreamNameTooLong      = errors.New("upstream name must be <= 255 characters")
	ErrUpstreamInvalidAlgorithm = errors.New("algorithm must be round-robin, least-connections, or consistent-hashing")
	ErrUpstreamInvalidSlots     = errors.New("slots must be between 10 and 65535")
)

type Algorithm string

const (
	AlgorithmRoundRobin       Algorithm = "round-robin"
	AlgorithmLeastConnections Algorithm = "least-connections"
	AlgorithmConsistentHashing Algorithm = "consistent-hashing"
)

type HashOn string

const (
	HashOnNone     HashOn = "none"
	HashOnConsumer HashOn = "consumer"
	HashOnIP       HashOn = "ip"
	HashOnHeader   HashOn = "header"
	HashOnCookie   HashOn = "cookie"
)

type Upstream struct {
	ID                 uuid.UUID       `json:"id"`
	TenantID           uuid.UUID       `json:"tenant_id"`
	Name               string          `json:"name"`
	Algorithm          Algorithm       `json:"algorithm"`
	Slots              int             `json:"slots"`
	HealthChecks       *HealthChecks   `json:"healthchecks,omitempty"`
	HashOn             HashOn          `json:"hash_on"`
	HashFallback       HashOn          `json:"hash_fallback"`
	HashOnHeader       string          `json:"hash_on_header,omitempty"`
	HashFallbackHeader string          `json:"hash_fallback_header,omitempty"`
	HashOnCookie       string          `json:"hash_on_cookie,omitempty"`
	HashOnCookiePath   string          `json:"hash_on_cookie_path,omitempty"`
	Tags               []string        `json:"tags"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

type HealthChecks struct {
	Active  *ActiveHealthCheck  `json:"active,omitempty"`
	Passive *PassiveHealthCheck `json:"passive,omitempty"`
}

type ActiveHealthCheck struct {
	Enabled               bool              `json:"enabled"`
	Interval              int               `json:"interval"`
	Timeout               int               `json:"timeout"`
	HTTPPath              string            `json:"http_path"`
	Concurrency           int               `json:"concurrency"`
	UnhealthyThreshold    int               `json:"unhealthy_threshold"`
	HealthyThreshold      int               `json:"healthy_threshold"`
	HTTPSServerName       string            `json:"https_server_name,omitempty"`
	HTTPSVerifyCertificate bool             `json:"https_verify_certificate"`
	Headers               map[string][]string `json:"headers,omitempty"`
}

type PassiveHealthCheck struct {
	Enabled   bool           `json:"enabled"`
	Type      string         `json:"type,omitempty"`
	Unhealthy *UnhealthyCheck `json:"unhealthy,omitempty"`
}

type UnhealthyCheck struct {
	HTTPFailures int   `json:"http_failures"`
	TCPFailures  int   `json:"tcp_failures"`
	Timeouts     int   `json:"timeouts"`
	HTTPStatuses []int `json:"http_statuses,omitempty"`
}

func New(name string) (*Upstream, error) {
	if strings.TrimSpace(name) == "" {
		return nil, ErrUpstreamNameRequired
	}
	if len(name) > 255 {
		return nil, ErrUpstreamNameTooLong
	}

	now := time.Now()
	return &Upstream{
		ID:        uuid.New(),
		Name:      name,
		Algorithm: AlgorithmRoundRobin,
		Slots:     10000,
		HashOn:    HashOnNone,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (u *Upstream) Validate() error {
	if strings.TrimSpace(u.Name) == "" {
		return ErrUpstreamNameRequired
	}
	if len(u.Name) > 255 {
		return ErrUpstreamNameTooLong
	}
	if u.Algorithm != AlgorithmRoundRobin &&
		u.Algorithm != AlgorithmLeastConnections &&
		u.Algorithm != AlgorithmConsistentHashing {
		return ErrUpstreamInvalidAlgorithm
	}
	if u.Slots < 10 || u.Slots > 65535 {
		return ErrUpstreamInvalidSlots
	}
	return nil
}

func (u *Upstream) SetAlgorithm(algo Algorithm) {
	u.Algorithm = algo
}

func (u *Upstream) EnableActiveHealthChecks() {
	if u.HealthChecks == nil {
		u.HealthChecks = &HealthChecks{}
	}
	if u.HealthChecks.Active == nil {
		u.HealthChecks.Active = &ActiveHealthCheck{
			Enabled:            true,
			Interval:           10,
			Timeout:            1,
			HTTPPath:           "/",
			Concurrency:        10,
			UnhealthyThreshold: 3,
			HealthyThreshold:   2,
		}
	} else {
		u.HealthChecks.Active.Enabled = true
	}
}

func (u *Upstream) DisableActiveHealthChecks() {
	if u.HealthChecks != nil && u.HealthChecks.Active != nil {
		u.HealthChecks.Active.Enabled = false
	}
}

func (u *Upstream) AddTag(tag string) {
	for _, t := range u.Tags {
		if t == tag {
			return
		}
	}
	u.Tags = append(u.Tags, tag)
}
