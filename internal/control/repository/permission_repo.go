package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/domain/permission"
	"github.com/taichirain/portkey/internal/platform/postgres"
)

type PermissionRepository interface {
	Repository
	Create(ctx context.Context, p *permission.Permission) error
	GetByID(ctx context.Context, id uuid.UUID) (*permission.Permission, error)
	GetByResourceAndAction(ctx context.Context, resource, action string) (*permission.Permission, error)
	List(ctx context.Context, pagination *Pagination) (*PageResult[*permission.Permission], error)
	ListByResource(ctx context.Context, resource string) ([]*permission.Permission, error)
	GetAll(ctx context.Context) ([]*permission.Permission, error)
}

type PostgresPermissionRepository struct {
	db *postgres.DB
}

func NewPostgresPermissionRepository(db *postgres.DB) *PostgresPermissionRepository {
	return &PostgresPermissionRepository{db: db}
}

func (r *PostgresPermissionRepository) BeginTx(ctx context.Context) (Tx, error) {
	return r.db.BeginTx(ctx)
}

func (r *PostgresPermissionRepository) Create(ctx context.Context, p *permission.Permission) error {
	if err := p.Validate(); err != nil {
		return err
	}

	query := `
		INSERT INTO permissions (id, resource, action, description, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (resource, action) DO NOTHING
	`

	_, err := r.db.ExecContext(ctx, query,
		p.ID,
		p.Resource,
		p.Action,
		nullString(p.Description),
		p.CreatedAt,
	)

	if err != nil {
		return err
	}

	return nil
}

func (r *PostgresPermissionRepository) GetByID(ctx context.Context, id uuid.UUID) (*permission.Permission, error) {
	query := `
		SELECT id, resource, action, description, created_at
		FROM permissions
		WHERE id = $1
	`

	var p permission.Permission
	var description sql.NullString

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&p.ID,
		&p.Resource,
		&p.Action,
		&description,
		&p.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	p.Description = description.String
	return &p, nil
}

