package data

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/config"
	"github.com/taichirain/portkey/internal/data/consumer"
	"github.com/taichirain/portkey/internal/data/proxy"
	"github.com/taichirain/portkey/internal/data/snapshot"
	"github.com/taichirain/portkey/internal/domain/route"
	"github.com/taichirain/portkey/internal/domain/service"
	"github.com/taichirain/portkey/internal/domain/target"
	"github.com/taichirain/portkey/internal/domain/upstream"
	"go.uber.org/zap"
)

type App struct {
	cfg      *config.Config
	logger   *zap.Logger
	server   *http.Server
	proxy    *proxy.Proxy
	consumer *consumer.SnapshotConsumer
}

func NewApp(ctx context.Context, cfg *config.Config, logger *zap.Logger) (*App, error) {
	logger.Info("初始化 Data Plane",
		zap.String("host", cfg.Data.Host),
		zap.Int("port", cfg.Data.Port),
		zap.String("control_url", cfg.Data.ControlURL),
	)

	app := &App{
		cfg:    cfg,
		logger: logger,
	}

	app.proxy = proxy.NewProxy(logger.Named("proxy"))

	if cfg.Data.ControlURL == "" {
		logger.Warn("Control URL 未配置，使用测试配置快照")
		app.initTestSnapshot()
	} else {
		logger.Info("Control URL 已配置，将使用 CP 拉取配置快照")
		app.initConsumer(ctx)
	}

	mux := http.NewServeMux()
	app.registerRoutes(mux)

	addr := fmt.Sprintf("%s:%d", cfg.Data.Host, cfg.Data.Port)
	app.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return app, nil
}

func (a *App) initConsumer(ctx context.Context) {
	consumerCfg := &consumer.SnapshotConsumerConfig{
		ControlURL:   a.cfg.Data.ControlURL,
		PollInterval: 10 * time.Second,
		Logger:       a.logger.Named("consumer"),
	}

	a.consumer = consumer.NewSnapshotConsumer(consumerCfg)

	a.consumer.SetOnSnapshotUpdate(func(snap *snapshot.ConfigSnapshot) {
		a.proxy.UpdateSnapshot(snap)
		a.logger.Info("配置快照已原子更新到 proxy",
			zap.String("revision_id", snap.RevisionID.String()),
			zap.Int("services_count", len(snap.Services)),
			zap.Int("routes_count", len(snap.Routes)),
			zap.Int("upstreams_count", len(snap.Upstreams)),
			zap.Int("traffic_policies_count", len(snap.TrafficPolicies)),
		)
	})

	if err := a.consumer.Start(ctx); err != nil {
		a.logger.Error("启动 snapshot consumer 失败", zap.Error(err))
	}
}

func (a *App) initTestSnapshot() {
	snap := snapshot.NewConfigSnapshot(uuid.New())

	svc, _ := service.New("test-service")
	svc.Host = "httpbin.org"
	svc.Port = 80
	svc.Protocol = service.ProtocolHTTP
	snap.AddService(svc)

	r, _ := route.New(svc.ID)
	r.AddPath("/test")
	r.AddMethod("GET")
	r.AddMethod("POST")
	snap.AddRoute(r)

	up, _ := upstream.New("test-upstream")
	snap.AddUpstream(up)

	t1, _ := target.New(up.ID, "httpbin.org", 80)
	t2, _ := target.New(up.ID, "httpbin.org", 80)
	snap.AddTargets(up.ID, []*target.Target{t1, t2})

	svc2, _ := service.New("lb-service")
	svc2.UpstreamID = up.ID
	svc2.Protocol = service.ProtocolHTTP
	snap.AddService(svc2)

	r2, _ := route.New(svc2.ID)
	r2.AddPath("/lb")
	r2.AddMethod("GET")
	snap.AddRoute(r2)

	if err := snap.Build(); err != nil {
		a.logger.Error("构建测试快照失败", zap.Error(err))
		return
	}

	a.proxy.UpdateSnapshot(snap)
	a.logger.Info("测试配置快照已初始化",
		zap.String("service", svc.Name),
		zap.String("route_path", "/test"),
		zap.String("lb_route_path", "/lb"),
	)
}

func (a *App) Start(ctx context.Context) error {
	a.logger.Info("启动 Data Plane", zap.String("addr", a.server.Addr))

	ln, err := net.Listen("tcp", a.server.Addr)
	if err != nil {
		return fmt.Errorf("监听端口失败: %w", err)
	}

	go func() {
		if err := a.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			a.logger.Error("Data Plane 服务错误", zap.Error(err))
		}
	}()

	a.logger.Info("Data Plane 已启动", zap.String("addr", a.server.Addr))
	return nil
}

func (a *App) Stop(ctx context.Context) error {
	a.logger.Info("停止 Data Plane")

	if a.consumer != nil {
		if err := a.consumer.Stop(ctx); err != nil {
			a.logger.Warn("停止 consumer 失败", zap.Error(err))
		}
	}

	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := a.server.Shutdown(shutdownCtx); err != nil {
		a.logger.Warn("HTTP server 关闭超时", zap.Error(err))
	}

	return nil
}

func (a *App) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	})

	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ready"}`))
	})

	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		metrics := a.proxy.EnhancedMetrics()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(metrics)
	})

	mux.HandleFunc("/revision", func(w http.ResponseWriter, r *http.Request) {
		a.handleGetRevision(w, r)
	})

	mux.Handle("/", a.proxy)
}

type RevisionStatusResponse struct {
	CurrentRevisionID string     `json:"current_revision_id"`
	HasRevision       bool       `json:"has_revision"`
	ConfigStats       ConfigStats `json:"config_stats"`
	LastUpdated       *time.Time `json:"last_updated,omitempty"`
}

type ConfigStats struct {
	ServicesCount       int `json:"services_count"`
	RoutesCount         int `json:"routes_count"`
	UpstreamsCount      int `json:"upstreams_count"`
	TrafficPoliciesCount int `json:"traffic_policies_count"`
}

func (a *App) handleGetRevision(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var resp RevisionStatusResponse

	if a.consumer != nil {
		revID := a.consumer.GetCurrentRevisionID()
		snap := a.consumer.GetCurrentSnapshot()

		resp.CurrentRevisionID = revID
		resp.HasRevision = revID != ""

		if snap != nil {
			resp.ConfigStats = ConfigStats{
				ServicesCount:       len(snap.Services),
				RoutesCount:         len(snap.Routes),
				UpstreamsCount:      len(snap.Upstreams),
				TrafficPoliciesCount: len(snap.TrafficPolicies),
			}
			resp.LastUpdated = &snap.CreatedAt
		}
	} else {
		resp.HasRevision = true
		resp.CurrentRevisionID = "test-mode"
		resp.ConfigStats = ConfigStats{
			ServicesCount:       1,
			RoutesCount:         2,
			UpstreamsCount:      1,
			TrafficPoliciesCount: 0,
		}
	}

	data, _ := json.MarshalIndent(resp, "", "  ")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}
