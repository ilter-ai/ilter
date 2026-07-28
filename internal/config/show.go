package config

import (
	"os"
	"strings"
	"time"
)

// Entry represents one resolved config key with source annotation.
type Entry struct {
	Key    string `json:"key"`
	Value  any    `json:"value"`
	Source string `json:"source"` // "default" | "env"
}

// envVarForKey returns the conventional ILTER_* environment variable name
// for a dotted config key (e.g. "server.port" → "ILTER_SERVER_PORT").
func envVarForKey(key string) string {
	return "ILTER_" + keyEnvName(key)
}

// keyEnvName maps a dotted config key to its uppercase env-var suffix.
// Sections with underscores are normalised: "rate_limit" → "RATE_LIMIT".
func keyEnvName(key string) string {
	return strings.NewReplacer(".", "_").Replace(strings.ToUpper(key))
}

// envSource checks whether a given ILTER_* env var is set. Returns "env" when
// set, empty string otherwise.
func envSource(key string) string {
	if _, ok := os.LookupEnv(envVarForKey(key)); ok {
		return "env"
	}
	return ""
}

// resolveSource determines the config source for a given key+value pair.
// The algorithm:
//  1. If the ILTER_* env var is set → "env"
//  2. Otherwise → "default"
func resolveSource(key string, _ any, _ any) string {
	if src := envSource(key); src != "" {
		return src
	}
	return "default"
}

// equalValues compares two values for equality, handling common config types.
// time.Duration is compared as int64 nanoseconds; bool/string/int as themselves.
func equalValues(a, b any) bool {
	switch va := a.(type) {
	case time.Duration:
		vb, ok := b.(time.Duration)
		return ok && va == vb
	case string:
		vb, ok := b.(string)
		return ok && va == vb
	case int:
		vb, ok := b.(int)
		return ok && va == vb
	case bool:
		vb, ok := b.(bool)
		return ok && va == vb
	case float64:
		vb, ok := b.(float64)
		return ok && va == vb
	default:
		return a == b
	}
}

// maskVal replaces a value with "***" for secret fields.
func maskVal(key string, val any) any {
	switch key {
	case "auth.admin_key",
		"auth.auth_token",
		"dashboard.auth_token",
		"dashboard.user_auth_jwt_secret",
		"jobs.api_key",
		"auth.admin_key_hash":
		return "***"
	}
	return val
}

// ─────────────────────────────────────────────────────────────────────
// ShowConfig — enumerates all effective config entries with source
// annotation. The cache parameter is optional (may be nil); only
// env + defaults are checked when cache is nil.
// ─────────────────────────────────────────────────────────────────────