func (r *PostgresPermissionRepository) GetByResourceAndAction(ctx context.Context, resource, action string) (*permission.Permission, error) {
	query := `
		SELECT id, resource, action, description, created_at
		FROM permissions
		WHERE resource = $1 AND action = $2
	`

	var p permission.Permission
	var description sql.NullString

	err := r.db.QueryRowContext(ctx, query, resource, action).Scan(
		&p.ID,
		&p.Resource,
		&p.Action,
		&description,
		&p.CreatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	p.Description = description.String
	return &p, nil
}

func (r *PostgresPermissionRepository) List(ctx context.Context, pagination *Pagination) (*PageResult[*permission.Permission], error) {
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

	countQuery := `SELECT COUNT(*) FROM permissions`
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery).Scan(&total); err != nil {
		return nil, err
	}

	query := `
		SELECT id, resource, action, description, created_at
		FROM permissions
		ORDER BY resource, action
		LIMIT $1 OFFSET $2
	`

	rows, err := r.db.QueryContext(ctx, query, pagination.PageSize, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*permission.Permission, 0)
	for rows.Next() {
		var p permission.Permission
		var description sql.NullString

		if err := rows.Scan(
			&p.ID,
			&p.Resource,
			&p.Action,
			&description,
			&p.CreatedAt,
		); err != nil {
			return nil, err
		}

		p.Description = description.String
		items = append(items, &p)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return NewPageResult(items, total, pagination.Page, pagination.PageSize), nil
}

func (r *PostgresPermissionRepository) ListByResource(ctx context.Context, resource string) ([]*permission.Permission, error) {
	query := `
		SELECT id, resource, action, description, created_at
		FROM permissions
		WHERE resource = $1
		ORDER BY action
	`

	rows, err := r.db.QueryContext(ctx, query, resource)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*permission.Permission, 0)
	for rows.Next() {
		var p permission.Permission
		var description sql.NullString

		if err := rows.Scan(
			&p.ID,
			&p.Resource,
			&p.Action,
			&description,
			&p.CreatedAt,
		); err != nil {
			return nil, err
		}

		p.Description = description.String
		items = append(items, &p)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (r *PostgresPermissionRepository) GetAll(ctx context.Context) ([]*permission.Permission, error) {
	query := `
		SELECT id, resource, action, description, created_at
		FROM permissions
		ORDER BY resource, action
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*permission.Permission, 0)
	for rows.Next() {
		var p permission.Permission
		var description sql.NullString

		if err := rows.Scan(
			&p.ID,
			&p.Resource,
			&p.Action,
			&description,
			&p.CreatedAt,
		); err != nil {
			return nil, err
		}

		p.Description = description.String
		items = append(items, &p)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (r *PostgresPermissionRepository) InitDefaultPermissions(ctx context.Context) error {
	defaultPermissions := []struct {
		resource    string
		action      string
		description string
	}{
		{string(permission.ResourceService), string(permission.ActionCreate), "创建服务"},
		{string(permission.ResourceService), string(permission.ActionRead), "查看服务"},
		{string(permission.ResourceService), string(permission.ActionUpdate), "更新服务"},
		{string(permission.ResourceService), string(permission.ActionDelete), "删除服务"},

		{string(permission.ResourceRoute), string(permission.ActionCreate), "创建路由"},
		{string(permission.ResourceRoute), string(permission.ActionRead), "查看路由"},
		{string(permission.ResourceRoute), string(permission.ActionUpdate), "更新路由"},
		{string(permission.ResourceRoute), string(permission.ActionDelete), "删除路由"},

		{string(permission.ResourceUpstream), string(permission.ActionCreate), "创建上游"},
		{string(permission.ResourceUpstream), string(permission.ActionRead), "查看上游"},
		{string(permission.ResourceUpstream), string(permission.ActionUpdate), "更新上游"},
		{string(permission.ResourceUpstream), string(permission.ActionDelete), "删除上游"},

		{string(permission.ResourceTarget), string(permission.ActionCreate), "创建目标"},
		{string(permission.ResourceTarget), string(permission.ActionRead), "查看目标"},
		{string(permission.ResourceTarget), string(permission.ActionUpdate), "更新目标"},
		{string(permission.ResourceTarget), string(permission.ActionDelete), "删除目标"},

		{string(permission.ResourceConsumer), string(permission.ActionCreate), "创建消费者"},
		{string(permission.ResourceConsumer), string(permission.ActionRead), "查看消费者"},
		{string(permission.ResourceConsumer), string(permission.ActionUpdate), "更新消费者"},
		{string(permission.ResourceConsumer), string(permission.ActionDelete), "删除消费者"},

		{string(permission.ResourceCredential), string(permission.ActionCreate), "创建凭证"},
		{string(permission.ResourceCredential), string(permission.ActionRead), "查看凭证"},
		{string(permission.ResourceCredential), string(permission.ActionUpdate), "更新凭证"},
		{string(permission.ResourceCredential), string(permission.ActionDelete), "删除凭证"},

		{string(permission.ResourcePlugin), string(permission.ActionCreate), "创建插件"},
		{string(permission.ResourcePlugin), string(permission.ActionRead), "查看插件"},
		{string(permission.ResourcePlugin), string(permission.ActionUpdate), "更新插件"},
		{string(permission.ResourcePlugin), string(permission.ActionDelete), "删除插件"},

		{string(permission.ResourceRevision), string(permission.ActionCreate), "创建版本"},
		{string(permission.ResourceRevision), string(permission.ActionRead), "查看版本"},
		{string(permission.ResourceRevision), string(permission.ActionPublish), "发布版本"},
		{string(permission.ResourceRevision), string(permission.ActionRollback), "回滚版本"},

		{string(permission.ResourceAudit), string(permission.ActionRead), "查看审计日志"},

		{string(permission.ResourceAdmin), string(permission.ActionCreate), "创建管理员"},
		{string(permission.ResourceAdmin), string(permission.ActionRead), "查看管理员"},
		{string(permission.ResourceAdmin), string(permission.ActionUpdate), "更新管理员"},
		{string(permission.ResourceAdmin), string(permission.ActionDelete), "删除管理员"},

		{string(permission.ResourceRole), string(permission.ActionCreate), "创建角色"},
		{string(permission.ResourceRole), string(permission.ActionRead), "查看角色"},
		{string(permission.ResourceRole), string(permission.ActionUpdate), "更新角色"},
		{string(permission.ResourceRole), string(permission.ActionDelete), "删除角色"},
		{string(permission.ResourceRole), string(permission.ActionAssign), "分配角色"},

		{string(permission.ResourceTenant), string(permission.ActionCreate), "创建租户"},
		{string(permission.ResourceTenant), string(permission.ActionRead), "查看租户"},
		{string(permission.ResourceTenant), string(permission.ActionUpdate), "更新租户"},
		{string(permission.ResourceTenant), string(permission.ActionDelete), "删除租户"},

		{string(permission.ResourceTrafficPolicy), string(permission.ActionCreate), "创建流量策略"},
		{string(permission.ResourceTrafficPolicy), string(permission.ActionRead), "查看流量策略"},
		{string(permission.ResourceTrafficPolicy), string(permission.ActionUpdate), "更新流量策略"},
		{string(permission.ResourceTrafficPolicy), string(permission.ActionDelete), "删除流量策略"},
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, dp := range defaultPermissions {
		p, err := permission.New(dp.resource, dp.action)
		if err != nil {
			return err
		}
		p.Description = dp.description

		query := `
			INSERT INTO permissions (id, resource, action, description, created_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (resource, action) DO NOTHING
		`

		_, err = tx.ExecContext(ctx, query,
			p.ID,
			p.Resource,
			p.Action,
			nullString(p.Description),
			p.CreatedAt,
		)

		if err != nil {
			return err
		}
	}

	return tx.Commit()
}
