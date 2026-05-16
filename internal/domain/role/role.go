package role

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrRoleNameRequired   = errors.New("role name is required")
	ErrRoleNameTooLong    = errors.New("role name must be <= 255 characters")
	ErrRoleTenantRequired = errors.New("role tenant_id is required")
)

type Role struct {
	ID          uuid.UUID `json:"id"`
	TenantID    uuid.UUID `json:"tenant_id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	IsSystem    bool      `json:"is_system"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	Permissions []string `json:"permissions,omitempty"`
}

func New(tenantID uuid.UUID, name string) (*Role, error) {
	if tenantID == uuid.Nil {
		return nil, ErrRoleTenantRequired
	}

	now := time.Now()
	r := &Role{
		ID:        uuid.New(),
		TenantID:  tenantID,
		Name:      strings.TrimSpace(name),
		IsSystem:  false,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := r.Validate(); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *Role) Validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return ErrRoleNameRequired
	}
	if len(r.Name) > 255 {
		return ErrRoleNameTooLong
	}
	if r.TenantID == uuid.Nil {
		return ErrRoleTenantRequired
	}
	return nil
}

type SystemRole string

const (
	SystemRoleSuperAdmin   SystemRole = "super_admin"
	SystemRoleTenantAdmin  SystemRole = "tenant_admin"
	SystemRoleDeveloper    SystemRole = "developer"
	SystemRoleViewer       SystemRole = "viewer"
)

func GetSystemRoleNames() []string {
	return []string{
		string(SystemRoleSuperAdmin),
		string(SystemRoleTenantAdmin),
		string(SystemRoleDeveloper),
		string(SystemRoleViewer),
	}
}

func IsSystemRole(name string) bool {
	for _, sr := range GetSystemRoleNames() {
		if sr == name {
			return true
		}
	}
	return false
}
