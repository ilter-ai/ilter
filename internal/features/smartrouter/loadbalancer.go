package smartrouter

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/circuitbreaker"
	"github.com/ilter-ai/ilter/internal/platform/cooldown"
	"github.com/ilter-ai/ilter/internal/provider"
)

// Route binds a provider to a model config.
type Route struct {
	Provider provider.Provider
	Model    config.ModelConfig
}

type ProviderModelEntry struct {
	Name    string
	Active  bool
	Tier    string
	CostIn  float64
	CostOut float64
}

// LoadBalancer selects provider routes for a given model.
type LoadBalancer struct {
	mu             sync.RWMutex
	routes         map[string][]Route
	providers      map[string]provider.Provider
	inactiveModels map[string]bool
	rrCounters     map[string]int // per-model round-robin index, survives config reloads
	cache          *config.Cache
	cfg            *config.Config
}

// NewLoadBalancer creates a LoadBalancer with routes from the config.
func NewLoadBalancer(cfg *config.Config, reg *provider.Registry, cache *config.Cache) (*LoadBalancer, error) {
	config.EnrichConfig(cfg)

	lb := &LoadBalancer{
		routes:         make(map[string][]Route),
		providers:      make(map[string]provider.Provider),
		inactiveModels: make(map[string]bool),
		rrCounters:     make(map[string]int),
		cache:          cache,
		cfg:            cfg,
	}

	for _, pCfg := range cfg.Providers {
		p, err := reg.Get(pCfg.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to get provider %s from registry: %w", pCfg.Name, err)
		}

		lb.providers[pCfg.Name] = p

		for _, mCfg := range pCfg.Models {
			lb.routes[mCfg.Name] = append(lb.routes[mCfg.Name], Route{
				Provider: p,
				Model:    mCfg,
			})
		}
	}

	return lb, nil
}

// GetProvider returns a provider by name from the LoadBalancer.
func (lb *LoadBalancer) GetProvider(name string) (provider.Provider, error) {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	p, ok := lb.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %s not found in load balancer", name)
	}
	return p, nil
}

// filterAvailable returns routes whose circuit breaker is not open.
func filterAvailable(routes []Route) []Route {
	return slices.DeleteFunc(routes, func(r Route) bool {
		return circuitbreaker.State(r.Provider.Client().Transport) == "open"
	})
}

// NextRoute selects a provider for the given model.
func (lb *LoadBalancer) NextRoute(modelName string, preference string) (Route, error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	if lb.inactiveModels[modelName] {
		return Route{}, fmt.Errorf("model %s is deactivated", modelName)
	}

	routes, ok := lb.routes[modelName]
	if !ok || len(routes) == 0 {
		return Route{}, fmt.Errorf("no providers configured for model: %s", modelName)
	}

	avail := filterAvailable(routes)
	if len(avail) == 0 {
		return Route{}, fmt.Errorf("all providers for model %s have open circuit breakers", modelName)
	}

	switch preference {
	case "round-robin":
		idx := lb.rrCounters[modelName]
		lb.rrCounters[modelName] = (idx + 1) % len(avail)
		return avail[idx], nil
	default:
		return avail[0], nil
	}
}

