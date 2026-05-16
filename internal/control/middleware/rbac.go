package middleware

import (
	"context"
	"net/http"

	"go.uber.org/zap"
)

type RBACMiddleware struct {
	logger *zap.Logger
}

func NewRBACMiddleware(logger *zap.Logger) *RBACMiddleware {
	return &RBACMiddleware{
		logger: logger,
	}
}

func (m *RBACMiddleware) RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !HasRole(r.Context(), role) {
				m.logger.Warn("角色权限不足",
					zap.String("required_role", role),
					zap.Any("user_roles", getRolesFromContext(r.Context())),
				)
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (m *RBACMiddleware) RequireAnyRole(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !HasAnyRole(r.Context(), roles...) {
				m.logger.Warn("角色权限不足",
					zap.Strings("required_roles", roles),
					zap.Strings("user_roles", getRolesFromContext(r.Context())),
				)
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (m *RBACMiddleware) RequireAllRoles(roles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !HasAllRoles(r.Context(), roles...) {
				m.logger.Warn("角色权限不足",
					zap.Strings("required_roles", roles),
					zap.Strings("user_roles", getRolesFromContext(r.Context())),
				)
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (m *RBACMiddleware) RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !HasPermission(r.Context(), permission) {
				m.logger.Warn("权限不足",
					zap.String("required_permission", permission),
					zap.Any("user_permissions", getPermissionsFromContext(r.Context())),
				)
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (m *RBACMiddleware) RequireAnyPermission(permissions ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !HasAnyPermission(r.Context(), permissions...) {
				m.logger.Warn("权限不足",
					zap.Strings("required_permissions", permissions),
					zap.Strings("user_permissions", getPermissionsFromContext(r.Context())),
				)
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (m *RBACMiddleware) RequireAllPermissions(permissions ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !HasAllPermissions(r.Context(), permissions...) {
				m.logger.Warn("权限不足",
					zap.Strings("required_permissions", permissions),
					zap.Strings("user_permissions", getPermissionsFromContext(r.Context())),
				)
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (m *RBACMiddleware) RequireSuperAdmin() func(http.Handler) http.Handler {
	return m.RequireRole("super_admin")
}

func (m *RBACMiddleware) RequireTenantAdmin() func(http.Handler) http.Handler {
	return m.RequireAnyRole("super_admin", "tenant_admin")
}

func getRolesFromContext(ctx context.Context) []string {
	if roles, ok := GetRoles(ctx); ok {
		return roles
	}
	return []string{}
}

func getPermissionsFromContext(ctx context.Context) []string {
	if perms, ok := GetPermissions(ctx); ok {
		return perms
	}
	return []string{}
}
