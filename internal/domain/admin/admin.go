package admin

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrAdminUsernameRequired     = errors.New("username is required")
	ErrAdminPasswordHashRequired = errors.New("password hash is required")
	ErrAdminUsernameTooLong      = errors.New("username must be <= 255 characters")
	ErrAdminEmailTooLong         = errors.New("email must be <= 255 characters")
	ErrAdminInvalidEmail         = errors.New("invalid email format")
)

type Admin struct {
	ID           uuid.UUID  `json:"id"`
	Username     string     `json:"username"`
	Email        string     `json:"email,omitempty"`
	PasswordHash string     `json:"-"`
	TenantID     *uuid.UUID `json:"tenant_id,omitempty"`
	RBACToken    *uuid.UUID `json:"-"`
	Enabled      bool       `json:"enabled"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`

	Roles       []string `json:"roles,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
}

func New(username, email, passwordHash string) (*Admin, error) {
	if strings.TrimSpace(username) == "" {
		return nil, ErrAdminUsernameRequired
	}
	if strings.TrimSpace(passwordHash) == "" {
		return nil, ErrAdminPasswordHashRequired
	}

	now := time.Now()
	return &Admin{
		ID:           uuid.New(),
		Username:     strings.TrimSpace(username),
		Email:        strings.TrimSpace(email),
		PasswordHash: passwordHash,
		TenantID:     nil,
		Enabled:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

func NewWithTenant(username, email, passwordHash string, tenantID uuid.UUID) (*Admin, error) {
	admin, err := New(username, email, passwordHash)
	if err != nil {
		return nil, err
	}
	admin.TenantID = &tenantID
	return admin, nil
}

func (a *Admin) Validate() error {
	if strings.TrimSpace(a.Username) == "" {
		return ErrAdminUsernameRequired
	}
	if len(a.Username) > 255 {
		return ErrAdminUsernameTooLong
	}
	if len(a.Email) > 255 {
		return ErrAdminEmailTooLong
	}
	if a.Email != "" && !strings.Contains(a.Email, "@") {
		return ErrAdminInvalidEmail
	}
	return nil
}

func (a *Admin) Enable() {
	a.Enabled = true
}

func (a *Admin) Disable() {
	a.Enabled = false
}

func (a *Admin) GenerateRBACToken() {
	token := uuid.New()
	a.RBACToken = &token
}

func (a *Admin) RevokeRBACToken() {
	a.RBACToken = nil
}

func (a *Admin) IsSuperAdmin() bool {
	return a.TenantID == nil
}

func (a *Admin) HasTenant() bool {
	return a.TenantID != nil && *a.TenantID != uuid.Nil
}

func (a *Admin) GetTenantID() uuid.UUID {
	if a.TenantID == nil {
		return uuid.Nil
	}
	return *a.TenantID
}

func (a *Admin) HasPermission(resource, action string) bool {
	key := resource + ":" + action
	for _, p := range a.Permissions {
		if p == key {
			return true
		}
	}
	return false
}

func (a *Admin) HasRole(roleName string) bool {
	for _, r := range a.Roles {
		if r == roleName {
			return true
		}
	}
	return false
}
