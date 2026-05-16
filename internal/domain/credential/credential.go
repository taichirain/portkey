package credential

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrCredentialConsumerIDRequired = errors.New("consumer id is required")
	ErrCredentialTypeRequired       = errors.New("credential type is required")
	ErrCredentialKeyRequired        = errors.New("credential key is required")
	ErrCredentialInvalidType        = errors.New("invalid credential type")
)

type Type string

const (
	TypeKeyAuth Type = "key-auth"
	TypeJWTAuth Type = "jwt-auth"
)

type Credential struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	ConsumerID uuid.UUID
	Type       Type
	Key        string
	Secret     string
	Algorithm  string
	Claims     map[string]interface{}
	Tags       []string
	Enabled    bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func New(consumerID uuid.UUID, credType Type, key string) (*Credential, error) {
	if consumerID == uuid.Nil {
		return nil, ErrCredentialConsumerIDRequired
	}
	if credType == "" {
		return nil, ErrCredentialTypeRequired
	}
	if strings.TrimSpace(key) == "" {
		return nil, ErrCredentialKeyRequired
	}

	if credType != TypeKeyAuth && credType != TypeJWTAuth {
		return nil, ErrCredentialInvalidType
	}

	now := time.Now()
	return &Credential{
		ID:         uuid.New(),
		ConsumerID: consumerID,
		Type:       credType,
		Key:        key,
		Enabled:    true,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

func (c *Credential) Validate() error {
	if c.ConsumerID == uuid.Nil {
		return ErrCredentialConsumerIDRequired
	}
	if c.Type == "" {
		return ErrCredentialTypeRequired
	}
	if strings.TrimSpace(c.Key) == "" {
		return ErrCredentialKeyRequired
	}
	if c.Type != TypeKeyAuth && c.Type != TypeJWTAuth {
		return ErrCredentialInvalidType
	}
	return nil
}

func (c *Credential) Enable() {
	c.Enabled = true
}

func (c *Credential) Disable() {
	c.Enabled = false
}

func (c *Credential) AddTag(tag string) {
	for _, t := range c.Tags {
		if t == tag {
			return
		}
	}
	c.Tags = append(c.Tags, tag)
}

func (c *Credential) IsKeyAuth() bool {
	return c.Type == TypeKeyAuth
}

func (c *Credential) IsJWTAuth() bool {
	return c.Type == TypeJWTAuth
}

func (c *Credential) MaskSecret() string {
	if c.Secret == "" {
		return ""
	}
	
	if len(c.Secret) <= 4 {
		return "****"
	}
	
	firstTwo := c.Secret[:2]
	lastTwo := c.Secret[len(c.Secret)-2:]
	return firstTwo + "****" + lastTwo
}

func (c *Credential) MaskKey() string {
	if c.Key == "" {
		return ""
	}
	
	if len(c.Key) <= 4 {
		return "****"
	}
	
	firstTwo := c.Key[:2]
	lastTwo := c.Key[len(c.Key)-2:]
	return firstTwo + "****" + lastTwo
}
