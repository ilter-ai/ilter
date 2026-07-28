package dashboard

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ilter-ai/ilter/internal/features/pii"
	"github.com/ilter-ai/ilter/internal/model"
)

// ---------------------------------------------------------------------------
// PII Pattern management
// ---------------------------------------------------------------------------

type PatternItem struct {
	Name    string `json:"name"`
	Regex   string `json:"regex"`
	Enabled bool   `json:"enabled"`
	Action  string `json:"action"`
}

type piiPatternRequest struct {
	Name    string `json:"name"`
	Regex   string `json:"regex"`
	Enabled *bool  `json:"enabled,omitempty"`
	Action  string `json:"action,omitempty"`
}

func isValidPIIAction(a string) bool {
	switch a {
	case pii.ActionMask, pii.ActionMaskReversible, pii.ActionBlock, pii.ActionLogOnly, "":
		return true
	}
	return false
}

// HandleListPatterns lists all PII patterns.
func (h *PIIHandler) HandleListPatterns(w http.ResponseWriter, _ *http.Request) {
	rows, err := h.store.DB.Query(
		"SELECT name, regex, enabled, COALESCE(action, 'mask') FROM pii_patterns ORDER BY name",
	)
	if err != nil {
		slog.Error("Failed to query pii_patterns", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	defer rows.Close()

	items := make([]PatternItem, 0)
	for rows.Next() {
		var item PatternItem
		if err := rows.Scan(&item.Name, &item.Regex, &item.Enabled, &item.Action); err != nil {
			slog.Error("Failed to scan pii_pattern row", "error", err)
			continue
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		slog.Error("Error iterating pii_pattern rows", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	model.WriteJSON(w, http.StatusOK, items)
}

// HandleCreatePattern creates a new PII pattern.
func (h *PIIHandler) HandleCreatePattern(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req piiPatternRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}
	if req.Name == "" || req.Regex == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "name and regex are required")
		return
	}
	if !isValidPIIAction(req.Action) {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "invalid action")
		return
	}
	if _, err := regexp.Compile(req.Regex); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid regex: "+err.Error())
		return
	}

	enabled := 1
	if req.Enabled != nil && !*req.Enabled {
		enabled = 0
	}
	action := req.Action
	if action == "" {
		action = pii.ActionMask
	}

	_, err := h.store.DB.Exec(
		"INSERT INTO pii_patterns (name, regex, enabled, action) VALUES (?, ?, ?, ?)",
		req.Name, req.Regex, enabled, action,
	)
	if err != nil {
		slog.Error("Failed to create pii pattern", "name", req.Name, "error", err)
		model.WriteJSONError(w, http.StatusConflict, "conflict", "Pattern already exists: "+err.Error())
		return
	}

	if err := pii.LoadPatternsFromDB(h.store.DB); err != nil {
		slog.Warn("Failed to reload PII patterns after create", "error", err)
	}

	slog.Info("PII pattern created", "name", req.Name)
	model.WriteJSON(w, http.StatusCreated, map[string]any{"status": "ok", "name": req.Name})
}

// HandleUpdatePattern updates an existing PII pattern.
func (h *PIIHandler) HandleUpdatePattern(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "pattern name is required")
		return
	}

	defer r.Body.Close()
	var req piiPatternRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}
	if !isValidPIIAction(req.Action) {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "invalid action")
		return
	}
	if req.Regex != "" {
		if _, err := regexp.Compile(req.Regex); err != nil {
			model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid regex: "+err.Error())
			return
		}
	}

	setClauses := []string{}
	args := []any{}
	if req.Regex != "" {
		setClauses = append(setClauses, "regex = ?")
		args = append(args, req.Regex)
	}
	if req.Enabled != nil {
		val := 0
		if *req.Enabled {
			val = 1
		}
		setClauses = append(setClauses, "enabled = ?")
		args = append(args, val)
	}
	if req.Action != "" {
		setClauses = append(setClauses, "action = ?")
		args = append(args, req.Action)
	}
	if len(setClauses) == 0 {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "no fields to update")
		return
	}
	args = append(args, name)

	result, err := h.store.DB.Exec(
		"UPDATE pii_patterns SET "+strings.Join(setClauses, ", ")+" WHERE name = ?",
		args...,
	)
	if err != nil {
		slog.Error("Failed to update pii pattern", "name", name, "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Pattern not found")
		return
	}

	if err := pii.LoadPatternsFromDB(h.store.DB); err != nil {
		slog.Warn("Failed to reload PII patterns after update", "error", err)
	}

	slog.Info("PII pattern updated", "name", name)
	model.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "name": name})
}

// HandleDeletePattern deletes a PII pattern.
func (h *PIIHandler) HandleDeletePattern(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "pattern name is required")
		return
	}

	result, err := h.store.DB.Exec("DELETE FROM pii_patterns WHERE name = ?", name)
	if err != nil {
		slog.Error("Failed to delete pii pattern", "name", name, "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	if n, _ := result.RowsAffected(); n == 0 {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Pattern not found")
		return
	}

	if err := pii.LoadPatternsFromDB(h.store.DB); err != nil {
		slog.Warn("Failed to reload PII patterns after delete", "error", err)
	}

	slog.Info("PII pattern deleted", "name", name)
	w.WriteHeader(http.StatusNoContent)
}

// HandleReloadPatterns forces a reload of PII patterns from DB into the runtime masker.
func (h *PIIHandler) HandleReloadPatterns(w http.ResponseWriter, _ *http.Request) {
	if err := pii.LoadPatternsFromDB(h.store.DB); err != nil {
		slog.Error("Failed to reload PII patterns", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	slog.Info("PII patterns reloaded")
	model.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}
