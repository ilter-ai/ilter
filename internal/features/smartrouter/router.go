package smartrouter

import (
	"context"
	"fmt"
	"math/rand/v2"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
)

// SmartRouter directs incoming requests to the most cost-effective tier.
// In the new architecture the scorer config comes from the active strategy
// JSON in runtime_config — the middleware reads it and passes to RouteRequest.
type SmartRouter struct {
	lb         *LoadBalancer
	routingCfg config.RoutingConfig
	scorer     Scorer
}

// NewSmartRouter creates a new SmartRouter from boot config.
func NewSmartRouter(cfg *config.Config, lb *LoadBalancer) *SmartRouter {
	return &SmartRouter{
		routingCfg: cfg.Routing,
		lb:         lb,
		scorer:     NewScorerFromConfig(cfg.Routing.Scorer, cfg.Providers),
	}
}

// NewSmartRouterFromCache creates a SmartRouter from a ConfigCache snapshot.
// It is safe for concurrent use as long as the returned *SmartRouter is not
// mutated after creation — the caller should atomically swap a new one on config
// change instead of updating fields in-place.
func NewSmartRouterFromCache(cache *config.Cache, lb *LoadBalancer) *SmartRouter {
	snap := cache.Get()
	return &SmartRouter{
		routingCfg: snap.RoutingConfig(),
		lb:         lb,
		scorer:     NewScorerFromConfig(snap.RoutingConfig().Scorer, snap.Providers()),
	}
}

// RouteRequest analyzes a chat completion request and selects the appropriate model name.
// Rules are evaluated first (first match wins); if no rule matches, falls back to
// threshold-based tier classification.
func (sr *SmartRouter) RouteRequest(ctx context.Context, req *model.ChatCompletionRequest) (string, float64, error) {
	score := sr.scorer.Score(ctx, req.Messages, req.Tools)

	if len(sr.routingCfg.Rules) > 0 {
		if rule := FindMatchingRule(sr.routingCfg.Rules, req, score); rule != nil {
			return rule.TargetModel, score, nil
		}
	}

	econThreshold := 15.0
	stdThreshold := 50.0
	if t := sr.routingCfg.ComplexityThresholds; t.Economy > 0 || t.Standard > 0 {
		if t.Economy > 0 {
			econThreshold = t.Economy
		}
		if t.Standard > 0 {
			stdThreshold = t.Standard
		}
	}

	tier := "economy"
	if score >= stdThreshold {
		tier = "premium"
	} else if score >= econThreshold {
		tier = "standard"
	}

	selectedModel, err := sr.selectModelForTier(tier)
	if err != nil {
		if available := chatModels(sr.lb.GetAvailableModels()); len(available) > 0 {
			return available[0], score, nil
		}
		return "", score, fmt.Errorf("no models configured in load balancer: %w", err)
	}

	return selectedModel, score, nil
}

// selectModelForTier selects a model from active routes matching the specified tier,
// with graceful degradation to adjacent tiers.
func (sr *SmartRouter) selectModelForTier(tier string) (string, error) {
	modelsByTier := map[string][]string{
		"economy":  {},
		"standard": {},
		"premium":  {},
	}

	for _, modelName := range chatModels(sr.lb.GetAvailableModels()) {
		t := "standard"
		if entries, ok := catalog.Models[modelName]; ok && len(entries) > 0 {
			mInfo := entries[0]
			if mInfo.Tier == "free" {
				t = "economy"
			} else {
				t = mInfo.Tier
			}
		}
		modelsByTier[t] = append(modelsByTier[t], modelName)
	}

	var searchOrder []string
	switch tier {
	case "economy":
		searchOrder = []string{"economy", "standard", "premium"}
	case "premium":
		searchOrder = []string{"premium", "standard", "economy"}
	default:
		searchOrder = []string{"standard", "premium", "economy"}
	}

	for _, t := range searchOrder {
		candidates := modelsByTier[t]
		if len(candidates) > 0 {
			return candidates[rand.IntN(len(candidates))], nil //nolint:gosec // load-spreading choice among candidates, not a security context
		}
	}

	return "", fmt.Errorf("no active models found for any tier")
}

// chatModels drops decision-only models (e.g. Jev) that cannot serve chat
// completions, so they are never picked as a routing target.
func chatModels(names []string) []string {
	catalog.ModelsMu.RLock()
	defer catalog.ModelsMu.RUnlock()
	out := make([]string, 0, len(names))
	for _, name := range names {
		entries := catalog.Models[name]
		if len(entries) > 0 && entries[0].IsDecisionModel() {
			continue
		}
		out = append(out, name)
	}
	return out
}
