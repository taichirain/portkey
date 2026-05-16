package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/taichirain/portkey/internal/domain/target"
	"github.com/taichirain/portkey/internal/platform/postgres"
)

type TargetRepository interface {
	Repository
	Create(ctx context.Context, t *target.Target, auditCtx *AuditContext) error
	GetByID(ctx context.Context, id uuid.UUID) (*target.Target, error)
	ListByUpstreamID(ctx context.Context, upstreamID uuid.UUID) ([]*target.Target, error)
	List(ctx context.Context, pagination *Pagination) (*PageResult[*target.Target], error)
	Update(ctx context.Context, t *target.Target, auditCtx *AuditContext) error
	Delete(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error
}

type PostgresTargetRepository struct {
	db        *postgres.DB
	auditRepo AuditRepository
}

func NewPostgresTargetRepository(db *postgres.DB, auditRepo AuditRepository) *PostgresTargetRepository {
	return &PostgresTargetRepository{
		db:        db,
		auditRepo: auditRepo,
	}
}

func (r *PostgresTargetRepository) BeginTx(ctx context.Context) (Tx, error) {
	return r.db.BeginTx(ctx)
}

func (r *PostgresTargetRepository) Create(ctx context.Context, t *target.Target, auditCtx *AuditContext) error {
	if err := t.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	if t.TenantID == uuid.Nil {
		if tenantID, ok := GetTenantIDFromContext(ctx); ok {
			t.TenantID = tenantID
		}
	}

	if auditCtx == nil {
		return r.createWithoutAudit(ctx, t)
	}

	return r.createWithAudit(ctx, t, auditCtx)
}

func (r *PostgresTargetRepository) createWithoutAudit(ctx context.Context, t *target.Target) error {
	query := `
		INSERT INTO targets (id, tenant_id, upstream_id, target, port, weight, tags, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	_, err := r.db.ExecContext(ctx, query,
		t.ID,
		t.TenantID,
		t.UpstreamID,
		t.Target,
		t.Port,
		t.Weight,
		pq.Array(t.Tags),
		t.Enabled,
		t.CreatedAt,
		t.UpdatedAt,
	)

	if err != nil {
		if isForeignKeyViolation(err) {
			return fmt.Errorf("%w: upstream not found", ErrInvalidInput)
		}
		return err
	}

	return nil
}

func (r *PostgresTargetRepository) createWithAudit(ctx context.Context, t *target.Target, auditCtx *AuditContext) error {
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
		INSERT INTO targets (id, tenant_id, upstream_id, target, port, weight, tags, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	_, err = tx.ExecContext(ctx, query,
		t.ID,
		t.TenantID,
		t.UpstreamID,
		t.Target,
		t.Port,
		t.Weight,
		pq.Array(t.Tags),
		t.Enabled,
		t.CreatedAt,
		t.UpdatedAt,
	)

	if err != nil {
		if isForeignKeyViolation(err) {
			return fmt.Errorf("%w: upstream not found", ErrInvalidInput)
		}
		return err
	}

	if err := auditRepo.logCreateWithTx(ctx, tx, auditCtx, "target", t.ID, t); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (r *PostgresTargetRepository) GetByID(ctx context.Context, id uuid.UUID) (*target.Target, error) {
	return r.getByID(ctx, r.db, id)
}

func (r *PostgresTargetRepository) getByID(ctx context.Context, exec Executor, id uuid.UUID) (*target.Target, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, upstream_id, target, port, weight, tags, enabled, created_at, updated_at
			FROM targets
			WHERE id = $1 AND tenant_id = $2
		`
		args = []interface{}{id, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, upstream_id, target, port, weight, tags, enabled, created_at, updated_at
			FROM targets
			WHERE id = $1
		`
		args = []interface{}{id}
	}

	var t target.Target
	err := exec.QueryRowContext(ctx, query, args...).Scan(
		&t.ID,
		&t.TenantID,
		&t.UpstreamID,
		&t.Target,
		&t.Port,
		&t.Weight,
		pq.Array(&t.Tags),
		&t.Enabled,
		&t.CreatedAt,
		&t.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &t, nil
}

func (r *PostgresTargetRepository) ListByUpstreamID(ctx context.Context, upstreamID uuid.UUID) ([]*target.Target, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, upstream_id, target, port, weight, tags, enabled, created_at, updated_at
			FROM targets
			WHERE upstream_id = $1 AND tenant_id = $2
			ORDER BY created_at DESC
		`
		args = []interface{}{upstreamID, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, upstream_id, target, port, weight, tags, enabled, created_at, updated_at
			FROM targets
			WHERE upstream_id = $1
			ORDER BY created_at DESC
		`
		args = []interface{}{upstreamID}
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*target.Target, 0)
	for rows.Next() {
		var t target.Target
		if err := rows.Scan(
			&t.ID,
			&t.TenantID,
			&t.UpstreamID,
			&t.Target,
			&t.Port,
			&t.Weight,
			pq.Array(&t.Tags),
			&t.Enabled,
			&t.CreatedAt,
			&t.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, &t)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (r *PostgresTargetRepository) List(ctx context.Context, pagination *Pagination) (*PageResult[*target.Target], error) {
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
		countQuery = `SELECT COUNT(*) FROM targets WHERE tenant_id = $1`
		countArgs = []interface{}{tenantID}

		listQuery = `
			SELECT id, tenant_id, upstream_id, target, port, weight, tags, enabled, created_at, updated_at
			FROM targets
			WHERE tenant_id = $1
			ORDER BY created_at DESC
			LIMIT $2 OFFSET $3
		`
		listArgs = []interface{}{tenantID, pagination.PageSize, offset}
	} else {
		countQuery = `SELECT COUNT(*) FROM targets`
		countArgs = []interface{}{}

		listQuery = `
			SELECT id, tenant_id, upstream_id, target, port, weight, tags, enabled, created_at, updated_at
			FROM targets
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

	items := make([]*target.Target, 0)
	for rows.Next() {
		var t target.Target
		if err := rows.Scan(
			&t.ID,
			&t.TenantID,
			&t.UpstreamID,
			&t.Target,
			&t.Port,
			&t.Weight,
			pq.Array(&t.Tags),
			&t.Enabled,
			&t.CreatedAt,
			&t.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, &t)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return NewPageResult(items, total, pagination.Page, pagination.PageSize), nil
}

func (r *PostgresTargetRepository) Update(ctx context.Context, t *target.Target, auditCtx *AuditContext) error {
	if err := t.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	if auditCtx == nil {
		return r.updateWithoutAudit(ctx, t)
	}

	return r.updateWithAudit(ctx, t, auditCtx)
}

func (r *PostgresTargetRepository) updateWithoutAudit(ctx context.Context, t *target.Target) error {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			UPDATE targets
			SET upstream_id = $2, target = $3, port = $4, weight = $5, tags = $6, enabled = $7, updated_at = NOW()
			WHERE id = $1 AND tenant_id = $8
		`
		args = []interface{}{
			t.ID,
			t.UpstreamID,
			t.Target,
			t.Port,
			t.Weight,
			pq.Array(t.Tags),
			t.Enabled,
			tenantID,
		}
	} else {
		query = `
			UPDATE targets
			SET upstream_id = $2, target = $3, port = $4, weight = $5, tags = $6, enabled = $7, updated_at = NOW()
			WHERE id = $1
		`
		args = []interface{}{
			t.ID,
			t.UpstreamID,
			t.Target,
			t.Port,
			t.Weight,
			pq.Array(t.Tags),
			t.Enabled,
		}
	}

	result, err := r.db.ExecContext(ctx, query, args...)

	if err != nil {
		if isForeignKeyViolation(err) {
			return fmt.Errorf("%w: upstream not found", ErrInvalidInput)
		}
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

func (r *PostgresTargetRepository) updateWithAudit(ctx context.Context, t *target.Target, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	oldTarget, err := r.getByID(ctx, tx, t.ID)
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
			UPDATE targets
			SET upstream_id = $2, target = $3, port = $4, weight = $5, tags = $6, enabled = $7, updated_at = NOW()
			WHERE id = $1 AND tenant_id = $8
		`
		args = []interface{}{
			t.ID,
			t.UpstreamID,
			t.Target,
			t.Port,
			t.Weight,
			pq.Array(t.Tags),
			t.Enabled,
			tenantID,
		}
	} else {
		query = `
			UPDATE targets
			SET upstream_id = $2, target = $3, port = $4, weight = $5, tags = $6, enabled = $7, updated_at = NOW()
			WHERE id = $1
		`
		args = []interface{}{
			t.ID,
			t.UpstreamID,
			t.Target,
			t.Port,
			t.Weight,
			pq.Array(t.Tags),
			t.Enabled,
		}
	}

	result, err := tx.ExecContext(ctx, query, args...)

	if err != nil {
		if isForeignKeyViolation(err) {
			return fmt.Errorf("%w: upstream not found", ErrInvalidInput)
		}
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}

	if err := auditRepo.logUpdateWithTx(ctx, tx, auditCtx, "target", t.ID, oldTarget, t); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (r *PostgresTargetRepository) Delete(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error {
	if auditCtx == nil {
		return r.deleteWithoutAudit(ctx, id)
	}

	return r.deleteWithAudit(ctx, id, auditCtx)
}

func (r *PostgresTargetRepository) deleteWithoutAudit(ctx context.Context, id uuid.UUID) error {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `DELETE FROM targets WHERE id = $1 AND tenant_id = $2`
		args = []interface{}{id, tenantID}
	} else {
		query = `DELETE FROM targets WHERE id = $1`
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

func (r *PostgresTargetRepository) deleteWithAudit(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	oldTarget, err := r.getByID(ctx, tx, id)
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
		query = `DELETE FROM targets WHERE id = $1 AND tenant_id = $2`
		args = []interface{}{id, tenantID}
	} else {
		query = `DELETE FROM targets WHERE id = $1`
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

	if err := auditRepo.logDeleteWithTx(ctx, tx, auditCtx, "target", id, oldTarget); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}
