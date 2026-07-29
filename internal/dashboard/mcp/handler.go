package dashmcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/ilter-ai/ilter/internal/model"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/db/audit"
	"github.com/ilter-ai/ilter/internal/features/mcp"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

type MCPHandler struct {
	store         *db.SQLiteStore
	auditLogger   *mcp.AuditLogger
	registry      *mcp.Registry
	configAuditor *audit.SQLiteConfigAuditor
}

func NewMCPHandler(store *db.SQLiteStore, auditLogger *mcp.AuditLogger, configAuditor *audit.SQLiteConfigAuditor) *MCPHandler {
	return &MCPHandler{
		store:         store,
		auditLogger:   auditLogger,
		configAuditor: configAuditor,
	}
}

func (h *MCPHandler) SetRegistry(r *mcp.Registry) {
	h.registry = r
}

func (h *MCPHandler) SyncAllEnabledServers(ctx context.Context) {
	mcpLog := slog.With("component", "mcp-sync-all")

	rows, err := h.store.DB.QueryContext(ctx, "SELECT id FROM mcp_servers WHERE enabled = 1")
	if err != nil {
		mcpLog.Error("failed to query enabled MCP servers", "error", err)
		return
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}

	mcpLog.Info("auto-syncing MCP servers on startup", "count", len(ids))

	errCount := 0
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		if err := h.syncServerByID(ctx, id); err != nil {
			errCount++
			mcpLog.Warn("failed to sync MCP server on startup", "server_id", id, "error", err)
		}
	}

	mcpLog.Info("synced MCP servers on startup", "count", len(ids)-errCount, "errors", errCount)
}

func nullToEmpty(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}

// nullToEmptyOr is nullToEmpty with a fallback for the empty-string case
// (not just SQL NULL) — used for protocol_version, where "" and NULL both
// mean "not set, use the default negotiation behavior".
func nullToEmptyOr(s sql.NullString, fallback string) string {
	if v := nullToEmpty(s); v != "" {
		return v
	}
	return fallback
}

var idSlugRegex = regexp.MustCompile(`[^a-z0-9-]`)

func slugify(name string) string {
	slug := strings.ToLower(name)
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = idSlugRegex.ReplaceAllString(slug, "")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "mcp-server"
	}
	return slug
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func countEnabledServers(store *db.SQLiteStore) int {
	var count int
	if err := store.DB.QueryRow("SELECT COUNT(*) FROM mcp_servers WHERE enabled = 1").Scan(&count); err != nil {
		slog.Warn("Failed to count enabled MCP servers", "error", err)
	}
	return count
}

// extractOAuthURL scans stderr for an OAuth authorization URL.
// Matches https://.../authorize?... patterns printed by MCP runners.
func extractOAuthURL(stderr string) string {
	if stderr == "" {
		return ""
	}
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "/authorize") && strings.HasPrefix(line, "https://") {
			return line
		}
	}
	return ""
}

func extractStderr(client mcp.TransportClient) string {
	type stderrProvider interface {
		Stderr() string
	}
	if sc, ok := client.(stderrProvider); ok {
		return sc.Stderr()
	}
	return ""
}

func (h *MCPHandler) ListAllGrants(w http.ResponseWriter, _ *http.Request) {
	rows, err := h.store.DB.Query(
		`SELECT g.id, g.subject_type, g.subject_id, g.server_id, s.name, g.tools, g.effect, g.created_at
		 FROM mcp_grant g LEFT JOIN mcp_servers s ON g.server_id = s.id
		 ORDER BY g.created_at DESC`,
	)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list grants")
		return
	}
	defer rows.Close()

	grants := make([]map[string]any, 0)
	for rows.Next() {
		var id, subjectType, subjectID, serverID, tools, createdAt string
		var serverName sql.NullString
		var effect string
		if err := rows.Scan(&id, &subjectType, &subjectID, &serverID, &serverName, &tools, &effect, &createdAt); err != nil {
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
			"created_at":   createdAt,
		})
	}
	model.WriteJSON(w, http.StatusOK, map[string]any{"grants": grants})
}

func (h *MCPHandler) ListGrants(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "id")
	rows, err := h.store.DB.Query(
		`SELECT id, subject_type, subject_id, server_id, tools, effect, created_at FROM mcp_grant WHERE server_id = ? ORDER BY created_at DESC`,
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
		if err := rows.Scan(&id, &subjectType, &subjectID, &serverID, &tools, &effect, &createdAt); err != nil {
			continue
		}
		grants = append(grants, map[string]any{
			"id":           id,
			"subject_type": subjectType,
			"subject_id":   subjectID,
			"server_id":    serverID,
			"tools":        tools,
			"effect":       effect,
			"created_at":   createdAt,
		})
	}
	model.WriteJSON(w, http.StatusOK, map[string]any{"grants": grants})
}

func (h *MCPHandler) CreateGrant(w http.ResponseWriter, r *http.Request) {
	serverID := chi.URLParam(r, "id")

	var req struct {
		SubjectType string `json:"subject_type"`
		SubjectID   string `json:"subject_id"`
		Tools       string `json:"tools"`
		Effect      string `json:"effect"`
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

	id := uuid.New().String()
	_, err := h.store.DB.Exec(
		`INSERT INTO mcp_grant (id, subject_type, subject_id, server_id, tools, effect, created_at) VALUES (?, ?, ?, ?, ?, ?, datetime('now'))`,
		id, req.SubjectType, req.SubjectID, serverID, req.Tools, req.Effect,
	)
	if err != nil {
		slog.Error("Failed to create grant", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to create grant")
		return
	}

	if h.configAuditor != nil {
		vals := map[string]any{
			"subject_type": req.SubjectType,
			"subject_id":   req.SubjectID,
			"server_id":    serverID,
			"tools":        req.Tools,
			"effect":       req.Effect,
		}
		if err := h.configAuditor.LogCreate("mcp_grant", id, vals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Error("failed to log audit create mcp_grant", "error", err)
		}
	}

	model.WriteJSON(w, http.StatusCreated, map[string]any{"status": "ok", "id": id})
}

func (h *MCPHandler) DeleteGrant(w http.ResponseWriter, r *http.Request) {
	grantID := chi.URLParam(r, "grantId")

	var oldSubjectType, oldSubjectID, oldServerID, oldTools, oldEffect string
	if err := h.store.DB.QueryRow(
		"SELECT subject_type, subject_id, server_id, tools, effect FROM mcp_grant WHERE id = ?", grantID,
	).Scan(&oldSubjectType, &oldSubjectID, &oldServerID, &oldTools, &oldEffect); err != nil {
		slog.Warn("Failed to read old grant for audit", "grant_id", grantID, "error", err)
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

	if h.configAuditor != nil && oldSubjectID != "" {
		vals := map[string]any{
			"subject_type": oldSubjectType,
			"subject_id":   oldSubjectID,
			"server_id":    oldServerID,
			"tools":        oldTools,
			"effect":       oldEffect,
		}
		if err := h.configAuditor.LogDelete("mcp_grant", grantID, vals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Error("failed to log audit delete mcp_grant", "error", err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
