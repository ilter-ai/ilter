package dashboard

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/features/guardrails"
	iltermiddleware "github.com/ilter-ai/ilter/internal/middleware"
	"github.com/ilter-ai/ilter/internal/model"
)

// GuardrailsHandler serves admin endpoints for guardrails configuration and testing.
type GuardrailsHandler struct {
	middleware  *iltermiddleware.GuardrailsMiddleware
	checker     *guardrails.Checker
	store       *db.SQLiteStore
	cfg         *config.Config
	configCache *config.Cache
}

// NewGuardrailsHandler creates a GuardrailsHandler from the middleware instance.
func NewGuardrailsHandler(mw *iltermiddleware.GuardrailsMiddleware, store *db.SQLiteStore, cfg *config.Config, configCache *config.Cache) *GuardrailsHandler {
	h := &GuardrailsHandler{
		middleware:  mw,
		store:       store,
		cfg:         cfg,
		configCache: configCache,
	}
	if mw != nil {
		h.checker = mw.Checker()
	}
	return h
}

// TestGuardrailRequest is the JSON body for the test endpoint.
type TestGuardrailRequest struct {
	Content string `json:"content"`
}

// TestGuardrail executes guardrails checks against a single message and returns the result.
func (h *GuardrailsHandler) TestGuardrail(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	if h.checker == nil {
		model.WriteJSONError(w, http.StatusServiceUnavailable, "guardrails_disabled", "Guardrails middleware is not initialized")
		return
	}

	var req TestGuardrailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}

	if req.Content == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "content is required")
		return
	}

	messages := []guardrails.Message{
		{Index: 0, Role: "user", Content: req.Content},
	}

	result := h.checker.Check(r.Context(), messages)

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"blocked":  result.Blocked,
		"warned":   result.Warned,
		"rule_id":  result.RuleID,
		"rule_set": result.RuleSet,
		"severity": string(result.Severity),
		"matched":  result.MatchedText,
		"action":   string(result.Action),
	})
}

// GuardrailStats returns basic guardrails status.
func (h *GuardrailsHandler) GuardrailStats(w http.ResponseWriter, _ *http.Request) {
	enabled := h.middleware != nil
	ruleCount := 0
	var ruleSets []string

	if h.checker != nil {
		ruleCount = h.checker.RuleCount()
		ruleSets = h.checker.RuleSets()
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"enabled":    enabled,
		"rule_count": ruleCount,
		"rule_sets":  ruleSets,
	})
}

func parseIntParam(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return v
}

// Constants for guardrail summary period options.
const (
	periodWeekly  = "7d"
	periodMonthly = "30d"
	periodDaily   = "24h"
)

// sincePeriodDays returns the number of days for a given period string.
func sincePeriodDays(period string) string {
	switch period {
	case "24h":
		return "1"
	case "7d":
		return "7"
	case "30d":
		return "30"
	default:
		return "7"
	}
}

type GuardrailEventItem struct {
	ID            int      `json:"id"`
	Timestamp     string   `json:"timestamp"`
	KeyID         string   `json:"key_id"`
	Key           *KeyInfo `json:"key,omitempty"`
	GuardrailType string   `json:"guardrail_type"`
	ActionTaken   string   `json:"action_taken"`
	Model         string   `json:"model,omitempty"`
	Provider      string   `json:"provider,omitempty"`
	Details       string   `json:"details,omitempty"`
}

type GuardrailSummaryItem struct {
	GuardrailType string  `json:"guardrail_type"`
	Count         int     `json:"count"`
	Pct           float64 `json:"pct"`
}

type GuardrailSummaryResponse struct {
	TotalEvents int                    `json:"total_events"`
	Period      string                 `json:"period"`
	ByType      []GuardrailSummaryItem `json:"by_type"`
	RecentTrend []TrendItem            `json:"recent_trend,omitempty"`
}

type TrendItem struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

type GuardrailRule struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Mode       string `json:"mode"`
	Severity   string `json:"severity"`
	Enabled    bool   `json:"enabled"`
	TargetType string `json:"target_type,omitempty"`
	TargetID   *int   `json:"target_id,omitempty"`
}

type ListResponse struct {
	Rules []GuardrailRule `json:"rules"`
	Total int             `json:"total"`
}

