package stats

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/ilter-ai/ilter/internal/model"

	"github.com/ilter-ai/ilter/internal/model/catalog"
)

type TopExpensiveItem struct {
	ID               int     `json:"id"`
	Model            string  `json:"model"`
	Provider         string  `json:"provider"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalCost        float64 `json:"total_cost"`
	ComplexityScore  float64 `json:"complexity_score"`
	Timestamp        string  `json:"timestamp"`
}

func (h *Handler) HandleTopExpensiveRequests(w http.ResponseWriter, _ *http.Request) {
	rows, err := h.store.DB.Query(`
		SELECT id, model, provider, prompt_tokens, completion_tokens,
		       total_cost, COALESCE(complexity_score, 0), timestamp
		FROM audit_log
		ORDER BY total_cost DESC
		LIMIT 10
	`)
	if err != nil {
		slog.Error("Failed to query top expensive requests", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	defer rows.Close()

	items := make([]TopExpensiveItem, 0, 10)
	for rows.Next() {
		var item TopExpensiveItem
		if err := rows.Scan(&item.ID, &item.Model, &item.Provider,
			&item.PromptTokens, &item.CompletionTokens,
			&item.TotalCost, &item.ComplexityScore, &item.Timestamp); err != nil {
			slog.Error("Failed to scan top expensive row", "error", err)
			continue
		}
		items = append(items, item)
	}

	model.WriteJSON(w, http.StatusOK, items)
}

type CostTrendItem struct {
	Date             string  `json:"date"`
	TotalCost        float64 `json:"total_cost"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	RequestCount     int     `json:"request_count"`
}

