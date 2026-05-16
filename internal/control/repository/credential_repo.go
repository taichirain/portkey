package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/taichirain/portkey/internal/domain/credential"
	"github.com/taichirain/portkey/internal/platform/postgres"
)

type CredentialRepository interface {
	Repository
	Create(ctx context.Context, c *credential.Credential, auditCtx *AuditContext) error
	GetByID(ctx context.Context, id uuid.UUID) (*credential.Credential, error)
	GetByKey(ctx context.Context, key string) (*credential.Credential, error)
	ListByConsumerID(ctx context.Context, consumerID uuid.UUID) ([]*credential.Credential, error)
	ListByType(ctx context.Context, credType credential.Type) ([]*credential.Credential, error)
	List(ctx context.Context, pagination *Pagination) (*PageResult[*credential.Credential], error)
	Update(ctx context.Context, c *credential.Credential, auditCtx *AuditContext) error
	Delete(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error
}

type PostgresCredentialRepository struct {
	db        *postgres.DB
	auditRepo AuditRepository
}

func NewPostgresCredentialRepository(db *postgres.DB, auditRepo AuditRepository) *PostgresCredentialRepository {
	return &PostgresCredentialRepository{
		db:        db,
		auditRepo: auditRepo,
	}
}

func (r *PostgresCredentialRepository) BeginTx(ctx context.Context) (Tx, error) {
	return r.db.BeginTx(ctx)
}

func (r *PostgresCredentialRepository) Create(ctx context.Context, c *credential.Credential, auditCtx *AuditContext) error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	if c.TenantID == uuid.Nil {
		if tenantID, ok := GetTenantIDFromContext(ctx); ok {
			c.TenantID = tenantID
		}
	}

	if auditCtx == nil {
		return r.createWithoutAudit(ctx, c)
	}

	return r.createWithAudit(ctx, c, auditCtx)
}