// SelectCandidates returns a ranked list of cooldown.Candidate for the requested model.
// Active (available) routes are prioritized; candidates in cooldown are placed at the tail.
// When FallbackConfig.ModelDowngrade is non-none, downgrade model candidates are appended
// after the primary model's candidates so FallbackExecutor can try them on exhaustion.
func (lb *LoadBalancer) SelectCandidates(ctx context.Context, modelName string, preference string, cooldownStore cooldown.Store) ([]cooldown.Candidate, error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	if lb.inactiveModels[modelName] {
		return nil, fmt.Errorf("model %s is deactivated", modelName)
	}

	routes, ok := lb.routes[modelName]
	hasPrimaryRoutes := ok && len(routes) > 0

	// Read fallback config from cache (includes runtime_config overrides).
	fb := lb.cfg.Fallback
	if lb.cache != nil {
		if snap := lb.cache.Get(); snap != nil {
			fb = snap.Fallback()
		}
	}

	// Model not in registry — try downgrade fallback before erroring.
	// Uses the same dashboard-configured algorithm (cheapest/specific/none).
	if !hasPrimaryRoutes {
		if !fb.Enabled || fb.ModelDowngrade == "" || fb.ModelDowngrade == "none" {
			return nil, fmt.Errorf("no providers configured for model: %s", modelName)
		}
		downgradeModels := lb.resolveDowngradeModels(modelName, fb)
		var candidates []cooldown.Candidate
		for _, dm := range downgradeModels {
			if dRoutes, ok := lb.routes[dm]; ok && len(dRoutes) > 0 {
				extra := lb.buildCandidates(ctx, dRoutes, dm, preference, true, cooldownStore)
				candidates = append(candidates, extra...)
			}
		}
		if len(candidates) > 0 {
			return candidates, nil
		}
		return nil, fmt.Errorf("no providers configured for model: %s", modelName)
	}

	candidates := lb.buildCandidates(ctx, routes, modelName, preference, false, cooldownStore)

	// Append ModelDowngrade candidates when the fallback config requests it.
	if fb.Enabled && fb.ModelDowngrade != "" && fb.ModelDowngrade != "none" {
		downgradeModels := lb.resolveDowngradeModels(modelName, fb)
		for _, dm := range downgradeModels {
			if dm == modelName {
				continue
			}
			if dRoutes, ok := lb.routes[dm]; ok && len(dRoutes) > 0 {
				extra := lb.buildCandidates(ctx, dRoutes, dm, preference, true, cooldownStore)
				candidates = append(candidates, extra...)
			}
		}
	}

	return candidates, nil
}

// buildCandidates creates candidate entries from routes, optionally marking them as IsDowngrade.
func (lb *LoadBalancer) buildCandidates(ctx context.Context, routes []Route, modelName, preference string, isDowngrade bool, cooldownStore cooldown.Store) []cooldown.Candidate {
	avail := filterAvailable(routes)
	if len(avail) == 0 {
		avail = routes
	}

	if preference == "round-robin" && len(avail) > 1 {
		idx := lb.rrCounters[modelName]
		lb.rrCounters[modelName] = (idx + 1) % len(avail)
		avail = append(avail[idx:], avail[:idx]...)
	}

	var candidates []cooldown.Candidate
	var inCooldown []cooldown.Candidate

	for _, r := range avail {
		var keys []string
		if kp, ok := r.Provider.(interface{ APIKeys() []string }); ok {
			keys = kp.APIKeys()
		}
		if len(keys) == 0 {
			keys = []string{""}
		}

		for i, k := range keys {
			keyID := "default"
			if len(keys) > 1 {
				keyID = fmt.Sprintf("key_%d", i+1) // 1-indexed, only when multi-key
			}
			cand := cooldown.Candidate{
				Provider:    r.Provider.Name(),
				Model:       r.Model.Name,
				APIKey:      strings.TrimSpace(k),
				KeyID:       keyID,
				IsDowngrade: isDowngrade,
			}
			if cooldownStore != nil && cooldownStore.InCooldown(ctx, cand) {
				inCooldown = append(inCooldown, cand)
			} else {
				candidates = append(candidates, cand)
			}
		}
	}

	candidates = append(candidates, inCooldown...)
	return candidates
}

// resolveDowngradeModels returns models to use as fallback candidates.
func (lb *LoadBalancer) resolveDowngradeModels(primaryModel string, fb config.FallbackConfig) []string {
	if fb.ModelDowngrade == "" || fb.ModelDowngrade == "none" {
		return nil
	}

	switch fb.ModelDowngrade {
	case "cheapest":
		return lb.findCheapestDowngrade(primaryModel, fb.AllowedModels)
	default:
		if len(fb.AllowedModels) > 0 {
			for _, m := range fb.AllowedModels {
				if m == fb.ModelDowngrade {
					return []string{m}
				}
			}
			return nil
		}
		if fb.ModelDowngrade == primaryModel {
			return nil
		}
		return []string{fb.ModelDowngrade}
	}
}

