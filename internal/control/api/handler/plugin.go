package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/control/api"
	"github.com/taichirain/portkey/internal/control/middleware"
	"github.com/taichirain/portkey/internal/control/repository"
	"github.com/taichirain/portkey/internal/domain/plugin"
	"go.uber.org/zap"
)

type PluginHandler struct {
	repo   repository.PluginRepository
	logger *zap.Logger
}

func NewPluginHandler(repo repository.PluginRepository, logger *zap.Logger) *PluginHandler {
	return &PluginHandler{
		repo:   repo,
		logger: logger,
	}
}

type CreatePluginRequest struct {
	Name       string                 `json:"name"`
	RouteID    *string                `json:"route_id"`
	ServiceID  *string                `json:"service_id"`
	ConsumerID *string                `json:"consumer_id"`
	Config     map[string]interface{} `json:"config"`
	Protocols  []string               `json:"protocols"`
	Enabled    *bool                  `json:"enabled"`
	RunOn      string                 `json:"run_on"`
	Tags       []string               `json:"tags"`
}

type UpdatePluginRequest struct {
	Name       *string                `json:"name"`
	RouteID    *string                `json:"route_id"`
	ServiceID  *string                `json:"service_id"`
	ConsumerID *string                `json:"consumer_id"`
	Config     map[string]interface{} `json:"config"`
	Protocols  []string               `json:"protocols"`
	Enabled    *bool                  `json:"enabled"`
	RunOn      *string                `json:"run_on"`
	Tags       []string               `json:"tags"`
}

