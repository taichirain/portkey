package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/control/api"
	"github.com/taichirain/portkey/internal/control/middleware"
	"github.com/taichirain/portkey/internal/control/repository"
	"github.com/taichirain/portkey/internal/domain/consumer"
	"go.uber.org/zap"
)

type ConsumerHandler struct {
	repo   repository.ConsumerRepository
	logger *zap.Logger
}

func NewConsumerHandler(repo repository.ConsumerRepository, logger *zap.Logger) *ConsumerHandler {
	return &ConsumerHandler{
		repo:   repo,
		logger: logger,
	}
}

type CreateConsumerRequest struct {
	Username string   `json:"username"`
	CustomID string   `json:"custom_id"`
	Tags     []string `json:"tags"`
}

type UpdateConsumerRequest struct {
	Username *string  `json:"username"`
	CustomID *string  `json:"custom_id"`
	Tags     []string `json:"tags"`
}

func (h *ConsumerHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateConsumerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	c, err := consumer.New(req.Username, req.CustomID)
	if err != nil {
		api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	if req.Tags != nil {
		c.Tags = req.Tags
	}

	if err := c.Validate(); err != nil {
		api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	auditCtx := h.createAuditContext(r)
	if err := h.repo.Create(r.Context(), c, auditCtx); err != nil {
		if errors.Is(err, repository.ErrAlreadyExists) {
			api.JSONError(w, http.StatusConflict, "Consumer with this username or custom_id already exists", "CONFLICT")
			return
		}
		h.logger.Error("Failed to create consumer", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to create consumer", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Consumer created", zap.Stringer("id", c.ID), zap.String("username", c.Username))
	api.JSON(w, http.StatusCreated, c)
}

func (h *ConsumerHandler) GetByID(w http.ResponseWriter, r *http.Request) {
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

	c, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Consumer not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to get consumer", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to get consumer", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, c)
}

func (h *ConsumerHandler) GetByUsername(w http.ResponseWriter, r *http.Request) {
	username := r.URL.Query().Get("username")
	if username == "" {
		api.JSONError(w, http.StatusBadRequest, "Username is required", "VALIDATION_ERROR")
		return
	}

	c, err := h.repo.GetByUsername(r.Context(), username)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Consumer not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to get consumer by username", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to get consumer", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, c)
}

func (h *ConsumerHandler) GetByCustomID(w http.ResponseWriter, r *http.Request) {
	customID := r.URL.Query().Get("custom_id")
	if customID == "" {
		api.JSONError(w, http.StatusBadRequest, "Custom ID is required", "VALIDATION_ERROR")
		return
	}

	c, err := h.repo.GetByCustomID(r.Context(), customID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Consumer not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to get consumer by custom_id", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to get consumer", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, c)
}

func (h *ConsumerHandler) List(w http.ResponseWriter, r *http.Request) {
	page, pageSize := api.ParsePagination(r)

	result, err := h.repo.List(r.Context(), &repository.Pagination{Page: page, PageSize: pageSize})
	if err != nil {
		h.logger.Error("Failed to list consumers", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to list consumers", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, api.ToPaginationResponse(result))
}

func (h *ConsumerHandler) Update(w http.ResponseWriter, r *http.Request) {
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

	c, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Consumer not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to get consumer", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to get consumer", "INTERNAL_ERROR")
		return
	}

	var req UpdateConsumerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	if req.Username != nil {
		c.Username = *req.Username
	}
	if req.CustomID != nil {
		c.CustomID = *req.CustomID
	}
	if req.Tags != nil {
		c.Tags = req.Tags
	}

	if err := c.Validate(); err != nil {
		api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	auditCtx := h.createAuditContext(r)
	if err := h.repo.Update(r.Context(), c, auditCtx); err != nil {
		if errors.Is(err, repository.ErrAlreadyExists) {
			api.JSONError(w, http.StatusConflict, "Consumer with this username or custom_id already exists", "CONFLICT")
			return
		}
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Consumer not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to update consumer", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to update consumer", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Consumer updated", zap.Stringer("id", c.ID))
	api.JSON(w, http.StatusOK, c)
}

func (h *ConsumerHandler) Delete(w http.ResponseWriter, r *http.Request) {
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
			api.JSONError(w, http.StatusNotFound, "Consumer not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to delete consumer", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to delete consumer", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Consumer deleted", zap.Stringer("id", id))
	api.JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *ConsumerHandler) createAuditContext(r *http.Request) *repository.AuditContext {
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
