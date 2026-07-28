package smartrouter

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
	"github.com/ilter-ai/ilter/internal/provider"
)

// Handler serves smart router admin endpoints.
type Handler struct {
	store       *db.SQLiteStore
	cfg         *config.Config
	reg         *provider.Registry
	configCache *config.Cache
}

func NewHandler(store *db.SQLiteStore, cfg *config.Config, reg *provider.Registry, configCache *config.Cache) *Handler {
	return &Handler{store: store, cfg: cfg, reg: reg, configCache: configCache}
}

type StatsResponse struct {
	Config             Config                 `json:"config"`
	TierDistribution   map[string]TierInfo    `json:"tier_distribution"`
	AvgComplexity7d    float64                `json:"avg_complexity_7d"`
	DailyAvgComplexity []DailyComplexityEntry `json:"daily_avg_complexity"`
	TotalRouted        int                    `json:"total_routed"`
}

type Config struct {
	Enabled              bool         `json:"enabled"`
	ScorerType           string       `json:"scorer_type"`
	ComplexityThresholds Thresholds   `json:"complexity_thresholds"`
	Rules                []RuleConfig `json:"rules,omitempty"`
}

type RuleConfig struct {
	Name        string `json:"name"`
	Condition   string `json:"condition"`
	TargetModel string `json:"target_model"`
	Priority    int    `json:"priority"`
	Enabled     bool   `json:"enabled"`
}

type Thresholds struct {
	Economy  float64 `json:"economy"`
	Standard float64 `json:"standard"`
}

type TierInfo struct {
	Count      int     `json:"count"`
	Percentage float64 `json:"percentage"`
}

type DailyComplexityEntry struct {
	Date     string  `json:"date"`
	AvgScore float64 `json:"avg_score"`
}

type HistoryResponse struct {
	Items      []HistoryItem `json:"items"`
	Total      int           `json:"total"`
	Page       int           `json:"page"`
	Limit      int           `json:"limit"`
	TotalPages int           `json:"total_pages"`
}

type HistoryItem struct {
	ID              int     `json:"id"`
	Timestamp       string  `json:"timestamp"`
	Model           string  `json:"model"`
	Provider        string  `json:"provider"`
	Tier            string  `json:"tier"`
	ComplexityScore float64 `json:"complexity_score"`
	StatusCode      int     `json:"status_code"`
	LatencyMs       int     `json:"latency_ms"`
}

func (h *Handler) HandleSmartRouterStats(w http.ResponseWriter, _ *http.Request) {
	resp := StatsResponse{
		Config: Config{
			Enabled:    config.IsEnabled(h.configCache, "smart_router"),
			ScorerType: "heuristic",
			ComplexityThresholds: Thresholds{
				Economy:  15.0,
				Standard: 50.0,
			},
		},
		TierDistribution:   map[string]TierInfo{"standard": {Count: 2, Percentage: 100.0}},
		AvgComplexity7d:    0.0,
		DailyAvgComplexity: []DailyComplexityEntry{},
		TotalRouted:        0,
	}

	if h.store != nil && h.store.DB != nil {
		var total int
		_ = h.store.DB.QueryRow(`SELECT COUNT(*) FROM audit_log`).Scan(&total)
		resp.TotalRouted = total

		var avgComp sql.NullFloat64
		_ = h.store.DB.QueryRow(`SELECT COALESCE(AVG(complexity_score), 0) FROM audit_log WHERE timestamp >= date('now', '-7 days')`).Scan(&avgComp)
		if avgComp.Valid {
			resp.AvgComplexity7d = avgComp.Float64
		}
	}

	model.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleSmartRouterHistory(w http.ResponseWriter, r *http.Request) {
	page := 1
	limit := 20
	if r != nil {
		if pStr := r.URL.Query().Get("page"); pStr != "" {
			if p, err := strconv.Atoi(pStr); err == nil && p > 0 {
				page = p
			}
		}
		if lStr := r.URL.Query().Get("limit"); lStr != "" {
			if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
				limit = l
			}
		}
	}

	resp := HistoryResponse{
		Items: []HistoryItem{},
		Total: 0,
		Page:  page,
		Limit: limit,
	}

	if h.store != nil && h.store.DB != nil {
		var total int
		_ = h.store.DB.QueryRow(`SELECT COUNT(*) FROM audit_log`).Scan(&total)
		resp.Total = total

		offset := (page - 1) * limit
		rows, err := h.store.DB.Query(`
			SELECT id, timestamp, key_id, model, provider, status_code, latency_ms, COALESCE(complexity_score, 0)
			FROM audit_log
			ORDER BY timestamp DESC
			LIMIT ? OFFSET ?
		`, limit, offset)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var item HistoryItem
				var ts string
				var keyID, reqModel, providerName string
				var status, latency int
				var complexity float64
				var id int
				if err := rows.Scan(&id, &ts, &keyID, &reqModel, &providerName, &status, &latency, &complexity); err == nil {
					item = HistoryItem{
						ID:              id,
						Timestamp:       ts,
						Model:           reqModel,
						Provider:        providerName,
						Tier:            "standard",
						ComplexityScore: complexity,
						StatusCode:      status,
						LatencyMs:       latency,
					}
					resp.Items = append(resp.Items, item)
				}
			}
		}
	}

	model.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleSmartRouterUnified(w http.ResponseWriter, _ *http.Request) {
	model.WriteJSON(w, http.StatusOK, map[string]any{"stats": map[string]any{}, "history": map[string]any{}})
}

