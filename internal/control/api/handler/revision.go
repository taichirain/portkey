package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/control/api"
	"github.com/taichirain/portkey/internal/control/middleware"
	"github.com/taichirain/portkey/internal/control/publisher"
	"github.com/taichirain/portkey/internal/control/repository"
	"github.com/taichirain/portkey/internal/control/validator"
	"go.uber.org/zap"
)

type RevisionHandler struct {
	publisher *publisher.ConfigPublisher
	logger    *zap.Logger
}

type CreateRevisionRequest struct {
	Version     string `json:"version"`
	Description string `json:"description"`
}

type PublishRevisionRequest struct {
	RevisionID string `json:"revision_id"`
}

type RollbackRequest struct {
	TargetRevisionID string `json:"target_revision_id"`
}

type ValidationResponse struct {
	Valid  bool                    `json:"valid"`
	Errors []validator.ValidationError `json:"errors,omitempty"`
}

type SnapshotResponse struct {
	Timestamp string                `json:"timestamp"`
	Services  int                   `json:"services_count"`
	Routes    int                   `json:"routes_count"`
	Upstreams int                   `json:"upstreams_count"`
}

func NewRevisionHandler(pub *publisher.ConfigPublisher, logger *zap.Logger) *RevisionHandler {
	return &RevisionHandler{
		publisher: pub,
		logger:    logger,
	}
}

