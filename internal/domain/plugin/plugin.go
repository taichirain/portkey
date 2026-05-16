package plugin

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrPluginNameRequired    = errors.New("plugin name is required")
	ErrPluginConfigRequired  = errors.New("plugin config is required")
	ErrPluginInvalidRunOn    = errors.New("run_on must be first, second, last, or all")
)

type Scope int

const (
	ScopeGlobal Scope = iota
	ScopeService
	ScopeRoute
	ScopeConsumer
)

type RunOn string

const (
	RunOnFirst  RunOn = "first"
	RunOnSecond RunOn = "second"
	RunOnLast   RunOn = "last"
	RunOnAll    RunOn = "all"
)

type Plugin struct {
	ID         uuid.UUID              `json:"id"`
	TenantID   uuid.UUID              `json:"tenant_id"`
	Name       string                 `json:"name"`
	RouteID    *uuid.UUID             `json:"route_id,omitempty"`
	ServiceID  *uuid.UUID             `json:"service_id,omitempty"`
	ConsumerID *uuid.UUID             `json:"consumer_id,omitempty"`
	Config     map[string]interface{} `json:"config"`
	Protocols  []string               `json:"protocols"`
	Enabled    bool                   `json:"enabled"`
	RunOn      RunOn                  `json:"run_on"`
	Tags       []string               `json:"tags"`
	CreatedAt  time.Time              `json:"created_at"`
	UpdatedAt  time.Time              `json:"updated_at"`
}

func New(name string, config map[string]interface{}) (*Plugin, error) {
	if strings.TrimSpace(name) == "" {
		return nil, ErrPluginNameRequired
	}
	if config == nil {
		return nil, ErrPluginConfigRequired
	}

	now := time.Now()
	return &Plugin{
		ID:        uuid.New(),
		Name:      name,
		Config:    config,
		Protocols: []string{"http", "https"},
		Enabled:   true,
		RunOn:     RunOnFirst,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (p *Plugin) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return ErrPluginNameRequired
	}
	if p.Config == nil {
		return ErrPluginConfigRequired
	}
	if p.RunOn != "" && p.RunOn != RunOnFirst && p.RunOn != RunOnSecond && p.RunOn != RunOnLast && p.RunOn != RunOnAll {
		return ErrPluginInvalidRunOn
	}
	return nil
}

func (p *Plugin) Enable() {
	p.Enabled = true
}

func (p *Plugin) Disable() {
	p.Enabled = false
}

func (p *Plugin) AddTag(tag string) {
	for _, t := range p.Tags {
		if t == tag {
			return
		}
	}
	p.Tags = append(p.Tags, tag)
}

func (p *Plugin) Scope() Scope {
	if p.ConsumerID != nil {
		return ScopeConsumer
	}
	if p.RouteID != nil {
		return ScopeRoute
	}
	if p.ServiceID != nil {
		return ScopeService
	}
	return ScopeGlobal
}

func (p *Plugin) IsGlobal() bool {
	return p.RouteID == nil && p.ServiceID == nil && p.ConsumerID == nil
}

func (p *Plugin) ConfigJSON() ([]byte, error) {
	return json.Marshal(p.Config)
}

func (p *Plugin) SetConfigFromJSON(data []byte) error {
	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}
	p.Config = config
	return nil
}
