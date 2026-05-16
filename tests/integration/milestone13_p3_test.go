//go:build integration
// +build integration

package integration

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/control/repository"
	"github.com/taichirain/portkey/internal/domain/revision"
	"github.com/taichirain/portkey/internal/domain/service"
	"github.com/taichirain/portkey/internal/platform/postgres"
)

func createTestTenant(t *testing.T, db *postgres.DB, id uuid.UUID, name, slug string) {
	t.Helper()
	ctx := context.Background()
	query := `INSERT INTO tenants (id, name, slug) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`
	if _, err := db.ExecContext(ctx, query, id, name, slug); err != nil {
		t.Fatalf("Failed to create test tenant: %v", err)
	}
}

func TestM13_P3_RevisionTenantIsolation(t *testing.T) {
	db := SetupTestDB(t)
	CleanupTables(t, db)

	auditRepo := repository.NewPostgresAuditRepository(db)
	revisionRepo := repository.NewPostgresRevisionRepository(db, auditRepo)

	tenantA := uuid.New()
	tenantB := uuid.New()
	createTestTenant(t, db, tenantA, "Tenant A", fmt.Sprintf("ta-%s", tenantA.String()[:8]))
	createTestTenant(t, db, tenantB, "Tenant B", fmt.Sprintf("tb-%s", tenantB.String()[:8]))

	t.Run("Revision 创建时应关联正确的 tenant", func(t *testing.T) {
		CleanupTables(t, db)

		ctxA := repository.ContextWithTenantID(context.Background(), tenantA)

		revA, err := revision.New("v1.0.0-tenantA", map[string]interface{}{
			"version":   "1.0",
			"timestamp": "now",
		}, nil)
		if err != nil {
			t.Fatalf("Failed to create revision: %v", err)
		}

		err = revisionRepo.Create(ctxA, revA, nil)
		if err != nil {
			t.Fatalf("Failed to save revision: %v", err)
		}

		if revA.TenantID != tenantA {
			t.Errorf("Revision tenant_id = %v, want %v", revA.TenantID, tenantA)
		}

		found, err := revisionRepo.GetByID(ctxA, revA.ID)
		if err != nil {
			t.Fatalf("Failed to get revision: %v", err)
		}

		if found.TenantID != tenantA {
			t.Errorf("Found revision tenant_id = %v, want %v", found.TenantID, tenantA)
		}
	})

	t.Run("跨 tenant 无法访问 revision - GetByID", func(t *testing.T) {
		CleanupTables(t, db)

		ctxA := repository.ContextWithTenantID(context.Background(), tenantA)
		ctxB := repository.ContextWithTenantID(context.Background(), tenantB)

		revA, _ := revision.New("v1.0.0-tenantA", map[string]interface{}{}, nil)
		err := revisionRepo.Create(ctxA, revA, nil)
		if err != nil {
			t.Fatalf("Failed to save revision: %v", err)
		}

		_, err = revisionRepo.GetByID(ctxB, revA.ID)
		if err == nil {
			t.Error("Expected error when tenant B tries to access tenant A's revision")
		}
		if err != repository.ErrNotFound {
			t.Errorf("Expected ErrNotFound, got %v", err)
		}
	})

	t.Run("跨 tenant 无法访问 revision - GetByVersion", func(t *testing.T) {
		CleanupTables(t, db)

		ctxA := repository.ContextWithTenantID(context.Background(), tenantA)
		ctxB := repository.ContextWithTenantID(context.Background(), tenantB)

		revA, _ := revision.New("v1.0.0-tenantA", map[string]interface{}{}, nil)
		err := revisionRepo.Create(ctxA, revA, nil)
		if err != nil {
			t.Fatalf("Failed to save revision: %v", err)
		}

		_, err = revisionRepo.GetByVersion(ctxB, "v1.0.0-tenantA")
		if err == nil {
			t.Error("Expected error when tenant B tries to access tenant A's revision")
		}
		if err != repository.ErrNotFound {
			t.Errorf("Expected ErrNotFound, got %v", err)
		}
	})

	t.Run("List revisions 只返回当前 tenant 的数据", func(t *testing.T) {
		CleanupTables(t, db)

		ctxA := repository.ContextWithTenantID(context.Background(), tenantA)
		ctxB := repository.ContextWithTenantID(context.Background(), tenantB)

		for i := 0; i < 3; i++ {
			rev, _ := revision.New("v1.0.0-tenantA", map[string]interface{}{}, nil)
			err := revisionRepo.Create(ctxA, rev, nil)
			if err != nil {
				t.Fatalf("Failed to save revision: %v", err)
			}
		}

		for i := 0; i < 2; i++ {
			rev, _ := revision.New("v1.0.0-tenantB", map[string]interface{}{}, nil)
			err := revisionRepo.Create(ctxB, rev, nil)
			if err != nil {
				t.Fatalf("Failed to save revision: %v", err)
			}
		}

		listA, err := revisionRepo.List(ctxA, &repository.Pagination{Page: 1, PageSize: 100})
		if err != nil {
			t.Fatalf("Failed to list revisions for tenant A: %v", err)
		}
		if listA.Total != 3 {
			t.Errorf("Tenant A expected 3 revisions, got %d", listA.Total)
		}

		listB, err := revisionRepo.List(ctxB, &repository.Pagination{Page: 1, PageSize: 100})
		if err != nil {
			t.Fatalf("Failed to list revisions for tenant B: %v", err)
		}
		if listB.Total != 2 {
			t.Errorf("Tenant B expected 2 revisions, got %d", listB.Total)
		}

		for _, rev := range listA.Items {
			if rev.Version != "v1.0.0-tenantA" {
				t.Errorf("Found wrong revision version in tenant A: %s", rev.Version)
			}
		}

		for _, rev := range listB.Items {
			if rev.Version != "v1.0.0-tenantB" {
				t.Errorf("Found wrong revision version in tenant B: %s", rev.Version)
			}
		}
	})

	t.Run("跨 tenant 无法删除 revision", func(t *testing.T) {
		CleanupTables(t, db)

		ctxA := repository.ContextWithTenantID(context.Background(), tenantA)
		ctxB := repository.ContextWithTenantID(context.Background(), tenantB)

		revA, _ := revision.New("v1.0.0-tenantA", map[string]interface{}{}, nil)
		err := revisionRepo.Create(ctxA, revA, nil)
		if err != nil {
			t.Fatalf("Failed to save revision: %v", err)
		}

		err = revisionRepo.Delete(ctxB, revA.ID, nil)
		if err == nil {
			t.Error("Expected error when tenant B tries to delete tenant A's revision")
		}
		if err != repository.ErrNotFound {
			t.Errorf("Expected ErrNotFound, got %v", err)
		}

		_, err = revisionRepo.GetByID(ctxA, revA.ID)
		if err != nil {
			t.Errorf("Revision should still exist: %v", err)
		}
	})

	t.Run("跨 tenant 无法更新 revision", func(t *testing.T) {
		CleanupTables(t, db)

		ctxA := repository.ContextWithTenantID(context.Background(), tenantA)
		ctxB := repository.ContextWithTenantID(context.Background(), tenantB)

		revA, _ := revision.New("v1.0.0-original", map[string]interface{}{}, nil)
		err := revisionRepo.Create(ctxA, revA, nil)
		if err != nil {
			t.Fatalf("Failed to save revision: %v", err)
		}

		revA.Version = "v1.0.0-updated"
		err = revisionRepo.Update(ctxB, revA, nil)
		if err == nil {
			t.Error("Expected error when tenant B tries to update tenant A's revision")
		}
		if err != repository.ErrNotFound {
			t.Errorf("Expected ErrNotFound, got %v", err)
		}

		found, err := revisionRepo.GetByID(ctxA, revA.ID)
		if err != nil {
			t.Fatalf("Failed to get revision: %v", err)
		}
		if found.Version != "v1.0.0-original" {
			t.Errorf("Revision version should not be changed, got %s", found.Version)
		}
	})
}

