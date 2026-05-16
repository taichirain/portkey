package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/config"
	"github.com/taichirain/portkey/internal/control/api/handler"
	"github.com/taichirain/portkey/internal/control/auth"
	"github.com/taichirain/portkey/internal/control/middleware"
	"github.com/taichirain/portkey/internal/control/publisher"
	"github.com/taichirain/portkey/internal/control/repository"
	"github.com/taichirain/portkey/internal/control/validator"
	"github.com/taichirain/portkey/internal/domain/admin"
	"github.com/taichirain/portkey/internal/domain/tenant"
	"github.com/taichirain/portkey/internal/platform/postgres"
	"go.uber.org/zap"
)

type App struct {
	cfg         *config.Config
	logger      *zap.Logger
	server      *http.Server
	db          *postgres.DB
	instanceID  uuid.UUID

	authService     *auth.AuthService
	authMiddleware  *middleware.AuthMiddleware
	rbacMiddleware  *middleware.RBACMiddleware

	serviceHandler        *handler.ServiceHandler
	routeHandler          *handler.RouteHandler
	upstreamHandler       *handler.UpstreamHandler
	targetHandler         *handler.TargetHandler
	consumerHandler       *handler.ConsumerHandler
	pluginHandler         *handler.PluginHandler
	loginHandler          *handler.LoginHandler
	revisionHandler       *handler.RevisionHandler
	trafficPolicyHandler  *handler.TrafficPolicyHandler
	auditHandler          *handler.AuditHandler
	monitoringHandler     *handler.MonitoringHandler

	configPublisher *publisher.ConfigPublisher
}

func NewApp(ctx context.Context, cfg *config.Config, logger *zap.Logger) (*App, error) {
	instanceID := uuid.New()
	logger.Info("初始化 Control Plane",
		zap.String("host", cfg.Control.Host),
		zap.Int("port", cfg.Control.Port),
		zap.Stringer("instance_id", instanceID),
	)

	db, err := postgres.New(&cfg.Control.DB)
	if err != nil {
		return nil, fmt.Errorf("初始化数据库失败: %w", err)
	}

	app := &App{
		cfg:        cfg,
		logger:     logger,
		db:         db,
		instanceID: instanceID,
	}

	if err := app.initComponents(ctx); err != nil {
		return nil, fmt.Errorf("初始化组件失败: %w", err)
	}

	mux := http.NewServeMux()
	app.registerRoutes(mux)

	addr := fmt.Sprintf("%s:%d", cfg.Control.Host, cfg.Control.Port)
	app.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	return app, nil
}

