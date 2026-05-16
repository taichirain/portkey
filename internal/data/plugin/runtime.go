package plugin

import (
	"net/http"

	"github.com/google/uuid"
)

type Scope int

const (
	ScopeGlobal Scope = iota
	ScopeService
	ScopeRoute
	ScopeConsumer
)

func (s Scope) String() string {
	switch s {
	case ScopeGlobal:
		return "global"
	case ScopeService:
		return "service"
	case ScopeRoute:
		return "route"
	case ScopeConsumer:
		return "consumer"
	default:
		return "unknown"
	}
}

type PluginContext struct {
	Request        *http.Request
	ResponseWriter http.ResponseWriter
	MatchedRoute   *MatchedRouteInfo
	ConsumerID     *uuid.UUID
	TraceID        string
	Attributes     map[string]interface{}
	shortCircuited  bool
}

type MatchedRouteInfo struct {
	RouteID    uuid.UUID
	ServiceID  uuid.UUID
	UpstreamID uuid.UUID
}

func NewPluginContext(w http.ResponseWriter, r *http.Request, traceID string) *PluginContext {
	return &PluginContext{
		Request:        r,
		ResponseWriter: w,
		TraceID:        traceID,
		Attributes:     make(map[string]interface{}),
		shortCircuited:  false,
	}
}

func (ctx *PluginContext) SetMatchedRoute(routeID, serviceID, upstreamID uuid.UUID) {
	ctx.MatchedRoute = &MatchedRouteInfo{
		RouteID:    routeID,
		ServiceID:  serviceID,
		UpstreamID: upstreamID,
	}
}

func (ctx *PluginContext) SetConsumerID(consumerID uuid.UUID) {
	ctx.ConsumerID = &consumerID
}

func (ctx *PluginContext) SetAttribute(key string, value interface{}) {
	ctx.Attributes[key] = value
}

func (ctx *PluginContext) GetAttribute(key string) interface{} {
	return ctx.Attributes[key]
}

func (ctx *PluginContext) ShortCircuit() {
	ctx.shortCircuited = true
}

func (ctx *PluginContext) IsShortCircuited() bool {
	return ctx.shortCircuited
}

type Plugin interface {
	Name() string
	OnRequest(ctx *PluginContext) error
	OnResponse(ctx *PluginContext, resp *http.Response) error
	OnError(ctx *PluginContext, err error) error
}

type PluginConfig struct {
	ID         uuid.UUID
	Name       string
	Scope      Scope
	RouteID    *uuid.UUID
	ServiceID  *uuid.UUID
	ConsumerID *uuid.UUID
	Config     map[string]interface{}
	Enabled    bool
}

type PluginFactory interface {
	Name() string
	Create(config map[string]interface{}) (Plugin, error)
}

type PluginRegistry struct {
	factories map[string]PluginFactory
}

func NewPluginRegistry() *PluginRegistry {
	return &PluginRegistry{
		factories: make(map[string]PluginFactory),
	}
}

func (r *PluginRegistry) Register(factory PluginFactory) {
	r.factories[factory.Name()] = factory
}

func (r *PluginRegistry) Unregister(name string) {
	delete(r.factories, name)
}

func (r *PluginRegistry) Get(name string) (PluginFactory, bool) {
	factory, ok := r.factories[name]
	return factory, ok
}

func (r *PluginRegistry) CreateInstance(name string, config map[string]interface{}) (Plugin, error) {
	factory, ok := r.factories[name]
	if !ok {
		return nil, ErrPluginNotFound
	}
	return factory.Create(config)
}

var ErrPluginNotFound = &PluginError{
	Message: "plugin not found in registry",
}

type PluginError struct {
	Message  string
	Plugin   string
	Original error
}

func (e *PluginError) Error() string {
	if e.Plugin != "" {
		return "plugin [" + e.Plugin + "]: " + e.Message
	}
	return e.Message
}

func (e *PluginError) Unwrap() error {
	return e.Original
}

func NewPluginError(pluginName string, message string, original error) *PluginError {
	return &PluginError{
		Message:  message,
		Plugin:   pluginName,
		Original: original,
	}
}
