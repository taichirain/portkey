package snapshot

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	pluginPkg "github.com/taichirain/portkey/internal/data/plugin"
	"github.com/taichirain/portkey/internal/domain/credential"
	"github.com/taichirain/portkey/internal/domain/plugin"
	"github.com/taichirain/portkey/internal/domain/route"
	"github.com/taichirain/portkey/internal/domain/service"
	"github.com/taichirain/portkey/internal/domain/target"
	"github.com/taichirain/portkey/internal/domain/trafficpolicy"
	"github.com/taichirain/portkey/internal/domain/upstream"
)

const (
	PolicyTypeHeader   = "header"
	PolicyTypeWeight   = "weight"
	PolicyTypeConsumer = "consumer"
	PolicyTypeQuery    = "query"
	PolicyTypeCookie   = "cookie"
	PolicyTypeIP       = "ip"
	PolicyTypePath     = "path"
	PolicyTypeMethod   = "method"
	PolicyTypeTag      = "tag"
	PolicyTypeCompound = "compound"
	PolicyTypeFallback = "fallback"
)

type MatchOperator string

const (
	MatchOperatorExact         MatchOperator = "exact"
	MatchOperatorPrefix        MatchOperator = "prefix"
	MatchOperatorSuffix        MatchOperator = "suffix"
	MatchOperatorContains      MatchOperator = "contains"
	MatchOperatorRegex         MatchOperator = "regex"
	MatchOperatorNotExact      MatchOperator = "not_exact"
	MatchOperatorNotContains   MatchOperator = "not_contains"
	MatchOperatorGreaterThan   MatchOperator = "greater_than"
	MatchOperatorLessThan      MatchOperator = "less_than"
	MatchOperatorGreaterEqual  MatchOperator = "greater_equal"
	MatchOperatorLessEqual     MatchOperator = "less_equal"
)

type CompoundOperator string

const (
	CompoundOperatorAND CompoundOperator = "and"
	CompoundOperatorOR  CompoundOperator = "or"
)

type TagMatchMode string

const (
	TagMatchModeAny   TagMatchMode = "any"
	TagMatchModeAll   TagMatchMode = "all"
	TagMatchModeExact TagMatchMode = "exact"
)

type TrafficPolicy struct {
	ID              uuid.UUID
	Name            string
	RouteID         uuid.UUID
	Priority        int
	Type            string
	MatchConfig     json.RawMessage
	TargetServiceID uuid.UUID
	Enabled         bool
	Tags            []string
	RuleSetID       *uuid.UUID
}

type HeaderMatchConfig struct {
	Header   string        `json:"header"`
	Value    string        `json:"value"`
	Operator MatchOperator `json:"operator,omitempty"`
}

type WeightMatchConfig struct {
	Percentage int `json:"percentage"`
}

type QueryMatchConfig struct {
	Key      string        `json:"key"`
	Value    string        `json:"value"`
	Operator MatchOperator `json:"operator,omitempty"`
}

type CookieMatchConfig struct {
	Name     string        `json:"name"`
	Value    string        `json:"value,omitempty"`
	Operator MatchOperator `json:"operator,omitempty"`
}

type IPMatchConfig struct {
	IPList   []string `json:"ip_list,omitempty"`
	CIDRList []string `json:"cidr_list,omitempty"`
	Negate   bool     `json:"negate,omitempty"`
}

type PathMatchConfig struct {
	Pattern  string        `json:"pattern"`
	Operator MatchOperator `json:"operator,omitempty"`
}

type MethodMatchConfig struct {
	Methods []string `json:"methods"`
	Negate  bool     `json:"negate,omitempty"`
}

type TagMatchConfig struct {
	Tags       []string     `json:"tags"`
	MatchMode  TagMatchMode `json:"match_mode,omitempty"`
	SourceType string       `json:"source_type,omitempty"`
}

type ConsumerMatchConfig struct {
	ConsumerIDs []uuid.UUID    `json:"consumer_ids,omitempty"`
	Tags        []string       `json:"tags,omitempty"`
	MatchMode   TagMatchMode   `json:"match_mode,omitempty"`
}

type CompoundCondition struct {
	Type        string          `json:"type"`
	MatchConfig json.RawMessage `json:"match_config"`
}

type CompoundMatchConfig struct {
	Operator   CompoundOperator   `json:"operator"`
	Conditions []CompoundCondition `json:"conditions"`
}

type FallbackMatchConfig struct {
	FallbackServiceID uuid.UUID `json:"fallback_service_id"`
	HealthCheckType   string    `json:"health_check_type,omitempty"`
	MinHealthyTargets int       `json:"min_healthy_targets,omitempty"`
}

type HealthStatus int

const (
	HealthStatusUnknown HealthStatus = iota
	HealthStatusHealthy
	HealthStatusUnhealthy
	HealthStatusDegraded
)

type ConfigSnapshot struct {
	RevisionID uuid.UUID
	CreatedAt  time.Time

	Routes                 []*route.Route
	Services               map[uuid.UUID]*service.Service
	Upstreams              map[uuid.UUID]*upstream.Upstream
	Targets                map[uuid.UUID][]*target.Target
	Plugins                *pluginPkg.SnapshotPluginStore
	Credentials            map[string]*credential.Credential
	CredentialsByConsumer  map[uuid.UUID][]*credential.Credential
	TrafficPolicies        []*TrafficPolicy

	routeMatchers          []*RouteMatcher
	balancers              map[uuid.UUID]*Balancer
	trafficPoliciesByRoute map[uuid.UUID][]*TrafficPolicy

	targetHealthStatus     map[uuid.UUID]map[string]*TargetHealthInfo
	mu                     sync.RWMutex
}

type TargetHealthInfo struct {
	Target   string
	Port     int
	Healthy  bool
	LastCheck time.Time
}

