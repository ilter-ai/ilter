package smartrouter

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
	"github.com/ilter-ai/ilter/internal/provider"
)

type OptimizeRequest struct {
	Prompt       string `json:"prompt"`
	CurrentModel string `json:"current_model"`
}

type OptimizeResponse struct {
	ComplexityScore     float64          `json:"complexity_score"`
	CurrentCostEstimate float64          `json:"current_cost_estimate"`
	Recommendations     []Recommendation `json:"recommendations"`
}

type Recommendation struct {
	Model          string  `json:"model"`
	EstimatedCost  float64 `json:"estimated_cost"`
	SavingsPercent int     `json:"savings_percent"`
	QualityImpact  string  `json:"quality_impact"`
}

type updateProviderRequest struct {
	Name    string   `json:"name"`
	BaseURL string   `json:"base_url"`
	APIKey  *string  `json:"api_key"`  // nil = keep current, "" = clear, "sk-..." = set
	APIKeys []string `json:"api_keys"` // optional list of multi-keys
}

func (h *Handler) HandleOptimize(w http.ResponseWriter, r *http.Request) {
	var req OptimizeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "invalid JSON body")
		return
	}

	wordCount := len(strings.Fields(req.Prompt))
	inputTokens := int(float64(wordCount) * 1.3)
	outputTokens := 150

	currentInputCost := 0.0000025
	currentOutputCost := 0.00001

	currentModelInfos, foundCurrent := catalog.Models[req.CurrentModel]
	if foundCurrent && len(currentModelInfos) > 0 {
		currentInputCost = currentModelInfos[0].CostPerInputToken
		currentOutputCost = currentModelInfos[0].CostPerOutputToken
	}
	currentCostEstimate := float64(inputTokens)*currentInputCost + float64(outputTokens)*currentOutputCost

	messages := []model.Message{{Role: "user", Content: req.Prompt}}
	score := smartrouter.ScoreComplexity(messages)

	var recommendations []Recommendation
	for mName, mInfos := range catalog.Models {
		if len(mInfos) == 0 {
			continue
		}
		mInfo := mInfos[0]
		if (mInfo.Tier == "economy" || mInfo.Tier == "free") && mName != req.CurrentModel {
			estCost := float64(inputTokens)*mInfo.CostPerInputToken + float64(outputTokens)*mInfo.CostPerOutputToken
			if estCost < currentCostEstimate && currentCostEstimate > 0 {
				savingsPercent := int((1.0 - estCost/currentCostEstimate) * 100)
				if savingsPercent > 0 {
					impact := "minimal — sufficient for simple questions"
					if score >= 50 {
						impact = "medium — quality may degrade for complex reasoning tasks"
					} else if score >= 20 {
						impact = "low — sufficient for standard tasks"
					}

					recommendations = append(recommendations, Recommendation{
						Model:          mName,
						EstimatedCost:  estCost,
						SavingsPercent: savingsPercent,
						QualityImpact:  impact,
					})
				}
			}
		}
	}

	sort.Slice(recommendations, func(i, j int) bool {
		return recommendations[i].SavingsPercent > recommendations[j].SavingsPercent
	})

	resp := OptimizeResponse{
		ComplexityScore:     score,
		CurrentCostEstimate: currentCostEstimate,
		Recommendations:     recommendations,
	}

	model.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req updateProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}
	if req.Name == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Provider name is required")
		return
	}

	apiKeyToSave := ""
	if req.APIKey != nil {
		apiKeyToSave = *req.APIKey
	}

	var cleanedAPIKeys []string
	for _, k := range req.APIKeys {
		if trimmed := strings.TrimSpace(k); trimmed != "" {
			cleanedAPIKeys = append(cleanedAPIKeys, trimmed)
		}
	}
	if len(cleanedAPIKeys) > 0 && apiKeyToSave == "" {
		apiKeyToSave = cleanedAPIKeys[0]
	}

	prov, err := h.reg.Get(req.Name)
	if err == nil {
		if cp, ok := prov.(provider.ConfigurableProvider); ok {
			if len(cleanedAPIKeys) > 0 {
				cp.UpdateKeys(req.BaseURL, apiKeyToSave, cleanedAPIKeys)
			} else {
				cp.UpdateConfig(req.BaseURL, apiKeyToSave)
			}
		}
	} else {
		slog.Debug("Provider not found in registry for runtime update", "provider", req.Name, "error", err)
	}

	for i := range h.cfg.Providers {
		p := &h.cfg.Providers[i]
		if p.Name == req.Name {
			if req.BaseURL != "" {
				p.BaseURL = req.BaseURL
			}
			if req.APIKey != nil || len(cleanedAPIKeys) > 0 {
				p.APIKey = apiKeyToSave
			}
			if len(cleanedAPIKeys) > 0 {
				p.APIKeys = cleanedAPIKeys
			} else if req.APIKey != nil && *req.APIKey == "" {
				p.APIKeys = nil
			}
			break
		}
	}

	// Auto-sync: discover models from this provider and save to DB.
	go func() {
		syncCtx, syncCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer syncCancel()
		prov, err := h.reg.Get(req.Name)
		if err == nil {
			models, err := prov.DiscoverModels(syncCtx)
			if err != nil {
				slog.Warn("Failed to discover models after provider update", "provider", req.Name, "error", err)
			} else if err := h.store.SaveDiscoveredModels(req.Name, models); err != nil {
				slog.Warn("Failed to save discovered models after provider update", "provider", req.Name, "error", err)
			} else {
				slog.Debug("Discovered and saved models after provider update", "provider", req.Name, "count", len(models))
			}
		}
	}()

	model.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}
