package access

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/ilter-ai/ilter/internal/model"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"

	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/db/audit"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

type Handler struct {
	store   *db.SQLiteStore
	redis   *redis.Client
	auditor *audit.SQLiteConfigAuditor
}

func NewHandler(store *db.SQLiteStore, redis *redis.Client, auditor *audit.SQLiteConfigAuditor) *Handler {
	return &Handler{store: store, redis: redis, auditor: auditor}
}

func newID() string {
	return uuid.New().String()
}

func (h *Handler) ListAllGrants(w http.ResponseWriter, r *http.Request) {
	serverID := r.URL.Query().Get("server_id")
	if serverID != "" {
		h.listGrantsByServer(w, serverID)
		return
	}

	rows, err := h.store.DB.Query(
		`SELECT g.id, g.subject_type, g.subject_id, g.server_id, s.name, g.tools, g.effect, g.enabled, g.priority, g.created_at
		 FROM mcp_grant g LEFT JOIN mcp_servers s ON g.server_id = s.id
		 ORDER BY g.priority DESC, g.created_at DESC`,
	)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list grants")
		return
	}
	defer rows.Close()

	grants := make([]map[string]any, 0)
	for rows.Next() {
		var id, subjectType, subjectID, serverID, tools, effect, createdAt string
		var serverName sql.NullString
		var enabled, priority int
		if err := rows.Scan(&id, &subjectType, &subjectID, &serverID, &serverName, &tools, &effect, &enabled, &priority, &createdAt); err != nil {
			continue
		}
		grants = append(grants, map[string]any{
			"id":           id,
			"subject_type": subjectType,
			"subject_id":   subjectID,
			"server_id":    serverID,
			"server_name":  nullToEmpty(serverName),
			"tools":        tools,
			"effect":       effect,
			"enabled":      enabled == 1,
			"priority":     priority,
			"created_at":   createdAt,
		})
	}
	model.WriteJSON(w, http.StatusOK, map[string]any{"grants": grants})
}

func (h *Handler) listGrantsByServer(w http.ResponseWriter, serverID string) {
	rows, err := h.store.DB.Query(
		`SELECT id, subject_type, subject_id, server_id, tools, effect, enabled, priority, created_at
		 FROM mcp_grant WHERE server_id = ? ORDER BY priority DESC, created_at DESC`,
		serverID,
	)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list grants")
		return
	}
	defer rows.Close()

	grants := make([]map[string]any, 0)
	for rows.Next() {
		var id, subjectType, subjectID, serverID, tools, effect, createdAt string
		var enabled, priority int
		if err := rows.Scan(&id, &subjectType, &subjectID, &serverID, &tools, &effect, &enabled, &priority, &createdAt); err != nil {
			continue
		}
		grants = append(grants, map[string]any{
			"id":           id,
			"subject_type": subjectType,
			"subject_id":   subjectID,
			"server_id":    serverID,
			"tools":        tools,
			"effect":       effect,
			"enabled":      enabled == 1,
			"priority":     priority,
			"created_at":   createdAt,
		})
	}
	model.WriteJSON(w, http.StatusOK, map[string]any{"grants": grants})
}

func (h *Handler) GetGrant(w http.ResponseWriter, r *http.Request) {
	grantID := chi.URLParam(r, "id")
	if grantID == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Grant ID is required")
		return
	}

	var id, subjectType, subjectID, serverID, tools, effect, createdAt string
	var serverName sql.NullString
	var enabled, priority int
	err := h.store.DB.QueryRow(
		`SELECT g.id, g.subject_type, g.subject_id, g.server_id, s.name, g.tools, g.effect, g.enabled, g.priority, g.created_at
		 FROM mcp_grant g LEFT JOIN mcp_servers s ON g.server_id = s.id
		 WHERE g.id = ?`, grantID,
	).Scan(&id, &subjectType, &subjectID, &serverID, &serverName, &tools, &effect, &enabled, &priority, &createdAt)
	if err == sql.ErrNoRows {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Grant not found")
		return
	}
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to get grant")
		return
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"grant": map[string]any{
			"id":           id,
			"subject_type": subjectType,
			"subject_id":   subjectID,
			"server_id":    serverID,
			"server_name":  nullToEmpty(serverName),
			"tools":        tools,
			"effect":       effect,
			"enabled":      enabled == 1,
			"priority":     priority,
			"created_at":   createdAt,
		},
	})
}

