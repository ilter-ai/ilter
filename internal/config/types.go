package config

import (
	"strings"
	"time"

	"github.com/ilter-ai/ilter/internal/model"
)

type FallbackConfig struct {
	Enabled          bool
	CooldownDuration time.Duration
	ModelDowngrade   string
	AllowedModels    []string
	MaxAttempts      int
}

type Config struct {
	Server     ServerConfig
	Auth       AuthConfig
	Providers  []ProviderConfig
	Cache      CacheConfig
	RateLimit  RateLimitConfig
	Budget     BudgetConfig
	Logging    LoggingConfig
	Metrics    MetricsConfig
	Telemetry  TelemetryConfig
	Audit      AuditConfig
	Storage    StorageConfig
	CostGuard  CostGuardConfig
	Headers    HeadersConfig
	PII        PIIConfig
	Routing    RoutingConfig
	Guardrails GuardrailsConfig
	Dashboard  DashboardConfig
	MCP        MCPConfig
	Fallback   FallbackConfig
	Jobs       JobsConfig
}

type DashboardConfig struct {
	Enabled           bool
	Port              int
	AuthToken         string
	UserAuthJWTSecret string
}

type PIIConfig struct {
	Enabled  bool
	Mode     string
	Patterns []string
	RedisURL string
}

type RoutingConfig struct {
	Enabled              bool
	ProviderPreference   string
	LoadBalancerStrategy string
	ComplexityThresholds ComplexityThresholdsConfig
	Scorer               ScorerConfig
	Rules                []model.RoutingRule
}

type ComplexityThresholdsConfig struct {
	Economy  float64
	Standard float64
}

type ScorerConfig struct {
	Type      string // "heuristic" (default) | "llm" | "embedding" | "trainable"
	LLM       *LLMScorerConfig
	Embedding *EmbeddingScorerConfig
	Trainable *TrainableScorerConfig
}

type LLMScorerConfig struct {
	Model           string
	Provider        string
	CacheTTL        time.Duration
	CacheMaxEntries int
	Timeout         time.Duration
}

type EmbeddingScorerConfig struct {
	Model               string
	Dimensions          int
	ReferenceCount      int
	SimilarityThreshold float64
}

type TrainableScorerConfig struct {
	ModelPath       string
	FeatureVersion  int
	FallbackOnError bool
}

type ServerConfig struct {
	Host             string
	Port             int
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration
	IdleTimeout      time.Duration
	MaxRequestBody   string
	GracefulShutdown time.Duration
}

type AuthConfig struct {
	AdminKey         string
	KeyHashAlgorithm string
}

type ProviderConfig struct {
	Name            string               `json:"name"`
	Type            string               `json:"type"`
	BaseURL         string               `json:"base_url"`
	APIKey          string               `json:"api_key,omitempty"`
	APIKeys         []string             `json:"api_keys,omitempty"`
	APIKeySource    string               `json:"api_key_source,omitempty"` // "env" | "db" | "" — set by ResolveProviderKeys
	Models          []ModelConfig        `json:"models,omitempty"`
	Timeout         time.Duration        `json:"timeout,omitempty"`
	MaxRetries      int                  `json:"max_retries,omitempty"`
	CircuitBreaker  CircuitBreakerConfig `json:"circuit_breaker,omitempty"`
	Headers         map[string]string    `json:"headers,omitempty"`
	DiscoveryPublic bool                 `json:"discovery_public,omitempty"` // discovery /models endpoint works without API key
}

// GetAPIKeys returns all configured API keys. If APIKeys is set and non-empty, it returns cleaned APIKeys.
// Otherwise if APIKey is set, it returns []string{APIKey}. Returns nil if no key is configured.
func (p ProviderConfig) GetAPIKeys() []string {
	var keys []string
	for _, k := range p.APIKeys {
		if trimmed := strings.TrimSpace(k); trimmed != "" {
			keys = append(keys, trimmed)
		}
	}
	if len(keys) > 0 {
		return keys
	}
	if trimmed := strings.TrimSpace(p.APIKey); trimmed != "" {
		return []string{trimmed}
	}
	return nil
}

type ModelConfig struct {
	Name               string
	Weight             int
	Priority           int
	MaxTokens          int
	CostPerInputToken  float64
	CostPerOutputToken float64
}

type CircuitBreakerConfig struct {
	MaxFailures         int
	Timeout             time.Duration
	HalfOpenMaxRequests int
}

type CacheConfig struct {
	Enabled             bool
	Type                string
	RedisURL            string
	SimilarityThreshold float64
	TTL                 time.Duration
	OllamaURL           string
	MaxEntries          int
}

type RateLimitConfig struct {
	Enabled     bool
	AdminBypass bool
	DefaultRPM  int
	DefaultTPM  int
	RedisURL    string
}

type BudgetConfig struct {
	Enabled             bool
	DefaultDailyLimit   float64
	DefaultMonthlyLimit float64
	AlertThreshold      float64
}

type LoggingConfig struct {
	Level    string
	Format   string
	Output   string
	FilePath string
}

type MetricsConfig struct {
	Enabled            bool
	Path               string
	ListenAddr         string
	IncludeModelLabels bool
}

