package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/control/api"
	"github.com/taichirain/portkey/internal/control/auth"
	"github.com/taichirain/portkey/internal/control/repository"
	"go.uber.org/zap"
)

type LoginHandler struct {
	authService *auth.AuthService
	adminRepo   repository.AdminRepository
	logger      *zap.Logger
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token       string   `json:"token"`
	ExpiresIn   int64    `json:"expires_in"`
	AdminID     string   `json:"admin_id"`
	Username    string   `json:"username"`
	TenantID    string   `json:"tenant_id,omitempty"`
	Roles       []string `json:"roles"`
	Permissions []string `json:"permissions"`
}

func NewLoginHandler(authService *auth.AuthService, adminRepo repository.AdminRepository, logger *zap.Logger) *LoginHandler {
	return &LoginHandler{
		authService: authService,
		adminRepo:   adminRepo,
		logger:      logger,
	}
}

func (h *LoginHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	if req.Username == "" || req.Password == "" {
		api.JSONError(w, http.StatusBadRequest, "Username and password are required", "VALIDATION_ERROR")
		return
	}

	admin, err := h.adminRepo.GetByUsername(r.Context(), req.Username)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			h.logger.Warn("Login attempt with non-existent username", zap.String("username", req.Username))
			api.JSONError(w, http.StatusUnauthorized, "Invalid username or password", "INVALID_CREDENTIALS")
			return
		}
		h.logger.Error("Failed to get admin by username", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR")
		return
	}

	if !admin.Enabled {
		h.logger.Warn("Login attempt with disabled admin", zap.String("username", req.Username))
		api.JSONError(w, http.StatusForbidden, "Admin account is disabled", "ACCOUNT_DISABLED")
		return
	}

	if err := h.authService.VerifyPassword(admin.PasswordHash, req.Password); err != nil {
		h.logger.Warn("Login attempt with invalid password", zap.String("username", req.Username))
		api.JSONError(w, http.StatusUnauthorized, "Invalid username or password", "INVALID_CREDENTIALS")
		return
	}

	tenantID := admin.GetTenantID()

	var roles []string
	var permissions []string

	if admin.IsSuperAdmin() {
		roles = []string{"super_admin"}
		permissions = []string{}
	} else {
		adminWithRoles, err := h.adminRepo.GetByIDWithRolesAndPermissions(r.Context(), admin.ID, tenantID)
		if err == nil && adminWithRoles != nil {
			roles = adminWithRoles.Roles
			permissions = adminWithRoles.Permissions
		}
	}

	if roles == nil {
		roles = []string{}
	}
	if permissions == nil {
		permissions = []string{}
	}

	authResult, err := h.authService.GenerateToken(admin.ID, admin.Username, tenantID, roles, permissions)
	if err != nil {
		h.logger.Error("Failed to generate token", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to generate token", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Admin logged in",
		zap.String("username", req.Username),
		zap.Stringer("admin_id", admin.ID),
		zap.Stringer("tenant_id", tenantID),
		zap.Strings("roles", roles),
	)

	response := LoginResponse{
		Token:       authResult.Token,
		ExpiresIn:   int64(authResult.ExpiresIn.Seconds()),
		AdminID:     authResult.AdminID.String(),
		Username:    authResult.Username,
		Roles:       authResult.Roles,
		Permissions: authResult.Permissions,
	}

	if tenantID != uuid.Nil {
		response.TenantID = tenantID.String()
	}

	api.JSON(w, http.StatusOK, response)
}
