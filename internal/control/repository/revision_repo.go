package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/domain/revision"
	"github.com/taichirain/portkey/internal/platform/postgres"
)

var (
	ErrRevisionPublishConflict = errors.New("revision publish conflict due to concurrent operation")
)

type RevisionRepository interface {
	Repository
	Create(ctx context.Context, r *revision.ConfigRevision, auditCtx *AuditContext) error
	GetByID(ctx context.Context, id uuid.UUID) (*revision.ConfigRevision, error)
	GetByVersion(ctx context.Context, version string) (*revision.ConfigRevision, error)
	GetActive(ctx context.Context) (*revision.ConfigRevision, error)
	List(ctx context.Context, pagination *Pagination) (*PageResult[*revision.ConfigRevision], error)
	Activate(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error
	ActivateWithLock(ctx context.Context, id uuid.UUID, auditCtx *AuditContext, lockRepo DistributedLockRepository, holderID uuid.UUID) error
	Update(ctx context.Context, r *revision.ConfigRevision, auditCtx *AuditContext) error
	Delete(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error
}

type PostgresRevisionRepository struct {
	db         *postgres.DB
	auditRepo  AuditRepository
	instanceID uuid.UUID
}

func NewPostgresRevisionRepository(db *postgres.DB, auditRepo AuditRepository) *PostgresRevisionRepository {
	return &PostgresRevisionRepository{
		db:         db,
		auditRepo:  auditRepo,
		instanceID: uuid.New(),
	}
}

func NewPostgresRevisionRepositoryWithInstanceID(db *postgres.DB, auditRepo AuditRepository, instanceID uuid.UUID) *PostgresRevisionRepository {
	return &PostgresRevisionRepository{
		db:         db,
		auditRepo:  auditRepo,
		instanceID: instanceID,
	}
}

func (r *PostgresRevisionRepository) BeginTx(ctx context.Context) (Tx, error) {
	return r.db.BeginTx(ctx)
}

func (r *PostgresRevisionRepository) Create(ctx context.Context, rev *revision.ConfigRevision, auditCtx *AuditContext) error {
	if err := rev.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	if rev.TenantID == uuid.Nil {
		if tenantID, ok := GetTenantIDFromContext(ctx); ok {
			rev.TenantID = tenantID
		}
	}

	snapshotJSON, err := json.Marshal(rev.Snapshot)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO config_revisions (id, tenant_id, version, description, snapshot, is_active, created_by, created_at, published_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err = r.db.ExecContext(ctx, query,
		rev.ID,
		nullUUID(rev.TenantID),
		rev.Version,
		nullString(rev.Description),
		snapshotJSON,
		rev.IsActive,
		func() interface{} { if rev.CreatedBy == nil { return nil }; return *rev.CreatedBy }(),
		rev.CreatedAt,
		rev.PublishedAt,
	)

	if err != nil {
		return err
	}

	if auditCtx != nil {
		if err := r.auditRepo.LogCreate(ctx, auditCtx, "revision", rev.ID, rev); err != nil {
			return err
		}
	}

	return nil
}

func (r *PostgresRevisionRepository) GetByID(ctx context.Context, id uuid.UUID) (*revision.ConfigRevision, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, version, description, snapshot, is_active, created_by, created_at, published_at
			FROM config_revisions
			WHERE id = $1 AND tenant_id = $2
		`
		args = []interface{}{id, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, version, description, snapshot, is_active, created_by, created_at, published_at
			FROM config_revisions
			WHERE id = $1
		`
		args = []interface{}{id}
	}

	var rev revision.ConfigRevision
	var description sql.NullString
	var snapshotJSON []byte

	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&rev.ID,
		&rev.TenantID,
		&rev.Version,
		&description,
		&snapshotJSON,
		&rev.IsActive,
		&rev.CreatedBy,
		&rev.CreatedAt,
		&rev.PublishedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	rev.Description = description.String

	if len(snapshotJSON) > 0 {
		var snapshot map[string]interface{}
		if err := json.Unmarshal(snapshotJSON, &snapshot); err == nil {
			rev.Snapshot = snapshot
		}
	}

	return &rev, nil
}

func (r *PostgresRevisionRepository) GetByVersion(ctx context.Context, version string) (*revision.ConfigRevision, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, version, description, snapshot, is_active, created_by, created_at, published_at
			FROM config_revisions
			WHERE version = $1 AND tenant_id = $2
		`
		args = []interface{}{version, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, version, description, snapshot, is_active, created_by, created_at, published_at
			FROM config_revisions
			WHERE version = $1
		`
		args = []interface{}{version}
	}

	var rev revision.ConfigRevision
	var description sql.NullString
	var snapshotJSON []byte

	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&rev.ID,
		&rev.TenantID,
		&rev.Version,
		&description,
		&snapshotJSON,
		&rev.IsActive,
		&rev.CreatedBy,
		&rev.CreatedAt,
		&rev.PublishedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	rev.Description = description.String

	if len(snapshotJSON) > 0 {
		var snapshot map[string]interface{}
		if err := json.Unmarshal(snapshotJSON, &snapshot); err == nil {
			rev.Snapshot = snapshot
		}
	}

	return &rev, nil
}

func (r *PostgresRevisionRepository) GetActive(ctx context.Context) (*revision.ConfigRevision, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, version, description, snapshot, is_active, created_by, created_at, published_at
			FROM config_revisions
			WHERE is_active = TRUE AND tenant_id = $1
			LIMIT 1
		`
		args = []interface{}{tenantID}
	} else {
		query = `
			SELECT id, tenant_id, version, description, snapshot, is_active, created_by, created_at, published_at
			FROM config_revisions
			WHERE is_active = TRUE
			LIMIT 1
		`
		args = []interface{}{}
	}

	var rev revision.ConfigRevision
	var description sql.NullString
	var snapshotJSON []byte

	err := r.db.QueryRowContext(ctx, query, args...).Scan(
		&rev.ID,
		&rev.TenantID,
		&rev.Version,
		&description,
		&snapshotJSON,
		&rev.IsActive,
		&rev.CreatedBy,
		&rev.CreatedAt,
		&rev.PublishedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	rev.Description = description.String

	if len(snapshotJSON) > 0 {
		var snapshot map[string]interface{}
		if err := json.Unmarshal(snapshotJSON, &snapshot); err == nil {
			rev.Snapshot = snapshot
		}
	}

	return &rev, nil
}

func (r *PostgresRevisionRepository) List(ctx context.Context, pagination *Pagination) (*PageResult[*revision.ConfigRevision], error) {
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
		countQuery = `SELECT COUNT(*) FROM config_revisions WHERE tenant_id = $1`
		countArgs = []interface{}{tenantID}

		listQuery = `
			SELECT id, tenant_id, version, description, snapshot, is_active, created_by, created_at, published_at
			FROM config_revisions
			WHERE tenant_id = $1
			ORDER BY created_at DESC
			LIMIT $2 OFFSET $3
		`
		listArgs = []interface{}{tenantID, pagination.PageSize, offset}
	} else {
		countQuery = `SELECT COUNT(*) FROM config_revisions`
		countArgs = []interface{}{}

		listQuery = `
			SELECT id, tenant_id, version, description, snapshot, is_active, created_by, created_at, published_at
			FROM config_revisions
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

	items := make([]*revision.ConfigRevision, 0)
	for rows.Next() {
		var rev revision.ConfigRevision
		var description sql.NullString
		var snapshotJSON []byte

		if err := rows.Scan(
			&rev.ID,
			&rev.TenantID,
			&rev.Version,
			&description,
			&snapshotJSON,
			&rev.IsActive,
			&rev.CreatedBy,
			&rev.CreatedAt,
			&rev.PublishedAt,
		); err != nil {
			return nil, err
		}

		rev.Description = description.String

		if len(snapshotJSON) > 0 {
			var snapshot map[string]interface{}
			if err := json.Unmarshal(snapshotJSON, &snapshot); err == nil {
				rev.Snapshot = snapshot
			}
		}

		items = append(items, &rev)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return NewPageResult(items, total, pagination.Page, pagination.PageSize), nil
}

func (r *PostgresRevisionRepository) Activate(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	tx, err := r.db.BeginTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if hasTenant && tenantID != uuid.Nil {
		if err := r.activateInTx(ctx, tx, id, tenantID); err != nil {
			return err
		}
	} else {
		if err := r.activateInTx(ctx, tx, id, uuid.Nil); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		if isUniqueViolation(err) {
			return ErrRevisionPublishConflict
		}
		return err
	}

	if auditCtx != nil {
		rev, err := r.GetByID(ctx, id)
		if err == nil {
			_ = r.auditRepo.LogUpdate(ctx, auditCtx, "revision", id, nil, rev)
		}
	}

	return nil
}

func (r *PostgresRevisionRepository) activateInTx(ctx context.Context, tx Tx, id uuid.UUID, tenantID uuid.UUID) error {
	var err error

	if tenantID != uuid.Nil {
		_, err = tx.ExecContext(ctx, `
			UPDATE config_revisions 
			SET is_active = FALSE, updated_at = NOW() 
			WHERE is_active = TRUE AND tenant_id = $1
		`, tenantID)
	} else {
		_, err = tx.ExecContext(ctx, `
			UPDATE config_revisions 
			SET is_active = FALSE, updated_at = NOW() 
			WHERE is_active = TRUE AND tenant_id IS NULL
		`)
	}
	if err != nil {
		return err
	}

	var result sql.Result
	if tenantID != uuid.Nil {
		result, err = tx.ExecContext(ctx, `
			UPDATE config_revisions 
			SET is_active = TRUE, published_at = NOW(), updated_at = NOW() 
			WHERE id = $1 AND tenant_id = $2
		`, id, tenantID)
	} else {
		result, err = tx.ExecContext(ctx, `
			UPDATE config_revisions 
			SET is_active = TRUE, published_at = NOW(), updated_at = NOW() 
			WHERE id = $1 AND tenant_id IS NULL
		`, id)
	}
	if err != nil {
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}

func (r *PostgresRevisionRepository) ActivateWithLock(ctx context.Context, id uuid.UUID, auditCtx *AuditContext, lockRepo DistributedLockRepository, holderID uuid.UUID) error {
	rev, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if rev.IsActive {
		return nil
	}

	tenantID := rev.TenantID
	lockKey := GetRevisionPublishLockKey(tenantID)

	lockManager := NewLockManager(lockRepo, holderID)

	lockTTL := 30 * time.Second
	waitForLock := false

	var lockErr error
	for i := 0; i < 3; i++ {
		err = lockManager.WithLock(ctx, lockKey, lockTTL, func(lockCtx context.Context) error {
			return r.Activate(lockCtx, id, auditCtx)
		})

		if err == nil {
			return nil
		}

		if errors.Is(err, ErrLockAlreadyHeld) {
			waitForLock = true
			lockErr = err
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}

		if errors.Is(err, ErrRevisionPublishConflict) {
			lockErr = err
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(50 * time.Millisecond):
			}
			continue
		}

		return err
	}

	if waitForLock {
		return fmt.Errorf("failed to acquire publish lock for tenant %s: %w", tenantID, lockErr)
	}

	return fmt.Errorf("failed to activate revision after retries: %w", lockErr)
}

func (r *PostgresRevisionRepository) Update(ctx context.Context, rev *revision.ConfigRevision, auditCtx *AuditContext) error {
	if err := rev.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}

	var oldRevision *revision.ConfigRevision
	if auditCtx != nil {
		var err error
		oldRevision, err = r.GetByID(ctx, rev.ID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
	}

	snapshotJSON, err := json.Marshal(rev.Snapshot)
	if err != nil {
		return err
	}

	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			UPDATE config_revisions
			SET version = $2, description = $3, snapshot = $4, is_active = $5, published_at = $6, updated_at = NOW()
			WHERE id = $1 AND tenant_id = $7
		`
		args = []interface{}{
			rev.ID,
			rev.Version,
			nullString(rev.Description),
			snapshotJSON,
			rev.IsActive,
			rev.PublishedAt,
			tenantID,
		}
	} else {
		query = `
			UPDATE config_revisions
			SET version = $2, description = $3, snapshot = $4, is_active = $5, published_at = $6, updated_at = NOW()
			WHERE id = $1
		`
		args = []interface{}{
			rev.ID,
			rev.Version,
			nullString(rev.Description),
			snapshotJSON,
			rev.IsActive,
			rev.PublishedAt,
		}
	}

	result, err := r.db.ExecContext(ctx, query, args...)

	if err != nil {
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrNotFound
	}

	if auditCtx != nil && oldRevision != nil {
		if err := r.auditRepo.LogUpdate(ctx, auditCtx, "revision", rev.ID, oldRevision, rev); err != nil {
			return err
		}
	}

	return nil
}

func (r *PostgresRevisionRepository) Delete(ctx context.Context, id uuid.UUID, auditCtx *AuditContext) error {
	var oldRevision *revision.ConfigRevision
	if auditCtx != nil {
		var err error
		oldRevision, err = r.GetByID(ctx, id)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
	}

	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `DELETE FROM config_revisions WHERE id = $1 AND tenant_id = $2`
		args = []interface{}{id, tenantID}
	} else {
		query = `DELETE FROM config_revisions WHERE id = $1`
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

	if auditCtx != nil && oldRevision != nil {
		if err := r.auditRepo.LogDelete(ctx, auditCtx, "revision", id, oldRevision); err != nil {
			return err
		}
	}

	return nil
}