func (a *App) initComponents(ctx context.Context) error {
	a.logger.Info("初始化存储库")

	auditRepo := repository.NewPostgresAuditRepository(a.db)
	adminRepo := repository.NewPostgresAdminRepository(a.db, auditRepo)
	tenantRepo := repository.NewPostgresTenantRepository(a.db, auditRepo)
	serviceRepo := repository.NewPostgresServiceRepository(a.db, auditRepo)
	routeRepo := repository.NewPostgresRouteRepository(a.db, auditRepo)
	upstreamRepo := repository.NewPostgresUpstreamRepository(a.db, auditRepo)
	targetRepo := repository.NewPostgresTargetRepository(a.db, auditRepo)
	consumerRepo := repository.NewPostgresConsumerRepository(a.db, auditRepo)
	pluginRepo := repository.NewPostgresPluginRepository(a.db, auditRepo)
	revisionRepo := repository.NewPostgresRevisionRepositoryWithInstanceID(a.db, auditRepo, a.instanceID)
	trafficPolicyRepo := repository.NewPostgresTrafficPolicyRepository(a.db, auditRepo)
	lockRepo := repository.NewPostgresDistributedLockRepository(a.db)

	if err := a.ensureDefaultTenant(ctx, tenantRepo); err != nil {
		return fmt.Errorf("确保默认租户失败: %w", err)
	}

	a.logger.Info("初始化认证服务")
	passwordHasher := auth.NewPasswordHasher()
	jwtManager := auth.NewJWTManager("portkey-secret-key")
	a.authService = auth.NewAuthService(passwordHasher, jwtManager)
	a.authMiddleware = middleware.NewAuthMiddleware(a.authService, a.logger)
	a.rbacMiddleware = middleware.NewRBACMiddleware(a.logger)

	a.logger.Info("初始化配置校验器和发布服务")
	configValidator := validator.NewConfigValidator(routeRepo, serviceRepo, upstreamRepo, targetRepo, trafficPolicyRepo)
	a.configPublisher = publisher.NewConfigPublisherWithLock(
		configValidator,
		routeRepo,
		serviceRepo,
		upstreamRepo,
		targetRepo,
		revisionRepo,
		auditRepo,
		trafficPolicyRepo,
		lockRepo,
		a.instanceID,
		a.logger,
	)

	a.logger.Info("初始化 API 处理器")
	a.serviceHandler = handler.NewServiceHandler(serviceRepo, a.logger)
	a.routeHandler = handler.NewRouteHandler(routeRepo, a.logger)
	a.upstreamHandler = handler.NewUpstreamHandler(upstreamRepo, a.logger)
	a.targetHandler = handler.NewTargetHandler(targetRepo, a.logger)
	a.consumerHandler = handler.NewConsumerHandler(consumerRepo, a.logger)
	a.pluginHandler = handler.NewPluginHandler(pluginRepo, a.logger)
	a.loginHandler = handler.NewLoginHandler(a.authService, adminRepo, a.logger)
	a.revisionHandler = handler.NewRevisionHandler(a.configPublisher, a.logger)
	a.trafficPolicyHandler = handler.NewTrafficPolicyHandler(trafficPolicyRepo, a.logger)
	a.auditHandler = handler.NewAuditHandler(auditRepo, a.logger)

	a.monitoringHandler = handler.NewMonitoringHandler(a.cfg, a.logger, func() (string, string, *time.Time) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		rev, err := a.configPublisher.GetActiveRevision(ctx)
		if err != nil {
			return "", "", nil
		}
		return rev.ID.String(), rev.Version, rev.PublishedAt
	})

	if err := a.ensureDefaultAdmin(ctx, adminRepo); err != nil {
		return fmt.Errorf("确保默认管理员失败: %w", err)
	}

	return nil
}

