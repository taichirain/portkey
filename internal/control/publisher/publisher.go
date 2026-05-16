package publisher

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/control/repository"
	"github.com/taichirain/portkey/internal/control/validator"
	"github.com/taichirain/portkey/internal/domain/revision"
	"go.uber.org/zap"
)

var (
	ErrNoActiveRevision   = errors.New("no active revision found")
	ErrPublishFailed      = errors.New("publish failed")
	ErrPublishConcurrency = errors.New("publish operation failed due to concurrent modification")
)

type ConfigPublisher struct {
	validator         *validator.ConfigValidator
	routeRepo         repository.RouteRepository
	serviceRepo       repository.ServiceRepository
	upstreamRepo      repository.UpstreamRepository
	targetRepo        repository.TargetRepository
	revisionRepo      repository.RevisionRepository
	auditRepo         repository.AuditRepository
	trafficPolicyRepo repository.TrafficPolicyRepository
	lockRepo          repository.DistributedLockRepository
	instanceID        uuid.UUID
	logger            *zap.Logger
}

type PublishResult struct {
	RevisionID uuid.UUID
	Version    string
	IsActive   bool
	CreatedAt  time.Time
}

type RollbackResult struct {
	RevisionID uuid.UUID
	Version    string
	IsActive   bool
}

func NewConfigPublisher(
	validator *validator.ConfigValidator,
	routeRepo repository.RouteRepository,
	serviceRepo repository.ServiceRepository,
	upstreamRepo repository.UpstreamRepository,
	targetRepo repository.TargetRepository,
	revisionRepo repository.RevisionRepository,
	auditRepo repository.AuditRepository,
	trafficPolicyRepo repository.TrafficPolicyRepository,
	logger *zap.Logger,
) *ConfigPublisher {
	return &ConfigPublisher{
		validator:         validator,
		routeRepo:         routeRepo,
		serviceRepo:       serviceRepo,
		upstreamRepo:      upstreamRepo,
		targetRepo:        targetRepo,
		revisionRepo:      revisionRepo,
		auditRepo:         auditRepo,
		trafficPolicyRepo: trafficPolicyRepo,
		lockRepo:          nil,
		instanceID:        uuid.New(),
		logger:            logger,
	}
}

func NewConfigPublisherWithLock(
	validator *validator.ConfigValidator,
	routeRepo repository.RouteRepository,
	serviceRepo repository.ServiceRepository,
	upstreamRepo repository.UpstreamRepository,
	targetRepo repository.TargetRepository,
	revisionRepo repository.RevisionRepository,
	auditRepo repository.AuditRepository,
	trafficPolicyRepo repository.TrafficPolicyRepository,
	lockRepo repository.DistributedLockRepository,
	instanceID uuid.UUID,
	logger *zap.Logger,
) *ConfigPublisher {
	if instanceID == uuid.Nil {
		instanceID = uuid.New()
	}
	return &ConfigPublisher{
		validator:         validator,
		routeRepo:         routeRepo,
		serviceRepo:       serviceRepo,
		upstreamRepo:      upstreamRepo,
		targetRepo:        targetRepo,
		revisionRepo:      revisionRepo,
		auditRepo:         auditRepo,
		trafficPolicyRepo: trafficPolicyRepo,
		lockRepo:          lockRepo,
		instanceID:        instanceID,
		logger:            logger,
	}
}

func (p *ConfigPublisher) Validate(ctx context.Context) (*validator.ValidationResult, error) {
	p.logger.Info("开始验证配置")
	result, err := p.validator.ValidateAll(ctx)
	if err != nil {
		p.logger.Error("配置验证失败", zap.Error(err))
		return nil, err
	}
	if !result.Valid {
		p.logger.Warn("配置验证不通过", zap.Int("error_count", len(result.Errors)))
		for _, e := range result.Errors {
			p.logger.Debug("验证错误",
				zap.String("resource_type", e.ResourceType),
				zap.Stringer("resource_id", e.ResourceID),
				zap.String("field", e.Field),
				zap.String("message", e.Message),
			)
		}
	} else {
		p.logger.Info("配置验证通过")
	}
	return result, nil
}

