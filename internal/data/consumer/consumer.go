package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/data/snapshot"
	"github.com/taichirain/portkey/internal/domain/credential"
	"github.com/taichirain/portkey/internal/domain/plugin"
	"github.com/taichirain/portkey/internal/domain/route"
	"github.com/taichirain/portkey/internal/domain/service"
	"github.com/taichirain/portkey/internal/domain/target"
	"github.com/taichirain/portkey/internal/domain/upstream"
	"go.uber.org/zap"
)

var (
	ErrNoActiveRevision = fmt.Errorf("no active revision found")
	ErrSnapshotInvalid  = fmt.Errorf("snapshot is invalid")
)

type RevisionResponse struct {
	RevisionID  string                 `json:"revision_id"`
	Version     string                 `json:"version"`
	Description string                 `json:"description"`
	PublishedAt *time.Time             `json:"published_at"`
	Snapshot    *RevisionSnapshotData  `json:"snapshot"`
}

type RevisionSnapshotData struct {
	Version         string                   `json:"version"`
	Timestamp       string                   `json:"timestamp"`
	Services        []ServiceSnapshot        `json:"services"`
	Routes          []RouteSnapshot          `json:"routes"`
	Upstreams       []UpstreamSnapshot       `json:"upstreams"`
	Consumers       []ConsumerSnapshot       `json:"consumers"`
	Credentials     []CredentialSnapshot     `json:"credentials"`
	Plugins         []PluginSnapshot         `json:"plugins"`
	TrafficPolicies []TrafficPolicySnapshot  `json:"traffic_policies"`
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

type ServiceSnapshot struct {
	ID             uuid.UUID `json:"id"`
	Name           string    `json:"name"`
	Protocol       string    `json:"protocol"`
	Host           string    `json:"host"`
	Port           int       `json:"port"`
	Path           string    `json:"path"`
	Retries        int       `json:"retries"`
	ConnectTimeout int       `json:"connect_timeout"`
	WriteTimeout   int       `json:"write_timeout"`
	ReadTimeout    int       `json:"read_timeout"`
	Tags           []string  `json:"tags"`
	Enabled        bool      `json:"enabled"`
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
	ID        uuid.UUID        `json:"id"`
	Name      string           `json:"name"`
	Algorithm string           `json:"algorithm"`
	Slots     int              `json:"slots"`
	Targets   []TargetSnapshot `json:"targets"`
	Tags      []string         `json:"tags"`
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

type CredentialSnapshot struct {
	ID         uuid.UUID              `json:"id"`
	ConsumerID uuid.UUID              `json:"consumer_id"`
	Type       string                 `json:"type"`
	Key        string                 `json:"key"`
	Secret     string                 `json:"secret"`
	Algorithm  string                 `json:"algorithm"`
	Claims     map[string]interface{} `json:"claims"`
	Tags       []string               `json:"tags"`
	Enabled    bool                   `json:"enabled"`
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

type SnapshotConsumer struct {
	controlURL   string
	httpClient   *http.Client
	logger       *zap.Logger
	pollInterval time.Duration

	currentRevisionID atomic.Value
	currentSnapshot   atomic.Value

	running     bool
	runningMu   sync.Mutex
	stopChan    chan struct{}
	updateChan  chan struct{}

	onSnapshotUpdate func(*snapshot.ConfigSnapshot)
}

type SnapshotConsumerConfig struct {
	ControlURL   string
	PollInterval time.Duration
	Logger       *zap.Logger
}

func NewSnapshotConsumer(cfg *SnapshotConsumerConfig) *SnapshotConsumer {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = 10 * time.Second
	}

	return &SnapshotConsumer{
		controlURL:   cfg.ControlURL,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		logger:       cfg.Logger,
		pollInterval: cfg.PollInterval,
		stopChan:     make(chan struct{}),
		updateChan:   make(chan struct{}, 1),
	}
}

func (c *SnapshotConsumer) SetOnSnapshotUpdate(fn func(*snapshot.ConfigSnapshot)) {
	c.onSnapshotUpdate = fn
}

func (c *SnapshotConsumer) Start(ctx context.Context) error {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()

	if c.running {
		return nil
	}

	c.running = true

	c.logger.Info("启动 snapshot consumer",
		zap.String("control_url", c.controlURL),
		zap.Duration("poll_interval", c.pollInterval),
	)

	if err := c.fetchAndUpdate(ctx); err != nil {
		c.logger.Warn("首次拉取快照失败", zap.Error(err))
	}

	go c.pollLoop()

	return nil
}

func (c *SnapshotConsumer) Stop(ctx context.Context) error {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()

	if !c.running {
		return nil
	}

	c.logger.Info("停止 snapshot consumer")
	close(c.stopChan)
	c.running = false

	return nil
}

func (c *SnapshotConsumer) pollLoop() {
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopChan:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := c.fetchAndUpdate(ctx); err != nil {
				c.logger.Error("拉取快照失败", zap.Error(err))
			}
			cancel()
		case <-c.updateChan:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := c.fetchAndUpdate(ctx); err != nil {
				c.logger.Error("拉取快照失败", zap.Error(err))
			}
			cancel()
		}
	}
}

