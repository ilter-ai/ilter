package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// EnvVarForKey
// ─────────────────────────────────────────────────────────────────────

func TestEnvVarForKey(t *testing.T) {
	tests := []struct {
		key      string
		expected string
	}{
		// Server
		{"server.port", "ILTER_SERVER_PORT"},
		{"server.host", "ILTER_SERVER_HOST"},
		{"server.read_timeout", "ILTER_SERVER_READ_TIMEOUT"},
		{"server.write_timeout", "ILTER_SERVER_WRITE_TIMEOUT"},
		{"server.idle_timeout", "ILTER_SERVER_IDLE_TIMEOUT"},
		{"server.max_request_body", "ILTER_SERVER_MAX_REQUEST_BODY"},
		{"server.graceful_shutdown", "ILTER_SERVER_GRACEFUL_SHUTDOWN"},
		// Auth
		{"auth.admin_key", "ILTER_AUTH_ADMIN_KEY"},
		{"auth.key_hash_algorithm", "ILTER_AUTH_KEY_HASH_ALGORITHM"},
		// Storage
		{"db.type", "ILTER_DB_TYPE"},
		{"db.sqlite_path", "ILTER_DB_SQLITE_PATH"},
		// Logging
		{"logging.level", "ILTER_LOGGING_LEVEL"},
		{"logging.format", "ILTER_LOGGING_FORMAT"},
		{"logging.output", "ILTER_LOGGING_OUTPUT"},
		{"logging.file_path", "ILTER_LOGGING_FILE_PATH"},
		// Cache
		{"cache.enabled", "ILTER_CACHE_ENABLED"},
		{"cache.type", "ILTER_CACHE_TYPE"},
		{"cache.redis_url", "ILTER_CACHE_REDIS_URL"},
		{"cache.similarity_threshold", "ILTER_CACHE_SIMILARITY_THRESHOLD"},
		{"cache.ttl", "ILTER_CACHE_TTL"},
		{"cache.ollama_url", "ILTER_CACHE_OLLAMA_URL"},
		{"cache.max_entries", "ILTER_CACHE_MAX_ENTRIES"},
		// Rate limit
		{"rate_limit.enabled", "ILTER_RATE_LIMIT_ENABLED"},
		{"rate_limit.admin_bypass", "ILTER_RATE_LIMIT_ADMIN_BYPASS"},
		{"rate_limit.default_rpm", "ILTER_RATE_LIMIT_DEFAULT_RPM"},
		{"rate_limit.default_tpm", "ILTER_RATE_LIMIT_DEFAULT_TPM"},
		// Budget
		{"budget.enabled", "ILTER_BUDGET_ENABLED"},
		{"budget.default_daily_limit", "ILTER_BUDGET_DEFAULT_DAILY_LIMIT"},
		{"budget.default_monthly_limit", "ILTER_BUDGET_DEFAULT_MONTHLY_LIMIT"},
		{"budget.alert_threshold", "ILTER_BUDGET_ALERT_THRESHOLD"},
		// PII
		{"pii.enabled", "ILTER_PII_ENABLED"},
		{"pii.mode", "ILTER_PII_MODE"},
		{"guardrails.enabled", "ILTER_GUARDRAILS_ENABLED"},
		{"guardrails.mode", "ILTER_GUARDRAILS_MODE"},
		// MCP
		{"mcp.enabled", "ILTER_MCP_ENABLED"},
		{"mcp.endpoint", "ILTER_MCP_ENDPOINT"},
		{"mcp.hub_endpoint", "ILTER_MCP_HUB_ENDPOINT"},
		{"mcp.default_policy", "ILTER_MCP_DEFAULT_POLICY"},
		{"mcp.injection.enabled", "ILTER_MCP_INJECTION_ENABLED"},
		{"mcp.injection.default_tool_choice", "ILTER_MCP_INJECTION_DEFAULT_TOOL_CHOICE"},
		{"dashboard.enabled", "ILTER_DASHBOARD_ENABLED"},
		{"dashboard.port", "ILTER_DASHBOARD_PORT"},
		{"dashboard.auth_token", "ILTER_DASHBOARD_AUTH_TOKEN"},
		{"dashboard.user_auth_jwt_secret", "ILTER_DASHBOARD_USER_AUTH_JWT_SECRET"},
		// Jobs
		{"jobs.enabled", "ILTER_JOBS_ENABLED"},
		{"jobs.api_key", "ILTER_JOBS_API_KEY"},
		{"jobs.default_billing_key_id", "ILTER_JOBS_DEFAULT_BILLING_KEY_ID"},
		{"jobs.proxy_url", "ILTER_JOBS_PROXY_URL"},
		{"jobs.max_concurrent_jobs", "ILTER_JOBS_MAX_CONCURRENT_JOBS"},
		{"jobs.default_timeout_ms", "ILTER_JOBS_DEFAULT_TIMEOUT_MS"},
		{"jobs.redis_lock_enabled", "ILTER_JOBS_REDIS_LOCK_ENABLED"},
		{"jobs.min_interval_seconds", "ILTER_JOBS_MIN_INTERVAL_SECONDS"},
		{"jobs.history_max_per_job", "ILTER_JOBS_HISTORY_MAX_PER_JOB"},
		{"jobs.history_retention_days", "ILTER_JOBS_HISTORY_RETENTION_DAYS"},
		{"jobs.enforce_budget", "ILTER_JOBS_ENFORCE_BUDGET"},
		// Metrics
		{"metrics.enabled", "ILTER_METRICS_ENABLED"},
		{"metrics.path", "ILTER_METRICS_PATH"},
		{"metrics.listen_addr", "ILTER_METRICS_LISTEN_ADDR"},
		{"metrics.include_model_labels", "ILTER_METRICS_INCLUDE_MODEL_LABELS"},
		// Telemetry
		{"telemetry.enabled", "ILTER_TELEMETRY_ENABLED"},
		{"telemetry.metrics_path", "ILTER_TELEMETRY_METRICS_PATH"},
		{"telemetry.otlp_endpoint", "ILTER_TELEMETRY_OTLP_ENDPOINT"},
		{"telemetry.trace_sampling", "ILTER_TELEMETRY_TRACE_SAMPLING"},
		// Audit
		{"audit.enabled", "ILTER_AUDIT_ENABLED"},
		{"audit.log_prompts", "ILTER_AUDIT_LOG_PROMPTS"},
		{"audit.log_bodies", "ILTER_AUDIT_LOG_BODIES"},
		{"audit.retention_days", "ILTER_AUDIT_RETENTION_DAYS"},
		// Cost Guard
		{"cost_guard.loop_detection", "ILTER_COST_GUARD_LOOP_DETECTION"},
		// Headers
		{"headers.emit_standard", "ILTER_HEADERS_EMIT_STANDARD"},
		// Unknown keys
		{"nonexistent.key", "ILTER_NONEXISTENT_KEY"},
		{"foo.bar.baz", "ILTER_FOO_BAR_BAZ"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			got := EnvVarForKey(tt.key)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestEnvVarForKey_UnknownKey(t *testing.T) {
	// Should not panic.
	result := EnvVarForKey("totally.unknown.key")
	assert.Equal(t, "ILTER_TOTALLY_UNKNOWN_KEY", result)

	result = EnvVarForKey("a.b.c")
	assert.Equal(t, "ILTER_A_B_C", result)
}

// ─────────────────────────────────────────────────────────────────────
// expectedShowKeys — source-of-truth list of every key ShowConfig emits.
// Keep in sync with show.go.
// ─────────────────────────────────────────────────────────────────────

// countShowKeys returns the number of expected keys.
func countShowKeys() int {
	return len(expectedShowKeys)
}

var expectedShowKeys = []string{
	// Server
	"server.host", "server.port", "server.read_timeout", "server.write_timeout",
	"server.idle_timeout", "server.max_request_body", "server.graceful_shutdown",
	// Auth
	"auth.admin_key", "auth.key_hash_algorithm",
	// Storage
	"db.type", "db.sqlite_path",
	// Logging
	"logging.level", "logging.format", "logging.output", "logging.file_path",
	// Cache
	"cache.enabled", "cache.type", "cache.redis_url", "cache.similarity_threshold",
	"cache.ttl", "cache.ollama_url", "cache.max_entries",
	// Rate Limit
	"rate_limit.enabled", "rate_limit.admin_bypass", "rate_limit.default_rpm",
	"rate_limit.default_tpm",
	// Budget
	"budget.enabled", "budget.default_daily_limit", "budget.default_monthly_limit",
	"budget.alert_threshold",
	// PII
	"pii.enabled", "pii.mode",
	// Guardrails
	"guardrails.enabled", "guardrails.mode",
	// MCP
	"mcp.enabled", "mcp.endpoint", "mcp.hub_endpoint", "mcp.default_policy",
	"mcp.injection.enabled", "mcp.injection.default_tool_choice",
	// Dashboard
	"dashboard.enabled", "dashboard.port", "dashboard.auth_token", "dashboard.user_auth_jwt_secret",
	// Jobs
	"jobs.enabled", "jobs.api_key", "jobs.default_billing_key_id", "jobs.proxy_url",
	"jobs.max_concurrent_jobs", "jobs.default_timeout_ms", "jobs.redis_lock_enabled",
	"jobs.min_interval_seconds", "jobs.history_max_per_job", "jobs.history_retention_days",
	"jobs.enforce_budget",
	// Metrics
	"metrics.enabled", "metrics.path", "metrics.listen_addr", "metrics.include_model_labels",
	// Telemetry
	"telemetry.enabled", "telemetry.metrics_path", "telemetry.otlp_endpoint", "telemetry.trace_sampling",
	// Audit
	"audit.enabled", "audit.log_prompts", "audit.log_bodies", "audit.retention_days",
	// Cost Guard
	"cost_guard.loop_detection",
	// Headers
	"headers.emit_standard",
}

// ─────────────────────────────────────────────────────────────────────
// ShowConfig — all keys present in correct order
// ─────────────────────────────────────────────────────────────────────

func TestShowConfig_AllKeysPresent(t *testing.T) {
	resetForTest()
	cfg := DefaultConfig()
	entries := ShowConfig(&cfg, nil)

	require.Len(t, entries, countShowKeys(),
		"ShowConfig should return %d entries (check show.go for drift)", countShowKeys())

	got := make([]string, len(entries))
	for i, e := range entries {
		got[i] = e.Key
	}

	for i, key := range expectedShowKeys {
		assert.Equal(t, key, got[i],
			"key at position %d should match (check show.go ordering)", i)
	}
}

func TestShowConfig_NilCache(t *testing.T) {
	resetForTest()
	cfg := DefaultConfig()

	// Should not panic with nil cache.
	entries := ShowConfig(&cfg, nil)
	require.NotNil(t, entries)
	require.Len(t, entries, countShowKeys())

	got := make(map[string]bool, len(entries))
	for _, e := range entries {
		got[e.Key] = true
	}
	for _, key := range expectedShowKeys {
		assert.True(t, got[key], "key %q should be present with nil cache", key)
	}
}

// ─────────────────────────────────────────────────────────────────────
// Source detection — defaults
// ─────────────────────────────────────────────────────────────────────

func TestShowConfig_DefaultSource(t *testing.T) {
	resetForTest()
	cfg := DefaultConfig()
	entries := ShowConfig(&cfg, nil)

	for _, e := range entries {
		assert.Equal(t, "default", e.Source,
			"key %q should have source 'default' with DefaultConfig", e.Key)
	}
}

// ─────────────────────────────────────────────────────────────────────
// Source detection — env
//
// NOTE: Some env var names differ between envVarForKey (derived from
// config key path) and the EnvVar registry (hard-coded in env.go).
// For example:
//   - envVarForKey("logging.level") => "ILTER_LOGGING_LEVEL"
//   - EnvVar registry:              => "ILTER_LOG_LEVEL"
// These tests only use env vars where both systems agree.
// ─────────────────────────────────────────────────────────────────────

func TestShowConfig_EnvSource(t *testing.T) {
	resetForTest()

	// Set env vars that match envVarForKey.
	t.Setenv("ILTER_SERVER_PORT", "9090")

	cfg := DefaultConfig()
	entries := ShowConfig(&cfg, nil)

	byKey := make(map[string]Entry, len(entries))
	for _, e := range entries {
		byKey[e.Key] = e
	}

	t.Run("server.port source and value", func(t *testing.T) {
		e, ok := byKey["server.port"]
		require.True(t, ok)
		assert.Equal(t, "env", e.Source)
		assert.Equal(t, 9090, e.Value)
	})

	t.Run("dashboard.port remains default", func(t *testing.T) {
		e, ok := byKey["dashboard.port"]
		require.True(t, ok)
		assert.Equal(t, "default", e.Source,
			"dashboard.port has no env var — must be sourced from default")
	})

	t.Run("unset fields remain default", func(t *testing.T) {
		e, ok := byKey["server.host"]
		require.True(t, ok)
		assert.Equal(t, "default", e.Source,
			"server.host should remain 'default' when env is not set")
		assert.Equal(t, "0.0.0.0", e.Value)
	})
}

func TestShowConfig_EnvSourceOnlyWhenSet(t *testing.T) {
	resetForTest()

	// Set only a single env var — all other entries remain default.
	t.Setenv("ILTER_SERVER_PORT", "7070")

	cfg := DefaultConfig()
	entries := ShowConfig(&cfg, nil)

	for _, e := range entries {
		switch e.Key {
		case "server.port":
			assert.Equal(t, "env", e.Source, "server.port should be 'env'")
		default:
			assert.Equal(t, "default", e.Source,
				"key %q should remain 'default' when only ILTER_SERVER_PORT is set", e.Key)
		}
	}
}

// envSourceKey derives the env var name that envSource() checks for a key.
// We use this to set env vars that envSource (not ApplyEnvOverrides) detects.
func envSourceKey(configKey string) string {
	return envVarForKey(configKey)
}

// ─────────────────────────────────────────────────────────────────────
// Source detection — envSource uses derived env var names
// ─────────────────────────────────────────────────────────────────────

func TestShowConfig_EnvSourceViaDerivedName(t *testing.T) {
	resetForTest()

	// Set the derived env var name that envSource() checks.
	// This differs from the EnvVar registry name for some keys.
	// Example: envVarForKey("logging.level") = "ILTER_LOGGING_LEVEL"
	//          EnvVar registry:              = "ILTER_LOG_LEVEL"
	// Here we set the derived name so envSource finds it.
	derived := envSourceKey("logging.level")
	assert.Equal(t, "ILTER_LOGGING_LEVEL", derived)
	t.Setenv(derived, "debug")

	cfg := DefaultConfig()
	entries := ShowConfig(&cfg, nil)

	byKey := make(map[string]Entry, len(entries))
	for _, e := range entries {
		byKey[e.Key] = e
	}

	t.Run("logging.level source is env", func(t *testing.T) {
		e, ok := byKey["logging.level"]
		require.True(t, ok)
		// envSource checks ILTER_LOGGING_LEVEL (which is set)
		assert.Equal(t, "env", e.Source)
		// ApplyEnvOverrides checks ILTER_LOG_LEVEL (NOT set),
		// so the value stays at default ("info").
		assert.Equal(t, "info", e.Value,
			"value is default because ILTER_LOG_LEVEL (registry name) was not set")
	})
}

// ─────────────────────────────────────────────────────────────────────
// Secret masking
// ─────────────────────────────────────────────────────────────────────

func TestShowConfig_SecretMasking(t *testing.T) {
	resetForTest()

	cfg := DefaultConfig()
	cfg.Auth.AdminKey = "real-admin-key"
	cfg.Dashboard.AuthToken = "real-dash-token"
	cfg.Dashboard.UserAuthJWTSecret = "real-jwt-secret"
	cfg.Jobs.APIKey = "real-job-key"

	entries := ShowConfig(&cfg, nil)

	byKey := make(map[string]Entry, len(entries))
	for _, e := range entries {
		byKey[e.Key] = e
	}

	// Keys that ShowConfig emits AND maskVal masks:
	secretEmittedKeys := map[string]string{
		"auth.admin_key":                 "real-admin-key",
		"dashboard.auth_token":           "real-dash-token",
		"dashboard.user_auth_jwt_secret": "real-jwt-secret",
		"jobs.api_key":                   "real-job-key",
	}

	for secretKey := range secretEmittedKeys {
		t.Run(secretKey+" is masked", func(t *testing.T) {
			e, ok := byKey[secretKey]
			if !ok {
				t.Skipf("%q is not emitted by ShowConfig yet", secretKey)
			}
			assert.Equal(t, "***", e.Value,
				"secret key %q should be masked", secretKey)
		})
	}

	t.Run("non-secret keys are not masked", func(t *testing.T) {
		for key, e := range byKey {
			if _, isSecret := secretEmittedKeys[key]; !isSecret {
				assert.NotEqual(t, "***", e.Value,
					"non-secret key %q should not be masked", key)
			}
		}
	})
}

func TestShowConfig_SecretMaskingSetViaEnv(t *testing.T) {
	resetForTest()

	// Set the EnvVar registry names (ApplyEnvOverrides checks these).
	t.Setenv("ILTER_ADMIN_API_KEY", "env-admin-value")
	t.Setenv("ILTER_DASHBOARD_TOKEN", "env-dash-value")

	cfg := DefaultConfig()
	entries := ShowConfig(&cfg, nil)

	byKey := make(map[string]Entry, len(entries))
	for _, e := range entries {
		byKey[e.Key] = e
	}

	t.Run("auth.admin_key masked when set via env", func(t *testing.T) {
		e := byKey["auth.admin_key"]
		require.NotNil(t, e)
		assert.Equal(t, "***", e.Value)
		// ApplyEnvOverrides uses ILTER_ADMIN_API_KEY (registry) which is set,
		// so cfg.Auth.AdminKey is populated. Then maskVal masks it.
		// envSource checks ILTER_AUTH_ADMIN_KEY (derived) which is NOT set,
		// so source resolves by comparing value to default. Since the
		// default is "" and the value is "env-admin-value" (from ApplyEnvOverrides),
		// the source is "default" (value differs from default, no matching env source).
	})

	t.Run("dashboard.auth_token masked when set via env", func(t *testing.T) {
		e := byKey["dashboard.auth_token"]
		require.NotNil(t, e)
		assert.Equal(t, "***", e.Value)
	})
}

// ─────────────────────────────────────────────────────────────────────
// Secret masking — verify maskVal directly for future-proofing
// ─────────────────────────────────────────────────────────────────────

func TestMaskVal(t *testing.T) {
	secrets := []string{
		"auth.admin_key",
		"auth.auth_token",
		"auth.admin_key_hash",
		"dashboard.auth_token",
		"dashboard.user_auth_jwt_secret",
		"jobs.api_key",
	}

	for _, key := range secrets {
		t.Run(key, func(t *testing.T) {
			got := maskVal(key, "sensitive-value")
			assert.Equal(t, "***", got, "maskVal should mask %q", key)
		})
	}

	t.Run("non-secret key not masked", func(t *testing.T) {
		got := maskVal("server.port", 8181)
		assert.Equal(t, 8181, got, "non-secret values should pass through")
	})
}

// ─────────────────────────────────────────────────────────────────────
// Value types — all values are JSON-serialisable primitives
// ─────────────────────────────────────────────────────────────────────

// durationType is a helper to create a typed zero for type assertions.
func durationType(d time.Duration) time.Duration { return d }

func TestShowConfig_ValueTypes(t *testing.T) {
	resetForTest()
	cfg := DefaultConfig()
	entries := ShowConfig(&cfg, nil)

	byKey := make(map[string]Entry, len(entries))
	for _, e := range entries {
		byKey[e.Key] = e
	}

	typeCheck := func(key string, expectedType any) {
		e, ok := byKey[key]
		if !ok {
			t.Errorf("key %q not found in ShowConfig output", key)
			return
		}
		assert.IsType(t, expectedType, e.Value, "key %q", key)
	}

	// String values
	for _, k := range []string{
		"server.host", "server.max_request_body",
		"auth.admin_key", "auth.key_hash_algorithm",
		"db.type", "db.sqlite_path",
		"logging.level", "logging.format", "logging.output", "logging.file_path",
		"cache.type", "cache.redis_url", "cache.ollama_url",
		"pii.mode",
		"guardrails.mode",
		"mcp.endpoint", "mcp.hub_endpoint", "mcp.default_policy",
		"mcp.injection.default_tool_choice",
		"dashboard.auth_token", "dashboard.user_auth_jwt_secret",
		"jobs.api_key", "jobs.default_billing_key_id", "jobs.proxy_url",
		"metrics.path", "metrics.listen_addr",
		"telemetry.metrics_path", "telemetry.otlp_endpoint",
	} {
		typeCheck(k, "")
	}

	// Int values
	for _, k := range []string{
		"server.port",
		"cache.max_entries",
		"rate_limit.default_rpm", "rate_limit.default_tpm",
		"dashboard.port",
		"jobs.max_concurrent_jobs", "jobs.default_timeout_ms",
		"jobs.min_interval_seconds", "jobs.history_max_per_job",
		"jobs.history_retention_days",
		"audit.retention_days",
	} {
		typeCheck(k, 0)
	}

	// Bool values
	for _, k := range []string{
		"cache.enabled", "rate_limit.enabled", "rate_limit.admin_bypass",
		"budget.enabled",
		"pii.enabled",
		"guardrails.enabled",
		"mcp.enabled", "mcp.injection.enabled",
		"dashboard.enabled",
		"jobs.enabled", "jobs.redis_lock_enabled", "jobs.enforce_budget",
		"metrics.enabled", "metrics.include_model_labels",
		"telemetry.enabled",
		"audit.enabled", "audit.log_prompts", "audit.log_bodies",
		"cost_guard.loop_detection",
		"headers.emit_standard",
	} {
		typeCheck(k, true)
	}

	// Float64 values
	for _, k := range []string{
		"budget.default_daily_limit", "budget.default_monthly_limit",
		"budget.alert_threshold",
		"cache.similarity_threshold",
		"telemetry.trace_sampling",
	} {
		typeCheck(k, float64(0))
	}

	// Duration values (time.Duration)
	for _, k := range []string{
		"server.read_timeout", "server.write_timeout",
		"server.idle_timeout", "server.graceful_shutdown",
		"cache.ttl",
	} {
		typeCheck(k, durationType(0))
	}
}

// ─────────────────────────────────────────────────────────────────────
// extractDefault — unknown keys return nil
// ─────────────────────────────────────────────────────────────────────

func TestExtractDefault_UnknownKey(t *testing.T) {
	base := DefaultConfig()

	val := extractDefault("nonexistent.key", base)
	assert.Nil(t, val, "extractDefault for unknown key should return nil")
}

// ─────────────────────────────────────────────────────────────────────
// envSource — unknown keys don't crash
// ─────────────────────────────────────────────────────────────────────

func TestEnvSource_UnknownKey(t *testing.T) {
	// Should not panic.
	src := envSource("nonexistent.key")
	assert.Equal(t, "", src, "envSource for unknown key should return empty string")
}

// ─────────────────────────────────────────────────────────────────────
// Precedence: env > default
// ─────────────────────────────────────────────────────────────────────

func TestShowConfig_PrecedenceEnvOverDefaults(t *testing.T) {
	resetForTest()

	// Set env var where both systems agree:
	t.Setenv("ILTER_SERVER_PORT", "9999")

	cfg := DefaultConfig()

	entries := ShowConfig(&cfg, nil)

	byKey := make(map[string]Entry, len(entries))
	for _, e := range entries {
		byKey[e.Key] = e
	}

	e := byKey["server.port"]
	assert.Equal(t, "env", e.Source, "env should win over defaults")
	assert.Equal(t, 9999, e.Value, "env value should override default port")
}

// ─────────────────────────────────────────────────────────────────────
// equalValues — edge cases
// ─────────────────────────────────────────────────────────────────────

func TestEqualValues(t *testing.T) {
	t.Run("string equality", func(t *testing.T) {
		assert.True(t, equalValues("hello", "hello"))
		assert.False(t, equalValues("hello", "world"))
	})

	t.Run("int equality", func(t *testing.T) {
		assert.True(t, equalValues(42, 42))
		assert.False(t, equalValues(42, 43))
	})

	t.Run("bool equality", func(t *testing.T) {
		assert.True(t, equalValues(true, true))
		assert.False(t, equalValues(true, false))
	})

	t.Run("float64 equality", func(t *testing.T) {
		assert.True(t, equalValues(1.5, 1.5))
		assert.False(t, equalValues(1.5, 2.5))
	})

	t.Run("duration equality", func(t *testing.T) {
		assert.True(t, equalValues(time.Second, time.Second))
		assert.False(t, equalValues(time.Second, time.Minute))
	})

	t.Run("type mismatch", func(t *testing.T) {
		assert.False(t, equalValues(42, "42"))
		assert.False(t, equalValues(true, 1))
	})

	t.Run("nil values", func(t *testing.T) {
		// nil == nil via the default comparison path.
		assert.True(t, equalValues(nil, nil))
	})

	t.Run("unhandled comparable type falls through to ==", func(t *testing.T) {
		type point struct{ X, Y int }
		assert.True(t, equalValues(point{1, 2}, point{1, 2}))
		assert.False(t, equalValues(point{1, 2}, point{3, 4}))
	})
}
