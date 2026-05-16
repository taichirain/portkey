package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/domain/role"
	"github.com/taichirain/portkey/internal/platform/postgres"
)

type RoleRepository interface {
	Repository
	Create(ctx context.Context, r *role.Role, auditCtx *AuditContext) error
	GetByID(ctx context.Context, id uuid.UUID) (*role.Role, error)
	GetByTenantAndName(ctx context.Context, tenantID uuid.UUID, name string) (*role.Role, error)
	ListByTenant(ctx context.Context, tenantID uuid.UUID, pagination *Pagination) (*PageResult[*role.Role], error)
	ListByAdmin(ctx context.Context, adminID uuid.UUID, tenantID uuid.UUID) ([]*role.Role, error)
	Update(ctx context.Context, r *role.Role, auditCtx *AuditContext) error
	Delete(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error

	AssignPermission(ctx context.Context, roleID uuid.UUID, permissionID uuid.UUID) error
	RevokePermission(ctx context.Context, roleID uuid.UUID, permissionID uuid.UUID) error
	GetPermissions(ctx context.Context, roleID uuid.UUID) ([]string, error)

	AssignToAdmin(ctx context.Context, adminID uuid.UUID, roleID uuid.UUID, tenantID uuid.UUID) error
	RevokeFromAdmin(ctx context.Context, adminID uuid.UUID, roleID uuid.UUID, tenantID uuid.UUID) error
	GetAdminPermissions(ctx context.Context, adminID uuid.UUID, tenantID uuid.UUID) ([]string, error)
}

type PostgresRoleRepository struct {
	db        *postgres.DB
	auditRepo AuditRepository
}

func NewPostgresRoleRepository(db *postgres.DB, auditRepo AuditRepository) *PostgresRoleRepository {
	return &PostgresRoleRepository{
		db:        db,
		auditRepo: auditRepo,
	}
}

func (r *PostgresRoleRepository) BeginTx(ctx context.Context) (Tx, error) {
	return r.db.BeginTx(ctx)
}

func (r *PostgresRoleRepository) Create(ctx context.Context, ro *role.Role, auditCtx *AuditContext) error {
	if err := ro.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	if auditCtx == nil {
		return r.createWithoutAudit(ctx, ro)
	}

	return r.createWithAudit(ctx, ro, auditCtx)
}

func (r *PostgresRoleRepository) createWithoutAudit(ctx context.Context, ro *role.Role) error {
	query := `
		INSERT INTO roles (id, tenant_id, name, description, is_system, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err := r.db.ExecContext(ctx, query,
		ro.ID,
		ro.TenantID,
		ro.Name,
		nullString(ro.Description),
		ro.IsSystem,
		ro.CreatedAt,
		ro.UpdatedAt,
	)

	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		if isForeignKeyViolation(err) {
			return ErrTenantRequired
		}
		return err
	}

	return nil
}

func (r *PostgresRoleRepository) createWithAudit(ctx context.Context, ro *role.Role, auditCtx *AuditContext) error {
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
		INSERT INTO roles (id, tenant_id, name, description, is_system, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err = tx.ExecContext(ctx, query,
		ro.ID,
		ro.TenantID,
		ro.Name,
		nullString(ro.Description),
		ro.IsSystem,
		ro.CreatedAt,
		ro.UpdatedAt,
	)

	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		if isForeignKeyViolation(err) {
			return ErrTenantRequired
		}
		return err
	}

	if err := auditRepo.logCreateWithTx(ctx, tx, auditCtx, "role", ro.ID, ro); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (r *PostgresRoleRepository) GetByID(ctx context.Context, id uuid.UUID) (*role.Role, error) {
	return r.getByID(ctx, r.db, id)
}

func (r *PostgresRoleRepository) getByID(ctx context.Context, exec Executor, id uuid.UUID) (*role.Role, error) {
	query := `
		SELECT id, tenant_id, name, description, is_system, created_at, updated_at
		FROM roles
		WHERE id = $1
	`

	var ro role.Role
	var description sql.NullString

	err := exec.QueryRowContext(ctx, query, id).Scan(
		&ro.ID,
		&ro.TenantID,
		&ro.Name,
		&description,
		&ro.IsSystem,
		&ro.CreatedAt,
		&ro.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	ro.Description = description.String

	permissions, err := r.getPermissionsByRoleID(ctx, exec, id)
	if err == nil {
		ro.Permissions = permissions
	}

	return &ro, nil
}

func (r *PostgresRoleRepository) GetByTenantAndName(ctx context.Context, tenantID uuid.UUID, name string) (*role.Role, error) {
	query := `
		SELECT id, tenant_id, name, description, is_system, created_at, updated_at
		FROM roles
		WHERE tenant_id = $1 AND name = $2
	`

	var ro role.Role
	var description sql.NullString

	err := r.db.QueryRowContext(ctx, query, tenantID, name).Scan(
		&ro.ID,
		&ro.TenantID,
		&ro.Name,
		&description,
		&ro.IsSystem,
		&ro.CreatedAt,
		&ro.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	ro.Description = description.String

	permissions, err := r.GetPermissions(ctx, ro.ID)
	if err == nil {
		ro.Permissions = permissions
	}

	return &ro, nil
}

func (r *PostgresRoleRepository) ListByTenant(ctx context.Context, tenantID uuid.UUID, pagination *Pagination) (*PageResult[*role.Role], error) {
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

	countQuery := `SELECT COUNT(*) FROM roles WHERE tenant_id = $1`
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, tenantID).Scan(&total); err != nil {
		return nil, err
	}

	query := `
		SELECT id, tenant_id, name, description, is_system, created_at, updated_at
		FROM roles
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.QueryContext(ctx, query, tenantID, pagination.PageSize, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*role.Role, 0)
	for rows.Next() {
		var ro role.Role
		var description sql.NullString

		if err := rows.Scan(
			&ro.ID,
			&ro.TenantID,
			&ro.Name,
			&description,
			&ro.IsSystem,
			&ro.CreatedAt,
			&ro.UpdatedAt,
		); err != nil {
			return nil, err
		}

		ro.Description = description.String
		items = append(items, &ro)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return NewPageResult(items, total, pagination.Page, pagination.PageSize), nil
}

func (r *PostgresRoleRepository) ListByAdmin(ctx context.Context, adminID uuid.UUID, tenantID uuid.UUID) ([]*role.Role, error) {
	query := `
		SELECT r.id, r.tenant_id, r.name, r.description, r.is_system, r.created_at, r.updated_at
		FROM roles r
		INNER JOIN admin_roles ar ON r.id = ar.role_id
		WHERE ar.admin_id = $1 AND ar.tenant_id = $2
		ORDER BY r.created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query, adminID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*role.Role, 0)
	for rows.Next() {
		var ro role.Role
		var description sql.NullString

		if err := rows.Scan(
			&ro.ID,
			&ro.TenantID,
			&ro.Name,
			&description,
			&ro.IsSystem,
			&ro.CreatedAt,
			&ro.UpdatedAt,
		); err != nil {
			return nil, err
		}

		ro.Description = description.String
		items = append(items, &ro)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (r *PostgresRoleRepository) Update(ctx context.Context, ro *role.Role, auditCtx *AuditContext) error {
	if err := ro.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	if auditCtx == nil {
		return r.updateWithoutAudit(ctx, ro)
	}

	return r.updateWithAudit(ctx, ro, auditCtx)
}

func (r *PostgresRoleRepository) updateWithoutAudit(ctx context.Context, ro *role.Role) error {
	query := `
		UPDATE roles
		SET name = $2, description = $3, updated_at = NOW()
		WHERE id = $1
	`

	result, err := r.db.ExecContext(ctx, query,
		ro.ID,
		ro.Name,
		nullString(ro.Description),
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

func (r *PostgresRoleRepository) updateWithAudit(ctx context.Context, ro *role.Role, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	oldRole, err := r.getByID(ctx, tx, ro.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}

	query := `
		UPDATE roles
		SET name = $2, description = $3, updated_at = NOW()
		WHERE id = $1
	`

	result, err := tx.ExecContext(ctx, query,
		ro.ID,
		ro.Name,
		nullString(ro.Description),
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

	if err := auditRepo.logUpdateWithTx(ctx, tx, auditCtx, "role", ro.ID, oldRole, ro); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (r *PostgresRoleRepository) Delete(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error {
	if auditCtx == nil {
		return r.deleteWithoutAudit(ctx, id)
	}

	return r.deleteWithAudit(ctx, id, auditCtx)
}

func (r *PostgresRoleRepository) deleteWithoutAudit(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM roles WHERE id = $1`
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

func (r *PostgresRoleRepository) deleteWithAudit(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	oldRole, err := r.getByID(ctx, tx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}

	query := `DELETE FROM roles WHERE id = $1`
	result, err := tx.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}

	if err := auditRepo.logDeleteWithTx(ctx, tx, auditCtx, "role", id, oldRole); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (r *PostgresRoleRepository) AssignPermission(ctx context.Context, roleID uuid.UUID, permissionID uuid.UUID) error {
	query := `
		INSERT INTO role_permissions (role_id, permission_id, created_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT DO NOTHING
	`

	_, err := r.db.ExecContext(ctx, query, roleID, permissionID)
	if err != nil {
		return err
	}

	return nil
}

func (r *PostgresRoleRepository) RevokePermission(ctx context.Context, roleID uuid.UUID, permissionID uuid.UUID) error {
	query := `DELETE FROM role_permissions WHERE role_id = $1 AND permission_id = $2`
	result, err := r.db.ExecContext(ctx, query, roleID, permissionID)
	if err != nil {
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

func (r *PostgresRoleRepository) GetPermissions(ctx context.Context, roleID uuid.UUID) ([]string, error) {
	return r.getPermissionsByRoleID(ctx, r.db, roleID)
}

func (r *PostgresRoleRepository) getPermissionsByRoleID(ctx context.Context, exec Executor, roleID uuid.UUID) ([]string, error) {
	query := `
		SELECT p.resource || ':' || p.action as permission_key
		FROM permissions p
		INNER JOIN role_permissions rp ON p.id = rp.permission_id
		WHERE rp.role_id = $1
	`

	rows, err := exec.QueryContext(ctx, query, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	permissions := make([]string, 0)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		permissions = append(permissions, key)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return permissions, nil
}

func (r *PostgresRoleRepository) AssignToAdmin(ctx context.Context, adminID uuid.UUID, roleID uuid.UUID, tenantID uuid.UUID) error {
	query := `
		INSERT INTO admin_roles (admin_id, role_id, tenant_id, created_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT DO NOTHING
	`

	_, err := r.db.ExecContext(ctx, query, adminID, roleID, tenantID)
	if err != nil {
		return err
	}

	return nil
}

func (r *PostgresRoleRepository) RevokeFromAdmin(ctx context.Context, adminID uuid.UUID, roleID uuid.UUID, tenantID uuid.UUID) error {
	query := `DELETE FROM admin_roles WHERE admin_id = $1 AND role_id = $2 AND tenant_id = $3`
	result, err := r.db.ExecContext(ctx, query, adminID, roleID, tenantID)
	if err != nil {
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

func (r *PostgresRoleRepository) GetAdminPermissions(ctx context.Context, adminID uuid.UUID, tenantID uuid.UUID) ([]string, error) {
	query := `
		SELECT DISTINCT p.resource || ':' || p.action as permission_key
		FROM permissions p
		INNER JOIN role_permissions rp ON p.id = rp.permission_id
		INNER JOIN admin_roles ar ON rp.role_id = ar.role_id
		WHERE ar.admin_id = $1 AND ar.tenant_id = $2
	`

	rows, err := r.db.QueryContext(ctx, query, adminID, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	permissions := make([]string, 0)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		permissions = append(permissions, key)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return permissions, nil
}

func (r *PostgresRoleRepository) CreateSystemRoles(ctx context.Context, tenantID uuid.UUID) error {
	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	systemRoles := []struct {
		name        string
		description string
	}{
		{string(role.SystemRoleTenantAdmin), "租户管理员 - 拥有租户内所有权限"},
		{string(role.SystemRoleDeveloper), "开发者 - 可以查看和修改配置，但不能管理管理员"},
		{string(role.SystemRoleViewer), "观察者 - 只能查看配置"},
	}

	for _, sr := range systemRoles {
		ro, err := role.New(tenantID, sr.name)
		if err != nil {
			return err
		}
		ro.Description = sr.description
		ro.IsSystem = true

		query := `
			INSERT INTO roles (id, tenant_id, name, description, is_system, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT DO NOTHING
		`

		_, err = tx.ExecContext(ctx, query,
			ro.ID,
			ro.TenantID,
			ro.Name,
			ro.Description,
			ro.IsSystem,
			ro.CreatedAt,
			ro.UpdatedAt,
		)

		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (r *PostgresRoleRepository) GetRoleWithPermissions(ctx context.Context, roleID uuid.UUID) (*role.Role, error) {
	ro, err := r.GetByID(ctx, roleID)
	if err != nil {
		return nil, err
	}

	permissions, err := r.GetPermissions(ctx, roleID)
	if err != nil {
		return nil, err
	}

	ro.Permissions = permissions
	return ro, nil
}

func (r *PostgresRoleRepository) SetRolePermissions(ctx context.Context, roleID uuid.UUID, permissionIDs []uuid.UUID) error {
	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	deleteQuery := `DELETE FROM role_permissions WHERE role_id = $1`
	if _, err := tx.ExecContext(ctx, deleteQuery, roleID); err != nil {
		return err
	}

	if len(permissionIDs) > 0 {
		valueStrings := make([]string, 0, len(permissionIDs))
		valueArgs := make([]interface{}, 0, len(permissionIDs)*3)
		for i, pid := range permissionIDs {
			valueStrings = append(valueStrings, fmt.Sprintf("($%d, $%d, NOW())", i*3+1, i*3+2))
			valueArgs = append(valueArgs, roleID, pid)
		}

		insertQuery := fmt.Sprintf(`
			INSERT INTO role_permissions (role_id, permission_id, created_at)
			VALUES %s
			ON CONFLICT DO NOTHING
		`, stringsJoin(valueStrings, ", "))

		if _, err := tx.ExecContext(ctx, insertQuery, valueArgs...); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func stringsJoin(s []string, sep string) string {
	if len(s) == 0 {
		return ""
	}
	result := s[0]
	for i := 1; i < len(s); i++ {
		result += sep + s[i]
	}
	return result
}