func (h *PluginHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreatePluginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	if req.Config == nil {
		req.Config = make(map[string]interface{})
	}

	p, err := plugin.New(req.Name, req.Config)
	if err != nil {
		api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	if req.RouteID != nil && *req.RouteID != "" {
		routeID, err := uuid.Parse(*req.RouteID)
		if err != nil {
			api.JSONError(w, http.StatusBadRequest, "Invalid route_id", "VALIDATION_ERROR")
			return
		}
		p.RouteID = &routeID
	}

	if req.ServiceID != nil && *req.ServiceID != "" {
		serviceID, err := uuid.Parse(*req.ServiceID)
		if err != nil {
			api.JSONError(w, http.StatusBadRequest, "Invalid service_id", "VALIDATION_ERROR")
			return
		}
		p.ServiceID = &serviceID
	}

	if req.ConsumerID != nil && *req.ConsumerID != "" {
		consumerID, err := uuid.Parse(*req.ConsumerID)
		if err != nil {
			api.JSONError(w, http.StatusBadRequest, "Invalid consumer_id", "VALIDATION_ERROR")
			return
		}
		p.ConsumerID = &consumerID
	}

	if req.Protocols != nil {
		p.Protocols = req.Protocols
	}
	if req.Enabled != nil {
		p.Enabled = *req.Enabled
	}
	if req.RunOn != "" {
		p.RunOn = plugin.RunOn(req.RunOn)
	}
	if req.Tags != nil {
		p.Tags = req.Tags
	}

	if err := p.Validate(); err != nil {
		api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	auditCtx := h.createAuditContext(r)
	if err := h.repo.Create(r.Context(), p, auditCtx); err != nil {
		if errors.Is(err, repository.ErrInvalidInput) {
			api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
			return
		}
		h.logger.Error("Failed to create plugin", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to create plugin", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Plugin created", zap.Stringer("id", p.ID), zap.String("name", p.Name))
	api.JSON(w, http.StatusCreated, p)
}

func (h *PluginHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		api.JSONError(w, http.StatusBadRequest, "ID is required", "VALIDATION_ERROR")
		return
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid ID format", "VALIDATION_ERROR")
		return
	}

	p, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Plugin not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to get plugin", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to get plugin", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, p)
}

func (h *PluginHandler) ListByName(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		api.JSONError(w, http.StatusBadRequest, "Plugin name is required", "VALIDATION_ERROR")
		return
	}

	plugins, err := h.repo.ListByName(r.Context(), name)
	if err != nil {
		h.logger.Error("Failed to list plugins by name", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to list plugins", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, plugins)
}

func (h *PluginHandler) ListByRouteID(w http.ResponseWriter, r *http.Request) {
	routeIDStr := r.URL.Query().Get("route_id")
	if routeIDStr == "" {
		api.JSONError(w, http.StatusBadRequest, "route_id is required", "VALIDATION_ERROR")
		return
	}

	routeID, err := uuid.Parse(routeIDStr)
	if err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid route_id", "VALIDATION_ERROR")
		return
	}

	plugins, err := h.repo.ListByRouteID(r.Context(), routeID)
	if err != nil {
		h.logger.Error("Failed to list plugins by route_id", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to list plugins", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, plugins)
}

func (h *PluginHandler) ListByServiceID(w http.ResponseWriter, r *http.Request) {
	serviceIDStr := r.URL.Query().Get("service_id")
	if serviceIDStr == "" {
		api.JSONError(w, http.StatusBadRequest, "service_id is required", "VALIDATION_ERROR")
		return
	}

	serviceID, err := uuid.Parse(serviceIDStr)
	if err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid service_id", "VALIDATION_ERROR")
		return
	}

	plugins, err := h.repo.ListByServiceID(r.Context(), serviceID)
	if err != nil {
		h.logger.Error("Failed to list plugins by service_id", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to list plugins", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, plugins)
}

func (h *PluginHandler) ListByConsumerID(w http.ResponseWriter, r *http.Request) {
	consumerIDStr := r.URL.Query().Get("consumer_id")
	if consumerIDStr == "" {
		api.JSONError(w, http.StatusBadRequest, "consumer_id is required", "VALIDATION_ERROR")
		return
	}

	consumerID, err := uuid.Parse(consumerIDStr)
	if err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid consumer_id", "VALIDATION_ERROR")
		return
	}

	plugins, err := h.repo.ListByConsumerID(r.Context(), consumerID)
	if err != nil {
		h.logger.Error("Failed to list plugins by consumer_id", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to list plugins", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, plugins)
}

func (h *PluginHandler) ListGlobal(w http.ResponseWriter, r *http.Request) {
	plugins, err := h.repo.ListGlobal(r.Context())
	if err != nil {
		h.logger.Error("Failed to list global plugins", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to list plugins", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, plugins)
}

func (h *PluginHandler) List(w http.ResponseWriter, r *http.Request) {
	page, pageSize := api.ParsePagination(r)

	result, err := h.repo.List(r.Context(), &repository.Pagination{Page: page, PageSize: pageSize})
	if err != nil {
		h.logger.Error("Failed to list plugins", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to list plugins", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, api.ToPaginationResponse(result))
}

func (h *PluginHandler) Update(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		api.JSONError(w, http.StatusBadRequest, "ID is required", "VALIDATION_ERROR")
		return
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid ID format", "VALIDATION_ERROR")
		return
	}

	p, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Plugin not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to get plugin", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to get plugin", "INTERNAL_ERROR")
		return
	}

	var req UpdatePluginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	if req.Name != nil {
		p.Name = *req.Name
	}

	if req.RouteID != nil {
		if *req.RouteID == "" {
			p.RouteID = nil
		} else {
			routeID, err := uuid.Parse(*req.RouteID)
			if err != nil {
				api.JSONError(w, http.StatusBadRequest, "Invalid route_id", "VALIDATION_ERROR")
				return
			}
			p.RouteID = &routeID
		}
	}

	if req.ServiceID != nil {
		if *req.ServiceID == "" {
			p.ServiceID = nil
		} else {
			serviceID, err := uuid.Parse(*req.ServiceID)
			if err != nil {
				api.JSONError(w, http.StatusBadRequest, "Invalid service_id", "VALIDATION_ERROR")
				return
			}
			p.ServiceID = &serviceID
		}
	}

	if req.ConsumerID != nil {
		if *req.ConsumerID == "" {
			p.ConsumerID = nil
		} else {
			consumerID, err := uuid.Parse(*req.ConsumerID)
			if err != nil {
				api.JSONError(w, http.StatusBadRequest, "Invalid consumer_id", "VALIDATION_ERROR")
				return
			}
			p.ConsumerID = &consumerID
		}
	}

	if req.Config != nil {
		p.Config = req.Config
	}
	if req.Protocols != nil {
		p.Protocols = req.Protocols
	}
	if req.Enabled != nil {
		p.Enabled = *req.Enabled
	}
	if req.RunOn != nil {
		p.RunOn = plugin.RunOn(*req.RunOn)
	}
	if req.Tags != nil {
		p.Tags = req.Tags
	}

	if err := p.Validate(); err != nil {
		api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	auditCtx := h.createAuditContext(r)
	if err := h.repo.Update(r.Context(), p, auditCtx); err != nil {
		if errors.Is(err, repository.ErrInvalidInput) {
			api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
			return
		}
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Plugin not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to update plugin", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to update plugin", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Plugin updated", zap.Stringer("id", p.ID))
	api.JSON(w, http.StatusOK, p)
}

func (h *PluginHandler) Delete(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		api.JSONError(w, http.StatusBadRequest, "ID is required", "VALIDATION_ERROR")
		return
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid ID format", "VALIDATION_ERROR")
		return
	}

	auditCtx := h.createAuditContext(r)
	if err := h.repo.Delete(r.Context(), id, auditCtx); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Plugin not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to delete plugin", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to delete plugin", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Plugin deleted", zap.Stringer("id", id))
	api.JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *PluginHandler) createAuditContext(r *http.Request) *repository.AuditContext {
	adminID, ok := middleware.GetAdminID(r.Context())
	if !ok {
		return nil
	}

	return &repository.AuditContext{
		AdminID:   &adminID,
		ClientIP:  r.RemoteAddr,
		UserAgent: r.UserAgent(),
	}
}