// Filters: type, action, page, limit.
func (h *GuardrailsHandler) HandleGuardrailViolations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page := parseIntParam(q.Get("page"), 1)
	limit := min(parseIntParam(q.Get("limit"), 50), 500)
	offset := (page - 1) * limit

	typeFilter := q.Get("type")
	actionFilter := q.Get("action")

	var conditions []string
	var args []any

	if typeFilter != "" {
		conditions = append(conditions, "guardrail_type = ?")
		args = append(args, typeFilter)
	}
	if actionFilter != "" {
		conditions = append(conditions, "action_taken = ?")
		args = append(args, actionFilter)
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM guardrail_events %s", whereClause)
	var total int
	if err := h.store.DB.QueryRow(countQuery, args...).Scan(&total); err != nil {
		slog.Error("Failed to count guardrail violations", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	rows, err := h.store.DB.Query(fmt.Sprintf(
		`SELECT ge.id, ge.timestamp, ge.key_id,
		       COALESCE(vk.id, ''), COALESCE(vk.name, ''),
		       CASE WHEN vk.user_id IS NOT NULL THEN 'user' WHEN vk.group_id IS NOT NULL THEN 'group' ELSE '' END,
		       COALESCE(vk.user_id, vk.group_id, 0),
		       ge.guardrail_type, ge.action_taken,
		       COALESCE(ge.model, ''), COALESCE(ge.provider, ''), COALESCE(ge.details, '')
		FROM guardrail_events ge
		LEFT JOIN api_keys vk ON ge.key_id = vk.id %s
		ORDER BY ge.timestamp DESC LIMIT ? OFFSET ?
	`, whereClause,
	), append(args, limit, offset)...)
	if err != nil {
		slog.Error("Failed to query guardrail violations", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	defer rows.Close()

	items := make([]GuardrailEventItem, 0, limit)
	for rows.Next() {
		var item GuardrailEventItem
		var keyIDStr sql.NullString
		var vkID sql.NullString
		var vkName sql.NullString
		var ownerType sql.NullString
		var ownerID sql.NullInt64
		var model, provider, details sql.NullString

		if err := rows.Scan(&item.ID, &item.Timestamp, &keyIDStr,
			&vkID, &vkName, &ownerType, &ownerID,
			&item.GuardrailType, &item.ActionTaken,
			&model, &provider, &details); err != nil {
			slog.Error("Failed to scan guardrail row", "error", err)
			continue
		}
		if keyIDStr.Valid {
			item.KeyID = keyIDStr.String
		}
		if vkID.Valid && vkID.String != "" {
			item.Key = &KeyInfo{
				ID:        vkID.String,
				KeyName:   vkName.String,
				OwnerType: ownerType.String,
				OwnerID:   int(ownerID.Int64),
			}
		}
		if model.Valid {
			item.Model = model.String
		}
		if provider.Valid {
			item.Provider = provider.String
		}
		if details.Valid {
			item.Details = details.String
		}
		item.Timestamp = db.FormatSQLiteTimestamp(item.Timestamp)
		items = append(items, item)
	}

	resp := Page[GuardrailEventItem]{Items: items, Total: total, Page: page, Limit: limit}
	model.WriteJSON(w, http.StatusOK, resp)
}

// Period options: 24h, 7d, 30d (default: 7d).
func (h *GuardrailsHandler) HandleGuardrailSummary(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	if period == "" {
		period = periodWeekly
	}
	sinceClause := "timestamp >= date('now', '-" + sincePeriodDays(period) + " days')"

	var total int
	if err := h.store.DB.QueryRow(
		fmt.Sprintf(
			"SELECT COUNT(*) FROM guardrail_events WHERE %s", sinceClause,
		),
	).Scan(&total); err != nil {
		slog.Warn("Failed to count guardrail events", "error", err)
	}

	typeRows, err := h.store.DB.Query(fmt.Sprintf(
		`SELECT guardrail_type, COUNT(*) as cnt
		FROM guardrail_events WHERE %s
		GROUP BY guardrail_type ORDER BY cnt DESC
	`, sinceClause,
	))
	if err != nil {
		slog.Error("Failed to query guardrail type breakdown", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	defer typeRows.Close()

	byType := make([]GuardrailSummaryItem, 0)
	for typeRows.Next() {
		var item GuardrailSummaryItem
		if err = typeRows.Scan(&item.GuardrailType, &item.Count); err != nil {
			continue
		}
		if total > 0 {
			item.Pct = float64(item.Count) / float64(total) * 100
		}
		byType = append(byType, item)
	}

	trendRows, err := h.store.DB.Query(
		`SELECT DATE(timestamp) as day, COUNT(*) as cnt
		FROM guardrail_events
		WHERE timestamp >= date('now', '-14 days')
		GROUP BY DATE(timestamp)
		ORDER BY day ASC
	`,
	)
	trend := make([]TrendItem, 0)
	if err == nil {
		defer trendRows.Close()
		for trendRows.Next() {
			var item TrendItem
			if err := trendRows.Scan(&item.Date, &item.Count); err != nil {
				continue
			}
			trend = append(trend, item)
		}
	}

	resp := GuardrailSummaryResponse{
		TotalEvents: total,
		Period:      period,
		ByType:      byType,
		RecentTrend: trend,
	}
	model.WriteJSON(w, http.StatusOK, resp)
}

// Query param: format=csv|json (default: json).
func (h *GuardrailsHandler) HandleGuardrailExport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "json"
	}

	rows, err := h.store.DB.Query(
		`SELECT ge.id, ge.timestamp, ge.key_id,
		       COALESCE(vk.id, ''), COALESCE(vk.name, ''),
		       CASE WHEN vk.user_id IS NOT NULL THEN 'user' WHEN vk.group_id IS NOT NULL THEN 'group' ELSE '' END,
		       COALESCE(vk.user_id, vk.group_id, 0),
		       ge.guardrail_type, ge.action_taken,
		       COALESCE(ge.model, ''), COALESCE(ge.provider, ''), COALESCE(ge.details, '')
		FROM guardrail_events ge
		LEFT JOIN api_keys vk ON ge.key_id = vk.id
		ORDER BY ge.timestamp DESC LIMIT 10000
	`,
	)
	if err != nil {
		slog.Error("Failed to query guardrail events for export", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	defer rows.Close()

	items := make([]GuardrailEventItem, 0, 1000)
	for rows.Next() {
		var item GuardrailEventItem
		var vkID sql.NullString
		var vkName sql.NullString
		var ownerType sql.NullString
		var ownerID sql.NullInt64
		if err := rows.Scan(&item.ID, &item.Timestamp, &item.KeyID,
			&vkID, &vkName, &ownerType, &ownerID,
			&item.GuardrailType, &item.ActionTaken,
			&item.Model, &item.Provider, &item.Details); err != nil {
			continue
		}
		if vkID.Valid && vkID.String != "" {
			item.Key = &KeyInfo{
				ID:        vkID.String,
				KeyName:   vkName.String,
				OwnerType: ownerType.String,
				OwnerID:   int(ownerID.Int64),
			}
		}
		items = append(items, item)
	}

	if format == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=guardrail_events.csv")

		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"id", "timestamp", "key_id", "guardrail_type", "action_taken", "model", "provider", "details"})
		for _, item := range items {
			_ = cw.Write([]string{
				strconv.Itoa(item.ID), item.Timestamp, item.KeyID,
				item.GuardrailType, item.ActionTaken,
				item.Model, item.Provider, item.Details,
			})
		}
		cw.Flush()
		return
	}

	w.Header().Set("Content-Disposition", "attachment; filename=guardrail_events.json")
	model.WriteJSON(w, http.StatusOK, items)
}

func (h *GuardrailsHandler) HandleAdminGuardrails(w http.ResponseWriter, _ *http.Request) {
	rules := make([]GuardrailRule, 0)

	dbRows, err := h.store.ListGuardrailRules()
	if err == nil {
		for _, row := range dbRows {
			rules = append(rules, GuardrailRule{
				ID:         row.ID,
				Name:       row.Name,
				Type:       row.Type,
				Mode:       row.Mode,
				Severity:   row.Severity,
				Enabled:    row.Enabled,
				TargetType: row.TargetType,
				TargetID:   row.TargetID,
			})
		}
	}

	resp := ListResponse{Rules: rules, Total: len(rules)}
	model.WriteJSON(w, http.StatusOK, resp)
}

func (h *GuardrailsHandler) HandleToggleGuardrail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "rule ID is required")
		return
	}

	type ToggleRequest struct {
		Enabled bool `json:"enabled"`
	}

	var req ToggleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}

	found, err := h.store.ToggleGuardrailRule(id, req.Enabled)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to toggle rule")
		return
	}
	if !found {
		model.WriteJSONError(w, http.StatusNotFound, "not_found_error", "guardrail rule not found")
		return
	}

	if h.middleware != nil {
		h.middleware.LoadDBRules(h.store)
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"rule_id": id,
		"enabled": req.Enabled,
		"message": "Guardrail rule updated successfully",
	})
}

