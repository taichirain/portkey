package permission

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrPermissionResourceRequired = errors.New("permission resource is required")
	ErrPermissionActionRequired   = errors.New("permission action is required")
	ErrPermissionResourceTooLong  = errors.New("permission resource must be <= 100 characters")
	ErrPermissionActionTooLong    = errors.New("permission action must be <= 50 characters")
)

type Permission struct {
	ID          uuid.UUID `json:"id"`
	Resource    string    `json:"resource"`
	Action      string    `json:"action"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

func New(resource, action string) (*Permission, error) {
	now := time.Now()
	p := &Permission{
		ID:        uuid.New(),
		Resource:  strings.TrimSpace(resource),
		Action:    strings.TrimSpace(action),
		CreatedAt: now,
	}

	if err := p.Validate(); err != nil {
		return nil, err
	}

	return p, nil
}

func (p *Permission) Validate() error {
	if strings.TrimSpace(p.Resource) == "" {
		return ErrPermissionResourceRequired
	}
	if len(p.Resource) > 100 {
		return ErrPermissionResourceTooLong
	}
	if strings.TrimSpace(p.Action) == "" {
		return ErrPermissionActionRequired
	}
	if len(p.Action) > 50 {
		return ErrPermissionActionTooLong
	}
	return nil
}

func (p *Permission) Key() string {
	return p.Resource + ":" + p.Action
}

type Resource string

const (
	ResourceService       Resource = "service"
	ResourceRoute         Resource = "route"
	ResourceUpstream      Resource = "upstream"
	ResourceTarget        Resource = "target"
	ResourceConsumer      Resource = "consumer"
	ResourceCredential    Resource = "credential"
	ResourcePlugin        Resource = "plugin"
	ResourceRevision      Resource = "revision"
	ResourceAudit         Resource = "audit"
	ResourceAdmin         Resource = "admin"
	ResourceRole          Resource = "role"
	ResourceTenant        Resource = "tenant"
	ResourceTrafficPolicy Resource = "traffic_policy"
)

type Action string

const (
	ActionCreate  Action = "create"
	ActionRead    Action = "read"
	ActionUpdate  Action = "update"
	ActionDelete  Action = "delete"
	ActionPublish Action = "publish"
	ActionRollback Action = "rollback"
	ActionAssign  Action = "assign"
)

func GetAllResources() []Resource {
	return []Resource{
		ResourceService,
		ResourceRoute,
		ResourceUpstream,
		ResourceTarget,
		ResourceConsumer,
		ResourceCredential,
		ResourcePlugin,
		ResourceRevision,
		ResourceAudit,
		ResourceAdmin,
		ResourceRole,
		ResourceTenant,
		ResourceTrafficPolicy,
	}
}

func GetAllActions() []Action {
	return []Action{
		ActionCreate,
		ActionRead,
		ActionUpdate,
		ActionDelete,
		ActionPublish,
		ActionRollback,
		ActionAssign,
	}
}

func GetCRUDActions() []Action {
	return []Action{ActionCreate, ActionRead, ActionUpdate, ActionDelete}
}

func GetActionsForResource(resource Resource) []Action {
	switch resource {
	case ResourceRevision:
		return []Action{ActionCreate, ActionRead, ActionPublish, ActionRollback}
	case ResourceRole:
		return []Action{ActionCreate, ActionRead, ActionUpdate, ActionDelete, ActionAssign}
	case ResourceAudit:
		return []Action{ActionRead}
	default:
		return GetCRUDActions()
	}
}
