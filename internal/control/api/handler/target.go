package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/control/api"
	"github.com/taichirain/portkey/internal/control/middleware"
	"github.com/taichirain/portkey/internal/control/repository"
	"github.com/taichirain/portkey/internal/domain/target"
	"go.uber.org/zap"
)

type TargetHandler struct {
	repo   repository.TargetRepository
	logger *zap.Logger
}

func NewTargetHandler(repo repository.TargetRepository, logger *zap.Logger) *TargetHandler {
	return &TargetHandler{
		repo:   repo,
		logger: logger,
	}
}

type CreateTargetRequest struct {
	UpstreamID string   `json:"upstream_id"`
	Target     string   `json:"target"`
	Port       int      `json:"port"`
	Weight     int      `json:"weight"`
	Tags       []string `json:"tags"`
	Enabled    *bool    `json:"enabled"`
}

type UpdateTargetRequest struct {
	Target  *string  `json:"target"`
	Port    *int     `json:"port"`
	Weight  *int     `json:"weight"`
	Tags    []string `json:"tags"`
	Enabled *bool    `json:"enabled"`
}

func (h *TargetHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateTargetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	var upstreamID uuid.UUID
	if req.UpstreamID != "" {
		var err error
		upstreamID, err = uuid.Parse(req.UpstreamID)
		if err != nil {
			api.JSONError(w, http.StatusBadRequest, "Invalid upstream_id", "VALIDATION_ERROR")
			return
		}
	}

	t, err := target.New(upstreamID, req.Target, req.Port)
	if err != nil {
		api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	if req.Weight > 0 {
		t.Weight = req.Weight
	}
	if req.Tags != nil {
		t.Tags = req.Tags
	}
	if req.Enabled != nil {
		t.Enabled = *req.Enabled
	}

	if err := t.Validate(); err != nil {
		api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	auditCtx := h.createAuditContext(r)
	if err := h.repo.Create(r.Context(), t, auditCtx); err != nil {
		h.logger.Error("Failed to create target", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to create target", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Target created", zap.Stringer("id", t.ID), zap.String("target", t.Target))
	api.JSON(w, http.StatusCreated, t)
}

func (h *TargetHandler) GetByID(w http.ResponseWriter, r *http.Request) {
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

	t, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Target not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to get target", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to get target", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, t)
}

func (h *TargetHandler) ListByUpstreamID(w http.ResponseWriter, r *http.Request) {
	upstreamIDStr := r.URL.Query().Get("upstream_id")
	if upstreamIDStr == "" {
		api.JSONError(w, http.StatusBadRequest, "upstream_id is required", "VALIDATION_ERROR")
		return
	}

	upstreamID, err := uuid.Parse(upstreamIDStr)
	if err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid upstream_id", "VALIDATION_ERROR")
		return
	}

	targets, err := h.repo.ListByUpstreamID(r.Context(), upstreamID)
	if err != nil {
		h.logger.Error("Failed to list targets by upstream_id", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to list targets", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, targets)
}

func (h *TargetHandler) List(w http.ResponseWriter, r *http.Request) {
	page, pageSize := api.ParsePagination(r)

	result, err := h.repo.List(r.Context(), &repository.Pagination{Page: page, PageSize: pageSize})
	if err != nil {
		h.logger.Error("Failed to list targets", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to list targets", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, api.ToPaginationResponse(result))
}

func (h *TargetHandler) Update(w http.ResponseWriter, r *http.Request) {
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

	t, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Target not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to get target", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to get target", "INTERNAL_ERROR")
		return
	}

	var req UpdateTargetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	if req.Target != nil {
		t.Target = *req.Target
	}
	if req.Port != nil {
		t.Port = *req.Port
	}
	if req.Weight != nil {
		if err := t.SetWeight(*req.Weight); err != nil {
			api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
			return
		}
	}
	if req.Tags != nil {
		t.Tags = req.Tags
	}
	if req.Enabled != nil {
		t.Enabled = *req.Enabled
	}

	if err := t.Validate(); err != nil {
		api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	auditCtx := h.createAuditContext(r)
	if err := h.repo.Update(r.Context(), t, auditCtx); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Target not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to update target", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to update target", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Target updated", zap.Stringer("id", t.ID))
	api.JSON(w, http.StatusOK, t)
}

func (h *TargetHandler) Delete(w http.ResponseWriter, r *http.Request) {
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
			api.JSONError(w, http.StatusNotFound, "Target not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to delete target", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to delete target", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Target deleted", zap.Stringer("id", id))
	api.JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *TargetHandler) createAuditContext(r *http.Request) *repository.AuditContext {
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
