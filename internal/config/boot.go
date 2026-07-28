package config

// resolveFloat64 unwraps an OptionFloat64 or returns the fallback.
func resolveFloat64(o OptionFloat64, fallback float64) float64 {
	if v, ok := o.Value(); ok {
		return v
	}
	return fallback
}

// resolveInt64 unwraps an OptionInt64 or returns the fallback.
func resolveInt64(o OptionInt64, fallback int64) int64 {
	if v, ok := o.Value(); ok {
		return v
	}
	return fallback
}

// ─────────────────────────────────────────────────────────────────────
// Boot-level sub-types
// These are subsets of the full config types used only at boot time.
// Fields that can be overridden by StateConfig use Option[T] types.
// ─────────────────────────────────────────────────────────────────────

// BootCacheConfig contains cache settings needed at boot time.
// The type and RedisURL are required to connect to the cache at startup.
// SimilarityThreshold is an optional default that the state can override.
type BootCacheConfig struct {
	Enabled             bool
	Type                string
	RedisURL            string
	SimilarityThreshold OptionFloat64
}

// BootAuditConfig contains audit-log settings set at boot time.
// RetentionDays is an optional default that the state can override.
type BootAuditConfig struct {
	Enabled       bool
	LogPrompts    bool
	LogBodies     bool
	RetentionDays OptionInt64
}

// BootGuardConfig contains guardrail defaults set at boot time.
// Mode flags and the enabled toggle provide engine-wide defaults;
// per-key or per-group overrides come from StateConfig.
type BootGuardConfig struct {
	Enabled       bool
	Mode          string
	ModerationAPI ModerationAPIConfig
}

// BootMCPConfig contains MCP engine defaults set at boot time.
// Individual MCP server definitions and access rules come from StateConfig.
type BootMCPConfig struct {
	Enabled       bool
	Endpoint      string
	DefaultPolicy string
	HubEndpoint   string
}

// ─────────────────────────────────────────────────────────────────────
// BootConfig — the subset of configuration with compiled-in defaults.
// It covers infrastructure wiring (server, storage, Redis, logging,
// metrics, telemetry) plus engine-default values for features whose
// active state is managed at runtime via StateConfig.
// ─────────────────────────────────────────────────────────────────────
type BootConfig struct {
	Server     ServerConfig
	Auth       AuthConfig
	Storage    StorageConfig
	Logging    LoggingConfig
	Metrics    MetricsConfig
	Telemetry  TelemetryConfig
	Cache      BootCacheConfig
	Audit      BootAuditConfig
	Guardrails BootGuardConfig
	MCP        BootMCPConfig
	Jobs       JobsConfig
	Headers    HeadersConfig
	RateLimit  RateLimitConfig
	Routing    RoutingConfig
	Budget     BudgetConfig
	CostGuard  CostGuardConfig
	PII        PIIConfig
	Fallback   FallbackConfig
	Dashboard  DashboardConfig
}

// ─────────────────────────────────────────────────────────────────────
// DefaultBootConfig returns a BootConfig with sensible defaults.
// Compiled-in defaults; env vars override at runtime.
// ─────────────────────────────────────────────────────────────────────
func DefaultBootConfig() BootConfig {
	return BootConfig{
		Headers: HeadersConfig{
			EmitStandard: true,
		},
		PII: PIIConfig{
			Enabled: true,
			Mode:    "mask",
		},
		Fallback: FallbackConfig{
			Enabled:          true,
			CooldownDuration: DefaultCooldownDuration,
			ModelDowngrade:   "none",
			MaxAttempts:      0,
		},
		Dashboard: DashboardConfig{
			Enabled: true,
			Port:    DefaultDashboardPort,
		},
		Telemetry: TelemetryConfig{
			Enabled:       true,
			MetricsPath:   DefaultMetricsPath,
			TraceSampling: DefaultTraceSampling,
		},
		Metrics: MetricsConfig{
			Enabled:    true,
			Path:       DefaultMetricsPath,
			ListenAddr: DefaultMetricsListenAddr,
		},
		CostGuard: CostGuardConfig{
			LoopDetection: true,
		},
		Audit: BootAuditConfig{
			Enabled:       true,
			LogPrompts:    false,
			LogBodies:     false,
			RetentionDays: OptionInt64{optionCore[int64]{val: DefaultRetentionDays, present: true}},
		},
		Cache: BootCacheConfig{
			Enabled:             true,
			SimilarityThreshold: OptionFloat64{optionCore[float64]{val: 0.70, present: true}},
		},
		MCP: BootMCPConfig{
			Enabled:       true,
			Endpoint:      DefaultMCPEndpoint,
			DefaultPolicy: "allow",
			HubEndpoint:   DefaultMCPHubEndpoint,
		},
		Jobs: JobsConfig{
			Enabled:              true,
			MaxConcurrentJobs:    10,
			DefaultTimeoutMs:     120000,
			RedisLockEnabled:     true,
			MinIntervalSeconds:   60,
			HistoryMaxPerJob:     100,
			HistoryRetentionDays: 30,
			EnforceBudget:        true,
		},
		RateLimit: RateLimitConfig{
			Enabled:    true,
			DefaultRPM: 6000,
			DefaultTPM: 500000,
		},
		Budget: BudgetConfig{
			Enabled:        true,
			AlertThreshold: 0.8,
		},
		Routing: RoutingConfig{
			Enabled: true,
		},
	}
}

