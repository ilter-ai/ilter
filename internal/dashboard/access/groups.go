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

// GET /api/groups
func (h *Handler) ListGroups(w http.ResponseWriter, _ *http.Request) {
	all, err := h.store.ListGroups()
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list groups")
		return
	}
	if all == nil {
		all = make([]auth.Group, 0)
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"groups": all,
	})
}

// POST /api/groups
func (h *Handler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req auth.CreateGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}

	if req.Name == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Name is required")
		return
	}

	group, err := h.store.CreateGroup(req)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "UNIQUE") {
			model.WriteJSONError(w, http.StatusConflict, "duplicate_name", "A group with this name already exists")
			return
		}
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request", "Failed to create group: "+errStr)
		return
	}

	if h.auditor != nil {
		vals := map[string]any{
			"name":        group.Name,
			"description": group.Description,
		}
		if err := h.auditor.LogCreate("group", strconv.Itoa(group.ID), vals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Error("failed to log audit create group", "error", err)
		}
	}

	model.WriteJSON(w, http.StatusCreated, group)
}

// GET /api/groups/{id}
func (h *Handler) GetGroup(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid ID format")
		return
	}

	group, err := h.store.GetGroup(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			model.WriteJSONError(w, http.StatusNotFound, "not_found", "Group not found")
			return
		}
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to get group")
		return
	}

	model.WriteJSON(w, http.StatusOK, group)
}

// PUT /api/groups/{id}
func (h *Handler) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid ID format")
		return
	}

	defer r.Body.Close()
	var req auth.UpdateGroupRequest
	if err = json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}

	oldGroup, err := h.store.GetGroup(id)
	if err != nil {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Group not found")
		return
	}

	group, err := h.store.UpdateGroup(id, req)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			model.WriteJSONError(w, http.StatusNotFound, "not_found", "Group not found")
			return
		}
		errStr := err.Error()
		if strings.Contains(errStr, "UNIQUE") {
			model.WriteJSONError(w, http.StatusConflict, "duplicate_name", "A group with this name already exists")
			return
		}
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to update group")
		return
	}

	if h.auditor != nil {
		oldVals := map[string]any{
			"name":        oldGroup.Name,
			"description": oldGroup.Description,
		}
		newVals := map[string]any{
			"name":        group.Name,
			"description": group.Description,
		}
		if err := h.auditor.LogUpdate("group", strconv.Itoa(id), oldVals, newVals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Error("failed to log audit update group", "error", err)
		}
	}

	model.WriteJSON(w, http.StatusOK, group)
}

// DELETE /api/groups/{id}
func (h *Handler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid ID format")
		return
	}

	oldGroup, fetchErr := h.store.GetGroup(id)

	if err := h.store.DeleteGroup(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			model.WriteJSONError(w, http.StatusNotFound, "not_found", "Group not found")
			return
		}
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to delete group")
		return
	}

	if h.auditor != nil && fetchErr == nil {
		vals := map[string]any{
			"name":        oldGroup.Name,
			"description": oldGroup.Description,
		}
		if err := h.auditor.LogDelete("group", strconv.Itoa(id), vals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Error("failed to log audit delete group", "error", err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// POST /api/groups/{groupId}/members
func (h *Handler) AddMember(w http.ResponseWriter, r *http.Request) {
	groupID, err := strconv.Atoi(chi.URLParam(r, "groupId"))
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid group ID")
		return
	}
	defer r.Body.Close()
	var req struct {
		UserID int    `json:"user_id"`
		Role   string `json:"role,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}
	if req.UserID == 0 {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "user_id is required")
		return
	}

	if _, err := h.store.GetGroup(groupID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			model.WriteJSONError(w, http.StatusNotFound, "not_found", "Group not found")
			return
		}
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to verify group")
		return
	}

	if _, err := h.store.GetUser(req.UserID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			model.WriteJSONError(w, http.StatusNotFound, "not_found", "User not found")
			return
		}
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to verify user")
		return
	}

	if err := h.store.AddUserToGroup(req.UserID, groupID, req.Role); err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "UNIQUE") {
			model.WriteJSONError(w, http.StatusConflict, "duplicate_membership", "User is already a member of this group")
			return
		}
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to add member: "+errStr)
		return
	}

	if h.auditor != nil {
		vals := map[string]any{
			"group_id": groupID,
			"user_id":  req.UserID,
			"role":     req.Role,
		}
		if err := h.auditor.LogCreate("group_membership", strconv.Itoa(groupID)+":"+strconv.Itoa(req.UserID), vals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Error("failed to log audit create group_membership", "error", err)
		}
	}

	model.WriteJSON(w, http.StatusCreated, map[string]string{"status": "member_added"})
}

// DELETE /api/groups/{groupId}/members/{userId}
func (h *Handler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	groupID, err := strconv.Atoi(chi.URLParam(r, "groupId"))
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid group ID")
		return
	}
	userID, err := strconv.Atoi(chi.URLParam(r, "userId"))
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid user ID")
		return
	}

	if err := h.store.RemoveUserFromGroup(userID, groupID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			model.WriteJSONError(w, http.StatusNotFound, "not_found", "Membership not found")
			return
		}
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to remove member")
		return
	}

	if h.auditor != nil {
		vals := map[string]any{
			"group_id": groupID,
			"user_id":  userID,
		}
		if err := h.auditor.LogDelete("group_membership", strconv.Itoa(groupID)+":"+strconv.Itoa(userID), vals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Error("failed to log audit delete group_membership", "error", err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// GET /api/groups/{groupId}/members
func (h *Handler) ListMembers(w http.ResponseWriter, r *http.Request) {
	groupID, err := strconv.Atoi(chi.URLParam(r, "groupId"))
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid group ID")
		return
	}

	users, err := h.store.GetGroupUsers(groupID)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list members")
		return
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{"members": users})
}

// GET /api/users/{id}/groups
func (h *Handler) ListUserGroups(w http.ResponseWriter, r *http.Request) {
	userID, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid user ID")
		return
	}

	groups, err := h.store.GetUserGroups(userID)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list user groups")
		return
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{"groups": groups})
}
