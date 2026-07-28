// Package model defines domain types for the ilter AI gateway.
package model

import (
	"fmt"
	"slices"
)

// ─────────────────────────────────────────────────────────────────────
// Routing types — persisted in the runtime_config store as JSON.
// Each strategy is stored under section=routing_strategy, key=<name>.
// A separate key routing_config:active_strategy holds the name of
// the currently active strategy.
// ─────────────────────────────────────────────────────────────────────

// ValidProviderPreferences lists every recognized provider-preference mode.
var ValidProviderPreferences = []string{
	"cheapest",
	"round-robin",
}

// ValidLBStrategies lists every recognized load balancer strategy name.
var ValidLBStrategies = []string{
	"weighted-random",
	"round-robin",
	"cost-optimized",
	"latency-optimized",
	"priority-based",
}

// ValidScorerTypes lists every recognized scorer engine type.
var ValidScorerTypes = []string{
	"heuristic",
	"llm",
	"embedding",
	"trainable",
}

// ComplexityThresholds holds the complexity-boundary values used by
// the smart routing strategy to classify requests into economy,
// standard, or premium tiers.
type ComplexityThresholds struct {
	Economy  float64 `json:"economy"`
	Standard float64 `json:"standard"`
}

// ScorerConfig configures the scoring engine for smart routing.
type ScorerConfig struct {
	Type      string                 `json:"type"`                // "heuristic" | "llm" | "embedding" | "trainable"
	LLM       *LLMScorerConfig       `json:"llm,omitempty"`       // present when Type == "llm"
	Embedding *EmbeddingScorerConfig `json:"embedding,omitempty"` // present when Type == "embedding"
	Trainable *TrainableScorerConfig `json:"trainable,omitempty"` // present when Type == "trainable"
}

// LLMScorerConfig configures an LLM-based complexity scorer.
type LLMScorerConfig struct {
	Model           string `json:"model"`
	Provider        string `json:"provider"`
	CacheTTL        string `json:"cache_ttl"` // duration string, e.g. "5m"
	CacheMaxEntries int    `json:"cache_max_entries"`
	Timeout         string `json:"timeout"` // duration string, e.g. "10s"
}

// EmbeddingScorerConfig configures an embedding-based complexity scorer.
type EmbeddingScorerConfig struct {
	Model               string  `json:"model"`
	Dimensions          int     `json:"dimensions"`
	ReferenceCount      int     `json:"reference_count"`
	SimilarityThreshold float64 `json:"similarity_threshold"`
}

// TrainableScorerConfig configures a trainable (ML-model) complexity scorer.
type TrainableScorerConfig struct {
	ModelPath       string `json:"model_path"`
	FeatureVersion  int    `json:"feature_version"`
	FallbackOnError bool   `json:"fallback_on_error"`
}

// RoutingRule is a single rule inside a routing strategy.
type RoutingRule struct {
	Name        string `json:"name"`
	Condition   string `json:"condition"`
	TargetModel string `json:"target_model"`
	Priority    int    `json:"priority"`
	Enabled     bool   `json:"enabled"`
}

// RoutingStrategy is a named group of routing rules with a provider preference.
// Persisted as JSON in runtime_config under section=routing_strategy, key=<name>.
type RoutingStrategy struct {
	Name                 string               `json:"name"`
	Description          string               `json:"description"`
	Enabled              bool                 `json:"enabled"`
	ProviderPreference   string               `json:"provider_preference"`
	LoadBalancerStrategy string               `json:"load_balancer_strategy"`
	Scorer               ScorerConfig         `json:"scorer"`
	ComplexityThresholds ComplexityThresholds `json:"complexity_thresholds"`
	Rules                []RoutingRule        `json:"rules"`
}

// Validate checks that the strategy has a valid provider preference, load balancer
// strategy, and scorer type.
func (rs *RoutingStrategy) Validate() error {
	if rs.ProviderPreference != "" && !slices.Contains(ValidProviderPreferences, rs.ProviderPreference) {
		return fmt.Errorf("invalid provider preference %q: must be one of %v", rs.ProviderPreference, ValidProviderPreferences)
	}
	if rs.LoadBalancerStrategy != "" && !slices.Contains(ValidLBStrategies, rs.LoadBalancerStrategy) {
		return fmt.Errorf("invalid load balancer strategy %q: must be one of %v", rs.LoadBalancerStrategy, ValidLBStrategies)
	}
	if rs.Scorer.Type != "" && !slices.Contains(ValidScorerTypes, rs.Scorer.Type) {
		return fmt.Errorf("invalid scorer type %q: must be one of %v", rs.Scorer.Type, ValidScorerTypes)
	}
	for _, r := range rs.Rules {
		if r.Name == "" {
			return fmt.Errorf("rule name is required")
		}
		if r.TargetModel == "" {
			return fmt.Errorf("rule %q: target_model is required", r.Name)
		}
	}
	return nil
}

// KnownFeatureFlags enumerates every recognized feature flag name.
var KnownFeatureFlags = []string{
	"rate_limit",
	"pii",
	"semantic_cache",
	"budget",
	"loop_detection",
	"guardrails",
	"smart_router",
	"circuit_breaker",
	"mcp",
	"openapi",
}

// FeatureFlag represents a single persisted feature flag.
type FeatureFlag struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// Validate checks that the feature flag has a recognized name.
func (f *FeatureFlag) Validate() error {
	if f.Name == "" {
		return fmt.Errorf("feature flag name is required")
	}
	if !slices.Contains(KnownFeatureFlags, f.Name) {
		return fmt.Errorf("unknown feature flag %q: must be one of %v", f.Name, KnownFeatureFlags)
	}
	return nil
}