// ShowConfig returns a JSON-encodable slice of all effective config entries.
// It applies env var overrides first, then checks envSource for each key.
func ShowConfig(cfg *Config, _ *Cache) []Entry {
	ApplyEnvOverrides(cfg)

	base := DefaultConfig()
	var entries []Entry

	add := func(key string, value any) {
		val := maskVal(key, value)
		baseline := extractDefault(key, base)
		src := resolveSource(key, value, baseline)
		entries = append(entries, Entry{Key: key, Value: val, Source: src})
	}

	// ── Server ──
	add("server.host", cfg.Server.Host)
	add("server.port", cfg.Server.Port)
	add("server.read_timeout", cfg.Server.ReadTimeout)
	add("server.write_timeout", cfg.Server.WriteTimeout)
	add("server.idle_timeout", cfg.Server.IdleTimeout)
	add("server.max_request_body", cfg.Server.MaxRequestBody)
	add("server.graceful_shutdown", cfg.Server.GracefulShutdown)

	// ── Auth ──
	add("auth.admin_key", cfg.Auth.AdminKey)
	add("auth.key_hash_algorithm", cfg.Auth.KeyHashAlgorithm)

	// ── Storage ──
	add("db.type", cfg.Storage.Type)
	add("db.sqlite_path", cfg.Storage.SqlitePath)

	// ── Logging ──
	add("logging.level", cfg.Logging.Level)
	add("logging.format", cfg.Logging.Format)
	add("logging.output", cfg.Logging.Output)
	add("logging.file_path", cfg.Logging.FilePath)

	// ── Cache ──
	add("cache.enabled", cfg.Cache.Enabled)
	add("cache.type", cfg.Cache.Type)
	add("cache.redis_url", cfg.Cache.RedisURL)
	add("cache.similarity_threshold", cfg.Cache.SimilarityThreshold)
	add("cache.ttl", cfg.Cache.TTL)
	add("cache.ollama_url", cfg.Cache.OllamaURL)
	add("cache.max_entries", cfg.Cache.MaxEntries)

	// ── Rate Limit ──
	add("rate_limit.enabled", cfg.RateLimit.Enabled)
	add("rate_limit.admin_bypass", cfg.RateLimit.AdminBypass)
	add("rate_limit.default_rpm", cfg.RateLimit.DefaultRPM)
	add("rate_limit.default_tpm", cfg.RateLimit.DefaultTPM)

	// ── Budget ──
	add("budget.enabled", cfg.Budget.Enabled)
	add("budget.default_daily_limit", cfg.Budget.DefaultDailyLimit)
	add("budget.default_monthly_limit", cfg.Budget.DefaultMonthlyLimit)
	add("budget.alert_threshold", cfg.Budget.AlertThreshold)

	// ── PII ──
	add("pii.enabled", cfg.PII.Enabled)
	add("pii.mode", cfg.PII.Mode)

	// ── Guardrails ──
	add("guardrails.enabled", cfg.Guardrails.Enabled)
	add("guardrails.mode", cfg.Guardrails.Mode)

	// ── MCP ──
	add("mcp.enabled", cfg.MCP.Enabled)
	add("mcp.endpoint", cfg.MCP.Endpoint)
	add("mcp.hub_endpoint", cfg.MCP.HubEndpoint)
	add("mcp.default_policy", cfg.MCP.DefaultPolicy)
	add("mcp.injection.enabled", cfg.MCP.Injection.Enabled)
	add("mcp.injection.default_tool_choice", cfg.MCP.Injection.DefaultToolChoice)

	// ── Dashboard ──
	add("dashboard.enabled", cfg.Dashboard.Enabled)
	add("dashboard.port", cfg.Dashboard.Port)
	add("dashboard.auth_token", cfg.Dashboard.AuthToken)
	add("dashboard.user_auth_jwt_secret", cfg.Dashboard.UserAuthJWTSecret)

	// ── Jobs ──
	add("jobs.enabled", cfg.Jobs.Enabled)
	add("jobs.api_key", cfg.Jobs.APIKey)
	add("jobs.default_billing_key_id", cfg.Jobs.DefaultBillingKeyID)
	add("jobs.proxy_url", cfg.Jobs.ProxyURL)
	add("jobs.max_concurrent_jobs", cfg.Jobs.MaxConcurrentJobs)
	add("jobs.default_timeout_ms", cfg.Jobs.DefaultTimeoutMs)
	add("jobs.redis_lock_enabled", cfg.Jobs.RedisLockEnabled)
	add("jobs.min_interval_seconds", cfg.Jobs.MinIntervalSeconds)
	add("jobs.history_max_per_job", cfg.Jobs.HistoryMaxPerJob)
	add("jobs.history_retention_days", cfg.Jobs.HistoryRetentionDays)
	add("jobs.enforce_budget", cfg.Jobs.EnforceBudget)

	// ── Metrics ──
	add("metrics.enabled", cfg.Metrics.Enabled)
	add("metrics.path", cfg.Metrics.Path)
	add("metrics.listen_addr", cfg.Metrics.ListenAddr)
	add("metrics.include_model_labels", cfg.Metrics.IncludeModelLabels)

	// ── Telemetry ──
	add("telemetry.enabled", cfg.Telemetry.Enabled)
	add("telemetry.metrics_path", cfg.Telemetry.MetricsPath)
	add("telemetry.otlp_endpoint", cfg.Telemetry.OTLPEndpoint)
	add("telemetry.trace_sampling", cfg.Telemetry.TraceSampling)

	// ── Audit ──
	add("audit.enabled", cfg.Audit.Enabled)
	add("audit.log_prompts", cfg.Audit.LogPrompts)
	add("audit.log_bodies", cfg.Audit.LogBodies)
	add("audit.retention_days", cfg.Audit.RetentionDays)

	// ── Cost Guard ──
	add("cost_guard.loop_detection", cfg.CostGuard.LoopDetection)

	// ── Headers ──
	add("headers.emit_standard", cfg.Headers.EmitStandard)

	return entries
}

