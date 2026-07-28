package config

// IsEnabled checks whether a named feature is enabled via the resolved
// configuration snapshot. It reads the field corresponding to the feature
// directly from the snapshot, which already includes runtime_config overrides.
func IsEnabled(cache *Cache, feature string) bool {
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
		return snap.Routing.Enabled
	case "mcp":
		return snap.MCPEnabled
	case "openapi":
		return snap.OpenAPIEnabled
	}
	return false
}
