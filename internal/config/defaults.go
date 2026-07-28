package config

import "time"

const (
	DefaultDashboardPort     = 9191
	DefaultEconomyThreshold  = 15.0
	DefaultStandardThreshold = 50.0
	DefaultRetentionDays     = 90
	DefaultMetricsPath       = "/metrics"
	DefaultMetricsListenAddr = "0.0.0.0:9192"
	DefaultTraceSampling     = 1.0
	DefaultMCPEndpoint       = "/mcp"
	DefaultMCPHubEndpoint    = "/mcp/sse"
	DefaultMCPToolChoice     = "auto"
	DefaultCooldownDuration  = 5 * time.Minute
)

var DefaultBaseURLs = map[string]string{
	"openai":       "https://api.openai.com/v1",
	"anthropic":    "https://api.anthropic.com/v1",
	"deepseek":     "https://api.deepseek.com/v1",
	"gemini":       "https://generativelanguage.googleapis.com/v1beta/openai",
	"ollama":       "http://localhost:11434",
	"openrouter":   "https://openrouter.ai/api/v1",
	"opencode_go":  "https://opencode.ai/zen/go/v1",
	"opencode_zen": "https://opencode.ai/zen/v1",
	"qwen":         "https://dashscope-intl.aliyuncs.com/compatible-mode/v1",
	"kop":          "https://www.kop.gg/api/v1",
}

// Defaults are used as the base before env overrides are applied.
func DefaultConfig() Config {
	return Config{
		Server: ServerConfig{
			Host:         "0.0.0.0",
			Port:         8181,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 120 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
		Auth: AuthConfig{
			AdminKey: "ilter-admin-key-default-32-chars-hex",
		},
		Storage: StorageConfig{
			Type:       "sqlite",
			SqlitePath: "./data/ilter.db",
		},

		Headers: HeadersConfig{
			EmitStandard: true,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "console",
		},
		PII: PIIConfig{
			Enabled: true,
			Mode:    "mask",
		},
		Cache: CacheConfig{
			Enabled:             true,
			SimilarityThreshold: 0.70,
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
		Audit: AuditConfig{
			Enabled:       true,
			LogPrompts:    false,
			LogBodies:     false,
			RetentionDays: DefaultRetentionDays,
		},
		Jobs: JobsConfig{
			Enabled:              true,
			APIKey:               "test",
			MaxConcurrentJobs:    10,
			DefaultTimeoutMs:     120000,
			RedisLockEnabled:     true,
			MinIntervalSeconds:   60,
			HistoryMaxPerJob:     100,
			HistoryRetentionDays: 30,
			EnforceBudget:        true,
		},
		MCP: MCPConfig{
			Enabled:       true,
			Endpoint:      DefaultMCPEndpoint,
			HubEndpoint:   DefaultMCPHubEndpoint,
			DefaultPolicy: "allow",
			Injection: MCPInjectionConfig{
				Enabled:                false,
				DefaultToolChoice:      DefaultMCPToolChoice,
				StripToolsFromResponse: true,
			},
			OAuth: OAuthConfig{
				Enabled:          true,
				DefaultBudget:    100.0,
				DefaultRateLimit: 60,
				TokenTTL:         time.Hour,
			},
		},
	}
}