func (c *SnapshotConsumer) fetchAndUpdate(ctx context.Context) error {
	resp, err := c.fetchActiveRevision(ctx)
	if err != nil {
		if err == ErrNoActiveRevision {
			c.logger.Warn("Control Plane 没有 active revision")
			return nil
		}
		return fmt.Errorf("failed to fetch active revision: %w", err)
	}

	currentRevID := c.getCurrentRevisionID()
	if resp.RevisionID == currentRevID {
		c.logger.Debug("快照没有变化，跳过更新", zap.String("revision_id", resp.RevisionID))
		return nil
	}

	c.logger.Info("检测到新的 revision",
		zap.String("new_revision_id", resp.RevisionID),
		zap.String("current_revision_id", currentRevID),
	)

	snap, err := c.buildConfigSnapshot(resp)
	if err != nil {
		c.logger.Error("构建配置快照失败",
			zap.Error(err),
			zap.String("revision_id", resp.RevisionID),
		)
		return ErrSnapshotInvalid
	}

	if err := c.validateSnapshot(snap); err != nil {
		c.logger.Error("验证快照失败",
			zap.Error(err),
			zap.String("revision_id", resp.RevisionID),
		)
		return ErrSnapshotInvalid
	}

	c.currentSnapshot.Store(snap)
	c.setCurrentRevisionID(resp.RevisionID)

	c.logger.Info("快照更新成功",
		zap.String("revision_id", resp.RevisionID),
		zap.String("version", resp.Version),
		zap.Int("services_count", len(snap.Services)),
		zap.Int("routes_count", len(snap.Routes)),
		zap.Int("upstreams_count", len(snap.Upstreams)),
		zap.Int("traffic_policies_count", len(snap.TrafficPolicies)),
	)

	if c.onSnapshotUpdate != nil {
		c.onSnapshotUpdate(snap)
	}

	return nil
}

func (c *SnapshotConsumer) fetchActiveRevision(ctx context.Context) (*RevisionResponse, error) {
	url := c.controlURL + "/api/v1/public/active-revision"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNoActiveRevision
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result RevisionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &result, nil
}

