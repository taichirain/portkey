package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/taichirain/portkey/internal/domain/service"
	"github.com/taichirain/portkey/internal/platform/postgres"
)

type ServiceRepository interface {
	Repository
	Create(ctx context.Context, svc *service.Service, auditCtx *AuditContext) error
	GetByID(ctx context.Context, id uuid.UUID) (*service.Service, error)
	GetByName(ctx context.Context, name string) (*service.Service, error)
	List(ctx context.Context, pagination *Pagination) (*PageResult[*service.Service], error)
	Update(ctx context.Context, svc *service.Service, auditCtx *AuditContext) error
	Delete(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error
}

type PostgresServiceRepository struct {
	db        *postgres.DB
	auditRepo AuditRepository
}

func NewPostgresServiceRepository(db *postgres.DB, auditRepo AuditRepository) *PostgresServiceRepository {
	return &PostgresServiceRepository{
		db:        db,
		auditRepo: auditRepo,
	}
}

func (r *PostgresServiceRepository) BeginTx(ctx context.Context) (Tx, error) {
	return r.db.BeginTx(ctx)
}

func (r *PostgresServiceRepository) Create(ctx context.Context, svc *service.Service, auditCtx *AuditContext) error {
	if err := svc.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	if svc.TenantID == uuid.Nil {
		if tenantID, ok := GetTenantIDFromContext(ctx); ok {
			svc.TenantID = tenantID
		}
	}

	if auditCtx == nil {
		return r.createWithoutAudit(ctx, svc)
	}

	return r.createWithAudit(ctx, svc, auditCtx)
}

func (r *PostgresServiceRepository) createWithoutAudit(ctx context.Context, svc *service.Service) error {
	query := `
		INSERT INTO services (id, tenant_id, name, protocol, host, port, path, upstream_id, retries, connect_timeout, write_timeout, read_timeout, tags, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`

	_, err := r.db.ExecContext(ctx, query,
		svc.ID,
		svc.TenantID,
		svc.Name,
		string(svc.Protocol),
		nullString(svc.Host),
		nullInt(svc.Port),
		nullString(svc.Path),
		nullUUID(svc.UpstreamID),
		svc.Retries,
		svc.ConnectTimeout,
		svc.WriteTimeout,
		svc.ReadTimeout,
		pq.Array(svc.Tags),
		svc.Enabled,
		svc.CreatedAt,
		svc.UpdatedAt,
	)

	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}

	return nil
}

