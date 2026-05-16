package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/control/api"
	"github.com/taichirain/portkey/internal/control/middleware"
	"github.com/taichirain/portkey/internal/control/repository"
	"github.com/taichirain/portkey/internal/domain/upstream"
	"go.uber.org/zap"
)

type UpstreamHandler struct {
	repo   repository.UpstreamRepository
	logger *zap.Logger
}

func NewUpstreamHandler(repo repository.UpstreamRepository, logger *zap.Logger) *UpstreamHandler {
	return &UpstreamHandler{
		repo:   repo,
		logger: logger,
	}
}

type CreateUpstreamRequest struct {
	Name      string   `json:"name"`
	Algorithm string   `json:"algorithm"`
	Slots     int      `json:"slots"`
	Tags      []string `json:"tags"`
}

type UpdateUpstreamRequest struct {
	Name      *string  `json:"name"`
	Algorithm *string  `json:"algorithm"`
	Slots     *int     `json:"slots"`
	Tags      []string `json:"tags"`
}

func (h *UpstreamHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateUpstreamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	u, err := upstream.New(req.Name)
	if err != nil {
		api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	if req.Algorithm != "" {
		u.Algorithm = upstream.Algorithm(req.Algorithm)
	}
	if req.Slots > 0 {
		u.Slots = req.Slots
	}
	if req.Tags != nil {
		u.Tags = req.Tags
	}

	if err := u.Validate(); err != nil {
		api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	auditCtx := h.createAuditContext(r)
	if err := h.repo.Create(r.Context(), u, auditCtx); err != nil {
		if errors.Is(err, repository.ErrAlreadyExists) {
			api.JSONError(w, http.StatusConflict, "Upstream with this name already exists", "CONFLICT")
			return
		}
		h.logger.Error("Failed to create upstream", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to create upstream", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Upstream created", zap.Stringer("id", u.ID), zap.String("name", u.Name))
	api.JSON(w, http.StatusCreated, u)
}

func (h *UpstreamHandler) GetByID(w http.ResponseWriter, r *http.Request) {
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

	u, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Upstream not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to get upstream", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to get upstream", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, u)
}

func (h *UpstreamHandler) GetByName(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		api.JSONError(w, http.StatusBadRequest, "Name is required", "VALIDATION_ERROR")
		return
	}

	u, err := h.repo.GetByName(r.Context(), name)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Upstream not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to get upstream by name", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to get upstream", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, u)
}

func (h *UpstreamHandler) List(w http.ResponseWriter, r *http.Request) {
	page, pageSize := api.ParsePagination(r)

	result, err := h.repo.List(r.Context(), &repository.Pagination{Page: page, PageSize: pageSize})
	if err != nil {
		h.logger.Error("Failed to list upstreams", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to list upstreams", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, api.ToPaginationResponse(result))
}

func (h *UpstreamHandler) Update(w http.ResponseWriter, r *http.Request) {
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

	u, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Upstream not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to get upstream", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to get upstream", "INTERNAL_ERROR")
		return
	}

	var req UpdateUpstreamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	if req.Name != nil {
		u.Name = *req.Name
	}
	if req.Algorithm != nil {
		u.Algorithm = upstream.Algorithm(*req.Algorithm)
	}
	if req.Slots != nil {
		u.Slots = *req.Slots
	}
	if req.Tags != nil {
		u.Tags = req.Tags
	}

	if err := u.Validate(); err != nil {
		api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	auditCtx := h.createAuditContext(r)
	if err := h.repo.Update(r.Context(), u, auditCtx); err != nil {
		if errors.Is(err, repository.ErrAlreadyExists) {
			api.JSONError(w, http.StatusConflict, "Upstream with this name already exists", "CONFLICT")
			return
		}
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Upstream not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to update upstream", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to update upstream", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Upstream updated", zap.Stringer("id", u.ID))
	api.JSON(w, http.StatusOK, u)
}

func (h *UpstreamHandler) Delete(w http.ResponseWriter, r *http.Request) {
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
			api.JSONError(w, http.StatusNotFound, "Upstream not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to delete upstream", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to delete upstream", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Upstream deleted", zap.Stringer("id", id))
	api.JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *UpstreamHandler) createAuditContext(r *http.Request) *repository.AuditContext {
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
