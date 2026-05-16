package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/platform/postgres"
)

var (
	ErrLockAlreadyHeld   = errors.New("lock is already held by another holder")
	ErrLockNotHeld       = errors.New("lock is not held by current holder")
	ErrLockNotFound      = errors.New("lock not found")
	ErrLockExpired       = errors.New("lock has expired")
)

const (
	DefaultLockTTL        = 30 * time.Second
	DefaultLockRetryDelay = 100 * time.Millisecond
	DefaultLockMaxRetries = 10
)

type DistributedLock struct {
	ID          uuid.UUID
	LockKey     string
	HolderID    uuid.UUID
	HolderInfo  string
	AcquiredAt  time.Time
	ExpiresAt   time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type DistributedLockRepository interface {
	Repository
	TryLock(ctx context.Context, lockKey string, holderID uuid.UUID, holderInfo string, ttl time.Duration) (*DistributedLock, error)
	Release(ctx context.Context, lockKey string, holderID uuid.UUID) error
	Refresh(ctx context.Context, lockKey string, holderID uuid.UUID, ttl time.Duration) error
	Get(ctx context.Context, lockKey string) (*DistributedLock, error)
	ForceRelease(ctx context.Context, lockKey string) error
	CleanupExpired(ctx context.Context) (int64, error)
}

type PostgresDistributedLockRepository struct {
	db *postgres.DB
}

func NewPostgresDistributedLockRepository(db *postgres.DB) *PostgresDistributedLockRepository {
	return &PostgresDistributedLockRepository{db: db}
}

func (r *PostgresDistributedLockRepository) BeginTx(ctx context.Context) (Tx, error) {
	return r.db.BeginTx(ctx)
}

func (r *PostgresDistributedLockRepository) TryLock(ctx context.Context, lockKey string, holderID uuid.UUID, holderInfo string, ttl time.Duration) (*DistributedLock, error) {
	if ttl <= 0 {
		ttl = DefaultLockTTL
	}

	now := time.Now()
	expiresAt := now.Add(ttl)

	query := `
		INSERT INTO distributed_locks (lock_key, holder_id, holder_info, acquired_at, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (lock_key) DO UPDATE
		SET 
			holder_id = EXCLUDED.holder_id,
			holder_info = EXCLUDED.holder_info,
			acquired_at = EXCLUDED.acquired_at,
			expires_at = EXCLUDED.expires_at,
			updated_at = NOW()
		WHERE distributed_locks.expires_at < NOW()
		RETURNING id, lock_key, holder_id, holder_info, acquired_at, expires_at, created_at, updated_at
	`

	var lock DistributedLock
	err := r.db.QueryRowContext(ctx, query,
		lockKey,
		holderID,
		holderInfo,
		now,
		expiresAt,
	).Scan(
		&lock.ID,
		&lock.LockKey,
		&lock.HolderID,
		&lock.HolderInfo,
		&lock.AcquiredAt,
		&lock.ExpiresAt,
		&lock.CreatedAt,
		&lock.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrLockAlreadyHeld
		}
		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}

	return &lock, nil
}

func (r *PostgresDistributedLockRepository) Release(ctx context.Context, lockKey string, holderID uuid.UUID) error {
	query := `
		DELETE FROM distributed_locks
		WHERE lock_key = $1 AND holder_id = $2
	`

	result, err := r.db.ExecContext(ctx, query, lockKey, holderID)
	if err != nil {
		return fmt.Errorf("failed to release lock: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrLockNotHeld
	}

	return nil
}

func (r *PostgresDistributedLockRepository) Refresh(ctx context.Context, lockKey string, holderID uuid.UUID, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = DefaultLockTTL
	}

	expiresAt := time.Now().Add(ttl)

	query := `
		UPDATE distributed_locks
		SET expires_at = $1, updated_at = NOW()
		WHERE lock_key = $2 AND holder_id = $3 AND expires_at > NOW()
	`

	result, err := r.db.ExecContext(ctx, query, expiresAt, lockKey, holderID)
	if err != nil {
		return fmt.Errorf("failed to refresh lock: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrLockNotHeld
	}

	return nil
}

func (r *PostgresDistributedLockRepository) Get(ctx context.Context, lockKey string) (*DistributedLock, error) {
	query := `
		SELECT id, lock_key, holder_id, holder_info, acquired_at, expires_at, created_at, updated_at
		FROM distributed_locks
		WHERE lock_key = $1
	`

	var lock DistributedLock
	var holderInfo sql.NullString
	err := r.db.QueryRowContext(ctx, query, lockKey).Scan(
		&lock.ID,
		&lock.LockKey,
		&lock.HolderID,
		&holderInfo,
		&lock.AcquiredAt,
		&lock.ExpiresAt,
		&lock.CreatedAt,
		&lock.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrLockNotFound
		}
		return nil, fmt.Errorf("failed to get lock: %w", err)
	}

	lock.HolderInfo = holderInfo.String

	if lock.ExpiresAt.Before(time.Now()) {
		return &lock, ErrLockExpired
	}

	return &lock, nil
}

