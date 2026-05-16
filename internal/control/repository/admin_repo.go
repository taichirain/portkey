package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/domain/admin"
	"github.com/taichirain/portkey/internal/platform/postgres"
)

type AdminRepository interface {
	Repository
	Create(ctx context.Context, a *admin.Admin, auditCtx *AuditContext) error
	GetByID(ctx context.Context, id uuid.UUID) (*admin.Admin, error)
	GetByIDWithRolesAndPermissions(ctx context.Context, id uuid.UUID, tenantID uuid.UUID) (*admin.Admin, error)
	GetByUsername(ctx context.Context, username string) (*admin.Admin, error)
	GetByUsernameWithRolesAndPermissions(ctx context.Context, username string, tenantID uuid.UUID) (*admin.Admin, error)
	GetByEmail(ctx context.Context, email string) (*admin.Admin, error)
	List(ctx context.Context, pagination *Pagination) (*PageResult[*admin.Admin], error)
	ListByTenant(ctx context.Context, tenantID uuid.UUID, pagination *Pagination) (*PageResult[*admin.Admin], error)
	Update(ctx context.Context, a *admin.Admin, auditCtx *AuditContext) error
	Delete(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error
}

type PostgresAdminRepository struct {
	db        *postgres.DB
	auditRepo AuditRepository
}

func NewPostgresAdminRepository(db *postgres.DB, auditRepo AuditRepository) *PostgresAdminRepository {
	return &PostgresAdminRepository{
		db:        db,
		auditRepo: auditRepo,
	}
}

func (r *PostgresAdminRepository) BeginTx(ctx context.Context) (Tx, error) {
	return r.db.BeginTx(ctx)
}

func (r *PostgresAdminRepository) Create(ctx context.Context, a *admin.Admin, auditCtx *AuditContext) error {
	if err := a.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	if auditCtx == nil {
		return r.createWithoutAudit(ctx, a)
	}

	return r.createWithAudit(ctx, a, auditCtx)
}

func (r *PostgresAdminRepository) createWithoutAudit(ctx context.Context, a *admin.Admin) error {
	query := `
		INSERT INTO admins (id, username, email, password_hash, tenant_id, rbac_token, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err := r.db.ExecContext(ctx, query,
		a.ID,
		a.Username,
		nullString(a.Email),
		a.PasswordHash,
		a.TenantID,
		a.RBACToken,
		a.Enabled,
		a.CreatedAt,
		a.UpdatedAt,
	)

	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}

	return nil
}

func (r *PostgresAdminRepository) createWithAudit(ctx context.Context, a *admin.Admin, auditCtx *AuditContext) error {
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
		INSERT INTO admins (id, username, email, password_hash, tenant_id, rbac_token, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err = tx.ExecContext(ctx, query,
		a.ID,
		a.Username,
		nullString(a.Email),
		a.PasswordHash,
		a.TenantID,
		a.RBACToken,
		a.Enabled,
		a.CreatedAt,
		a.UpdatedAt,
	)

	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}

	if err := auditRepo.logCreateWithTx(ctx, tx, auditCtx, "admin", a.ID, a); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (r *PostgresAdminRepository) GetByID(ctx context.Context, id uuid.UUID) (*admin.Admin, error) {
	return r.getByID(ctx, r.db, id)
}

func (r *PostgresAdminRepository) getByID(ctx context.Context, exec Executor, id uuid.UUID) (*admin.Admin, error) {
	query := `
		SELECT id, username, email, password_hash, tenant_id, rbac_token, enabled, created_at, updated_at
		FROM admins
		WHERE id = $1
	`

	var a admin.Admin
	var email sql.NullString
	var tenantID uuid.NullUUID
	var rbacToken uuid.NullUUID

	err := exec.QueryRowContext(ctx, query, id).Scan(
		&a.ID,
		&a.Username,
		&email,
		&a.PasswordHash,
		&tenantID,
		&rbacToken,
		&a.Enabled,
		&a.CreatedAt,
		&a.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	a.Email = email.String
	if tenantID.Valid {
		a.TenantID = &tenantID.UUID
	}
	if rbacToken.Valid {
		a.RBACToken = &rbacToken.UUID
	}

	return &a, nil
}

func (r *PostgresAdminRepository) GetByIDWithRolesAndPermissions(ctx context.Context, id uuid.UUID, tenantID uuid.UUID) (*admin.Admin, error) {
	a, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	roleQuery := `
		SELECT r.name
		FROM roles r
		INNER JOIN admin_roles ar ON r.id = ar.role_id
		WHERE ar.admin_id = $1 AND ar.tenant_id = $2
	`

	rows, err := r.db.QueryContext(ctx, roleQuery, id, tenantID)
	if err != nil {
		return a, nil
	}
	defer rows.Close()

	roles := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		roles = append(roles, name)
	}
	a.Roles = roles

	permQuery := `
		SELECT DISTINCT p.resource || ':' || p.action as permission_key
		FROM permissions p
		INNER JOIN role_permissions rp ON p.id = rp.permission_id
		INNER JOIN admin_roles ar ON rp.role_id = ar.role_id
		WHERE ar.admin_id = $1 AND ar.tenant_id = $2
	`

	permRows, err := r.db.QueryContext(ctx, permQuery, id, tenantID)
	if err != nil {
		return a, nil
	}
	defer permRows.Close()

	permissions := make([]string, 0)
	for permRows.Next() {
		var key string
		if err := permRows.Scan(&key); err != nil {
			continue
		}
		permissions = append(permissions, key)
	}
	a.Permissions = permissions

	return a, nil
}

func (r *PostgresAdminRepository) GetByUsername(ctx context.Context, username string) (*admin.Admin, error) {
	query := `
		SELECT id, username, email, password_hash, tenant_id, rbac_token, enabled, created_at, updated_at
		FROM admins
		WHERE username = $1
	`

	var a admin.Admin
	var email sql.NullString
	var tenantID uuid.NullUUID
	var rbacToken uuid.NullUUID

	err := r.db.QueryRowContext(ctx, query, username).Scan(
		&a.ID,
		&a.Username,
		&email,
		&a.PasswordHash,
		&tenantID,
		&rbacToken,
		&a.Enabled,
		&a.CreatedAt,
		&a.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	a.Email = email.String
	if tenantID.Valid {
		a.TenantID = &tenantID.UUID
	}
	if rbacToken.Valid {
		a.RBACToken = &rbacToken.UUID
	}

	return &a, nil
}

func (r *PostgresAdminRepository) GetByUsernameWithRolesAndPermissions(ctx context.Context, username string, tenantID uuid.UUID) (*admin.Admin, error) {
	a, err := r.GetByUsername(ctx, username)
	if err != nil {
		return nil, err
	}

	roleQuery := `
		SELECT r.name
		FROM roles r
		INNER JOIN admin_roles ar ON r.id = ar.role_id
		WHERE ar.admin_id = $1 AND ar.tenant_id = $2
	`

	rows, err := r.db.QueryContext(ctx, roleQuery, a.ID, tenantID)
	if err != nil {
		return a, nil
	}
	defer rows.Close()

	roles := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		roles = append(roles, name)
	}
	a.Roles = roles

	permQuery := `
		SELECT DISTINCT p.resource || ':' || p.action as permission_key
		FROM permissions p
		INNER JOIN role_permissions rp ON p.id = rp.permission_id
		INNER JOIN admin_roles ar ON rp.role_id = ar.role_id
		WHERE ar.admin_id = $1 AND ar.tenant_id = $2
	`

	permRows, err := r.db.QueryContext(ctx, permQuery, a.ID, tenantID)
	if err != nil {
		return a, nil
	}
	defer permRows.Close()

	permissions := make([]string, 0)
	for permRows.Next() {
		var key string
		if err := permRows.Scan(&key); err != nil {
			continue
		}
		permissions = append(permissions, key)
	}
	a.Permissions = permissions

	return a, nil
}

func (r *PostgresAdminRepository) GetByEmail(ctx context.Context, email string) (*admin.Admin, error) {
	query := `
		SELECT id, username, email, password_hash, tenant_id, rbac_token, enabled, created_at, updated_at
		FROM admins
		WHERE email = $1
	`

	var a admin.Admin
	var em sql.NullString
	var tenantID uuid.NullUUID
	var rbacToken uuid.NullUUID

	err := r.db.QueryRowContext(ctx, query, email).Scan(
		&a.ID,
		&a.Username,
		&em,
		&a.PasswordHash,
		&tenantID,
		&rbacToken,
		&a.Enabled,
		&a.CreatedAt,
		&a.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	a.Email = em.String
	if tenantID.Valid {
		a.TenantID = &tenantID.UUID
	}
	if rbacToken.Valid {
		a.RBACToken = &rbacToken.UUID
	}

	return &a, nil
}

func (r *PostgresAdminRepository) List(ctx context.Context, pagination *Pagination) (*PageResult[*admin.Admin], error) {
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

	countQuery := `SELECT COUNT(*) FROM admins`
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery).Scan(&total); err != nil {
		return nil, err
	}

	query := `
		SELECT id, username, email, password_hash, tenant_id, rbac_token, enabled, created_at, updated_at
		FROM admins
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := r.db.QueryContext(ctx, query, pagination.PageSize, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*admin.Admin, 0)
	for rows.Next() {
		var a admin.Admin
		var email sql.NullString
		var tenantID uuid.NullUUID
		var rbacToken uuid.NullUUID

		if err := rows.Scan(
			&a.ID,
			&a.Username,
			&email,
			&a.PasswordHash,
			&tenantID,
			&rbacToken,
			&a.Enabled,
			&a.CreatedAt,
			&a.UpdatedAt,
		); err != nil {
			return nil, err
		}

		a.Email = email.String
		if tenantID.Valid {
			a.TenantID = &tenantID.UUID
		}
		if rbacToken.Valid {
			a.RBACToken = &rbacToken.UUID
		}
		items = append(items, &a)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return NewPageResult(items, total, pagination.Page, pagination.PageSize), nil
}

func (r *PostgresAdminRepository) ListByTenant(ctx context.Context, tenantID uuid.UUID, pagination *Pagination) (*PageResult[*admin.Admin], error) {
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

	countQuery := `
		SELECT COUNT(DISTINCT a.id) 
		FROM admins a
		INNER JOIN admin_roles ar ON a.id = ar.admin_id
		WHERE ar.tenant_id = $1
	`
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, tenantID).Scan(&total); err != nil {
		return nil, err
	}

	query := `
		SELECT DISTINCT a.id, a.username, a.email, a.password_hash, a.tenant_id, a.rbac_token, a.enabled, a.created_at, a.updated_at
		FROM admins a
		INNER JOIN admin_roles ar ON a.id = ar.admin_id
		WHERE ar.tenant_id = $1
		ORDER BY a.created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.QueryContext(ctx, query, tenantID, pagination.PageSize, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*admin.Admin, 0)
	for rows.Next() {
		var a admin.Admin
		var email sql.NullString
		var tid uuid.NullUUID
		var rbacToken uuid.NullUUID

		if err := rows.Scan(
			&a.ID,
			&a.Username,
			&email,
			&a.PasswordHash,
			&tid,
			&rbacToken,
			&a.Enabled,
			&a.CreatedAt,
			&a.UpdatedAt,
		); err != nil {
			return nil, err
		}

		a.Email = email.String
		if tid.Valid {
			a.TenantID = &tid.UUID
		}
		if rbacToken.Valid {
			a.RBACToken = &rbacToken.UUID
		}
		items = append(items, &a)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return NewPageResult(items, total, pagination.Page, pagination.PageSize), nil
}

func (r *PostgresAdminRepository) Update(ctx context.Context, a *admin.Admin, auditCtx *AuditContext) error {
	if err := a.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	if auditCtx == nil {
		return r.updateWithoutAudit(ctx, a)
	}

	return r.updateWithAudit(ctx, a, auditCtx)
}

func (r *PostgresAdminRepository) updateWithoutAudit(ctx context.Context, a *admin.Admin) error {
	query := `
		UPDATE admins
		SET username = $2, email = $3, password_hash = $4, tenant_id = $5, rbac_token = $6, enabled = $7, updated_at = NOW()
		WHERE id = $1
	`

	result, err := r.db.ExecContext(ctx, query,
		a.ID,
		a.Username,
		nullString(a.Email),
		a.PasswordHash,
		a.TenantID,
		a.RBACToken,
		a.Enabled,
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

func (r *PostgresAdminRepository) updateWithAudit(ctx context.Context, a *admin.Admin, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	oldAdmin, err := r.getByID(ctx, tx, a.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}

	query := `
		UPDATE admins
		SET username = $2, email = $3, password_hash = $4, tenant_id = $5, rbac_token = $6, enabled = $7, updated_at = NOW()
		WHERE id = $1
	`

	result, err := tx.ExecContext(ctx, query,
		a.ID,
		a.Username,
		nullString(a.Email),
		a.PasswordHash,
		a.TenantID,
		a.RBACToken,
		a.Enabled,
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

	if err := auditRepo.logUpdateWithTx(ctx, tx, auditCtx, "admin", a.ID, oldAdmin, a); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (r *PostgresAdminRepository) Delete(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error {
	if auditCtx == nil {
		return r.deleteWithoutAudit(ctx, id)
	}

	return r.deleteWithAudit(ctx, id, auditCtx)
}

func (r *PostgresAdminRepository) deleteWithoutAudit(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM admins WHERE id = $1`
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

func (r *PostgresAdminRepository) deleteWithAudit(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	oldAdmin, err := r.getByID(ctx, tx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}

	query := `DELETE FROM admins WHERE id = $1`
	result, err := tx.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}

	if err := auditRepo.logDeleteWithTx(ctx, tx, auditCtx, "admin", id, oldAdmin); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}
