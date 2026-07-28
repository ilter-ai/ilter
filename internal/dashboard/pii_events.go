package dashboard

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/model"
)

// ---------------------------------------------------------------------------
// PII Event handlers
// ---------------------------------------------------------------------------

// HandlePIIEvents returns paginated PII events with filtering.
func (h *PIIHandler) HandlePIIEvents(w http.ResponseWriter, r *http.Request) {
	piiTypeFilter := r.URL.Query().Get("pii_type")
	actionFilter := r.URL.Query().Get("action")
	keyIDFilter := r.URL.Query().Get("key_id")
	dateFrom := r.URL.Query().Get("date_from")
	dateTo := r.URL.Query().Get("date_to")
	page := 1
	limit := 50
	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}
	offset := (page - 1) * limit

	countQuery := `SELECT COUNT(*) FROM pii_events p WHERE p.key_id IS NOT NULL AND p.key_id != ''`
	dataQuery := `
		SELECT p.id, p.timestamp, p.key_id,
		       COALESCE(vk.id, ''), COALESCE(vk.name, 'Unknown'),
		       CASE WHEN vk.user_id IS NOT NULL THEN 'user' WHEN vk.group_id IS NOT NULL THEN 'group' ELSE '' END,
		       COALESCE(vk.user_id, vk.group_id, 0),
		       p.pii_type, p.action_taken, COALESCE(p.client_ip, '') as client_ip,
		       p.request_id, p.masked_prompt_preview, p.pii_value,
		       a.model, a.provider, a.latency_ms, a.total_cost, a.cache_hit
		FROM pii_events p
		LEFT JOIN audit_log a ON p.request_id = a.id
		LEFT JOIN api_keys vk ON p.key_id = vk.id
		WHERE p.key_id IS NOT NULL AND p.key_id != ''
	`
	args := make([]any, 0)
	countArgs := make([]any, 0)

	if piiTypeFilter != "" {
		dataQuery += " AND p.pii_type = ?"
		countQuery += " AND p.pii_type = ?"
		args = append(args, piiTypeFilter)
		countArgs = append(countArgs, piiTypeFilter)
	}
	if actionFilter != "" {
		dataQuery += " AND p.action_taken = ?"
		countQuery += " AND p.action_taken = ?"
		args = append(args, actionFilter)
		countArgs = append(countArgs, actionFilter)
	}
	if keyIDFilter != "" {
		dataQuery += " AND p.key_id = ?"
		countQuery += " AND p.key_id = ?"
		args = append(args, keyIDFilter)
		countArgs = append(countArgs, keyIDFilter)
	}
	if dateFrom != "" {
		dataQuery += " AND p.timestamp >= ?"
		countQuery += " AND p.timestamp >= ?"
		args = append(args, dateFrom)
		countArgs = append(countArgs, dateFrom)
	}
	if dateTo != "" {
		dataQuery += " AND p.timestamp <= ?"
		countQuery += " AND p.timestamp <= ?"
		args = append(args, dateTo+" 23:59:59")
		countArgs = append(countArgs, dateTo+" 23:59:59")
	}

	var total int
	err := h.store.DB.QueryRow(countQuery, countArgs...).Scan(&total)
	if err != nil {
		slog.Error("Failed to count PII events", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	dataQuery += " ORDER BY p.timestamp DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := h.store.DB.Query(dataQuery, args...)
	if err != nil {
		slog.Error("Failed to query pii events", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	defer rows.Close()

	items := make([]EventItem, 0)
	for rows.Next() {
		var item EventItem
		var apiKeyID sql.NullString
		var keyID sql.NullString
		var keyName sql.NullString
		var ownerType sql.NullString
		var ownerID sql.NullInt64
		var piiType sql.NullString
		var actionTaken sql.NullString
		var clientIP sql.NullString
		var requestID sql.NullInt64
		var maskedPromptPreview sql.NullString
		var piiValue sql.NullString
		var modelVal sql.NullString
		var providerVal sql.NullString
		var latencyMs sql.NullInt64
		var totalCost sql.NullFloat64
		var cacheHit sql.NullBool

		errScan := rows.Scan(
			&item.ID, &item.Timestamp, &apiKeyID,
			&keyID, &keyName, &ownerType, &ownerID,
			&piiType, &actionTaken, &clientIP,
			&requestID, &maskedPromptPreview, &piiValue, &modelVal, &providerVal,
			&latencyMs, &totalCost, &cacheHit,
		)
		if errScan != nil {
			slog.Error("Failed to scan PII event row", "error", errScan)
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
		if piiType.Valid {
			item.PIIType = piiType.String
		}
		if actionTaken.Valid {
			item.ActionTaken = actionTaken.String
		}
		if clientIP.Valid {
			item.ClientIP = clientIP.String
		}
		if requestID.Valid {
			item.RequestID = &requestID.Int64
		}
		if maskedPromptPreview.Valid {
			item.MaskedPromptPreview = &maskedPromptPreview.String
		}
		if piiValue.Valid {
			item.PIIValue = &piiValue.String
		}
		if modelVal.Valid {
			item.Model = &modelVal.String
		}
		if providerVal.Valid {
			item.Provider = &providerVal.String
		}
		if latencyMs.Valid {
			lat := int(latencyMs.Int64)
			item.LatencyMs = &lat
		}
		if totalCost.Valid {
			item.TotalCost = &totalCost.Float64
		}
		if cacheHit.Valid {
			item.CacheHit = &cacheHit.Bool
		}
		item.Timestamp = db.FormatSQLiteTimestamp(item.Timestamp)
		items = append(items, item)
	}

	totalPages := max((total+limit-1)/limit, 1)

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"items":       items,
		"total":       total,
		"page":        page,
		"limit":       limit,
		"total_pages": totalPages,
	})
}

// HandlePIIExport exports PII events as JSON or CSV.
func (h *PIIHandler) HandlePIIExport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	rows, err := h.store.DB.Query(`
		SELECT p.id, p.timestamp, p.key_id, COALESCE(vk.name, 'Unknown') as api_key_name,
		       p.pii_type, p.action_taken, COALESCE(p.client_ip, '') as client_ip,
		       p.request_id, p.masked_prompt_preview, p.pii_value,
		       a.model, a.provider
		FROM pii_events p
		LEFT JOIN audit_log a ON p.request_id = a.id
		LEFT JOIN api_keys vk ON p.key_id = vk.id
		WHERE p.key_id IS NOT NULL AND p.key_id != ''
		ORDER BY p.timestamp DESC
		LIMIT 5000
	`)
	if err != nil {
		slog.Error("Failed to query pii events for export", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	defer rows.Close()

	items := make([]ExportItem, 0)
	for rows.Next() {
		var item ExportItem
		var apiKeyID sql.NullString
		var apiKeyName sql.NullString
		var piiType sql.NullString
		var actionTaken sql.NullString
		var clientIP sql.NullString
		var requestID sql.NullInt64
		var maskedPromptPreview sql.NullString
		var piiValue sql.NullString
		var modelVal sql.NullString
		var providerVal sql.NullString

		errScan := rows.Scan(
			&item.ID, &item.Timestamp, &apiKeyID, &apiKeyName,
			&piiType, &actionTaken, &clientIP,
			&requestID, &maskedPromptPreview, &piiValue, &modelVal, &providerVal,
		)
		if errScan != nil {
			slog.Error("Failed to scan PII export event row", "error", errScan)
			continue
		}
		if apiKeyID.Valid {
			item.KeyID = apiKeyID.String
		}
		if apiKeyName.Valid {
			item.APIKeyName = apiKeyName.String
		}
		if piiType.Valid {
			item.PIIType = piiType.String
		}
		if actionTaken.Valid {
			item.ActionTaken = actionTaken.String
		}
		if clientIP.Valid {
			item.ClientIP = clientIP.String
		}
		if requestID.Valid {
			item.RequestID = &requestID.Int64
		}
		if maskedPromptPreview.Valid {
			item.MaskedPromptPreview = &maskedPromptPreview.String
		}
		if piiValue.Valid {
			item.PIIValue = &piiValue.String
		}
		if modelVal.Valid {
			item.Model = &modelVal.String
		}
		if providerVal.Valid {
			item.Provider = &providerVal.String
		}
		item.Timestamp = db.FormatSQLiteTimestamp(item.Timestamp)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		slog.Error("Error iterating PII export rows", "error", err)
	}

	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=pii_events_export.csv")
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"id", "timestamp", "key_id", "api_key_name", "pii_type", "action_taken", "client_ip", "model", "provider"})
		for _, item := range items {
			modelName, provider := "", ""
			if item.Model != nil {
				modelName = *item.Model
			}
			if item.Provider != nil {
				provider = *item.Provider
			}
			_ = cw.Write([]string{
				strconv.Itoa(item.ID), item.Timestamp, item.KeyID, item.APIKeyName,
				item.PIIType, item.ActionTaken, item.ClientIP, modelName, provider,
			})
		}
		cw.Flush()
		return
	}

	w.Header().Set("Content-Disposition", "attachment; filename=pii_events_export.json")
	model.WriteJSON(w, http.StatusOK, items)
}