func (r *PostgresDistributedLockRepository) ForceRelease(ctx context.Context, lockKey string) error {
	query := `
		DELETE FROM distributed_locks
		WHERE lock_key = $1
	`

	result, err := r.db.ExecContext(ctx, query, lockKey)
	if err != nil {
		return fmt.Errorf("failed to force release lock: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return ErrLockNotFound
	}

	return nil
}

func (r *PostgresDistributedLockRepository) CleanupExpired(ctx context.Context) (int64, error) {
	query := `
		DELETE FROM distributed_locks
		WHERE expires_at < NOW()
	`

	result, err := r.db.ExecContext(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup expired locks: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	return rowsAffected, nil
}

func GetRevisionPublishLockKey(tenantID uuid.UUID) string {
	if tenantID == uuid.Nil {
		return "revision:publish:global"
	}
	return fmt.Sprintf("revision:publish:%s", tenantID.String())
}

type LockManager struct {
	repo    DistributedLockRepository
	holderID uuid.UUID
}

func NewLockManager(repo DistributedLockRepository, holderID uuid.UUID) *LockManager {
	return &LockManager{
		repo:     repo,
		holderID: holderID,
	}
}

func (lm *LockManager) WithLock(ctx context.Context, lockKey string, ttl time.Duration, fn func(ctx context.Context) error) error {
	holderInfo := fmt.Sprintf("holder:%s", lm.holderID.String())

	_, err := lm.repo.TryLock(ctx, lockKey, lm.holderID, holderInfo, ttl)
	if err != nil {
		if errors.Is(err, ErrLockAlreadyHeld) {
			return fmt.Errorf("cannot acquire lock %s: %w", lockKey, err)
		}
		return fmt.Errorf("failed to acquire lock %s: %w", lockKey, err)
	}

	defer func() {
		_ = lm.repo.Release(context.Background(), lockKey, lm.holderID)
	}()

	refreshCtx, cancelRefresh := context.WithCancel(ctx)
	defer cancelRefresh()

	refreshTTL := ttl / 2
	go func() {
		ticker := time.NewTicker(refreshTTL)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = lm.repo.Refresh(refreshCtx, lockKey, lm.holderID, ttl)
			case <-refreshCtx.Done():
				return
			}
		}
	}()

	return fn(ctx)
}

func (lm *LockManager) TryLockWithRetry(ctx context.Context, lockKey string, ttl time.Duration, maxRetries int, retryDelay time.Duration) (*DistributedLock, error) {
	if maxRetries <= 0 {
		maxRetries = DefaultLockMaxRetries
	}
	if retryDelay <= 0 {
		retryDelay = DefaultLockRetryDelay
	}

	holderInfo := fmt.Sprintf("holder:%s", lm.holderID.String())

	var lastErr error
	for i := 0; i < maxRetries; i++ {
		lock, err := lm.repo.TryLock(ctx, lockKey, lm.holderID, holderInfo, ttl)
		if err == nil {
			return lock, nil
		}
		if !errors.Is(err, ErrLockAlreadyHeld) {
			return nil, err
		}
		lastErr = err

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retryDelay):
		}
	}

	return nil, fmt.Errorf("failed to acquire lock after %d retries: %w", maxRetries, lastErr)
}