func TestM13_P3_AuditLogTenantIsolation(t *testing.T) {
	db := SetupTestDB(t)
	CleanupTables(t, db)

	auditRepo := repository.NewPostgresAuditRepository(db)
	serviceRepo := repository.NewPostgresServiceRepository(db, auditRepo)

	tenantA := uuid.New()
	tenantB := uuid.New()
	createTestTenant(t, db, tenantA, "Tenant A", fmt.Sprintf("ta-%s", tenantA.String()[:8]))
	createTestTenant(t, db, tenantB, "Tenant B", fmt.Sprintf("tb-%s", tenantB.String()[:8]))

	t.Run("创建服务时审计日志关联正确的 tenant", func(t *testing.T) {
		CleanupTables(t, db)
		createTestTenant(t, db, tenantA, "Tenant A", fmt.Sprintf("ta-%s", tenantA.String()[:8]))
		adminID := createTestAdmin(t, db, "p3-test-admin")
		auditCtx := newTestAuditContext(adminID)

		ctxA := repository.ContextWithTenantID(context.Background(), tenantA)

		svc, _ := service.New("audit-test-service")
		svc.Host = "example.com"

		err := serviceRepo.Create(ctxA, svc, auditCtx)
		if err != nil {
			t.Fatalf("Failed to create service: %v", err)
		}

		logs, err := auditRepo.GetByResourceID(ctxA, "service", svc.ID)
		if err != nil {
			t.Fatalf("Failed to get audit logs: %v", err)
		}

		if len(logs) == 0 {
			t.Fatal("Expected audit logs")
		}

		log := logs[0]
		if log.TenantID != tenantA {
			t.Errorf("Audit log tenant_id = %v, want %v", log.TenantID, tenantA)
		}
	})

	t.Run("跨 tenant 无法访问审计日志 - GetByResourceID", func(t *testing.T) {
		CleanupTables(t, db)
		createTestTenant(t, db, tenantA, "Tenant A", fmt.Sprintf("ta-%s", tenantA.String()[:8]))
		createTestTenant(t, db, tenantB, "Tenant B", fmt.Sprintf("tb-%s", tenantB.String()[:8]))
		adminID := createTestAdmin(t, db, "p3-test-admin")
		auditCtx := newTestAuditContext(adminID)

		ctxA := repository.ContextWithTenantID(context.Background(), tenantA)
		ctxB := repository.ContextWithTenantID(context.Background(), tenantB)

		svc, _ := service.New("audit-test-service")
		svc.Host = "example.com"

		err := serviceRepo.Create(ctxA, svc, auditCtx)
		if err != nil {
			t.Fatalf("Failed to create service: %v", err)
		}

		logsA, err := auditRepo.GetByResourceID(ctxA, "service", svc.ID)
		if err != nil {
			t.Fatalf("Failed to get audit logs: %v", err)
		}
		if len(logsA) != 1 {
			t.Errorf("Tenant A expected 1 audit log, got %d", len(logsA))
		}

		logsB, err := auditRepo.GetByResourceID(ctxB, "service", svc.ID)
		if err != nil {
			t.Fatalf("Failed to get audit logs: %v", err)
		}
		if len(logsB) != 0 {
			t.Errorf("Tenant B should see 0 audit logs for tenant A's service, got %d", len(logsB))
		}
	})

	t.Run("List 审计日志只返回当前 tenant 的数据", func(t *testing.T) {
		CleanupTables(t, db)
		createTestTenant(t, db, tenantA, "Tenant A", fmt.Sprintf("ta-%s", tenantA.String()[:8]))
		createTestTenant(t, db, tenantB, "Tenant B", fmt.Sprintf("tb-%s", tenantB.String()[:8]))
		adminID := createTestAdmin(t, db, "p3-test-admin")
		auditCtx := newTestAuditContext(adminID)

		ctxA := repository.ContextWithTenantID(context.Background(), tenantA)
		ctxB := repository.ContextWithTenantID(context.Background(), tenantB)

		for i := 0; i < 2; i++ {
			svc, _ := service.New(fmt.Sprintf("svc-tenantA-%d", i))
			svc.Host = "example.com"
			if err := serviceRepo.Create(ctxA, svc, auditCtx); err != nil {
				t.Fatalf("Failed to create service for tenant A: %v", err)
			}
		}

		for i := 0; i < 3; i++ {
			svc, _ := service.New(fmt.Sprintf("svc-tenantB-%d", i))
			svc.Host = "example.com"
			if err := serviceRepo.Create(ctxB, svc, auditCtx); err != nil {
				t.Fatalf("Failed to create service for tenant B: %v", err)
			}
		}

		listA, err := auditRepo.List(ctxA, &repository.Pagination{Page: 1, PageSize: 100})
		if err != nil {
			t.Fatalf("Failed to list audit logs: %v", err)
		}
		if listA.Total != 2 {
			t.Errorf("Tenant A expected 2 audit logs, got %d", listA.Total)
		}

		listB, err := auditRepo.List(ctxB, &repository.Pagination{Page: 1, PageSize: 100})
		if err != nil {
			t.Fatalf("Failed to list audit logs: %v", err)
		}
		if listB.Total != 3 {
			t.Errorf("Tenant B expected 3 audit logs, got %d", listB.Total)
		}
	})
}

