package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/domain/tenant"
	"github.com/taichirain/portkey/internal/platform/postgres"
)

type TenantRepository interface {
	Repository
	Create(ctx context.Context, t *tenant.Tenant, auditCtx *AuditContext) error
	GetByID(ctx context.Context, id uuid.UUID) (*tenant.Tenant, error)
	GetBySlug(ctx context.Context, slug string) (*tenant.Tenant, error)
	List(ctx context.Context, pagination *Pagination) (*PageResult[*tenant.Tenant], error)
	Update(ctx context.Context, t *tenant.Tenant, auditCtx *AuditContext) error
	Delete(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error
}

type PostgresTenantRepository struct {
	db        *postgres.DB
	auditRepo AuditRepository
}

func NewPostgresTenantRepository(db *postgres.DB, auditRepo AuditRepository) *PostgresTenantRepository {
	return &PostgresTenantRepository{
		db:        db,
		auditRepo: auditRepo,
	}
}

func (r *PostgresTenantRepository) BeginTx(ctx context.Context) (Tx, error) {
	return r.db.BeginTx(ctx)
}

func (r *PostgresTenantRepository) Create(ctx context.Context, t *tenant.Tenant, auditCtx *AuditContext) error {
	if err := t.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	if auditCtx == nil {
		return r.createWithoutAudit(ctx, t)
	}

	return r.createWithAudit(ctx, t, auditCtx)
}

func (r *PostgresTenantRepository) createWithoutAudit(ctx context.Context, t *tenant.Tenant) error {
	query := `
		INSERT INTO tenants (id, name, slug, description, settings, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	_, err := r.db.ExecContext(ctx, query,
		t.ID,
		t.Name,
		t.Slug,
		nullString(t.Description),
		t.Settings,
		t.Enabled,
		t.CreatedAt,
		t.UpdatedAt,
	)

	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}

	return nil
}

func (r *PostgresTenantRepository) createWithAudit(ctx context.Context, t *tenant.Tenant, auditCtx *AuditContext) error {
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
		INSERT INTO tenants (id, name, slug, description, settings, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	_, err = tx.ExecContext(ctx, query,
		t.ID,
		t.Name,
		t.Slug,
		nullString(t.Description),
		t.Settings,
		t.Enabled,
		t.CreatedAt,
		t.UpdatedAt,
	)

	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}

	if err := auditRepo.logCreateWithTx(ctx, tx, auditCtx, "tenant", t.ID, t); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (r *PostgresTenantRepository) GetByID(ctx context.Context, id uuid.UUID) (*tenant.Tenant, error) {
	return r.getByID(ctx, r.db, id)
}

func (r *PostgresTenantRepository) getByID(ctx context.Context, exec Executor, id uuid.UUID) (*tenant.Tenant, error) {
	query := `
		SELECT id, name, slug, description, settings, enabled, created_at, updated_at
		FROM tenants
		WHERE id = $1
	`

	var t tenant.Tenant
	var description sql.NullString
	var settings []byte

	err := exec.QueryRowContext(ctx, query, id).Scan(
		&t.ID,
		&t.Name,
		&t.Slug,
		&description,
		&settings,
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

	t.Description = description.String
	if len(settings) > 0 {
		t.Settings = settings
	} else {
		t.Settings = []byte("{}")
	}

	return &t, nil
}

func (r *PostgresTenantRepository) GetBySlug(ctx context.Context, slug string) (*tenant.Tenant, error) {
	query := `
		SELECT id, name, slug, description, settings, enabled, created_at, updated_at
		FROM tenants
		WHERE slug = $1
	`

	var t tenant.Tenant
	var description sql.NullString
	var settings []byte

	err := r.db.QueryRowContext(ctx, query, slug).Scan(
		&t.ID,
		&t.Name,
		&t.Slug,
		&description,
		&settings,
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

	t.Description = description.String
	if len(settings) > 0 {
		t.Settings = settings
	} else {
		t.Settings = []byte("{}")
	}

	return &t, nil
}

func (r *PostgresTenantRepository) List(ctx context.Context, pagination *Pagination) (*PageResult[*tenant.Tenant], error) {
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

	countQuery := `SELECT COUNT(*) FROM tenants`
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery).Scan(&total); err != nil {
		return nil, err
	}

	query := `
		SELECT id, name, slug, description, settings, enabled, created_at, updated_at
		FROM tenants
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := r.db.QueryContext(ctx, query, pagination.PageSize, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*tenant.Tenant, 0)
	for rows.Next() {
		var t tenant.Tenant
		var description sql.NullString
		var settings []byte

		if err := rows.Scan(
			&t.ID,
			&t.Name,
			&t.Slug,
			&description,
			&settings,
			&t.Enabled,
			&t.CreatedAt,
			&t.UpdatedAt,
		); err != nil {
			return nil, err
		}

		t.Description = description.String
		if len(settings) > 0 {
			t.Settings = settings
		} else {
			t.Settings = []byte("{}")
		}
		items = append(items, &t)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return NewPageResult(items, total, pagination.Page, pagination.PageSize), nil
}

func (r *PostgresTenantRepository) Update(ctx context.Context, t *tenant.Tenant, auditCtx *AuditContext) error {
	if err := t.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	if auditCtx == nil {
		return r.updateWithoutAudit(ctx, t)
	}

	return r.updateWithAudit(ctx, t, auditCtx)
}

func (r *PostgresTenantRepository) updateWithoutAudit(ctx context.Context, t *tenant.Tenant) error {
	query := `
		UPDATE tenants
		SET name = $2, slug = $3, description = $4, settings = $5, enabled = $6, updated_at = NOW()
		WHERE id = $1
	`

	result, err := r.db.ExecContext(ctx, query,
		t.ID,
		t.Name,
		t.Slug,
		nullString(t.Description),
		t.Settings,
		t.Enabled,
	)

	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

func (r *PostgresTenantRepository) updateWithAudit(ctx context.Context, t *tenant.Tenant, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	oldTenant, err := r.getByID(ctx, tx, t.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}

	query := `
		UPDATE tenants
		SET name = $2, slug = $3, description = $4, settings = $5, enabled = $6, updated_at = NOW()
		WHERE id = $1
	`

	result, err := tx.ExecContext(ctx, query,
		t.ID,
		t.Name,
		t.Slug,
		nullString(t.Description),
		t.Settings,
		t.Enabled,
	)

	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}

	if err := auditRepo.logUpdateWithTx(ctx, tx, auditCtx, "tenant", t.ID, oldTenant, t); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (r *PostgresTenantRepository) Delete(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error {
	if auditCtx == nil {
		return r.deleteWithoutAudit(ctx, id)
	}

	return r.deleteWithAudit(ctx, id, auditCtx)
}

func (r *PostgresTenantRepository) deleteWithoutAudit(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM tenants WHERE id = $1`
	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

func (r *PostgresTenantRepository) deleteWithAudit(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	oldTenant, err := r.getByID(ctx, tx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}

	query := `DELETE FROM tenants WHERE id = $1`
	result, err := tx.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}

	if err := auditRepo.logDeleteWithTx(ctx, tx, auditCtx, "tenant", id, oldTenant); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}