func (a *App) ensureDefaultTenant(ctx context.Context, tenantRepo repository.TenantRepository) error {
	_, err := tenantRepo.GetByID(ctx, uuid.Nil)
	if err == nil {
		a.logger.Info("默认租户已存在")
		return nil
	}

	if err != repository.ErrNotFound {
		return fmt.Errorf("检查默认租户失败: %w", err)
	}

	defaultTenant := &tenant.Tenant{
		ID:          uuid.Nil,
		Name:        "System",
		Slug:        "system",
		Description: "Default system tenant for super_admin operations",
		Settings:    json.RawMessage("{}"),
		Enabled:     true,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := tenantRepo.Create(ctx, defaultTenant, nil); err != nil {
		if err != repository.ErrAlreadyExists {
			return fmt.Errorf("创建默认租户失败: %w", err)
		}
	}

	a.logger.Info("已创建默认租户", zap.String("name", defaultTenant.Name))
	return nil
}

func (a *App) ensureDefaultAdmin(ctx context.Context, adminRepo repository.AdminRepository) error {
	_, err := adminRepo.GetByUsername(ctx, "admin")
	if err == nil {
		a.logger.Info("默认管理员已存在")
		return nil
	}

	hashedPassword, err := a.authService.HashPassword("admin123")
	if err != nil {
		return fmt.Errorf("哈希密码失败: %w", err)
	}

	admin, err := admin.New("admin", "admin@portkey.local", hashedPassword)
	if err != nil {
		return fmt.Errorf("创建管理员失败: %w", err)
	}

	if err := adminRepo.Create(ctx, admin, nil); err != nil {
		return fmt.Errorf("保存管理员失败: %w", err)
	}

	a.logger.Warn("创建了默认管理员账户 - 请在生产环境中立即修改密码!",
		zap.String("username", "admin"),
		zap.String("password", "admin123"),
	)

	return nil
}

type ActiveRevisionResponse struct {
	RevisionID  string                 `json:"revision_id"`
	Version     string                 `json:"version"`
	Description string                 `json:"description"`
	PublishedAt *time.Time             `json:"published_at"`
	Snapshot    map[string]interface{} `json:"snapshot"`
}

func (a *App) handleGetActiveRevision(w http.ResponseWriter, r *http.Request) {
	rev, err := a.configPublisher.GetActiveRevision(r.Context())
	if err != nil {
		if errors.Is(err, publisher.ErrNoActiveRevision) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "No active revision found",
			})
			return
		}
		a.logger.Error("获取 active revision 失败", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	response := ActiveRevisionResponse{
		RevisionID:  rev.ID.String(),
		Version:     rev.Version,
		Description: rev.Description,
		PublishedAt: rev.PublishedAt,
		Snapshot:    rev.Snapshot,
	}

	a.logger.Debug("返回 active revision",
		zap.String("revision_id", rev.ID.String()),
		zap.String("version", rev.Version),
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func (a *App) Start(ctx context.Context) error {
	a.logger.Info("启动 Control Plane", zap.String("addr", a.server.Addr))

	ln, err := net.Listen("tcp", a.server.Addr)
	if err != nil {
		return fmt.Errorf("监听端口失败: %w", err)
	}

	go func() {
		if err := a.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			a.logger.Error("Control Plane 服务错误", zap.Error(err))
		}
	}()

	a.logger.Info("Control Plane 已启动", zap.String("addr", a.server.Addr))
	return nil
}

func (a *App) Stop(ctx context.Context) error {
	a.logger.Info("停止 Control Plane")

	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := a.server.Shutdown(shutdownCtx); err != nil {
		a.logger.Warn("HTTP server 关闭超时", zap.Error(err))
	}

	if a.db != nil {
		if err := a.db.Close(); err != nil {
			a.logger.Warn("数据库关闭出错", zap.Error(err))
		}
	}

	return nil
}

func pathToResource(path string) (string, bool) {
	parts := strings.Split(path, "/")
	if len(parts) < 3 {
		return "", false
	}

	resource := parts[3]

	switch resource {
	case "services":
		return "service", true
	case "routes":
		return "route", true
	case "upstreams":
		return "upstream", true
	case "targets":
		return "target", true
	case "consumers":
		return "consumer", true
	case "plugins":
		return "plugin", true
	case "revisions":
		return "revision", true
	case "traffic-policies":
		return "traffic_policy", true
	case "audit-logs":
		return "audit", true
	default:
		return "", false
	}
}

func methodToAction(method string) string {
	switch method {
	case http.MethodGet:
		return "read"
	case http.MethodPost:
		return "create"
	case http.MethodPut:
		return "update"
	case http.MethodDelete:
		return "delete"
	default:
		return "read"
	}
}

func getRequiredPermission(path, method string) string {
	resource, ok := pathToResource(path)
	if !ok {
		return ""
	}

	if strings.HasPrefix(path, "/api/v1/revisions/publish") {
		return "revision:publish"
	}
	if strings.HasPrefix(path, "/api/v1/revisions/rollback") {
		return "revision:rollback"
	}
	if strings.HasPrefix(path, "/api/v1/revisions/validate") {
		return "revision:read"
	}
	if strings.HasPrefix(path, "/api/v1/revisions/snapshot") {
		return "revision:create"
	}
	if strings.HasPrefix(path, "/api/v1/revisions/active") {
		return "revision:read"
	}
	if strings.HasPrefix(path, "/api/v1/revisions/create-and-publish") {
		return "revision:publish"
	}
	if strings.HasPrefix(path, "/api/v1/plugins/global") {
		return "plugin:read"
	}

	action := methodToAction(method)
	return resource + ":" + action
}

func (a *App) dynamicPermissionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if middleware.HasRole(r.Context(), "super_admin") {
			next.ServeHTTP(w, r)
			return
		}

		if middleware.HasRole(r.Context(), "tenant_admin") {
			next.ServeHTTP(w, r)
			return
		}

		requiredPerm := getRequiredPermission(r.URL.Path, r.Method)
		if requiredPerm == "" {
			next.ServeHTTP(w, r)
			return
		}

		if !middleware.HasPermission(r.Context(), requiredPerm) {
			a.logger.Warn("权限不足",
				zap.String("path", r.URL.Path),
				zap.String("method", r.Method),
				zap.String("required_permission", requiredPerm),
				zap.Strings("user_roles", getRolesFromContext(r.Context())),
				zap.Strings("user_permissions", getPermissionsFromContext(r.Context())),
			)
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func getRolesFromContext(ctx context.Context) []string {
	if roles, ok := middleware.GetRoles(ctx); ok {
		return roles
	}
	return []string{}
}

func getPermissionsFromContext(ctx context.Context) []string {
	if perms, ok := middleware.GetPermissions(ctx); ok {
		return perms
	}
	return []string{}
}

func (a *App) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	})

	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if err := a.db.Ping(r.Context()); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"status":"unhealthy","reason":"database_unavailable"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ready"}`))
	})

	mux.HandleFunc("/api/v1/login", a.loginHandler.Login)

	mux.HandleFunc("/api/v1/public/active-revision", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		a.handleGetActiveRevision(w, r)
	})

	protected := http.NewServeMux()

	protected.HandleFunc("/api/v1/services", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Get("id") != "" {
				a.serviceHandler.GetByID(w, r)
			} else if r.URL.Query().Get("name") != "" {
				a.serviceHandler.GetByName(w, r)
			} else {
				a.serviceHandler.List(w, r)
			}
		case http.MethodPost:
			a.serviceHandler.Create(w, r)
		case http.MethodPut:
			a.serviceHandler.Update(w, r)
		case http.MethodDelete:
			a.serviceHandler.Delete(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	protected.HandleFunc("/api/v1/routes", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Get("id") != "" {
				a.routeHandler.GetByID(w, r)
			} else {
				a.routeHandler.List(w, r)
			}
		case http.MethodPost:
			a.routeHandler.Create(w, r)
		case http.MethodPut:
			a.routeHandler.Update(w, r)
		case http.MethodDelete:
			a.routeHandler.Delete(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	protected.HandleFunc("/api/v1/upstreams", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Get("id") != "" {
				a.upstreamHandler.GetByID(w, r)
			} else if r.URL.Query().Get("name") != "" {
				a.upstreamHandler.GetByName(w, r)
			} else {
				a.upstreamHandler.List(w, r)
			}
		case http.MethodPost:
			a.upstreamHandler.Create(w, r)
		case http.MethodPut:
			a.upstreamHandler.Update(w, r)
		case http.MethodDelete:
			a.upstreamHandler.Delete(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	protected.HandleFunc("/api/v1/targets", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Get("id") != "" {
				a.targetHandler.GetByID(w, r)
			} else if r.URL.Query().Get("upstream_id") != "" {
				a.targetHandler.ListByUpstreamID(w, r)
			} else {
				a.targetHandler.List(w, r)
			}
		case http.MethodPost:
			a.targetHandler.Create(w, r)
		case http.MethodPut:
			a.targetHandler.Update(w, r)
		case http.MethodDelete:
			a.targetHandler.Delete(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	protected.HandleFunc("/api/v1/consumers", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Get("id") != "" {
				a.consumerHandler.GetByID(w, r)
			} else if r.URL.Query().Get("username") != "" {
				a.consumerHandler.GetByUsername(w, r)
			} else if r.URL.Query().Get("custom_id") != "" {
				a.consumerHandler.GetByCustomID(w, r)
			} else {
				a.consumerHandler.List(w, r)
			}
		case http.MethodPost:
			a.consumerHandler.Create(w, r)
		case http.MethodPut:
			a.consumerHandler.Update(w, r)
		case http.MethodDelete:
			a.consumerHandler.Delete(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	protected.HandleFunc("/api/v1/plugins", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Get("id") != "" {
				a.pluginHandler.GetByID(w, r)
			} else if r.URL.Query().Get("name") != "" {
				a.pluginHandler.ListByName(w, r)
			} else if r.URL.Query().Get("route_id") != "" {
				a.pluginHandler.ListByRouteID(w, r)
			} else if r.URL.Query().Get("service_id") != "" {
				a.pluginHandler.ListByServiceID(w, r)
			} else if r.URL.Query().Get("consumer_id") != "" {
				a.pluginHandler.ListByConsumerID(w, r)
			} else {
				a.pluginHandler.List(w, r)
			}
		case http.MethodPost:
			a.pluginHandler.Create(w, r)
		case http.MethodPut:
			a.pluginHandler.Update(w, r)
		case http.MethodDelete:
			a.pluginHandler.Delete(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	protected.HandleFunc("/api/v1/plugins/global", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		a.pluginHandler.ListGlobal(w, r)
	})

	protected.HandleFunc("/api/v1/revisions", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Get("id") != "" {
				a.revisionHandler.GetByID(w, r)
			} else {
				a.revisionHandler.List(w, r)
			}
		case http.MethodPost:
			a.revisionHandler.CreateRevision(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	protected.HandleFunc("/api/v1/revisions/validate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		a.revisionHandler.Validate(w, r)
	})

	protected.HandleFunc("/api/v1/revisions/snapshot", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		a.revisionHandler.CreateSnapshot(w, r)
	})

	protected.HandleFunc("/api/v1/revisions/publish", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		a.revisionHandler.Publish(w, r)
	})

	protected.HandleFunc("/api/v1/revisions/active", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		a.revisionHandler.GetActive(w, r)
	})

	protected.HandleFunc("/api/v1/revisions/create-and-publish", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		a.revisionHandler.CreateAndPublish(w, r)
	})

	protected.HandleFunc("/api/v1/revisions/rollback", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		a.revisionHandler.Rollback(w, r)
	})

	protected.HandleFunc("/api/v1/traffic-policies", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Get("id") != "" {
				a.trafficPolicyHandler.GetByID(w, r)
			} else if r.URL.Query().Get("route_id") != "" {
				a.trafficPolicyHandler.ListByRouteID(w, r)
			} else {
				a.trafficPolicyHandler.List(w, r)
			}
		case http.MethodPost:
			a.trafficPolicyHandler.Create(w, r)
		case http.MethodPut:
			a.trafficPolicyHandler.Update(w, r)
		case http.MethodDelete:
			a.trafficPolicyHandler.Delete(w, r)
		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	protected.HandleFunc("/api/v1/audit-logs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		a.auditHandler.List(w, r)
	})

	protected.HandleFunc("/api/v1/monitoring/metrics", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		a.monitoringHandler.GetMetrics(w, r)
	})

	protected.HandleFunc("/api/v1/monitoring/dp-status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		a.monitoringHandler.GetDPStatus(w, r)
	})

	withRBAC := a.dynamicPermissionMiddleware(protected)
	authHandler := a.authMiddleware.Authenticate(withRBAC)
	mux.Handle("/api/v1/", authHandler)
}
