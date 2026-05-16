package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

var (
	ErrNotFound          = errors.New("resource not found")
	ErrAlreadyExists     = errors.New("resource already exists")
	ErrInvalidInput      = errors.New("invalid input")
	ErrTenantRequired    = errors.New("tenant is required")
	ErrPermissionDenied  = errors.New("permission denied")
	ErrTenantMismatch    = errors.New("tenant mismatch")
)

type tenantContextKey string

const (
	TenantIDKey tenantContextKey = "tenant_id"
)

const (
	pgUniqueViolation      = "23505"
	pgForeignKeyViolation  = "23503"
	pgNotNullViolation     = "23502"
	pgCheckViolation       = "23514"
)

type Pagination struct {
	Page     int
	PageSize int
}

type PageResult[T any] struct {
	Items      []T
	Total      int
	Page       int
	PageSize   int
	TotalPages int
}

type AuditContext struct {
	AdminID   *uuid.UUID
	ClientIP  string
	UserAgent string
	RequestID string
}

func NewPageResult[T any](items []T, total int, page int, pageSize int) *PageResult[T] {
	totalPages := (total + pageSize - 1) / pageSize
	if totalPages == 0 && total > 0 {
		totalPages = 1
	}
	return &PageResult[T]{
		Items:      items,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}
}

type Executor interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

type Tx interface {
	Executor
	Commit() error
	Rollback() error
}

type Repository interface {
	BeginTx(ctx context.Context) (Tx, error)
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullInt(i int) sql.NullInt32 {
	if i == 0 {
		return sql.NullInt32{Valid: false}
	}
	return sql.NullInt32{Int32: int32(i), Valid: true}
}

func nullUUID(id uuid.UUID) interface{} {
	if id == uuid.Nil {
		return nil
	}
	return id
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return pqErr.Code == pgUniqueViolation
	}
	return false
}

func isForeignKeyViolation(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return pqErr.Code == pgForeignKeyViolation
	}
	return false
}

func isNotNullViolation(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return pqErr.Code == pgNotNullViolation
	}
	return false
}

func isCheckViolation(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return pqErr.Code == pgCheckViolation
	}
	return false
}

func getPGErrorCode(err error) string {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return string(pqErr.Code)
	}
	return ""
}

func ContextWithTenantID(ctx context.Context, tenantID uuid.UUID) context.Context {
	return context.WithValue(ctx, TenantIDKey, tenantID)
}

func GetTenantIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	tenantID, ok := ctx.Value(TenantIDKey).(uuid.UUID)
	return tenantID, ok
}

func HasTenantInContext(ctx context.Context) bool {
	_, ok := GetTenantIDFromContext(ctx)
	return ok
}