// findCheapestDowngrade finds all allowed models at the cheapest per-token cost
// that aren't the primary model. All equal-cost models are returned (e.g., all
// free models with cost=0) so FallbackExecutor can iterate through them — if
// one is in cooldown or fails, the next is tried instead of stopping at one.
// Among ties, order follows the dashboard-configured AllowedModels priority
// list (first added = tried first), not an arbitrary/alphabetical order —
// FallbackExecutor stops at the first candidate that succeeds, so list order
// decides the winner.
func (lb *LoadBalancer) findCheapestDowngrade(primaryModel string, allowedModels []string) []string {
	var candidates []string
	for name := range lb.routes {
		if name == primaryModel {
			continue
		}
		if len(allowedModels) > 0 {
			found := slices.Contains(allowedModels, name)
			if !found {
				continue
			}
		}
		candidates = append(candidates, name)
	}
	if len(candidates) == 0 {
		return nil
	}

	minCost := math.MaxFloat64
	for _, name := range candidates {
		routes := lb.routes[name]
		if len(routes) == 0 {
			continue
		}
		cost := routes[0].Model.CostPerInputToken + routes[0].Model.CostPerOutputToken
		if cost < minCost {
			minCost = cost
		}
	}

	inCheapest := make(map[string]bool, len(candidates))
	for _, name := range candidates {
		routes := lb.routes[name]
		if len(routes) == 0 {
			continue
		}
		cost := routes[0].Model.CostPerInputToken + routes[0].Model.CostPerOutputToken
		if cost == minCost {
			inCheapest[name] = true
		}
	}

	var cheapest []string
	if len(allowedModels) > 0 {
		for _, name := range allowedModels {
			if inCheapest[name] {
				cheapest = append(cheapest, name)
			}
		}
	} else {
		names := make([]string, 0, len(inCheapest))
		for name := range inCheapest {
			names = append(names, name)
		}
		sort.Strings(names)
		cheapest = names
	}

	return cheapest
}

// RebuildProviders clears all routes and reloads them from the given provider catalog.
func (lb *LoadBalancer) RebuildProviders(reg *provider.Registry) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.routes = make(map[string][]Route)
	lb.providers = make(map[string]provider.Provider)

	for _, p := range reg.List() {
		name := p.Name()
		lb.providers[name] = p
		models, err := p.DiscoverModels(context.Background())
		if err != nil {
			continue
		}
		for _, m := range models {
			lb.routes[m.ID] = append(lb.routes[m.ID], Route{
				Provider: p,
				Model: config.ModelConfig{
					Name: m.ID,
				},
			})
		}
	}
}

// LoadRoutesFromDB adds routes from a provider model store for providers that
// have no YAML-configured models.
func (lb *LoadBalancer) LoadRoutesFromDB(fn func(provider string) ([]ProviderModelEntry, error)) error {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	for name, p := range lb.providers {
		models, err := fn(p.Type())
		if err != nil {
			return fmt.Errorf("load provider %s models: %w", name, err)
		}
		for _, m := range models {
			if !m.Active {
				continue
			}
			lb.routes[m.Name] = append(lb.routes[m.Name], Route{
				Provider: p,
				Model: config.ModelConfig{
					Name:               m.Name,
					CostPerInputToken:  m.CostIn,
					CostPerOutputToken: m.CostOut,
				},
			})
		}
	}
	return nil
}

// GetRoutes returns routes for a specific model.
func (lb *LoadBalancer) GetRoutes(modelName string) ([]Route, error) {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	if lb.inactiveModels[modelName] {
		return nil, fmt.Errorf("model %s is deactivated", modelName)
	}

	routes, ok := lb.routes[modelName]
	if !ok || len(routes) == 0 {
		return nil, fmt.Errorf("no providers configured for model: %s", modelName)
	}

	routesCopy := make([]Route, len(routes))
	copy(routesCopy, routes)
	return routesCopy, nil
}

// GetAvailableModels returns model names that have at least one route.
func (lb *LoadBalancer) GetAvailableModels() []string {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	models := make([]string, 0, len(lb.routes))
	for model := range lb.routes {
		if !lb.inactiveModels[model] {
			models = append(models, model)
		}
	}
	return models
}

