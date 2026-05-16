package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/taichirain/portkey/internal/domain/plugin"
	"github.com/taichirain/portkey/internal/platform/postgres"
)

type PluginRepository interface {
	Repository
	Create(ctx context.Context, p *plugin.Plugin, auditCtx *AuditContext) error
	GetByID(ctx context.Context, id uuid.UUID) (*plugin.Plugin, error)
	ListByName(ctx context.Context, name string) ([]*plugin.Plugin, error)
	ListByServiceID(ctx context.Context, serviceID uuid.UUID) ([]*plugin.Plugin, error)
	ListByRouteID(ctx context.Context, routeID uuid.UUID) ([]*plugin.Plugin, error)
	ListByConsumerID(ctx context.Context, consumerID uuid.UUID) ([]*plugin.Plugin, error)
	ListGlobal(ctx context.Context) ([]*plugin.Plugin, error)
	List(ctx context.Context, pagination *Pagination) (*PageResult[*plugin.Plugin], error)
	Update(ctx context.Context, p *plugin.Plugin, auditCtx *AuditContext) error
	Delete(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error
}

type PostgresPluginRepository struct {
	db        *postgres.DB
	auditRepo AuditRepository
}

func NewPostgresPluginRepository(db *postgres.DB, auditRepo AuditRepository) *PostgresPluginRepository {
	return &PostgresPluginRepository{
		db:        db,
		auditRepo: auditRepo,
	}
}

func (r *PostgresPluginRepository) BeginTx(ctx context.Context) (Tx, error) {
	return r.db.BeginTx(ctx)
}

func (r *PostgresPluginRepository) Create(ctx context.Context, p *plugin.Plugin, auditCtx *AuditContext) error {
	if err := p.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	if p.TenantID == uuid.Nil {
		if tenantID, ok := GetTenantIDFromContext(ctx); ok {
			p.TenantID = tenantID
		}
	}

	if auditCtx == nil {
		return r.createWithoutAudit(ctx, p)
	}

	return r.createWithAudit(ctx, p, auditCtx)
}

func (r *PostgresPluginRepository) createWithoutAudit(ctx context.Context, p *plugin.Plugin) error {
	configJSON, err := json.Marshal(p.Config)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO plugins (id, tenant_id, name, route_id, service_id, consumer_id, config, protocols, enabled, run_on, tags, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	_, err = r.db.ExecContext(ctx, query,
		p.ID,
		p.TenantID,
		p.Name,
		p.RouteID,
		p.ServiceID,
		p.ConsumerID,
		configJSON,
		pq.Array(p.Protocols),
		p.Enabled,
		nullString(string(p.RunOn)),
		pq.Array(p.Tags),
		p.CreatedAt,
		p.UpdatedAt,
	)

	if err != nil {
		if isForeignKeyViolation(err) {
			return fmt.Errorf("%w: invalid foreign key reference", ErrInvalidInput)
		}
		return err
	}

	return nil
}

func (r *PostgresPluginRepository) createWithAudit(ctx context.Context, p *plugin.Plugin, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	configJSON, err := json.Marshal(p.Config)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO plugins (id, tenant_id, name, route_id, service_id, consumer_id, config, protocols, enabled, run_on, tags, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	_, err = tx.ExecContext(ctx, query,
		p.ID,
		p.TenantID,
		p.Name,
		p.RouteID,
		p.ServiceID,
		p.ConsumerID,
		configJSON,
		pq.Array(p.Protocols),
		p.Enabled,
		nullString(string(p.RunOn)),
		pq.Array(p.Tags),
		p.CreatedAt,
		p.UpdatedAt,
	)

	if err != nil {
		if isForeignKeyViolation(err) {
			return fmt.Errorf("%w: invalid foreign key reference", ErrInvalidInput)
		}
		return err
	}

	if err := auditRepo.logCreateWithTx(ctx, tx, auditCtx, "plugin", p.ID, p); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (r *PostgresPluginRepository) GetByID(ctx context.Context, id uuid.UUID) (*plugin.Plugin, error) {
	return r.getByID(ctx, r.db, id)
}

func (r *PostgresPluginRepository) getByID(ctx context.Context, exec Executor, id uuid.UUID) (*plugin.Plugin, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, name, route_id, service_id, consumer_id, config, protocols, enabled, run_on, tags, created_at, updated_at
			FROM plugins
			WHERE id = $1 AND tenant_id = $2
		`
		args = []interface{}{id, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, name, route_id, service_id, consumer_id, config, protocols, enabled, run_on, tags, created_at, updated_at
			FROM plugins
			WHERE id = $1
		`
		args = []interface{}{id}
	}

	var p plugin.Plugin
	var configJSON []byte
	var runOn sql.NullString

	err := exec.QueryRowContext(ctx, query, args...).Scan(
		&p.ID,
		&p.TenantID,
		&p.Name,
		&p.RouteID,
		&p.ServiceID,
		&p.ConsumerID,
		&configJSON,
		pq.Array(&p.Protocols),
		&p.Enabled,
		&runOn,
		pq.Array(&p.Tags),
		&p.CreatedAt,
		&p.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if runOn.Valid {
		p.RunOn = plugin.RunOn(runOn.String)
	}

	if len(configJSON) > 0 {
		var config map[string]interface{}
		if err := json.Unmarshal(configJSON, &config); err == nil {
			p.Config = config
		}
	}

	return &p, nil
}

func (r *PostgresPluginRepository) ListByName(ctx context.Context, name string) ([]*plugin.Plugin, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, name, route_id, service_id, consumer_id, config, protocols, enabled, run_on, tags, created_at, updated_at
			FROM plugins
			WHERE name = $1 AND tenant_id = $2
			ORDER BY created_at DESC
		`
		args = []interface{}{name, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, name, route_id, service_id, consumer_id, config, protocols, enabled, run_on, tags, created_at, updated_at
			FROM plugins
			WHERE name = $1
			ORDER BY created_at DESC
		`
		args = []interface{}{name}
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanPluginRows(rows)
}

func (r *PostgresPluginRepository) ListByServiceID(ctx context.Context, serviceID uuid.UUID) ([]*plugin.Plugin, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, name, route_id, service_id, consumer_id, config, protocols, enabled, run_on, tags, created_at, updated_at
			FROM plugins
			WHERE service_id = $1 AND tenant_id = $2
			ORDER BY created_at DESC
		`
		args = []interface{}{serviceID, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, name, route_id, service_id, consumer_id, config, protocols, enabled, run_on, tags, created_at, updated_at
			FROM plugins
			WHERE service_id = $1
			ORDER BY created_at DESC
		`
		args = []interface{}{serviceID}
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanPluginRows(rows)
}

func (r *PostgresPluginRepository) ListByRouteID(ctx context.Context, routeID uuid.UUID) ([]*plugin.Plugin, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, name, route_id, service_id, consumer_id, config, protocols, enabled, run_on, tags, created_at, updated_at
			FROM plugins
			WHERE route_id = $1 AND tenant_id = $2
			ORDER BY created_at DESC
		`
		args = []interface{}{routeID, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, name, route_id, service_id, consumer_id, config, protocols, enabled, run_on, tags, created_at, updated_at
			FROM plugins
			WHERE route_id = $1
			ORDER BY created_at DESC
		`
		args = []interface{}{routeID}
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanPluginRows(rows)
}

func (r *PostgresPluginRepository) ListByConsumerID(ctx context.Context, consumerID uuid.UUID) ([]*plugin.Plugin, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, name, route_id, service_id, consumer_id, config, protocols, enabled, run_on, tags, created_at, updated_at
			FROM plugins
			WHERE consumer_id = $1 AND tenant_id = $2
			ORDER BY created_at DESC
		`
		args = []interface{}{consumerID, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, name, route_id, service_id, consumer_id, config, protocols, enabled, run_on, tags, created_at, updated_at
			FROM plugins
			WHERE consumer_id = $1
			ORDER BY created_at DESC
		`
		args = []interface{}{consumerID}
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanPluginRows(rows)
}

func (r *PostgresPluginRepository) ListGlobal(ctx context.Context) ([]*plugin.Plugin, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, name, route_id, service_id, consumer_id, config, protocols, enabled, run_on, tags, created_at, updated_at
			FROM plugins
			WHERE route_id IS NULL AND service_id IS NULL AND consumer_id IS NULL AND tenant_id = $1
			ORDER BY created_at DESC
		`
		args = []interface{}{tenantID}
	} else {
		query = `
			SELECT id, tenant_id, name, route_id, service_id, consumer_id, config, protocols, enabled, run_on, tags, created_at, updated_at
			FROM plugins
			WHERE route_id IS NULL AND service_id IS NULL AND consumer_id IS NULL
			ORDER BY created_at DESC
		`
		args = []interface{}{}
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return r.scanPluginRows(rows)
}

func (r *PostgresPluginRepository) List(ctx context.Context, pagination *Pagination) (*PageResult[*plugin.Plugin], error) {
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
		countQuery = `SELECT COUNT(*) FROM plugins WHERE tenant_id = $1`
		countArgs = []interface{}{tenantID}

		listQuery = `
			SELECT id, tenant_id, name, route_id, service_id, consumer_id, config, protocols, enabled, run_on, tags, created_at, updated_at
			FROM plugins
			WHERE tenant_id = $1
			ORDER BY created_at DESC
			LIMIT $2 OFFSET $3
		`
		listArgs = []interface{}{tenantID, pagination.PageSize, offset}
	} else {
		countQuery = `SELECT COUNT(*) FROM plugins`
		countArgs = []interface{}{}

		listQuery = `
			SELECT id, tenant_id, name, route_id, service_id, consumer_id, config, protocols, enabled, run_on, tags, created_at, updated_at
			FROM plugins
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

	items, err := r.scanPluginRows(rows)
	if err != nil {
		return nil, err
	}

	return NewPageResult(items, total, pagination.Page, pagination.PageSize), nil
}

func (r *PostgresPluginRepository) scanPluginRows(rows *sql.Rows) ([]*plugin.Plugin, error) {
	items := make([]*plugin.Plugin, 0)
	for rows.Next() {
		var p plugin.Plugin
		var configJSON []byte
		var runOn sql.NullString

		if err := rows.Scan(
			&p.ID,
			&p.TenantID,
			&p.Name,
			&p.RouteID,
			&p.ServiceID,
			&p.ConsumerID,
			&configJSON,
			pq.Array(&p.Protocols),
			&p.Enabled,
			&runOn,
			pq.Array(&p.Tags),
			&p.CreatedAt,
			&p.UpdatedAt,
		); err != nil {
			return nil, err
		}

		if runOn.Valid {
			p.RunOn = plugin.RunOn(runOn.String)
		}

		if len(configJSON) > 0 {
			var config map[string]interface{}
			if err := json.Unmarshal(configJSON, &config); err == nil {
				p.Config = config
			}
		}

		items = append(items, &p)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (r *PostgresPluginRepository) Update(ctx context.Context, p *plugin.Plugin, auditCtx *AuditContext) error {
	if err := p.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	if auditCtx == nil {
		return r.updateWithoutAudit(ctx, p)
	}

	return r.updateWithAudit(ctx, p, auditCtx)
}

func (r *PostgresPluginRepository) updateWithoutAudit(ctx context.Context, p *plugin.Plugin) error {
	configJSON, err := json.Marshal(p.Config)
	if err != nil {
		return err
	}

	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			UPDATE plugins
			SET name = $2, route_id = $3, service_id = $4, consumer_id = $5, config = $6, protocols = $7, enabled = $8, run_on = $9, tags = $10, updated_at = NOW()
			WHERE id = $1 AND tenant_id = $11
		`
		args = []interface{}{
			p.ID,
			p.Name,
			p.RouteID,
			p.ServiceID,
			p.ConsumerID,
			configJSON,
			pq.Array(p.Protocols),
			p.Enabled,
			nullString(string(p.RunOn)),
			pq.Array(p.Tags),
			tenantID,
		}
	} else {
		query = `
			UPDATE plugins
			SET name = $2, route_id = $3, service_id = $4, consumer_id = $5, config = $6, protocols = $7, enabled = $8, run_on = $9, tags = $10, updated_at = NOW()
			WHERE id = $1
		`
		args = []interface{}{
			p.ID,
			p.Name,
			p.RouteID,
			p.ServiceID,
			p.ConsumerID,
			configJSON,
			pq.Array(p.Protocols),
			p.Enabled,
			nullString(string(p.RunOn)),
			pq.Array(p.Tags),
		}
	}

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		if isForeignKeyViolation(err) {
			return fmt.Errorf("%w: invalid foreign key reference", ErrInvalidInput)
		}
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

func (r *PostgresPluginRepository) updateWithAudit(ctx context.Context, p *plugin.Plugin, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	oldPlugin, err := r.getByID(ctx, tx, p.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}

	configJSON, err := json.Marshal(p.Config)
	if err != nil {
		return err
	}

	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			UPDATE plugins
			SET name = $2, route_id = $3, service_id = $4, consumer_id = $5, config = $6, protocols = $7, enabled = $8, run_on = $9, tags = $10, updated_at = NOW()
			WHERE id = $1 AND tenant_id = $11
		`
		args = []interface{}{
			p.ID,
			p.Name,
			p.RouteID,
			p.ServiceID,
			p.ConsumerID,
			configJSON,
			pq.Array(p.Protocols),
			p.Enabled,
			nullString(string(p.RunOn)),
			pq.Array(p.Tags),
			tenantID,
		}
	} else {
		query = `
			UPDATE plugins
			SET name = $2, route_id = $3, service_id = $4, consumer_id = $5, config = $6, protocols = $7, enabled = $8, run_on = $9, tags = $10, updated_at = NOW()
			WHERE id = $1
		`
		args = []interface{}{
			p.ID,
			p.Name,
			p.RouteID,
			p.ServiceID,
			p.ConsumerID,
			configJSON,
			pq.Array(p.Protocols),
			p.Enabled,
			nullString(string(p.RunOn)),
			pq.Array(p.Tags),
		}
	}

	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		if isForeignKeyViolation(err) {
			return fmt.Errorf("%w: invalid foreign key reference", ErrInvalidInput)
		}
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}

	if err := auditRepo.logUpdateWithTx(ctx, tx, auditCtx, "plugin", p.ID, oldPlugin, p); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (r *PostgresPluginRepository) Delete(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error {
	if auditCtx == nil {
		return r.deleteWithoutAudit(ctx, id)
	}

	return r.deleteWithAudit(ctx, id, auditCtx)
}

func (r *PostgresPluginRepository) deleteWithoutAudit(ctx context.Context, id uuid.UUID) error {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `DELETE FROM plugins WHERE id = $1 AND tenant_id = $2`
		args = []interface{}{id, tenantID}
	} else {
		query = `DELETE FROM plugins WHERE id = $1`
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

func (r *PostgresPluginRepository) deleteWithAudit(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	oldPlugin, err := r.getByID(ctx, tx, id)
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
		query = `DELETE FROM plugins WHERE id = $1 AND tenant_id = $2`
		args = []interface{}{id, tenantID}
	} else {
		query = `DELETE FROM plugins WHERE id = $1`
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

	if err := auditRepo.logDeleteWithTx(ctx, tx, auditCtx, "plugin", id, oldPlugin); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}
