package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Helper: build context with roles and permissions
func ctxWithRoles(roles []string) context.Context {
	ctx := context.Background()
	if len(roles) > 0 {
		ctx = context.WithValue(ctx, RolesKey, roles)
	}
	return ctx
}

func ctxWithPermissions(permissions []string) context.Context {
	ctx := context.Background()
	if len(permissions) > 0 {
		ctx = context.WithValue(ctx, PermissionsKey, permissions)
	}
	return ctx
}

func ctxWithRolesAndPermissions(roles, permissions []string) context.Context {
	ctx := context.Background()
	if len(roles) > 0 {
		ctx = context.WithValue(ctx, RolesKey, roles)
	}
	if len(permissions) > 0 {
		ctx = context.WithValue(ctx, PermissionsKey, permissions)
	}
	return ctx
}

func ctxWithTenant(tenantID uuid.UUID) context.Context {
	ctx := context.Background()
	ctx = context.WithValue(ctx, TenantIDKey, tenantID)
	return ctx
}

// ==================== HasRole Tests ====================

func TestHasRole_AdminHasRole(t *testing.T) {
	ctx := ctxWithRoles([]string{"tenant_admin", "developer"})
	if !HasRole(ctx, "tenant_admin") {
		t.Error("HasRole(tenant_admin) should return true")
	}
	if !HasRole(ctx, "developer") {
		t.Error("HasRole(developer) should return true")
	}
}

func TestHasRole_AdminDoesNotHaveRole(t *testing.T) {
	ctx := ctxWithRoles([]string{"viewer"})
	if HasRole(ctx, "super_admin") {
		t.Error("HasRole(super_admin) should return false")
	}
}

func TestHasRole_NoRolesInContext(t *testing.T) {
	ctx := context.Background()
	if HasRole(ctx, "anything") {
		t.Error("HasRole should return false when no roles in context")
	}
}

// ==================== HasPermission Tests ====================

func TestHasPermission_AdminHasPermission(t *testing.T) {
	ctx := ctxWithPermissions([]string{"service:read", "route:create"})
	if !HasPermission(ctx, "service:read") {
		t.Error("HasPermission(service:read) should return true")
	}
	if !HasPermission(ctx, "route:create") {
		t.Error("HasPermission(route:create) should return true")
	}
}

func TestHasPermission_AdminDoesNotHavePermission(t *testing.T) {
	ctx := ctxWithPermissions([]string{"service:read"})
	if HasPermission(ctx, "service:delete") {
		t.Error("HasPermission(service:delete) should return false")
	}
}

func TestHasPermission_NoPermissionsInContext(t *testing.T) {
	ctx := context.Background()
	if HasPermission(ctx, "anything") {
		t.Error("HasPermission should return false when no permissions in context")
	}
}

// ==================== GetTenantID Tests ====================

func TestGetTenantID_WithTenantID(t *testing.T) {
	tenantID := uuid.New()
	ctx := ctxWithTenant(tenantID)
	id, ok := GetTenantID(ctx)
	if !ok {
		t.Fatal("GetTenantID returned false")
	}
	if id != tenantID {
		t.Errorf("GetTenantID = %s, want %s", id, tenantID)
	}
}

func TestGetTenantID_NoTenantID(t *testing.T) {
	ctx := context.Background()
	_, ok := GetTenantID(ctx)
	if ok {
		t.Error("GetTenantID should return false when no tenant_id in context")
	}
}

func TestGetTenantID_NilUUID(t *testing.T) {
	ctx := ctxWithTenant(uuid.Nil)
	id, ok := GetTenantID(ctx)
	if !ok {
		t.Fatal("GetTenantID returned false for nil UUID")
	}
	if id != uuid.Nil {
		t.Errorf("GetTenantID = %s, want nil UUID", id)
	}
}

// ==================== RBAC Middleware: RequireRole Tests ====================

func TestRBACMiddleware_RequireRole_AllowsMatchingRole(t *testing.T) {
	logger := zap.NewNop()
	m := NewRBACMiddleware(logger)

	handler := m.RequireRole("tenant_admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(ctxWithRoles([]string{"tenant_admin", "viewer"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("RequireRole(tenant_admin): expected 200, got %d", w.Code)
	}
}

func TestRBACMiddleware_RequireRole_BlocksNonMatchingRole(t *testing.T) {
	logger := zap.NewNop()
	m := NewRBACMiddleware(logger)

	handler := m.RequireRole("super_admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(ctxWithRoles([]string{"viewer"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("RequireRole(super_admin) for viewer: expected 403, got %d", w.Code)
	}
}

func TestRBACMiddleware_RequireRole_BlocksNoRoles(t *testing.T) {
	logger := zap.NewNop()
	m := NewRBACMiddleware(logger)

	handler := m.RequireRole("anything")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(context.Background())
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("no roles: expected 403, got %d", w.Code)
	}
}

// ==================== RBAC Middleware: RequireAnyRole Tests ====================

func TestRBACMiddleware_RequireAnyRole_AllowsOneMatching(t *testing.T) {
	logger := zap.NewNop()
	m := NewRBACMiddleware(logger)

	handler := m.RequireAnyRole("super_admin", "tenant_admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(ctxWithRoles([]string{"tenant_admin"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("RequireAnyRole: expected 200, got %d", w.Code)
	}
}