func (p *ConfigPublisher) CreateSnapshot(ctx context.Context) (*revision.Snapshot, error) {
	p.logger.Info("开始创建配置快照")

	snap := &revision.Snapshot{
		Timestamp:       time.Now(),
		Services:        make([]revision.ServiceSnapshot, 0),
		Routes:          make([]revision.RouteSnapshot, 0),
		Upstreams:       make([]revision.UpstreamSnapshot, 0),
		Consumers:       make([]revision.ConsumerSnapshot, 0),
		Plugins:         make([]revision.PluginSnapshot, 0),
		TrafficPolicies: make([]revision.TrafficPolicySnapshot, 0),
	}

	services, err := p.serviceRepo.List(ctx, &repository.Pagination{Page: 1, PageSize: 1000})
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	for _, svc := range services.Items {
		if svc.Enabled {
			snap.Services = append(snap.Services, revision.ServiceSnapshot{
				ID:             svc.ID,
				Name:           svc.Name,
				Protocol:       string(svc.Protocol),
				Host:           svc.Host,
				Port:           svc.Port,
				Path:           svc.Path,
				Retries:        svc.Retries,
				ConnectTimeout: svc.ConnectTimeout,
				WriteTimeout:   svc.WriteTimeout,
				ReadTimeout:    svc.ReadTimeout,
				Tags:           svc.Tags,
				Enabled:        svc.Enabled,
			})
		}
	}

	routes, err := p.routeRepo.List(ctx, &repository.Pagination{Page: 1, PageSize: 1000})
	if err != nil {
		return nil, fmt.Errorf("failed to list routes: %w", err)
	}

	for _, r := range routes.Items {
		if r.Enabled {
			snap.Routes = append(snap.Routes, revision.RouteSnapshot{
				ID:            r.ID,
				Name:          r.Name,
				ServiceID:     r.ServiceID,
				Protocols:     r.Protocols,
				Methods:       r.Methods,
				Hosts:         r.Hosts,
				Paths:         r.Paths,
				Headers:       r.Headers,
				StripPath:     r.StripPath,
				PreserveHost:  r.PreserveHost,
				RegexPriority: r.RegexPriority,
				Tags:          r.Tags,
				Enabled:       r.Enabled,
			})
		}
	}

	upstreams, err := p.upstreamRepo.List(ctx, &repository.Pagination{Page: 1, PageSize: 1000})
	if err != nil {
		return nil, fmt.Errorf("failed to list upstreams: %w", err)
	}

	for _, u := range upstreams.Items {
		targets, err := p.targetRepo.ListByUpstreamID(ctx, u.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to list targets for upstream %s: %w", u.ID, err)
		}

		targetSnapshots := make([]revision.TargetSnapshot, 0)
		for _, t := range targets {
			if t.Enabled {
				targetSnapshots = append(targetSnapshots, revision.TargetSnapshot{
					ID:      t.ID,
					Target:  t.Target,
					Port:    t.Port,
					Weight:  t.Weight,
					Enabled: t.Enabled,
				})
			}
		}

		snap.Upstreams = append(snap.Upstreams, revision.UpstreamSnapshot{
			ID:        u.ID,
			Name:      u.Name,
			Algorithm: string(u.Algorithm),
			Slots:     u.Slots,
			Targets:   targetSnapshots,
			Tags:      u.Tags,
		})
	}

	trafficPolicies, err := p.trafficPolicyRepo.List(ctx, &repository.Pagination{Page: 1, PageSize: 1000})
	if err != nil {
		return nil, fmt.Errorf("failed to list traffic policies: %w", err)
	}

	for _, tp := range trafficPolicies.Items {
		snap.TrafficPolicies = append(snap.TrafficPolicies, revision.TrafficPolicySnapshot{
			ID:              tp.ID,
			Name:            tp.Name,
			RouteID:         tp.RouteID,
			Priority:        tp.Priority,
			Type:            string(tp.Type),
			MatchConfig:     tp.MatchConfig,
			TargetServiceID: tp.TargetServiceID,
			Enabled:         tp.Enabled,
			Tags:            tp.Tags,
		})
	}

	p.logger.Info("配置快照创建完成",
		zap.Int("services_count", len(snap.Services)),
		zap.Int("routes_count", len(snap.Routes)),
		zap.Int("upstreams_count", len(snap.Upstreams)),
		zap.Int("traffic_policies_count", len(snap.TrafficPolicies)),
	)

	return snap, nil
}

