package proxy

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
)

// CalculateCost returns the dollar cost for a request. The 6-decimal
// rounding protects the SQLite REAL column from float64 representation
// noise (PRD 04-IMPLEMENTATION-PLAN.md Sprint 2.2 floating point risk).
func CalculateCost(m config.ModelConfig, promptTokens, completionTokens int) float64 {
	inputCost := float64(promptTokens) * m.CostPerInputToken
	outputCost := float64(completionTokens) * m.CostPerOutputToken
	return math.Round((inputCost+outputCost)*1e6) / 1e6
}

func estimateInputTokens(messages []model.Message) int {
	var wordCount int
	for _, msg := range messages {
		if msg.Content == nil {
			continue
		}
		switch v := msg.Content.(type) {
		case string:
			wordCount += len(strings.Fields(v))
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					wordCount += len(strings.Fields(s))
				} else if m, ok := item.(map[string]any); ok {
					if text, ok := m["text"].(string); ok {
						wordCount += len(strings.Fields(text))
					}
				}
			}
		}
	}
	if wordCount < 4 {
		wordCount = 4
	}
	return int(float64(wordCount) * 1.3)
}

func findCheapestAlternativeCost(selectedModel string, inputTokens, outputTokens int) float64 {
	catalog.ModelsMu.RLock()
	defer catalog.ModelsMu.RUnlock()

	selectedInfos, found := catalog.Models[selectedModel]
	if !found || len(selectedInfos) == 0 {
		return 0
	}
	selectedInfo := selectedInfos[0]

	// Normalize tier for comparison — treat "free" as "economy" for alternative matching
	targetTier := selectedInfo.Tier
	if targetTier == "free" {
		targetTier = "economy"
	}

	var cheapestCost float64
	var foundCheaper bool

	for name, infos := range catalog.Models {
		if name == selectedModel || len(infos) == 0 {
			continue
		}
		info := infos[0]
		compareTier := info.Tier
		if compareTier == "free" {
			compareTier = "economy"
		}
		if compareTier == targetTier {
			cost := float64(inputTokens)*info.CostPerInputToken + float64(outputTokens)*info.CostPerOutputToken
			if !foundCheaper || cost < cheapestCost {
				cheapestCost = cost
				foundCheaper = true
			}
		}
	}

	if !foundCheaper {
		return 0
	}
	return math.Round(cheapestCost*1e6) / 1e6
}

func providerErrorStatus(err error) int {
	if err == nil {
		return http.StatusBadGateway
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "quota"), strings.Contains(msg, "insufficient"), strings.Contains(msg, "billing"),
		strings.Contains(msg, "limit exceeded"), strings.Contains(msg, "rate limit"), strings.Contains(msg, "credit"),
		strings.Contains(msg, "balance"), strings.Contains(msg, "payment required"), strings.Contains(msg, "funds"), strings.Contains(msg, "429"):
		return http.StatusTooManyRequests
	case strings.Contains(msg, "401"), strings.Contains(msg, "unauthorized"), strings.Contains(msg, "forbidden"), strings.Contains(msg, "403"):
		return http.StatusUnauthorized
	case strings.Contains(msg, "400"), strings.Contains(msg, "invalid_request"):
		return http.StatusBadRequest
	default:
		return http.StatusBadGateway
	}
}

func sanitizeProviderErrorMessage(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return trimmed
	}

	var envelope struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Error   struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(trimmed), &envelope); err == nil {
		if envelope.Error.Message != "" {
			return envelope.Error.Message
		}
		if envelope.Message != "" {
			return envelope.Message
		}
	}

	return trimmed
}

func computeCostEstimates(messages []model.Message, modelCfg config.ModelConfig, selectedModel string, maxTokens *int) (costEstimate, altCost, savingsPotential float64) {
	estInputTokens := estimateInputTokens(messages)
	estOutputTokens := 150
	if maxTokens != nil && *maxTokens > 0 {
		estOutputTokens = *maxTokens
	}

	costEstimate = float64(estInputTokens)*modelCfg.CostPerInputToken +
		float64(estOutputTokens)*modelCfg.CostPerOutputToken
	costEstimate = math.Round(costEstimate*1e6) / 1e6

	altCost = findCheapestAlternativeCost(selectedModel, estInputTokens, estOutputTokens)

	if altCost > 0 && costEstimate > 0 && altCost < costEstimate {
		savingsPotential = (1.0 - altCost/costEstimate) * 100
	}
	return
}
