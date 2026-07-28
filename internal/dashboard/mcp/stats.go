package dashmcp

import (
	"database/sql"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/ilter-ai/ilter/internal/model"

	"github.com/ilter-ai/ilter/internal/features/mcp"
)

func (h *MCPHandler) GetStats(w http.ResponseWriter, _ *http.Request) {
	var totalServers, totalTools, totalGrantCount int

	if err := h.store.DB.QueryRow("SELECT COUNT(*) FROM mcp_servers").Scan(&totalServers); err != nil {
		slog.Warn("Failed to count MCP servers", "error", err)
	}
	if err := h.store.DB.QueryRow("SELECT COUNT(*) FROM mcp_tools").Scan(&totalTools); err != nil {
		slog.Warn("Failed to count MCP tools", "error", err)
	}
	if err := h.store.DB.QueryRow("SELECT COUNT(*) FROM mcp_grant").Scan(&totalGrantCount); err != nil {
		slog.Warn("Failed to count MCP grants", "error", err)
	}

	var totalCalls int
	var avgDuration sql.NullFloat64
	var errorCount int
	if err := h.store.DB.QueryRow("SELECT COUNT(*) FROM mcp_audit_log").Scan(&totalCalls); err != nil {
		slog.Warn("Failed to count MCP calls", "error", err)
	}
	if err := h.store.DB.QueryRow("SELECT AVG(duration_ms) FROM mcp_audit_log WHERE success = 1").Scan(&avgDuration); err != nil {
		slog.Warn("Failed to average MCP duration", "error", err)
	}
	if err := h.store.DB.QueryRow("SELECT COUNT(*) FROM mcp_audit_log WHERE success = 0").Scan(&errorCount); err != nil {
		slog.Warn("Failed to count MCP errors", "error", err)
	}

	callsByTool := []map[string]any{}
	rows, err := h.store.DB.Query(`SELECT tool, COUNT(*) as count FROM mcp_audit_log
		WHERE created_at >= datetime('now', '-24 hours') GROUP BY tool ORDER BY count DESC LIMIT 10`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var tool string
			var count int
			if err := rows.Scan(&tool, &count); err == nil {
				callsByTool = append(callsByTool, map[string]any{
					"tool":  tool,
					"count": count,
				})
			}
		}
	}

	avgDur := 0.0
	if avgDuration.Valid {
		avgDur = avgDuration.Float64
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"servers": map[string]any{
			"total":   totalServers,
			"enabled": countEnabledServers(h.store),
		},
		"tools":        totalTools,
		"access_rules": totalGrantCount,
		"usage": map[string]any{
			"total_calls":       totalCalls,
			"error_count":       errorCount,
			"avg_duration_ms":   avgDur,
			"calls_by_tool_24h": callsByTool,
		},
	})
}

func (h *MCPHandler) GetAuditLog(w http.ResponseWriter, r *http.Request) {
	if h.auditLogger == nil {
		model.WriteJSONError(w, http.StatusServiceUnavailable, "audit_disabled", "MCP audit logging is not enabled")
		return
	}

	limit := 50
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	filter := mcp.AuditFilter{
		Tool:     r.URL.Query().Get("tool"),
		ServerID: r.URL.Query().Get("server_id"),
		Method:   r.URL.Query().Get("method"),
		From:     r.URL.Query().Get("from"),
		To:       r.URL.Query().Get("to"),
		Limit:    limit,
		Offset:   offset,
		Source:   r.URL.Query().Get("source"),
	}

	entries, total, err := h.auditLogger.Query(filter)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to query audit log")
		return
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"items":  entries,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}