func (r *PostgresServiceRepository) createWithAudit(ctx context.Context, svc *service.Service, auditCtx *AuditContext) error {
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
		INSERT INTO services (id, tenant_id, name, protocol, host, port, path, upstream_id, retries, connect_timeout, write_timeout, read_timeout, tags, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`

	_, err = tx.ExecContext(ctx, query,
		svc.ID,
		svc.TenantID,
		svc.Name,
		string(svc.Protocol),
		nullString(svc.Host),
		nullInt(svc.Port),
		nullString(svc.Path),
		nullUUID(svc.UpstreamID),
		svc.Retries,
		svc.ConnectTimeout,
		svc.WriteTimeout,
		svc.ReadTimeout,
		pq.Array(svc.Tags),
		svc.Enabled,
		svc.CreatedAt,
		svc.UpdatedAt,
	)

	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}

	if err := auditRepo.logCreateWithTx(ctx, tx, auditCtx, "service", svc.ID, svc); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (r *PostgresServiceRepository) GetByID(ctx context.Context, id uuid.UUID) (*service.Service, error) {
	return r.getByID(ctx, r.db, id)
}

func (r *PostgresServiceRepository) getByID(ctx context.Context, exec Executor, id uuid.UUID) (*service.Service, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, name, protocol, host, port, path, upstream_id, retries, connect_timeout, write_timeout, read_timeout, tags, enabled, created_at, updated_at
			FROM services
			WHERE id = $1 AND tenant_id = $2
		`
		args = []interface{}{id, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, name, protocol, host, port, path, upstream_id, retries, connect_timeout, write_timeout, read_timeout, tags, enabled, created_at, updated_at
			FROM services
			WHERE id = $1
		`
		args = []interface{}{id}
	}

	svc := &service.Service{}
	var host sql.NullString
	var port sql.NullInt32
	var path sql.NullString
	var upstreamID uuid.UUID

	err := exec.QueryRowContext(ctx, query, args...).Scan(
		&svc.ID,
		&svc.TenantID,
		&svc.Name,
		&svc.Protocol,
		&host,
		&port,
		&path,
		&upstreamID,
		&svc.Retries,
		&svc.ConnectTimeout,
		&svc.WriteTimeout,
		&svc.ReadTimeout,
		pq.Array(&svc.Tags),
		&svc.Enabled,
		&svc.CreatedAt,
		&svc.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	svc.Host = host.String
	svc.Port = int(port.Int32)
	svc.Path = path.String
	svc.UpstreamID = upstreamID

	return svc, nil
}

func (r *PostgresServiceRepository) GetByName(ctx context.Context, name string) (*service.Service, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, name, protocol, host, port, path, upstream_id, retries, connect_timeout, write_timeout, read_timeout, tags, enabled, created_at, updated_at
			FROM services
			WHERE name = $1 AND tenant_id = $2
		`
		args = []interface{}{name, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, name, protocol, host, port, path, upstream_id, retries, connect_timeout, write_timeout, read_timeout, tags, enabled, created_at, updated_at
			FROM services
			WHERE name = $1
		`
		args = []interface{}{name}
	}

	var svc service.Service
	var host sql.NullString
	var port sql.NullInt32
	var path sql.NullString
	var upstreamID uuid.UUID

	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&svc.ID,
		&svc.TenantID,
		&svc.Name,
		&svc.Protocol,
		&host,
		&port,
		&path,
		&upstreamID,
		&svc.Retries,
		&svc.ConnectTimeout,
		&svc.WriteTimeout,
		&svc.ReadTimeout,
		pq.Array(&svc.Tags),
		&svc.Enabled,
		&svc.CreatedAt,
		&svc.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	svc.Host = host.String
	svc.Port = int(port.Int32)
	svc.Path = path.String
	svc.UpstreamID = upstreamID

	return &svc, nil
}

func (r *PostgresServiceRepository) List(ctx context.Context, pagination *Pagination) (*PageResult[*service.Service], error) {
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
		countQuery = `SELECT COUNT(*) FROM services WHERE tenant_id = $1`
		countArgs = []interface{}{tenantID}

		listQuery = `
			SELECT id, tenant_id, name, protocol, host, port, path, upstream_id, retries, connect_timeout, write_timeout, read_timeout, tags, enabled, created_at, updated_at
			FROM services
			WHERE tenant_id = $1
			ORDER BY created_at DESC
			LIMIT $2 OFFSET $3
		`
		listArgs = []interface{}{tenantID, pagination.PageSize, offset}
	} else {
		countQuery = `SELECT COUNT(*) FROM services`
		countArgs = []interface{}{}

		listQuery = `
			SELECT id, tenant_id, name, protocol, host, port, path, upstream_id, retries, connect_timeout, write_timeout, read_timeout, tags, enabled, created_at, updated_at
			FROM services
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

	items := make([]*service.Service, 0)
	for rows.Next() {
		var svc service.Service
		var host sql.NullString
		var port sql.NullInt32
		var path sql.NullString
		var upstreamID uuid.UUID

		if err := rows.Scan(
			&svc.ID,
			&svc.TenantID,
			&svc.Name,
			&svc.Protocol,
			&host,
			&port,
			&path,
			&upstreamID,
			&svc.Retries,
			&svc.ConnectTimeout,
			&svc.WriteTimeout,
			&svc.ReadTimeout,
			pq.Array(&svc.Tags),
			&svc.Enabled,
			&svc.CreatedAt,
			&svc.UpdatedAt,
		); err != nil {
			return nil, err
		}

		svc.Host = host.String
		svc.Port = int(port.Int32)
		svc.Path = path.String
		svc.UpstreamID = upstreamID
		items = append(items, &svc)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return NewPageResult(items, total, pagination.Page, pagination.PageSize), nil
}

func (r *PostgresServiceRepository) Update(ctx context.Context, svc *service.Service, auditCtx *AuditContext) error {
	if err := svc.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	if auditCtx == nil {
		return r.updateWithoutAudit(ctx, svc)
	}

	return r.updateWithAudit(ctx, svc, auditCtx)
}

func (r *PostgresServiceRepository) updateWithoutAudit(ctx context.Context, svc *service.Service) error {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			UPDATE services
			SET name = $2, protocol = $3, host = $4, port = $5, path = $6, upstream_id = $7, retries = $8, connect_timeout = $9, write_timeout = $10, read_timeout = $11, tags = $12, enabled = $13, updated_at = NOW()
			WHERE id = $1 AND tenant_id = $14
		`
		args = []interface{}{
			svc.ID,
			svc.Name,
			string(svc.Protocol),
			nullString(svc.Host),
			nullInt(svc.Port),
			nullString(svc.Path),
			nullUUID(svc.UpstreamID),
			svc.Retries,
			svc.ConnectTimeout,
			svc.WriteTimeout,
			svc.ReadTimeout,
			pq.Array(svc.Tags),
			svc.Enabled,
			tenantID,
		}
	} else {
		query = `
			UPDATE services
			SET name = $2, protocol = $3, host = $4, port = $5, path = $6, upstream_id = $7, retries = $8, connect_timeout = $9, write_timeout = $10, read_timeout = $11, tags = $12, enabled = $13, updated_at = NOW()
			WHERE id = $1
		`
		args = []interface{}{
			svc.ID,
			svc.Name,
			string(svc.Protocol),
			nullString(svc.Host),
			nullInt(svc.Port),
			nullString(svc.Path),
			nullUUID(svc.UpstreamID),
			svc.Retries,
			svc.ConnectTimeout,
			svc.WriteTimeout,
			svc.ReadTimeout,
			pq.Array(svc.Tags),
			svc.Enabled,
		}
	}

	result, err := r.db.ExecContext(ctx, query, args...)
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

func (r *PostgresServiceRepository) updateWithAudit(ctx context.Context, svc *service.Service, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	oldSvc, err := r.getByID(ctx, tx, svc.ID)
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
			UPDATE services
			SET name = $2, protocol = $3, host = $4, port = $5, path = $6, upstream_id = $7, retries = $8, connect_timeout = $9, write_timeout = $10, read_timeout = $11, tags = $12, enabled = $13, updated_at = NOW()
			WHERE id = $1 AND tenant_id = $14
		`
		args = []interface{}{
			svc.ID,
			svc.Name,
			string(svc.Protocol),
			nullString(svc.Host),
			nullInt(svc.Port),
			nullString(svc.Path),
			nullUUID(svc.UpstreamID),
			svc.Retries,
			svc.ConnectTimeout,
			svc.WriteTimeout,
			svc.ReadTimeout,
			pq.Array(svc.Tags),
			svc.Enabled,
			tenantID,
		}
	} else {
		query = `
			UPDATE services
			SET name = $2, protocol = $3, host = $4, port = $5, path = $6, upstream_id = $7, retries = $8, connect_timeout = $9, write_timeout = $10, read_timeout = $11, tags = $12, enabled = $13, updated_at = NOW()
			WHERE id = $1
		`
		args = []interface{}{
			svc.ID,
			svc.Name,
			string(svc.Protocol),
			nullString(svc.Host),
			nullInt(svc.Port),
			nullString(svc.Path),
			nullUUID(svc.UpstreamID),
			svc.Retries,
			svc.ConnectTimeout,
			svc.WriteTimeout,
			svc.ReadTimeout,
			pq.Array(svc.Tags),
			svc.Enabled,
		}
	}

	result, err := tx.ExecContext(ctx, query, args...)
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

	if err := auditRepo.logUpdateWithTx(ctx, tx, auditCtx, "service", svc.ID, oldSvc, svc); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (r *PostgresServiceRepository) Delete(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error {
	if auditCtx == nil {
		return r.deleteWithoutAudit(ctx, id)
	}

	return r.deleteWithAudit(ctx, id, auditCtx)
}

func (r *PostgresServiceRepository) deleteWithoutAudit(ctx context.Context, id uuid.UUID) error {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `DELETE FROM services WHERE id = $1 AND tenant_id = $2`
		args = []interface{}{id, tenantID}
	} else {
		query = `DELETE FROM services WHERE id = $1`
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

func (r *PostgresServiceRepository) deleteWithAudit(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	oldSvc, err := r.getByID(ctx, tx, id)
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
		query = `DELETE FROM services WHERE id = $1 AND tenant_id = $2`
		args = []interface{}{id, tenantID}
	} else {
		query = `DELETE FROM services WHERE id = $1`
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

	if err := auditRepo.logDeleteWithTx(ctx, tx, auditCtx, "service", id, oldSvc); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}
