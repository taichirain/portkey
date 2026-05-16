package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/control/repository"
)

type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *ErrorResponse `json:"error,omitempty"`
}

type ErrorResponse struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

type PaginationResponse struct {
	Items      interface{} `json:"items"`
	Total      int         `json:"total"`
	Page       int         `json:"page"`
	PageSize   int         `json:"page_size"`
	TotalPages int         `json:"total_pages"`
}

func JSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		json.NewEncoder(w).Encode(Response{
			Success: true,
			Data:    data,
		})
	} else {
		json.NewEncoder(w).Encode(Response{
			Success: true,
		})
	}
}

func JSONError(w http.ResponseWriter, status int, message string, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(Response{
		Success: false,
		Error: &ErrorResponse{
			Message: message,
			Code:    code,
		},
	})
}

func ParseIDFromPath(r *http.Request, pathPrefix string) (uuid.UUID, error) {
	path := r.URL.Path
	if len(path) <= len(pathPrefix) {
		return uuid.Nil, errors.New("invalid path")
	}
	idStr := path[len(pathPrefix):]
	if idx := len(idStr); idx > 36 {
		idStr = idStr[:36]
	}
	return uuid.Parse(idStr)
}

func ParsePagination(r *http.Request) (page, pageSize int) {
	pageStr := r.URL.Query().Get("page")
	pageSizeStr := r.URL.Query().Get("page_size")

	page = 1
	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	pageSize = 50
	if pageSizeStr != "" {
		if s, err := strconv.Atoi(pageSizeStr); err == nil && s > 0 && s <= 100 {
			pageSize = s
		}
	}

	return page, pageSize
}

func ToPaginationResponse[T any](result *repository.PageResult[T]) *PaginationResponse {
	totalPages := (result.Total + result.PageSize - 1) / result.PageSize
	return &PaginationResponse{
		Items:      result.Items,
		Total:      result.Total,
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: totalPages,
	}
}
