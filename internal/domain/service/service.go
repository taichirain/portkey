package service

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrServiceNameRequired    = errors.New("service name is required")
	ErrServiceNameTooLong     = errors.New("service name must be <= 255 characters")
	ErrServiceInvalidProtocol = errors.New("protocol must be http or https")
	ErrServiceInvalidPort     = errors.New("port must be between 1 and 65535")
	ErrServiceInvalidPath     = errors.New("path must start with /")
)

type Protocol string

const (
	ProtocolHTTP  Protocol = "http"
	ProtocolHTTPS Protocol = "https"
)

type Service struct {
	ID             uuid.UUID  `json:"id"`
	TenantID       uuid.UUID  `json:"tenant_id"`
	Name           string     `json:"name"`
	Protocol       Protocol   `json:"protocol"`
	Host           string     `json:"host,omitempty"`
	Port           int        `json:"port,omitempty"`
	Path           string     `json:"path,omitempty"`
	UpstreamID     uuid.UUID  `json:"upstream_id,omitempty"`
	Retries        int        `json:"retries"`
	ConnectTimeout int        `json:"connect_timeout"`
	WriteTimeout   int        `json:"write_timeout"`
	ReadTimeout    int        `json:"read_timeout"`
	Tags           []string   `json:"tags"`
	Enabled        bool       `json:"enabled"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func New(name string) (*Service, error) {
	now := time.Now()
	s := &Service{
		ID:             uuid.New(),
		Name:           name,
		Protocol:       ProtocolHTTP,
		Retries:        5,
		ConnectTimeout: 60000,
		WriteTimeout:   60000,
		ReadTimeout:    60000,
		Enabled:        true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := s.Validate(); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Service) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return ErrServiceNameRequired
	}
	if len(s.Name) > 255 {
		return ErrServiceNameTooLong
	}
	if s.Protocol != ProtocolHTTP && s.Protocol != ProtocolHTTPS {
		return ErrServiceInvalidProtocol
	}
	if s.Port < 0 || s.Port > 65535 {
		return ErrServiceInvalidPort
	}
	if s.Path != "" && !strings.HasPrefix(s.Path, "/") {
		return ErrServiceInvalidPath
	}
	return nil
}

func (s *Service) Enable() {
	s.Enabled = true
}

func (s *Service) Disable() {
	s.Enabled = false
}

func (s *Service) AddTag(tag string) {
	for _, t := range s.Tags {
		if t == tag {
			return
		}
	}
	s.Tags = append(s.Tags, tag)
}

func (s *Service) RemoveTag(tag string) {
	newTags := make([]string, 0, len(s.Tags))
	for _, t := range s.Tags {
		if t != tag {
			newTags = append(newTags, t)
		}
	}
	s.Tags = newTags
}
