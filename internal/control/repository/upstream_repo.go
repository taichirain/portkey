package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/taichirain/portkey/internal/domain/upstream"
	"github.com/taichirain/portkey/internal/platform/postgres"
)

type UpstreamRepository interface {
	Repository
	Create(ctx context.Context, u *upstream.Upstream, auditCtx *AuditContext) error
	GetByID(ctx context.Context, id uuid.UUID) (*upstream.Upstream, error)
	GetByName(ctx context.Context, name string) (*upstream.Upstream, error)
	List(ctx context.Context, pagination *Pagination) (*PageResult[*upstream.Upstream], error)
	Update(ctx context.Context, u *upstream.Upstream, auditCtx *AuditContext) error
	Delete(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error
}

type PostgresUpstreamRepository struct {
	db        *postgres.DB
	auditRepo AuditRepository
}

func NewPostgresUpstreamRepository(db *postgres.DB, auditRepo AuditRepository) *PostgresUpstreamRepository {
	return &PostgresUpstreamRepository{
		db:        db,
		auditRepo: auditRepo,
	}
}

func (r *PostgresUpstreamRepository) BeginTx(ctx context.Context) (Tx, error) {
	return r.db.BeginTx(ctx)
}

func (r *PostgresUpstreamRepository) Create(ctx context.Context, u *upstream.Upstream, auditCtx *AuditContext) error {
	if err := u.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	if u.TenantID == uuid.Nil {
		if tenantID, ok := GetTenantIDFromContext(ctx); ok {
			u.TenantID = tenantID
		}
	}

	if auditCtx == nil {
		return r.createWithoutAudit(ctx, u)
	}

	return r.createWithAudit(ctx, u, auditCtx)
}

func (r *PostgresUpstreamRepository) createWithoutAudit(ctx context.Context, u *upstream.Upstream) error {
	healthChecksJSON, err := json.Marshal(u.HealthChecks)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO upstreams (id, tenant_id, name, algorithm, slots, healthchecks, hash_on, hash_fallback, hash_on_header, hash_fallback_header, hash_on_cookie, hash_on_cookie_path, tags, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`

	_, err = r.db.ExecContext(ctx, query,
		u.ID,
		u.TenantID,
		u.Name,
		string(u.Algorithm),
		u.Slots,
		healthChecksJSON,
		nullString(string(u.HashOn)),
		nullString(string(u.HashFallback)),
		nullString(u.HashOnHeader),
		nullString(u.HashFallbackHeader),
		nullString(u.HashOnCookie),
		nullString(u.HashOnCookiePath),
		pq.Array(u.Tags),
		u.CreatedAt,
		u.UpdatedAt,
	)

	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}

	return nil
}

func (r *PostgresUpstreamRepository) createWithAudit(ctx context.Context, u *upstream.Upstream, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	healthChecksJSON, err := json.Marshal(u.HealthChecks)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO upstreams (id, tenant_id, name, algorithm, slots, healthchecks, hash_on, hash_fallback, hash_on_header, hash_fallback_header, hash_on_cookie, hash_on_cookie_path, tags, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`

	_, err = tx.ExecContext(ctx, query,
		u.ID,
		u.TenantID,
		u.Name,
		string(u.Algorithm),
		u.Slots,
		healthChecksJSON,
		nullString(string(u.HashOn)),
		nullString(string(u.HashFallback)),
		nullString(u.HashOnHeader),
		nullString(u.HashFallbackHeader),
		nullString(u.HashOnCookie),
		nullString(u.HashOnCookiePath),
		pq.Array(u.Tags),
		u.CreatedAt,
		u.UpdatedAt,
	)

	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}

	if err := auditRepo.logCreateWithTx(ctx, tx, auditCtx, "upstream", u.ID, u); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (r *PostgresUpstreamRepository) GetByID(ctx context.Context, id uuid.UUID) (*upstream.Upstream, error) {
	return r.getByID(ctx, r.db, id)
}

func (r *PostgresUpstreamRepository) getByID(ctx context.Context, exec Executor, id uuid.UUID) (*upstream.Upstream, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, name, algorithm, slots, healthchecks, hash_on, hash_fallback, hash_on_header, hash_fallback_header, hash_on_cookie, hash_on_cookie_path, tags, created_at, updated_at
			FROM upstreams
			WHERE id = $1 AND tenant_id = $2
		`
		args = []interface{}{id, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, name, algorithm, slots, healthchecks, hash_on, hash_fallback, hash_on_header, hash_fallback_header, hash_on_cookie, hash_on_cookie_path, tags, created_at, updated_at
			FROM upstreams
			WHERE id = $1
		`
		args = []interface{}{id}
	}

	var u upstream.Upstream
	var healthChecksJSON []byte
	var hashOn, hashFallback sql.NullString
	var hashOnHeader, hashFallbackHeader, hashOnCookie, hashOnCookiePath sql.NullString

	err := exec.QueryRowContext(ctx, query, args...).Scan(
		&u.ID,
		&u.TenantID,
		&u.Name,
		&u.Algorithm,
		&u.Slots,
		&healthChecksJSON,
		&hashOn,
		&hashFallback,
		&hashOnHeader,
		&hashFallbackHeader,
		&hashOnCookie,
		&hashOnCookiePath,
		pq.Array(&u.Tags),
		&u.CreatedAt,
		&u.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	u.HashOn = upstream.HashOn(hashOn.String)
	u.HashFallback = upstream.HashOn(hashFallback.String)
	u.HashOnHeader = hashOnHeader.String
	u.HashFallbackHeader = hashFallbackHeader.String
	u.HashOnCookie = hashOnCookie.String
	u.HashOnCookiePath = hashOnCookiePath.String

	if len(healthChecksJSON) > 0 {
		var hc upstream.HealthChecks
		if err := json.Unmarshal(healthChecksJSON, &hc); err == nil {
			u.HealthChecks = &hc
		}
	}

	return &u, nil
}

func (r *PostgresUpstreamRepository) GetByName(ctx context.Context, name string) (*upstream.Upstream, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, name, algorithm, slots, healthchecks, hash_on, hash_fallback, hash_on_header, hash_fallback_header, hash_on_cookie, hash_on_cookie_path, tags, created_at, updated_at
			FROM upstreams
			WHERE name = $1 AND tenant_id = $2
		`
		args = []interface{}{name, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, name, algorithm, slots, healthchecks, hash_on, hash_fallback, hash_on_header, hash_fallback_header, hash_on_cookie, hash_on_cookie_path, tags, created_at, updated_at
			FROM upstreams
			WHERE name = $1
		`
		args = []interface{}{name}
	}

	var u upstream.Upstream
	var healthChecksJSON []byte
	var hashOn, hashFallback sql.NullString
	var hashOnHeader, hashFallbackHeader, hashOnCookie, hashOnCookiePath sql.NullString

	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&u.ID,
		&u.TenantID,
		&u.Name,
		&u.Algorithm,
		&u.Slots,
		&healthChecksJSON,
		&hashOn,
		&hashFallback,
		&hashOnHeader,
		&hashFallbackHeader,
		&hashOnCookie,
		&hashOnCookiePath,
		pq.Array(&u.Tags),
		&u.CreatedAt,
		&u.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	u.HashOn = upstream.HashOn(hashOn.String)
	u.HashFallback = upstream.HashOn(hashFallback.String)
	u.HashOnHeader = hashOnHeader.String
	u.HashFallbackHeader = hashFallbackHeader.String
	u.HashOnCookie = hashOnCookie.String
	u.HashOnCookiePath = hashOnCookiePath.String

	if len(healthChecksJSON) > 0 {
		var hc upstream.HealthChecks
		if err := json.Unmarshal(healthChecksJSON, &hc); err == nil {
			u.HealthChecks = &hc
		}
	}

	return &u, nil
}

func (r *PostgresUpstreamRepository) List(ctx context.Context, pagination *Pagination) (*PageResult[*upstream.Upstream], error) {
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
		countQuery = `SELECT COUNT(*) FROM upstreams WHERE tenant_id = $1`
		countArgs = []interface{}{tenantID}

		listQuery = `
			SELECT id, tenant_id, name, algorithm, slots, healthchecks, hash_on, hash_fallback, hash_on_header, hash_fallback_header, hash_on_cookie, hash_on_cookie_path, tags, created_at, updated_at
			FROM upstreams
			WHERE tenant_id = $1
			ORDER BY created_at DESC
			LIMIT $2 OFFSET $3
		`
		listArgs = []interface{}{tenantID, pagination.PageSize, offset}
	} else {
		countQuery = `SELECT COUNT(*) FROM upstreams`
		countArgs = []interface{}{}

		listQuery = `
			SELECT id, tenant_id, name, algorithm, slots, healthchecks, hash_on, hash_fallback, hash_on_header, hash_fallback_header, hash_on_cookie, hash_on_cookie_path, tags, created_at, updated_at
			FROM upstreams
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

	items := make([]*upstream.Upstream, 0)
	for rows.Next() {
		var u upstream.Upstream
		var healthChecksJSON []byte
		var hashOn, hashFallback sql.NullString
		var hashOnHeader, hashFallbackHeader, hashOnCookie, hashOnCookiePath sql.NullString

		if err := rows.Scan(
			&u.ID,
			&u.TenantID,
			&u.Name,
			&u.Algorithm,
			&u.Slots,
			&healthChecksJSON,
			&hashOn,
			&hashFallback,
			&hashOnHeader,
			&hashFallbackHeader,
			&hashOnCookie,
			&hashOnCookiePath,
			pq.Array(&u.Tags),
			&u.CreatedAt,
			&u.UpdatedAt,
		); err != nil {
			return nil, err
		}

		u.HashOn = upstream.HashOn(hashOn.String)
		u.HashFallback = upstream.HashOn(hashFallback.String)
		u.HashOnHeader = hashOnHeader.String
		u.HashFallbackHeader = hashFallbackHeader.String
		u.HashOnCookie = hashOnCookie.String
		u.HashOnCookiePath = hashOnCookiePath.String

		if len(healthChecksJSON) > 0 {
			var hc upstream.HealthChecks
			if err := json.Unmarshal(healthChecksJSON, &hc); err == nil {
				u.HealthChecks = &hc
			}
		}

		items = append(items, &u)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return NewPageResult(items, total, pagination.Page, pagination.PageSize), nil
}

func (r *PostgresUpstreamRepository) Update(ctx context.Context, u *upstream.Upstream, auditCtx *AuditContext) error {
	if err := u.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	if auditCtx == nil {
		return r.updateWithoutAudit(ctx, u)
	}

	return r.updateWithAudit(ctx, u, auditCtx)
}

func (r *PostgresUpstreamRepository) updateWithoutAudit(ctx context.Context, u *upstream.Upstream) error {
	healthChecksJSON, err := json.Marshal(u.HealthChecks)
	if err != nil {
		return err
	}

	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			UPDATE upstreams
			SET name = $2, algorithm = $3, slots = $4, healthchecks = $5, hash_on = $6, hash_fallback = $7, hash_on_header = $8, hash_fallback_header = $9, hash_on_cookie = $10, hash_on_cookie_path = $11, tags = $12, updated_at = NOW()
			WHERE id = $1 AND tenant_id = $13
		`
		args = []interface{}{
			u.ID,
			u.Name,
			string(u.Algorithm),
			u.Slots,
			healthChecksJSON,
			nullString(string(u.HashOn)),
			nullString(string(u.HashFallback)),
			nullString(u.HashOnHeader),
			nullString(u.HashFallbackHeader),
			nullString(u.HashOnCookie),
			nullString(u.HashOnCookiePath),
			pq.Array(u.Tags),
			tenantID,
		}
	} else {
		query = `
			UPDATE upstreams
			SET name = $2, algorithm = $3, slots = $4, healthchecks = $5, hash_on = $6, hash_fallback = $7, hash_on_header = $8, hash_fallback_header = $9, hash_on_cookie = $10, hash_on_cookie_path = $11, tags = $12, updated_at = NOW()
			WHERE id = $1
		`
		args = []interface{}{
			u.ID,
			u.Name,
			string(u.Algorithm),
			u.Slots,
			healthChecksJSON,
			nullString(string(u.HashOn)),
			nullString(string(u.HashFallback)),
			nullString(u.HashOnHeader),
			nullString(u.HashFallbackHeader),
			nullString(u.HashOnCookie),
			nullString(u.HashOnCookiePath),
			pq.Array(u.Tags),
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

func (r *PostgresUpstreamRepository) updateWithAudit(ctx context.Context, u *upstream.Upstream, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	oldUpstream, err := r.getByID(ctx, tx, u.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}

	healthChecksJSON, err := json.Marshal(u.HealthChecks)
	if err != nil {
		return err
	}

	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			UPDATE upstreams
			SET name = $2, algorithm = $3, slots = $4, healthchecks = $5, hash_on = $6, hash_fallback = $7, hash_on_header = $8, hash_fallback_header = $9, hash_on_cookie = $10, hash_on_cookie_path = $11, tags = $12, updated_at = NOW()
			WHERE id = $1 AND tenant_id = $13
		`
		args = []interface{}{
			u.ID,
			u.Name,
			string(u.Algorithm),
			u.Slots,
			healthChecksJSON,
			nullString(string(u.HashOn)),
			nullString(string(u.HashFallback)),
			nullString(u.HashOnHeader),
			nullString(u.HashFallbackHeader),
			nullString(u.HashOnCookie),
			nullString(u.HashOnCookiePath),
			pq.Array(u.Tags),
			tenantID,
		}
	} else {
		query = `
			UPDATE upstreams
			SET name = $2, algorithm = $3, slots = $4, healthchecks = $5, hash_on = $6, hash_fallback = $7, hash_on_header = $8, hash_fallback_header = $9, hash_on_cookie = $10, hash_on_cookie_path = $11, tags = $12, updated_at = NOW()
			WHERE id = $1
		`
		args = []interface{}{
			u.ID,
			u.Name,
			string(u.Algorithm),
			u.Slots,
			healthChecksJSON,
			nullString(string(u.HashOn)),
			nullString(string(u.HashFallback)),
			nullString(u.HashOnHeader),
			nullString(u.HashFallbackHeader),
			nullString(u.HashOnCookie),
			nullString(u.HashOnCookiePath),
			pq.Array(u.Tags),
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

	if err := auditRepo.logUpdateWithTx(ctx, tx, auditCtx, "upstream", u.ID, oldUpstream, u); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (r *PostgresUpstreamRepository) Delete(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error {
	if auditCtx == nil {
		return r.deleteWithoutAudit(ctx, id)
	}

	return r.deleteWithAudit(ctx, id, auditCtx)
}

func (r *PostgresUpstreamRepository) deleteWithoutAudit(ctx context.Context, id uuid.UUID) error {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `DELETE FROM upstreams WHERE id = $1 AND tenant_id = $2`
		args = []interface{}{id, tenantID}
	} else {
		query = `DELETE FROM upstreams WHERE id = $1`
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

func (r *PostgresUpstreamRepository) deleteWithAudit(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	oldUpstream, err := r.getByID(ctx, tx, id)
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
		query = `DELETE FROM upstreams WHERE id = $1 AND tenant_id = $2`
		args = []interface{}{id, tenantID}
	} else {
		query = `DELETE FROM upstreams WHERE id = $1`
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

	if err := auditRepo.logDeleteWithTx(ctx, tx, auditCtx, "upstream", id, oldUpstream); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}
