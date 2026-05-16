package route

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrRouteServiceIDRequired = errors.New("service id is required")
	ErrRouteNameTooLong       = errors.New("route name must be <= 255 characters")
	ErrRouteAtLeastOneMatch   = errors.New("route must have at least one match condition (methods, hosts, paths, or headers)")
)

type Route struct {
	ID            uuid.UUID              `json:"id"`
	TenantID      uuid.UUID              `json:"tenant_id"`
	Name          string                 `json:"name"`
	ServiceID     uuid.UUID              `json:"service_id"`
	Protocols     []string               `json:"protocols"`
	Methods       []string               `json:"methods,omitempty"`
	Hosts         []string               `json:"hosts,omitempty"`
	Paths         []string               `json:"paths,omitempty"`
	Headers       map[string][]string    `json:"headers,omitempty"`
	StripPath     bool                   `json:"strip_path"`
	PreserveHost  bool                   `json:"preserve_host"`
	RegexPriority int                    `json:"regex_priority"`
	Tags          []string               `json:"tags"`
	Enabled       bool                   `json:"enabled"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
}

func New(serviceID uuid.UUID) (*Route, error) {
	if serviceID == uuid.Nil {
		return nil, ErrRouteServiceIDRequired
	}

	now := time.Now()
	return &Route{
		ID:            uuid.New(),
		ServiceID:     serviceID,
		Protocols:     []string{"http"},
		StripPath:     true,
		PreserveHost:  false,
		RegexPriority: 0,
		Enabled:       true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

func (r *Route) Validate() error {
	if r.ServiceID == uuid.Nil {
		return ErrRouteServiceIDRequired
	}
	if len(r.Name) > 255 {
		return ErrRouteNameTooLong
	}
	if len(r.Methods) == 0 && len(r.Hosts) == 0 && len(r.Paths) == 0 && len(r.Headers) == 0 {
		return ErrRouteAtLeastOneMatch
	}
	return nil
}

func (r *Route) Enable() {
	r.Enabled = true
}

func (r *Route) Disable() {
	r.Enabled = false
}

func (r *Route) AddMethod(method string) {
	upperMethod := strings.ToUpper(method)
	for _, m := range r.Methods {
		if m == upperMethod {
			return
		}
	}
	r.Methods = append(r.Methods, upperMethod)
}

func (r *Route) RemoveMethod(method string) {
	upperMethod := strings.ToUpper(method)
	newMethods := make([]string, 0, len(r.Methods))
	for _, m := range r.Methods {
		if m != upperMethod {
			newMethods = append(newMethods, m)
		}
	}
	r.Methods = newMethods
}

func (r *Route) AddHost(host string) {
	for _, h := range r.Hosts {
		if h == host {
			return
		}
	}
	r.Hosts = append(r.Hosts, host)
}

func (r *Route) AddPath(path string) {
	for _, p := range r.Paths {
		if p == path {
			return
		}
	}
	r.Paths = append(r.Paths, path)
}

func (r *Route) AddTag(tag string) {
	for _, t := range r.Tags {
		if t == tag {
			return
		}
	}
	r.Tags = append(r.Tags, tag)
}