func (p *ConfigPublisher) CreateRevision(ctx context.Context, version, description string, createdBy *uuid.UUID, auditCtx *repository.AuditContext) (*PublishResult, error) {
	p.logger.Info("开始创建 revision", zap.String("version", version))

	validationResult, err := p.Validate(ctx)
	if err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}
	if !validationResult.Valid {
		return nil, validator.ErrValidationFailed
	}

	snap, err := p.CreateSnapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot: %w", err)
	}

	rev, err := revision.NewFromSnapshot(version, snap, createdBy)
	if err != nil {
		return nil, fmt.Errorf("failed to create revision: %w", err)
	}
	rev.Description = description

	if err := p.revisionRepo.Create(ctx, rev, auditCtx); err != nil {
		return nil, fmt.Errorf("failed to save revision: %w", err)
	}

	p.logger.Info("Revision 创建成功", zap.Stringer("revision_id", rev.ID), zap.String("version", version))

	return &PublishResult{
		RevisionID: rev.ID,
		Version:    rev.Version,
		IsActive:   rev.IsActive,
		CreatedAt:  rev.CreatedAt,
	}, nil
}

func (p *ConfigPublisher) Publish(ctx context.Context, revisionID uuid.UUID, auditCtx *repository.AuditContext) (*PublishResult, error) {
	startTime := time.Now()
	p.logger.Info("开始发布 revision",
		zap.String("operation", "publish"),
		zap.Stringer("revision_id", revisionID),
		zap.Stringer("instance_id", p.instanceID),
	)

	rev, err := p.revisionRepo.GetByID(ctx, revisionID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			p.logger.Warn("发布失败: revision 不存在",
				zap.String("operation", "publish"),
				zap.Stringer("revision_id", revisionID),
				zap.String("error_type", "revision_not_found"),
				zap.Error(err),
			)
			return nil, fmt.Errorf("revision %s not found: %w", revisionID, err)
		}
		p.logger.Error("发布失败: 获取 revision 出错",
			zap.String("operation", "publish"),
			zap.Stringer("revision_id", revisionID),
			zap.String("error_type", "get_revision_failed"),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to get revision: %w", err)
	}

	if rev.IsActive {
		p.logger.Warn("发布跳过: revision 已经是 active 状态",
			zap.String("operation", "publish"),
			zap.Stringer("revision_id", revisionID),
			zap.String("version", rev.Version),
			zap.String("status", "already_active"),
		)
		return &PublishResult{
			RevisionID: rev.ID,
			Version:    rev.Version,
			IsActive:   true,
			CreatedAt:  rev.CreatedAt,
		}, nil
	}

	var activateErr error
	if p.lockRepo != nil {
		p.logger.Debug("使用分布式锁进行发布",
			zap.String("operation", "publish"),
			zap.Stringer("revision_id", revisionID),
			zap.Stringer("tenant_id", rev.TenantID),
		)
		activateErr = p.revisionRepo.ActivateWithLock(ctx, revisionID, auditCtx, p.lockRepo, p.instanceID)
	} else {
		p.logger.Warn("未启用分布式锁，可能存在并发冲突风险",
			zap.String("operation", "publish"),
			zap.Stringer("revision_id", revisionID),
		)
		activateErr = p.revisionRepo.Activate(ctx, revisionID, auditCtx)
	}

	if activateErr != nil {
		if errors.Is(activateErr, repository.ErrRevisionPublishConflict) {
			p.logger.Warn("发布失败: 并发冲突",
				zap.String("operation", "publish"),
				zap.Stringer("revision_id", revisionID),
				zap.String("error_type", "concurrency_conflict"),
				zap.Duration("duration", time.Since(startTime)),
				zap.Error(activateErr),
			)
			return nil, ErrPublishConcurrency
		}
		if errors.Is(activateErr, repository.ErrLockAlreadyHeld) {
			p.logger.Warn("发布失败: 锁被其他实例持有",
				zap.String("operation", "publish"),
				zap.Stringer("revision_id", revisionID),
				zap.String("error_type", "lock_held"),
				zap.Duration("duration", time.Since(startTime)),
				zap.Error(activateErr),
			)
			return nil, ErrPublishConcurrency
		}
		p.logger.Error("发布失败: 激活 revision 出错",
			zap.String("operation", "publish"),
			zap.Stringer("revision_id", revisionID),
			zap.String("version", rev.Version),
			zap.String("error_type", "activate_failed"),
			zap.Duration("duration", time.Since(startTime)),
			zap.Error(activateErr),
		)
		return nil, fmt.Errorf("failed to activate revision: %w", activateErr)
	}

	p.logger.Info("发布成功",
		zap.String("operation", "publish"),
		zap.Stringer("revision_id", revisionID),
		zap.String("version", rev.Version),
		zap.String("status", "success"),
		zap.Duration("duration", time.Since(startTime)),
	)

	return &PublishResult{
		RevisionID: revisionID,
		Version:    rev.Version,
		IsActive:   true,
		CreatedAt:  rev.CreatedAt,
	}, nil
}

