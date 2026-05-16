package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/taichirain/portkey/internal/domain/route"
	"github.com/taichirain/portkey/internal/platform/postgres"
)

type RouteRepository interface {
	Repository
	Create(ctx context.Context, r *route.Route, auditCtx *AuditContext) error
	GetByID(ctx context.Context, id uuid.UUID) (*route.Route, error)
	ListByServiceID(ctx context.Context, serviceID uuid.UUID) ([]*route.Route, error)
	List(ctx context.Context, pagination *Pagination) (*PageResult[*route.Route], error)
	Update(ctx context.Context, r *route.Route, auditCtx *AuditContext) error
	Delete(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error
}

type PostgresRouteRepository struct {
	db        *postgres.DB
	auditRepo AuditRepository
}

func NewPostgresRouteRepository(db *postgres.DB, auditRepo AuditRepository) *PostgresRouteRepository {
	return &PostgresRouteRepository{
		db:        db,
		auditRepo: auditRepo,
	}
}

func (r *PostgresRouteRepository) BeginTx(ctx context.Context) (Tx, error) {
	return r.db.BeginTx(ctx)
}

func (r *PostgresRouteRepository) Create(ctx context.Context, rt *route.Route, auditCtx *AuditContext) error {
	if err := rt.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	if rt.TenantID == uuid.Nil {
		if tenantID, ok := GetTenantIDFromContext(ctx); ok {
			rt.TenantID = tenantID
		}
	}

	if auditCtx == nil {
		return r.createWithoutAudit(ctx, rt)
	}

	return r.createWithAudit(ctx, rt, auditCtx)
}

func (r *PostgresRouteRepository) createWithoutAudit(ctx context.Context, rt *route.Route) error {
	headersJSON, err := json.Marshal(rt.Headers)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO routes (id, tenant_id, name, service_id, protocols, methods, hosts, paths, headers, strip_path, preserve_host, regex_priority, tags, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`

	_, err = r.db.ExecContext(ctx, query,
		rt.ID,
		rt.TenantID,
		nullString(rt.Name),
		rt.ServiceID,
		pq.Array(rt.Protocols),
		pq.Array(rt.Methods),
		pq.Array(rt.Hosts),
		pq.Array(rt.Paths),
		headersJSON,
		rt.StripPath,
		rt.PreserveHost,
		rt.RegexPriority,
		pq.Array(rt.Tags),
		rt.Enabled,
		rt.CreatedAt,
		rt.UpdatedAt,
	)

	if err != nil {
		if isForeignKeyViolation(err) {
			return fmt.Errorf("%w: service not found", ErrInvalidInput)
		}
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}

	return nil
}

func (r *PostgresRouteRepository) createWithAudit(ctx context.Context, rt *route.Route, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	headersJSON, err := json.Marshal(rt.Headers)
	if err != nil {
		return err
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `
		INSERT INTO routes (id, tenant_id, name, service_id, protocols, methods, hosts, paths, headers, strip_path, preserve_host, regex_priority, tags, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`

	_, err = tx.ExecContext(ctx, query,
		rt.ID,
		rt.TenantID,
		nullString(rt.Name),
		rt.ServiceID,
		pq.Array(rt.Protocols),
		pq.Array(rt.Methods),
		pq.Array(rt.Hosts),
		pq.Array(rt.Paths),
		headersJSON,
		rt.StripPath,
		rt.PreserveHost,
		rt.RegexPriority,
		pq.Array(rt.Tags),
		rt.Enabled,
		rt.CreatedAt,
		rt.UpdatedAt,
	)

	if err != nil {
		if isForeignKeyViolation(err) {
			return fmt.Errorf("%w: service not found", ErrInvalidInput)
		}
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}

	if err := auditRepo.logCreateWithTx(ctx, tx, auditCtx, "route", rt.ID, rt); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (r *PostgresRouteRepository) GetByID(ctx context.Context, id uuid.UUID) (*route.Route, error) {
	return r.getByID(ctx, r.db, id)
}

func (r *PostgresRouteRepository) getByID(ctx context.Context, exec Executor, id uuid.UUID) (*route.Route, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, name, service_id, protocols, methods, hosts, paths, headers, strip_path, preserve_host, regex_priority, tags, enabled, created_at, updated_at
			FROM routes
			WHERE id = $1 AND tenant_id = $2
		`
		args = []interface{}{id, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, name, service_id, protocols, methods, hosts, paths, headers, strip_path, preserve_host, regex_priority, tags, enabled, created_at, updated_at
			FROM routes
			WHERE id = $1
		`
		args = []interface{}{id}
	}

	var rt route.Route
	var name sql.NullString
	var headersJSON []byte

	err := exec.QueryRowContext(ctx, query, args...).Scan(
		&rt.ID,
		&rt.TenantID,
		&name,
		&rt.ServiceID,
		pq.Array(&rt.Protocols),
		pq.Array(&rt.Methods),
		pq.Array(&rt.Hosts),
		pq.Array(&rt.Paths),
		&headersJSON,
		&rt.StripPath,
		&rt.PreserveHost,
		&rt.RegexPriority,
		pq.Array(&rt.Tags),
		&rt.Enabled,
		&rt.CreatedAt,
		&rt.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	rt.Name = name.String
	if len(headersJSON) > 0 {
		var headers map[string][]string
		if err := json.Unmarshal(headersJSON, &headers); err == nil {
			rt.Headers = headers
		}
	}

	return &rt, nil
}

func (r *PostgresRouteRepository) ListByServiceID(ctx context.Context, serviceID uuid.UUID) ([]*route.Route, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, name, service_id, protocols, methods, hosts, paths, headers, strip_path, preserve_host, regex_priority, tags, enabled, created_at, updated_at
			FROM routes
			WHERE service_id = $1 AND tenant_id = $2
			ORDER BY created_at DESC
		`
		args = []interface{}{serviceID, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, name, service_id, protocols, methods, hosts, paths, headers, strip_path, preserve_host, regex_priority, tags, enabled, created_at, updated_at
			FROM routes
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

	items := make([]*route.Route, 0)
	for rows.Next() {
		var rt route.Route
		var name sql.NullString
		var headersJSON []byte

		if err := rows.Scan(
			&rt.ID,
			&rt.TenantID,
			&name,
			&rt.ServiceID,
			pq.Array(&rt.Protocols),
			pq.Array(&rt.Methods),
			pq.Array(&rt.Hosts),
			pq.Array(&rt.Paths),
			&headersJSON,
			&rt.StripPath,
			&rt.PreserveHost,
			&rt.RegexPriority,
			pq.Array(&rt.Tags),
			&rt.Enabled,
			&rt.CreatedAt,
			&rt.UpdatedAt,
		); err != nil {
			return nil, err
		}

		rt.Name = name.String
		if len(headersJSON) > 0 {
			var headers map[string][]string
			if err := json.Unmarshal(headersJSON, &headers); err == nil {
				rt.Headers = headers
			}
		}

		items = append(items, &rt)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (r *PostgresRouteRepository) List(ctx context.Context, pagination *Pagination) (*PageResult[*route.Route], error) {
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
		countQuery = `SELECT COUNT(*) FROM routes WHERE tenant_id = $1`
		countArgs = []interface{}{tenantID}

		listQuery = `
			SELECT id, tenant_id, name, service_id, protocols, methods, hosts, paths, headers, strip_path, preserve_host, regex_priority, tags, enabled, created_at, updated_at
			FROM routes
			WHERE tenant_id = $1
			ORDER BY created_at DESC
			LIMIT $2 OFFSET $3
		`
		listArgs = []interface{}{tenantID, pagination.PageSize, offset}
	} else {
		countQuery = `SELECT COUNT(*) FROM routes`
		countArgs = []interface{}{}

		listQuery = `
			SELECT id, tenant_id, name, service_id, protocols, methods, hosts, paths, headers, strip_path, preserve_host, regex_priority, tags, enabled, created_at, updated_at
			FROM routes
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

	items := make([]*route.Route, 0)
	for rows.Next() {
		var rt route.Route
		var name sql.NullString
		var headersJSON []byte

		if err := rows.Scan(
			&rt.ID,
			&rt.TenantID,
			&name,
			&rt.ServiceID,
			pq.Array(&rt.Protocols),
			pq.Array(&rt.Methods),
			pq.Array(&rt.Hosts),
			pq.Array(&rt.Paths),
			&headersJSON,
			&rt.StripPath,
			&rt.PreserveHost,
			&rt.RegexPriority,
			pq.Array(&rt.Tags),
			&rt.Enabled,
			&rt.CreatedAt,
			&rt.UpdatedAt,
		); err != nil {
			return nil, err
		}

		rt.Name = name.String
		if len(headersJSON) > 0 {
			var headers map[string][]string
			if err := json.Unmarshal(headersJSON, &headers); err == nil {
				rt.Headers = headers
			}
		}

		items = append(items, &rt)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return NewPageResult(items, total, pagination.Page, pagination.PageSize), nil
}

func (r *PostgresRouteRepository) Update(ctx context.Context, rt *route.Route, auditCtx *AuditContext) error {
	if err := rt.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	if auditCtx == nil {
		return r.updateWithoutAudit(ctx, rt)
	}

	return r.updateWithAudit(ctx, rt, auditCtx)
}

func (r *PostgresRouteRepository) updateWithoutAudit(ctx context.Context, rt *route.Route) error {
	headersJSON, err := json.Marshal(rt.Headers)
	if err != nil {
		return err
	}

	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			UPDATE routes
			SET name = $2, service_id = $3, protocols = $4, methods = $5, hosts = $6, paths = $7, headers = $8, strip_path = $9, preserve_host = $10, regex_priority = $11, tags = $12, enabled = $13, updated_at = NOW()
			WHERE id = $1 AND tenant_id = $14
		`
		args = []interface{}{
			rt.ID,
			nullString(rt.Name),
			rt.ServiceID,
			pq.Array(rt.Protocols),
			pq.Array(rt.Methods),
			pq.Array(rt.Hosts),
			pq.Array(rt.Paths),
			headersJSON,
			rt.StripPath,
			rt.PreserveHost,
			rt.RegexPriority,
			pq.Array(rt.Tags),
			rt.Enabled,
			tenantID,
		}
	} else {
		query = `
			UPDATE routes
			SET name = $2, service_id = $3, protocols = $4, methods = $5, hosts = $6, paths = $7, headers = $8, strip_path = $9, preserve_host = $10, regex_priority = $11, tags = $12, enabled = $13, updated_at = NOW()
			WHERE id = $1
		`
		args = []interface{}{
			rt.ID,
			nullString(rt.Name),
			rt.ServiceID,
			pq.Array(rt.Protocols),
			pq.Array(rt.Methods),
			pq.Array(rt.Hosts),
			pq.Array(rt.Paths),
			headersJSON,
			rt.StripPath,
			rt.PreserveHost,
			rt.RegexPriority,
			pq.Array(rt.Tags),
			rt.Enabled,
		}
	}

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		if isForeignKeyViolation(err) {
			return fmt.Errorf("%w: service not found", ErrInvalidInput)
		}
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

func (r *PostgresRouteRepository) updateWithAudit(ctx context.Context, rt *route.Route, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	headersJSON, err := json.Marshal(rt.Headers)
	if err != nil {
		return err
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	oldRoute, err := r.getByID(ctx, tx, rt.ID)
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
			UPDATE routes
			SET name = $2, service_id = $3, protocols = $4, methods = $5, hosts = $6, paths = $7, headers = $8, strip_path = $9, preserve_host = $10, regex_priority = $11, tags = $12, enabled = $13, updated_at = NOW()
			WHERE id = $1 AND tenant_id = $14
		`
		args = []interface{}{
			rt.ID,
			nullString(rt.Name),
			rt.ServiceID,
			pq.Array(rt.Protocols),
			pq.Array(rt.Methods),
			pq.Array(rt.Hosts),
			pq.Array(rt.Paths),
			headersJSON,
			rt.StripPath,
			rt.PreserveHost,
			rt.RegexPriority,
			pq.Array(rt.Tags),
			rt.Enabled,
			tenantID,
		}
	} else {
		query = `
			UPDATE routes
			SET name = $2, service_id = $3, protocols = $4, methods = $5, hosts = $6, paths = $7, headers = $8, strip_path = $9, preserve_host = $10, regex_priority = $11, tags = $12, enabled = $13, updated_at = NOW()
			WHERE id = $1
		`
		args = []interface{}{
			rt.ID,
			nullString(rt.Name),
			rt.ServiceID,
			pq.Array(rt.Protocols),
			pq.Array(rt.Methods),
			pq.Array(rt.Hosts),
			pq.Array(rt.Paths),
			headersJSON,
			rt.StripPath,
			rt.PreserveHost,
			rt.RegexPriority,
			pq.Array(rt.Tags),
			rt.Enabled,
		}
	}

	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		if isForeignKeyViolation(err) {
			return fmt.Errorf("%w: service not found", ErrInvalidInput)
		}
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}

	if err := auditRepo.logUpdateWithTx(ctx, tx, auditCtx, "route", rt.ID, oldRoute, rt); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (r *PostgresRouteRepository) Delete(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error {
	if auditCtx == nil {
		return r.deleteWithoutAudit(ctx, id)
	}

	return r.deleteWithAudit(ctx, id, auditCtx)
}

func (r *PostgresRouteRepository) deleteWithoutAudit(ctx context.Context, id uuid.UUID) error {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `DELETE FROM routes WHERE id = $1 AND tenant_id = $2`
		args = []interface{}{id, tenantID}
	} else {
		query = `DELETE FROM routes WHERE id = $1`
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

func (r *PostgresRouteRepository) deleteWithAudit(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	oldRoute, err := r.getByID(ctx, tx, id)
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
		query = `DELETE FROM routes WHERE id = $1 AND tenant_id = $2`
		args = []interface{}{id, tenantID}
	} else {
		query = `DELETE FROM routes WHERE id = $1`
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

	if err := auditRepo.logDeleteWithTx(ctx, tx, auditCtx, "route", id, oldRoute); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}
