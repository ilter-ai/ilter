package proxy

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

// recordPostResponse records usage and enforces budget/loop tracking.
func (h *Handler) recordPostResponse(r *http.Request, chatResp *model.ChatCompletionResponse, route *smartrouter.Route, _ time.Time) {
	if chatResp == nil || chatResp.Usage == nil {
		return
	}

	meta := reqmeta.GetRequestMetadata(r.Context())
	keyID := reqmeta.GetKeyID(r.Context())
	cost := CalculateCost(route.Model, chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens)

	if meta != nil {
		meta.SetTokensAndCost(chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens, cost)
	}

	if h.budgetEnforcer != nil {
		if err := h.budgetEnforcer.RecordUsage(r.Context(), keyID, cost); err != nil {
			slog.Error("failed to record budget usage", "error", err)
		}
	}

	// Record daily usage statistics (PRD 04 — Daily Usage Logging)
	if h.store != nil {
		today := time.Now().UTC().Format("2006-01-02")
		cacheHits := 0
		if meta != nil && meta.CacheHit != nil && *meta.CacheHit {
			cacheHits = 1
		}
		if err := h.store.RecordDailyUsage(
			keyID,
			today,
			route.Model.Name,
			route.Provider.Name(),
			chatResp.Usage.PromptTokens,
			chatResp.Usage.CompletionTokens,
			cacheHits,
			cost,
		); err != nil {
			slog.Error("Failed to record daily usage", "error", err)
		}
	}

	if h.loopDetector != nil && h.cfg != nil && h.cfg.CostGuard.LoopDetection {
		h.loopDetector.RecordCost(keyID, cost)
	}
}
