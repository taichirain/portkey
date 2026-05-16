//go:build integration
// +build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/taichirain/portkey/internal/control/repository"
	"github.com/taichirain/portkey/internal/domain/audit"
	"github.com/taichirain/portkey/internal/domain/route"
	"github.com/taichirain/portkey/internal/domain/service"
)

func TestConstraints(t *testing.T) {
	db := SetupTestDB(t)
	CleanupTables(t, db)

	ctx := context.Background()

	auditRepo := repository.NewPostgresAuditRepository(db)
	serviceRepo := repository.NewPostgresServiceRepository(db, auditRepo)

	t.Run("Unique Constraint - Duplicate Service Name", func(t *testing.T) {
		svc1, _ := service.New("unique-test-service")
		svc1.Host = "example.com"
		if err := serviceRepo.Create(ctx, svc1, nil); err != nil {
			t.Fatalf("Failed to create first service: %v", err)
		}

		svc2, _ := service.New("unique-test-service")
		svc2.Host = "another.com"
		err := serviceRepo.Create(ctx, svc2, nil)

		if !errors.Is(err, repository.ErrAlreadyExists) {
			t.Errorf("Expected ErrAlreadyExists for duplicate service name, got %v", err)
		}
	})

	t.Run("Unique Constraint - Duplicate Route Name", func(t *testing.T) {
		adminID := createTestAdmin(t, db, "unique-admin")
		auditCtx := newTestAuditContext(adminID)

		parentSvc, _ := service.New("parent-for-unique")
		parentSvc.Host = "example.com"
		serviceRepo.Create(ctx, parentSvc, nil)

		routeRepo := repository.NewPostgresRouteRepository(db, auditRepo)

		rt1, _ := route.New(parentSvc.ID)
		rt1.Name = "unique-route"
		rt1.AddPath("/test1")
		routeRepo.Create(ctx, rt1, auditCtx)

		rt2, _ := route.New(parentSvc.ID)
		rt2.Name = "unique-route"
		rt2.AddPath("/test2")
		err := routeRepo.Create(ctx, rt2, auditCtx)

		if !errors.Is(err, repository.ErrAlreadyExists) {
			t.Errorf("Expected ErrAlreadyExists for duplicate route name, got %v", err)
		}
	})
}