func NewConfigSnapshot(revisionID uuid.UUID) *ConfigSnapshot {
	return &ConfigSnapshot{
		RevisionID:              revisionID,
		CreatedAt:               time.Now(),
		Routes:                  make([]*route.Route, 0),
		Services:                make(map[uuid.UUID]*service.Service),
		Upstreams:               make(map[uuid.UUID]*upstream.Upstream),
		Targets:                 make(map[uuid.UUID][]*target.Target),
		Plugins:                 pluginPkg.NewSnapshotPluginStore(),
		Credentials:             make(map[string]*credential.Credential),
		CredentialsByConsumer:   make(map[uuid.UUID][]*credential.Credential),
		TrafficPolicies:         make([]*TrafficPolicy, 0),
		balancers:               make(map[uuid.UUID]*Balancer),
		trafficPoliciesByRoute:  make(map[uuid.UUID][]*TrafficPolicy),
		targetHealthStatus:      make(map[uuid.UUID]map[string]*TargetHealthInfo),
	}
}

func (s *ConfigSnapshot) AddRoute(r *route.Route) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Routes = append(s.Routes, r)
}

func (s *ConfigSnapshot) AddService(svc *service.Service) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Services[svc.ID] = svc
}

func (s *ConfigSnapshot) AddUpstream(u *upstream.Upstream) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Upstreams[u.ID] = u
}

func (s *ConfigSnapshot) AddTargets(upstreamID uuid.UUID, t []*target.Target) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Targets[upstreamID] = t
}

func (s *ConfigSnapshot) AddTrafficPolicy(tp *TrafficPolicy) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TrafficPolicies = append(s.TrafficPolicies, tp)
}

func (s *ConfigSnapshot) AddPlugin(p *plugin.Plugin) {
	s.mu.Lock()
	defer s.mu.Unlock()

	config := &pluginPkg.PluginConfig{
		ID:         p.ID,
		Name:       p.Name,
		RouteID:    p.RouteID,
		ServiceID:  p.ServiceID,
		ConsumerID: p.ConsumerID,
		Config:     p.Config,
		Enabled:    p.Enabled,
	}

	switch {
	case p.ConsumerID != nil:
		config.Scope = pluginPkg.ScopeConsumer
		s.Plugins.AddConsumer(*p.ConsumerID, config)
	case p.RouteID != nil:
		config.Scope = pluginPkg.ScopeRoute
		s.Plugins.AddRoute(*p.RouteID, config)
	case p.ServiceID != nil:
		config.Scope = pluginPkg.ScopeService
		s.Plugins.AddService(*p.ServiceID, config)
	default:
		config.Scope = pluginPkg.ScopeGlobal
		s.Plugins.AddGlobal(config)
	}
}

func (s *ConfigSnapshot) AddCredential(c *credential.Credential) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.Credentials[c.Key] = c
	s.CredentialsByConsumer[c.ConsumerID] = append(s.CredentialsByConsumer[c.ConsumerID], c)
}

func (s *ConfigSnapshot) GetCredentialByKey(key string) (*credential.Credential, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	c, ok := s.Credentials[key]
	return c, ok
}

func (s *ConfigSnapshot) GetCredentialsByConsumer(consumerID uuid.UUID) []*credential.Credential {
	s.mu.RLock()
	defer s.mu.RUnlock()

	creds, ok := s.CredentialsByConsumer[consumerID]
	if !ok {
		return []*credential.Credential{}
	}
	return creds
}

func (s *ConfigSnapshot) Build() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.routeMatchers = make([]*RouteMatcher, 0, len(s.Routes))
	for _, r := range s.Routes {
		if !r.Enabled {
			continue
		}
		matcher, err := NewRouteMatcher(r)
		if err != nil {
			return fmt.Errorf("failed to create route matcher for %s: %w", r.ID, err)
		}
		s.routeMatchers = append(s.routeMatchers, matcher)
	}

	for upstreamID, targets := range s.Targets {
		enabledTargets := make([]*target.Target, 0)
		for _, t := range targets {
			if t.Enabled {
				enabledTargets = append(enabledTargets, t)
			}
		}
		if len(enabledTargets) > 0 {
			u, ok := s.Upstreams[upstreamID]
			if !ok {
				return fmt.Errorf("upstream %s not found for targets", upstreamID)
			}
			s.balancers[upstreamID] = NewBalancer(u, enabledTargets)
		}
	}

	s.trafficPoliciesByRoute = make(map[uuid.UUID][]*TrafficPolicy)
	for _, tp := range s.TrafficPolicies {
		if tp.RouteID == uuid.Nil {
			continue
		}
		s.trafficPoliciesByRoute[tp.RouteID] = append(s.trafficPoliciesByRoute[tp.RouteID], tp)
	}

	for routeID, policies := range s.trafficPoliciesByRoute {
		sort.Slice(policies, func(i, j int) bool {
			return policies[i].Priority < policies[j].Priority
		})
		s.trafficPoliciesByRoute[routeID] = policies
	}

	return nil
}

func (s *ConfigSnapshot) UpdateTargetHealth(upstreamID uuid.UUID, target string, port int, healthy bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.targetHealthStatus[upstreamID] == nil {
		s.targetHealthStatus[upstreamID] = make(map[string]*TargetHealthInfo)
	}
	key := fmt.Sprintf("%s:%d", target, port)
	s.targetHealthStatus[upstreamID][key] = &TargetHealthInfo{
		Target:    target,
		Port:      port,
		Healthy:   healthy,
		LastCheck: time.Now(),
	}
}

func (s *ConfigSnapshot) GetTargetHealth(upstreamID uuid.UUID, target string, port int) (bool, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.targetHealthStatus[upstreamID] == nil {
		return true, false
	}
	key := fmt.Sprintf("%s:%d", target, port)
	if info, ok := s.targetHealthStatus[upstreamID][key]; ok {
		return info.Healthy, true
	}
	return true, false
}

func (s *ConfigSnapshot) CountHealthyTargets(upstreamID uuid.UUID) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	targets, ok := s.Targets[upstreamID]
	if !ok {
		return 0
	}

	count := 0
	for _, t := range targets {
		if !t.Enabled {
			continue
		}
		healthy, exists := s.GetTargetHealth(upstreamID, t.Target, t.Port)
		if !exists || healthy {
			count++
		}
	}
	return count
}

