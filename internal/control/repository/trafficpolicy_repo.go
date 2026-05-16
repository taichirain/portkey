package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/taichirain/portkey/internal/domain/trafficpolicy"
	"github.com/taichirain/portkey/internal/platform/postgres"
)

type TrafficPolicyRepository interface {
	Repository
	Create(ctx context.Context, tp *trafficpolicy.TrafficPolicy, auditCtx *AuditContext) error
	GetByID(ctx context.Context, id uuid.UUID) (*trafficpolicy.TrafficPolicy, error)
	ListByRouteID(ctx context.Context, routeID uuid.UUID) ([]*trafficpolicy.TrafficPolicy, error)
	List(ctx context.Context, pagination *Pagination) (*PageResult[*trafficpolicy.TrafficPolicy], error)
	Update(ctx context.Context, tp *trafficpolicy.TrafficPolicy, auditCtx *AuditContext) error
	Delete(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error
}

type PostgresTrafficPolicyRepository struct {
	db        *postgres.DB
	auditRepo AuditRepository
}

func NewPostgresTrafficPolicyRepository(db *postgres.DB, auditRepo AuditRepository) *PostgresTrafficPolicyRepository {
	return &PostgresTrafficPolicyRepository{
		db:        db,
		auditRepo: auditRepo,
	}
}

func (r *PostgresTrafficPolicyRepository) BeginTx(ctx context.Context) (Tx, error) {
	return r.db.BeginTx(ctx)
}

func (r *PostgresTrafficPolicyRepository) Create(ctx context.Context, tp *trafficpolicy.TrafficPolicy, auditCtx *AuditContext) error {
	if err := tp.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	if tp.TenantID == uuid.Nil {
		if tenantID, ok := GetTenantIDFromContext(ctx); ok {
			tp.TenantID = tenantID
		}
	}

	if auditCtx == nil {
		return r.createWithoutAudit(ctx, tp)
	}

	return r.createWithAudit(ctx, tp, auditCtx)
}

func (r *PostgresTrafficPolicyRepository) createWithoutAudit(ctx context.Context, tp *trafficpolicy.TrafficPolicy) error {
	query := `
		INSERT INTO traffic_policies (id, tenant_id, name, route_id, priority, type, match_config, target_service_id, enabled, tags, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`

	_, err := r.db.ExecContext(ctx, query,
		tp.ID,
		tp.TenantID,
		nullString(tp.Name),
		tp.RouteID,
		tp.Priority,
		string(tp.Type),
		tp.MatchConfig,
		tp.TargetServiceID,
		tp.Enabled,
		pq.Array(tp.Tags),
		tp.CreatedAt,
		tp.UpdatedAt,
	)

	if err != nil {
		if isForeignKeyViolation(err) {
			return fmt.Errorf("%w: route or service not found", ErrInvalidInput)
		}
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: priority already exists for this route", ErrAlreadyExists)
		}
		return err
	}

	return nil
}