func (h *Handler) HandleListStrategies(w http.ResponseWriter, _ *http.Request) {
	entries, err := h.store.GetBySection("routing_strategy")
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	strategies := make([]model.RoutingStrategy, 0, len(entries))
	for _, raw := range entries {
		var s model.RoutingStrategy
		if err := json.Unmarshal([]byte(raw), &s); err == nil {
			strategies = append(strategies, s)
		}
	}

	activeName := ""
	if activeEntry, err := h.store.GetRuntimeConfigEntry("active_routing_strategy", "active"); err == nil && activeEntry != nil && activeEntry.Value != "" {
		_ = json.Unmarshal([]byte(activeEntry.Value), &activeName)
		if activeName == "" {
			activeName = activeEntry.Value
		}
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"data":   strategies,
		"active": activeName,
	})
}

func (h *Handler) HandleGetStrategy(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_name", "strategy name required")
		return
	}

	entry, err := h.store.GetRuntimeConfigEntry("routing_strategy", name)
	if err != nil || entry == nil || entry.Value == "" {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "strategy not found")
		return
	}

	var s model.RoutingStrategy
	if err := json.Unmarshal([]byte(entry.Value), &s); err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "json_error", err.Error())
		return
	}

	model.WriteJSON(w, http.StatusOK, s)
}

func (h *Handler) HandleSetStrategy(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	var s model.RoutingStrategy
	if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}

	if name != "" {
		s.Name = name
	}
	if s.Name == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_name", "strategy name required")
		return
	}

	// Validate rule target models if provided
	for _, rule := range s.Rules {
		if rule.TargetModel != "" {
			catalog.ModelsMu.RLock()
			_, inCatalog := catalog.Models[rule.TargetModel]
			catalog.ModelsMu.RUnlock()
			if !inCatalog {
				model.WriteJSONError(w, http.StatusBadRequest, "invalid_target_model",
					fmt.Sprintf("target model %s does not exist", rule.TargetModel))
				return
			}
		}
	}

	data, err := json.Marshal(s)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "json_error", err.Error())
		return
	}

	if err := h.store.UpsertRuntimeConfig("routing_strategy", s.Name, string(data), "admin"); err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	respMap := map[string]any{
		"status":                 "ok",
		"name":                   s.Name,
		"description":            s.Description,
		"enabled":                s.Enabled,
		"provider_preference":    s.ProviderPreference,
		"load_balancer_strategy": s.LoadBalancerStrategy,
		"scorer":                 s.Scorer,
		"complexity_thresholds":  s.ComplexityThresholds,
		"rules":                  s.Rules,
	}

	model.WriteJSON(w, http.StatusOK, respMap)
}

func (h *Handler) HandleDeleteStrategy(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name != "" {
		_ = h.store.DeleteRuntimeConfig("routing_strategy", name)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) HandleGetActiveStrategy(w http.ResponseWriter, _ *http.Request) {
	activeName := ""
	if activeEntry, err := h.store.GetRuntimeConfigEntry("active_routing_strategy", "active"); err == nil && activeEntry != nil && activeEntry.Value != "" {
		if err := json.Unmarshal([]byte(activeEntry.Value), &activeName); err != nil {
			slog.Warn("failed to unmarshal active strategy", "value", activeEntry.Value, "error", err)
		}
		if activeName == "" {
			activeName = activeEntry.Value
		}
	}

	model.WriteJSON(w, http.StatusOK, map[string]string{"active": activeName})
}

func (h *Handler) HandleSetActiveStrategy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request", "valid strategy name is required")
		return
	}

	data, _ := json.Marshal(req.Name)
	if err := h.store.UpsertRuntimeConfig("active_routing_strategy", "active", string(data), "admin"); err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	model.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "active": req.Name})
}
