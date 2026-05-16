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
	"github.com/taichirain/portkey/internal/domain/route"
	"github.com/taichirain/portkey/internal/domain/service"
)

func TestRouteCRUD(t *testing.T) {
	db := SetupTestDB(t)
	CleanupTables(t, db)

	ctx := context.Background()
	adminID := createTestAdmin(t, db, "test-admin")
	auditCtx := newTestAuditContext(adminID)

	auditRepo := repository.NewPostgresAuditRepository(db)
	serviceRepo := repository.NewPostgresServiceRepository(db, auditRepo)
	routeRepo := repository.NewPostgresRouteRepository(db, auditRepo)

	parentSvc, _ := service.New("parent-service")
	parentSvc.Host = "example.com"
	if err := serviceRepo.Create(ctx, parentSvc, nil); err != nil {
		t.Fatalf("Failed to create parent service: %v", err)
	}

	t.Run("Create Route", func(t *testing.T) {
		rt, err := route.New(parentSvc.ID)
		if err != nil {
			t.Fatalf("Failed to create route: %v", err)
		}
		rt.Name = "test-route"
		rt.AddPath("/api/*")
		rt.AddMethod("GET")

		err = routeRepo.Create(ctx, rt, auditCtx)
		if err != nil {
			t.Fatalf("Failed to save route: %v", err)
		}

		fetched, err := routeRepo.GetByID(ctx, rt.ID)
		if err != nil {
			t.Fatalf("Failed to get route: %v", err)
		}

		if fetched.Name != "test-route" {
			t.Errorf("Expected route name 'test-route', got '%s'", fetched.Name)
		}
		if fetched.ServiceID != parentSvc.ID {
			t.Errorf("Expected service ID %v, got %v", parentSvc.ID, fetched.ServiceID)
		}
		if len(fetched.Paths) != 1 || fetched.Paths[0] != "/api/*" {
			t.Errorf("Expected path '/api/*', got %v", fetched.Paths)
		}
	})

	t.Run("Create Route Without Service - Foreign Key Violation", func(t *testing.T) {
		nonExistentServiceID := uuid.New()
		rt, err := route.New(nonExistentServiceID)
		if err != nil {
			t.Fatalf("Failed to create route: %v", err)
		}
		rt.AddPath("/test")

		err = routeRepo.Create(ctx, rt, nil)
		if !errors.Is(err, repository.ErrInvalidInput) {
			t.Errorf("Expected ErrInvalidInput for non-existent service, got %v", err)
		}
	})

	t.Run("Get Route By Service ID", func(t *testing.T) {
		svc2, _ := service.New("service-with-routes")
		svc2.Host = "example.com"
		serviceRepo.Create(ctx, svc2, nil)

		for i := 0; i < 3; i++ {
			rt, _ := route.New(svc2.ID)
			rt.AddPath(fmt.Sprintf("/test-%d", i))
			routeRepo.Create(ctx, rt, nil)
		}

		routes, err := routeRepo.ListByServiceID(ctx, svc2.ID)
		if err != nil {
			t.Fatalf("Failed to list routes by service ID: %v", err)
		}

		if len(routes) != 3 {
			t.Errorf("Expected 3 routes, got %d", len(routes))
		}
	})

	t.Run("Update Route", func(t *testing.T) {
		rt, _ := route.New(parentSvc.ID)
		rt.Name = "route-to-update"
		rt.AddPath("/original")
		rt.AddMethod("GET")
		routeRepo.Create(ctx, rt, auditCtx)

		rt.Name = "updated-route"
		rt.Paths = []string{"/updated"}
		rt.Methods = []string{"POST", "PUT"}
		rt.StripPath = false

		err := routeRepo.Update(ctx, rt, auditCtx)
		if err != nil {
			t.Fatalf("Failed to update route: %v", err)
		}

		fetched, _ := routeRepo.GetByID(ctx, rt.ID)
		if fetched.Name != "updated-route" {
			t.Errorf("Expected name 'updated-route', got '%s'", fetched.Name)
		}
		if len(fetched.Paths) != 1 || fetched.Paths[0] != "/updated" {
			t.Errorf("Expected path '/updated', got %v", fetched.Paths)
		}
		if len(fetched.Methods) != 2 {
			t.Errorf("Expected 2 methods, got %d", len(fetched.Methods))
		}
		if fetched.StripPath != false {
			t.Errorf("Expected strip_path false, got %v", fetched.StripPath)
		}
	})

	t.Run("Delete Route", func(t *testing.T) {
		rt, _ := route.New(parentSvc.ID)
		rt.AddPath("/delete-me")
		routeRepo.Create(ctx, rt, auditCtx)

		err := routeRepo.Delete(ctx, rt.ID, auditCtx)
		if err != nil {
			t.Fatalf("Failed to delete route: %v", err)
		}

		_, err = routeRepo.GetByID(ctx, rt.ID)
		if !errors.Is(err, repository.ErrNotFound) {
			t.Errorf("Expected route to be deleted, got error: %v", err)
		}
	})

	t.Run("Route Validation - No Match Conditions", func(t *testing.T) {
		rt, _ := route.New(parentSvc.ID)

		err := routeRepo.Create(ctx, rt, nil)
		if !errors.Is(err, repository.ErrInvalidInput) {
			t.Errorf("Expected ErrInvalidInput for route without match conditions, got %v", err)
		}
	})

	t.Run("List Routes", func(t *testing.T) {
		CleanupTables(t, db)

		svc, _ := service.New("list-service")
		svc.Host = "example.com"
		serviceRepo.Create(ctx, svc, nil)

		for i := 0; i < 10; i++ {
			rt, _ := route.New(svc.ID)
			rt.AddPath(fmt.Sprintf("/list-%d", i))
			routeRepo.Create(ctx, rt, nil)
		}

		result, err := routeRepo.List(ctx, &repository.Pagination{Page: 1, PageSize: 5})
		if err != nil {
			t.Fatalf("Failed to list routes: %v", err)
		}

		if result.Total != 10 {
			t.Errorf("Expected total 10, got %d", result.Total)
		}
		if len(result.Items) != 5 {
			t.Errorf("Expected 5 items on page 1, got %d", len(result.Items))
		}
	})
}