func (s *ConfigSnapshot) MatchRoute(r *http.Request) (*MatchedRoute, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, matcher := range s.routeMatchers {
		if matched, params := matcher.Match(r); matched {
			originalSvc, ok := s.Services[matcher.Route().ServiceID]
			if !ok {
				continue
			}

			effectiveSvc := originalSvc
			policyHit := false
			hitPolicyID := uuid.Nil
			hitPolicyType := ""
			policyMatchDetails := make([]PolicyMatchDetail, 0)

			if policies, ok := s.trafficPoliciesByRoute[matcher.Route().ID]; ok {
				for _, tp := range policies {
					policyDetail := PolicyMatchDetail{
						PolicyID:        tp.ID,
						PolicyName:      tp.Name,
						PolicyType:      tp.Type,
						Priority:        tp.Priority,
						Enabled:         tp.Enabled,
						Matched:         false,
						Selected:        false,
						TargetServiceID: tp.TargetServiceID,
						ConditionDetails: make([]ConditionMatchDetail, 0),
					}

					if !tp.Enabled {
						policyDetail.SkipReason = "policy_disabled"
						policyMatchDetails = append(policyMatchDetails, policyDetail)
						continue
					}

					matched, condDetails := s.matchTrafficPolicy(tp, r, originalSvc)
					policyDetail.Matched = matched
					policyDetail.ConditionDetails = condDetails

					if matched {
						targetSvcID := tp.TargetServiceID
						if tp.Type == PolicyTypeFallback {
							var cfg FallbackMatchConfig
							if err := json.Unmarshal(tp.MatchConfig, &cfg); err == nil {
								targetSvcID = cfg.FallbackServiceID
								policyDetail.TargetServiceID = cfg.FallbackServiceID
							}
						}

						if targetSvc, ok := s.Services[targetSvcID]; ok {
							effectiveSvc = targetSvc
							policyHit = true
							hitPolicyID = tp.ID
							hitPolicyType = tp.Type
							policyDetail.Selected = true
							policyMatchDetails = append(policyMatchDetails, policyDetail)
							break
						} else {
							policyDetail.SkipReason = "target_service_not_found"
						}
					}

					policyMatchDetails = append(policyMatchDetails, policyDetail)
				}
			}

			result := &MatchedRoute{
				Route:             matcher.Route(),
				Service:           effectiveSvc,
				Upstream:          nil,
				Balancer:          nil,
				Params:            params,
				OriginalService:   originalSvc,
				EffectiveService:  effectiveSvc,
				TrafficPolicyHit:  policyHit,
				HitPolicyID:       hitPolicyID,
				HitPolicyType:     hitPolicyType,
				PolicyMatchDetails: policyMatchDetails,
			}

			if effectiveSvc.UpstreamID != uuid.Nil {
				if u, ok := s.Upstreams[effectiveSvc.UpstreamID]; ok {
					result.Upstream = u
				}
				if b, ok := s.balancers[effectiveSvc.UpstreamID]; ok {
					result.Balancer = b
				}
			}

			return result, true
		}
	}

	return nil, false
}

