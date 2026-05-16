package consumer

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrConsumerUsernameOrCustomIDRequired = errors.New("either username or custom_id is required")
	ErrConsumerUsernameTooLong             = errors.New("username must be <= 255 characters")
	ErrConsumerCustomIDTooLong             = errors.New("custom_id must be <= 255 characters")
)

type Consumer struct {
	ID        uuid.UUID `json:"id"`
	TenantID  uuid.UUID `json:"tenant_id"`
	Username  string    `json:"username,omitempty"`
	CustomID  string    `json:"custom_id,omitempty"`
	Tags      []string  `json:"tags"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func New(username, customID string) (*Consumer, error) {
	if strings.TrimSpace(username) == "" && strings.TrimSpace(customID) == "" {
		return nil, ErrConsumerUsernameOrCustomIDRequired
	}

	now := time.Now()
	return &Consumer{
		ID:        uuid.New(),
		Username:  strings.TrimSpace(username),
		CustomID:  strings.TrimSpace(customID),
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (c *Consumer) Validate() error {
	if strings.TrimSpace(c.Username) == "" && strings.TrimSpace(c.CustomID) == "" {
		return ErrConsumerUsernameOrCustomIDRequired
	}
	if len(c.Username) > 255 {
		return ErrConsumerUsernameTooLong
	}
	if len(c.CustomID) > 255 {
		return ErrConsumerCustomIDTooLong
	}
	return nil
}

func (c *Consumer) AddTag(tag string) {
	for _, t := range c.Tags {
		if t == tag {
			return
		}
	}
	c.Tags = append(c.Tags, tag)
}

func (c *Consumer) RemoveTag(tag string) {
	newTags := make([]string, 0, len(c.Tags))
	for _, t := range c.Tags {
		if t != tag {
			newTags = append(newTags, t)
		}
	}
	c.Tags = newTags
}
