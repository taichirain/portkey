//go:build integration
// +build integration

package integration

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/control/repository"
	"github.com/taichirain/portkey/internal/domain/service"
)

func TestServiceCRUD(t *testing.T) {
	db := SetupTestDB(t)
	CleanupTables(t, db)

	ctx := context.Background()
	adminID := createTestAdmin(t, db, "test-admin")
	auditCtx := newTestAuditContext(adminID)

	auditRepo := repository.NewPostgresAuditRepository(db)
	serviceRepo := repository.NewPostgresServiceRepository(db, auditRepo)

	t.Run("Create Service", func(t *testing.T) {
		svc, err := service.New("test-service")
		if err != nil {
			t.Fatalf("Failed to create service: %v", err)
		}
		svc.Host = "example.com"
		svc.Port = 80

		err = serviceRepo.Create(ctx, svc, auditCtx)
		if err != nil {
			t.Fatalf("Failed to save service: %v", err)
		}

		fetched, err := serviceRepo.GetByID(ctx, svc.ID)
		if err != nil {
			t.Fatalf("Failed to get service: %v", err)
		}

		if fetched.Name != "test-service" {
			t.Errorf("Expected service name 'test-service', got '%s'", fetched.Name)
		}
		if fetched.Host != "example.com" {
			t.Errorf("Expected host 'example.com', got '%s'", fetched.Host)
		}
		if fetched.Port != 80 {
			t.Errorf("Expected port 80, got %d", fetched.Port)
		}
	})

	t.Run("Get Service By Name", func(t *testing.T) {
		svc, _ := service.New("test-service-by-name")
		svc.Host = "test.example.com"
		err := serviceRepo.Create(ctx, svc, nil)
		if err != nil {
			t.Fatalf("Failed to create service: %v", err)
		}

		fetched, err := serviceRepo.GetByName(ctx, "test-service-by-name")
		if err != nil {
			t.Fatalf("Failed to get service by name: %v", err)
		}

		if fetched.ID != svc.ID {
			t.Errorf("Expected service ID %v, got %v", svc.ID, fetched.ID)
		}
	})

	t.Run("Get Non-Existent Service", func(t *testing.T) {
		nonExistentID := uuid.New()
		_, err := serviceRepo.GetByID(ctx, nonExistentID)

		if !errors.Is(err, repository.ErrNotFound) {
			t.Errorf("Expected ErrNotFound, got %v", err)
		}
	})

	t.Run("Update Service", func(t *testing.T) {
		svc, _ := service.New("service-to-update")
		svc.Host = "original.example.com"
		err := serviceRepo.Create(ctx, svc, auditCtx)
		if err != nil {
			t.Fatalf("Failed to create service: %v", err)
		}

		svc.Host = "updated.example.com"
		svc.Port = 8080
		svc.Enabled = false

		err = serviceRepo.Update(ctx, svc, auditCtx)
		if err != nil {
			t.Fatalf("Failed to update service: %v", err)
		}

		fetched, err := serviceRepo.GetByID(ctx, svc.ID)
		if err != nil {
			t.Fatalf("Failed to get updated service: %v", err)
		}

		if fetched.Host != "updated.example.com" {
			t.Errorf("Expected host 'updated.example.com', got '%s'", fetched.Host)
		}
		if fetched.Port != 8080 {
			t.Errorf("Expected port 8080, got %d", fetched.Port)
		}
		if fetched.Enabled != false {
			t.Errorf("Expected enabled false, got %v", fetched.Enabled)
		}
		if fetched.UpdatedAt == fetched.CreatedAt {
			t.Errorf("Expected UpdatedAt to be different from CreatedAt")
		}
	})

	t.Run("Update Non-Existent Service", func(t *testing.T) {
		svc, _ := service.New("non-existent-service")
		svc.ID = uuid.New()

		err := serviceRepo.Update(ctx, svc, nil)
		if !errors.Is(err, repository.ErrNotFound) {
			t.Errorf("Expected ErrNotFound, got %v", err)
		}
	})

	t.Run("Delete Service", func(t *testing.T) {
		svc, _ := service.New("service-to-delete")
		err := serviceRepo.Create(ctx, svc, auditCtx)
		if err != nil {
			t.Fatalf("Failed to create service: %v", err)
		}

		err = serviceRepo.Delete(ctx, svc.ID, auditCtx)
		if err != nil {
			t.Fatalf("Failed to delete service: %v", err)
		}

		_, err = serviceRepo.GetByID(ctx, svc.ID)
		if !errors.Is(err, repository.ErrNotFound) {
			t.Errorf("Expected service to be deleted, got error: %v", err)
		}
	})

	t.Run("Delete Non-Existent Service", func(t *testing.T) {
		nonExistentID := uuid.New()
		err := serviceRepo.Delete(ctx, nonExistentID, nil)
		if !errors.Is(err, repository.ErrNotFound) {
			t.Errorf("Expected ErrNotFound, got %v", err)
		}
	})

	t.Run("List Services", func(t *testing.T) {
		CleanupTables(t, db)

		for i := 0; i < 5; i++ {
			svc, _ := service.New(fmt.Sprintf("list-service-%d", i))
			serviceRepo.Create(ctx, svc, nil)
		}

		result, err := serviceRepo.List(ctx, &repository.Pagination{Page: 1, PageSize: 10})
		if err != nil {
			t.Fatalf("Failed to list services: %v", err)
		}

		if result.Total != 5 {
			t.Errorf("Expected total 5, got %d", result.Total)
		}
		if len(result.Items) != 5 {
			t.Errorf("Expected 5 items, got %d", len(result.Items))
		}
	})

	t.Run("List Services With Pagination", func(t *testing.T) {
		CleanupTables(t, db)

		for i := 0; i < 15; i++ {
			svc, _ := service.New(fmt.Sprintf("pagination-service-%d", i))
			serviceRepo.Create(ctx, svc, nil)
		}

		result, err := serviceRepo.List(ctx, &repository.Pagination{Page: 2, PageSize: 10})
		if err != nil {
			t.Fatalf("Failed to list services: %v", err)
		}

		if result.Total != 15 {
			t.Errorf("Expected total 15, got %d", result.Total)
		}
		if result.TotalPages != 2 {
			t.Errorf("Expected 2 total pages, got %d", result.TotalPages)
		}
		if len(result.Items) != 5 {
			t.Errorf("Expected 5 items on page 2, got %d", len(result.Items))
		}
	})

	t.Run("Service Validation - Empty Name", func(t *testing.T) {
		svc, _ := service.New("valid-name")
		svc.Name = ""

		err := serviceRepo.Create(ctx, svc, nil)
		if !errors.Is(err, repository.ErrInvalidInput) {
			t.Errorf("Expected ErrInvalidInput for empty name, got %v", err)
		}
	})

	t.Run("Service Validation - Invalid Protocol", func(t *testing.T) {
		svc, _ := service.New("test-service")
		svc.Protocol = "ftp"

		err := serviceRepo.Create(ctx, svc, nil)
		if !errors.Is(err, repository.ErrInvalidInput) {
			t.Errorf("Expected ErrInvalidInput for invalid protocol, got %v", err)
		}
	})

	t.Run("Service Validation - Invalid Port", func(t *testing.T) {
		svc, _ := service.New("test-service")
		svc.Port = 99999

		err := serviceRepo.Create(ctx, svc, nil)
		if !errors.Is(err, repository.ErrInvalidInput) {
			t.Errorf("Expected ErrInvalidInput for invalid port, got %v", err)
		}
	})

	t.Run("Service Enable/Disable", func(t *testing.T) {
		svc, _ := service.New("toggle-service")
		if err := serviceRepo.Create(ctx, svc, nil); err != nil {
			t.Fatalf("Failed to create service: %v", err)
		}

		fetched, _ := serviceRepo.GetByID(ctx, svc.ID)
		if !fetched.Enabled {
			t.Errorf("Expected service to be enabled by default")
		}

		fetched.Disable()
		if err := serviceRepo.Update(ctx, fetched, nil); err != nil {
			t.Fatalf("Failed to update service: %v", err)
		}

		fetched, _ = serviceRepo.GetByID(ctx, svc.ID)
		if fetched.Enabled {
			t.Errorf("Expected service to be disabled")
		}

		fetched.Enable()
		serviceRepo.Update(ctx, fetched, nil)
		fetched, _ = serviceRepo.GetByID(ctx, svc.ID)
		if !fetched.Enabled {
			t.Errorf("Expected service to be enabled again")
		}
	})

	t.Run("Service Tags", func(t *testing.T) {
		svc, _ := service.New("tagged-service")
		svc.AddTag("api")
		svc.AddTag("v1")

		if err := serviceRepo.Create(ctx, svc, nil); err != nil {
			t.Fatalf("Failed to create service: %v", err)
		}

		fetched, _ := serviceRepo.GetByID(ctx, svc.ID)
		if len(fetched.Tags) != 2 {
			t.Errorf("Expected 2 tags, got %d", len(fetched.Tags))
		}

		fetched.RemoveTag("api")
		serviceRepo.Update(ctx, fetched, nil)

		fetched, _ = serviceRepo.GetByID(ctx, svc.ID)
		if len(fetched.Tags) != 1 {
			t.Errorf("Expected 1 tag after removal, got %d", len(fetched.Tags))
		}
		if fetched.Tags[0] != "v1" {
			t.Errorf("Expected tag 'v1', got '%s'", fetched.Tags[0])
		}
	})
}
