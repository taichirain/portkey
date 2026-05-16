//go:build integration
// +build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/control/repository"
	"github.com/taichirain/portkey/internal/domain/revision"
)

func TestMultiCPHighAvailability(t *testing.T) {
	db := SetupTestDB(t)
	CleanupTables(t, db)

	ctx := context.Background()

	auditRepo := repository.NewPostgresAuditRepository(db)

	t.Run("Distributed Lock - Basic Operations", func(t *testing.T) {
		CleanupTables(t, db)

		lockRepo := repository.NewPostgresDistributedLockRepository(db)
		holderID := uuid.New()

		t.Run("Acquire Lock Successfully", func(t *testing.T) {
			lock, err := lockRepo.TryLock(ctx, "test-lock-basic", holderID, "test-holder-info", 30*time.Second)
			if err != nil {
				t.Fatalf("Failed to acquire lock: %v", err)
			}
			if lock == nil {
				t.Fatal("Expected lock to be returned")
			}
			if lock.HolderID != holderID {
				t.Errorf("Expected holder ID %v, got %v", holderID, lock.HolderID)
			}
		})

		t.Run("Cannot Acquire Lock Held by Others", func(t *testing.T) {
			lockKey := "test-lock-conflict"
			holder1 := uuid.New()
			holder2 := uuid.New()

			_, err := lockRepo.TryLock(ctx, lockKey, holder1, "holder-1", 30*time.Second)
			if err != nil {
				t.Fatalf("Failed to acquire first lock: %v", err)
			}

			_, err = lockRepo.TryLock(ctx, lockKey, holder2, "holder-2", 30*time.Second)
			if !errors.Is(err, repository.ErrLockAlreadyHeld) {
				t.Errorf("Expected ErrLockAlreadyHeld, got %v", err)
			}
		})

		t.Run("Release Lock and Reacquire", func(t *testing.T) {
			lockKey := "test-lock-release"
			holder1 := uuid.New()
			holder2 := uuid.New()

			_, err := lockRepo.TryLock(ctx, lockKey, holder1, "holder-1", 30*time.Second)
			if err != nil {
				t.Fatalf("Failed to acquire initial lock: %v", err)
			}

			err = lockRepo.Release(ctx, lockKey, holder1)
			if err != nil {
				t.Fatalf("Failed to release lock: %v", err)
			}

			lock, err := lockRepo.TryLock(ctx, lockKey, holder2, "holder-2", 30*time.Second)
			if err != nil {
				t.Fatalf("Failed to acquire lock after release: %v", err)
			}
			if lock.HolderID != holder2 {
				t.Errorf("Expected holder2, got %v", lock.HolderID)
			}
		})

		t.Run("Refresh Lock", func(t *testing.T) {
			lockKey := "test-lock-refresh"
			holder := uuid.New()

			_, err := lockRepo.TryLock(ctx, lockKey, holder, "holder", 1*time.Second)
			if err != nil {
				t.Fatalf("Failed to acquire lock: %v", err)
			}

			err = lockRepo.Refresh(ctx, lockKey, holder, 30*time.Second)
			if err != nil {
				t.Errorf("Failed to refresh lock: %v", err)
			}

			lock, err := lockRepo.Get(ctx, lockKey)
			if err != nil {
				t.Fatalf("Failed to get lock: %v", err)
			}

			now := time.Now()
			if lock.ExpiresAt.Before(now.Add(20 * time.Second)) {
				t.Errorf("Lock should expire after at least 20 seconds from now, but expires at %v", lock.ExpiresAt)
			}
		})

		t.Run("Expired Lock Can Be Acquired", func(t *testing.T) {
			lockKey := "test-lock-expired"
			holder1 := uuid.New()
			holder2 := uuid.New()

			_, err := lockRepo.TryLock(ctx, lockKey, holder1, "holder-1", 1*time.Millisecond)
			if err != nil {
				t.Fatalf("Failed to acquire initial lock: %v", err)
			}

			time.Sleep(10 * time.Millisecond)

			lock, err := lockRepo.TryLock(ctx, lockKey, holder2, "holder-2", 30*time.Second)
			if err != nil {
				t.Fatalf("Failed to acquire expired lock: %v", err)
			}
			if lock.HolderID != holder2 {
				t.Errorf("Expected holder2, got %v", lock.HolderID)
			}
		})
	})

	t.Run("Revision Publish - Single Active Constraint", func(t *testing.T) {
		CleanupTables(t, db)

		revRepo := repository.NewPostgresRevisionRepository(db, auditRepo)
		tenantID := uuid.New()
		createTestTenant(t, db, tenantID, "single-active-test", "single-active-test")
		tenantCtx := repository.ContextWithTenantID(ctx, tenantID)

		t.Run("Only One Active Revision Per Tenant", func(t *testing.T) {
			rev1, _ := revision.New("v1.0", map[string]interface{}{"test": "data"}, nil)
			rev2, _ := revision.New("v2.0", map[string]interface{}{"test": "data2"}, nil)

			err := revRepo.Create(tenantCtx, rev1, nil)
			if err != nil {
				t.Fatalf("Failed to create rev1: %v", err)
			}

			err = revRepo.Create(tenantCtx, rev2, nil)
			if err != nil {
				t.Fatalf("Failed to create rev2: %v", err)
			}

			err = revRepo.Activate(tenantCtx, rev1.ID, nil)
			if err != nil {
				t.Fatalf("Failed to activate rev1: %v", err)
			}

			activeRev, err := revRepo.GetActive(tenantCtx)
			if err != nil {
				t.Fatalf("Failed to get active revision: %v", err)
			}
			if activeRev.ID != rev1.ID {
				t.Errorf("Expected active revision to be rev1, got %v", activeRev.ID)
			}

			err = revRepo.Activate(tenantCtx, rev2.ID, nil)
			if err != nil {
				t.Fatalf("Failed to activate rev2: %v", err)
			}

			activeRev, err = revRepo.GetActive(tenantCtx)
			if err != nil {
				t.Fatalf("Failed to get active revision after activating rev2: %v", err)
			}
			if activeRev.ID != rev2.ID {
				t.Errorf("Expected active revision to be rev2, got %v", activeRev.ID)
			}

			updatedRev1, err := revRepo.GetByID(tenantCtx, rev1.ID)
			if err != nil {
				t.Fatalf("Failed to get rev1: %v", err)
			}
			if updatedRev1.IsActive {
				t.Error("Expected rev1 to be inactive after rev2 activation")
			}
		})
	})

	t.Run("Multi CP Concurrent Publish", func(t *testing.T) {
		CleanupTables(t, db)

		auditRepo := repository.NewPostgresAuditRepository(db)
		lockRepo := repository.NewPostgresDistributedLockRepository(db)
		tenantID := uuid.New()
		createTestTenant(t, db, tenantID, "multi-cp-concurrent-test", "multi-cp-concurrent-test")
		tenantCtx := repository.ContextWithTenantID(ctx, tenantID)

		instance1ID := uuid.New()
		instance2ID := uuid.New()
		instance3ID := uuid.New()

		revRepo1 := repository.NewPostgresRevisionRepositoryWithInstanceID(db, auditRepo, instance1ID)
		revRepo2 := repository.NewPostgresRevisionRepositoryWithInstanceID(db, auditRepo, instance2ID)
		revRepo3 := repository.NewPostgresRevisionRepositoryWithInstanceID(db, auditRepo, instance3ID)

		revisions := make([]*revision.ConfigRevision, 5)
		for i := 0; i < 5; i++ {
			rev, _ := revision.New(fmt.Sprintf("v%d.0", i+1), map[string]interface{}{"version": i + 1}, nil)
			revisions[i] = rev
			err := revRepo1.Create(tenantCtx, rev, nil)
			if err != nil {
				t.Fatalf("Failed to create revision %d: %v", i, err)
			}
		}

		var successCount int64
		var failureCount int64
		var wg sync.WaitGroup

		publishRevision := func(revIdx int, repo *repository.PostgresRevisionRepository, instanceID uuid.UUID) {
			defer wg.Done()

			err := repo.ActivateWithLock(tenantCtx, revisions[revIdx].ID, nil, lockRepo, instanceID)
			if err == nil {
				atomic.AddInt64(&successCount, 1)
				t.Logf("Instance %v successfully published revision v%d.0", instanceID, revIdx+1)
			} else {
				atomic.AddInt64(&failureCount, 1)
				t.Logf("Instance %v failed to publish revision v%d.0: %v", instanceID, revIdx+1, err)
			}
		}

		wg.Add(6)

		go publishRevision(0, revRepo1, instance1ID)
		go publishRevision(1, revRepo2, instance2ID)
		go publishRevision(2, revRepo3, instance3ID)
		go publishRevision(3, revRepo1, instance1ID)
		go publishRevision(4, revRepo2, instance2ID)
		go publishRevision(0, revRepo3, instance3ID)

		wg.Wait()

		t.Logf("Total success: %d, Total failures: %d", successCount, failureCount)

		activeRev, err := revRepo1.GetActive(tenantCtx)
		if err != nil {
			t.Fatalf("Failed to get active revision: %v", err)
		}

		t.Logf("Final active revision: %v (%v)", activeRev.ID, activeRev.Version)

		var activeCount int
		for _, rev := range revisions {
			r, err := revRepo1.GetByID(tenantCtx, rev.ID)
			if err != nil {
				t.Fatalf("Failed to get revision: %v", err)
			}
			if r.IsActive {
				activeCount++
				if r.ID != activeRev.ID {
					t.Errorf("Revision %v is active but not the same as GetActive result", r.ID)
				}
			}
		}

		if activeCount != 1 {
			t.Errorf("Expected exactly 1 active revision, got %d", activeCount)
		}
	})

	t.Run("Publish Lock Key Per Tenant", func(t *testing.T) {
		tenant1ID := uuid.New()
		tenant2ID := uuid.New()

		globalKey := repository.GetRevisionPublishLockKey(uuid.Nil)
		tenant1Key := repository.GetRevisionPublishLockKey(tenant1ID)
		tenant2Key := repository.GetRevisionPublishLockKey(tenant2ID)

		if globalKey == tenant1Key {
			t.Error("Global and tenant1 lock keys should be different")
		}
		if tenant1Key == tenant2Key {
			t.Error("Tenant1 and tenant2 lock keys should be different")
		}

		expectedTenant1Key := fmt.Sprintf("revision:publish:%s", tenant1ID.String())
		if tenant1Key != expectedTenant1Key {
			t.Errorf("Expected tenant1 key %s, got %s", expectedTenant1Key, tenant1Key)
		}

		if globalKey != "revision:publish:global" {
			t.Errorf("Expected global key 'revision:publish:global', got '%s'", globalKey)
		}
	})

	t.Run("LockManager - WithLock Pattern", func(t *testing.T) {
		CleanupTables(t, db)

		lockRepo := repository.NewPostgresDistributedLockRepository(db)
		holderID := uuid.New()
		lockManager := repository.NewLockManager(lockRepo, holderID)

		lockKey := "test-withlock"
		lockExecuted := false

		err := lockManager.WithLock(ctx, lockKey, 30*time.Second, func(lockCtx context.Context) error {
			lockExecuted = true

			_, getErr := lockRepo.Get(ctx, lockKey)
			if getErr != nil {
				t.Errorf("Should be able to get lock inside WithLock: %v", getErr)
			}
			return nil
		})

		if err != nil {
			t.Fatalf("WithLock failed: %v", err)
		}
		if !lockExecuted {
			t.Error("WithLock function was not executed")
		}

		_, err = lockRepo.Get(ctx, lockKey)
		if !errors.Is(err, repository.ErrLockNotFound) {
			t.Errorf("Lock should be released after WithLock completes, got err: %v", err)
		}
	})
}