// ─────────────────────────────────────────────────────────────────────
// ToBootConfig extracts a BootConfig from a loaded Config
// for ConfigCache construction.
func ToBootConfig(cfg *Config) BootConfig {
	return BootConfig{
		Server:    cfg.Server,
		Auth:      cfg.Auth,
		Storage:   cfg.Storage,
		Logging:   cfg.Logging,
		Metrics:   cfg.Metrics,
		Telemetry: cfg.Telemetry,
		Headers:   cfg.Headers,
		RateLimit: cfg.RateLimit,
		Routing:   cfg.Routing,
		Budget:    cfg.Budget,
		CostGuard: cfg.CostGuard,
		PII:       cfg.PII,
		Dashboard: cfg.Dashboard,
		Cache: BootCacheConfig{
			Enabled:             cfg.Cache.Enabled,
			Type:                cfg.Cache.Type,
			RedisURL:            cfg.Cache.RedisURL,
			SimilarityThreshold: OptionFloat64{optionCore[float64]{val: cfg.Cache.SimilarityThreshold, present: true}},
		},
		Audit: BootAuditConfig{
			Enabled:       cfg.Audit.Enabled,
			LogPrompts:    cfg.Audit.LogPrompts,
			LogBodies:     cfg.Audit.LogBodies,
			RetentionDays: OptionInt64{optionCore[int64]{val: int64(cfg.Audit.RetentionDays), present: true}},
		},
		Guardrails: BootGuardConfig{
			Enabled:       cfg.Guardrails.Enabled,
			Mode:          cfg.Guardrails.Mode,
			ModerationAPI: cfg.Guardrails.ModerationAPI,
		},
		MCP: BootMCPConfig{
			Enabled:       cfg.MCP.Enabled,
			Endpoint:      cfg.MCP.Endpoint,
			DefaultPolicy: cfg.MCP.DefaultPolicy,
			HubEndpoint:   cfg.MCP.HubEndpoint,
		},
		Jobs: cfg.Jobs,
	}
}

// ─────────────────────────────────────────────────────────────────────
// BootConfigToConfig converts a BootConfig into the legacy Config struct,
// filling in default values for sections that BootConfig does not carry.
// This seeds defaults from BootConfig into the legacy Config struct format.
// ─────────────────────────────────────────────────────────────────────
func BootConfigToConfig(boot *BootConfig) Config {
	cfg := Config{
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
		PII:       boot.PII,
		Dashboard: boot.Dashboard,

		Cache: CacheConfig{
			Enabled:             boot.Cache.Enabled,
			Type:                boot.Cache.Type,
			RedisURL:            boot.Cache.RedisURL,
			SimilarityThreshold: resolveFloat64(boot.Cache.SimilarityThreshold, 0.70),
		},
		Audit: AuditConfig{
			Enabled:       boot.Audit.Enabled,
			LogPrompts:    boot.Audit.LogPrompts,
			LogBodies:     boot.Audit.LogBodies,
			RetentionDays: int(resolveInt64(boot.Audit.RetentionDays, DefaultRetentionDays)),
		},
		Guardrails: GuardrailsConfig{
			Enabled:       boot.Guardrails.Enabled,
			Mode:          boot.Guardrails.Mode,
			ModerationAPI: boot.Guardrails.ModerationAPI,
		},
		MCP: MCPConfig{
			Enabled:       boot.MCP.Enabled,
			Endpoint:      boot.MCP.Endpoint,
			HubEndpoint:   boot.MCP.HubEndpoint,
			DefaultPolicy: boot.MCP.DefaultPolicy,
			Injection: MCPInjectionConfig{
				Enabled:                false,
				DefaultToolChoice:      DefaultMCPToolChoice,
				StripToolsFromResponse: true,
			},
		},
		Jobs: boot.Jobs,
	}

	return cfg
}
