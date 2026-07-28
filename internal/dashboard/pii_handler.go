package dashboard

import (
	"database/sql"
	"encoding/csv"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/model"
)

// PIIHandler serves PII and loop detection admin endpoints.
type PIIHandler struct {
	store       *db.SQLiteStore
	configCache *config.Cache
}

func NewPIIHandler(store *db.SQLiteStore, configCache *config.Cache) *PIIHandler {
	return &PIIHandler{store: store, configCache: configCache}
}

// ── Types ──

// LoopEventItem is a detection event from the loop detector middleware.
type LoopEventItem struct {
	ID            int      `json:"id"`
	DetectedAt    string   `json:"detected_at"`
	KeyID         string   `json:"key_id"`
	Key           *KeyInfo `json:"key,omitempty"`
	ClientIP      string   `json:"client_ip"`
	PromptHash    string   `json:"prompt_hash"`
	RepeatCount   int      `json:"repeat_count"`
	WindowSeconds int      `json:"window_seconds"`
	ActionTaken   string   `json:"action_taken"`
	ResolvedAt    *string  `json:"resolved_at"`
}

type ExportItem struct {
	ID                  int     `json:"id"`
	Timestamp           string  `json:"timestamp"`
	KeyID               string  `json:"key_id"`
	APIKeyName          string  `json:"api_key_name"`
	PIIType             string  `json:"pii_type"`
	ActionTaken         string  `json:"action_taken"`
	ClientIP            string  `json:"client_ip"`
	RequestID           *int64  `json:"request_id,omitempty"`
	MaskedPromptPreview *string `json:"masked_prompt_preview,omitempty"`
	PIIValue            *string `json:"pii_value,omitempty"`
	Model               *string `json:"model,omitempty"`
	Provider            *string `json:"provider,omitempty"`
}

type EventItem struct {
	ID                  int      `json:"id"`
	Timestamp           string   `json:"timestamp"`
	KeyID               string   `json:"key_id"`
	Key                 *KeyInfo `json:"key,omitempty"`
	PIIType             string   `json:"pii_type"`
	ActionTaken         string   `json:"action_taken"`
	ClientIP            string   `json:"client_ip"`
	RequestID           *int64   `json:"request_id,omitempty"`
	MaskedPromptPreview *string  `json:"masked_prompt_preview,omitempty"`
	PIIValue            *string  `json:"pii_value,omitempty"`
	Model               *string  `json:"model,omitempty"`
	Provider            *string  `json:"provider,omitempty"`
	LatencyMs           *int     `json:"latency_ms,omitempty"`
	TotalCost           *float64 `json:"total_cost,omitempty"`
	CacheHit            *bool    `json:"cache_hit,omitempty"`
}

type Stats struct {
	TotalEvents   int          `json:"total_events"`
	BlockedCount  int          `json:"blocked_count"`
	MaskedCount   int          `json:"masked_count"`
	BlockedRate   float64      `json:"blocked_rate"`
	TypeBreakdown []TypeCount  `json:"type_breakdown"`
	TopKeys       []KeyEvent   `json:"top_keys"`
	RecentTrend   []DailyCount `json:"recent_trend"`
}

type TypeCount struct {
	PIIType string `json:"pii_type"`
	Count   int    `json:"count"`
}

type KeyEvent struct {
	KeyID      string `json:"key_id"`
	APIKeyName string `json:"api_key_name"`
	Count      int    `json:"count"`
}

type DailyCount struct {
	Date    string `json:"date"`
	Blocked int    `json:"blocked"`
	Masked  int    `json:"masked"`
}

// ── Loop handlers ──