// extractDefault extracts the baseline default value for a given config key
// from the DefaultConfig struct.
func extractDefault(key string, d Config) any {
	switch key {
	// ── Server ──
	case "server.host":
		return d.Server.Host
	case "server.port":
		return d.Server.Port
	case "server.read_timeout":
		return d.Server.ReadTimeout
	case "server.write_timeout":
		return d.Server.WriteTimeout
	case "server.idle_timeout":
		return d.Server.IdleTimeout
	case "server.max_request_body":
		return d.Server.MaxRequestBody
	case "server.graceful_shutdown":
		return d.Server.GracefulShutdown

	// ── Auth ──
	case "auth.admin_key":
		return d.Auth.AdminKey
	case "auth.key_hash_algorithm":
		return d.Auth.KeyHashAlgorithm
	case "auth.auth_token":
		return d.Auth.AdminKey

	// ── Storage ──
	case "db.type":
		return d.Storage.Type
	case "db.sqlite_path":
		return d.Storage.SqlitePath

	// ── Logging ──
	case "logging.level":
		return d.Logging.Level
	case "logging.format":
		return d.Logging.Format
	case "logging.output":
		return d.Logging.Output
	case "logging.file_path":
		return d.Logging.FilePath

	// ── Cache ──
	case "cache.enabled":
		return d.Cache.Enabled
	case "cache.type":
		return d.Cache.Type
	case "cache.redis_url":
		return d.Cache.RedisURL
	case "cache.similarity_threshold":
		return d.Cache.SimilarityThreshold
	case "cache.ttl":
		return d.Cache.TTL
	case "cache.ollama_url":
		return d.Cache.OllamaURL
	case "cache.max_entries":
		return d.Cache.MaxEntries

	// ── Rate Limit ──
	case "rate_limit.enabled":
		return d.RateLimit.Enabled
	case "rate_limit.admin_bypass":
		return d.RateLimit.AdminBypass
	case "rate_limit.default_rpm":
		return d.RateLimit.DefaultRPM
	case "rate_limit.default_tpm":
		return d.RateLimit.DefaultTPM
	case "rate_limit.redis_url":
		return d.RateLimit.RedisURL

	// ── Budget ──
	case "budget.enabled":
		return d.Budget.Enabled
	case "budget.default_daily_limit":
		return d.Budget.DefaultDailyLimit
	case "budget.default_monthly_limit":
		return d.Budget.DefaultMonthlyLimit
	case "budget.alert_threshold":
		return d.Budget.AlertThreshold

	// ── PII ──
	case "pii.enabled":
		return d.PII.Enabled
	case "pii.mode":
		return d.PII.Mode
	case "pii.redis_url":
		return d.PII.RedisURL

	// ── Guardrails ──
	case "guardrails.enabled":
		return d.Guardrails.Enabled
	case "guardrails.mode":
		return d.Guardrails.Mode

	// ── MCP ──
	case "mcp.enabled":
		return d.MCP.Enabled
	case "mcp.endpoint":
		return d.MCP.Endpoint
	case "mcp.hub_endpoint":
		return d.MCP.HubEndpoint
	case "mcp.default_policy":
		return d.MCP.DefaultPolicy
	case "mcp.injection.enabled":
		return d.MCP.Injection.Enabled
	case "mcp.injection.default_tool_choice":
		return d.MCP.Injection.DefaultToolChoice

	// ── Dashboard ──
	case "dashboard.enabled":
		return d.Dashboard.Enabled
	case "dashboard.port":
		return d.Dashboard.Port
	case "dashboard.auth_token":
		return d.Dashboard.AuthToken
	case "dashboard.user_auth_jwt_secret":
		return d.Dashboard.UserAuthJWTSecret

	// ── Jobs ──
	case "jobs.enabled":
		return d.Jobs.Enabled
	case "jobs.api_key":
		return d.Jobs.APIKey
	case "jobs.max_concurrent_jobs":
		return d.Jobs.MaxConcurrentJobs
	case "jobs.default_timeout_ms":
		return d.Jobs.DefaultTimeoutMs
	case "jobs.redis_lock_enabled":
		return d.Jobs.RedisLockEnabled
	case "jobs.min_interval_seconds":
		return d.Jobs.MinIntervalSeconds
	case "jobs.history_max_per_job":
		return d.Jobs.HistoryMaxPerJob
	case "jobs.history_retention_days":
		return d.Jobs.HistoryRetentionDays
	case "jobs.enforce_budget":
		return d.Jobs.EnforceBudget

	// ── Metrics ──
	case "metrics.enabled":
		return d.Metrics.Enabled
	case "metrics.path":
		return d.Metrics.Path
	case "metrics.listen_addr":
		return d.Metrics.ListenAddr
	case "metrics.include_model_labels":
		return d.Metrics.IncludeModelLabels

	// ── Telemetry ──
	case "telemetry.enabled":
		return d.Telemetry.Enabled
	case "telemetry.metrics_path":
		return d.Telemetry.MetricsPath
	case "telemetry.otlp_endpoint":
		return d.Telemetry.OTLPEndpoint
	case "telemetry.trace_sampling":
		return d.Telemetry.TraceSampling

	// ── Audit ──
	case "audit.enabled":
		return d.Audit.Enabled
	case "audit.log_prompts":
		return d.Audit.LogPrompts
	case "audit.log_bodies":
		return d.Audit.LogBodies
	case "audit.retention_days":
		return d.Audit.RetentionDays

	// ── Cost Guard ──
	case "cost_guard.loop_detection":
		return d.CostGuard.LoopDetection

	// ── Headers ──
	case "headers.emit_standard":
		return d.Headers.EmitStandard

	default:
		return nil
	}
}

// ─────────────────────────────────────────────────────────────────────
// Public env-var helpers (useful for CLI flag binding)
// ─────────────────────────────────────────────────────────────────────

// EnvVarForKey returns the conventional ILTER_* env var name for a key path.
func EnvVarForKey(key string) string {
	return envVarForKey(key)
}
