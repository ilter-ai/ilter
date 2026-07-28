package access

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/ilter-ai/ilter/internal/model"

	"github.com/go-chi/chi/v5"

	"github.com/ilter-ai/ilter/internal/auth"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

func (h *Handler) ListUsers(w http.ResponseWriter, _ *http.Request) {
	all, err := h.store.ListUsers()
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list users")
		return
	}
	if all == nil {
		all = make([]auth.User, 0)
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"users": all,
	})
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req auth.CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}

	if req.Name == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Name is required")
		return
	}
	if req.Email == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Email is required")
		return
	}
	if req.Password != "" && len(req.Password) < 6 {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Password must be at least 6 characters")
		return
	}

	user, err := h.store.CreateUser(req)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "UNIQUE") {
			model.WriteJSONError(w, http.StatusConflict, "duplicate_email", "A user with this email already exists")
			return
		}
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request", "Failed to create user: "+errStr)
		return
	}

	if h.auditor != nil {
		vals := map[string]any{
			"name":     user.Name,
			"email":    user.Email,
			"status":   user.Status,
			"budget":   user.Budget,
			"metadata": user.Metadata,
		}
		if user.PasswordHash != "" {
			vals["password_hash"] = "***"
		}
		if err := h.auditor.LogCreate("user", strconv.Itoa(user.ID), vals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Error("failed to log audit create user", "error", err)
		}
	}

	model.WriteJSON(w, http.StatusCreated, user)
}

func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid ID format")
		return
	}

	user, err := h.store.GetUser(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			model.WriteJSONError(w, http.StatusNotFound, "not_found", "User not found")
			return
		}
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to get user")
		return
	}

	model.WriteJSON(w, http.StatusOK, user)
}

func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid ID format")
		return
	}

	defer r.Body.Close()
	var req auth.UpdateUserRequest
	if err = json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}

	oldUser, err := h.store.GetUser(id)
	if err != nil {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "User not found")
		return
	}

	user, err := h.store.UpdateUser(id, req)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			model.WriteJSONError(w, http.StatusNotFound, "not_found", "User not found")
			return
		}
		errStr := err.Error()
		if strings.Contains(errStr, "UNIQUE") {
			model.WriteJSONError(w, http.StatusConflict, "duplicate_email", "A user with this email already exists")
			return
		}
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to update user")
		return
	}

	if h.auditor != nil {
		oldVals := map[string]any{
			"name":     oldUser.Name,
			"email":    oldUser.Email,
			"status":   oldUser.Status,
			"budget":   oldUser.Budget,
			"metadata": oldUser.Metadata,
		}
		if oldUser.PasswordHash != "" {
			oldVals["password_hash"] = "***"
		}
		newVals := map[string]any{
			"name":     user.Name,
			"email":    user.Email,
			"status":   user.Status,
			"budget":   user.Budget,
			"metadata": user.Metadata,
		}
		if user.PasswordHash != "" {
			newVals["password_hash"] = "***"
		}
		if err := h.auditor.LogUpdate("user", strconv.Itoa(id), oldVals, newVals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Error("failed to log audit update user", "error", err)
		}
	}

	model.WriteJSON(w, http.StatusOK, user)
}

func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid ID format")
		return
	}

	oldUser, fetchErr := h.store.GetUser(id)

	if err := h.store.DeleteUser(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			model.WriteJSONError(w, http.StatusNotFound, "not_found", "User not found")
			return
		}
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to delete user")
		return
	}

	if h.auditor != nil && fetchErr == nil {
		vals := map[string]any{
			"name":     oldUser.Name,
			"email":    oldUser.Email,
			"status":   oldUser.Status,
			"budget":   oldUser.Budget,
			"metadata": oldUser.Metadata,
		}
		if oldUser.PasswordHash != "" {
			vals["password_hash"] = "***"
		}
		if err := h.auditor.LogDelete("user", strconv.Itoa(id), vals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Error("failed to log audit delete user", "error", err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
