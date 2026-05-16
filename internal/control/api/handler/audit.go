package handler

import (
	"net/http"

	"github.com/taichirain/portkey/internal/control/api"
	"github.com/taichirain/portkey/internal/control/repository"
	"go.uber.org/zap"
)

type AuditHandler struct {
	repo   repository.AuditRepository
	logger *zap.Logger
}

func NewAuditHandler(repo repository.AuditRepository, logger *zap.Logger) *AuditHandler {
	return &AuditHandler{
		repo:   repo,
		logger: logger,
	}
}

func (h *AuditHandler) List(w http.ResponseWriter, r *http.Request) {
	page, pageSize := api.ParsePagination(r)

	result, err := h.repo.List(r.Context(), &repository.Pagination{Page: page, PageSize: pageSize})
	if err != nil {
		h.logger.Error("Failed to list audit logs", zap.Error(err))
		api.JSONError(w, http.StatusInternalServerError, "Failed to list audit logs", "INTERNAL_ERROR")
		return
	}

	api.JSON(w, http.StatusOK, api.ToPaginationResponse(result))
}