type GuardrailRuleItem struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Patterns    []string `json:"patterns"`
	Mode        string   `json:"mode"`
	Severity    string   `json:"severity"`
	Enabled     bool     `json:"enabled"`
	TargetType  string   `json:"target_type,omitempty"`
	TargetID    *int     `json:"target_id,omitempty"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

func (h *GuardrailsHandler) HandleCreateGuardrailRule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Type        string   `json:"type"`
		Description string   `json:"description"`
		Patterns    []string `json:"patterns"`
		Mode        string   `json:"mode"`
		Severity    string   `json:"severity"`
		TargetType  string   `json:"target_type"`
		TargetID    *int     `json:"target_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}
	if req.ID == "" || req.Name == "" || len(req.Patterns) == 0 {
		model.WriteJSONError(w, http.StatusBadRequest, "validation_error", "id, name, and patterns are required")
		return
	}
	if req.Type == "" {
		req.Type = "custom"
	}
	if req.Mode == "" {
		req.Mode = "block"
	}
	if req.Severity == "" {
		req.Severity = "medium"
	}
	if req.TargetType == "" {
		req.TargetType = "global"
	}

	patternsJSON, err := json.Marshal(req.Patterns)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to encode patterns")
		return
	}

	err = h.store.CreateGuardrailRule(db.CreateGuardrailRuleParams{
		ID:          req.ID,
		Name:        req.Name,
		Description: req.Description,
		Patterns:    string(patternsJSON),
		Mode:        req.Mode,
		Severity:    req.Severity,
		TargetType:  req.TargetType,
		TargetID:    req.TargetID,
		Type:        req.Type,
	})
	if err != nil {
		model.WriteJSONError(w, http.StatusConflict, "duplicate_error", "Rule ID already exists: "+err.Error())
		return
	}

	if h.middleware != nil {
		h.middleware.LoadDBRules(h.store)
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"rule_id": req.ID,
		"message": "Guardrail rule created",
	})
}

