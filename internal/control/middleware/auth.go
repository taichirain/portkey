package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/control/auth"
	"github.com/taichirain/portkey/internal/control/repository"
	"go.uber.org/zap"
)

type contextKey string

const (
	AdminIDKey     contextKey = "admin_id"
	UsernameKey    contextKey = "username"
	RolesKey       contextKey = "roles"
	PermissionsKey contextKey = "permissions"
)

var TenantIDKey = repository.TenantIDKey

type AuthMiddleware struct {
	authService *auth.AuthService
	logger      *zap.Logger
}

func NewAuthMiddleware(authService *auth.AuthService, logger *zap.Logger) *AuthMiddleware {
	return &AuthMiddleware{
		authService: authService,
		logger:      logger,
	}
}

func (m *AuthMiddleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			m.logger.Warn("缺少 Authorization 头")
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || (strings.ToLower(parts[0]) != "bearer" && strings.ToLower(parts[0]) != "token") {
			m.logger.Warn("无效的 Authorization 格式")
			http.Error(w, "Invalid authorization format", http.StatusUnauthorized)
			return
		}

		token := parts[1]
		claims, err := m.authService.ValidateToken(token)
		if err != nil {
			m.logger.Warn("Token 验证失败", zap.Error(err))
			if err == auth.ErrTokenExpired {
				http.Error(w, "Token expired", http.StatusUnauthorized)
			} else {
				http.Error(w, "Invalid token", http.StatusUnauthorized)
			}
			return
		}

		ctx := r.Context()
		ctx = context.WithValue(ctx, AdminIDKey, claims.AdminID)
		ctx = context.WithValue(ctx, UsernameKey, claims.Username)
		ctx = context.WithValue(ctx, TenantIDKey, claims.TenantID)
		ctx = context.WithValue(ctx, RolesKey, claims.Roles)
		ctx = context.WithValue(ctx, PermissionsKey, claims.Permissions)
		r = r.WithContext(ctx)

		m.logger.Debug("认证成功",
			zap.Stringer("admin_id", claims.AdminID),
			zap.String("username", claims.Username),
			zap.Stringer("tenant_id", claims.TenantID),
			zap.Strings("roles", claims.Roles),
		)

		next.ServeHTTP(w, r)
	})
}

func GetAdminID(ctx context.Context) (uuid.UUID, bool) {
	adminID, ok := ctx.Value(AdminIDKey).(uuid.UUID)
	return adminID, ok
}

func GetUsername(ctx context.Context) (string, bool) {
	username, ok := ctx.Value(UsernameKey).(string)
	return username, ok
}

func GetTenantID(ctx context.Context) (uuid.UUID, bool) {
	tenantID, ok := ctx.Value(TenantIDKey).(uuid.UUID)
	return tenantID, ok
}

func GetRoles(ctx context.Context) ([]string, bool) {
	roles, ok := ctx.Value(RolesKey).([]string)
	return roles, ok
}

func GetPermissions(ctx context.Context) ([]string, bool) {
	permissions, ok := ctx.Value(PermissionsKey).([]string)
	return permissions, ok
}

func HasRole(ctx context.Context, role string) bool {
	roles, ok := GetRoles(ctx)
	if !ok {
		return false
	}
	for _, r := range roles {
		if r == "super_admin" {
			return true
		}
		if r == role {
			return true
		}
	}
	return false
}

func HasAnyRole(ctx context.Context, roles ...string) bool {
	userRoles, ok := GetRoles(ctx)
	if !ok {
		return false
	}
	for _, userRole := range userRoles {
		if userRole == "super_admin" {
			return true
		}
		for _, required := range roles {
			if userRole == required {
				return true
			}
		}
	}
	return false
}

func HasAllRoles(ctx context.Context, roles ...string) bool {
	userRoles, ok := GetRoles(ctx)
	if !ok {
		return false
	}
	userRoleMap := make(map[string]bool)
	isSuperAdmin := false
	for _, r := range userRoles {
		userRoleMap[r] = true
		if r == "super_admin" {
			isSuperAdmin = true
		}
	}
	if isSuperAdmin {
		return true
	}
	for _, required := range roles {
		if !userRoleMap[required] {
			return false
		}
	}
	return true
}

func HasPermission(ctx context.Context, permission string) bool {
	if HasRole(ctx, "super_admin") {
		return true
	}
	permissions, ok := GetPermissions(ctx)
	if !ok {
		return false
	}
	for _, p := range permissions {
		if p == permission {
			return true
		}
	}
	return false
}

func HasAnyPermission(ctx context.Context, permissions ...string) bool {
	if HasRole(ctx, "super_admin") {
		return true
	}
	userPerms, ok := GetPermissions(ctx)
	if !ok {
		return false
	}
	for _, userPerm := range userPerms {
		for _, required := range permissions {
			if userPerm == required {
				return true
			}
		}
	}
	return false
}

func HasAllPermissions(ctx context.Context, permissions ...string) bool {
	if HasRole(ctx, "super_admin") {
		return true
	}
	userPerms, ok := GetPermissions(ctx)
	if !ok {
		return false
	}
	userPermMap := make(map[string]bool)
	for _, p := range userPerms {
		userPermMap[p] = true
	}
	for _, required := range permissions {
		if !userPermMap[required] {
			return false
		}
	}
	return true
}