type TelemetryConfig struct {
	Enabled       bool
	MetricsPath   string
	OTLPEndpoint  string
	TraceSampling float64
}

type AuditConfig struct {
	Enabled       bool
	LogPrompts    bool
	LogBodies     bool
	RetentionDays int
}

type StorageConfig struct {
	Type       string
	SqlitePath string
}

type HeadersConfig struct {
	// EmitStandard, when true, adds standard-compatible response headers
	// (X-Request-Cost, X-Request-Pricing) alongside X-Ilter-* headers.
	EmitStandard bool
}

type CostGuardConfig struct {
	LoopDetection bool
	LoopSettings  LoopSettingsConfig
}

type LoopSettingsConfig struct {
	RateThreshold         int
	FingerprintWindow     int
	FingerprintDuplicates int
	CostWindow            time.Duration
	CostThreshold         float64
	SessionMaxRequests    int

	// Output loop detection — catches intra-response repetitive generation.
	// Runs inside the proxy streaming handler, not the middleware chain.
	OutputLoopMode      string // "off" | "observe" | "enforce" (default: "observe")
	OutputLoopThreshold int    // consecutive identical sentences to trigger (default: 6)
	OutputMinSentence   int    // minimum rune count for a sentence (default: 20)
}

// GuardrailsConfig controls the prompt-injection, toxicity, and topic-block middleware.
type GuardrailsConfig struct {
	Enabled       bool
	Mode          string   // "block" | "warn" | "mask" (default "block")
	RuleSets      []string // e.g. ["prompt_injection", "toxic_content"]
	TopicBlock    TopicBlockConfig
	ModerationAPI ModerationAPIConfig
	Output        string // "stdout" | "stderr" | file path; empty = reuse main logger
}

// CustomRuleConfig defines a user-supplied regex rule.
type CustomRuleConfig struct {
	ID       string
	Patterns []string
	Mode     string // "block" | "warn" | "mask"
	Severity string // "low" | "medium" | "high" | "critical"
	Enabled  bool
}

// TopicBlockConfig configures keyword-based topic blocking.
type TopicBlockConfig struct {
	Enabled bool
	Topics  []string
	Mode    string // "block" | "warn" | "mask"
}

type MCPConfig struct {
	Enabled       bool
	Endpoint      string
	HubEndpoint   string
	DefaultPolicy string // "allow" or "deny", fallback when nothing matches
	Injection     MCPInjectionConfig
	OAuth         OAuthConfig
}

type MCPServerConfig struct {
	ID          string
	Name        string
	Description string
	Transport   string
	URL         string
	Command     string
	Args        []string
	Env         map[string]string
	Handler     string
	Enabled     bool
	Timeout     string
	MaxRetries  int
	AuthType    string
	AuthKeyEnv  string
}

// MCPAccessRule defines a per-key, per-group, or per-key-id tool access rule.
type MCPAccessRule struct {
	KeyPrefix string
	GroupID   *int
	KeyID     string
	Tools     []string
	Effect    string // "allow" or "deny", defaults to "allow"
}

type JobsConfig struct {
	Enabled              bool
	APIKey               string
	DefaultBillingKeyID  string
	ProxyURL             string
	MaxConcurrentJobs    int
	DefaultTimeoutMs     int
	RedisLockEnabled     bool
	MinIntervalSeconds   int
	HistoryMaxPerJob     int
	HistoryRetentionDays int
	EnforceBudget        bool
	MaxVarLength         int
	PollInterval         string // periodic reconciler interval (e.g. "60s")
	RetryDelayBase       string // base delay for retry backoff (e.g. "10s")
}

type MCPInjectionConfig struct {
	Enabled                bool
	DefaultToolChoice      string
	StripToolsFromResponse bool
}

// OAuthConfig controls the OAuth 2.0 PKCE authorization flow for MCP.
type OAuthConfig struct {
	Enabled             bool
	DefaultBudget       float64       // default monthly budget in USD for OAuth-minted keys
	DefaultRateLimit    int           // default rate limit (RPM) for OAuth-minted keys
	AllowedRedirectURIs []string      // allowed redirect URIs (empty = all loopback allowed)
	TokenTTL            time.Duration // access token / auth code TTL
}

// v1 ships a no-op implementation; the HTTP client is deferred to v1.1.
type ModerationAPIConfig struct {
	Enabled bool
	URL     string
	APIKey  string
	Timeout string // e.g. "3s"
}

// OpenAPISpecConfig defines a single OpenAPI spec that is exposed as MCP tools.
type OpenAPISpecConfig struct {
	Name        string   // unique spec identifier
	Description string   // human-readable summary of the spec/service
	SpecURL     string   // URL or file path to the spec
	Operations  []string // last-synced catalog of operation IDs discovered in the spec (informational, not a filter)
	Auth        OpenAPIAuthConfig
	Timeout     time.Duration // per-request timeout, default 30s
}

// OpenAPIAuthConfig defines authentication for an OpenAPI tool spec.
type OpenAPIAuthConfig struct {
	Type  string // "bearer" | "api_key" | "basic" | "none"
	Value string // supports "${ENV_VAR}" substitution
	Key   string // header name for api_key type (default "X-API-Key")
}
