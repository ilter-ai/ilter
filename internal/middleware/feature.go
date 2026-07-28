package middleware

import (
	"github.com/ilter-ai/ilter/internal/config"
)

// IsEnabled returns true if the named feature is enabled in the resolved
// configuration snapshot. The snapshot already includes runtime_config overrides
// merged at boot time.
//
// Known feature names: "rate_limit", "budget", "pii", "semantic_cache", "loop_detection", "guardrails", "smart_router", "circuit_breaker", "mcp", "openapi".
// When cache is nil the function returns false.
func IsEnabled(cache *config.Cache, feature string) bool {
	if cache == nil {
		return false
	}
	snap := cache.Get()
	if snap == nil {
		return false
	}

	switch feature {
	case "rate_limit":
		return snap.RateLimit.Enabled
	case "budget":
		return snap.Budget.Enabled
	case "pii":
		return snap.PII.Enabled
	case "semantic_cache":
		return snap.CacheEnabled
	case "loop_detection":
		return snap.CostGuard.LoopDetection
	case "guardrails":
		return snap.GuardrailsEnabled
	case "smart_router":
		// Smart router is enabled when routing config is enabled
		return snap.RoutingConfig().Enabled
	case "circuit_breaker":
		// Circuit breaker is enabled per-provider when MaxFailures > 0
		for _, p := range snap.Providers() {
			if p.CircuitBreaker.MaxFailures > 0 {
				return true
			}
		}
		return false
	case "mcp":
		return snap.MCPEnabled
	case "openapi":
		return snap.OpenAPIEnabled
	default:
		return false
	}
}