func (s *ConfigSnapshot) matchTrafficPolicy(tp *TrafficPolicy, r *http.Request, originalSvc *service.Service) (bool, []ConditionMatchDetail) {
	details := make([]ConditionMatchDetail, 0, 1)
	detail := ConditionMatchDetail{
		ConditionType: tp.Type,
		Matched:       false,
	}

	switch tp.Type {
	case PolicyTypeHeader:
		var cfg HeaderMatchConfig
		if err := json.Unmarshal(tp.MatchConfig, &cfg); err != nil {
			detail.Reason = "config_parse_error"
			detail.ExpectedValue = err.Error()
			details = append(details, detail)
			return false, details
		}
		if cfg.Header == "" || cfg.Value == "" {
			detail.Reason = "invalid_config"
			detail.ExpectedValue = "header and value required"
			details = append(details, detail)
			return false, details
		}
		detail.Operator = cfg.Operator
		detail.ExpectedValue = cfg.Value
		headerValues := r.Header.Values(cfg.Header)
		detail.ActualValue = strings.Join(headerValues, ", ")
		matched := MatchHeader(r, cfg.Header, cfg.Value, cfg.Operator)
		detail.Matched = matched
		if matched {
			detail.Reason = "matched"
		} else {
			detail.Reason = "not_matched"
		}
		details = append(details, detail)
		return matched, details

	case PolicyTypeWeight:
		var cfg WeightMatchConfig
		if err := json.Unmarshal(tp.MatchConfig, &cfg); err != nil {
			detail.Reason = "config_parse_error"
			detail.ExpectedValue = err.Error()
			details = append(details, detail)
			return false, details
		}
		if cfg.Percentage < 1 || cfg.Percentage > 100 {
			detail.Reason = "invalid_config"
			detail.ExpectedValue = fmt.Sprintf("percentage %d out of range [1,100]", cfg.Percentage)
			details = append(details, detail)
			return false, details
		}
		detail.ExpectedValue = fmt.Sprintf("%d%%", cfg.Percentage)
		randValue := rand.Intn(100)
		detail.ActualValue = fmt.Sprintf("%d", randValue)
		matched := randValue < cfg.Percentage
		detail.Matched = matched
		if matched {
			detail.Reason = "weight_matched"
		} else {
			detail.Reason = "weight_not_matched"
		}
		details = append(details, detail)
		return matched, details

	case PolicyTypeQuery:
		var cfg QueryMatchConfig
		if err := json.Unmarshal(tp.MatchConfig, &cfg); err != nil {
			detail.Reason = "config_parse_error"
			detail.ExpectedValue = err.Error()
			details = append(details, detail)
			return false, details
		}
		if cfg.Key == "" || cfg.Value == "" {
			detail.Reason = "invalid_config"
			detail.ExpectedValue = "key and value required"
			details = append(details, detail)
			return false, details
		}
		detail.Operator = cfg.Operator
		detail.ExpectedValue = cfg.Value
		queryValue := r.URL.Query().Get(cfg.Key)
		detail.ActualValue = queryValue
		matched := MatchValue(queryValue, cfg.Value, cfg.Operator)
		detail.Matched = matched
		if matched {
			detail.Reason = "matched"
		} else {
			detail.Reason = "not_matched"
		}
		details = append(details, detail)
		return matched, details

	case PolicyTypeCookie:
		var cfg CookieMatchConfig
		if err := json.Unmarshal(tp.MatchConfig, &cfg); err != nil {
			detail.Reason = "config_parse_error"
			detail.ExpectedValue = err.Error()
			details = append(details, detail)
			return false, details
		}
		if cfg.Name == "" {
			detail.Reason = "invalid_config"
			detail.ExpectedValue = "cookie name required"
			details = append(details, detail)
			return false, details
		}
		detail.Operator = cfg.Operator
		detail.ExpectedValue = cfg.Value
		cookie, err := r.Cookie(cfg.Name)
		if err != nil {
			detail.Reason = "cookie_not_found"
			detail.ActualValue = "<not found>"
			details = append(details, detail)
			return false, details
		}
		detail.ActualValue = cookie.Value
		if cfg.Value == "" {
			detail.Matched = true
			detail.Reason = "cookie_exists"
			details = append(details, detail)
			return true, details
		}
		matched := MatchValue(cookie.Value, cfg.Value, cfg.Operator)
		detail.Matched = matched
		if matched {
			detail.Reason = "matched"
		} else {
			detail.Reason = "not_matched"
		}
		details = append(details, detail)
		return matched, details

	case PolicyTypeIP:
		var cfg IPMatchConfig
		if err := json.Unmarshal(tp.MatchConfig, &cfg); err != nil {
			detail.Reason = "config_parse_error"
			detail.ExpectedValue = err.Error()
			details = append(details, detail)
			return false, details
		}
		clientIP := extractClientIP(r)
		detail.ActualValue = clientIP
		detail.ExpectedValue = fmt.Sprintf("ip_list: %v, cidr_list: %v, negate: %v", cfg.IPList, cfg.CIDRList, cfg.Negate)
		matched := MatchIP(r, cfg.IPList, cfg.CIDRList)
		if cfg.Negate {
			matched = !matched
		}
		detail.Matched = matched
		if matched {
			detail.Reason = "ip_matched"
		} else {
			detail.Reason = "ip_not_matched"
		}
		details = append(details, detail)
		return matched, details

	case PolicyTypePath:
		var cfg PathMatchConfig
		if err := json.Unmarshal(tp.MatchConfig, &cfg); err != nil {
			detail.Reason = "config_parse_error"
			detail.ExpectedValue = err.Error()
			details = append(details, detail)
			return false, details
		}
		if cfg.Pattern == "" {
			detail.Reason = "invalid_config"
			detail.ExpectedValue = "path pattern required"
			details = append(details, detail)
			return false, details
		}
		detail.Operator = cfg.Operator
		detail.ExpectedValue = cfg.Pattern
		detail.ActualValue = r.URL.Path
		matched := MatchValue(r.URL.Path, cfg.Pattern, cfg.Operator)
		detail.Matched = matched
		if matched {
			detail.Reason = "path_matched"
		} else {
			detail.Reason = "path_not_matched"
		}
		details = append(details, detail)
		return matched, details

	case PolicyTypeMethod:
		var cfg MethodMatchConfig
		if err := json.Unmarshal(tp.MatchConfig, &cfg); err != nil {
			detail.Reason = "config_parse_error"
			detail.ExpectedValue = err.Error()
			details = append(details, detail)
			return false, details
		}
		if len(cfg.Methods) == 0 {
			detail.Reason = "invalid_config"
			detail.ExpectedValue = "methods list required"
			details = append(details, detail)
			return false, details
		}
		detail.ExpectedValue = strings.Join(cfg.Methods, ", ")
		detail.ActualValue = r.Method
		matched := false
		for _, m := range cfg.Methods {
			if strings.EqualFold(r.Method, m) {
				matched = true
				break
			}
		}
		if cfg.Negate {
			matched = !matched
		}
		detail.Matched = matched
		if matched {
			detail.Reason = "method_matched"
		} else {
			detail.Reason = "method_not_matched"
		}
		details = append(details, detail)
		return matched, details

	case PolicyTypeTag:
		var cfg TagMatchConfig
		if err := json.Unmarshal(tp.MatchConfig, &cfg); err != nil {
			detail.Reason = "config_parse_error"
			detail.ExpectedValue = err.Error()
			details = append(details, detail)
			return false, details
		}
		if len(cfg.Tags) == 0 {
			detail.Reason = "invalid_config"
			detail.ExpectedValue = "tags list required"
			details = append(details, detail)
			return false, details
		}
		detail.ExpectedValue = fmt.Sprintf("tags: %v, mode: %v, source: %v", cfg.Tags, cfg.MatchMode, cfg.SourceType)
		matched := MatchTags(cfg.Tags, cfg.MatchMode, cfg.SourceType, s, r)
		detail.Matched = matched
		if matched {
			detail.Reason = "tags_matched"
		} else {
			detail.Reason = "tags_not_matched"
		}
		details = append(details, detail)
		return matched, details

	case PolicyTypeConsumer:
		var cfg ConsumerMatchConfig
		if err := json.Unmarshal(tp.MatchConfig, &cfg); err != nil {
			detail.Reason = "config_parse_error"
			detail.ExpectedValue = err.Error()
			details = append(details, detail)
			return false, details
		}
		detail.ExpectedValue = fmt.Sprintf("consumer_ids: %v, tags: %v", cfg.ConsumerIDs, cfg.Tags)
		matched := MatchConsumer(&cfg, s, r)
		detail.Matched = matched
		if matched {
			detail.Reason = "consumer_matched"
		} else {
			detail.Reason = "consumer_not_matched"
		}
		details = append(details, detail)
		return matched, details

	case PolicyTypeCompound:
		var cfg CompoundMatchConfig
		if err := json.Unmarshal(tp.MatchConfig, &cfg); err != nil {
			detail.Reason = "config_parse_error"
			detail.ExpectedValue = err.Error()
			details = append(details, detail)
			return false, details
		}
		if len(cfg.Conditions) == 0 {
			detail.Reason = "invalid_config"
			detail.ExpectedValue = "conditions list required"
			details = append(details, detail)
			return false, details
		}
		matched, condDetails := s.matchCompoundConditions(&cfg, r, originalSvc)
		detail.Matched = matched
		detail.ExpectedValue = fmt.Sprintf("operator: %s, conditions: %d", cfg.Operator, len(cfg.Conditions))
		if matched {
			detail.Reason = "compound_matched"
		} else {
			detail.Reason = "compound_not_matched"
		}
		allDetails := append([]ConditionMatchDetail{detail}, condDetails...)
		return matched, allDetails

	case PolicyTypeFallback:
		var cfg FallbackMatchConfig
		if err := json.Unmarshal(tp.MatchConfig, &cfg); err != nil {
			detail.Reason = "config_parse_error"
			detail.ExpectedValue = err.Error()
			details = append(details, detail)
			return false, details
		}
		if originalSvc == nil || originalSvc.UpstreamID == uuid.Nil {
			detail.Matched = true
			detail.Reason = "fallback_no_upstream"
			detail.ActualValue = "no upstream configured"
			details = append(details, detail)
			return true, details
		}
		minTargets := 1
		if cfg.MinHealthyTargets > 0 {
			minTargets = cfg.MinHealthyTargets
		}
		healthyCount := s.CountHealthyTargets(originalSvc.UpstreamID)
		detail.ActualValue = fmt.Sprintf("healthy: %d", healthyCount)
		detail.ExpectedValue = fmt.Sprintf("min_healthy: %d, fallback_service: %s", minTargets, cfg.FallbackServiceID)
		matched := healthyCount < minTargets
		detail.Matched = matched
		if matched {
			detail.Reason = "fallback_activated"
		} else {
			detail.Reason = "fallback_not_needed"
		}
		details = append(details, detail)
		return matched, details

	default:
		detail.Reason = "unknown_policy_type"
		detail.ExpectedValue = tp.Type
		details = append(details, detail)
		return false, details
	}
}