func (p *ConfigPublisher) CreateAndPublish(ctx context.Context, version, description string, createdBy *uuid.UUID, auditCtx *repository.AuditContext) (*PublishResult, error) {
	p.logger.Info("开始创建并发布 revision", zap.String("version", version))

	result, err := p.CreateRevision(ctx, version, description, createdBy, auditCtx)
	if err != nil {
		return nil, err
	}

	publishResult, err := p.Publish(ctx, result.RevisionID, auditCtx)
	if err != nil {
		return nil, err
	}

	return publishResult, nil
}

func (p *ConfigPublisher) GetActiveRevision(ctx context.Context) (*revision.ConfigRevision, error) {
	rev, err := p.revisionRepo.GetActive(ctx)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrNoActiveRevision
		}
		return nil, err
	}
	return rev, nil
}

func (p *ConfigPublisher) ListRevisions(ctx context.Context, page, pageSize int) (*repository.PageResult[*revision.ConfigRevision], error) {
	return p.revisionRepo.List(ctx, &repository.Pagination{
		Page:     page,
		PageSize: pageSize,
	})
}

func (p *ConfigPublisher) Rollback(ctx context.Context, targetRevisionID uuid.UUID, auditCtx *repository.AuditContext) (*RollbackResult, error) {
	startTime := time.Now()
	p.logger.Info("开始回滚到 revision",
		zap.String("operation", "rollback"),
		zap.Stringer("target_revision_id", targetRevisionID),
		zap.Stringer("instance_id", p.instanceID),
	)

	currentActive, err := p.GetActiveRevision(ctx)
	if err != nil && !errors.Is(err, ErrNoActiveRevision) {
		p.logger.Error("回滚失败: 获取当前 active revision 出错",
			zap.String("operation", "rollback"),
			zap.Stringer("target_revision_id", targetRevisionID),
			zap.String("error_type", "get_active_failed"),
			zap.Error(err),
		)
		return nil, err
	}

	var fromVersion string
	if currentActive != nil {
		fromVersion = currentActive.Version
	}

	targetRev, err := p.revisionRepo.GetByID(ctx, targetRevisionID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			p.logger.Warn("回滚失败: 目标 revision 不存在",
				zap.String("operation", "rollback"),
				zap.Stringer("target_revision_id", targetRevisionID),
				zap.String("error_type", "target_not_found"),
				zap.Error(err),
			)
			return nil, fmt.Errorf("target revision %s not found: %w", targetRevisionID, err)
		}
		p.logger.Error("回滚失败: 获取目标 revision 出错",
			zap.String("operation", "rollback"),
			zap.Stringer("target_revision_id", targetRevisionID),
			zap.String("error_type", "get_target_failed"),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to get target revision: %w", err)
	}

	if currentActive != nil && currentActive.ID == targetRevisionID {
		p.logger.Warn("回滚跳过: 目标 revision 已经是 active 状态",
			zap.String("operation", "rollback"),
			zap.Stringer("target_revision_id", targetRevisionID),
			zap.String("version", targetRev.Version),
			zap.String("status", "already_active"),
		)
		return &RollbackResult{
			RevisionID: targetRev.ID,
			Version:    targetRev.Version,
			IsActive:   true,
		}, nil
	}

	var activateErr error
	if p.lockRepo != nil {
		p.logger.Debug("使用分布式锁进行回滚",
			zap.String("operation", "rollback"),
			zap.Stringer("target_revision_id", targetRevisionID),
			zap.Stringer("tenant_id", targetRev.TenantID),
		)
		activateErr = p.revisionRepo.ActivateWithLock(ctx, targetRevisionID, auditCtx, p.lockRepo, p.instanceID)
	} else {
		p.logger.Warn("未启用分布式锁，可能存在并发冲突风险",
			zap.String("operation", "rollback"),
			zap.Stringer("target_revision_id", targetRevisionID),
		)
		activateErr = p.revisionRepo.Activate(ctx, targetRevisionID, auditCtx)
	}

	if activateErr != nil {
		if errors.Is(activateErr, repository.ErrRevisionPublishConflict) {
			p.logger.Warn("回滚失败: 并发冲突",
				zap.String("operation", "rollback"),
				zap.Stringer("target_revision_id", targetRevisionID),
				zap.String("error_type", "concurrency_conflict"),
				zap.Duration("duration", time.Since(startTime)),
				zap.Error(activateErr),
			)
			return nil, ErrPublishConcurrency
		}
		if errors.Is(activateErr, repository.ErrLockAlreadyHeld) {
			p.logger.Warn("回滚失败: 锁被其他实例持有",
				zap.String("operation", "rollback"),
				zap.Stringer("target_revision_id", targetRevisionID),
				zap.String("error_type", "lock_held"),
				zap.Duration("duration", time.Since(startTime)),
				zap.Error(activateErr),
			)
			return nil, ErrPublishConcurrency
		}
		p.logger.Error("回滚失败: 激活目标 revision 出错",
			zap.String("operation", "rollback"),
			zap.Stringer("from_revision_id", currentActive.ID),
			zap.String("from_version", fromVersion),
			zap.Stringer("target_revision_id", targetRevisionID),
			zap.String("target_version", targetRev.Version),
			zap.String("error_type", "activate_failed"),
			zap.Duration("duration", time.Since(startTime)),
			zap.Error(activateErr),
		)
		return nil, fmt.Errorf("failed to activate target revision: %w", activateErr)
	}

	var fromRevisionID uuid.UUID
	if currentActive != nil {
		fromRevisionID = currentActive.ID
	}
	p.logger.Info("回滚成功",
		zap.String("operation", "rollback"),
		zap.Stringer("from_revision_id", fromRevisionID),
		zap.String("from_version", fromVersion),
		zap.Stringer("to_revision_id", targetRevisionID),
		zap.String("to_version", targetRev.Version),
		zap.String("status", "success"),
		zap.Duration("duration", time.Since(startTime)),
	)

	return &RollbackResult{
		RevisionID: targetRev.ID,
		Version:    targetRev.Version,
		IsActive:   true,
	}, nil
}

func (p *ConfigPublisher) GetRevision(ctx context.Context, revisionID uuid.UUID) (*revision.ConfigRevision, error) {
	return p.revisionRepo.GetByID(ctx, revisionID)
}