func (h *GuardrailsHandler) HandleUpdateGuardrailRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Rule ID is required")
		return
	}

	var req struct {
		Name        string   `json:"name"`
		Type        string   `json:"type"`
		Description string   `json:"description"`
		Patterns    []string `json:"patterns"`
		Mode        string   `json:"mode"`
		Severity    string   `json:"severity"`
		Enabled     *bool    `json:"enabled"`
		TargetType  *string  `json:"target_type"`
		TargetID    *int     `json:"target_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}

	patternsJSON, err := json.Marshal(req.Patterns)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to encode patterns")
		return
	}

	found, err := h.store.UpdateGuardrailRule(db.UpdateGuardrailRuleParams{
		ID:          id,
		Name:        req.Name,
		Type:        req.Type,
		Description: req.Description,
		Patterns:    string(patternsJSON),
		Mode:        req.Mode,
		Severity:    req.Severity,
		Enabled:     req.Enabled,
		TargetType:  req.TargetType,
		TargetID:    req.TargetID,
	})
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to update rule: "+err.Error())
		return
	}
	if !found {
		model.WriteJSONError(w, http.StatusNotFound, "not_found_error", "Guardrail rule not found")
		return
	}

	if h.middleware != nil {
		h.middleware.LoadDBRules(h.store)
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"rule_id": id,
		"message": "Guardrail rule updated",
	})
}

func (h *GuardrailsHandler) HandleDeleteGuardrailRule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Rule ID is required")
		return
	}

	found, err := h.store.DeleteGuardrailRule(id)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to delete rule")
		return
	}
	if !found {
		model.WriteJSONError(w, http.StatusNotFound, "not_found_error", "Guardrail rule not found")
		return
	}

	if h.middleware != nil {
		h.middleware.LoadDBRules(h.store)
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"rule_id": id,
		"message": "Guardrail rule deleted",
	})
}