func (r *PostgresCredentialRepository) createWithoutAudit(ctx context.Context, c *credential.Credential) error {
	claimsJSON, err := json.Marshal(c.Claims)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO credentials (id, tenant_id, consumer_id, type, key, secret, algorithm, claims, tags, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`

	_, err = r.db.ExecContext(ctx, query,
		c.ID,
		c.TenantID,
		c.ConsumerID,
		string(c.Type),
		c.Key,
		nullString(c.Secret),
		nullString(c.Algorithm),
		claimsJSON,
		pq.Array(c.Tags),
		c.Enabled,
		c.CreatedAt,
		c.UpdatedAt,
	)

	if err != nil {
		if isForeignKeyViolation(err) {
			return fmt.Errorf("%w: consumer not found", ErrInvalidInput)
		}
		return err
	}

	return nil
}

func (r *PostgresCredentialRepository) createWithAudit(ctx context.Context, c *credential.Credential, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	claimsJSON, err := json.Marshal(c.Claims)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO credentials (id, tenant_id, consumer_id, type, key, secret, algorithm, claims, tags, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`

	_, err = tx.ExecContext(ctx, query,
		c.ID,
		c.TenantID,
		c.ConsumerID,
		string(c.Type),
		c.Key,
		nullString(c.Secret),
		nullString(c.Algorithm),
		claimsJSON,
		pq.Array(c.Tags),
		c.Enabled,
		c.CreatedAt,
		c.UpdatedAt,
	)

	if err != nil {
		if isForeignKeyViolation(err) {
			return fmt.Errorf("%w: consumer not found", ErrInvalidInput)
		}
		return err
	}

	if err := auditRepo.logCreateWithTx(ctx, tx, auditCtx, "credential", c.ID, c); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (r *PostgresCredentialRepository) GetByID(ctx context.Context, id uuid.UUID) (*credential.Credential, error) {
	return r.getByID(ctx, r.db, id)
}

func (r *PostgresCredentialRepository) getByID(ctx context.Context, exec Executor, id uuid.UUID) (*credential.Credential, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, consumer_id, type, key, secret, algorithm, claims, tags, enabled, created_at, updated_at
			FROM credentials
			WHERE id = $1 AND tenant_id = $2
		`
		args = []interface{}{id, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, consumer_id, type, key, secret, algorithm, claims, tags, enabled, created_at, updated_at
			FROM credentials
			WHERE id = $1
		`
		args = []interface{}{id}
	}

	var c credential.Credential
	var secret, algorithm sql.NullString
	var claimsJSON []byte

	err := exec.QueryRowContext(ctx, query, args...).Scan(
		&c.ID,
		&c.TenantID,
		&c.ConsumerID,
		&c.Type,
		&c.Key,
		&secret,
		&algorithm,
		&claimsJSON,
		pq.Array(&c.Tags),
		&c.Enabled,
		&c.CreatedAt,
		&c.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	c.Secret = secret.String
	c.Algorithm = algorithm.String

	if len(claimsJSON) > 0 {
		var claims map[string]interface{}
		if err := json.Unmarshal(claimsJSON, &claims); err == nil {
			c.Claims = claims
		}
	}

	return &c, nil
}

func (r *PostgresCredentialRepository) GetByKey(ctx context.Context, key string) (*credential.Credential, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, consumer_id, type, key, secret, algorithm, claims, tags, enabled, created_at, updated_at
			FROM credentials
			WHERE key = $1 AND tenant_id = $2
		`
		args = []interface{}{key, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, consumer_id, type, key, secret, algorithm, claims, tags, enabled, created_at, updated_at
			FROM credentials
			WHERE key = $1
		`
		args = []interface{}{key}
	}

	var c credential.Credential
	var secret, algorithm sql.NullString
	var claimsJSON []byte

	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&c.ID,
		&c.TenantID,
		&c.ConsumerID,
		&c.Type,
		&c.Key,
		&secret,
		&algorithm,
		&claimsJSON,
		pq.Array(&c.Tags),
		&c.Enabled,
		&c.CreatedAt,
		&c.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	c.Secret = secret.String
	c.Algorithm = algorithm.String

	if len(claimsJSON) > 0 {
		var claims map[string]interface{}
		if err := json.Unmarshal(claimsJSON, &claims); err == nil {
			c.Claims = claims
		}
	}

	return &c, nil
}

func (r *PostgresCredentialRepository) ListByConsumerID(ctx context.Context, consumerID uuid.UUID) ([]*credential.Credential, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, consumer_id, type, key, secret, algorithm, claims, tags, enabled, created_at, updated_at
			FROM credentials
			WHERE consumer_id = $1 AND tenant_id = $2
			ORDER BY created_at DESC
		`
		args = []interface{}{consumerID, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, consumer_id, type, key, secret, algorithm, claims, tags, enabled, created_at, updated_at
			FROM credentials
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

	items := make([]*credential.Credential, 0)
	for rows.Next() {
		var c credential.Credential
		var secret, algorithm sql.NullString
		var claimsJSON []byte

		if err := rows.Scan(
			&c.ID,
			&c.TenantID,
			&c.ConsumerID,
			&c.Type,
			&c.Key,
			&secret,
			&algorithm,
			&claimsJSON,
			pq.Array(&c.Tags),
			&c.Enabled,
			&c.CreatedAt,
			&c.UpdatedAt,
		); err != nil {
			return nil, err
		}

		c.Secret = secret.String
		c.Algorithm = algorithm.String

		if len(claimsJSON) > 0 {
			var claims map[string]interface{}
			if err := json.Unmarshal(claimsJSON, &claims); err == nil {
				c.Claims = claims
			}
		}

		items = append(items, &c)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (r *PostgresCredentialRepository) ListByType(ctx context.Context, credType credential.Type) ([]*credential.Credential, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, consumer_id, type, key, secret, algorithm, claims, tags, enabled, created_at, updated_at
			FROM credentials
			WHERE type = $1 AND tenant_id = $2
			ORDER BY created_at DESC
		`
		args = []interface{}{credType, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, consumer_id, type, key, secret, algorithm, claims, tags, enabled, created_at, updated_at
			FROM credentials
			WHERE type = $1
			ORDER BY created_at DESC
		`
		args = []interface{}{credType}
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*credential.Credential, 0)
	for rows.Next() {
		var c credential.Credential
		var secret, algorithm sql.NullString
		var claimsJSON []byte

		if err := rows.Scan(
			&c.ID,
			&c.TenantID,
			&c.ConsumerID,
			&c.Type,
			&c.Key,
			&secret,
			&algorithm,
			&claimsJSON,
			pq.Array(&c.Tags),
			&c.Enabled,
			&c.CreatedAt,
			&c.UpdatedAt,
		); err != nil {
			return nil, err
		}

		c.Secret = secret.String
		c.Algorithm = algorithm.String

		if len(claimsJSON) > 0 {
			var claims map[string]interface{}
			if err := json.Unmarshal(claimsJSON, &claims); err == nil {
				c.Claims = claims
			}
		}

		items = append(items, &c)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (r *PostgresCredentialRepository) List(ctx context.Context, pagination *Pagination) (*PageResult[*credential.Credential], error) {
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
		countQuery = `SELECT COUNT(*) FROM credentials WHERE tenant_id = $1`
		countArgs = []interface{}{tenantID}

		listQuery = `
			SELECT id, tenant_id, consumer_id, type, key, secret, algorithm, claims, tags, enabled, created_at, updated_at
			FROM credentials
			WHERE tenant_id = $1
			ORDER BY created_at DESC
			LIMIT $2 OFFSET $3
		`
		listArgs = []interface{}{tenantID, pagination.PageSize, offset}
	} else {
		countQuery = `SELECT COUNT(*) FROM credentials`
		countArgs = []interface{}{}

		listQuery = `
			SELECT id, tenant_id, consumer_id, type, key, secret, algorithm, claims, tags, enabled, created_at, updated_at
			FROM credentials
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

	items := make([]*credential.Credential, 0)
	for rows.Next() {
		var c credential.Credential
		var secret, algorithm sql.NullString
		var claimsJSON []byte

		if err := rows.Scan(
			&c.ID,
			&c.TenantID,
			&c.ConsumerID,
			&c.Type,
			&c.Key,
			&secret,
			&algorithm,
			&claimsJSON,
			pq.Array(&c.Tags),
			&c.Enabled,
			&c.CreatedAt,
			&c.UpdatedAt,
		); err != nil {
			return nil, err
		}

		c.Secret = secret.String
		c.Algorithm = algorithm.String

		if len(claimsJSON) > 0 {
			var claims map[string]interface{}
			if err := json.Unmarshal(claimsJSON, &claims); err == nil {
				c.Claims = claims
			}
		}

		items = append(items, &c)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return NewPageResult(items, total, pagination.Page, pagination.PageSize), nil
}

func (r *PostgresCredentialRepository) Update(ctx context.Context, c *credential.Credential, auditCtx *AuditContext) error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	if auditCtx == nil {
		return r.updateWithoutAudit(ctx, c)
	}

	return r.updateWithAudit(ctx, c, auditCtx)
}

func (r *PostgresCredentialRepository) updateWithoutAudit(ctx context.Context, c *credential.Credential) error {
	claimsJSON, err := json.Marshal(c.Claims)
	if err != nil {
		return err
	}

	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			UPDATE credentials
			SET consumer_id = $2, type = $3, key = $4, secret = $5, algorithm = $6, claims = $7, tags = $8, enabled = $9, updated_at = NOW()
			WHERE id = $1 AND tenant_id = $10
		`
		args = []interface{}{
			c.ID,
			c.ConsumerID,
			string(c.Type),
			c.Key,
			nullString(c.Secret),
			nullString(c.Algorithm),
			claimsJSON,
			pq.Array(c.Tags),
			c.Enabled,
			tenantID,
		}
	} else {
		query = `
			UPDATE credentials
			SET consumer_id = $2, type = $3, key = $4, secret = $5, algorithm = $6, claims = $7, tags = $8, enabled = $9, updated_at = NOW()
			WHERE id = $1
		`
		args = []interface{}{
			c.ID,
			c.ConsumerID,
			string(c.Type),
			c.Key,
			nullString(c.Secret),
			nullString(c.Algorithm),
			claimsJSON,
			pq.Array(c.Tags),
			c.Enabled,
		}
	}

	result, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		if isForeignKeyViolation(err) {
			return fmt.Errorf("%w: consumer not found", ErrInvalidInput)
		}
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

func (r *PostgresCredentialRepository) updateWithAudit(ctx context.Context, c *credential.Credential, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	oldCredential, err := r.getByID(ctx, tx, c.ID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		return err
	}

	claimsJSON, err := json.Marshal(c.Claims)
	if err != nil {
		return err
	}

	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			UPDATE credentials
			SET consumer_id = $2, type = $3, key = $4, secret = $5, algorithm = $6, claims = $7, tags = $8, enabled = $9, updated_at = NOW()
			WHERE id = $1 AND tenant_id = $10
		`
		args = []interface{}{
			c.ID,
			c.ConsumerID,
			string(c.Type),
			c.Key,
			nullString(c.Secret),
			nullString(c.Algorithm),
			claimsJSON,
			pq.Array(c.Tags),
			c.Enabled,
			tenantID,
		}
	} else {
		query = `
			UPDATE credentials
			SET consumer_id = $2, type = $3, key = $4, secret = $5, algorithm = $6, claims = $7, tags = $8, enabled = $9, updated_at = NOW()
			WHERE id = $1
		`
		args = []interface{}{
			c.ID,
			c.ConsumerID,
			string(c.Type),
			c.Key,
			nullString(c.Secret),
			nullString(c.Algorithm),
			claimsJSON,
			pq.Array(c.Tags),
			c.Enabled,
		}
	}

	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		if isForeignKeyViolation(err) {
			return fmt.Errorf("%w: consumer not found", ErrInvalidInput)
		}
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}

	if err := auditRepo.logUpdateWithTx(ctx, tx, auditCtx, "credential", c.ID, oldCredential, c); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (r *PostgresCredentialRepository) Delete(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error {
	if auditCtx == nil {
		return r.deleteWithoutAudit(ctx, id)
	}

	return r.deleteWithAudit(ctx, id, auditCtx)
}

func (r *PostgresCredentialRepository) deleteWithoutAudit(ctx context.Context, id uuid.UUID) error {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `DELETE FROM credentials WHERE id = $1 AND tenant_id = $2`
		args = []interface{}{id, tenantID}
	} else {
		query = `DELETE FROM credentials WHERE id = $1`
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

func (r *PostgresCredentialRepository) deleteWithAudit(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	oldCredential, err := r.getByID(ctx, tx, id)
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
		query = `DELETE FROM credentials WHERE id = $1 AND tenant_id = $2`
		args = []interface{}{id, tenantID}
	} else {
		query = `DELETE FROM credentials WHERE id = $1`
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

	if err := auditRepo.logDeleteWithTx(ctx, tx, auditCtx, "credential", id, oldCredential); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}