// HandleStats returns PII event statistics.
func (h *PIIHandler) HandleStats(w http.ResponseWriter, _ *http.Request) {
	stats := Stats{}

	err := h.store.DB.QueryRow("SELECT COUNT(*) FROM pii_events WHERE key_id IS NOT NULL AND key_id != ''").Scan(&stats.TotalEvents)
	if err != nil {
		slog.Error("Failed to query PII stats total", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	if err = h.store.DB.QueryRow("SELECT COUNT(*) FROM pii_events WHERE action_taken = 'blocked' AND key_id IS NOT NULL AND key_id != ''").Scan(&stats.BlockedCount); err != nil {
		slog.Warn("Failed to query blocked PII count", "error", err)
	}
	if err = h.store.DB.QueryRow("SELECT COUNT(*) FROM pii_events WHERE action_taken = 'masked' AND key_id IS NOT NULL AND key_id != ''").Scan(&stats.MaskedCount); err != nil {
		slog.Warn("Failed to query masked PII count", "error", err)
	}

	if stats.TotalEvents > 0 {
		stats.BlockedRate = float64(stats.BlockedCount) / float64(stats.TotalEvents) * 100
	}

	typeRows, err := h.store.DB.Query(`
		SELECT pii_type, COUNT(*) as cnt
		FROM pii_events
		WHERE key_id IS NOT NULL AND key_id != ''
		GROUP BY pii_type
		ORDER BY cnt DESC
		LIMIT 10
	`)
	if err == nil {
		defer typeRows.Close()
		for typeRows.Next() {
			var tc TypeCount
			if err = typeRows.Scan(&tc.PIIType, &tc.Count); err != nil {
				slog.Error("Failed to scan PII type breakdown row", "error", err)
				continue
			}
			stats.TypeBreakdown = append(stats.TypeBreakdown, tc)
		}
	}

	keyRows, err := h.store.DB.Query(`
		SELECT p.key_id, COALESCE(vk.name, 'Unknown') as api_key_name, COUNT(*) as cnt
		FROM pii_events p
		LEFT JOIN api_keys vk ON p.key_id = vk.id
		WHERE p.key_id IS NOT NULL AND p.key_id != ''
		GROUP BY p.key_id
		ORDER BY cnt DESC
		LIMIT 5
	`)
	if err == nil {
		defer keyRows.Close()
		for keyRows.Next() {
			var ke KeyEvent
			if err = keyRows.Scan(&ke.KeyID, &ke.APIKeyName, &ke.Count); err != nil {
				slog.Error("Failed to scan PII top keys row", "error", err)
				continue
			}
			stats.TopKeys = append(stats.TopKeys, ke)
		}
	}

	trendRows, err := h.store.DB.Query(`
		SELECT DATE(timestamp) as date,
		       SUM(CASE WHEN action_taken = 'blocked' THEN 1 ELSE 0 END) as blocked,
		       SUM(CASE WHEN action_taken = 'masked' THEN 1 ELSE 0 END) as masked
		FROM pii_events
		WHERE timestamp >= datetime('now', '-7 days') AND key_id IS NOT NULL AND key_id != ''
		GROUP BY DATE(timestamp)
		ORDER BY date ASC
	`)
	if err == nil {
		defer trendRows.Close()
		for trendRows.Next() {
			var tc DailyCount
			if err := trendRows.Scan(&tc.Date, &tc.Blocked, &tc.Masked); err != nil {
				slog.Error("Failed to scan PII daily trend row", "error", err)
				continue
			}
			stats.RecentTrend = append(stats.RecentTrend, tc)
		}
	}

	model.WriteJSON(w, http.StatusOK, stats)
}

type piiConfigRequest struct {
	Enabled bool `json:"enabled"`
}

// HandlePIIConfig handles GET/POST for PII feature flag configuration.
func (h *PIIHandler) HandlePIIConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		enabled := config.IsEnabled(h.configCache, "pii")
		model.WriteJSON(w, http.StatusOK, map[string]any{"enabled": enabled})
		return
	}

	defer r.Body.Close()
	var req piiConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}

	value := "false"
	if req.Enabled {
		value = "true"
	}
	_, err := h.store.DB.Exec(
		`INSERT INTO runtime_config (section, key, value, updated_at, version)
		 VALUES ('pii', 'enabled', ?, datetime('now'), 1)
		 ON CONFLICT(section, key) DO UPDATE SET value = excluded.value, version = version + 1, updated_at = datetime('now')`,
		value,
	)
	if err != nil {
		slog.Error("Failed to persist PII feature flag", "enabled", req.Enabled, "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save PII feature flag")
		return
	}

	if h.configCache != nil {
		stores := &config.RuntimeStores{RuntimeConfig: h.store}
		if err := h.configCache.Refresh(context.Background(), stores); err != nil {
			slog.Warn("config cache refresh after PII toggle failed", "error", err)
		}
	}

	slog.Info("PII masking toggled via feature flag", "enabled", req.Enabled)
	model.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "enabled": req.Enabled})
}