func TestRBACMiddleware_RequireAnyRole_BlocksNoneMatching(t *testing.T) {
	logger := zap.NewNop()
	m := NewRBACMiddleware(logger)

	handler := m.RequireAnyRole("super_admin", "tenant_admin")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(ctxWithRoles([]string{"viewer"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("RequireAnyRole with viewer: expected 403, got %d", w.Code)
	}
}

// ==================== RBAC Middleware: RequireAllRoles Tests ====================

func TestRBACMiddleware_RequireAllRoles_AllowsWhenAllPresent(t *testing.T) {
	logger := zap.NewNop()
	m := NewRBACMiddleware(logger)

	handler := m.RequireAllRoles("tenant_admin", "developer")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(ctxWithRoles([]string{"tenant_admin", "developer", "viewer"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("RequireAllRoles all present: expected 200, got %d", w.Code)
	}
}

func TestRBACMiddleware_RequireAllRoles_BlocksWhenOneMissing(t *testing.T) {
	logger := zap.NewNop()
	m := NewRBACMiddleware(logger)

	handler := m.RequireAllRoles("tenant_admin", "developer")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(ctxWithRoles([]string{"tenant_admin"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("RequireAllRoles missing one: expected 403, got %d", w.Code)
	}
}

// ==================== RBAC Middleware: RequirePermission Tests ====================

func TestRBACMiddleware_RequirePermission_AllowsMatchingPermission(t *testing.T) {
	logger := zap.NewNop()
	m := NewRBACMiddleware(logger)

	handler := m.RequirePermission("service:read")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(ctxWithPermissions([]string{"service:read", "route:create"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("RequirePermission(service:read): expected 200, got %d", w.Code)
	}
}

func TestRBACMiddleware_RequirePermission_BlocksNonMatching(t *testing.T) {
	logger := zap.NewNop()
	m := NewRBACMiddleware(logger)

	handler := m.RequirePermission("service:delete")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(ctxWithPermissions([]string{"service:read"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("RequirePermission(service:delete): expected 403, got %d", w.Code)
	}
}

func TestRBACMiddleware_RequirePermission_BlocksNoPermissions(t *testing.T) {
	logger := zap.NewNop()
	m := NewRBACMiddleware(logger)

	handler := m.RequirePermission("anything")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(context.Background())
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("no permissions: expected 403, got %d", w.Code)
	}
}

// ==================== RBAC Middleware: RequireAnyPermission Tests ====================

func TestRBACMiddleware_RequireAnyPermission_AllowsOneMatching(t *testing.T) {
	logger := zap.NewNop()
	m := NewRBACMiddleware(logger)

	handler := m.RequireAnyPermission("service:delete", "route:read")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(ctxWithPermissions([]string{"route:read"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("RequireAnyPermission: expected 200, got %d", w.Code)
	}
}

// ==================== RBAC Middleware: RequireAllPermissions Tests ====================

func TestRBACMiddleware_RequireAllPermissions_AllowsWhenAllPresent(t *testing.T) {
	logger := zap.NewNop()
	m := NewRBACMiddleware(logger)

	handler := m.RequireAllPermissions("service:read", "route:create")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(ctxWithPermissions([]string{"service:read", "route:create", "plugin:read"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("RequireAllPermissions all present: expected 200, got %d", w.Code)
	}
}

func TestRBACMiddleware_RequireAllPermissions_BlocksWhenOneMissing(t *testing.T) {
	logger := zap.NewNop()
	m := NewRBACMiddleware(logger)

	handler := m.RequireAllPermissions("service:read", "route:delete")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(ctxWithPermissions([]string{"service:read", "route:create"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("RequireAllPermissions missing one: expected 403, got %d", w.Code)
	}
}

// ==================== Convenience Methods Tests ====================

func TestRBACMiddleware_RequireSuperAdmin(t *testing.T) {
	logger := zap.NewNop()
	m := NewRBACMiddleware(logger)

	handler := m.RequireSuperAdmin()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(ctxWithRoles([]string{"super_admin"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("RequireSuperAdmin: expected 200, got %d", w.Code)
	}

	req2 := httptest.NewRequest("GET", "/test", nil)
	req2 = req2.WithContext(ctxWithRoles([]string{"tenant_admin"}))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w2.Code != http.StatusForbidden {
		t.Errorf("RequireSuperAdmin for tenant_admin: expected 403, got %d", w2.Code)
	}
}

func TestRBACMiddleware_RequireTenantAdmin(t *testing.T) {
	logger := zap.NewNop()
	m := NewRBACMiddleware(logger)

	handler := m.RequireTenantAdmin()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// super_admin should pass
	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(ctxWithRoles([]string{"super_admin"}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("RequireTenantAdmin for super_admin: expected 200, got %d", w.Code)
	}

	// tenant_admin should pass
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2 = req2.WithContext(ctxWithRoles([]string{"tenant_admin"}))
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("RequireTenantAdmin for tenant_admin: expected 200, got %d", w2.Code)
	}

	// viewer should NOT pass
	req3 := httptest.NewRequest("GET", "/test", nil)
	req3 = req3.WithContext(ctxWithRoles([]string{"viewer"}))
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, req3)
	if w3.Code != http.StatusForbidden {
		t.Errorf("RequireTenantAdmin for viewer: expected 403, got %d", w3.Code)
	}
}

// ==================== GetUsername / GetAdminID Tests ====================

func TestGetAdminID_FromContext(t *testing.T) {
	adminID := uuid.New()
	ctx := context.WithValue(context.Background(), AdminIDKey, adminID)

	id, ok := GetAdminID(ctx)
	if !ok {
		t.Fatal("GetAdminID returned false")
	}
	if id != adminID {
		t.Errorf("GetAdminID = %s, want %s", id, adminID)
	}
}

func TestGetUsername_FromContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), UsernameKey, "testuser")

	name, ok := GetUsername(ctx)
	if !ok {
		t.Fatal("GetUsername returned false")
	}
	if name != "testuser" {
		t.Errorf("GetUsername = %s, want testuser", name)
	}
}