func (s *ConfigSnapshot) matchCompoundConditions(cfg *CompoundMatchConfig, r *http.Request, originalSvc *service.Service) (bool, []ConditionMatchDetail) {
	details := make([]ConditionMatchDetail, 0, len(cfg.Conditions))

	if cfg.Operator == CompoundOperatorAND {
		allMatched := true
		for _, cond := range cfg.Conditions {
			matched, detail := s.matchSingleCondition(&cond, r, originalSvc)
			details = append(details, detail)
			if !matched {
				allMatched = false
			}
		}
		return allMatched, details
	} else if cfg.Operator == CompoundOperatorOR {
		anyMatched := false
		for _, cond := range cfg.Conditions {
			matched, detail := s.matchSingleCondition(&cond, r, originalSvc)
			details = append(details, detail)
			if matched {
				anyMatched = true
			}
		}
		return anyMatched, details
	}

	detail := ConditionMatchDetail{
		ConditionType: "compound",
		Matched:       false,
		Reason:        "invalid_operator",
	}
	return false, append(details, detail)
}

func (s *ConfigSnapshot) matchSingleCondition(cond *CompoundCondition, r *http.Request, originalSvc *service.Service) (bool, ConditionMatchDetail) {
	detail := ConditionMatchDetail{
		ConditionType: cond.Type,
		Matched:       false,
	}

	switch cond.Type {
	case PolicyTypeHeader:
		var cfg HeaderMatchConfig
		if err := json.Unmarshal(cond.MatchConfig, &cfg); err != nil {
			detail.Reason = "config_parse_error"
			detail.ExpectedValue = err.Error()
			return false, detail
		}
		if cfg.Header == "" || cfg.Value == "" {
			detail.Reason = "invalid_config"
			detail.ExpectedValue = "header and value required"
			return false, detail
		}
		detail.Operator = cfg.Operator
		detail.ExpectedValue = cfg.Value
		headerValues := r.Header.Values(cfg.Header)
		detail.ActualValue = strings.Join(headerValues, ", ")
		matched := MatchHeader(r, cfg.Header, cfg.Value, cfg.Operator)
		detail.Matched = matched
		if matched {
			detail.Reason = "matched"
		} else {
			detail.Reason = "not_matched"
		}
		return matched, detail

	case PolicyTypeWeight:
		var cfg WeightMatchConfig
		if err := json.Unmarshal(cond.MatchConfig, &cfg); err != nil {
			detail.Reason = "config_parse_error"
			detail.ExpectedValue = err.Error()
			return false, detail
		}
		if cfg.Percentage < 1 || cfg.Percentage > 100 {
			detail.Reason = "invalid_config"
			detail.ExpectedValue = fmt.Sprintf("percentage %d out of range [1,100]", cfg.Percentage)
			return false, detail
		}
		detail.ExpectedValue = fmt.Sprintf("%d%%", cfg.Percentage)
		randValue := rand.Intn(100)
		detail.ActualValue = fmt.Sprintf("%d", randValue)
		matched := randValue < cfg.Percentage
		detail.Matched = matched
		if matched {
			detail.Reason = "weight_matched"
		} else {
			detail.Reason = "weight_not_matched"
		}
		return matched, detail

	case PolicyTypeQuery:
		var cfg QueryMatchConfig
		if err := json.Unmarshal(cond.MatchConfig, &cfg); err != nil {
			detail.Reason = "config_parse_error"
			detail.ExpectedValue = err.Error()
			return false, detail
		}
		if cfg.Key == "" || cfg.Value == "" {
			detail.Reason = "invalid_config"
			detail.ExpectedValue = "key and value required"
			return false, detail
		}
		detail.Operator = cfg.Operator
		detail.ExpectedValue = cfg.Value
		queryValue := r.URL.Query().Get(cfg.Key)
		detail.ActualValue = queryValue
		matched := MatchValue(queryValue, cfg.Value, cfg.Operator)
		detail.Matched = matched
		if matched {
			detail.Reason = "matched"
		} else {
			detail.Reason = "not_matched"
		}
		return matched, detail

	case PolicyTypeCookie:
		var cfg CookieMatchConfig
		if err := json.Unmarshal(cond.MatchConfig, &cfg); err != nil {
			detail.Reason = "config_parse_error"
			detail.ExpectedValue = err.Error()
			return false, detail
		}
		if cfg.Name == "" {
			detail.Reason = "invalid_config"
			detail.ExpectedValue = "cookie name required"
			return false, detail
		}
		detail.Operator = cfg.Operator
		detail.ExpectedValue = cfg.Value
		cookie, err := r.Cookie(cfg.Name)
		if err != nil {
			detail.Reason = "cookie_not_found"
			detail.ActualValue = "<not found>"
			return false, detail
		}
		detail.ActualValue = cookie.Value
		if cfg.Value == "" {
			detail.Matched = true
			detail.Reason = "cookie_exists"
			return true, detail
		}
		matched := MatchValue(cookie.Value, cfg.Value, cfg.Operator)
		detail.Matched = matched
		if matched {
			detail.Reason = "matched"
		} else {
			detail.Reason = "not_matched"
		}
		return matched, detail

	case PolicyTypeIP:
		var cfg IPMatchConfig
		if err := json.Unmarshal(cond.MatchConfig, &cfg); err != nil {
			detail.Reason = "config_parse_error"
			detail.ExpectedValue = err.Error()
			return false, detail
		}
		clientIP := extractClientIP(r)
		detail.ActualValue = clientIP
		detail.ExpectedValue = fmt.Sprintf("ip_list: %v, cidr_list: %v, negate: %v", cfg.IPList, cfg.CIDRList, cfg.Negate)
		matched := MatchIP(r, cfg.IPList, cfg.CIDRList)
		if cfg.Negate {
			matched = !matched
		}
		detail.Matched = matched
		if matched {
			detail.Reason = "ip_matched"
		} else {
			detail.Reason = "ip_not_matched"
		}
		return matched, detail

	case PolicyTypePath:
		var cfg PathMatchConfig
		if err := json.Unmarshal(cond.MatchConfig, &cfg); err != nil {
			detail.Reason = "config_parse_error"
			detail.ExpectedValue = err.Error()
			return false, detail
		}
		if cfg.Pattern == "" {
			detail.Reason = "invalid_config"
			detail.ExpectedValue = "path pattern required"
			return false, detail
		}
		detail.Operator = cfg.Operator
		detail.ExpectedValue = cfg.Pattern
		detail.ActualValue = r.URL.Path
		matched := MatchValue(r.URL.Path, cfg.Pattern, cfg.Operator)
		detail.Matched = matched
		if matched {
			detail.Reason = "path_matched"
		} else {
			detail.Reason = "path_not_matched"
		}
		return matched, detail

	case PolicyTypeMethod:
		var cfg MethodMatchConfig
		if err := json.Unmarshal(cond.MatchConfig, &cfg); err != nil {
			detail.Reason = "config_parse_error"
			detail.ExpectedValue = err.Error()
			return false, detail
		}
		if len(cfg.Methods) == 0 {
			detail.Reason = "invalid_config"
			detail.ExpectedValue = "methods list required"
			return false, detail
		}
		detail.ExpectedValue = strings.Join(cfg.Methods, ", ")
		detail.ActualValue = r.Method
		matched := false
		for _, m := range cfg.Methods {
			if strings.EqualFold(r.Method, m) {
				matched = true
				break
			}
		}
		if cfg.Negate {
			matched = !matched
		}
		detail.Matched = matched
		if matched {
			detail.Reason = "method_matched"
		} else {
			detail.Reason = "method_not_matched"
		}
		return matched, detail

	case PolicyTypeTag:
		var cfg TagMatchConfig
		if err := json.Unmarshal(cond.MatchConfig, &cfg); err != nil {
			detail.Reason = "config_parse_error"
			detail.ExpectedValue = err.Error()
			return false, detail
		}
		if len(cfg.Tags) == 0 {
			detail.Reason = "invalid_config"
			detail.ExpectedValue = "tags list required"
			return false, detail
		}
		detail.ExpectedValue = fmt.Sprintf("tags: %v, mode: %v, source: %v", cfg.Tags, cfg.MatchMode, cfg.SourceType)
		matched := MatchTags(cfg.Tags, cfg.MatchMode, cfg.SourceType, s, r)
		detail.Matched = matched
		if matched {
			detail.Reason = "tags_matched"
		} else {
			detail.Reason = "tags_not_matched"
		}
		return matched, detail

	case PolicyTypeConsumer:
		var cfg ConsumerMatchConfig
		if err := json.Unmarshal(cond.MatchConfig, &cfg); err != nil {
			detail.Reason = "config_parse_error"
			detail.ExpectedValue = err.Error()
			return false, detail
		}
		detail.ExpectedValue = fmt.Sprintf("consumer_ids: %v, tags: %v", cfg.ConsumerIDs, cfg.Tags)
		matched := MatchConsumer(&cfg, s, r)
		detail.Matched = matched
		if matched {
			detail.Reason = "consumer_matched"
		} else {
			detail.Reason = "consumer_not_matched"
		}
		return matched, detail

	default:
		detail.Reason = "unknown_condition_type"
		detail.ExpectedValue = cond.Type
		return false, detail
	}
}

