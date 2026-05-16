package trafficpolicy

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrTrafficPolicyRouteIDRequired            = errors.New("route_id is required")
	ErrTrafficPolicyTargetServiceIDRequired    = errors.New("target_service_id is required")
	ErrTrafficPolicyNameTooLong                = errors.New("traffic policy name must be <= 255 characters")
	ErrTrafficPolicyInvalidType                 = errors.New("traffic policy type must be 'header', 'weight', 'consumer', 'query', 'cookie', 'ip', 'path', 'method', 'tag', 'compound', or 'fallback'")
	ErrTrafficPolicyPriorityRequired            = errors.New("priority is required and must be >= 0")
	ErrTrafficPolicyMatchConfigRequired         = errors.New("match_config is required")
	ErrTrafficPolicyInvalidHeaderConfig         = errors.New("header policy requires 'header' and 'value' in match_config")
	ErrTrafficPolicyInvalidWeightConfig         = errors.New("weight policy requires 'percentage' (1-100) in match_config")
	ErrTrafficPolicyInvalidQueryConfig          = errors.New("query policy requires 'key' and 'value' in match_config")
	ErrTrafficPolicyInvalidCookieConfig         = errors.New("cookie policy requires 'name' in match_config")
	ErrTrafficPolicyInvalidIPConfig             = errors.New("ip policy requires 'ip_list' or 'cidr_list' in match_config")
	ErrTrafficPolicyInvalidPathConfig           = errors.New("path policy requires 'pattern' in match_config")
	ErrTrafficPolicyInvalidTagConfig            = errors.New("tag policy requires 'tags' in match_config")
	ErrTrafficPolicyInvalidCompoundConfig       = errors.New("compound policy requires 'conditions' and 'operator' in match_config")
	ErrTrafficPolicyInvalidFallbackConfig       = errors.New("fallback policy requires 'fallback_service_id' in match_config")
	ErrTrafficPolicyInvalidMatchOperator        = errors.New("invalid match operator, must be 'exact', 'prefix', 'suffix', 'contains', 'regex', 'not_exact', 'not_contains', 'greater_than', 'less_than', 'greater_equal', 'less_equal'")
	ErrTrafficPolicyInvalidCompoundOperator     = errors.New("invalid compound operator, must be 'and' or 'or'")
)

type PolicyType string

const (
	PolicyTypeHeader   PolicyType = "header"
	PolicyTypeWeight   PolicyType = "weight"
	PolicyTypeConsumer PolicyType = "consumer"
	PolicyTypeQuery    PolicyType = "query"
	PolicyTypeCookie   PolicyType = "cookie"
	PolicyTypeIP       PolicyType = "ip"
	PolicyTypePath     PolicyType = "path"
	PolicyTypeMethod   PolicyType = "method"
	PolicyTypeTag      PolicyType = "tag"
	PolicyTypeCompound PolicyType = "compound"
	PolicyTypeFallback PolicyType = "fallback"
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
	TagMatchModeAny    TagMatchMode = "any"
	TagMatchModeAll    TagMatchMode = "all"
	TagMatchModeExact  TagMatchMode = "exact"
)

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
	ConsumerIDs []uuid.UUID `json:"consumer_ids,omitempty"`
	Tags        []string    `json:"tags,omitempty"`
	MatchMode   TagMatchMode `json:"match_mode,omitempty"`
}

type CompoundCondition struct {
	Type        PolicyType          `json:"type"`
	MatchConfig json.RawMessage     `json:"match_config"`
}

type CompoundMatchConfig struct {
	Operator   CompoundOperator    `json:"operator"`
	Conditions []CompoundCondition `json:"conditions"`
}

type FallbackMatchConfig struct {
	FallbackServiceID uuid.UUID `json:"fallback_service_id"`
	HealthCheckType   string    `json:"health_check_type,omitempty"`
	MinHealthyTargets int       `json:"min_healthy_targets,omitempty"`
}