func (c *SnapshotConsumer) buildConfigSnapshot(resp *RevisionResponse) (*snapshot.ConfigSnapshot, error) {
	if resp.Snapshot == nil {
		return nil, fmt.Errorf("snapshot data is nil")
	}

	revisionID, err := uuid.Parse(resp.RevisionID)
	if err != nil {
		return nil, fmt.Errorf("invalid revision_id: %w", err)
	}

	configSnap := snapshot.NewConfigSnapshot(revisionID)

	for _, svcSnap := range resp.Snapshot.Services {
		svc := &service.Service{
			ID:             svcSnap.ID,
			Name:           svcSnap.Name,
			Protocol:       service.Protocol(svcSnap.Protocol),
			Host:           svcSnap.Host,
			Port:           svcSnap.Port,
			Path:           svcSnap.Path,
			Retries:        svcSnap.Retries,
			ConnectTimeout: svcSnap.ConnectTimeout,
			WriteTimeout:   svcSnap.WriteTimeout,
			ReadTimeout:    svcSnap.ReadTimeout,
			Tags:           svcSnap.Tags,
			Enabled:        svcSnap.Enabled,
		}
		configSnap.AddService(svc)
	}

	for _, routeSnap := range resp.Snapshot.Routes {
		r := &route.Route{
			ID:            routeSnap.ID,
			Name:          routeSnap.Name,
			ServiceID:     routeSnap.ServiceID,
			Protocols:     routeSnap.Protocols,
			Methods:       routeSnap.Methods,
			Hosts:         routeSnap.Hosts,
			Paths:         routeSnap.Paths,
			Headers:       routeSnap.Headers,
			StripPath:     routeSnap.StripPath,
			PreserveHost:  routeSnap.PreserveHost,
			RegexPriority: routeSnap.RegexPriority,
			Tags:          routeSnap.Tags,
			Enabled:       routeSnap.Enabled,
		}
		configSnap.AddRoute(r)
	}

	upstreamTargets := make(map[uuid.UUID][]*target.Target)

	for _, upSnap := range resp.Snapshot.Upstreams {
		u := &upstream.Upstream{
			ID:        upSnap.ID,
			Name:      upSnap.Name,
			Algorithm: upstream.Algorithm(upSnap.Algorithm),
			Slots:     upSnap.Slots,
			Tags:      upSnap.Tags,
		}
		configSnap.AddUpstream(u)

		targets := make([]*target.Target, 0)
		for _, tSnap := range upSnap.Targets {
			t := &target.Target{
				ID:         tSnap.ID,
				UpstreamID: upSnap.ID,
				Target:     tSnap.Target,
				Port:       tSnap.Port,
				Weight:     tSnap.Weight,
				Enabled:    tSnap.Enabled,
			}
			targets = append(targets, t)
		}
		upstreamTargets[upSnap.ID] = targets
	}

	for upstreamID, targets := range upstreamTargets {
		configSnap.AddTargets(upstreamID, targets)
	}

	for _, credSnap := range resp.Snapshot.Credentials {
		c := &credential.Credential{
			ID:         credSnap.ID,
			ConsumerID: credSnap.ConsumerID,
			Type:       credential.Type(credSnap.Type),
			Key:        credSnap.Key,
			Secret:     credSnap.Secret,
			Algorithm:  credSnap.Algorithm,
			Claims:     credSnap.Claims,
			Tags:       credSnap.Tags,
			Enabled:    credSnap.Enabled,
		}
		configSnap.AddCredential(c)
	}

	for _, pluginSnap := range resp.Snapshot.Plugins {
		p := &plugin.Plugin{
			ID:         pluginSnap.ID,
			Name:       pluginSnap.Name,
			RouteID:    pluginSnap.RouteID,
			ServiceID:  pluginSnap.ServiceID,
			ConsumerID: pluginSnap.ConsumerID,
			Config:     pluginSnap.Config,
			Protocols:  pluginSnap.Protocols,
			Enabled:    pluginSnap.Enabled,
		}
		configSnap.AddPlugin(p)
	}

	for _, tpSnap := range resp.Snapshot.TrafficPolicies {
		tp := &snapshot.TrafficPolicy{
			ID:              tpSnap.ID,
			Name:            tpSnap.Name,
			RouteID:         tpSnap.RouteID,
			Priority:        tpSnap.Priority,
			Type:            tpSnap.Type,
			MatchConfig:     tpSnap.MatchConfig,
			TargetServiceID: tpSnap.TargetServiceID,
			Enabled:         tpSnap.Enabled,
			Tags:            tpSnap.Tags,
		}
		configSnap.AddTrafficPolicy(tp)
	}

	if err := configSnap.Build(); err != nil {
		return nil, fmt.Errorf("failed to build snapshot: %w", err)
	}

	return configSnap, nil
}

func (c *SnapshotConsumer) validateSnapshot(snap *snapshot.ConfigSnapshot) error {
	if len(snap.Services) == 0 && len(snap.Routes) == 0 {
		c.logger.Warn("快照中没有任何配置")
	}

	for _, r := range snap.Routes {
		if !r.Enabled {
			continue
		}
		if r.ServiceID != uuid.Nil {
			if _, ok := snap.GetService(r.ServiceID); !ok {
				c.logger.Warn("路由引用不存在的 service",
					zap.Stringer("route_id", r.ID),
					zap.Stringer("service_id", r.ServiceID),
				)
			}
		}
	}

	return nil
}

func (c *SnapshotConsumer) getCurrentRevisionID() string {
	val := c.currentRevisionID.Load()
	if val == nil {
		return ""
	}
	return val.(string)
}

func (c *SnapshotConsumer) setCurrentRevisionID(id string) {
	c.currentRevisionID.Store(id)
}

func (c *SnapshotConsumer) GetCurrentSnapshot() *snapshot.ConfigSnapshot {
	val := c.currentSnapshot.Load()
	if val == nil {
		return nil
	}
	return val.(*snapshot.ConfigSnapshot)
}

func (c *SnapshotConsumer) GetCurrentRevisionID() string {
	return c.getCurrentRevisionID()
}

func (c *SnapshotConsumer) TriggerUpdate() {
	select {
	case c.updateChan <- struct{}{}:
	default:
	}
}
