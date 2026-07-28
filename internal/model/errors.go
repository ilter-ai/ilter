package model

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
)

const (
	ErrTypeInvalidRequest    = "invalid_request_error"
	ErrTypeModelNotFound     = "model_not_found"
	ErrTypeAllProvidersFail  = "all_providers_failed"
	ErrTypeInsufficientQuota = "insufficient_quota"
	ErrTypeProviderError     = "provider_error"
	ErrTypeRoutingError      = "routing_error"
	ErrTypeLoopDetected      = "loop_detected"
)

var ErrPIIBlocked = errors.New("pii_blocked")

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to encode JSON response", "status", status, "error", err)
	}
}

func WriteJSONError(w http.ResponseWriter, status int, errType string, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(ErrorResponse{
		Error: ErrorDetail{
			Message: msg,
			Type:    errType,
			Code:    strconv.Itoa(status),
		},
	}); err != nil {
		slog.Error("failed to encode JSON error response", "status", status, "error", err)
	}
}