func (h *RevisionHandler) Validate(w http.ResponseWriter, r *http.Request) {
	h.logger.Info("Validating configuration")

	result, err := h.publisher.Validate(r.Context())
	if err != nil {
		h.logger.Error("Validation failed", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Validation failed", "INTERNAL_ERROR")
		return
	}

	response := ValidationResponse{
		Valid:  result.Valid,
		Errors: result.Errors,
	}

	if result.Valid {
		h.logger.Info("Configuration validation passed")
	} else {
		h.logger.Warn("Configuration validation failed", zap.Int("error_count", len(result.Errors)))
	}

	api.JSON(w, http.StatusOK, response)
}

func (h *RevisionHandler) CreateSnapshot(w http.ResponseWriter, r *http.Request) {
	h.logger.Info("Creating configuration snapshot")

	snap, err := h.publisher.CreateSnapshot(r.Context())
	if err != nil {
		h.logger.Error("Failed to create snapshot", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to create snapshot", "INTERNAL_ERROR")
		return
	}

	response := SnapshotResponse{
		Timestamp: snap.Timestamp.Format("2006-01-02T15:04:05Z"),
		Services:  len(snap.Services),
		Routes:    len(snap.Routes),
		Upstreams: len(snap.Upstreams),
	}

	h.logger.Info("Snapshot created successfully",
		zap.Int("services", len(snap.Services)),
		zap.Int("routes", len(snap.Routes)),
		zap.Int("upstreams", len(snap.Upstreams)),
	)

	api.JSON(w, http.StatusOK, response)
}

func (h *RevisionHandler) CreateRevision(w http.ResponseWriter, r *http.Request) {
	var req CreateRevisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	if req.Version == "" {
		api.JSONError(w, http.StatusBadRequest, "Version is required", "VALIDATION_ERROR")
		return
	}

	h.logger.Info("Creating revision", zap.String("version", req.Version))

	adminID, ok := middleware.GetAdminID(r.Context())
	if !ok {
		api.JSONError(w, http.StatusUnauthorized, "Admin ID not found in context", "UNAUTHORIZED")
		return
	}

	auditCtx := h.createAuditContext(r)

	result, err := h.publisher.CreateRevision(r.Context(), req.Version, req.Description, &adminID, auditCtx)
	if err != nil {
		if errors.Is(err, validator.ErrValidationFailed) {
			api.JSONError(w, http.StatusBadRequest, "Configuration validation failed", "VALIDATION_ERROR")
			return
		}
		h.logger.Error("Failed to create revision", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to create revision", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Revision created successfully",
		zap.Stringer("revision_id", result.RevisionID),
		zap.String("version", result.Version),
	)

	api.JSON(w, http.StatusCreated, result)
}

func (h *RevisionHandler) Publish(w http.ResponseWriter, r *http.Request) {
	var req PublishRevisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	if req.RevisionID == "" {
		api.JSONError(w, http.StatusBadRequest, "Revision ID is required", "VALIDATION_ERROR")
		return
	}

	revisionID, err := uuid.Parse(req.RevisionID)
	if err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid revision_id", "VALIDATION_ERROR")
		return
	}

	h.logger.Info("Publishing revision", zap.Stringer("revision_id", revisionID))

	auditCtx := h.createAuditContext(r)

	result, err := h.publisher.Publish(r.Context(), revisionID, auditCtx)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Revision not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to publish revision", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to publish revision", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Revision published successfully",
		zap.Stringer("revision_id", result.RevisionID),
		zap.String("version", result.Version),
	)

	api.JSON(w, http.StatusOK, result)
}

func (h *RevisionHandler) CreateAndPublish(w http.ResponseWriter, r *http.Request) {
	var req CreateRevisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	if req.Version == "" {
		api.JSONError(w, http.StatusBadRequest, "Version is required", "VALIDATION_ERROR")
		return
	}

	h.logger.Info("Creating and publishing revision", zap.String("version", req.Version))

	adminID, ok := middleware.GetAdminID(r.Context())
	if !ok {
		api.JSONError(w, http.StatusUnauthorized, "Admin ID not found in context", "UNAUTHORIZED")
		return
	}

	auditCtx := h.createAuditContext(r)

	result, err := h.publisher.CreateAndPublish(r.Context(), req.Version, req.Description, &adminID, auditCtx)
	if err != nil {
		if errors.Is(err, validator.ErrValidationFailed) {
			api.JSONError(w, http.StatusBadRequest, "Configuration validation failed", "VALIDATION_ERROR")
			return
		}
		h.logger.Error("Failed to create and publish revision", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to create and publish revision", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Revision created and published successfully",
		zap.Stringer("revision_id", result.RevisionID),
		zap.String("version", result.Version),
	)

	api.JSON(w, http.StatusCreated, result)
}

func (h *RevisionHandler) GetActive(w http.ResponseWriter, r *http.Request) {
	h.logger.Info("Getting active revision")

	rev, err := h.publisher.GetActiveRevision(r.Context())
	if err != nil {
		if errors.Is(err, publisher.ErrNoActiveRevision) {
			api.JSONError(w, http.StatusNotFound, "No active revision", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to get active revision", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to get active revision", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, rev)
}

func (h *RevisionHandler) GetByID(w http.ResponseWriter, r *http.Request) {
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

	rev, err := h.publisher.GetRevision(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Revision not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to get revision", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to get revision", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, rev)
}

func (h *RevisionHandler) List(w http.ResponseWriter, r *http.Request) {
	page, pageSize := api.ParsePagination(r)

	result, err := h.publisher.ListRevisions(r.Context(), page, pageSize)
	if err != nil {
		h.logger.Error("Failed to list revisions", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to list revisions", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, api.ToPaginationResponse(result))
}

func (h *RevisionHandler) Rollback(w http.ResponseWriter, r *http.Request) {
	var req RollbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	if req.TargetRevisionID == "" {
		api.JSONError(w, http.StatusBadRequest, "Target revision ID is required", "VALIDATION_ERROR")
		return
	}

	targetID, err := uuid.Parse(req.TargetRevisionID)
	if err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid target_revision_id", "VALIDATION_ERROR")
		return
	}

	h.logger.Info("Rolling back to revision", zap.Stringer("target_revision_id", targetID))

	auditCtx := h.createAuditContext(r)

	result, err := h.publisher.Rollback(r.Context(), targetID, auditCtx)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Target revision not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to rollback", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to rollback", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Rollback successful",
		zap.Stringer("target_revision_id", result.RevisionID),
		zap.String("version", result.Version),
	)

	api.JSON(w, http.StatusOK, result)
}

func (h *RevisionHandler) createAuditContext(r *http.Request) *repository.AuditContext {
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