func (h *Handler) HandleCostTrend(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	dateExpr := "DATE(timestamp)"
	if period == "weekly" {
		dateExpr = "DATE(timestamp, 'weekday 0')"
	}

	rows, err := h.store.DB.Query(fmt.Sprintf(`
		SELECT %s as date,
		       SUM(total_cost) as total_cost,
		       COALESCE(SUM(prompt_tokens), 0) as prompt_tokens,
		       COALESCE(SUM(completion_tokens), 0) as completion_tokens,
		       COUNT(*) as request_count
		FROM audit_log
		WHERE timestamp >= date('now', '-30 days')
		GROUP BY %s
		ORDER BY date ASC
	`, dateExpr, dateExpr))
	if err != nil {
		slog.Error("Failed to query cost trend", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	defer rows.Close()

	items := make([]CostTrendItem, 0)
	for rows.Next() {
		var item CostTrendItem
		var totalCost sql.NullFloat64
		var promptTokens, completionTokens, requestCount sql.NullInt64
		if err := rows.Scan(&item.Date, &totalCost, &promptTokens, &completionTokens, &requestCount); err != nil {
			slog.Error("Failed to scan cost trend row", "error", err)
			continue
		}
		if totalCost.Valid {
			item.TotalCost = totalCost.Float64
		}
		if promptTokens.Valid {
			item.PromptTokens = int(promptTokens.Int64)
		}
		if completionTokens.Valid {
			item.CompletionTokens = int(completionTokens.Int64)
		}
		if requestCount.Valid {
			item.RequestCount = int(requestCount.Int64)
		}
		items = append(items, item)
	}

	model.WriteJSON(w, http.StatusOK, items)
}

type ModelCostItem struct {
	Model            string  `json:"model"`
	TotalCost        float64 `json:"total_cost"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	RequestCount     int     `json:"request_count"`
}

func (h *Handler) HandleCostByModel(w http.ResponseWriter, _ *http.Request) {
	rows, err := h.store.DB.Query(`
		SELECT model,
		       SUM(total_cost) as total_cost,
		       COALESCE(SUM(prompt_tokens), 0) as prompt_tokens,
		       COALESCE(SUM(completion_tokens), 0) as completion_tokens,
		       COUNT(*) as request_count
		FROM audit_log
		WHERE model != ''
		GROUP BY model
		ORDER BY total_cost DESC
	`)
	if err != nil {
		slog.Error("Failed to query cost by model", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	defer rows.Close()

	items := make([]ModelCostItem, 0)
	for rows.Next() {
		var item ModelCostItem
		var totalCost sql.NullFloat64
		var promptTokens, completionTokens, requestCount sql.NullInt64
		if err := rows.Scan(&item.Model, &totalCost, &promptTokens, &completionTokens, &requestCount); err != nil {
			slog.Error("Failed to scan cost by model row", "error", err)
			continue
		}
		if totalCost.Valid {
			item.TotalCost = totalCost.Float64
		}
		if promptTokens.Valid {
			item.PromptTokens = int(promptTokens.Int64)
		}
		if completionTokens.Valid {
			item.CompletionTokens = int(completionTokens.Int64)
		}
		if requestCount.Valid {
			item.RequestCount = int(requestCount.Int64)
		}
		items = append(items, item)
	}

	model.WriteJSON(w, http.StatusOK, items)
}

type SavingsItem struct {
	Model                   string  `json:"model"`
	ActualCost              float64 `json:"actual_cost"`
	CheapestAlternativeCost float64 `json:"cheapest_alternative_cost"`
	Savings                 float64 `json:"savings"`
	RequestCount            int     `json:"request_count"`
	RecommendedModel        string  `json:"recommended_model"`
}

type SavingsResponse struct {
	TotalSavingsPotential float64       `json:"total_savings_potential"`
	ActualTotalCost       float64       `json:"actual_total_cost"`
	SavingsRate           float64       `json:"savings_rate"`
	RequestCount          int           `json:"request_count"`
	Opportunities         []SavingsItem `json:"opportunities"`
}

type tierCheapest struct {
	name    string
	inRate  float64
	outRate float64
}

func (h *Handler) HandleSavingsOpportunity(w http.ResponseWriter, _ *http.Request) {
	catalog.ModelsMu.RLock()
	modelTier := make(map[string]string, len(catalog.Models))
	cheapestInTier := make(map[string]tierCheapest)
	for name, infos := range catalog.Models {
		if len(infos) == 0 {
			continue
		}
		info := infos[0]
		tier := info.Tier
		if tier == "" {
			continue
		}
		modelTier[name] = tier

		rate := info.CostPerInputToken + info.CostPerOutputToken
		existing, ok := cheapestInTier[tier]
		if !ok || rate < existing.inRate+existing.outRate {
			cheapestInTier[tier] = tierCheapest{name: name, inRate: info.CostPerInputToken, outRate: info.CostPerOutputToken}
		}
	}
	catalog.ModelsMu.RUnlock()

	rows, err := h.store.DB.Query(`
		SELECT model, total_cost, prompt_tokens, completion_tokens
		FROM audit_log
		WHERE timestamp >= datetime('now', '-7 days') AND model != ''
	`)
	if err != nil {
		slog.Error("Failed to query audit_log for savings opportunity", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	defer rows.Close()

	type modelAccum struct {
		actualCost       float64
		altCost          float64
		count            int
		recommendedModel string
	}
	accum := make(map[string]*modelAccum)
	var totalActual float64
	var totalAlt float64
	var totalCount int

	for rows.Next() {
		var modelName string
		var totalCost sql.NullFloat64
		var promptTokens, completionTokens sql.NullInt64

		if err := rows.Scan(&modelName, &totalCost, &promptTokens, &completionTokens); err != nil {
			slog.Error("Failed to scan savings opportunity row", "error", err)
			continue
		}

		actualCost := 0.0
		if totalCost.Valid {
			actualCost = totalCost.Float64
		}
		pTokens := 0
		if promptTokens.Valid {
			pTokens = int(promptTokens.Int64)
		}
		cTokens := 0
		if completionTokens.Valid {
			cTokens = int(completionTokens.Int64)
		}

		tier := modelTier[modelName]
		cheapest, ok := cheapestInTier[tier]
		altCost := 0.0
		recModel := ""
		if ok {
			altCost = float64(pTokens)*cheapest.inRate + float64(cTokens)*cheapest.outRate
			recModel = cheapest.name
		} else {
			altCost = actualCost
		}

		if actualCost < altCost {
			altCost = actualCost
		}

		totalActual += actualCost
		totalAlt += altCost
		totalCount++

		a, exists := accum[modelName]
		if !exists {
			a = &modelAccum{recommendedModel: recModel}
			accum[modelName] = a
		}
		a.actualCost += actualCost
		a.altCost += altCost
		a.count++
		if recModel != "" {
			a.recommendedModel = recModel
		}
	}

	if err := rows.Err(); err != nil {
		slog.Error("Rows iteration error in savings opportunity", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	opportunities := make([]SavingsItem, 0, len(accum))
	var totalSavings float64
	for modelName, a := range accum {
		savings := a.actualCost - a.altCost
		if savings < 0 {
			savings = 0
		}
		totalSavings += savings
		opportunities = append(opportunities, SavingsItem{
			Model:                   modelName,
			ActualCost:              a.actualCost,
			CheapestAlternativeCost: a.altCost,
			Savings:                 savings,
			RequestCount:            a.count,
			RecommendedModel:        a.recommendedModel,
		})
	}

	savingsRate := 0.0
	if totalActual > 0 {
		savingsRate = (totalSavings / totalActual) * 100
	}

	resp := SavingsResponse{
		TotalSavingsPotential: totalSavings,
		ActualTotalCost:       totalActual,
		SavingsRate:           savingsRate,
		RequestCount:          totalCount,
		Opportunities:         opportunities,
	}

	model.WriteJSON(w, http.StatusOK, resp)
}

type ProviderCostBreakdown struct {
	Provider string  `json:"provider"`
	Cost     float64 `json:"cost"`
	Count    int     `json:"count"`
	Pct      float64 `json:"pct"`
}

type ModelCostBreakdown struct {
	Model string  `json:"model"`
	Cost  float64 `json:"cost"`
	Count int     `json:"count"`
	Pct   float64 `json:"pct"`
}

type TimeCostItem struct {
	Period string  `json:"period"`
	Cost   float64 `json:"cost"`
	Count  int     `json:"count"`
}

type CostAttributionResponse struct {
	TotalCost         float64                 `json:"total_cost"`
	TotalRequests     int                     `json:"total_requests"`
	AvgCostPerRequest float64                 `json:"avg_cost_per_request"`
	Period            string                  `json:"period"`
	ByProvider        []ProviderCostBreakdown `json:"by_provider"`
	ByModel           []ModelCostBreakdown    `json:"by_model"`
	TimeSeries        []TimeCostItem          `json:"time_series"`
	SavingsSummary    any                     `json:"savings_summary"`
}

func (h *Handler) HandleCostsOverview(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	var since string
	switch period {
	case "24h":
		since = "-24 hours"
	case "7d":
		since = "-7 days"
	case "30d":
		since = "-30 days"
	default:
		since = "-30 days"
		period = "30d"
	}

	db := h.store.DB

	// 1. Overall Summary
	var totalCost float64
	var totalRequests int
	err := db.QueryRow(`
		SELECT COALESCE(SUM(total_cost), 0.0), COUNT(*)
		FROM audit_log
		WHERE timestamp >= datetime('now', ?)
	`, since).Scan(&totalCost, &totalRequests)
	if err != nil && err != sql.ErrNoRows {
		slog.Error("Failed to query costs overall summary", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	var avgCost float64
	if totalRequests > 0 {
		avgCost = totalCost / float64(totalRequests)
	}

	// 2. Provider Breakdown
	provRows, err := db.Query(`
		SELECT provider, COALESCE(SUM(total_cost), 0.0) as cost, COUNT(*) as count
		FROM audit_log
		WHERE timestamp >= datetime('now', ?) AND provider != ''
		GROUP BY provider
		ORDER BY cost DESC
	`, since)
	if err != nil {
		slog.Error("Failed to query costs by provider", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	defer provRows.Close()

	byProvider := make([]ProviderCostBreakdown, 0)
	for provRows.Next() {
		var item ProviderCostBreakdown
		if scanErr := provRows.Scan(&item.Provider, &item.Cost, &item.Count); scanErr == nil {
			if totalCost > 0 {
				item.Pct = (item.Cost / totalCost) * 100.0
			}
			byProvider = append(byProvider, item)
		}
	}

	// 3. Model Breakdown
	modelRows, err := db.Query(`
		SELECT model, COALESCE(SUM(total_cost), 0.0) as cost, COUNT(*) as count
		FROM audit_log
		WHERE timestamp >= datetime('now', ?) AND model != ''
		GROUP BY model
		ORDER BY cost DESC
	`, since)
	if err != nil {
		slog.Error("Failed to query costs by model", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	defer modelRows.Close()

	byModel := make([]ModelCostBreakdown, 0)
	for modelRows.Next() {
		var item ModelCostBreakdown
		if scanErr := modelRows.Scan(&item.Model, &item.Cost, &item.Count); scanErr == nil {
			if totalCost > 0 {
				item.Pct = (item.Cost / totalCost) * 100.0
			}
			byModel = append(byModel, item)
		}
	}

	// 4. Time Series
	var timeFormat string
	if period == "24h" {
		timeFormat = "%Y-%m-%d %H:00"
	} else {
		timeFormat = "%Y-%m-%d"
	}

	tsRows, err := db.Query(`
		SELECT strftime(?, timestamp) as time_period, COALESCE(SUM(total_cost), 0.0) as cost, COUNT(*) as count
		FROM audit_log
		WHERE timestamp >= datetime('now', ?)
		GROUP BY time_period
		ORDER BY time_period ASC
	`, timeFormat, since)
	if err != nil {
		slog.Error("Failed to query costs time series", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	defer tsRows.Close()

	timeSeries := make([]TimeCostItem, 0)
	for tsRows.Next() {
		var item TimeCostItem
		if scanErr := tsRows.Scan(&item.Period, &item.Cost, &item.Count); scanErr == nil {
			timeSeries = append(timeSeries, item)
		}
	}

	resp := CostAttributionResponse{
		TotalCost:         totalCost,
		TotalRequests:     totalRequests,
		AvgCostPerRequest: avgCost,
		Period:            period,
		ByProvider:        byProvider,
		ByModel:           byModel,
		TimeSeries:        timeSeries,
		SavingsSummary:    nil,
	}

	model.WriteJSON(w, http.StatusOK, resp)
}

type KeyCostBreakdown struct {
	KeyID      string  `json:"key_id"`
	APIKeyName string  `json:"api_key_name"`
	Cost       float64 `json:"cost"`
	Count      int     `json:"count"`
	Pct        float64 `json:"pct"`
}

type CostByKeyResponse struct {
	Period string             `json:"period"`
	ByKey  []KeyCostBreakdown `json:"by_key"`
}

func (h *Handler) HandleCostsByKey(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	var since string
	switch period {
	case "24h":
		since = "-24 hours"
	case "7d":
		since = "-7 days"
	case "30d":
		since = "-30 days"
	default:
		since = "-30 days"
		period = "30d"
	}

	db := h.store.DB

	// Query total cost in that period to compute percentages
	var totalCost float64
	_ = db.QueryRow(`
		SELECT COALESCE(SUM(total_cost), 0.0)
		FROM audit_log
		WHERE timestamp >= datetime('now', ?)
	`, since).Scan(&totalCost)

	rows, err := db.Query(`
		SELECT al.key_id, COALESCE(ak.name, 'Admin/Unknown') as api_key_name, COALESCE(SUM(al.total_cost), 0.0) as cost, COUNT(*) as count
		FROM audit_log al
		LEFT JOIN api_keys ak ON ak.id = al.key_id
		WHERE al.timestamp >= datetime('now', ?) AND al.key_id IS NOT NULL
		GROUP BY al.key_id
		ORDER BY cost DESC
	`, since)
	if err != nil {
		slog.Error("Failed to query costs by key", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	defer rows.Close()

	byKey := make([]KeyCostBreakdown, 0)
	for rows.Next() {
		var item KeyCostBreakdown
		var keyID *string
		if scanErr := rows.Scan(&keyID, &item.APIKeyName, &item.Cost, &item.Count); scanErr == nil {
			if keyID != nil {
				item.KeyID = *keyID
			}
			if totalCost > 0 {
				item.Pct = (item.Cost / totalCost) * 100.0
			}
			byKey = append(byKey, item)
		}
	}

	resp := CostByKeyResponse{
		Period: period,
		ByKey:  byKey,
	}

	model.WriteJSON(w, http.StatusOK, resp)
}