type TrafficPolicy struct {
	ID              uuid.UUID       `json:"id"`
	TenantID        uuid.UUID       `json:"tenant_id"`
	Name            string          `json:"name"`
	RouteID         uuid.UUID       `json:"route_id"`
	Priority        int             `json:"priority"`
	Type            PolicyType      `json:"type"`
	MatchConfig     json.RawMessage `json:"match_config"`
	TargetServiceID uuid.UUID       `json:"target_service_id"`
	Enabled         bool            `json:"enabled"`
	Tags            []string        `json:"tags"`
	RuleSetID       *uuid.UUID      `json:"rule_set_id,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type RuleSet struct {
	ID          uuid.UUID   `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Tags        []string    `json:"tags"`
	PolicyIDs   []uuid.UUID `json:"policy_ids"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

func New(routeID uuid.UUID, targetServiceID uuid.UUID) (*TrafficPolicy, error) {
	if routeID == uuid.Nil {
		return nil, ErrTrafficPolicyRouteIDRequired
	}
	if targetServiceID == uuid.Nil {
		return nil, ErrTrafficPolicyTargetServiceIDRequired
	}

	now := time.Now()
	return &TrafficPolicy{
		ID:              uuid.New(),
		RouteID:         routeID,
		Priority:        0,
		Type:            PolicyTypeHeader,
		MatchConfig:     json.RawMessage("{}"),
		TargetServiceID: targetServiceID,
		Enabled:         true,
		CreatedAt:       now,
		UpdatedAt:       now,
	}, nil
}

func (p *TrafficPolicy) Validate() error {
	if p.RouteID == uuid.Nil {
		return ErrTrafficPolicyRouteIDRequired
	}
	if p.TargetServiceID == uuid.Nil && p.Type != PolicyTypeFallback {
		return ErrTrafficPolicyTargetServiceIDRequired
	}
	if len(p.Name) > 255 {
		return ErrTrafficPolicyNameTooLong
	}
	if p.Priority < 0 {
		return ErrTrafficPolicyPriorityRequired
	}

	validTypes := map[PolicyType]bool{
		PolicyTypeHeader:   true,
		PolicyTypeWeight:   true,
		PolicyTypeConsumer: true,
		PolicyTypeQuery:    true,
		PolicyTypeCookie:   true,
		PolicyTypeIP:       true,
		PolicyTypePath:     true,
		PolicyTypeMethod:   true,
		PolicyTypeTag:      true,
		PolicyTypeCompound: true,
		PolicyTypeFallback: true,
	}
	if !validTypes[p.Type] {
		return ErrTrafficPolicyInvalidType
	}

	if len(p.MatchConfig) == 0 || string(p.MatchConfig) == "{}" || string(p.MatchConfig) == "null" {
		return ErrTrafficPolicyMatchConfigRequired
	}

	switch p.Type {
	case PolicyTypeHeader:
		var cfg HeaderMatchConfig
		if err := json.Unmarshal(p.MatchConfig, &cfg); err != nil {
			return ErrTrafficPolicyInvalidHeaderConfig
		}
		if strings.TrimSpace(cfg.Header) == "" || strings.TrimSpace(cfg.Value) == "" {
			return ErrTrafficPolicyInvalidHeaderConfig
		}
		if cfg.Operator != "" && !isValidMatchOperator(cfg.Operator) {
			return ErrTrafficPolicyInvalidMatchOperator
		}

	case PolicyTypeWeight:
		var cfg WeightMatchConfig
		if err := json.Unmarshal(p.MatchConfig, &cfg); err != nil {
			return ErrTrafficPolicyInvalidWeightConfig
		}
		if cfg.Percentage < 1 || cfg.Percentage > 100 {
			return ErrTrafficPolicyInvalidWeightConfig
		}

	case PolicyTypeQuery:
		var cfg QueryMatchConfig
		if err := json.Unmarshal(p.MatchConfig, &cfg); err != nil {
			return ErrTrafficPolicyInvalidQueryConfig
		}
		if strings.TrimSpace(cfg.Key) == "" || strings.TrimSpace(cfg.Value) == "" {
			return ErrTrafficPolicyInvalidQueryConfig
		}
		if cfg.Operator != "" && !isValidMatchOperator(cfg.Operator) {
			return ErrTrafficPolicyInvalidMatchOperator
		}

	case PolicyTypeCookie:
		var cfg CookieMatchConfig
		if err := json.Unmarshal(p.MatchConfig, &cfg); err != nil {
			return ErrTrafficPolicyInvalidCookieConfig
		}
		if strings.TrimSpace(cfg.Name) == "" {
			return ErrTrafficPolicyInvalidCookieConfig
		}
		if cfg.Operator != "" && !isValidMatchOperator(cfg.Operator) {
			return ErrTrafficPolicyInvalidMatchOperator
		}

	case PolicyTypeIP:
		var cfg IPMatchConfig
		if err := json.Unmarshal(p.MatchConfig, &cfg); err != nil {
			return ErrTrafficPolicyInvalidIPConfig
		}
		if len(cfg.IPList) == 0 && len(cfg.CIDRList) == 0 {
			return ErrTrafficPolicyInvalidIPConfig
		}

	case PolicyTypePath:
		var cfg PathMatchConfig
		if err := json.Unmarshal(p.MatchConfig, &cfg); err != nil {
			return ErrTrafficPolicyInvalidPathConfig
		}
		if strings.TrimSpace(cfg.Pattern) == "" {
			return ErrTrafficPolicyInvalidPathConfig
		}
		if cfg.Operator != "" && !isValidMatchOperator(cfg.Operator) {
			return ErrTrafficPolicyInvalidMatchOperator
		}

	case PolicyTypeMethod:
		var cfg MethodMatchConfig
		if err := json.Unmarshal(p.MatchConfig, &cfg); err != nil {
			return fmt.Errorf("method policy requires 'methods' in match_config: %w", err)
		}
		if len(cfg.Methods) == 0 {
			return fmt.Errorf("method policy requires at least one method in 'methods'")
		}

	case PolicyTypeTag:
		var cfg TagMatchConfig
		if err := json.Unmarshal(p.MatchConfig, &cfg); err != nil {
			return ErrTrafficPolicyInvalidTagConfig
		}
		if len(cfg.Tags) == 0 {
			return ErrTrafficPolicyInvalidTagConfig
		}

	case PolicyTypeConsumer:
		var cfg ConsumerMatchConfig
		if err := json.Unmarshal(p.MatchConfig, &cfg); err != nil {
			return fmt.Errorf("consumer policy requires 'consumer_ids' or 'tags' in match_config: %w", err)
		}
		if len(cfg.ConsumerIDs) == 0 && len(cfg.Tags) == 0 {
			return fmt.Errorf("consumer policy requires at least one of 'consumer_ids' or 'tags'")
		}

	case PolicyTypeCompound:
		var cfg CompoundMatchConfig
		if err := json.Unmarshal(p.MatchConfig, &cfg); err != nil {
			return ErrTrafficPolicyInvalidCompoundConfig
		}
		if len(cfg.Conditions) == 0 {
			return ErrTrafficPolicyInvalidCompoundConfig
		}
		if cfg.Operator != CompoundOperatorAND && cfg.Operator != CompoundOperatorOR {
			return ErrTrafficPolicyInvalidCompoundOperator
		}
		for i, cond := range cfg.Conditions {
			if !validTypes[cond.Type] {
				return fmt.Errorf("compound condition %d: %w", i, ErrTrafficPolicyInvalidType)
			}
			if len(cond.MatchConfig) == 0 || string(cond.MatchConfig) == "{}" || string(cond.MatchConfig) == "null" {
				return fmt.Errorf("compound condition %d: %w", i, ErrTrafficPolicyMatchConfigRequired)
			}
		}

	case PolicyTypeFallback:
		var cfg FallbackMatchConfig
		if err := json.Unmarshal(p.MatchConfig, &cfg); err != nil {
			return ErrTrafficPolicyInvalidFallbackConfig
		}
		if cfg.FallbackServiceID == uuid.Nil {
			return ErrTrafficPolicyInvalidFallbackConfig
		}
	}

	return nil
}

func isValidMatchOperator(op MatchOperator) bool {
	validOps := map[MatchOperator]bool{
		MatchOperatorExact:        true,
		MatchOperatorPrefix:       true,
		MatchOperatorSuffix:       true,
		MatchOperatorContains:     true,
		MatchOperatorRegex:        true,
		MatchOperatorNotExact:     true,
		MatchOperatorNotContains:  true,
		MatchOperatorGreaterThan:  true,
		MatchOperatorLessThan:     true,
		MatchOperatorGreaterEqual: true,
		MatchOperatorLessEqual:    true,
	}
	return validOps[op]
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
	default:
		return value == pattern
	}
}

func (p *TrafficPolicy) Enable() {
	p.Enabled = true
}

func (p *TrafficPolicy) Disable() {
	p.Enabled = false
}

func (p *TrafficPolicy) AddTag(tag string) {
	for _, t := range p.Tags {
		if t == tag {
			return
		}
	}
	p.Tags = append(p.Tags, tag)
}

func (p *TrafficPolicy) RemoveTag(tag string) {
	newTags := make([]string, 0, len(p.Tags))
	for _, t := range p.Tags {
		if t != tag {
			newTags = append(newTags, t)
		}
	}
	p.Tags = newTags
}

func (p *TrafficPolicy) SetHeaderMatchConfig(header, value string) error {
	cfg := HeaderMatchConfig{
		Header:   header,
		Value:    value,
		Operator: MatchOperatorExact,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	p.Type = PolicyTypeHeader
	p.MatchConfig = data
	return nil
}

func (p *TrafficPolicy) SetHeaderMatchConfigWithOperator(header, value string, operator MatchOperator) error {
	if !isValidMatchOperator(operator) {
		return ErrTrafficPolicyInvalidMatchOperator
	}
	cfg := HeaderMatchConfig{
		Header:   header,
		Value:    value,
		Operator: operator,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	p.Type = PolicyTypeHeader
	p.MatchConfig = data
	return nil
}

func (p *TrafficPolicy) SetWeightMatchConfig(percentage int) error {
	if percentage < 1 || percentage > 100 {
		return ErrTrafficPolicyInvalidWeightConfig
	}
	cfg := WeightMatchConfig{
		Percentage: percentage,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	p.Type = PolicyTypeWeight
	p.MatchConfig = data
	return nil
}

func (p *TrafficPolicy) SetQueryMatchConfig(key, value string, operator MatchOperator) error {
	if operator != "" && !isValidMatchOperator(operator) {
		return ErrTrafficPolicyInvalidMatchOperator
	}
	cfg := QueryMatchConfig{
		Key:      key,
		Value:    value,
		Operator: operator,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	p.Type = PolicyTypeQuery
	p.MatchConfig = data
	return nil
}

func (p *TrafficPolicy) SetCookieMatchConfig(name, value string, operator MatchOperator) error {
	if operator != "" && !isValidMatchOperator(operator) {
		return ErrTrafficPolicyInvalidMatchOperator
	}
	cfg := CookieMatchConfig{
		Name:     name,
		Value:    value,
		Operator: operator,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	p.Type = PolicyTypeCookie
	p.MatchConfig = data
	return nil
}

func (p *TrafficPolicy) SetIPMatchConfig(ipList, cidrList []string, negate bool) error {
	if len(ipList) == 0 && len(cidrList) == 0 {
		return ErrTrafficPolicyInvalidIPConfig
	}
	cfg := IPMatchConfig{
		IPList:   ipList,
		CIDRList: cidrList,
		Negate:   negate,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	p.Type = PolicyTypeIP
	p.MatchConfig = data
	return nil
}

func (p *TrafficPolicy) SetPathMatchConfig(pattern string, operator MatchOperator) error {
	if operator != "" && !isValidMatchOperator(operator) {
		return ErrTrafficPolicyInvalidMatchOperator
	}
	cfg := PathMatchConfig{
		Pattern:  pattern,
		Operator: operator,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	p.Type = PolicyTypePath
	p.MatchConfig = data
	return nil
}

func (p *TrafficPolicy) SetMethodMatchConfig(methods []string, negate bool) error {
	if len(methods) == 0 {
		return fmt.Errorf("at least one method is required")
	}
	cfg := MethodMatchConfig{
		Methods: methods,
		Negate:  negate,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	p.Type = PolicyTypeMethod
	p.MatchConfig = data
	return nil
}

func (p *TrafficPolicy) SetTagMatchConfig(tags []string, matchMode TagMatchMode, sourceType string) error {
	if len(tags) == 0 {
		return ErrTrafficPolicyInvalidTagConfig
	}
	cfg := TagMatchConfig{
		Tags:       tags,
		MatchMode:  matchMode,
		SourceType: sourceType,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	p.Type = PolicyTypeTag
	p.MatchConfig = data
	return nil
}

func (p *TrafficPolicy) SetConsumerMatchConfig(consumerIDs []uuid.UUID, tags []string, matchMode TagMatchMode) error {
	if len(consumerIDs) == 0 && len(tags) == 0 {
		return fmt.Errorf("at least one of 'consumer_ids' or 'tags' is required")
	}
	cfg := ConsumerMatchConfig{
		ConsumerIDs: consumerIDs,
		Tags:        tags,
		MatchMode:   matchMode,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	p.Type = PolicyTypeConsumer
	p.MatchConfig = data
	return nil
}

func (p *TrafficPolicy) SetCompoundMatchConfig(operator CompoundOperator, conditions []CompoundCondition) error {
	if len(conditions) == 0 {
		return ErrTrafficPolicyInvalidCompoundConfig
	}
	if operator != CompoundOperatorAND && operator != CompoundOperatorOR {
		return ErrTrafficPolicyInvalidCompoundOperator
	}
	cfg := CompoundMatchConfig{
		Operator:   operator,
		Conditions: conditions,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	p.Type = PolicyTypeCompound
	p.MatchConfig = data
	return nil
}

func (p *TrafficPolicy) SetFallbackMatchConfig(fallbackServiceID uuid.UUID, healthCheckType string, minHealthyTargets int) error {
	if fallbackServiceID == uuid.Nil {
		return ErrTrafficPolicyInvalidFallbackConfig
	}
	cfg := FallbackMatchConfig{
		FallbackServiceID: fallbackServiceID,
		HealthCheckType:   healthCheckType,
		MinHealthyTargets: minHealthyTargets,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	p.Type = PolicyTypeFallback
	p.MatchConfig = data
	return nil
}

func (p *TrafficPolicy) GetHeaderMatchConfig() (*HeaderMatchConfig, error) {
	if p.Type != PolicyTypeHeader {
		return nil, errors.New("policy type is not header")
	}
	var cfg HeaderMatchConfig
	if err := json.Unmarshal(p.MatchConfig, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (p *TrafficPolicy) GetWeightMatchConfig() (*WeightMatchConfig, error) {
	if p.Type != PolicyTypeWeight {
		return nil, errors.New("policy type is not weight")
	}
	var cfg WeightMatchConfig
	if err := json.Unmarshal(p.MatchConfig, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (p *TrafficPolicy) GetQueryMatchConfig() (*QueryMatchConfig, error) {
	if p.Type != PolicyTypeQuery {
		return nil, errors.New("policy type is not query")
	}
	var cfg QueryMatchConfig
	if err := json.Unmarshal(p.MatchConfig, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (p *TrafficPolicy) GetCookieMatchConfig() (*CookieMatchConfig, error) {
	if p.Type != PolicyTypeCookie {
		return nil, errors.New("policy type is not cookie")
	}
	var cfg CookieMatchConfig
	if err := json.Unmarshal(p.MatchConfig, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (p *TrafficPolicy) GetIPMatchConfig() (*IPMatchConfig, error) {
	if p.Type != PolicyTypeIP {
		return nil, errors.New("policy type is not ip")
	}
	var cfg IPMatchConfig
	if err := json.Unmarshal(p.MatchConfig, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (p *TrafficPolicy) GetPathMatchConfig() (*PathMatchConfig, error) {
	if p.Type != PolicyTypePath {
		return nil, errors.New("policy type is not path")
	}
	var cfg PathMatchConfig
	if err := json.Unmarshal(p.MatchConfig, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (p *TrafficPolicy) GetMethodMatchConfig() (*MethodMatchConfig, error) {
	if p.Type != PolicyTypeMethod {
		return nil, errors.New("policy type is not method")
	}
	var cfg MethodMatchConfig
	if err := json.Unmarshal(p.MatchConfig, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (p *TrafficPolicy) GetTagMatchConfig() (*TagMatchConfig, error) {
	if p.Type != PolicyTypeTag {
		return nil, errors.New("policy type is not tag")
	}
	var cfg TagMatchConfig
	if err := json.Unmarshal(p.MatchConfig, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (p *TrafficPolicy) GetConsumerMatchConfig() (*ConsumerMatchConfig, error) {
	if p.Type != PolicyTypeConsumer {
		return nil, errors.New("policy type is not consumer")
	}
	var cfg ConsumerMatchConfig
	if err := json.Unmarshal(p.MatchConfig, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (p *TrafficPolicy) GetCompoundMatchConfig() (*CompoundMatchConfig, error) {
	if p.Type != PolicyTypeCompound {
		return nil, errors.New("policy type is not compound")
	}
	var cfg CompoundMatchConfig
	if err := json.Unmarshal(p.MatchConfig, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (p *TrafficPolicy) GetFallbackMatchConfig() (*FallbackMatchConfig, error) {
	if p.Type != PolicyTypeFallback {
		return nil, errors.New("policy type is not fallback")
	}
	var cfg FallbackMatchConfig
	if err := json.Unmarshal(p.MatchConfig, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func NewRuleSet(name, description string) *RuleSet {
	return &RuleSet{
		ID:          uuid.New(),
		Name:        name,
		Description: description,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

func (rs *RuleSet) AddPolicy(policyID uuid.UUID) {
	for _, id := range rs.PolicyIDs {
		if id == policyID {
			return
		}
	}
	rs.PolicyIDs = append(rs.PolicyIDs, policyID)
	rs.UpdatedAt = time.Now()
}

func (rs *RuleSet) RemovePolicy(policyID uuid.UUID) {
	newIDs := make([]uuid.UUID, 0, len(rs.PolicyIDs))
	for _, id := range rs.PolicyIDs {
		if id != policyID {
			newIDs = append(newIDs, id)
		}
	}
	rs.PolicyIDs = newIDs
	rs.UpdatedAt = time.Now()
}

func (rs *RuleSet) AddTag(tag string) {
	for _, t := range rs.Tags {
		if t == tag {
			return
		}
	}
	rs.Tags = append(rs.Tags, tag)
	rs.UpdatedAt = time.Now()
}

func (rs *RuleSet) RemoveTag(tag string) {
	newTags := make([]string, 0, len(rs.Tags))
	for _, t := range rs.Tags {
		if t != tag {
			newTags = append(newTags, t)
		}
	}
	rs.Tags = newTags
	rs.UpdatedAt = time.Now()
}
