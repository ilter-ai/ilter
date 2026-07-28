package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ─────────────────────────────────────────────────────────────────────
// StateConfig carries runtime configuration sourced from the database
// (or any stateful backend). It holds provider definitions, MCP server
// registrations, guardrail rules, routing strategy, feature flags, and
// OpenAPI tool specs.
//
// In addition to state-only sections, StateConfig includes override
// pointers for fields whose boot-time default can be superseded at
// runtime (e.g., cache.similarity_threshold, audit.retention_days).
// A nil pointer means "use the boot default."
// ─────────────────────────────────────────────────────────────────────
type StateConfig struct {
	// ── State-only sections ──
	Providers    []ProviderConfig    `json:"providers"`
	MCPServers   []MCPServerConfig   `json:"mcp_servers"`
	MCPAccess    []MCPAccessRule     `json:"mcp_access"`
	GuardRules   []string            `json:"guard_rules"` // rule-set names
	CustomRules  []CustomRuleConfig  `json:"custom_rules"`
	Routing      RoutingConfig       `json:"routing"`
	OpenAPITools []OpenAPISpecConfig `json:"openapi_tools"`

	// ── Override pointers ──
	// When non-nil, these values supersede the corresponding BootConfig
	// defaults at runtime. Nil means "keep the boot default."

	CacheSimilarityThreshold *float64 `json:"cache_similarity_threshold,omitempty"`
	AuditRetentionDays       *int64   `json:"audit_retention_days,omitempty"`
	GuardrailsMode           *string  `json:"guardrails_mode,omitempty"`

	// ── Generic runtime_config values ──
	// Raw key-value pairs loaded from the runtime_config table for entries
	// not covered by a specialised store.  The map key is "section:key".
	// Values are validated against the schema registry (see schema.go)
	// before being written by the admin API.
	RuntimeConfigValues map[string]string `json:"runtime_config_values,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────
// RuntimeConfigSnapshot is the fully resolved, flat configuration
// produced by merging a BootConfig with a StateConfig. Every field
// is a concrete value — no Option types, no override pointers.
//
// Middleware and handlers read from this struct directly. It is
// designed to be embedded in a sync.RWMutex-protected runtime value
// that can be atomically swapped on state reload.
// ─────────────────────────────────────────────────────────────────────
type RuntimeConfigSnapshot struct {
	// ── Boot pass-through (no state override) ──
	Server    ServerConfig
	Auth      AuthConfig
	Storage   StorageConfig
	Logging   LoggingConfig
	Metrics   MetricsConfig
	Telemetry TelemetryConfig
	Headers   HeadersConfig
	RateLimit RateLimitConfig
	Budget    BudgetConfig
	CostGuard CostGuardConfig
	Dashboard DashboardConfig
	PII       PIIConfig
	Fallback  FallbackConfig

	// ── Resolved overrides (Boot default, overridden by State if set) ──
	CacheType                string
	CacheRedisURL            string
	CacheSimilarityThreshold float64
	CacheEnabled             bool

	AuditEnabled       bool
	AuditLogPrompts    bool
	AuditLogBodies     bool
	AuditRetentionDays int64

	GuardrailsEnabled       bool
	GuardrailsMode          string
	GuardrailsModerationAPI ModerationAPIConfig

	MCPEnabled       bool
	MCPEndpoint      string
	MCPDefaultPolicy string
	MCPHubEndpoint   string
	OpenAPIEnabled   bool

	Jobs JobsConfig

	// ── From State ──
	Providers    []ProviderConfig
	MCPServers   []MCPServerConfig
	MCPAccess    []MCPAccessRule
	GuardRules   []string
	CustomRules  []CustomRuleConfig
	Routing      RoutingConfig
	OpenAPITools []OpenAPISpecConfig
}

// ─────────────────────────────────────────────────────────────────────
// ResolveRuntime merges a BootConfig with a StateConfig into a single
// RuntimeConfigSnapshot. The boot config provides defaults; any non-nil
// override pointer in the state config supersedes the corresponding
// boot value.
//
// Callers obtain a fresh snapshot whenever the runtime state changes.
// The returned value is intended to be stored behind a sync.RWMutex
// and swapped atomically.
// ─────────────────────────────────────────────────────────────────────
func ResolveRuntime(boot *BootConfig, state *StateConfig) *RuntimeConfigSnapshot {
	snap := &RuntimeConfigSnapshot{
		Server:    boot.Server,
		Auth:      boot.Auth,
		Storage:   boot.Storage,
		Logging:   boot.Logging,
		Metrics:   boot.Metrics,
		Telemetry: boot.Telemetry,
		Headers:   boot.Headers,
		RateLimit: boot.RateLimit,
		Budget:    boot.Budget,
		CostGuard: boot.CostGuard,
		Dashboard: boot.Dashboard,
		PII:       boot.PII,
		Fallback:  boot.Fallback,
		Routing:   boot.Routing,

		CacheType:     boot.Cache.Type,
		CacheRedisURL: boot.Cache.RedisURL,
		CacheEnabled:  boot.Cache.Enabled,
		CacheSimilarityThreshold: func() float64 {
			if v, ok := boot.Cache.SimilarityThreshold.Value(); ok {
				return v
			}
			return 0.70
		}(),

		AuditEnabled:    boot.Audit.Enabled,
		AuditLogPrompts: boot.Audit.LogPrompts,
		AuditLogBodies:  boot.Audit.LogBodies,
		AuditRetentionDays: func() int64 {
			if v, ok := boot.Audit.RetentionDays.Value(); ok {
				return v
			}
			return DefaultRetentionDays
		}(),

		GuardrailsEnabled:       boot.Guardrails.Enabled,
		GuardrailsMode:          boot.Guardrails.Mode,
		GuardrailsModerationAPI: boot.Guardrails.ModerationAPI,

		MCPEnabled:       boot.MCP.Enabled,
		MCPEndpoint:      boot.MCP.Endpoint,
		MCPDefaultPolicy: boot.MCP.DefaultPolicy,
		MCPHubEndpoint:   boot.MCP.HubEndpoint,
		OpenAPIEnabled:   true,

		Jobs: boot.Jobs,
	}

	if state != nil {
		snap.Providers = state.Providers
		snap.MCPServers = state.MCPServers
		snap.MCPAccess = state.MCPAccess
		snap.GuardRules = state.GuardRules
		snap.CustomRules = state.CustomRules
		snap.Routing = state.Routing
		snap.OpenAPITools = state.OpenAPITools

		// Generic runtime_config value overrides (parsed from stringly-typed
		// DB rows).  These provide the baseline for config keys that do not
		// have a typed pointer equivalent.
		if len(state.RuntimeConfigValues) > 0 {
			mergeRuntimeConfigValues(snap, state.RuntimeConfigValues)
		}

		if state.CacheSimilarityThreshold != nil {
			snap.CacheSimilarityThreshold = *state.CacheSimilarityThreshold
		}
		if state.AuditRetentionDays != nil {
			snap.AuditRetentionDays = *state.AuditRetentionDays
		}
		if state.GuardrailsMode != nil {
			snap.GuardrailsMode = *state.GuardrailsMode
		}
	}

	return snap
}

// ─────────────────────────────────────────────────────────────────────
// mergeRuntimeConfigValues applies generic runtime_config entries to the
// snapshot.  Each entry's (section:key) is looked up in the schema modelregistry
// (schema.go); if recognized, the string value is parsed to the declared
// type and written to the matching snapshot field.
//
// Unrecognized keys are silently ignored — they may be consumed by
// middleware or other subsystems that read the raw values independently.
// ─────────────────────────────────────────────────────────────────────

func mergeRuntimeConfigValues(snap *RuntimeConfigSnapshot, values map[string]string) {
	if v, ok := values["fallback:enabled"]; ok {
		if parsed, ok := parseBool(v); ok {
			snap.Fallback.Enabled = parsed
		}
	}
	if v, ok := values["fallback:cooldown_duration"]; ok && v != "" {
		if dur, err := time.ParseDuration(v); err == nil {
			snap.Fallback.CooldownDuration = dur
		}
	}
	if v, ok := values["fallback:model_downgrade"]; ok && v != "" {
		snap.Fallback.ModelDowngrade = v
	}
	if v, ok := values["fallback:max_attempts"]; ok {
		if n, err := strconv.Atoi(v); err == nil {
			snap.Fallback.MaxAttempts = n
		}
	}
	if v, ok := values["fallback:allowed_models"]; ok && v != "" {
		parts := strings.Split(v, ",")
		cleaned := make([]string, 0, len(parts))
		for _, p := range parts {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				cleaned = append(cleaned, trimmed)
			}
		}
		snap.Fallback.AllowedModels = cleaned
	}

	if v, ok := values["cache:similarity_threshold"]; ok {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil {
			snap.CacheSimilarityThreshold = parsed
		}
	}

	if v, ok := values["audit:retention_days"]; ok {
		if parsed, err := strconv.ParseInt(v, 10, 64); err == nil {
			snap.AuditRetentionDays = parsed
		}
	}

	if v, ok := values["guardrails:mode"]; ok && v != "" {
		snap.GuardrailsMode = v
	}
	if v, ok := values["pii:mode"]; ok && v != "" {
		snap.PII.Mode = v
	}
	if v, ok := values["dashboard:port"]; ok {
		if parsed, err := strconv.Atoi(v); err == nil {
			snap.Dashboard.Port = parsed
		}
	}
	if v, ok := values["metrics:port"]; ok {
		if parsed, err := strconv.Atoi(v); err == nil {
			snap.Metrics.ListenAddr = fmt.Sprintf("0.0.0.0:%d", parsed)
		}
	}

	// feature:<key> entries toggle typed feature enabled/disabled fields.
	// Unlike the old feature_flag section, these override the boot default
	// directly into the snapshot's typed field — no separate lookup required.
	featureBools := map[string]*bool{
		"feature:rate_limit":     &snap.RateLimit.Enabled,
		"feature:budget":         &snap.Budget.Enabled,
		"feature:pii":            &snap.PII.Enabled,
		"feature:loop_detection": &snap.CostGuard.LoopDetection,
		"feature:guardrails":     &snap.GuardrailsEnabled,
		"feature:mcp":            &snap.MCPEnabled,
		"feature:openapi":        &snap.OpenAPIEnabled,
	}
	for sectionKey, ptr := range featureBools {
		if v, ok := values[sectionKey]; ok {
			if parsed, ok := parseBool(v); ok {
				*ptr = parsed
			}
		}
	}
	if v, ok := values["feature:smart_router"]; ok {
		if parsed, ok := parseBool(v); ok {
			snap.Routing.Enabled = parsed
		}
	}
	if v, ok := values["feature:semantic_cache"]; ok {
		if parsed, ok := parseBool(v); ok {
			snap.CacheEnabled = parsed
		}
	}
}

// parseBool wraps strconv.ParseBool with the same (value, ok) signature.
func parseBool(s string) (bool, bool) {
	b, err := strconv.ParseBool(s)
	return b, err == nil
}
