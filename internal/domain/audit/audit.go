package audit

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Action string

const (
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	ActionDelete Action = "delete"
)

type ResourceType string

const (
	ResourceTypeService   ResourceType = "service"
	ResourceTypeRoute     ResourceType = "route"
	ResourceTypeUpstream  ResourceType = "upstream"
	ResourceTypeTarget    ResourceType = "target"
	ResourceTypeConsumer  ResourceType = "consumer"
	ResourceTypeCredential ResourceType = "credential"
	ResourceTypePlugin    ResourceType = "plugin"
	ResourceTypeAdmin     ResourceType = "admin"
	ResourceTypeRevision  ResourceType = "revision"
)

type AuditLog struct {
	ID           uuid.UUID
	TenantID     uuid.UUID
	AdminID      *uuid.UUID
	Action       Action
	ResourceType ResourceType
	ResourceID   *uuid.UUID
	OldValue     []byte
	NewValue     []byte
	ClientIP     string
	UserAgent    string
	RequestID    string
	CreatedAt    time.Time
}

func New(action Action, resourceType ResourceType, resourceID *uuid.UUID, adminID *uuid.UUID) *AuditLog {
	return &AuditLog{
		ID:           uuid.New(),
		AdminID:      adminID,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		CreatedAt:    time.Now(),
	}
}

func (a *AuditLog) SetOldValue(value interface{}) error {
	if value == nil {
		a.OldValue = nil
		return nil
	}

	jsonBytes, err := json.Marshal(value)
	if err != nil {
		return err
	}

	a.OldValue = jsonBytes
	return nil
}

func (a *AuditLog) SetNewValue(value interface{}) error {
	if value == nil {
		a.NewValue = nil
		return nil
	}

	jsonBytes, err := json.Marshal(value)
	if err != nil {
		return err
	}

	a.NewValue = jsonBytes
	return nil
}

func (a *AuditLog) OldValueJSON() ([]byte, error) {
	return a.OldValue, nil
}

func (a *AuditLog) NewValueJSON() ([]byte, error) {
	return a.NewValue, nil
}

func (a *AuditLog) OldValueMap() (map[string]interface{}, error) {
	if a.OldValue == nil {
		return nil, nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(a.OldValue, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func (a *AuditLog) NewValueMap() (map[string]interface{}, error) {
	if a.NewValue == nil {
		return nil, nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(a.NewValue, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func (a *AuditLog) IsCreate() bool {
	return a.Action == ActionCreate
}

func (a *AuditLog) IsUpdate() bool {
	return a.Action == ActionUpdate
}

func (a *AuditLog) IsDelete() bool {
	return a.Action == ActionDelete
}
