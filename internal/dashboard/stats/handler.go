package stats

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/ilter-ai/ilter/internal/model"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/db/audit"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/provider"
)

// Handler serves stats-related admin endpoints.
type Handler struct {
	store         *db.SQLiteStore
	cfg           *config.Config
	configCache   *config.Cache
	reg           *provider.Registry
	lb            *smartrouter.LoadBalancer
	configAuditor *audit.SQLiteConfigAuditor
}

func NewStatsHandler(store *db.SQLiteStore, cfg *config.Config, configCache *config.Cache, reg *provider.Registry, lb *smartrouter.LoadBalancer, configAuditor *audit.SQLiteConfigAuditor) *Handler {
	return &Handler{store: store, cfg: cfg, configCache: configCache, reg: reg, lb: lb, configAuditor: configAuditor}
}

func nullInt64(ns sql.NullInt64) int {
	if ns.Valid {
		return int(ns.Int64)
	}
	return 0
}

func nullFloat64(ns sql.NullFloat64) float64 {
	if ns.Valid {
		return ns.Float64
	}
	return 0
}

type DailyStatItem struct {
	Date      string  `json:"date"`
	Requests  int     `json:"requests"`
	TokensIn  int     `json:"tokens_in"`
	TokensOut int     `json:"tokens_out"`
	Cost      float64 `json:"cost"`
}

type ProviderBreakdownItem struct {
	Provider string  `json:"provider"`
	Requests int     `json:"requests"`
	Tokens   int     `json:"tokens"`
	Cost     float64 `json:"cost"`
	Pct      float64 `json:"pct"`
}

type SystemHealthItem struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Value  string `json:"value"`
	Metric string `json:"metric"`
}

type AnalyticsOverviewResponse struct {
	TotalRequests int     `json:"total_requests"`
	ErrorRate     float64 `json:"error_rate"`
	Cost          float64 `json:"cost"`
	CacheHitRate  float64 `json:"cache_hit_rate"`
}