func MatchHeader(r *http.Request, headerName, pattern string, operator MatchOperator) bool {
	headerValues := r.Header.Values(headerName)
	if len(headerValues) == 0 {
		return false
	}
	for _, v := range headerValues {
		if MatchValue(v, pattern, operator) {
			return true
		}
	}
	return false
}

func MatchValue(value, pattern string, operator MatchOperator) bool {
	switch operator {
	case MatchOperatorExact, "":
		return value == pattern
	case MatchOperatorPrefix:
		return strings.HasPrefix(value, pattern)
	case MatchOperatorSuffix:
		return strings.HasSuffix(value, pattern)
	case MatchOperatorContains:
		return strings.Contains(value, pattern)
	case MatchOperatorRegex:
		re, err := regexp.Compile(pattern)
		if err != nil {
			return false
		}
		return re.MatchString(value)
	case MatchOperatorNotExact:
		return value != pattern
	case MatchOperatorNotContains:
		return !strings.Contains(value, pattern)
	case MatchOperatorGreaterThan, MatchOperatorGreaterEqual, MatchOperatorLessThan, MatchOperatorLessEqual:
		return compareNumeric(value, pattern, operator)
	default:
		return value == pattern
	}
}

func compareNumeric(value, pattern string, operator MatchOperator) bool {
	v, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return false
	}
	p, err := strconv.ParseFloat(pattern, 64)
	if err != nil {
		return false
	}
	switch operator {
	case MatchOperatorGreaterThan:
		return v > p
	case MatchOperatorGreaterEqual:
		return v >= p
	case MatchOperatorLessThan:
		return v < p
	case MatchOperatorLessEqual:
		return v <= p
	default:
		return false
	}
}

