package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/control/api"
	"github.com/taichirain/portkey/internal/control/middleware"
	"github.com/taichirain/portkey/internal/control/repository"
	"github.com/taichirain/portkey/internal/domain/service"
	"go.uber.org/zap"
)

type ServiceHandler struct {
	repo   repository.ServiceRepository
	logger *zap.Logger
}

func NewServiceHandler(repo repository.ServiceRepository, logger *zap.Logger) *ServiceHandler {
	return &ServiceHandler{
		repo:   repo,
		logger: logger,
	}
}

type CreateServiceRequest struct {
	Name           string   `json:"name"`
	Protocol       string   `json:"protocol"`
	Host           string   `json:"host"`
	Port           int      `json:"port"`
	Path           string   `json:"path"`
	UpstreamID     string   `json:"upstream_id"`
	Retries        int      `json:"retries"`
	ConnectTimeout int      `json:"connect_timeout"`
	WriteTimeout   int      `json:"write_timeout"`
	ReadTimeout    int      `json:"read_timeout"`
	Tags           []string `json:"tags"`
	Enabled        *bool    `json:"enabled"`
}

type UpdateServiceRequest struct {
	Name           *string  `json:"name"`
	Protocol       *string  `json:"protocol"`
	Host           *string  `json:"host"`
	Port           *int     `json:"port"`
	Path           *string  `json:"path"`
	UpstreamID     *string  `json:"upstream_id"`
	Retries        *int     `json:"retries"`
	ConnectTimeout *int     `json:"connect_timeout"`
	WriteTimeout   *int     `json:"write_timeout"`
	ReadTimeout    *int     `json:"read_timeout"`
	Tags           []string `json:"tags"`
	Enabled        *bool    `json:"enabled"`
}

func (h *ServiceHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateServiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	svc, err := service.New(req.Name)
	if err != nil {
		api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	if req.Protocol != "" {
		svc.Protocol = service.Protocol(req.Protocol)
	}
	if req.Host != "" {
		svc.Host = req.Host
	}
	if req.Port > 0 {
		svc.Port = req.Port
	}
	if req.Path != "" {
		svc.Path = req.Path
	}
	if req.UpstreamID != "" {
		upstreamID, err := uuid.Parse(req.UpstreamID)
		if err != nil {
			api.JSONError(w, http.StatusBadRequest, "Invalid upstream_id", "VALIDATION_ERROR")
			return
		}
		svc.UpstreamID = upstreamID
	}
	if req.Retries > 0 {
		svc.Retries = req.Retries
	}
	if req.ConnectTimeout > 0 {
		svc.ConnectTimeout = req.ConnectTimeout
	}
	if req.WriteTimeout > 0 {
		svc.WriteTimeout = req.WriteTimeout
	}
	if req.ReadTimeout > 0 {
		svc.ReadTimeout = req.ReadTimeout
	}
	if req.Tags != nil {
		svc.Tags = req.Tags
	}
	if req.Enabled != nil {
		svc.Enabled = *req.Enabled
	}

	if err := svc.Validate(); err != nil {
		api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	auditCtx := h.createAuditContext(r)
	if err := h.repo.Create(r.Context(), svc, auditCtx); err != nil {
		if errors.Is(err, repository.ErrAlreadyExists) {
			api.JSONError(w, http.StatusConflict, "Service with this name already exists", "CONFLICT")
			return
		}
		h.logger.Error("Failed to create service", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to create service", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Service created", zap.Stringer("id", svc.ID), zap.String("name", svc.Name))
	api.JSON(w, http.StatusCreated, svc)
}

func (h *ServiceHandler) GetByID(w http.ResponseWriter, r *http.Request) {
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

	svc, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Service not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to get service", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to get service", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, svc)
}

func (h *ServiceHandler) GetByName(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		api.JSONError(w, http.StatusBadRequest, "Name is required", "VALIDATION_ERROR")
		return
	}

	svc, err := h.repo.GetByName(r.Context(), name)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Service not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to get service by name", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to get service", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, svc)
}

func (h *ServiceHandler) List(w http.ResponseWriter, r *http.Request) {
	page, pageSize := api.ParsePagination(r)

	result, err := h.repo.List(r.Context(), &repository.Pagination{Page: page, PageSize: pageSize})
	if err != nil {
		h.logger.Error("Failed to list services", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to list services", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, api.ToPaginationResponse(result))
}

func (h *ServiceHandler) Update(w http.ResponseWriter, r *http.Request) {
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

	svc, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Service not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to get service", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to get service", "INTERNAL_ERROR")
		return
	}

	var req UpdateServiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	if req.Name != nil {
		svc.Name = *req.Name
	}
	if req.Protocol != nil {
		svc.Protocol = service.Protocol(*req.Protocol)
	}
	if req.Host != nil {
		svc.Host = *req.Host
	}
	if req.Port != nil {
		svc.Port = *req.Port
	}
	if req.Path != nil {
		svc.Path = *req.Path
	}
	if req.UpstreamID != nil {
		if *req.UpstreamID == "" {
			svc.UpstreamID = uuid.Nil
		} else {
			upstreamID, err := uuid.Parse(*req.UpstreamID)
			if err != nil {
				api.JSONError(w, http.StatusBadRequest, "Invalid upstream_id", "VALIDATION_ERROR")
				return
			}
			svc.UpstreamID = upstreamID
		}
	}
	if req.Retries != nil {
		svc.Retries = *req.Retries
	}
	if req.ConnectTimeout != nil {
		svc.ConnectTimeout = *req.ConnectTimeout
	}
	if req.WriteTimeout != nil {
		svc.WriteTimeout = *req.WriteTimeout
	}
	if req.ReadTimeout != nil {
		svc.ReadTimeout = *req.ReadTimeout
	}
	if req.Tags != nil {
		svc.Tags = req.Tags
	}
	if req.Enabled != nil {
		svc.Enabled = *req.Enabled
	}

	if err := svc.Validate(); err != nil {
		api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	auditCtx := h.createAuditContext(r)
	if err := h.repo.Update(r.Context(), svc, auditCtx); err != nil {
		if errors.Is(err, repository.ErrAlreadyExists) {
			api.JSONError(w, http.StatusConflict, "Service with this name already exists", "CONFLICT")
			return
		}
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Service not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to update service", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to update service", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Service updated", zap.Stringer("id", svc.ID))
	api.JSON(w, http.StatusOK, svc)
}

func (h *ServiceHandler) Delete(w http.ResponseWriter, r *http.Request) {
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
			api.JSONError(w, http.StatusNotFound, "Service not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to delete service", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to delete service", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Service deleted", zap.Stringer("id", id))
	api.JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *ServiceHandler) createAuditContext(r *http.Request) *repository.AuditContext {
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
