package revision

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	ErrRevisionVersionRequired  = errors.New("version is required")
	ErrRevisionSnapshotRequired = errors.New("snapshot is required")
)

type ConfigRevision struct {
	ID          uuid.UUID              `json:"id"`
	TenantID    uuid.UUID              `json:"tenant_id"`
	Version     string                 `json:"version"`
	Description string                 `json:"description"`
	Snapshot    map[string]interface{} `json:"snapshot"`
	IsActive    bool                   `json:"is_active"`
	CreatedBy   *uuid.UUID             `json:"created_by,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	PublishedAt *time.Time             `json:"published_at,omitempty"`
}

type Snapshot struct {
	Version         string                  `json:"version"`
	Timestamp       time.Time               `json:"timestamp"`
	Services        []ServiceSnapshot       `json:"services"`
	Routes          []RouteSnapshot         `json:"routes"`
	Upstreams       []UpstreamSnapshot      `json:"upstreams"`
	Consumers       []ConsumerSnapshot      `json:"consumers"`
	Plugins         []PluginSnapshot        `json:"plugins"`
	Credentials     []CredentialSnapshot    `json:"credentials"`
	TrafficPolicies []TrafficPolicySnapshot `json:"traffic_policies"`
}

type ServiceSnapshot struct {
	ID             uuid.UUID              `json:"id"`
	Name           string                 `json:"name"`
	Protocol       string                 `json:"protocol"`
	Host           string                 `json:"host"`
	Port           int                    `json:"port"`
	Path           string                 `json:"path"`
	Retries        int                    `json:"retries"`
	ConnectTimeout int                    `json:"connect_timeout"`
	WriteTimeout   int                    `json:"write_timeout"`
	ReadTimeout    int                    `json:"read_timeout"`
	Tags           []string               `json:"tags"`
	Enabled        bool                   `json:"enabled"`
}

type RouteSnapshot struct {
	ID            uuid.UUID              `json:"id"`
	Name          string                 `json:"name"`
	ServiceID     uuid.UUID              `json:"service_id"`
	Protocols     []string               `json:"protocols"`
	Methods       []string               `json:"methods"`
	Hosts         []string               `json:"hosts"`
	Paths         []string               `json:"paths"`
	Headers       map[string][]string    `json:"headers"`
	StripPath     bool                   `json:"strip_path"`
	PreserveHost  bool                   `json:"preserve_host"`
	RegexPriority int                    `json:"regex_priority"`
	Tags          []string               `json:"tags"`
	Enabled       bool                   `json:"enabled"`
}

type UpstreamSnapshot struct {
	ID        uuid.UUID         `json:"id"`
	Name      string            `json:"name"`
	Algorithm string            `json:"algorithm"`
	Slots     int               `json:"slots"`
	Targets   []TargetSnapshot  `json:"targets"`
	Tags      []string          `json:"tags"`
}

type TargetSnapshot struct {
	ID      uuid.UUID `json:"id"`
	Target  string    `json:"target"`
	Port    int       `json:"port"`
	Weight  int       `json:"weight"`
	Enabled bool      `json:"enabled"`
}

type ConsumerSnapshot struct {
	ID       uuid.UUID `json:"id"`
	Username string    `json:"username"`
	CustomID string    `json:"custom_id"`
	Tags     []string  `json:"tags"`
}

type PluginSnapshot struct {
	ID         uuid.UUID              `json:"id"`
	Name       string                 `json:"name"`
	RouteID    *uuid.UUID             `json:"route_id"`
	ServiceID  *uuid.UUID             `json:"service_id"`
	ConsumerID *uuid.UUID             `json:"consumer_id"`
	Config     map[string]interface{} `json:"config"`
	Protocols  []string               `json:"protocols"`
	Enabled    bool                   `json:"enabled"`
}

type CredentialSnapshot struct {
	ID         uuid.UUID `json:"id"`
	ConsumerID uuid.UUID `json:"consumer_id"`
	Type       string    `json:"type"`
	Key        string    `json:"key"`
	Enabled    bool      `json:"enabled"`
}

type TrafficPolicySnapshot struct {
	ID              uuid.UUID       `json:"id"`
	Name            string          `json:"name"`
	RouteID         uuid.UUID       `json:"route_id"`
	Priority        int             `json:"priority"`
	Type            string          `json:"type"`
	MatchConfig     json.RawMessage `json:"match_config"`
	TargetServiceID uuid.UUID       `json:"target_service_id"`
	Enabled         bool            `json:"enabled"`
	Tags            []string        `json:"tags"`
}

func New(version string, snapshot map[string]interface{}, createdBy *uuid.UUID) (*ConfigRevision, error) {
	if version == "" {
		return nil, ErrRevisionVersionRequired
	}
	if snapshot == nil {
		return nil, ErrRevisionSnapshotRequired
	}

	now := time.Now()
	return &ConfigRevision{
		ID:        uuid.New(),
		Version:   version,
		Snapshot:  snapshot,
		IsActive:  false,
		CreatedBy: createdBy,
		CreatedAt: now,
	}, nil
}

func NewFromSnapshot(version string, snapshot *Snapshot, createdBy *uuid.UUID) (*ConfigRevision, error) {
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(snapshotJSON, &m); err != nil {
		return nil, fmt.Errorf("failed to unmarshal snapshot: %w", err)
	}

	return New(version, m, createdBy)
}

func (r *ConfigRevision) Validate() error {
	if r.Version == "" {
		return ErrRevisionVersionRequired
	}
	if r.Snapshot == nil {
		return ErrRevisionSnapshotRequired
	}
	return nil
}

func (r *ConfigRevision) Activate() {
	r.IsActive = true
	now := time.Now()
	r.PublishedAt = &now
}

func (r *ConfigRevision) Deactivate() {
	r.IsActive = false
}

func (r *ConfigRevision) GetSnapshot() (*Snapshot, error) {
	if r.Snapshot == nil {
		return nil, ErrRevisionSnapshotRequired
	}

	snapshotJSON, err := json.Marshal(r.Snapshot)
	if err != nil {
		return nil, err
	}

	var snap Snapshot
	if err := json.Unmarshal(snapshotJSON, &snap); err != nil {
		return nil, err
	}

	return &snap, nil
}

func (r *ConfigRevision) SetSnapshot(snap *Snapshot) error {
	if snap == nil {
		r.Snapshot = nil
		return nil
	}

	snapshotJSON, err := json.Marshal(snap)
	if err != nil {
		return err
	}

	var m map[string]interface{}
	if err := json.Unmarshal(snapshotJSON, &m); err != nil {
		return err
	}

	r.Snapshot = m
	return nil
}