func (h *Handler) CreateGrant(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var req struct {
		SubjectType string `json:"subject_type"`
		SubjectID   string `json:"subject_id"`
		ServerID    string `json:"server_id"`
		Tools       string `json:"tools"`
		Effect      string `json:"effect"`
		Enabled     *bool  `json:"enabled,omitempty"`
		Priority    *int   `json:"priority,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}
	if req.SubjectType != "key" && req.SubjectType != "user" && req.SubjectType != "group" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "subject_type must be 'key', 'user', or 'group'")
		return
	}
	if req.SubjectID == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "subject_id is required")
		return
	}
	if req.Tools == "" {
		req.Tools = "*"
	}
	if req.Effect == "" {
		req.Effect = "allow"
	}
	if req.Effect != "allow" && req.Effect != "deny" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "effect must be 'allow' or 'deny'")
		return
	}
	if req.ServerID == "" {
		req.ServerID = "*"
	}

	enabled := 1
	if req.Enabled != nil && !*req.Enabled {
		enabled = 0
	}
	priority := 0
	if req.Priority != nil {
		priority = *req.Priority
	}

	id := newID()
	_, err := h.store.DB.Exec(
		`INSERT INTO mcp_grant (id, subject_type, subject_id, server_id, tools, effect, enabled, priority, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		id, req.SubjectType, req.SubjectID, req.ServerID, req.Tools, req.Effect, enabled, priority,
	)
	if err != nil {
		slog.Error("Failed to create grant", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to create grant")
		return
	}

	if h.auditor != nil {
		vals := map[string]any{
			"subject_type": req.SubjectType,
			"subject_id":   req.SubjectID,
			"server_id":    req.ServerID,
			"tools":        req.Tools,
			"effect":       req.Effect,
			"enabled":      enabled == 1,
			"priority":     priority,
		}
		if err := h.auditor.LogCreate("mcp_grant", id, vals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Error("failed to log audit create mcp_grant", "error", err)
		}
	}

	model.WriteJSON(w, http.StatusCreated, map[string]any{"status": "ok", "id": id})
}

func (h *Handler) UpdateGrant(w http.ResponseWriter, r *http.Request) {
	grantID := chi.URLParam(r, "id")
	if grantID == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Grant ID is required")
		return
	}

	defer r.Body.Close()

	var req struct {
		SubjectType *string `json:"subject_type,omitempty"`
		SubjectID   *string `json:"subject_id,omitempty"`
		ServerID    *string `json:"server_id,omitempty"`
		Tools       *string `json:"tools,omitempty"`
		Effect      *string `json:"effect,omitempty"`
		Enabled     *bool   `json:"enabled,omitempty"`
		Priority    *int    `json:"priority,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}

	var existingID string
	err := h.store.DB.QueryRow("SELECT id FROM mcp_grant WHERE id = ?", grantID).Scan(&existingID)
	if err == sql.ErrNoRows {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Grant not found")
		return
	}
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to get grant")
		return
	}

	if req.SubjectType != nil {
		st := *req.SubjectType
		if st != "key" && st != "user" && st != "group" {
			model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "subject_type must be 'key', 'user', or 'group'")
			return
		}
	}
	if req.Effect != nil {
		eff := *req.Effect
		if eff != "allow" && eff != "deny" {
			model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "effect must be 'allow' or 'deny'")
			return
		}
	}

	var setClauses []string
	var args []any

	if req.SubjectType != nil {
		setClauses = append(setClauses, "subject_type = ?")
		args = append(args, *req.SubjectType)
	}
	if req.SubjectID != nil {
		setClauses = append(setClauses, "subject_id = ?")
		args = append(args, *req.SubjectID)
	}
	if req.ServerID != nil {
		setClauses = append(setClauses, "server_id = ?")
		args = append(args, *req.ServerID)
	}
	if req.Tools != nil {
		setClauses = append(setClauses, "tools = ?")
		args = append(args, *req.Tools)
	}
	if req.Effect != nil {
		setClauses = append(setClauses, "effect = ?")
		args = append(args, *req.Effect)
	}
	if req.Enabled != nil {
		val := 0
		if *req.Enabled {
			val = 1
		}
		setClauses = append(setClauses, "enabled = ?")
		args = append(args, val)
	}
	if req.Priority != nil {
		setClauses = append(setClauses, "priority = ?")
		args = append(args, *req.Priority)
	}

	if len(setClauses) == 0 {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "No fields to update")
		return
	}

	args = append(args, grantID)
	query := "UPDATE mcp_grant SET " + strings.Join(setClauses, ", ") + " WHERE id = ?"

	// Capture old grant values before update
	var oldSubjectType, oldSubjectID, oldServerID, oldTools, oldEffect string
	var oldEnabled, oldPriority int
	if err = h.store.DB.QueryRow(
		`SELECT subject_type, subject_id, server_id, tools, effect, enabled, priority
		 FROM mcp_grant WHERE id = ?`, grantID,
	).Scan(&oldSubjectType, &oldSubjectID, &oldServerID, &oldTools, &oldEffect, &oldEnabled, &oldPriority); err != nil {
		slog.Warn("Failed to read old grant values for audit", "grant_id", grantID, "error", err)
	}

	_, err = h.store.DB.Exec(query, args...)
	if err != nil {
		slog.Error("Failed to update grant", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to update grant")
		return
	}

	if h.auditor != nil {
		oldVals := map[string]any{
			"subject_type": oldSubjectType,
			"subject_id":   oldSubjectID,
			"server_id":    oldServerID,
			"tools":        oldTools,
			"effect":       oldEffect,
			"enabled":      oldEnabled == 1,
			"priority":     oldPriority,
		}
		newVals := map[string]any{
			"subject_type": ifVal(req.SubjectType, oldSubjectType),
			"subject_id":   ifVal(req.SubjectID, oldSubjectID),
			"server_id":    ifVal(req.ServerID, oldServerID),
			"tools":        ifVal(req.Tools, oldTools),
			"effect":       ifVal(req.Effect, oldEffect),
			"enabled":      ifBoolPtr(req.Enabled, oldEnabled == 1),
			"priority":     ifIntPtr(req.Priority, oldPriority),
		}
		if err := h.auditor.LogUpdate("mcp_grant", grantID, oldVals, newVals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Error("failed to log audit update mcp_grant", "error", err)
		}
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "id": grantID})
}

func (h *Handler) DeleteGrant(w http.ResponseWriter, r *http.Request) {
	grantID := chi.URLParam(r, "id")
	if grantID == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Grant ID is required")
		return
	}

	var oldSubjectType, oldSubjectID, oldServerID, oldTools, oldEffect string
	var oldEnabled, oldPriority int
	if err := h.store.DB.QueryRow(
		`SELECT subject_type, subject_id, server_id, tools, effect, enabled, priority
		 FROM mcp_grant WHERE id = ?`, grantID,
	).Scan(&oldSubjectType, &oldSubjectID, &oldServerID, &oldTools, &oldEffect, &oldEnabled, &oldPriority); err != nil {
		slog.Warn("Failed to read old grant values for audit", "grant_id", grantID, "error", err)
	}

	res, err := h.store.DB.Exec("DELETE FROM mcp_grant WHERE id = ?", grantID)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to delete grant")
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Grant not found")
		return
	}

	if h.auditor != nil && oldSubjectID != "" {
		vals := map[string]any{
			"subject_type": oldSubjectType,
			"subject_id":   oldSubjectID,
			"server_id":    oldServerID,
			"tools":        oldTools,
			"effect":       oldEffect,
			"enabled":      oldEnabled == 1,
			"priority":     oldPriority,
		}
		if err := h.auditor.LogDelete("mcp_grant", grantID, vals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Error("failed to log audit delete mcp_grant", "error", err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ToggleGrant(w http.ResponseWriter, r *http.Request) {
	grantID := chi.URLParam(r, "id")
	if grantID == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Grant ID is required")
		return
	}

	res, err := h.store.DB.Exec(
		"UPDATE mcp_grant SET enabled = CASE WHEN enabled = 1 THEN 0 ELSE 1 END WHERE id = ?",
		grantID,
	)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to toggle grant")
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Grant not found")
		return
	}

	var id, subjectType, subjectID, serverID, tools, effect, createdAt string
	var serverName sql.NullString
	var enabled, priority int
	err = h.store.DB.QueryRow(
		`SELECT g.id, g.subject_type, g.subject_id, g.server_id, s.name, g.tools, g.effect, g.enabled, g.priority, g.created_at
		 FROM mcp_grant g LEFT JOIN mcp_servers s ON g.server_id = s.id
		 WHERE g.id = ?`, grantID,
	).Scan(&id, &subjectType, &subjectID, &serverID, &serverName, &tools, &effect, &enabled, &priority, &createdAt)
	if err != nil {
		model.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
		return
	}

	if h.auditor != nil {
		oldEnabled := 0
		if enabled == 0 {
			oldEnabled = 1
		}
		if err := h.auditor.LogUpdate("mcp_grant", grantID,
			map[string]any{"enabled": oldEnabled == 1},
			map[string]any{"enabled": enabled == 1},
			reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Warn("Failed to log audit update", "error", err)
		}
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"grant": map[string]any{
			"id":           id,
			"subject_type": subjectType,
			"subject_id":   subjectID,
			"server_id":    serverID,
			"server_name":  nullToEmpty(serverName),
			"tools":        tools,
			"effect":       effect,
			"enabled":      enabled == 1,
			"priority":     priority,
			"created_at":   createdAt,
		},
	})
}

func (h *Handler) ListGrantsByServer(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "serverId")
	if serverID == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Server ID is required")
		return
	}
	h.listGrantsByServer(w, serverID)
}

func (h *Handler) GetDefaultPolicy(w http.ResponseWriter, _ *http.Request) {
	var value string
	err := h.store.DB.QueryRow(
		`SELECT value FROM runtime_config WHERE section = 'mcp' AND key = 'default_policy' LIMIT 1`,
	).Scan(&value)
	if err == sql.ErrNoRows {
		model.WriteJSON(w, http.StatusOK, map[string]any{"default_policy": "deny", "note": "default"})
		return
	}
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to read default policy")
		return
	}
	policy := "deny"
	if value == "true" {
		policy = "allow"
	}
	model.WriteJSON(w, http.StatusOK, map[string]any{"default_policy": policy})
}

func (h *Handler) SetDefaultPolicy(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var req struct {
		DefaultPolicy string `json:"default_policy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}
	if req.DefaultPolicy != "allow" && req.DefaultPolicy != "deny" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "default_policy must be 'allow' or 'deny'")
		return
	}

	value := "false"
	if req.DefaultPolicy == "allow" {
		value = "true"
	}

	_, err := h.store.DB.Exec(
		`INSERT INTO runtime_config (section, key, value, updated_at, version)
		 VALUES ('mcp', 'default_policy', ?, datetime('now'), 1)
		 ON CONFLICT(section, key) DO UPDATE SET value = excluded.value, version = version + 1, updated_at = datetime('now')`,
		value,
	)
	if err != nil {
		slog.Error("Failed to set default policy", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to set default policy")
		return
	}

	if h.auditor != nil {
		if err := h.auditor.LogCreate("mcp_default_policy", "global",
			map[string]any{"default_policy": req.DefaultPolicy},
			reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Warn("Failed to log audit create", "error", err)
		}
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "default_policy": req.DefaultPolicy})
}

