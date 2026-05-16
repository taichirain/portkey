package target

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrTargetUpstreamIDRequired = errors.New("upstream id is required")
	ErrTargetTargetRequired      = errors.New("target host is required")
	ErrTargetInvalidPort         = errors.New("port must be between 1 and 65535")
	ErrTargetInvalidWeight       = errors.New("weight must be between 0 and 1000")
)

type Target struct {
	ID         uuid.UUID `json:"id"`
	TenantID   uuid.UUID `json:"tenant_id"`
	UpstreamID uuid.UUID `json:"upstream_id"`
	Target     string    `json:"target"`
	Port       int       `json:"port"`
	Weight     int       `json:"weight"`
	Tags       []string  `json:"tags"`
	Enabled    bool      `json:"enabled"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func New(upstreamID uuid.UUID, targetHost string, port int) (*Target, error) {
	if upstreamID == uuid.Nil {
		return nil, ErrTargetUpstreamIDRequired
	}
	if strings.TrimSpace(targetHost) == "" {
		return nil, ErrTargetTargetRequired
	}
	if port <= 0 || port > 65535 {
		return nil, ErrTargetInvalidPort
	}

	now := time.Now()
	return &Target{
		ID:         uuid.New(),
		UpstreamID: upstreamID,
		Target:     targetHost,
		Port:       port,
		Weight:     100,
		Enabled:    true,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

func (t *Target) Validate() error {
	if t.UpstreamID == uuid.Nil {
		return ErrTargetUpstreamIDRequired
	}
	if strings.TrimSpace(t.Target) == "" {
		return ErrTargetTargetRequired
	}
	if t.Port <= 0 || t.Port > 65535 {
		return ErrTargetInvalidPort
	}
	if t.Weight < 0 || t.Weight > 1000 {
		return ErrTargetInvalidWeight
	}
	return nil
}

func (t *Target) Enable() {
	t.Enabled = true
}

func (t *Target) Disable() {
	t.Enabled = false
}

func (t *Target) SetWeight(weight int) error {
	if weight < 0 || weight > 1000 {
		return ErrTargetInvalidWeight
	}
	t.Weight = weight
	return nil
}

func (t *Target) AddTag(tag string) {
	for _, tg := range t.Tags {
		if tg == tag {
			return
		}
	}
	t.Tags = append(t.Tags, tag)
}
