package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/domain/audit"
	"github.com/taichirain/portkey/internal/platform/postgres"
)

type AuditRepository interface {
	Repository
	LogCreate(ctx context.Context, auditCtx *AuditContext, resourceType string, resourceID uuid.UUID, newVal interface{}) error
	LogUpdate(ctx context.Context, auditCtx *AuditContext, resourceType string, resourceID uuid.UUID, oldVal, newVal interface{}) error
	LogDelete(ctx context.Context, auditCtx *AuditContext, resourceType string, resourceID uuid.UUID, oldVal interface{}) error
	List(ctx context.Context, pagination *Pagination) (*PageResult[*audit.AuditLog], error)
	GetByResourceID(ctx context.Context, resourceType string, resourceID uuid.UUID) ([]*audit.AuditLog, error)
}

type PostgresAuditRepository struct {
	db *postgres.DB
}

func NewPostgresAuditRepository(db *postgres.DB) *PostgresAuditRepository {
	return &PostgresAuditRepository{db: db}
}

func (r *PostgresAuditRepository) BeginTx(ctx context.Context) (Tx, error) {
	return r.db.BeginTx(ctx)
}

func (r *PostgresAuditRepository) LogCreate(ctx context.Context, auditCtx *AuditContext, resourceType string, resourceID uuid.UUID, newVal interface{}) error {
	log := audit.New(audit.ActionCreate, audit.ResourceType(resourceType), &resourceID, auditCtx.AdminID)
	if auditCtx != nil {
		log.ClientIP = auditCtx.ClientIP
		log.UserAgent = auditCtx.UserAgent
		log.RequestID = auditCtx.RequestID
	}
	if err := log.SetNewValue(newVal); err != nil {
		return err
	}
	return r.insertLog(ctx, r.db, log)
}

func (r *PostgresAuditRepository) logCreateWithTx(ctx context.Context, tx Tx, auditCtx *AuditContext, resourceType string, resourceID uuid.UUID, newVal interface{}) error {
	log := audit.New(audit.ActionCreate, audit.ResourceType(resourceType), &resourceID, auditCtx.AdminID)
	if auditCtx != nil {
		log.ClientIP = auditCtx.ClientIP
		log.UserAgent = auditCtx.UserAgent
		log.RequestID = auditCtx.RequestID
	}
	if err := log.SetNewValue(newVal); err != nil {
		return err
	}
	return r.insertLog(ctx, tx, log)
}

func (r *PostgresAuditRepository) LogUpdate(ctx context.Context, auditCtx *AuditContext, resourceType string, resourceID uuid.UUID, oldVal, newVal interface{}) error {
	log := audit.New(audit.ActionUpdate, audit.ResourceType(resourceType), &resourceID, auditCtx.AdminID)
	if auditCtx != nil {
		log.ClientIP = auditCtx.ClientIP
		log.UserAgent = auditCtx.UserAgent
		log.RequestID = auditCtx.RequestID
	}
	if err := log.SetOldValue(oldVal); err != nil {
		return err
	}
	if err := log.SetNewValue(newVal); err != nil {
		return err
	}
	return r.insertLog(ctx, r.db, log)
}

func (r *PostgresAuditRepository) logUpdateWithTx(ctx context.Context, tx Tx, auditCtx *AuditContext, resourceType string, resourceID uuid.UUID, oldVal, newVal interface{}) error {
	log := audit.New(audit.ActionUpdate, audit.ResourceType(resourceType), &resourceID, auditCtx.AdminID)
	if auditCtx != nil {
		log.ClientIP = auditCtx.ClientIP
		log.UserAgent = auditCtx.UserAgent
		log.RequestID = auditCtx.RequestID
	}
	if err := log.SetOldValue(oldVal); err != nil {
		return err
	}
	if err := log.SetNewValue(newVal); err != nil {
		return err
	}
	return r.insertLog(ctx, tx, log)
}

func (r *PostgresAuditRepository) LogDelete(ctx context.Context, auditCtx *AuditContext, resourceType string, resourceID uuid.UUID, oldVal interface{}) error {
	log := audit.New(audit.ActionDelete, audit.ResourceType(resourceType), &resourceID, auditCtx.AdminID)
	if auditCtx != nil {
		log.ClientIP = auditCtx.ClientIP
		log.UserAgent = auditCtx.UserAgent
		log.RequestID = auditCtx.RequestID
	}
	if err := log.SetOldValue(oldVal); err != nil {
		return err
	}
	return r.insertLog(ctx, r.db, log)
}