func (h *PIIHandler) HandleLoops(w http.ResponseWriter, _ *http.Request) {
	rows, err := h.store.DB.Query(`
		SELECT le.id, le.detected_at, le.key_id,
		       COALESCE(vk.id, ''), COALESCE(vk.name, 'Unknown'),
		       CASE WHEN vk.user_id IS NOT NULL THEN 'user' WHEN vk.group_id IS NOT NULL THEN 'group' ELSE '' END,
		       COALESCE(vk.user_id, vk.group_id, 0),
		       le.client_ip, le.prompt_hash, le.repeat_count, le.window_seconds, le.action_taken, le.resolved_at
		FROM loop_events le
		LEFT JOIN api_keys vk ON le.key_id = vk.id
		ORDER BY le.detected_at DESC
		LIMIT 100
	`)
	if err != nil {
		slog.Error("Failed to query loop events", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	defer rows.Close()

	items := make([]LoopEventItem, 0)
	for rows.Next() {
		var item LoopEventItem
		var apiKeyID sql.NullString
		var clientIP sql.NullString
		var promptHash sql.NullString
		var repeatCount sql.NullInt64
		var windowSecs sql.NullInt64
		var actionTaken sql.NullString
		var resolvedAt sql.NullString
		var keyID sql.NullString
		var keyName sql.NullString
		var ownerType sql.NullString
		var ownerID sql.NullInt64

		errScan := rows.Scan(
			&item.ID, &item.DetectedAt, &apiKeyID,
			&keyID, &keyName, &ownerType, &ownerID,
			&clientIP, &promptHash,
			&repeatCount, &windowSecs, &actionTaken, &resolvedAt,
		)
		if errScan != nil {
			slog.Error("Failed to scan loop event row", "error", errScan)
			continue
		}
		if apiKeyID.Valid {
			item.KeyID = keyID.String
		}
		if keyID.String != "" {
			item.Key = &KeyInfo{
				ID:        keyID.String,
				KeyName:   keyName.String,
				OwnerType: ownerType.String,
				OwnerID:   int(ownerID.Int64),
			}
		}
		if clientIP.Valid {
			item.ClientIP = clientIP.String
		}
		if promptHash.Valid {
			item.PromptHash = promptHash.String
		}
		if repeatCount.Valid {
			item.RepeatCount = int(repeatCount.Int64)
		}
		if windowSecs.Valid {
			item.WindowSeconds = int(windowSecs.Int64)
		}
		if actionTaken.Valid {
			item.ActionTaken = actionTaken.String
		}
		if resolvedAt.Valid {
			resolved := db.FormatSQLiteTimestamp(resolvedAt.String)
			item.ResolvedAt = &resolved
		}
		item.DetectedAt = db.FormatSQLiteTimestamp(item.DetectedAt)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		slog.Error("Error iterating loop events rows", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	model.WriteJSON(w, http.StatusOK, items)
}

// HandleLoopExport exports loop events as JSON or CSV.
func (h *PIIHandler) HandleLoopExport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	rows, err := h.store.DB.Query(`
		SELECT le.id, le.detected_at, le.key_id, COALESCE(vk.name, 'Unknown'),
		       COALESCE(le.client_ip, ''), COALESCE(le.prompt_hash, ''),
		       le.repeat_count, le.window_seconds, le.action_taken
		FROM loop_events le
		LEFT JOIN api_keys vk ON le.key_id = vk.id
		ORDER BY le.detected_at DESC
		LIMIT 10000
	`)
	if err != nil {
		slog.Error("Failed to query loop events for export", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	defer rows.Close()

	type loopExportItem struct {
		ID            int    `json:"id"`
		DetectedAt    string `json:"detected_at"`
		KeyID         string `json:"key_id"`
		APIKeyName    string `json:"api_key_name"`
		ClientIP      string `json:"client_ip"`
		PromptHash    string `json:"prompt_hash"`
		RepeatCount   int    `json:"repeat_count"`
		WindowSeconds int    `json:"window_seconds"`
		ActionTaken   string `json:"action_taken"`
	}

	items := make([]loopExportItem, 0)
	for rows.Next() {
		var item loopExportItem
		var keyID, apiKeyName, clientIP, promptHash, actionTaken sql.NullString
		var repeatCount, windowSecs sql.NullInt64
		if err := rows.Scan(
			&item.ID, &item.DetectedAt, &keyID, &apiKeyName,
			&clientIP, &promptHash, &repeatCount, &windowSecs, &actionTaken,
		); err != nil {
			slog.Error("Failed to scan loop export event row", "error", err)
			continue
		}
		if keyID.Valid {
			item.KeyID = keyID.String
		}
		if apiKeyName.Valid {
			item.APIKeyName = apiKeyName.String
		}
		if clientIP.Valid {
			item.ClientIP = clientIP.String
		}
		if promptHash.Valid {
			item.PromptHash = promptHash.String
		}
		if repeatCount.Valid {
			item.RepeatCount = int(repeatCount.Int64)
		}
		if windowSecs.Valid {
			item.WindowSeconds = int(windowSecs.Int64)
		}
		if actionTaken.Valid {
			item.ActionTaken = actionTaken.String
		}
		item.DetectedAt = db.FormatSQLiteTimestamp(item.DetectedAt)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		slog.Error("Error iterating loop export rows", "error", err)
	}

	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=loop_events_export.csv")
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"id", "detected_at", "key_id", "api_key_name", "client_ip", "prompt_hash", "repeat_count", "window_seconds", "action_taken"})
		for _, item := range items {
			_ = cw.Write([]string{
				strconv.Itoa(item.ID), item.DetectedAt, item.KeyID, item.APIKeyName,
				item.ClientIP, item.PromptHash, strconv.Itoa(item.RepeatCount),
				strconv.Itoa(item.WindowSeconds), item.ActionTaken,
			})
		}
		cw.Flush()
		return
	}

	w.Header().Set("Content-Disposition", "attachment; filename=loop_events_export.json")
	model.WriteJSON(w, http.StatusOK, items)
}
