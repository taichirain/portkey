package validator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/control/repository"
	"github.com/taichirain/portkey/internal/domain/route"
	"github.com/taichirain/portkey/internal/domain/service"
	"github.com/taichirain/portkey/internal/domain/target"
	"github.com/taichirain/portkey/internal/domain/trafficpolicy"
	"github.com/taichirain/portkey/internal/domain/upstream"
)

var (
	ErrValidationFailed           = errors.New("validation failed")
	ErrServiceNotFound            = errors.New("service not found")
	ErrUpstreamNotFound           = errors.New("upstream not found")
	ErrRouteHasNoConditions       = errors.New("route must have at least one match condition")
	ErrServiceInvalidTarget       = errors.New("service must have either host/port or upstream_id")
	ErrUpstreamHasNoTargets       = errors.New("upstream has no enabled targets")
	ErrTrafficPolicyInvalidTarget = errors.New("traffic policy target_service_id cannot be the same as route's service_id")
)

type ConfigValidator struct {
	routeRepo          repository.RouteRepository
	serviceRepo        repository.ServiceRepository
	upstreamRepo       repository.UpstreamRepository
	targetRepo         repository.TargetRepository
	trafficPolicyRepo  repository.TrafficPolicyRepository
}

type ValidationResult struct {
	Valid  bool
	Errors []ValidationError
}

type ValidationError struct {
	ResourceType string
	ResourceID   uuid.UUID
	Field        string
	Message      string
}

func NewConfigValidator(
	routeRepo repository.RouteRepository,
	serviceRepo repository.ServiceRepository,
	upstreamRepo repository.UpstreamRepository,
	targetRepo repository.TargetRepository,
	trafficPolicyRepo repository.TrafficPolicyRepository,
) *ConfigValidator {
	return &ConfigValidator{
		routeRepo:         routeRepo,
		serviceRepo:       serviceRepo,
		upstreamRepo:      upstreamRepo,
		targetRepo:        targetRepo,
		trafficPolicyRepo: trafficPolicyRepo,
	}
}

func (v *ConfigValidator) ValidateRoute(ctx context.Context, r *route.Route) (*ValidationResult, error) {
	result := &ValidationResult{
		Valid:  true,
		Errors: make([]ValidationError, 0),
	}

	if err := r.Validate(); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			ResourceType: "route",
			ResourceID:   r.ID,
			Message:      err.Error(),
		})
	}

	if len(r.Methods) == 0 && len(r.Hosts) == 0 && len(r.Paths) == 0 && len(r.Headers) == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			ResourceType: "route",
			ResourceID:   r.ID,
			Field:        "match_conditions",
			Message:      ErrRouteHasNoConditions.Error(),
		})
	}

	if r.ServiceID != uuid.Nil {
		_, err := v.serviceRepo.GetByID(ctx, r.ServiceID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				result.Valid = false
				result.Errors = append(result.Errors, ValidationError{
					ResourceType: "route",
					ResourceID:   r.ID,
					Field:        "service_id",
					Message:      fmt.Sprintf("%s: %s", ErrServiceNotFound, r.ServiceID),
				})
			} else {
				return nil, err
			}
		}
	}

	return result, nil
}

func (v *ConfigValidator) ValidateService(ctx context.Context, s *service.Service) (*ValidationResult, error) {
	result := &ValidationResult{
		Valid:  true,
		Errors: make([]ValidationError, 0),
	}

	if err := s.Validate(); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			ResourceType: "service",
			ResourceID:   s.ID,
			Message:      err.Error(),
		})
	}

	hasDirectTarget := s.Host != "" && s.Port > 0
	hasUpstream := s.UpstreamID != uuid.Nil

	if !hasDirectTarget && !hasUpstream {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			ResourceType: "service",
			ResourceID:   s.ID,
			Message:      ErrServiceInvalidTarget.Error(),
		})
	}

	if hasUpstream {
		_, err := v.upstreamRepo.GetByID(ctx, s.UpstreamID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				result.Valid = false
				result.Errors = append(result.Errors, ValidationError{
					ResourceType: "service",
					ResourceID:   s.ID,
					Field:        "upstream_id",
					Message:      fmt.Sprintf("%s: %s", ErrUpstreamNotFound, s.UpstreamID),
				})
			} else {
				return nil, err
			}
		}
	}

	return result, nil
}

func (v *ConfigValidator) ValidateUpstream(ctx context.Context, u *upstream.Upstream) (*ValidationResult, error) {
	result := &ValidationResult{
		Valid:  true,
		Errors: make([]ValidationError, 0),
	}

	if err := u.Validate(); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			ResourceType: "upstream",
			ResourceID:   u.ID,
			Message:      err.Error(),
		})
	}

	return result, nil
}

func (v *ConfigValidator) ValidateTarget(ctx context.Context, t *target.Target) (*ValidationResult, error) {
	result := &ValidationResult{
		Valid:  true,
		Errors: make([]ValidationError, 0),
	}

	if err := t.Validate(); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			ResourceType: "target",
			ResourceID:   t.ID,
			Message:      err.Error(),
		})
	}

	if t.UpstreamID != uuid.Nil {
		_, err := v.upstreamRepo.GetByID(ctx, t.UpstreamID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				result.Valid = false
				result.Errors = append(result.Errors, ValidationError{
					ResourceType: "target",
					ResourceID:   t.ID,
					Field:        "upstream_id",
					Message:      fmt.Sprintf("%s: %s", ErrUpstreamNotFound, t.UpstreamID),
				})
			} else {
				return nil, err
			}
		}
	}

	return result, nil
}