func TestAuditLogging(t *testing.T) {
	db := SetupTestDB(t)
	CleanupTables(t, db)

	ctx := context.Background()

	auditRepo := repository.NewPostgresAuditRepository(db)
	serviceRepo := repository.NewPostgresServiceRepository(db, auditRepo)

	t.Run("Create Service - Audit Log Created", func(t *testing.T) {
		CleanupTables(t, db)
		adminID := createTestAdmin(t, db, "create-audit-admin")
		auditCtx := newTestAuditContext(adminID)

		svc, _ := service.New("audit-test-service")
		svc.Host = "example.com"

		initialAuditCount := countRows(t, db, "audit_logs")

		err := serviceRepo.Create(ctx, svc, auditCtx)
		if err != nil {
			t.Fatalf("Failed to create service: %v", err)
		}

		auditLogs, err := auditRepo.GetByResourceID(ctx, "service", svc.ID)
		if err != nil {
			t.Fatalf("Failed to get audit logs: %v", err)
		}

		if len(auditLogs) != 1 {
			t.Errorf("Expected 1 audit log, got %d", len(auditLogs))
		}

		log := auditLogs[0]
		if log.Action != audit.ActionCreate {
			t.Errorf("Expected action 'create', got '%s'", log.Action)
		}
		if log.ResourceType != audit.ResourceType("service") {
			t.Errorf("Expected resource type 'service', got '%s'", log.ResourceType)
		}
		if *log.ResourceID != svc.ID {
			t.Errorf("Expected resource ID %v, got %v", svc.ID, log.ResourceID)
		}
		if log.AdminID == nil || *log.AdminID != adminID {
			t.Errorf("Expected admin ID %v, got %v", adminID, log.AdminID)
		}
		if log.NewValue == nil {
			t.Error("Expected new_value to be populated")
		}
		newValueMap, err := log.NewValueMap()
		if err != nil {
			t.Errorf("Failed to unmarshal new_value: %v", err)
		}
		if newValueMap["Name"] != "audit-test-service" {
			t.Errorf("Expected new_value.name 'audit-test-service', got '%v'", newValueMap["Name"])
		}

		afterAuditCount := countRows(t, db, "audit_logs")
		if afterAuditCount != initialAuditCount+1 {
			t.Errorf("Expected audit count to increase by 1")
		}
	})

	t.Run("Update Service - Audit Log Created", func(t *testing.T) {
		CleanupTables(t, db)
		adminID := createTestAdmin(t, db, "update-audit-admin")
		auditCtx := newTestAuditContext(adminID)

		svc, _ := service.New("update-service")
		svc.Host = "original.com"
		serviceRepo.Create(ctx, svc, auditCtx)

		svc.Host = "updated.com"
		svc.Port = 8080
		err := serviceRepo.Update(ctx, svc, auditCtx)
		if err != nil {
			t.Fatalf("Failed to update service: %v", err)
		}

		auditLogs, err := auditRepo.GetByResourceID(ctx, "service", svc.ID)
		if err != nil {
			t.Fatalf("Failed to get audit logs: %v", err)
		}

		if len(auditLogs) != 2 {
			t.Errorf("Expected 2 audit logs (create + update), got %d", len(auditLogs))
		}

		var updateLog *audit.AuditLog
		for _, log := range auditLogs {
			if log.Action == audit.ActionUpdate {
				updateLog = log
				break
			}
		}

		if updateLog == nil {
			t.Fatal("Expected to find update audit log")
		}

		if updateLog.OldValue == nil {
			t.Error("Expected old_value to be populated for update")
		}
		if updateLog.NewValue == nil {
			t.Error("Expected new_value to be populated for update")
		}
	})

	t.Run("Delete Service - Audit Log Created", func(t *testing.T) {
		CleanupTables(t, db)
		adminID := createTestAdmin(t, db, "delete-audit-admin")
		auditCtx := newTestAuditContext(adminID)

		svc, _ := service.New("delete-service")
		svc.Host = "example.com"
		serviceRepo.Create(ctx, svc, auditCtx)

		err := serviceRepo.Delete(ctx, svc.ID, auditCtx)
		if err != nil {
			t.Fatalf("Failed to delete service: %v", err)
		}

		auditLogs, err := auditRepo.GetByResourceID(ctx, "service", svc.ID)
		if err != nil {
			t.Fatalf("Failed to get audit logs: %v", err)
		}

		if len(auditLogs) != 2 {
			t.Errorf("Expected 2 audit logs (create + delete), got %d", len(auditLogs))
		}

		var deleteLog *audit.AuditLog
		for _, log := range auditLogs {
			if log.Action == audit.ActionDelete {
				deleteLog = log
				break
			}
		}

		if deleteLog == nil {
			t.Fatal("Expected to find delete audit log")
		}

		if deleteLog.OldValue == nil {
			t.Error("Expected old_value to be populated for delete")
		}
	})

	t.Run("Create Without Audit Context - No Audit Log", func(t *testing.T) {
		CleanupTables(t, db)

		svc, _ := service.New("no-audit-service")
		svc.Host = "example.com"

		err := serviceRepo.Create(ctx, svc, nil)
		if err != nil {
			t.Fatalf("Failed to create service: %v", err)
		}

		auditLogs, err := auditRepo.GetByResourceID(ctx, "service", svc.ID)
		if err != nil {
			t.Fatalf("Failed to get audit logs: %v", err)
		}

		if len(auditLogs) != 0 {
			t.Errorf("Expected 0 audit logs when auditCtx is nil, got %d", len(auditLogs))
		}
	})

	t.Run("List Audit Logs", func(t *testing.T) {
		CleanupTables(t, db)
		adminID := createTestAdmin(t, db, "list-audit-admin")
		auditCtx := newTestAuditContext(adminID)

		for i := 0; i < 5; i++ {
			svc, _ := service.New(fmt.Sprintf("list-audit-svc-%d", i))
			svc.Host = "example.com"
			serviceRepo.Create(ctx, svc, auditCtx)
		}

		result, err := auditRepo.List(ctx, &repository.Pagination{Page: 1, PageSize: 10})
		if err != nil {
			t.Fatalf("Failed to list audit logs: %v", err)
		}

		if result.Total != 5 {
			t.Errorf("Expected 5 audit logs, got %d", result.Total)
		}
		if len(result.Items) != 5 {
			t.Errorf("Expected 5 items, got %d", len(result.Items))
		}
	})

	t.Run("Audit Log - Client IP and User Agent", func(t *testing.T) {
		CleanupTables(t, db)
		adminID := createTestAdmin(t, db, "client-info-admin")

		customAuditCtx := &repository.AuditContext{
			AdminID:   &adminID,
			ClientIP:  "192.168.1.100",
			UserAgent: "TestClient/1.0",
			RequestID: "req-12345",
		}

		svc, _ := service.New("client-info-service")
		svc.Host = "example.com"
		serviceRepo.Create(ctx, svc, customAuditCtx)

		auditLogs, _ := auditRepo.GetByResourceID(ctx, "service", svc.ID)
		if len(auditLogs) == 0 {
			t.Fatal("Expected audit log")
		}

		log := auditLogs[0]
		if log.ClientIP != "192.168.1.100" {
			t.Errorf("Expected client IP '192.168.1.100', got '%s'", log.ClientIP)
		}
		if log.UserAgent != "TestClient/1.0" {
			t.Errorf("Expected user agent 'TestClient/1.0', got '%s'", log.UserAgent)
		}
		if log.RequestID != "req-12345" {
			t.Errorf("Expected request ID 'req-12345', got '%s'", log.RequestID)
		}
	})
}
