package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/control/api"
	"github.com/taichirain/portkey/internal/control/middleware"
	"github.com/taichirain/portkey/internal/control/repository"
	"github.com/taichirain/portkey/internal/domain/trafficpolicy"
	"go.uber.org/zap"
)

type TrafficPolicyHandler struct {
	repo   repository.TrafficPolicyRepository
	logger *zap.Logger
}

func NewTrafficPolicyHandler(repo repository.TrafficPolicyRepository, logger *zap.Logger) *TrafficPolicyHandler {
	return &TrafficPolicyHandler{
		repo:   repo,
		logger: logger,
	}
}

type CreateTrafficPolicyRequest struct {
	Name            string          `json:"name"`
	RouteID         string          `json:"route_id"`
	Priority        *int            `json:"priority"`
	Type            string          `json:"type"`
	MatchConfig     json.RawMessage `json:"match_config"`
	TargetServiceID string          `json:"target_service_id"`
	Enabled         *bool           `json:"enabled"`
	Tags            []string        `json:"tags"`
}

type UpdateTrafficPolicyRequest struct {
	Name            *string         `json:"name"`
	RouteID         *string         `json:"route_id"`
	Priority        *int            `json:"priority"`
	Type            *string          `json:"type"`
	MatchConfig     *json.RawMessage `json:"match_config"`
	TargetServiceID *string          `json:"target_service_id"`
	Enabled         *bool           `json:"enabled"`
	Tags            []string        `json:"tags"`
}

func (h *TrafficPolicyHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateTrafficPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	var routeID uuid.UUID
	if req.RouteID != "" {
		var err error
		routeID, err = uuid.Parse(req.RouteID)
		if err != nil {
			api.JSONError(w, http.StatusBadRequest, "Invalid route_id", "VALIDATION_ERROR")
			return
		}
	}

	var targetServiceID uuid.UUID
	if req.TargetServiceID != "" {
		var err error
		targetServiceID, err = uuid.Parse(req.TargetServiceID)
		if err != nil {
			api.JSONError(w, http.StatusBadRequest, "Invalid target_service_id", "VALIDATION_ERROR")
			return
		}
	}

	tp, err := trafficpolicy.New(routeID, targetServiceID)
	if err != nil {
		api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	if req.Name != "" {
		tp.Name = req.Name
	}
	if req.Priority != nil {
		tp.Priority = *req.Priority
	}
	if req.Type != "" {
		tp.Type = trafficpolicy.PolicyType(req.Type)
	}
	if len(req.MatchConfig) > 0 {
		tp.MatchConfig = req.MatchConfig
	}
	if req.Enabled != nil {
		tp.Enabled = *req.Enabled
	}
	if req.Tags != nil {
		tp.Tags = req.Tags
	}

	if err := tp.Validate(); err != nil {
		api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	auditCtx := h.createAuditContext(r)
	if err := h.repo.Create(r.Context(), tp, auditCtx); err != nil {
		if errors.Is(err, repository.ErrAlreadyExists) {
			api.JSONError(w, http.StatusConflict, "Priority already exists for this route", "CONFLICT")
			return
		}
		if errors.Is(err, repository.ErrInvalidInput) {
			api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
			return
		}
		h.logger.Error("Failed to create traffic policy", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to create traffic policy", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Traffic policy created", zap.Stringer("id", tp.ID))
	api.JSON(w, http.StatusCreated, tp)
}

func (h *TrafficPolicyHandler) GetByID(w http.ResponseWriter, r *http.Request) {
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

	tp, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Traffic policy not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to get traffic policy", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to get traffic policy", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, tp)
}

func (h *TrafficPolicyHandler) ListByRouteID(w http.ResponseWriter, r *http.Request) {
	routeIDStr := r.URL.Query().Get("route_id")
	if routeIDStr == "" {
		api.JSONError(w, http.StatusBadRequest, "route_id is required", "VALIDATION_ERROR")
		return
	}

	routeID, err := uuid.Parse(routeIDStr)
	if err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid route_id format", "VALIDATION_ERROR")
		return
	}

	policies, err := h.repo.ListByRouteID(r.Context(), routeID)
	if err != nil {
		h.logger.Error("Failed to list traffic policies by route", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to list traffic policies", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, policies)
}

func (h *TrafficPolicyHandler) List(w http.ResponseWriter, r *http.Request) {
	page, pageSize := api.ParsePagination(r)

	result, err := h.repo.List(r.Context(), &repository.Pagination{Page: page, PageSize: pageSize})
	if err != nil {
		h.logger.Error("Failed to list traffic policies", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to list traffic policies", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, api.ToPaginationResponse(result))
}

func (h *TrafficPolicyHandler) Update(w http.ResponseWriter, r *http.Request) {
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

	tp, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Traffic policy not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to get traffic policy", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to get traffic policy", "INTERNAL_ERROR")
		return
	}

	var req UpdateTrafficPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.JSONError(w, http.StatusBadRequest, "Invalid request body", "INVALID_REQUEST")
		return
	}

	if req.Name != nil {
		tp.Name = *req.Name
	}
	if req.RouteID != nil {
		routeID, err := uuid.Parse(*req.RouteID)
		if err != nil {
			api.JSONError(w, http.StatusBadRequest, "Invalid route_id", "VALIDATION_ERROR")
			return
		}
		tp.RouteID = routeID
	}
	if req.Priority != nil {
		tp.Priority = *req.Priority
	}
	if req.Type != nil {
		tp.Type = trafficpolicy.PolicyType(*req.Type)
	}
	if req.MatchConfig != nil {
		tp.MatchConfig = *req.MatchConfig
	}
	if req.TargetServiceID != nil {
		targetServiceID, err := uuid.Parse(*req.TargetServiceID)
		if err != nil {
			api.JSONError(w, http.StatusBadRequest, "Invalid target_service_id", "VALIDATION_ERROR")
			return
		}
		tp.TargetServiceID = targetServiceID
	}
	if req.Enabled != nil {
		tp.Enabled = *req.Enabled
	}
	if req.Tags != nil {
		tp.Tags = req.Tags
	}

	if err := tp.Validate(); err != nil {
		api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
		return
	}

	auditCtx := h.createAuditContext(r)
	if err := h.repo.Update(r.Context(), tp, auditCtx); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			api.JSONError(w, http.StatusNotFound, "Traffic policy not found", "NOT_FOUND")
			return
		}
		if errors.Is(err, repository.ErrAlreadyExists) {
			api.JSONError(w, http.StatusConflict, "Priority already exists for this route", "CONFLICT")
			return
		}
		if errors.Is(err, repository.ErrInvalidInput) {
			api.JSONError(w, http.StatusBadRequest, err.Error(), "VALIDATION_ERROR")
			return
		}
		h.logger.Error("Failed to update traffic policy", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to update traffic policy", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Traffic policy updated", zap.Stringer("id", tp.ID))
	api.JSON(w, http.StatusOK, tp)
}

func (h *TrafficPolicyHandler) Delete(w http.ResponseWriter, r *http.Request) {
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
			api.JSONError(w, http.StatusNotFound, "Traffic policy not found", "NOT_FOUND")
			return
		}
		h.logger.Error("Failed to delete traffic policy", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to delete traffic policy", "INTERNAL_ERROR")
		return
	}

	h.logger.Info("Traffic policy deleted", zap.Stringer("id", id))
	api.JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (h *TrafficPolicyHandler) createAuditContext(r *http.Request) *repository.AuditContext {
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