func (r *PostgresAuditRepository) logDeleteWithTx(ctx context.Context, tx Tx, auditCtx *AuditContext, resourceType string, resourceID uuid.UUID, oldVal interface{}) error {
	log := audit.New(audit.ActionDelete, audit.ResourceType(resourceType), &resourceID, auditCtx.AdminID)
	if auditCtx != nil {
		log.ClientIP = auditCtx.ClientIP
		log.UserAgent = auditCtx.UserAgent
		log.RequestID = auditCtx.RequestID
	}
	if err := log.SetOldValue(oldVal); err != nil {
		return err
	}
	return r.insertLog(ctx, tx, log)
}

func (r *PostgresAuditRepository) insertLog(ctx context.Context, exec Executor, log *audit.AuditLog) error {
	oldValueJSON, err := log.OldValueJSON()
	if err != nil {
		return err
	}

	newValueJSON, err := log.NewValueJSON()
	if err != nil {
		return err
	}

	if log.TenantID == uuid.Nil {
		if tenantID, ok := GetTenantIDFromContext(ctx); ok && tenantID != uuid.Nil {
			log.TenantID = tenantID
		}
	}

	query := `
		INSERT INTO audit_logs (id, tenant_id, admin_id, action, resource_type, resource_id, old_value, new_value, client_ip, user_agent, request_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`

	_, err = exec.ExecContext(ctx, query,
		log.ID,
		log.TenantID,
		log.AdminID,
		string(log.Action),
		string(log.ResourceType),
		log.ResourceID,
		func() interface{} { if oldValueJSON == nil { return nil }; return string(oldValueJSON) }(),
		func() interface{} { if newValueJSON == nil { return nil }; return string(newValueJSON) }(),
		log.ClientIP,
		log.UserAgent,
		log.RequestID,
		log.CreatedAt,
	)

	return err
}

func (r *PostgresAuditRepository) List(ctx context.Context, pagination *Pagination) (*PageResult[*audit.AuditLog], error) {
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
		countQuery = `SELECT COUNT(*) FROM audit_logs WHERE tenant_id = $1`
		countArgs = []interface{}{tenantID}

		listQuery = `
			SELECT id, tenant_id, admin_id, action, resource_type, resource_id, old_value, new_value, client_ip, user_agent, request_id, created_at
			FROM audit_logs
			WHERE tenant_id = $1
			ORDER BY created_at DESC
			LIMIT $2 OFFSET $3
		`
		listArgs = []interface{}{tenantID, pagination.PageSize, offset}
	} else {
		countQuery = `SELECT COUNT(*) FROM audit_logs`
		countArgs = []interface{}{}

		listQuery = `
			SELECT id, tenant_id, admin_id, action, resource_type, resource_id, old_value, new_value, client_ip, user_agent, request_id, created_at
			FROM audit_logs
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

	items := make([]*audit.AuditLog, 0)
	for rows.Next() {
		var log audit.AuditLog

		if err := rows.Scan(
			&log.ID,
			&log.TenantID,
			&log.AdminID,
			&log.Action,
			&log.ResourceType,
			&log.ResourceID,
			&log.OldValue,
			&log.NewValue,
			&log.ClientIP,
			&log.UserAgent,
			&log.RequestID,
			&log.CreatedAt,
		); err != nil {
			return nil, err
		}

		items = append(items, &log)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return NewPageResult(items, total, pagination.Page, pagination.PageSize), nil
}

func (r *PostgresAuditRepository) GetByResourceID(ctx context.Context, resourceType string, resourceID uuid.UUID) ([]*audit.AuditLog, error) {
	tenantID, hasTenant := GetTenantIDFromContext(ctx)

	var query string
	var args []interface{}

	if hasTenant && tenantID != uuid.Nil {
		query = `
			SELECT id, tenant_id, admin_id, action, resource_type, resource_id, old_value, new_value, client_ip, user_agent, request_id, created_at
			FROM audit_logs
			WHERE resource_type = $1 AND resource_id = $2 AND tenant_id = $3
			ORDER BY created_at DESC
		`
		args = []interface{}{resourceType, resourceID, tenantID}
	} else {
		query = `
			SELECT id, tenant_id, admin_id, action, resource_type, resource_id, old_value, new_value, client_ip, user_agent, request_id, created_at
			FROM audit_logs
			WHERE resource_type = $1 AND resource_id = $2
			ORDER BY created_at DESC
		`
		args = []interface{}{resourceType, resourceID}
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]*audit.AuditLog, 0)
	for rows.Next() {
		var log audit.AuditLog

		if err := rows.Scan(
			&log.ID,
			&log.TenantID,
			&log.AdminID,
			&log.Action,
			&log.ResourceType,
			&log.ResourceID,
			&log.OldValue,
			&log.NewValue,
			&log.ClientIP,
			&log.UserAgent,
			&log.RequestID,
			&log.CreatedAt,
		); err != nil {
			return nil, err
		}

		items = append(items, &log)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}
