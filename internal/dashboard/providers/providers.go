package providers

import (
	"net/http"
	"time"

	"github.com/ilter-ai/ilter/internal/model"

	"github.com/ilter-ai/ilter/internal/features/smartrouter"
)

type ProviderModelItem struct {
	Name   string `json:"name"`
	Active bool   `json:"active"`
	Tier   string `json:"tier,omitempty"`
}

type ProviderSummary struct {
	Name                string              `json:"name"`
	Type                string              `json:"type"`
	BaseURL             string              `json:"base_url"`
	Models              []ProviderModelItem `json:"models"`
	ActiveModels        int                 `json:"active_models"`
	TotalModels         int                 `json:"total_models"`
	Status              string              `json:"status,omitempty"`
	CircuitBreakerState string              `json:"circuit_breaker_state,omitempty"`
	TotalRequests       int64               `json:"total_requests"`
	TotalErrors         int64               `json:"total_errors"`
	SuccessRate         float64             `json:"success_rate"`
	LastErrorTime       *time.Time          `json:"last_error_time,omitempty"`
	LastSuccessTime     *time.Time          `json:"last_success_time,omitempty"`
	APIKeySet           bool                `json:"api_key_set"`
	APIKeySource        string              `json:"api_key_source"`
}

func (h *Handler) HandleProviders(w http.ResponseWriter, _ *http.Request) {
	statusMap := make(map[string]smartrouter.ProviderStatus, 8)
	for _, ps := range h.lb.GetProviderStatus() {
		statusMap[ps.Name] = ps
	}

	summaries := make([]ProviderSummary, 0)
	for _, p := range h.cfg.Providers {
		summary := ProviderSummary{
			Name:         p.Name,
			Type:         p.Type,
			BaseURL:      p.BaseURL,
			APIKeySet:    p.APIKey != "",
			APIKeySource: p.APIKeySource,
		}

		dbModels, err := h.store.GetProviderModels(p.Name)
		if err == nil && len(dbModels) > 0 {
			summary.Models = make([]ProviderModelItem, len(dbModels))
			for i, m := range dbModels {
				summary.Models[i] = ProviderModelItem{
					Name:   m.Model,
					Active: m.Active,
					Tier:   m.Tier,
				}
			}
		} else {
			summary.Models = make([]ProviderModelItem, len(p.Models))
			for i, m := range p.Models {
				summary.Models[i] = ProviderModelItem{Name: m.Name, Active: true}
			}
		}

		summary.TotalModels = len(summary.Models)
		for _, m := range summary.Models {
			if m.Active {
				summary.ActiveModels++
			}
		}

		if ps, ok := statusMap[p.Name]; ok {
			summary.Status = ps.Status
			summary.CircuitBreakerState = ps.CircuitBreakerState
			summary.TotalRequests = ps.TotalRequests
			summary.TotalErrors = ps.TotalErrors
			summary.SuccessRate = ps.SuccessRate
			summary.LastErrorTime = ps.LastErrorTime
			summary.LastSuccessTime = ps.LastSuccessTime
		} else {
			summary.Status = "offline"
		}
		summaries = append(summaries, summary)
	}
	model.WriteJSON(w, http.StatusOK, summaries)
}