func MatchIP(r *http.Request, ipList, cidrList []string) bool {
	clientIP := extractClientIP(r)
	if clientIP == "" {
		return false
	}

	parsedIP := net.ParseIP(clientIP)
	if parsedIP == nil {
		return false
	}

	for _, ip := range ipList {
		if parsedIP.Equal(net.ParseIP(ip)) {
			return true
		}
	}

	for _, cidr := range cidrList {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if ipnet.Contains(parsedIP) {
			return true
		}
	}

	return false
}

func extractClientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		ips := strings.Split(forwarded, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func MatchTags(tags []string, matchMode TagMatchMode, sourceType string, s *ConfigSnapshot, r *http.Request) bool {
	var sourceTags []string

	switch sourceType {
	case "consumer":
		consumerID := getConsumerIDFromRequest(s, r)
		if consumerID != nil {
			creds := s.GetCredentialsByConsumer(*consumerID)
			if len(creds) > 0 {
				for _, cred := range creds {
					if cred.Tags != nil {
						sourceTags = append(sourceTags, cred.Tags...)
					}
				}
			}
		}
	case "service":
		if svcID := getServiceIDFromRequest(s, r); svcID != nil {
			if svc, ok := s.GetService(*svcID); ok {
				sourceTags = svc.Tags
			}
		}
	case "route":
		if routeID := getRouteIDFromRequest(s, r); routeID != nil {
			if route := getRouteByID(s, *routeID); route != nil {
				sourceTags = route.Tags
			}
		}
	default:
		return false
	}

	if len(sourceTags) == 0 {
		return false
	}

	switch matchMode {
	case TagMatchModeAny, "":
		for _, tag := range tags {
			for _, st := range sourceTags {
				if tag == st {
					return true
				}
			}
		}
		return false

	case TagMatchModeAll:
		for _, tag := range tags {
			found := false
			for _, st := range sourceTags {
				if tag == st {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true

	case TagMatchModeExact:
		if len(tags) != len(sourceTags) {
			return false
		}
		tagMap := make(map[string]bool)
		for _, t := range tags {
			tagMap[t] = true
		}
		for _, st := range sourceTags {
			if !tagMap[st] {
				return false
			}
		}
		return true

	default:
		return false
	}
}

func MatchConsumer(cfg *ConsumerMatchConfig, s *ConfigSnapshot, r *http.Request) bool {
	consumerID := getConsumerIDFromRequest(s, r)
	if consumerID == nil {
		return false
	}

	for _, cid := range cfg.ConsumerIDs {
		if cid == *consumerID {
			return true
		}
	}

	if len(cfg.Tags) > 0 {
		creds := s.GetCredentialsByConsumer(*consumerID)
		var consumerTags []string
		for _, cred := range creds {
			if cred.Tags != nil {
				consumerTags = append(consumerTags, cred.Tags...)
			}
		}

		if len(consumerTags) > 0 {
			switch cfg.MatchMode {
			case TagMatchModeAny, "":
				for _, tag := range cfg.Tags {
					for _, ct := range consumerTags {
						if tag == ct {
							return true
						}
					}
				}
			case TagMatchModeAll:
				allFound := true
				for _, tag := range cfg.Tags {
					found := false
					for _, ct := range consumerTags {
						if tag == ct {
							found = true
							break
						}
					}
					if !found {
						allFound = false
						break
					}
				}
				if allFound {
					return true
				}
			}
		}
	}

	return false
}

func getConsumerIDFromRequest(s *ConfigSnapshot, r *http.Request) *uuid.UUID {
	defaultKeyNames := []string{"apikey", "X-API-Key", "X-apikey"}

	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		key := strings.TrimPrefix(authHeader, "Bearer ")
		if cred, ok := s.GetCredentialByKey(key); ok {
			return &cred.ConsumerID
		}
	}

	for _, keyName := range defaultKeyNames {
		if apiKey := r.Header.Get(keyName); apiKey != "" {
			if cred, ok := s.GetCredentialByKey(apiKey); ok {
				return &cred.ConsumerID
			}
		}
	}

	for _, keyName := range defaultKeyNames {
		if apiKey := r.URL.Query().Get(keyName); apiKey != "" {
			if cred, ok := s.GetCredentialByKey(apiKey); ok {
				return &cred.ConsumerID
			}
		}
	}

	return nil
}

func getServiceIDFromRequest(s *ConfigSnapshot, r *http.Request) *uuid.UUID {
	for _, matcher := range s.routeMatchers {
		if matched, _ := matcher.Match(r); matched {
			svcID := matcher.Route().ServiceID
			return &svcID
		}
	}
	return nil
}

func getRouteIDFromRequest(s *ConfigSnapshot, r *http.Request) *uuid.UUID {
	for _, matcher := range s.routeMatchers {
		if matched, _ := matcher.Match(r); matched {
			routeID := matcher.Route().ID
			return &routeID
		}
	}
	return nil
}

func getRouteByID(s *ConfigSnapshot, id uuid.UUID) *route.Route {
	for _, r := range s.Routes {
		if r.ID == id {
			return r
		}
	}
	return nil
}

func (s *ConfigSnapshot) GetService(id uuid.UUID) (*service.Service, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	svc, ok := s.Services[id]
	return svc, ok
}

func (s *ConfigSnapshot) GetUpstream(id uuid.UUID) (*upstream.Upstream, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.Upstreams[id]
	return u, ok
}

func (s *ConfigSnapshot) GetTargets(upstreamID uuid.UUID) ([]*target.Target, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.Targets[upstreamID]
	return t, ok
}

func (s *ConfigSnapshot) GetBalancer(upstreamID uuid.UUID) (*Balancer, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.balancers[upstreamID]
	return b, ok
}

type ConditionMatchDetail struct {
	ConditionType string
	Matched       bool
	Reason        string
	ActualValue   string
	ExpectedValue string
	Operator      MatchOperator
}

type PolicyMatchDetail struct {
	PolicyID           uuid.UUID
	PolicyName         string
	PolicyType         string
	Priority           int
	Enabled            bool
	Matched            bool
	Selected           bool
	TargetServiceID    uuid.UUID
	ConditionDetails   []ConditionMatchDetail
	SkipReason         string
}

type MatchedRoute struct {
	Route             *route.Route
	Service           *service.Service
	Upstream          *upstream.Upstream
	Balancer          *Balancer
	Params            map[string]string

	OriginalService   *service.Service
	EffectiveService  *service.Service
	TrafficPolicyHit  bool
	HitPolicyID       uuid.UUID
	HitPolicyType     string

	PolicyMatchDetails []PolicyMatchDetail
}

type RouteMatcher struct {
	route   *route.Route
	pathRe  []*regexp.Regexp
	pathLit []string
}

func NewRouteMatcher(r *route.Route) (*RouteMatcher, error) {
	if len(r.Methods) == 0 && len(r.Hosts) == 0 && len(r.Paths) == 0 && len(r.Headers) == 0 {
		return nil, fmt.Errorf("route must have at least one match condition")
	}

	m := &RouteMatcher{
		route:   r,
		pathRe:  make([]*regexp.Regexp, 0),
		pathLit: make([]string, 0),
	}

	for _, p := range r.Paths {
		if strings.ContainsAny(p, "*()?+[]") {
			re, err := pathToRegexp(p)
			if err != nil {
				return nil, fmt.Errorf("invalid path pattern %q: %w", p, err)
			}
			m.pathRe = append(m.pathRe, re)
		} else {
			m.pathLit = append(m.pathLit, p)
		}
	}

	return m, nil
}

func (m *RouteMatcher) Route() *route.Route {
	return m.route
}

func (m *RouteMatcher) Match(r *http.Request) (bool, map[string]string) {
	params := make(map[string]string)

	if len(m.route.Methods) > 0 {
		methodMatch := false
		for _, method := range m.route.Methods {
			if r.Method == method {
				methodMatch = true
				break
			}
		}
		if !methodMatch {
			return false, nil
		}
	}

	if len(m.route.Hosts) > 0 {
		hostMatch := false
		requestHost := r.Host
		if idx := strings.Index(requestHost, ":"); idx != -1 {
			requestHost = requestHost[:idx]
		}
		for _, host := range m.route.Hosts {
			if strings.EqualFold(requestHost, host) {
				hostMatch = true
				break
			}
		}
		if !hostMatch {
			return false, nil
		}
	}

	if len(m.route.Paths) > 0 {
		pathMatch := false
		requestPath := r.URL.Path

		for _, p := range m.pathLit {
			if strings.HasPrefix(requestPath, p) {
				pathMatch = true
				break
			}
		}

		if !pathMatch {
			for _, re := range m.pathRe {
				if matches := re.FindStringSubmatch(requestPath); matches != nil {
					pathMatch = true
					names := re.SubexpNames()
					for i, name := range names {
						if name != "" && i < len(matches) {
							params[name] = matches[i]
						}
					}
					break
				}
			}
		}

		if !pathMatch {
			return false, nil
		}
	}

	if len(m.route.Headers) > 0 {
		for key, values := range m.route.Headers {
			found := false
			requestHeaders := r.Header.Values(key)
			for _, v := range values {
				for _, rv := range requestHeaders {
					if strings.EqualFold(v, rv) {
						found = true
						break
					}
				}
				if found {
					break
				}
			}
			if !found {
				return false, nil
			}
		}
	}

	return true, params
}

func pathToRegexp(pattern string) (*regexp.Regexp, error) {
	pattern = "^" + regexp.QuoteMeta(pattern) + "$"
	pattern = strings.ReplaceAll(pattern, `\*`, "(.*)")
	return regexp.Compile(pattern)
}

type Balancer struct {
	upstream     *upstream.Upstream
	targets      []*target.Target
	currentIndex uint32
}

func NewBalancer(u *upstream.Upstream, targets []*target.Target) *Balancer {
	return &Balancer{
		upstream: u,
		targets:  targets,
	}
}

func (b *Balancer) Upstream() *upstream.Upstream {
	return b.upstream
}

func (b *Balancer) Next() (*target.Target, bool) {
	if len(b.targets) == 0 {
		return nil, false
	}

	switch b.upstream.Algorithm {
	case upstream.AlgorithmRoundRobin:
		idx := atomic.AddUint32(&b.currentIndex, 1)
		return b.targets[(idx-1)%uint32(len(b.targets))], true
	default:
		idx := atomic.AddUint32(&b.currentIndex, 1)
		return b.targets[(idx-1)%uint32(len(b.targets))], true
	}
}

func (b *Balancer) Targets() []*target.Target {
	return b.targets
}

func ToDomainTrafficPolicy(tp *TrafficPolicy) *trafficpolicy.TrafficPolicy {
	dom := &trafficpolicy.TrafficPolicy{
		ID:              tp.ID,
		Name:            tp.Name,
		RouteID:         tp.RouteID,
		Priority:        tp.Priority,
		Type:            trafficpolicy.PolicyType(tp.Type),
		MatchConfig:     tp.MatchConfig,
		TargetServiceID: tp.TargetServiceID,
		Enabled:         tp.Enabled,
		Tags:            tp.Tags,
		RuleSetID:       tp.RuleSetID,
	}
	return dom
}

func FromDomainTrafficPolicy(tp *trafficpolicy.TrafficPolicy) *TrafficPolicy {
	return &TrafficPolicy{
		ID:              tp.ID,
		Name:            tp.Name,
		RouteID:         tp.RouteID,
		Priority:        tp.Priority,
		Type:            string(tp.Type),
		MatchConfig:     tp.MatchConfig,
		TargetServiceID: tp.TargetServiceID,
		Enabled:         tp.Enabled,
		Tags:            tp.Tags,
		RuleSetID:       tp.RuleSetID,
	}
}