func (h *Handler) HandleAnalyticsOverview(w http.ResponseWriter, _ *http.Request) {
	var totalRequests int
	var errorCount float64
	var cost float64
	var cacheHits float64

	err := h.store.DB.QueryRow(`
		SELECT COUNT(*),
		       COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(total_cost), 0),
		       COALESCE(SUM(CASE WHEN cache_hit = 1 THEN 1 ELSE 0 END), 0)
		FROM audit_log
	`).Scan(&totalRequests, &errorCount, &cost, &cacheHits)

	if err != nil && err != sql.ErrNoRows {
		slog.Error("Failed to query analytics overview", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	var errorRate, cacheHitRate float64
	if totalRequests > 0 {
		errorRate = errorCount / float64(totalRequests)
		cacheHitRate = cacheHits / float64(totalRequests)
	}

	resp := AnalyticsOverviewResponse{
		TotalRequests: totalRequests,
		ErrorRate:     errorRate,
		Cost:          cost,
		CacheHitRate:  cacheHitRate,
	}

	model.WriteJSON(w, http.StatusOK, resp)
}

type Response struct {
	TotalRequests      int                     `json:"total_requests"`
	TotalCost          float64                 `json:"total_cost"`
	EstimatedSavings   float64                 `json:"estimated_savings"`
	SuccessCount       int                     `json:"success_count"`
	ErrorCount         int                     `json:"error_count"`
	CacheHits          int                     `json:"cache_hits"`
	AvgLatencyMs       float64                 `json:"avg_latency_ms"`
	TotalTokens        int                     `json:"total_tokens"`
	ActiveKeysUsed     int                     `json:"active_keys_used"`
	ActiveKeys         int                     `json:"active_keys"`
	TotalKeys          int                     `json:"total_keys"`
	BlockedRequests24h int                     `json:"blocked_requests_24h"`
	DailyStats         []DailyStatItem         `json:"daily_stats"`
	ProviderBreakdown  []ProviderBreakdownItem `json:"provider_breakdown"`
	SystemHealth       []SystemHealthItem      `json:"system_health"`
}

// statsWindow bounds the headline KPI query to the trailing 24h so labels
// like "(24h)" reflect a rolling window instead of the entire audit_log history.
const statsWindow = "-1 day"

func (h *Handler) HandleStats(w http.ResponseWriter, _ *http.Request) {
	var totalRequests int
	var totalCost float64
	var successCount sql.NullInt64
	var errorCount sql.NullInt64
	var cacheHits sql.NullInt64
	var avgLatency sql.NullFloat64

	err := h.store.DB.QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(SUM(total_cost), 0.0),
			SUM(CASE WHEN status_code = 200 THEN 1 ELSE 0 END),
			SUM(CASE WHEN status_code != 200 THEN 1 ELSE 0 END),
			SUM(CASE WHEN cache_hit = 1 THEN 1 ELSE 0 END),
			COALESCE(AVG(latency_ms), 0.0)
		FROM audit_log
		WHERE timestamp >= datetime('now', ?)
	`, statsWindow).Scan(&totalRequests, &totalCost, &successCount, &errorCount, &cacheHits, &avgLatency)

	if err != nil && err != sql.ErrNoRows {
		slog.Error("Failed to query stats from DB", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	sCount := nullInt64(successCount)
	eCount := nullInt64(errorCount)
	cHits := nullInt64(cacheHits)
	aLatency := nullFloat64(avgLatency)

	var premiumCost sql.NullFloat64
	if qErr := h.store.DB.QueryRow(`
		SELECT SUM(total_cost) FROM audit_log
		WHERE model IN ('gpt-4o', 'gpt-4.1', 'claude-sonnet-4-20250514', 'deepseek-reasoner') AND cache_hit = 0
	`).Scan(&premiumCost); qErr != nil {
		slog.Error("failed to query premium cost", "error", qErr)
	}

	pCost := nullFloat64(premiumCost)
	routingSavings := pCost * 0.70

	var avgCost sql.NullFloat64
	if qErr := h.store.DB.QueryRow(`
		SELECT AVG(total_cost) FROM audit_log WHERE cache_hit = 0 AND total_cost > 0
	`).Scan(&avgCost); qErr != nil {
		slog.Error("failed to query avg cost", "error", qErr)
	}

	aCost := 0.0015
	if avgCost.Valid && avgCost.Float64 > 0 {
		aCost = avgCost.Float64
	}
	cacheSavings := float64(cHits) * aCost

	estimatedSavings := routingSavings + cacheSavings

	var totalTokens int
	if qErr := h.store.DB.QueryRow(
		`SELECT COALESCE(SUM(prompt_tokens + completion_tokens), 0) FROM audit_log`,
	).Scan(&totalTokens); qErr != nil {
		slog.Error("failed to query total tokens", "error", qErr)
	}

	var activeKeys int
	if qErr := h.store.DB.QueryRow(
		`SELECT COUNT(DISTINCT key_id) FROM audit_log`,
	).Scan(&activeKeys); qErr != nil {
		slog.Error("failed to query active keys", "error", qErr)
	}

	var totalKeys int
	if qErr := h.store.DB.QueryRow(
		`SELECT COUNT(*) FROM api_keys`,
	).Scan(&totalKeys); qErr != nil {
		slog.Error("failed to query total keys", "error", qErr)
	}

	var enabledKeys int
	if qErr := h.store.DB.QueryRow(
		`SELECT COUNT(*) FROM api_keys WHERE enabled = 1`,
	).Scan(&enabledKeys); qErr != nil {
		slog.Error("failed to query enabled keys", "error", qErr)
	}

	var blockedRequests int
	if qErr := h.store.DB.QueryRow(`
		SELECT
			(SELECT COUNT(*) FROM pii_events WHERE action_taken = 'blocked' AND timestamp >= datetime('now', ?)) +
			(SELECT COUNT(*) FROM loop_events WHERE action_taken = 'blocked' AND detected_at >= datetime('now', ?)) +
			(SELECT COUNT(*) FROM guardrail_events WHERE action_taken = 'blocked' AND timestamp >= datetime('now', ?))
	`, statsWindow, statsWindow, statsWindow).Scan(&blockedRequests); qErr != nil {
		slog.Error("failed to query blocked requests", "error", qErr)
	}

	dailyRows, err := h.store.DB.Query(
		`SELECT DATE(timestamp) as day,
		        COUNT(*) as requests,
		        COALESCE(SUM(prompt_tokens), 0) as tokens_in,
		        COALESCE(SUM(completion_tokens), 0) as tokens_out,
		        COALESCE(SUM(total_cost), 0.0) as cost
		 FROM audit_log
		 GROUP BY DATE(timestamp)
		 ORDER BY day ASC`,
	)
	dailyStats := make([]DailyStatItem, 0)
	if err == nil {
		defer dailyRows.Close()
		for dailyRows.Next() {
			var item DailyStatItem
			if err = dailyRows.Scan(&item.Date, &item.Requests, &item.TokensIn, &item.TokensOut, &item.Cost); err == nil {
				dailyStats = append(dailyStats, item)
			}
		}
	}

	provRows, err := h.store.DB.Query(
		`SELECT provider,
		        COUNT(*) as requests,
		        COALESCE(SUM(prompt_tokens + completion_tokens), 0) as tokens,
		        COALESCE(SUM(total_cost), 0.0) as cost
		 FROM audit_log
		 WHERE provider != ''
		 GROUP BY provider
		 ORDER BY cost DESC`,
	)
	providerBreakdown := make([]ProviderBreakdownItem, 0)
	provTotal := 0.0
	if err == nil {
		defer provRows.Close()
		for provRows.Next() {
			var item ProviderBreakdownItem
			if err = provRows.Scan(&item.Provider, &item.Requests, &item.Tokens, &item.Cost); err == nil {
				provTotal += item.Cost
				providerBreakdown = append(providerBreakdown, item)
			}
		}
	}
	for i := range providerBreakdown {
		if provTotal > 0 {
			providerBreakdown[i].Pct = providerBreakdown[i].Cost / provTotal * 100
		}
	}

	healthItems := []SystemHealthItem{
		{Name: "Database", Status: "healthy", Value: "Connected", Metric: "SQLite"},
	}
	if totalRequests > 0 {
		errRate := float64(eCount) / float64(totalRequests) * 100
		errStatus := "healthy"
		errValue := fmt.Sprintf("%.1f%%", errRate)
		if errRate > 10 {
			errStatus = "degraded"
		} else if errRate > 25 {
			errStatus = "down"
		}
		healthItems = append(healthItems, SystemHealthItem{
			Name: "Error Rate", Status: errStatus, Value: errValue, Metric: fmt.Sprintf("%d / %d requests", eCount, totalRequests),
		})
	}
	if totalRequests > 0 {
		avgCost := totalCost / float64(totalRequests)
		healthItems = append(healthItems, SystemHealthItem{
			Name: "Avg Cost/Req", Status: "healthy", Value: fmt.Sprintf("$%.4f", avgCost), Metric: "Cost efficiency",
		})
	}
	if h.cfg.RateLimit.RedisURL != "" {
		healthItems = append(healthItems, SystemHealthItem{
			Name: "Redis (Rate Limit)", Status: "healthy", Value: "Configured", Metric: h.cfg.RateLimit.RedisURL,
		})
	}
	rateLimitEnabled := h.cfg.RateLimit.Enabled
	if h.configCache != nil {
		rateLimitEnabled = config.IsEnabled(h.configCache, "rate_limit")
	}
	if rateLimitEnabled {
		healthItems = append(healthItems, SystemHealthItem{
			Name: "Rate Limiter", Status: "healthy", Value: "Enabled", Metric: fmt.Sprintf("%d RPM default", h.cfg.RateLimit.DefaultRPM),
		})
	}

	resp := Response{
		TotalRequests:      totalRequests,
		TotalCost:          totalCost,
		EstimatedSavings:   estimatedSavings,
		SuccessCount:       sCount,
		ErrorCount:         eCount,
		CacheHits:          cHits,
		AvgLatencyMs:       aLatency,
		TotalTokens:        totalTokens,
		ActiveKeysUsed:     activeKeys,
		ActiveKeys:         enabledKeys,
		TotalKeys:          totalKeys,
		BlockedRequests24h: blockedRequests,
		DailyStats:         dailyStats,
		ProviderBreakdown:  providerBreakdown,
		SystemHealth:       healthItems,
	}

	model.WriteJSON(w, http.StatusOK, resp)
}