// ModelInfo describes one (model, provider) pair.
type ModelInfo struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Type     string `json:"type"`
	OwnedBy  string `json:"owned_by"`
	Active   bool   `json:"active"`
}

func (lb *LoadBalancer) GetAvailableModelInfos() []ModelInfo {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	infos := make([]ModelInfo, 0)
	for _, routes := range lb.routes {
		for _, r := range routes {
			if r.Provider == nil {
				slog.Warn("route has nil provider, skipping", "model", r.Model.Name)
				continue
			}
			infos = append(infos, ModelInfo{
				Name:     r.Model.Name,
				Provider: r.Provider.Name(),
				Type:     r.Provider.Type(),
				OwnedBy:  r.Provider.Name(),
				Active:   !lb.inactiveModels[r.Model.Name],
			})
		}
	}
	return infos
}

// ProviderStatus summarizes provider health and circuit breaker state.
type ProviderStatus struct {
	Name                string     `json:"name"`
	Type                string     `json:"type"`
	Status              string     `json:"status"`
	CircuitBreakerState string     `json:"circuit_breaker_state"`
	LastErrorTime       *time.Time `json:"last_error_time,omitempty"`
	LastSuccessTime     *time.Time `json:"last_success_time,omitempty"`
	TotalRequests       int64      `json:"total_requests"`
	TotalErrors         int64      `json:"total_errors"`
	SuccessRate         float64    `json:"success_rate"`
	APIKeysCount        int        `json:"api_keys_count"`
	APIKeySet           bool       `json:"api_key_set"`
	APIKeySource        string     `json:"api_key_source"`
}

func (lb *LoadBalancer) GetProviderStatus() []ProviderStatus {
	lb.mu.RLock()
	defer lb.mu.RUnlock()

	seen := make(map[string]bool)
	var statuses []ProviderStatus

	for _, routes := range lb.routes {
		for _, r := range routes {
			name := r.Provider.Name()
			if seen[name] {
				continue
			}
			seen[name] = true

			cbState := "unknown"
			var totalReqs, totalErrs int64
			var lastErr, lastSuc *time.Time

			client := r.Provider.Client()
			if client != nil && client.Transport != nil {
				cbState = circuitbreaker.State(client.Transport)
				totalReqs, totalErrs, lastErr, lastSuc = circuitbreaker.Metrics(client.Transport)
			}

			healthStatus := "online"
			if cbState == "open" {
				healthStatus = "offline"
			} else if cbState == "half-open" {
				healthStatus = "degraded"
			}

			successRate := 0.0
			if totalReqs > 0 {
				successRate = float64(totalReqs-totalErrs) / float64(totalReqs) * 100
			}

			apiKeySet := false
			apiKeysCount := 0
			apiKeySource := ""
			if lb.cfg != nil {
				for _, p := range lb.cfg.Providers {
					if p.Name == name {
						keys := p.GetAPIKeys()
						apiKeysCount = len(keys)
						apiKeySet = apiKeysCount > 0
						apiKeySource = p.APIKeySource
						break
					}
				}
			}

			statuses = append(statuses, ProviderStatus{
				Name:                name,
				Type:                r.Provider.Type(),
				Status:              healthStatus,
				CircuitBreakerState: cbState,
				LastErrorTime:       lastErr,
				LastSuccessTime:     lastSuc,
				TotalRequests:       totalReqs,
				TotalErrors:         totalErrs,
				SuccessRate:         successRate,
				APIKeysCount:        apiKeysCount,
				APIKeySet:           apiKeySet,
				APIKeySource:        apiKeySource,
			})
		}
	}

	return statuses
}

func (lb *LoadBalancer) SetInactiveModels(names []string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.inactiveModels = make(map[string]bool)
	for _, name := range names {
		lb.inactiveModels[name] = true
	}
}

// HasProvider checks if a provider with the given name exists.
func (lb *LoadBalancer) HasProvider(name string) bool {
	lb.mu.RLock()
	defer lb.mu.RUnlock()
	_, ok := lb.providers[name]
	return ok
}
