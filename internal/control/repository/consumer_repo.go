package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/taichirain/portkey/internal/domain/consumer"
	"github.com/taichirain/portkey/internal/platform/postgres"
)

type ConsumerRepository interface {
	Repository
	Create(ctx context.Context, c *consumer.Consumer, auditCtx *AuditContext) error
	GetByID(ctx context.Context, id uuid.UUID) (*consumer.Consumer, error)
	GetByUsername(ctx context.Context, username string) (*consumer.Consumer, error)
	GetByCustomID(ctx context.Context, customID string) (*consumer.Consumer, error)
	List(ctx context.Context, pagination *Pagination) (*PageResult[*consumer.Consumer], error)
	Update(ctx context.Context, c *consumer.Consumer, auditCtx *AuditContext) error
	Delete(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error
}

type PostgresConsumerRepository struct {
	db        *postgres.DB
	auditRepo AuditRepository
}

func NewPostgresConsumerRepository(db *postgres.DB, auditRepo AuditRepository) *PostgresConsumerRepository {
	return &PostgresConsumerRepository{
		db:        db,
		auditRepo: auditRepo,
	}
}

func (r *PostgresConsumerRepository) BeginTx(ctx context.Context) (Tx, error) {
	return r.db.BeginTx(ctx)
}

func (r *PostgresConsumerRepository) Create(ctx context.Context, c *consumer.Consumer, auditCtx *AuditContext) error {
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

func (r *PostgresConsumerRepository) createWithoutAudit(ctx context.Context, c *consumer.Consumer) error {
	query := `
		INSERT INTO consumers (id, tenant_id, username, custom_id, tags, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err := r.db.ExecContext(ctx, query,
		c.ID,
		c.TenantID,
		nullString(c.Username),
		nullString(c.CustomID),
		pq.Array(c.Tags),
		c.CreatedAt,
		c.UpdatedAt,
	)

	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}

	return nil
}

func (r *PostgresConsumerRepository) createWithAudit(ctx context.Context, c *consumer.Consumer, auditCtx *AuditContext) error {
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
		INSERT INTO consumers (id, tenant_id, username, custom_id, tags, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err = tx.ExecContext(ctx, query,
		c.ID,
		c.TenantID,
		nullString(c.Username),
		nullString(c.CustomID),
		pq.Array(c.Tags),
		c.CreatedAt,
		c.UpdatedAt,
	)

	if err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyExists
		}
		return err
	}

	if err := auditRepo.logCreateWithTx(ctx, tx, auditCtx, "consumer", c.ID, c); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (r *PostgresConsumerRepository) GetByID(ctx context.Context, id uuid.UUID) (*consumer.Consumer, error) {
	return r.getByID(ctx, r.db, id)
}

func (r *PostgresConsumerRepository) getByID(ctx context.Context, exec Executor, id uuid.UUID) (*consumer.Consumer, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, username, custom_id, tags, created_at, updated_at
			FROM consumers
			WHERE id = $1 AND tenant_id = $2
		`
		args = []interface{}{id, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, username, custom_id, tags, created_at, updated_at
			FROM consumers
			WHERE id = $1
		`
		args = []interface{}{id}
	}

	var c consumer.Consumer
	var username, customID sql.NullString

	err := exec.QueryRowContext(ctx, query, args...).Scan(
		&c.ID,
		&c.TenantID,
		&username,
		&customID,
		pq.Array(&c.Tags),
		&c.CreatedAt,
		&c.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	c.Username = username.String
	c.CustomID = customID.String

	return &c, nil
}

func (r *PostgresConsumerRepository) GetByUsername(ctx context.Context, username string) (*consumer.Consumer, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, username, custom_id, tags, created_at, updated_at
			FROM consumers
			WHERE username = $1 AND tenant_id = $2
		`
		args = []interface{}{username, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, username, custom_id, tags, created_at, updated_at
			FROM consumers
			WHERE username = $1
		`
		args = []interface{}{username}
	}

	var c consumer.Consumer
	var uname, customID sql.NullString

	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&c.ID,
		&c.TenantID,
		&uname,
		&customID,
		pq.Array(&c.Tags),
		&c.CreatedAt,
		&c.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	c.Username = uname.String
	c.CustomID = customID.String

	return &c, nil
}

func (r *PostgresConsumerRepository) GetByCustomID(ctx context.Context, customID string) (*consumer.Consumer, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, username, custom_id, tags, created_at, updated_at
			FROM consumers
			WHERE custom_id = $1 AND tenant_id = $2
		`
		args = []interface{}{customID, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, username, custom_id, tags, created_at, updated_at
			FROM consumers
			WHERE custom_id = $1
		`
		args = []interface{}{customID}
	}

	var c consumer.Consumer
	var username, cid sql.NullString

	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&c.ID,
		&c.TenantID,
		&username,
		&cid,
		pq.Array(&c.Tags),
		&c.CreatedAt,
		&c.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	c.Username = username.String
	c.CustomID = cid.String

	return &c, nil
}

func (r *PostgresConsumerRepository) List(ctx context.Context, pagination *Pagination) (*PageResult[*consumer.Consumer], error) {
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
		countQuery = `SELECT COUNT(*) FROM consumers WHERE tenant_id = $1`
		countArgs = []interface{}{tenantID}

		listQuery = `
			SELECT id, tenant_id, username, custom_id, tags, created_at, updated_at
			FROM consumers
			WHERE tenant_id = $1
			ORDER BY created_at DESC
			LIMIT $2 OFFSET $3
		`
		listArgs = []interface{}{tenantID, pagination.PageSize, offset}
	} else {
		countQuery = `SELECT COUNT(*) FROM consumers`
		countArgs = []interface{}{}

		listQuery = `
			SELECT id, tenant_id, username, custom_id, tags, created_at, updated_at
			FROM consumers
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

	items := make([]*consumer.Consumer, 0)
	for rows.Next() {
		var c consumer.Consumer
		var username, customID sql.NullString

		if err := rows.Scan(
			&c.ID,
			&c.TenantID,
			&username,
			&customID,
			pq.Array(&c.Tags),
			&c.CreatedAt,
			&c.UpdatedAt,
		); err != nil {
			return nil, err
		}

		c.Username = username.String
		c.CustomID = customID.String
		items = append(items, &c)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return NewPageResult(items, total, pagination.Page, pagination.PageSize), nil
}

func (r *PostgresConsumerRepository) Update(ctx context.Context, c *consumer.Consumer, auditCtx *AuditContext) error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	if auditCtx == nil {
		return r.updateWithoutAudit(ctx, c)
	}

	return r.updateWithAudit(ctx, c, auditCtx)
}

func (r *PostgresConsumerRepository) updateWithoutAudit(ctx context.Context, c *consumer.Consumer) error {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			UPDATE consumers
			SET username = $2, custom_id = $3, tags = $4, updated_at = NOW()
			WHERE id = $1 AND tenant_id = $5
		`
		args = []interface{}{
			c.ID,
			nullString(c.Username),
			nullString(c.CustomID),
			pq.Array(c.Tags),
			tenantID,
		}
	} else {
		query = `
			UPDATE consumers
			SET username = $2, custom_id = $3, tags = $4, updated_at = NOW()
			WHERE id = $1
		`
		args = []interface{}{
			c.ID,
			nullString(c.Username),
			nullString(c.CustomID),
			pq.Array(c.Tags),
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

func (r *PostgresConsumerRepository) updateWithAudit(ctx context.Context, c *consumer.Consumer, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	oldConsumer, err := r.getByID(ctx, tx, c.ID)
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
			UPDATE consumers
			SET username = $2, custom_id = $3, tags = $4, updated_at = NOW()
			WHERE id = $1 AND tenant_id = $5
		`
		args = []interface{}{
			c.ID,
			nullString(c.Username),
			nullString(c.CustomID),
			pq.Array(c.Tags),
			tenantID,
		}
	} else {
		query = `
			UPDATE consumers
			SET username = $2, custom_id = $3, tags = $4, updated_at = NOW()
			WHERE id = $1
		`
		args = []interface{}{
			c.ID,
			nullString(c.Username),
			nullString(c.CustomID),
			pq.Array(c.Tags),
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

	if err := auditRepo.logUpdateWithTx(ctx, tx, auditCtx, "consumer", c.ID, oldConsumer, c); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (r *PostgresConsumerRepository) Delete(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error {
	if auditCtx == nil {
		return r.deleteWithoutAudit(ctx, id)
	}

	return r.deleteWithAudit(ctx, id, auditCtx)
}

func (r *PostgresConsumerRepository) deleteWithoutAudit(ctx context.Context, id uuid.UUID) error {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `DELETE FROM consumers WHERE id = $1 AND tenant_id = $2`
		args = []interface{}{id, tenantID}
	} else {
		query = `DELETE FROM consumers WHERE id = $1`
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

func (r *PostgresConsumerRepository) deleteWithAudit(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error {
	auditRepo, ok := r.auditRepo.(*PostgresAuditRepository)
	if !ok {
		return errors.New("audit repository does not support transactions")
	}

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	oldConsumer, err := r.getByID(ctx, tx, id)
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
		query = `DELETE FROM consumers WHERE id = $1 AND tenant_id = $2`
		args = []interface{}{id, tenantID}
	} else {
		query = `DELETE FROM consumers WHERE id = $1`
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

	if err := auditRepo.logDeleteWithTx(ctx, tx, auditCtx, "consumer", id, oldConsumer); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}