func (v *ConfigValidator) ValidateAll(ctx context.Context) (*ValidationResult, error) {
	result := &ValidationResult{
		Valid:  true,
		Errors: make([]ValidationError, 0),
	}

	services, err := v.serviceRepo.List(ctx, &repository.Pagination{Page: 1, PageSize: 1000})
	if err != nil {
		return nil, err
	}

	for _, svc := range services.Items {
		svcResult, err := v.ValidateService(ctx, svc)
		if err != nil {
			return nil, err
		}
		if !svcResult.Valid {
			result.Valid = false
			result.Errors = append(result.Errors, svcResult.Errors...)
		}
	}

	routes, err := v.routeRepo.List(ctx, &repository.Pagination{Page: 1, PageSize: 1000})
	if err != nil {
		return nil, err
	}

	for _, r := range routes.Items {
		routeResult, err := v.ValidateRoute(ctx, r)
		if err != nil {
			return nil, err
		}
		if !routeResult.Valid {
			result.Valid = false
			result.Errors = append(result.Errors, routeResult.Errors...)
		}
	}

	upstreams, err := v.upstreamRepo.List(ctx, &repository.Pagination{Page: 1, PageSize: 1000})
	if err != nil {
		return nil, err
	}

	for _, u := range upstreams.Items {
		upstreamResult, err := v.ValidateUpstream(ctx, u)
		if err != nil {
			return nil, err
		}
		if !upstreamResult.Valid {
			result.Valid = false
			result.Errors = append(result.Errors, upstreamResult.Errors...)
		}

		targets, err := v.targetRepo.ListByUpstreamID(ctx, u.ID)
		if err != nil {
			return nil, err
		}

		hasEnabledTargets := false
		for _, t := range targets {
			if t.Enabled {
				hasEnabledTargets = true
				break
			}
		}

		if !hasEnabledTargets {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				ResourceType: "upstream",
				ResourceID:   u.ID,
				Message:      ErrUpstreamHasNoTargets.Error(),
			})
		}
	}

	trafficPolicies, err := v.trafficPolicyRepo.List(ctx, &repository.Pagination{Page: 1, PageSize: 1000})
	if err != nil {
		return nil, err
	}

	for _, tp := range trafficPolicies.Items {
		tpResult, err := v.ValidateTrafficPolicy(ctx, tp)
		if err != nil {
			return nil, err
		}
		if !tpResult.Valid {
			result.Valid = false
			result.Errors = append(result.Errors, tpResult.Errors...)
		}
	}

	return result, nil
}

func (v *ConfigValidator) ValidateTrafficPolicy(ctx context.Context, tp *trafficpolicy.TrafficPolicy) (*ValidationResult, error) {
	result := &ValidationResult{
		Valid:  true,
		Errors: make([]ValidationError, 0),
	}

	if err := tp.Validate(); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			ResourceType: "traffic_policy",
			ResourceID:   tp.ID,
			Message:      err.Error(),
		})
	}

	rt, err := v.routeRepo.GetByID(ctx, tp.RouteID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				ResourceType: "traffic_policy",
				ResourceID:   tp.ID,
				Field:        "route_id",
				Message:      fmt.Sprintf("route not found: %s", tp.RouteID),
			})
		} else {
			return nil, err
		}
	}

	if tp.Type == trafficpolicy.PolicyTypeFallback {
		var cfg trafficpolicy.FallbackMatchConfig
		if err := json.Unmarshal(tp.MatchConfig, &cfg); err == nil {
			_, err = v.serviceRepo.GetByID(ctx, cfg.FallbackServiceID)
			if err != nil {
				if errors.Is(err, repository.ErrNotFound) {
					result.Valid = false
					result.Errors = append(result.Errors, ValidationError{
						ResourceType: "traffic_policy",
						ResourceID:   tp.ID,
						Field:        "fallback_service_id",
						Message:      fmt.Sprintf("%s: %s", ErrServiceNotFound, cfg.FallbackServiceID),
					})
				} else {
					return nil, err
				}
			}
		}
	} else {
		_, err = v.serviceRepo.GetByID(ctx, tp.TargetServiceID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				result.Valid = false
				result.Errors = append(result.Errors, ValidationError{
					ResourceType: "traffic_policy",
					ResourceID:   tp.ID,
					Field:        "target_service_id",
					Message:      fmt.Sprintf("%s: %s", ErrServiceNotFound, tp.TargetServiceID),
				})
			} else {
				return nil, err
			}
		}

		if rt != nil && tp.TargetServiceID == rt.ServiceID {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				ResourceType: "traffic_policy",
				ResourceID:   tp.ID,
				Field:        "target_service_id",
				Message:      ErrTrafficPolicyInvalidTarget.Error(),
			})
		}
	}

	return result, nil
}