func (r *PostgresTrafficPolicyRepository) createWithAudit(ctx context.Context, tp *trafficpolicy.TrafficPolicy, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
		INSERT INTO traffic_policies (id, tenant_id, name, route_id, priority, type, match_config, target_service_id, enabled, tags, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`

	_, err = tx.ExecContext(ctx, query,
		tp.ID,
		tp.TenantID,
		nullString(tp.Name),
		tp.RouteID,
		tp.Priority,
		string(tp.Type),
		tp.MatchConfig,
		tp.TargetServiceID,
		tp.Enabled,
		pq.Array(tp.Tags),
		tp.CreatedAt,
		tp.UpdatedAt,
	)

	if err != nil {
		if isForeignKeyViolation(err) {
			return fmt.Errorf("%w: route or service not found", ErrInvalidInput)
		}
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: priority already exists for this route", ErrAlreadyExists)
		}
		return err
	}

	if err := auditRepo.logCreateWithTx(ctx, tx, auditCtx, "traffic_policy", tp.ID, tp); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (r *PostgresTrafficPolicyRepository) GetByID(ctx context.Context, id uuid.UUID) (*trafficpolicy.TrafficPolicy, error) {
	return r.getByID(ctx, r.db, id)
}

func (r *PostgresTrafficPolicyRepository) getByID(ctx context.Context, exec Executor, id uuid.UUID) (*trafficpolicy.TrafficPolicy, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, name, route_id, priority, type, match_config, target_service_id, enabled, tags, created_at, updated_at
			FROM traffic_policies
			WHERE id = $1 AND tenant_id = $2
		`
		args = []interface{}{id, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, name, route_id, priority, type, match_config, target_service_id, enabled, tags, created_at, updated_at
			FROM traffic_policies
			WHERE id = $1
		`
		args = []interface{}{id}
	}

	var tp trafficpolicy.TrafficPolicy
	var name sql.NullString
	var policyType string

	err := exec.QueryRowContext(ctx, query, args...).Scan(
		&tp.ID,
		&tp.TenantID,
		&name,
		&tp.RouteID,
		&tp.Priority,
		&policyType,
		&tp.MatchConfig,
		&tp.TargetServiceID,
		&tp.Enabled,
		pq.Array(&tp.Tags),
		&tp.CreatedAt,
		&tp.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	tp.Name = name.String
	tp.Type = trafficpolicy.PolicyType(policyType)

	return &tp, nil
}

func (r *PostgresTrafficPolicyRepository) ListByRouteID(ctx context.Context, routeID uuid.UUID) ([]*trafficpolicy.TrafficPolicy, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, name, route_id, priority, type, match_config, target_service_id, enabled, tags, created_at, updated_at
			FROM traffic_policies
			WHERE route_id = $1 AND tenant_id = $2
			ORDER BY priority ASC
		`
		args = []interface{}{routeID, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, name, route_id, priority, type, match_config, target_service_id, enabled, tags, created_at, updated_at
			FROM traffic_policies
			WHERE route_id = $1
			ORDER BY priority ASC
		`
		args = []interface{}{routeID}
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*trafficpolicy.TrafficPolicy, 0)
	for rows.Next() {
		var tp trafficpolicy.TrafficPolicy
		var name sql.NullString
		var policyType string

		if err := rows.Scan(
			&tp.ID,
			&tp.TenantID,
			&name,
			&tp.RouteID,
			&tp.Priority,
			&policyType,
			&tp.MatchConfig,
			&tp.TargetServiceID,
			&tp.Enabled,
			pq.Array(&tp.Tags),
			&tp.CreatedAt,
			&tp.UpdatedAt,
		); err != nil {
			return nil, err
		}

		tp.Name = name.String
		tp.Type = trafficpolicy.PolicyType(policyType)
		items = append(items, &tp)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (r *PostgresTrafficPolicyRepository) List(ctx context.Context, pagination *Pagination) (*PageResult[*trafficpolicy.TrafficPolicy], error) {
	if pagination == nil {
		pagination = &Pagination{Page: 1, PageSize: 50}
	}
	if pagination.Page < 1 {
		pagination.Page = 1
	}
	if pagination.PageSize < 1 || pagination.PageSize > 100 {
		pagination.PageSize = 50
	}

	offset := (pagination.Page - 1) * pagination.PageSize
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var countQuery string
	var countArgs []interface{}
	var listQuery string
	var listArgs []interface{}

	if hasTenant && tenantID != uuid.Nil {
		countQuery = `SELECT COUNT(*) FROM traffic_policies WHERE tenant_id = $1`
		countArgs = []interface{}{tenantID}

		listQuery = `
			SELECT id, tenant_id, name, route_id, priority, type, match_config, target_service_id, enabled, tags, created_at, updated_at
			FROM traffic_policies
			WHERE tenant_id = $1
			ORDER BY created_at DESC
			LIMIT $2 OFFSET $3
		`
		listArgs = []interface{}{tenantID, pagination.PageSize, offset}
	} else {
		countQuery = `SELECT COUNT(*) FROM traffic_policies`
		countArgs = []interface{}{}

		listQuery = `
			SELECT id, tenant_id, name, route_id, priority, type, match_config, target_service_id, enabled, tags, created_at, updated_at
			FROM traffic_policies
			ORDER BY created_at DESC
			LIMIT $1 OFFSET $2
		`
		listArgs = []interface{}{pagination.PageSize, offset}
	}

	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, err
	}

	rows, err := r.db.QueryContext(ctx, listQuery, listArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*trafficpolicy.TrafficPolicy, 0)
	for rows.Next() {
		var tp trafficpolicy.TrafficPolicy
		var name sql.NullString
		var policyType string

		if err := rows.Scan(
			&tp.ID,
			&tp.TenantID,
			&name,
			&tp.RouteID,
			&tp.Priority,
			&policyType,
			&tp.MatchConfig,
			&tp.TargetServiceID,
			&tp.Enabled,
			pq.Array(&tp.Tags),
			&tp.CreatedAt,
			&tp.UpdatedAt,
		); err != nil {
			return nil, err
		}

		tp.Name = name.String
		tp.Type = trafficpolicy.PolicyType(policyType)
		items = append(items, &tp)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return NewPageResult(items, total, pagination.Page, pagination.PageSize), nil
}

func (r *PostgresTrafficPolicyRepository) Update(ctx context.Context, tp *trafficpolicy.TrafficPolicy, auditCtx *AuditContext) error {
	if err := tp.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	if auditCtx == nil {
		return r.updateWithoutAudit(ctx, tp)
	}

	return r.updateWithAudit(ctx, tp, auditCtx)
}

func (r *PostgresTrafficPolicyRepository) updateWithoutAudit(ctx context.Context, tp *trafficpolicy.TrafficPolicy) error {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			UPDATE traffic_policies
			SET name = $2, route_id = $3, priority = $4, type = $5, match_config = $6, target_service_id = $7, enabled = $8, tags = $9, updated_at = NOW()
			WHERE id = $1 AND tenant_id = $10
		`
		args = []interface{}{
			tp.ID,
			nullString(tp.Name),
			tp.RouteID,
			tp.Priority,
			string(tp.Type),
			tp.MatchConfig,
			tp.TargetServiceID,
			tp.Enabled,
			pq.Array(tp.Tags),
			tenantID,
		}
	} else {
		query = `
			UPDATE traffic_policies
			SET name = $2, route_id = $3, priority = $4, type = $5, match_config = $6, target_service_id = $7, enabled = $8, tags = $9, updated_at = NOW()
			WHERE id = $1
		`
		args = []interface{}{
			tp.ID,
			nullString(tp.Name),
			tp.RouteID,
			tp.Priority,
			string(tp.Type),
			tp.MatchConfig,
			tp.TargetServiceID,
			tp.Enabled,
			pq.Array(tp.Tags),
		}
	}

	result, err := r.db.ExecContext(ctx, query, args...)

	if err != nil {
		if isForeignKeyViolation(err) {
			return fmt.Errorf("%w: route or service not found", ErrInvalidInput)
		}
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: priority already exists for this route", ErrAlreadyExists)
		}
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

func (r *PostgresTrafficPolicyRepository) updateWithAudit(ctx context.Context, tp *trafficpolicy.TrafficPolicy, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	oldPolicy, err := r.getByID(ctx, tx, tp.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}

	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			UPDATE traffic_policies
			SET name = $2, route_id = $3, priority = $4, type = $5, match_config = $6, target_service_id = $7, enabled = $8, tags = $9, updated_at = NOW()
			WHERE id = $1 AND tenant_id = $10
		`
		args = []interface{}{
			tp.ID,
			nullString(tp.Name),
			tp.RouteID,
			tp.Priority,
			string(tp.Type),
			tp.MatchConfig,
			tp.TargetServiceID,
			tp.Enabled,
			pq.Array(tp.Tags),
			tenantID,
		}
	} else {
		query = `
			UPDATE traffic_policies
			SET name = $2, route_id = $3, priority = $4, type = $5, match_config = $6, target_service_id = $7, enabled = $8, tags = $9, updated_at = NOW()
			WHERE id = $1
		`
		args = []interface{}{
			tp.ID,
			nullString(tp.Name),
			tp.RouteID,
			tp.Priority,
			string(tp.Type),
			tp.MatchConfig,
			tp.TargetServiceID,
			tp.Enabled,
			pq.Array(tp.Tags),
		}
	}

	result, err := tx.ExecContext(ctx, query, args...)

	if err != nil {
		if isForeignKeyViolation(err) {
			return fmt.Errorf("%w: route or service not found", ErrInvalidInput)
		}
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: priority already exists for this route", ErrAlreadyExists)
		}
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}

	if err := auditRepo.logUpdateWithTx(ctx, tx, auditCtx, "traffic_policy", tp.ID, oldPolicy, tp); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (r *PostgresTrafficPolicyRepository) Delete(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error {
	if auditCtx == nil {
		return r.deleteWithoutAudit(ctx, id)
	}

	return r.deleteWithAudit(ctx, id, auditCtx)
}

func (r *PostgresTrafficPolicyRepository) deleteWithoutAudit(ctx context.Context, id uuid.UUID) error {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `DELETE FROM traffic_policies WHERE id = $1 AND tenant_id = $2`
		args = []interface{}{id, tenantID}
	} else {
		query = `DELETE FROM traffic_policies WHERE id = $1`
		args = []interface{}{id}
	}

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

func (r *PostgresTrafficPolicyRepository) deleteWithAudit(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	oldPolicy, err := r.getByID(ctx, tx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}

	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `DELETE FROM traffic_policies WHERE id = $1 AND tenant_id = $2`
		args = []interface{}{id, tenantID}
	} else {
		query = `DELETE FROM traffic_policies WHERE id = $1`
		args = []interface{}{id}
	}

	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}

	if err := auditRepo.logDeleteWithTx(ctx, tx, auditCtx, "traffic_policy", id, oldPolicy); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}
