package tenant

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrTenantNameRequired      = errors.New("tenant name is required")
	ErrTenantSlugRequired      = errors.New("tenant slug is required")
	ErrTenantNameTooLong       = errors.New("tenant name must be <= 255 characters")
	ErrTenantSlugTooLong       = errors.New("tenant slug must be <= 100 characters")
	ErrTenantSlugInvalidFormat = errors.New("tenant slug must contain only lowercase letters, numbers, and hyphens")
)

var slugRegex = regexp.MustCompile(`^[a-z0-9-]+$`)

type Tenant struct {
	ID          uuid.UUID       `json:"id"`
	Name        string          `json:"name"`
	Slug        string          `json:"slug"`
	Description string          `json:"description,omitempty"`
	Settings    json.RawMessage `json:"settings,omitempty"`
	Enabled     bool            `json:"enabled"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

func New(name, slug string) (*Tenant, error) {
	now := time.Now()
	t := &Tenant{
		ID:        uuid.New(),
		Name:      strings.TrimSpace(name),
		Slug:      strings.TrimSpace(slug),
		Settings:  json.RawMessage("{}"),
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := t.Validate(); err != nil {
		return nil, err
	}

	return t, nil
}

func (t *Tenant) Validate() error {
	if strings.TrimSpace(t.Name) == "" {
		return ErrTenantNameRequired
	}
	if len(t.Name) > 255 {
		return ErrTenantNameTooLong
	}
	if strings.TrimSpace(t.Slug) == "" {
		return ErrTenantSlugRequired
	}
	if len(t.Slug) > 100 {
		return ErrTenantSlugTooLong
	}
	if !slugRegex.MatchString(t.Slug) {
		return ErrTenantSlugInvalidFormat
	}
	return nil
}

func (t *Tenant) Enable() {
	t.Enabled = true
}

func (t *Tenant) Disable() {
	t.Enabled = false
}

func (t *Tenant) SetSettings(settings map[string]interface{}) error {
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	t.Settings = json.RawMessage(data)
	return nil
}

func (t *Tenant) GetSettings() (map[string]interface{}, error) {
	if t.Settings == nil || len(t.Settings) == 0 {
		return make(map[string]interface{}), nil
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(t.Settings, &settings); err != nil {
		return nil, err
	}
	return settings, nil
}
