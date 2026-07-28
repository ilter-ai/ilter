package access

import (
	"net/http"
	"strconv"

	iltdb "github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/model"
)

type configAuditEntry struct {
	ID          int64   `json:"id"`
	EntityType  string  `json:"entity_type"`
	EntityID    string  `json:"entity_id"`
	Action      string  `json:"action"`
	OldValues   *string `json:"old_values,omitempty"`
	NewValues   *string `json:"new_values,omitempty"`
	PerformedBy *string `json:"performed_by,omitempty"`
	PerformedAt string  `json:"performed_at"`
}

// ListConfigAuditLog returns admin config-change history (API keys, users,
// groups, MCP grants) recorded in config_audit_log.
func (h *Handler) ListConfigAuditLog(w http.ResponseWriter, r *http.Request) {
	page := 1
	if pStr := r.URL.Query().Get("page"); pStr != "" {
		if p, err := strconv.Atoi(pStr); err == nil && p > 0 {
			page = p
		}
	}
	limit := 50
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
			limit = l
		}
	}
	offset := (page - 1) * limit

	entityType := r.URL.Query().Get("entity_type")
	action := r.URL.Query().Get("action")
	start := r.URL.Query().Get("start")
	end := r.URL.Query().Get("end")

	query := `SELECT id, entity_type, entity_id, action, old_values, new_values, performed_by, performed_at
		FROM config_audit_log WHERE 1=1`
	countQuery := `SELECT COUNT(*) FROM config_audit_log WHERE 1=1`
	var args []any
	var countArgs []any

	if entityType != "" {
		query += " AND entity_type = ?"
		countQuery += " AND entity_type = ?"
		args = append(args, entityType)
		countArgs = append(countArgs, entityType)
	}
	if action != "" {
		query += " AND action = ?"
		countQuery += " AND action = ?"
		args = append(args, action)
		countArgs = append(countArgs, action)
	}
	// datetime(...) on both sides: performed_at is a bare "YYYY-MM-DD HH:MM:SS"
	// string, while the frontend sends a JS Date.toISOString() value
	// ("...T....000Z"). A raw string comparison between those two formats is
	// lexicographically wrong (space < 'T') and silently drops same-day rows;
	// wrapping both in datetime() normalizes them before comparing.
	if start != "" {
		query += " AND datetime(performed_at) >= datetime(?)"
		countQuery += " AND datetime(performed_at) >= datetime(?)"
		args = append(args, start)
		countArgs = append(countArgs, start)
	}
	if end != "" {
		query += " AND datetime(performed_at) <= datetime(?)"
		countQuery += " AND datetime(performed_at) <= datetime(?)"
		args = append(args, end)
		countArgs = append(countArgs, end)
	}

	db := h.store.DB

	var total int
	if err := db.QueryRow(countQuery, countArgs...).Scan(&total); err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to count audit log")
		return
	}

	query += " ORDER BY id DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list audit log")
		return
	}
	defer rows.Close()

	items := make([]configAuditEntry, 0)
	for rows.Next() {
		var e configAuditEntry
		if err := rows.Scan(&e.ID, &e.EntityType, &e.EntityID, &e.Action, &e.OldValues, &e.NewValues, &e.PerformedBy, &e.PerformedAt); err != nil {
			continue
		}
		e.PerformedAt = iltdb.FormatSQLiteTimestamp(e.PerformedAt)
		items = append(items, e)
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"total": total,
		"page":  page,
		"limit": limit,
	})
}