// TestRuleRequest is the JSON body for POST /api/access/mcp/test-rule.
type TestRuleRequest struct {
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`
	ToolName    string `json:"tool_name"`
}

func (h *Handler) TestRule(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req TestRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}
	if req.SubjectType == "" || req.SubjectID == "" || req.ToolName == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "subject_type, subject_id, and tool_name are required")
		return
	}
	if req.SubjectType != "key" && req.SubjectType != "user" && req.SubjectType != "group" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "subject_type must be 'key', 'user', or 'group'")
		return
	}

	// Resolve server ID from tool name (format "server_id/tool_name" or "server_id__tool_name").
	serverID := ""
	if idx := strings.IndexByte(req.ToolName, '/'); idx >= 0 {
		serverID = req.ToolName[:idx]
	} else if idx := strings.Index(req.ToolName, "__"); idx >= 0 {
		serverID = req.ToolName[:idx]
	}

	rows, err := h.store.DB.Query(
		`SELECT tools, effect, server_id FROM mcp_grant
		 WHERE subject_type = ? AND (subject_id = ? OR subject_id = '*')
		 AND enabled = 1 ORDER BY priority DESC`,
		req.SubjectType, req.SubjectID,
	)
	if err != nil {
		slog.Error("Failed to query grants", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to query grants")
		return
	}
	defer rows.Close()

	allowedResult := false
	matchedRule := ""
	matchedSource := ""
	hasDeny := false
	hasAllow := false

	for rows.Next() {
		var tools, effect, sID string
		if err := rows.Scan(&tools, &effect, &sID); err != nil {
			continue
		}
		if serverID != "" && sID != "*" && sID != serverID {
			continue
		}
		if !toolMatchPattern(tools, req.ToolName) {
			continue
		}
		if effect == "deny" {
			hasDeny = true
			if matchedRule == "" {
				matchedRule = "db_deny:" + tools
				matchedSource = "grant"
			}
		} else {
			hasAllow = true
			if !hasDeny {
				matchedRule = "db_allow:" + tools
				matchedSource = "grant"
			}
		}
	}

	if hasDeny {
		allowedResult = false
		if matchedRule == "" {
			matchedRule = "db_deny:*"
			matchedSource = "grant"
		}
	} else if hasAllow {
		allowedResult = true
	} else {
		// Fall back to default policy when no grants match.
		var value string
		err := h.store.DB.QueryRow(
			`SELECT value FROM runtime_config WHERE section = 'mcp' AND key = 'default_policy' LIMIT 1`,
		).Scan(&value)
		policy := "deny"
		if err == nil && value == "true" {
			policy = "allow"
		}
		allowedResult = policy == "allow"
		matchedRule = "default_policy:" + policy
		matchedSource = "default"
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"allowed":      allowedResult,
		"matched_rule": matchedRule,
		"source":       matchedSource,
	})
}

func toolMatchPattern(pattern, toolName string) bool {
	if pattern == "*" || pattern == "*/*" {
		return true
	}
	if strings.HasPrefix(pattern, "[") {
		t := strings.Trim(pattern, "[]")
		for _, tname := range strings.Split(t, ",") {
			tname = strings.Trim(tname, "\" ")
			if tname == toolName {
				return true
			}
		}
		return false
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(toolName, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == toolName
}

// ifVal returns v if p is non-nil, otherwise fallback.
func ifVal(p *string, fallback string) string {
	if p != nil {
		return *p
	}
	return fallback
}

func ifBoolPtr(p *bool, fallback bool) bool {
	if p != nil {
		return *p
	}
	return fallback
}

func ifIntPtr(p *int, fallback int) int {
	if p != nil {
		return *p
	}
	return fallback
}

func nullToEmpty(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}