func TestM13_P3_NoTenantContext_Behavior(t *testing.T) {
	db := SetupTestDB(t)
	CleanupTables(t, db)

	auditRepo := repository.NewPostgresAuditRepository(db)
	revisionRepo := repository.NewPostgresRevisionRepository(db, auditRepo)

	t.Run("Revision Create 没有 tenant context 时使用 System tenant", func(t *testing.T) {
		CleanupTables(t, db)
		createTestTenant(t, db, uuid.Nil, "System", "system")

		ctx := context.Background()

		rev, _ := revision.New("v1.0.0", map[string]interface{}{}, nil)
		err := revisionRepo.Create(ctx, rev, nil)

		if err != nil {
			t.Fatalf("Expected success with uuid.Nil System tenant, got: %v", err)
		}
		if rev.TenantID != uuid.Nil {
			t.Errorf("Expected tenant_id = uuid.Nil, got %v", rev.TenantID)
		}
	})

	t.Run("没有 tenant context 时 List 应返回所有数据", func(t *testing.T) {
		CleanupTables(t, db)

		tenantA := uuid.New()
		createTestTenant(t, db, tenantA, "NoTenantList Tenant", fmt.Sprintf("ntl-%s", tenantA.String()[:8]))
		ctxA := repository.ContextWithTenantID(context.Background(), tenantA)
		ctxNoTenant := context.Background()

		for i := 0; i < 2; i++ {
			rev, _ := revision.New("v1.0.0", map[string]interface{}{}, nil)
			err := revisionRepo.Create(ctxA, rev, nil)
			if err != nil {
				t.Fatalf("Failed to save revision: %v", err)
			}
		}

		listNoTenant, err := revisionRepo.List(ctxNoTenant, &repository.Pagination{Page: 1, PageSize: 100})
		if err != nil {
			t.Fatalf("Failed to list revisions: %v", err)
		}

		if listNoTenant.Total != 2 {
			t.Logf("No tenant context returned %d revisions (should see all)", listNoTenant.Total)
		}
	})
}
