package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/control/api"
	"github.com/taichirain/portkey/internal/control/middleware"
	"github.com/taichirain/portkey/internal/control/repository"
	"github.com/taichirain/portkey/internal/domain/route"
	"go.uber.org/zap"
)

type RouteHandler struct {
	repo   repository.RouteRepository
	logger *zap.Logger
}

func NewRouteHandler(repo repository.RouteRepository, logger *zap.Logger) *RouteHandler {
	return &RouteHandler{
		repo:   repo,
		logger: logger,
	}
}

type CreateRouteRequest struct {
	Name          string              `json:"name"`
	ServiceID     string              `json:"service_id"`
	Protocols     []string            `json:"protocols"`
	Methods       []string            `json:"methods"`
	Hosts         []string            `json:"hosts"`
	Paths         []string            `json:"paths"`
	Headers       map[string][]string `json:"headers"`
	StripPath     *bool               `json:"strip_path"`
	PreserveHost  *bool               `json:"preserve_host"`
	RegexPriority *int                `json:"regex_priority"`
	Tags          []string            `json:"tags"`
	Enabled       *bool               `json:"enabled"`
}

type UpdateRouteRequest struct {
	Name          *string             `json:"name"`
	ServiceID     *string             `json:"service_id"`
	Protocols     []string            `json:"protocols"`
	Methods       []string            `json:"methods"`
	Hosts         []string            `json:"hosts"`
	Paths         []string            `json:"paths"`
	Headers       map[string][]string `json:"headers"`
	StripPath     *bool               `json:"strip_path"`
	PreserveHost  *bool               `json:"preserve_host"`
	RegexPriority *int                `json:"regex_priority"`
	Tags          []string            `json:"tags"`
	Enabled       *bool               `json:"enabled"`
}

func (h *RouteHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateRouteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	var serviceID uuid.UUID
	if req.ServiceID != "" {
		var err error
		serviceID, err = uuid.Parse(req.ServiceID)
		if err != nil {
			api.JSONError(w, http.StatusBadRequest, "Invalid service_id", "VALIDATION_ERROR")
			return
		}
	}

	rt, err := route.New(serviceID)
	if err != nil {
		api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	if req.Name != "" {
		rt.Name = req.Name
	}
	if req.Protocols != nil {
		rt.Protocols = req.Protocols
	}
	if req.Methods != nil {
		rt.Methods = req.Methods
	}
	if req.Hosts != nil {
		rt.Hosts = req.Hosts
	}
	if req.Paths != nil {
		rt.Paths = req.Paths
	}
	if req.Headers != nil {
		rt.Headers = req.Headers
	}
	if req.StripPath != nil {
		rt.StripPath = *req.StripPath
	}
	if req.PreserveHost != nil {
		rt.PreserveHost = *req.PreserveHost
	}
	if req.RegexPriority != nil {
		rt.RegexPriority = *req.RegexPriority
	}
	if req.Tags != nil {
		rt.Tags = req.Tags
	}
	if req.Enabled != nil {
		rt.Enabled = *req.Enabled
	}

	if err := rt.Validate(); err != nil {
		api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	auditCtx := h.createAuditContext(r)
	if err := h.repo.Create(r.Context(), rt, auditCtx); err != nil {
		h.logger.Error("Failed to create route", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to create route", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Route created", zap.Stringer("id", rt.ID))
	api.JSON(w, http.StatusCreated, rt)
}

func (h *RouteHandler) GetByID(w http.ResponseWriter, r *http.Request) {
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

	rt, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Route not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to get route", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to get route", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, rt)
}

func (h *RouteHandler) List(w http.ResponseWriter, r *http.Request) {
	page, pageSize := api.ParsePagination(r)

	result, err := h.repo.List(r.Context(), &repository.Pagination{Page: page, PageSize: pageSize})
	if err != nil {
		h.logger.Error("Failed to list routes", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to list routes", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, api.ToPaginationResponse(result))
}

func (h *RouteHandler) Update(w http.ResponseWriter, r *http.Request) {
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

	rt, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Route not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to get route", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to get route", "INTERNAL_ERROR")
		return
	}

	var req UpdateRouteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	if req.Name != nil {
		rt.Name = *req.Name
	}
	if req.ServiceID != nil {
		serviceID, err := uuid.Parse(*req.ServiceID)
		if err != nil {
			api.JSONError(w, http.StatusBadRequest, "Invalid service_id", "VALIDATION_ERROR")
			return
		}
		rt.ServiceID = serviceID
	}
	if req.Protocols != nil {
		rt.Protocols = req.Protocols
	}
	if req.Methods != nil {
		rt.Methods = req.Methods
	}
	if req.Hosts != nil {
		rt.Hosts = req.Hosts
	}
	if req.Paths != nil {
		rt.Paths = req.Paths
	}
	if req.Headers != nil {
		rt.Headers = req.Headers
	}
	if req.StripPath != nil {
		rt.StripPath = *req.StripPath
	}
	if req.PreserveHost != nil {
		rt.PreserveHost = *req.PreserveHost
	}
	if req.RegexPriority != nil {
		rt.RegexPriority = *req.RegexPriority
	}
	if req.Tags != nil {
		rt.Tags = req.Tags
	}
	if req.Enabled != nil {
		rt.Enabled = *req.Enabled
	}

	if err := rt.Validate(); err != nil {
		api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	auditCtx := h.createAuditContext(r)
	if err := h.repo.Update(r.Context(), rt, auditCtx); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Route not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to update route", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to update route", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Route updated", zap.Stringer("id", rt.ID))
	api.JSON(w, http.StatusOK, rt)
}

func (h *RouteHandler) Delete(w http.ResponseWriter, r *http.Request) {
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
			api.JSONError(w, http.StatusNotFound, "Route not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to delete route", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to delete route", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Route deleted", zap.Stringer("id", id))
	api.JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *RouteHandler) createAuditContext(r *http.Request) *repository.AuditContext {
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
