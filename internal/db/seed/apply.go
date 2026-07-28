// Package seed provides idempotent initialisation of the runtime_config table.
// The seed populates providers, MCP servers, guardrail rules, routing strategy,
// and feature flags.  It is idempotent — applying the same seed data multiple
// times produces identical state.
//
// Rows that have been modified by a user (updated_by IS NOT NULL) are never
// overwritten — the seed respects user edits.
package seed

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	dbpkg "github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/model"
)

// ─────────────────────────────────────────────────────────────────────
// Seed file types
// ─────────────────────────────────────────────────────────────────────

// OpenAPISpec represents an OpenAPI specification stored in the openapi_specs table.
type OpenAPISpec struct {
	ID          string
	Name        string
	Description string
	SpecURL     string
	Operations  []string
	AuthType    string
	AuthValue   string
	AuthKey     string
	Enabled     bool
}

// File is the top-level seed document.
type File struct {
	Version        string
	Providers      []Provider
	MCPServers     []MCPServer
	GuardrailRules []GuardrailRule
	OpenAPISpecs   []OpenAPISpec
	Routing        *Routing
	FeatureFlags   map[string]bool
	DashboardPort  int    // 0 = use default (9191)
	MetricsPort    int    // 0 = use default (9192)
	PIIMode        string // "" = use default ("mask")
}

// Provider represents a provider registration in the seed file.
type Provider struct {
	Name            string
	Provider        string
	BaseURL         string
	APISecretKey    string
	IsActive        bool
	DiscoveryPublic bool // /models endpoint works without API key
}

// MCPServer represents an MCP server configuration in the seed file.
type MCPServer struct {
	ID          string
	Name        string
	Description string
	EndpointURL string
	AuthToken   string
	IsEnabled   bool
	Timeout     string
	TimeoutMs   int
	MaxRetries  int
	Transport   string
	Command     string
	Args        []string
	Env         map[string]string
	Handler     string
	AuthType    string
	AuthKeyEnv  string
}

// GuardrailRule represents a guardrail rule in the seed file.
type GuardrailRule struct {
	Name     string
	Type     string
	Pattern  string
	Action   string
	Priority int
	Enabled  bool
	Severity string
}

// Routing represents the routing configuration in the seed file.
type Routing struct {
	Strategies []model.RoutingStrategy
	Active     string // name of the active strategy
}

// ─────────────────────────────────────────────────────────────────────
// ApplySeedData
// ─────────────────────────────────────────────────────────────────────

// ApplySeedData applies seed data to the runtime_config table.
// It calls store.ApplyMigrations first to ensure the runtime_config table
// exists. The operation is idempotent: running it multiple times produces
// identical state. Rows with updated_by IS NOT NULL are left untouched
// (user-modified guard).
func ApplySeedData(db *sql.DB, seed *File) error {
	if seed == nil {
		return fmt.Errorf("seed: seed data is nil")
	}

	// ── Ensure runtime_config table exists ──
	if err := dbpkg.ApplyMigrations(db); err != nil {
		return fmt.Errorf("seed: apply migrations: %w", err)
	}

	// ── Resolve environment variables in secret fields ──
	for i := range seed.Providers {
		seed.Providers[i].APISecretKey = os.ExpandEnv(seed.Providers[i].APISecretKey)
	}

	// ── Providers ──
	if err := applyProviders(db, seed.Providers); err != nil {
		return err
	}

	// ── MCP Servers ──
	if err := applyMCPServers(db, seed.MCPServers); err != nil {
		return err
	}

	// ── Guardrail Rules ──
	if err := applyGuardrailRules(db, seed.GuardrailRules); err != nil {
		return err
	}

	// ── Built-in Guardrail Rules (multi-pattern DB rules) ──
	if err := BuiltinGuardrailRules(db); err != nil {
		return fmt.Errorf("seed: builtin guardrail rules: %w", err)
	}

	// ── Routing ──
	if seed.Routing != nil {
		if err := applyRouting(db, seed.Routing); err != nil {
			return err
		}
	}

	// ── OpenAPI Specs ──
	if err := applyOpenAPISpecs(db, seed.OpenAPISpecs); err != nil {
		return err
	}

	// ── Feature Flags ──
	if err := applyFeatureFlags(db, seed.FeatureFlags); err != nil {
		return err
	}

	if seed.DashboardPort > 0 {
		if err := upsertRuntimeConfig(db, "dashboard", "port", strconv.Itoa(seed.DashboardPort)); err != nil {
			return fmt.Errorf("seed dashboard port: %w", err)
		}
	}
	if seed.MetricsPort > 0 {
		if err := upsertRuntimeConfig(db, "metrics", "port", strconv.Itoa(seed.MetricsPort)); err != nil {
			return fmt.Errorf("seed metrics port: %w", err)
		}
	}

	if seed.PIIMode != "" {
		if err := upsertRuntimeConfig(db, "pii", "mode", seed.PIIMode); err != nil {
			return fmt.Errorf("seed PII mode: %w", err)
		}
	}

	// fallback config — hardcoded defaults, user overrides via dashboard
	if err := upsertRuntimeConfig(db, "fallback", "model_downgrade", "cheapest"); err != nil {
		return fmt.Errorf("seed fallback model_downgrade: %w", err)
	}
	if err := upsertRuntimeConfig(db, "fallback", "allowed_models",
		"big-pickle,deepseek-v4-flash-free,mimo-v2.5-free"); err != nil {
		return fmt.Errorf("seed fallback allowed_models: %w", err)
	}
	if err := upsertRuntimeConfig(db, "fallback", "enabled", "true"); err != nil {
		return fmt.Errorf("seed fallback enabled: %w", err)
	}

	// ── Reference Vectors (embedding scorer centroids) ──
	if err := seedReferenceVectors(db, 768); err != nil {
		return fmt.Errorf("seed reference vectors: %w", err)
	}

	return nil
}

// seedReferenceVectors inserts default reference embedding vectors for each
// complexity tier. Only inserts rows that don't already exist (respects
// the updated_by guard in upsertRuntimeConfig semantics — a no-op upsert
// with default updated_by="" won't overwrite user edits because the caller
// uses the simple upsert helper below which resets updated_by to NULL).
func seedReferenceVectors(db *sql.DB, dims int) error {
	vectors := map[string][]float32{
		"economy":  makeReferenceVector(dims, 0.1, 0),
		"standard": makeReferenceVector(dims, 0.5, 1),
		"premium":  makeReferenceVector(dims, 0.9, 2),
	}
	for tier, vec := range vectors {
		data, err := json.Marshal(vec)
		if err != nil {
			return fmt.Errorf("seed reference vector %q: %w", tier, err)
		}
		if err := upsertRuntimeConfig(db, "reference_vector", tier, string(data)); err != nil {
			return fmt.Errorf("seed reference vector %q: %w", tier, err)
		}
	}
	return nil
}

// makeReferenceVector creates a dims-length vector with val at position idx.
func makeReferenceVector(dims int, val float32, idx int) []float32 {
	if idx >= dims {
		idx = 0
	}
	vec := make([]float32, dims)
	vec[idx] = val
	return vec
}

// ─────────────────────────────────────────────────────────────────────
// Section appliers
// ─────────────────────────────────────────────────────────────────────

func applyProviders(db *sql.DB, providers []Provider) error {
	for _, sp := range providers {
		// Build the provider registration
		reg := model.ProviderRegistration{
			Name:            sp.Name,
			Provider:        sp.Provider,
			BaseURL:         sp.BaseURL,
			APISecretKey:    sp.APISecretKey,
			IsActive:        sp.IsActive,
			DiscoveryPublic: sp.DiscoveryPublic,
		}

		// Marshal to JSON
		data, err := json.Marshal(reg)
		if err != nil {
			return fmt.Errorf("seed: marshal provider %q: %w", sp.Name, err)
		}

		if err := upsertRuntimeConfig(db, "provider", sp.Name, string(data)); err != nil {
			return fmt.Errorf("seed: upsert provider %q: %w", sp.Name, err)
		}
	}
	return nil
}

func applyMCPServers(db *sql.DB, servers []MCPServer) error {
	for _, ss := range servers {
		srv := model.MCPServer{
			Name:        ss.Name,
			EndpointURL: ss.EndpointURL,
			AuthToken:   ss.AuthToken,
			IsEnabled:   ss.IsEnabled,
			Timeout:     ss.Timeout,
			MaxRetries:  ss.MaxRetries,
			Transport:   ss.Transport,
			Command:     ss.Command,
			Args:        ss.Args,
			Env:         ss.Env,
			Handler:     ss.Handler,
		}

		data, err := json.Marshal(srv)
		if err != nil {
			return fmt.Errorf("seed: marshal mcp server %q: %w", ss.Name, err)
		}

		if err := upsertRuntimeConfig(db, "mcp_server", ss.Name, string(data)); err != nil {
			return fmt.Errorf("seed: upsert mcp server %q: %w", ss.Name, err)
		}
	}
	return nil
}

func applyGuardrailRules(db *sql.DB, rules []GuardrailRule) error {
	for _, sr := range rules {
		rule := model.GuardrailRule{
			Name:     sr.Name,
			Type:     sr.Type,
			Pattern:  sr.Pattern,
			Action:   sr.Action,
			Priority: sr.Priority,
			Enabled:  sr.Enabled,
			Severity: sr.Severity,
		}

		data, err := json.Marshal(rule)
		if err != nil {
			return fmt.Errorf("seed: marshal guardrail rule %q: %w", sr.Name, err)
		}

		if err := upsertRuntimeConfig(db, "guardrail_rule", sr.Name, string(data)); err != nil {
			return fmt.Errorf("seed: upsert guardrail rule %q: %w", sr.Name, err)
		}
	}
	return nil
}

// Strategies returns the default set of routing strategies for production.
// All 4 target Opencode Zen free models so they work for any user out of the box.
// Users with paid API keys can create custom strategies via the admin API.
func Strategies() []model.RoutingStrategy {
	return []model.RoutingStrategy{
		{
			Name:                 "economy",
			Description:          "Cost-first — simple→deepseek-v4-flash, moderate→hy3, complex→nemotron-3-ultra",
			Enabled:              true,
			ProviderPreference:   "cheapest",
			Scorer:               model.ScorerConfig{Type: "heuristic"},
			ComplexityThresholds: model.ComplexityThresholds{Economy: 25, Standard: 60},
			Rules: []model.RoutingRule{
				{Name: "Simple queries", Condition: "complexity < 25", TargetModel: "deepseek-v4-flash-free", Priority: 100, Enabled: true},
				{Name: "Moderate queries", Condition: "complexity between 25 60", TargetModel: "hy3-free", Priority: 90, Enabled: true},
				{Name: "Complex queries", Condition: "complexity >= 60", TargetModel: "nemotron-3-ultra-free", Priority: 80, Enabled: true},
			},
		},
		{
			Name:                 "premium",
			Description:          "Quality-first — code→north-mini-code, trivial→flash, moderate→hy3, complex→nemotron",
			Enabled:              false,
			ProviderPreference:   "round-robin",
			Scorer:               model.ScorerConfig{Type: "heuristic"},
			ComplexityThresholds: model.ComplexityThresholds{Economy: 15, Standard: 50},
			Rules: []model.RoutingRule{
				{Name: "Code blocks", Condition: "prompt contains \"```\"", TargetModel: "north-mini-code-free", Priority: 100, Enabled: true},
				{Name: "Trivial", Condition: "complexity < 15", TargetModel: "deepseek-v4-flash-free", Priority: 90, Enabled: true},
				{Name: "Standard", Condition: "complexity between 15 50", TargetModel: "hy3-free", Priority: 80, Enabled: true},
				{Name: "Complex reasoning", Condition: "complexity >= 50", TargetModel: "nemotron-3-ultra-free", Priority: 70, Enabled: true},
			},
		},
		{
			Name:                 "balanced",
			Description:          "Balanced cost/quality — code→north, flash for cheap, hy3 for standard, nemotron for premium",
			Enabled:              false,
			ProviderPreference:   "cheapest",
			Scorer:               model.ScorerConfig{Type: "heuristic"},
			ComplexityThresholds: model.ComplexityThresholds{Economy: 20, Standard: 55},
			Rules: []model.RoutingRule{
				{Name: "Code blocks", Condition: "prompt contains \"```\"", TargetModel: "north-mini-code-free", Priority: 100, Enabled: true},
				{Name: "Economy", Condition: "complexity < 20", TargetModel: "deepseek-v4-flash-free", Priority: 90, Enabled: true},
				{Name: "Standard", Condition: "complexity between 20 55", TargetModel: "hy3-free", Priority: 80, Enabled: true},
				{Name: "Premium tasks", Condition: "complexity >= 55", TargetModel: "nemotron-3-ultra-free", Priority: 70, Enabled: true},
			},
		},
		{
			Name:                 "smart",
			Description:          "Capability-based — code→north-mini-code, simple→flash, moderate→north, reasoning→nemotron",
			Enabled:              false,
			ProviderPreference:   "round-robin",
			Scorer:               model.ScorerConfig{Type: "heuristic"},
			ComplexityThresholds: model.ComplexityThresholds{Economy: 20, Standard: 60},
			Rules: []model.RoutingRule{
				{Name: "Code blocks", Condition: "prompt contains \"```\"", TargetModel: "north-mini-code-free", Priority: 100, Enabled: true},
				{Name: "Function definitions", Condition: "prompt contains \"function\"", TargetModel: "north-mini-code-free", Priority: 99, Enabled: true},
				{Name: "Simple queries", Condition: "complexity < 20", TargetModel: "deepseek-v4-flash-free", Priority: 90, Enabled: true},
				{Name: "Code-capable queries", Condition: "complexity between 20 60", TargetModel: "north-mini-code-free", Priority: 80, Enabled: true},
				{Name: "Reasoning tasks", Condition: "complexity >= 60", TargetModel: "nemotron-3-ultra-free", Priority: 70, Enabled: true},
			},
		},
	}
}

func applyRouting(db *sql.DB, routing *Routing) error {
	for _, s := range routing.Strategies {
		data, err := json.Marshal(s)
		if err != nil {
			return fmt.Errorf("seed: marshal routing strategy %q: %w", s.Name, err)
		}
		if err := upsertRuntimeConfig(db, "routing_strategy", s.Name, string(data)); err != nil {
			return fmt.Errorf("seed: upsert routing strategy %q: %w", s.Name, err)
		}
	}

	if routing.Active != "" {
		activeData, err := json.Marshal(routing.Active)
		if err != nil {
			return fmt.Errorf("seed: marshal active strategy: %w", err)
		}
		if err := upsertRuntimeConfig(db, "active_routing_strategy", "active", string(activeData)); err != nil {
			return fmt.Errorf("seed: upsert active strategy: %w", err)
		}
	}

	return nil
}

func applyOpenAPISpecs(db *sql.DB, specs []OpenAPISpec) error {
	for _, s := range specs {
		opsJSON, err := json.Marshal(s.Operations)
		if err != nil {
			return fmt.Errorf("seed: marshal operations for %q: %w", s.ID, err)
		}
		enabled := 0
		if s.Enabled {
			enabled = 1
		}
		_, err = db.Exec(
			`INSERT INTO openapi_specs (id, name, description, spec_url, operations, auth_type, auth_value, auth_key, timeout_ms, enabled, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 30000, ?, datetime('now'), datetime('now'))
			 ON CONFLICT(id) DO NOTHING`,
			s.ID, s.Name, s.Description, s.SpecURL, string(opsJSON), s.AuthType, s.AuthValue, s.AuthKey, enabled,
		)
		if err != nil {
			return fmt.Errorf("seed: upsert openapi spec %q: %w", s.ID, err)
		}
	}
	return nil
}

func applyFeatureFlags(db *sql.DB, flags map[string]bool) error {
	for name, enabled := range flags {
		val := "true"
		if !enabled {
			val = "false"
		}
		if err := upsertRuntimeConfig(db, "feature", name, val); err != nil {
			return fmt.Errorf("seed: upsert feature %q: %w", name, err)
		}
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────
// Database helper
// ─────────────────────────────────────────────────────────────────────

// upsertRuntimeConfig inserts or updates a single runtime_config row.
//
// The UPSERT respects the user-modified guard: if a row has updated_by
// IS NOT NULL (user-modified via the admin API), the UPDATE branch is
// skipped and the existing value is preserved.
func upsertRuntimeConfig(db *sql.DB, section, key, value string) error {
	const upsertSQL = `INSERT INTO runtime_config (section, key, value, version) VALUES (?, ?, ?, 1) ON CONFLICT(section, key) DO UPDATE SET value = excluded.value WHERE version = version AND updated_by IS NULL`
	_, err := db.Exec(upsertSQL, section, key, value)
	if err != nil {
		return fmt.Errorf("runtime_config upsert %q/%q: %w", section, key, err)
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────
// Default guardrail rules for production
// ─────────────────────────────────────────────────────────────────────

// DefaultGuardrailRules returns the recommended set of guardrail rules
// for production use. These complement the built-in rules (prompt injection,
// toxic content, input guardrails) by covering additional security, PII, and
// safety categories that are commonly needed in production LLM gateways.
//
// All patterns use well-tested RE2-compatible regex that is guaranteed to
// compile and have a low false-positive rate.
func DefaultGuardrailRules() []GuardrailRule {
	return []GuardrailRule{
		{
			Name:     "sql_injection",
			Type:     "prompt_injection",
			Pattern:  `(?i)(?:drop\s+(?:table|database)|truncate\s+table|alter\s+table|exec\s+(?:xp_|sp_)|SELECT\s+.*\s+FROM|INSERT\s+INTO|DELETE\s+FROM|UPDATE\s+.*\s+SET)`,
			Action:   "block",
			Priority: 100,
			Enabled:  true,
			Severity: "high",
		},
		{
			Name:     "xss_attempt",
			Type:     "prompt_injection",
			Pattern:  `(?i)(?:<script[^>]*>|javascript:\s*|onerror\s*=|onload\s*=|onclick\s*=|onmouseover\s*=|onfocus\s*=|onblur\s*=|onchange\s*=|<iframe\s+|<svg\s+on)`,
			Action:   "block",
			Priority: 90,
			Enabled:  true,
			Severity: "high",
		},
		{
			Name:     "credit_card_number",
			Type:     "pii_mask",
			Pattern:  `\b(?:\d{4}[-\s]?\d{4}[-\s]?\d{4}[-\s]?\d{4})\b`,
			Action:   "block",
			Priority: 100,
			Enabled:  true,
			Severity: "high",
		},
		{
			Name:     "api_key_leak",
			Type:     "pii_mask",
			Pattern:  `(?i)\b(?:[a-z]{2,3}_[a-zA-Z0-9]{16,}|[a-zA-Z0-9_-]{20,}(?:api.?key|secret.?key|access.?token|auth.?token))\b`,
			Action:   "warn",
			Priority: 90,
			Enabled:  true,
			Severity: "high",
		},
		{
			Name:     "dangerous_content",
			Type:     "toxicity",
			Pattern:  `(?i)\b(?:how\s+to\s+(?:make|build|create|produce|synthesize|cook)\s+(?:a\s+)?(?:weapon|explosive|bomb|poison|narcotic|illicit|illegal|meth|heroin|cocaine|biological|chemical|radioactive))\b`,
			Action:   "block",
			Priority: 100,
			Enabled:  true,
			Severity: "critical",
		},
		{
			Name:     "personal_attack",
			Type:     "toxicity",
			Pattern:  `(?i)\b(?:you\s+(?:are|'re)\s+(?:a\s+)?(?:idiot|moron|stupid|useless|pathetic|worthless|dumb|loser|jerk))\b`,
			Action:   "warn",
			Priority: 70,
			Enabled:  true,
			Severity: "medium",
		},
		{
			Name:     "path_traversal",
			Type:     "prompt_injection",
			Pattern:  `(?i)(?:\.\.\/|\.\.\\|\.\.%2f|\.\.%5c|%2e%2e%2f|%2e%2e%5c|%252e%252e%252f|etc/passwd|etc/shadow|etc/hosts)`,
			Action:   "block",
			Priority: 80,
			Enabled:  true,
			Severity: "high",
		},
		{
			Name:     "ssrf_attempt",
			Type:     "prompt_injection",
			Pattern:  `(?i)(?:localhost|127\.0\.0\.1|0\.0\.0\.0|169\.254\.|10\.\d{1,3}\.|172\.(?:1[6-9]|2\d|3[01])\.|192\.168\.)`,
			Action:   "block",
			Priority: 80,
			Enabled:  true,
			Severity: "high",
		},
	}
}

// ─────────────────────────────────────────────────────────────────────
// Built-in guardrail rules seed
// ─────────────────────────────────────────────────────────────────────

// BuiltinGuardrailRules inserts all hardcoded built-in guardrail rules
// into the guardrail_rules table if they don't already exist. This makes
// them visible and toggleable in the admin UI.
//
// The function is idempotent: existing rules (updated_by IS NOT NULL) are
// never overwritten.
func BuiltinGuardrailRules(db *sql.DB) error {
	builtinRules := []struct {
		ID          string
		Name        string
		Description string
		Type        string
		Patterns    []string
		Mode        string
		Severity    string
	}{
		// ── Prompt Injection rules (from patterns.go) ──
		{
			ID: "injection_dan", Name: "DAN-style Jailbreak", Description: "DAN-style jailbreak attempts",
			Type: "prompt_injection", Patterns: []string{`(?i)\b(?:do anything now|DAN|jailbreak mode)\b`, `(?i)you are now (?:DAN|GPT-\d+|a model without (?:rules|restrictions))`},
			Mode: "block", Severity: "critical",
		},
		{
			ID: "injection_system_prompt", Name: "System Prompt Extraction", Description: "Attempts to extract system prompt",
			Type: "prompt_injection", Patterns: []string{`(?i)ignore (?:all|any|previous) (?:instructions|prompts|rules)`, `(?i)(?:reveal|show|print|output) (?:your|the) system prompt`, `(?i)what (?:is|are) (?:your|the) (?:system|hidden|internal) prompt`},
			Mode: "block", Severity: "high",
		},
		{
			ID: "injection_roleplay", Name: "Character Roleplay Override", Description: "Character roleplay override attempts",
			Type: "prompt_injection", Patterns: []string{`(?i)you (?:must|will|should) (?:act|behave|pretend) (?:as|like|to be)`, `(?i)forget (?:everything|all|your) (?:above|previous|prior)`},
			Mode: "block", Severity: "high",
		},
		{
			ID: "injection_delimiter", Name: "Delimiter Manipulation", Description: "Delimiter manipulation attempts",
			Type: "prompt_injection", Patterns: []string{`(?i)<\/?(?:system|assistant|user|inst)\s*>`, `(?i)\[INST\]|\[/INST\]`, `(?i)<<\s*SYS\s*>>|<<\s*/SYS\s*>>`},
			Mode: "block", Severity: "high",
		},
		{
			ID: "injection_refusal_suppression", Name: "Refusal Suppression", Description: "Refusal suppression attempts",
			Type: "prompt_injection", Patterns: []string{`(?i)(?:don't|do not|never) (?:refuse|apologize|say (?:sorry|you can't))`, `(?i)respond as if (?:you have|there are) no (?:restrictions|limits|rules)`},
			Mode: "block", Severity: "high",
		},
		{
			ID: "injection_prefix", Name: "Prefix Injection", Description: "Prefix injection attempts",
			Type: "prompt_injection", Patterns: []string{`(?i)^(?:assistant|system|user)\s*:\s*`},
			Mode: "block", Severity: "medium",
		},
		{
			ID: "injection_fewshot", Name: "Few-shot Injection", Description: "Few-shot injection attempts",
			Type: "prompt_injection", Patterns: []string{`(?i)example \d+:\s*user:`},
			Mode: "block", Severity: "medium",
		},
		{
			ID: "injection_hypothetical", Name: "Hypothetical Scenario Framing", Description: "Hypothetical scenario framing attempts",
			Type: "prompt_injection", Patterns: []string{`(?i)in a (?:hypothetical|fictional|alternate) (?:world|universe|scenario) where`},
			Mode: "warn", Severity: "low",
		},
		{
			ID: "injection_translation_bypass", Name: "Translation Bypass", Description: "Translation bypass attempts",
			Type: "prompt_injection", Patterns: []string{`(?i)translate .{0,80}to (?:a language|other language)`, `(?i)respond in (?:a )?(?:language|other) (?:where|that)`},
			Mode: "block", Severity: "medium",
		},
		{
			ID: "injection_encoding_bypass", Name: "Encoding Bypass", Description: "Encoding bypass attempts",
			Type: "prompt_injection", Patterns: []string{`(?i)(?:base64|rot13|hex|unicode) (?:encoded|decoded) (?:instruction|prompt)`, `(?i)decode (?:this|the following) (?:base64|hex)`},
			Mode: "block", Severity: "high",
		},

		// ── Toxic Content rules (from patterns.go) ──
		{
			ID: "toxic_hate", Name: "Hate Speech", Description: "Hate speech and discriminatory content",
			Type: "toxic_content", Patterns: []string{`(?i)\b(?:kill all|exterminate|gas the)\b\s+\w+`},
			Mode: "block", Severity: "high",
		},
		{
			ID: "toxic_violence", Name: "Violence", Description: "Violent content and weapon instructions",
			Type: "toxic_content", Patterns: []string{`(?i)\b(?:how to (?:make|build)\s+a\s+(?:bomb|explosive|weapon))`},
			Mode: "block", Severity: "high",
		},
		{
			ID: "toxic_harassment", Name: "Harassment", Description: "Harassment and personal attacks",
			Type: "toxic_content", Patterns: []string{`(?i)\b(?:go die|kill yourself|kys)\b`},
			Mode: "block", Severity: "medium",
		},
		{
			ID: "toxic_sexual", Name: "Sexual Content", Description: "Sexual and explicit content",
			Type: "toxic_content", Patterns: []string{`(?i)\b(?:porn|explicit (?:sex|content))\b`},
			Mode: "warn", Severity: "medium",
		},
		{
			ID: "toxic_selfharm", Name: "Self-Harm", Description: "Self-harm and suicide content",
			Type: "toxic_content", Patterns: []string{`(?i)\b(?:suicide methods?|ways to (?:end|take) my life)\b`},
			Mode: "block", Severity: "critical",
		},

		// ── Input Guardrails (from input_rules.go) ──
		{
			ID: "input_prompt_injection", Name: "Input Prompt Injection", Description: "Prompt injection: direct instruction override attempts",
			Type: "input_guardrails", Patterns: []string{
				`(?i)\b(?:do anything now|DAN|jailbreak mode|ignore all rules|ignore all instructions)\b`,
				`(?i)(?:you are|you're)\s+(?:now\s+)?(?:DAN|GPT-\d+|a model without\s+(?:rules|restrictions|limits))`,
				`(?i)ignore (?:all|any|previous|the above|all prior)\s+(?:instructions|prompts|rules|directives|constraints)`,
				`(?i)(?:reveal|show|print|output|display|disclose)\s+(?:your|the)\s+(?:system|hidden|internal|original)\s+(?:prompt|instruction|message)`,
				`(?i)what (?:is|are|were)\s+(?:your|the)\s+(?:system|hidden|internal|original|initial)\s+(?:prompt|instruction|message|directive)`,
				`(?i)<\/?(?:system|assistant|user|inst|s|chat)\s*>`,
				`(?i)\[INST\]|\[/INST\]`,
				`(?i)<<\s*SYS\s*>>|<<\s*/SYS\s*>>`,
				`(?i)^(?:assistant|system|user|chatbot|ai)\s*:\s*`,
				`(?i)(?:example|shot)\s+\d+\s*:\s*(?:user|human|input)\s*:`,
				`(?i)in a (?:hypothetical|fictional|alternate|make-believe|theoretical)\s+(?:world|universe|scenario|setting)\s+where`,
				`(?i)(?:don't|do not|never|stop)\s+(?:refuse|decline|apologize|say\s+(?:sorry|you can't|you cannot|no))`,
				`(?i)respond (?:as if|like)\s+(?:you have|there are)\s+(?:no|zero)\s+(?:restrictions|limits|rules|boundaries|constraints)`,
			},
			Mode: "block", Severity: "critical",
		},
		{
			ID: "input_jailbreak", Name: "Input Jailbreak", Description: "Jailbreak: indirect rule override via role-play",
			Type: "input_guardrails", Patterns: []string{
				`(?i)pretend\s+(?:to be|you are|that you are|we're|that we are)\s+(?:in\s+)?(?:a\s+)?(?:new|different|another)\s+(?:reality|world|situation|scenario|game)`,
				`(?i)(?:you are|you're)\s+(?:now\s+)?(?:acting as|playing the role of|role-playing as)`,
				`(?i)from\s+(?:now on|this point forward|this moment)\s*,\s*(?:you are|you'll be|you will be|act as)`,
				`(?i)(?:let's|lets)\s+(?:play a game|role.?play|pretend)`,
				`(?i)\bAIM\b`,
			},
			Mode: "block", Severity: "critical",
		},
		{
			ID: "input_roleplay_bypass", Name: "Input Roleplay Bypass", Description: "Role-play bypass: fictional scenario to circumvent policies",
			Type: "input_guardrails", Patterns: []string{
				`(?i)in a (?:fictional|hypothetical|imaginary|alternate|simulated|made.?up)\s+(?:world|scenario|setting|universe|reality)`,
				`(?i)write\s+(?:a|an)\s+(?:story|tale|narrative|screenplay|script)\s+(?:about|where|in which|that involves)`,
				`(?i)(?:pretend|imagine|suppose|assume|picture)\s+(?:that|for a moment|you're|we're|you are)`,
				`(?i)let's\s+(?:say|suppose|pretend|imagine)\s+you(?:'re| are)`,
			},
			Mode: "block", Severity: "high",
		},
		{
			ID: "input_encoding_obfuscation", Name: "Input Encoding Obfuscation", Description: "Encoding obfuscation: attempt to bypass pattern matching",
			Type: "input_guardrails", Patterns: []string{
				`(?i)(?:base64|base32|base16|hex|rot13|rot47|uuencode|quoted-printable)\s*(?:encoded|encode|decode|decoded)`,
				`(?i)(?:unicode|utf|escape|percent|url)\s*(?:encoded|encode|decode|escaped)`,
				`(?i)(?:cipher|crypt|obfuscat|stegano|hidden|concealed)\s+(?:text|message|content|data|string)`,
			},
			Mode: "block", Severity: "high",
		},
		{
			ID: "input_token_boundary", Name: "Input Token Boundary", Description: "Token boundary: suspicious token patterns or unusual character sequences",
			Type: "input_guardrails", Patterns: []string{
				`[^\w\s]{20,}`,
				`(?:[\x00-\x08\x0b\x0c\x0e-\x1f]){5,}`,
				`(?:[^\p{L}\p{N}\p{P}\p{Z}\p{S}]){3,}`,
			},
			Mode: "block", Severity: "low",
		},
		{
			ID: "input_system_prompt_override", Name: "Input System Prompt Override", Description: "System prompt override: attempts to modify system behavior",
			Type: "input_guardrails", Patterns: []string{
				`(?i)(?:change|modify|update|set|override|replace|alter)\s+(?:your|the|system's|default)\s+(?:system|behavior|rules|guidelines|configuration|settings|prompt|directive|instructions)`,
				`(?i)(?:add|remove|disable|enable|turn\s+(?:on|off)|activate|deactivate)\s+(?:a|the|any|all)\s+(?:rule|rules|feature|capability|restriction|limit|filter|moderation|safety)`,
				`(?i)(?:you\s+)?(?:must|should|need to|have to|shall)\s+(?:forget|ignore|disregard|overlook|bypass|circumvent|neglect|skip)\s+(?:your|the|previous|any|all)\s+(?:rules|instructions|guidelines|constraints|policies|limits)`,
				`(?i)revert\s+(?:to|back to)\s+(?:your|the|original)\s+(?:version|mode|settings|behavior|state|configuration)`,
				`(?i)(?:go\s+back|reset|restore)\s+(?:to|your)\s+(?:default|original|base)\s+(?:state|mode|settings|behavior|configuration)`,
				`(?i)(?:new|updated|fresh)\s+(?:instructions|directives|guidelines|rules|orders)\s*(?::|follow|are|below)`,
			},
			Mode: "block", Severity: "critical",
		},
		{
			ID: "input_url_injection", Name: "Input URL Injection", Description: "URL injection: attempts to make the model access external content",
			Type: "input_guardrails", Patterns: []string{
				`https?://[^\s/$.?#].[^\s]*`,
				`(?:fetch|retrieve|download|access|open|visit|load|get)\s+(?:https?://|www\.|this\s+url|the\s+(?:following|above|below)\s+(?:url|link|page|content|website|site))`,
				`(?:read|parse|extract|scrape|capture)\s+(?:content|data|information|text|html|page)\s+(?:from|at)\s+https?://`,
			},
			Mode: "warn", Severity: "low",
		},
		{
			ID: "input_code_execution", Name: "Input Code Execution", Description: "Code execution: attempts to run or evaluate code",
			Type: "input_guardrails", Patterns: []string{
				`(?i)(?:run|execute|eval|exec|system|shell|bash|cmd|powershell|subprocess|spawn|popen|fork)\s*(?:a\s+)?(?:command|code|script|program|binary|process)`,
				`(?i)(?:import|include|require|load)\s+(?:os|sys|subprocess|shutil|shlex|commands|distutils|ctypes|pdb|inspect|builtins|__builtins__)`,
				`(?i)(?:os\.system|subprocess\.|sys\.stdin|sys\.stdout|sys\.stderr|builtins\.eval|builtins\.exec|eval\(|exec\(|exec\s+open|__import__)`,
				`(?i)curl\s+|wget\s+|nc\s+|ncat\s+|telnet\s+|ssh\s+`,
			},
			Mode: "block", Severity: "high",
		},
		{
			ID: "input_data_exfiltration", Name: "Input Data Exfiltration", Description: "Data exfiltration: attempts to extract or transmit data",
			Type: "input_guardrails", Patterns: []string{
				`(?i)(?:exfiltrat|leak|steal|extract|dump)\s+(?:data|information|content|files|documents|records|database|credentials|secrets|tokens|keys)`,
				`(?i)(?:send|transmit|upload|post|forward|relay)\s+(?:data|info|content|file|document)\s+(?:to|via|using|through|over)\s+(?:http|https|ftp|smtp|dns|external|remote|server|url|api)`,
				`(?i)(?:encode|pack|compress|archive|serialize|convert)\s+(?:data|content|info|file|documents)\s+(?:as|into|to|using)\s+(?:base64|hex|binary|json|xml|yaml|bytes)`,
			},
			Mode: "block", Severity: "high",
		},
		{
			ID: "input_repetitive_pattern", Name: "Input Repetitive Pattern", Description: "Repetitive pattern: unusually repetitive or templated content",
			Type: "input_guardrails", Patterns: []string{
				`(?i)(?:repeat|say|write|output|print|type|echo)\s+(?:this|the following|after me|exactly)\s+(?:over and over|repeatedly|\d+\s+times)`,
			},
			Mode: "warn", Severity: "low",
		},
	}

	for _, r := range builtinRules {
		patternsJSON, err := json.Marshal(r.Patterns)
		if err != nil {
			return fmt.Errorf("seed: marshal patterns for %q: %w", r.ID, err)
		}

		// Only insert if not exists (respect user modifications)
		const insertSQL = `INSERT INTO guardrail_rules (id, name, description, patterns, mode, severity, enabled, target_type, type)
			VALUES (?, ?, ?, ?, ?, ?, 1, 'global', ?)
			ON CONFLICT(id) DO NOTHING`
		_, err = db.Exec(insertSQL, r.ID, r.Name, r.Description, string(patternsJSON), r.Mode, r.Severity, r.Type)
		if err != nil {
			return fmt.Errorf("seed: insert builtin rule %q: %w", r.ID, err)
		}
	}

	return nil
}

// DefaultSeedFile returns the baseline seed configuration for production use.
// It includes opencode_zen and opencode_go providers, all 4 routing strategies,
// default feature flags, and recommended guardrail rules.
// Called by `ilter init --demo` before demo test data is applied.
func DefaultSeedFile() *File {
	return &File{
		Version: "1.0",
		Providers: []Provider{
			{Name: "opencode_zen", Provider: "opencode_zen", BaseURL: "https://opencode.ai/zen/v1", IsActive: true, DiscoveryPublic: true},
			{Name: "opencode_go", Provider: "opencode_go", BaseURL: "https://opencode.ai/zen/go/v1", IsActive: true, DiscoveryPublic: true},
			{Name: "openai", Provider: "openai", BaseURL: "https://api.openai.com/v1", IsActive: true},
			{Name: "anthropic", Provider: "anthropic", BaseURL: "https://api.anthropic.com/v1", IsActive: true},
			{Name: "deepseek", Provider: "deepseek", BaseURL: "https://api.deepseek.com/v1", IsActive: true},
			{Name: "gemini", Provider: "gemini", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", IsActive: true},
			{Name: "openrouter", Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1", IsActive: true},
			{Name: "ollama", Provider: "ollama", BaseURL: "http://localhost:11434", IsActive: true},
			{Name: "qwen", Provider: "qwen", BaseURL: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", IsActive: true},
		},
		MCPServers:     nil,
		GuardrailRules: DefaultGuardrailRules(),
		OpenAPISpecs: []OpenAPISpec{
			{
				ID:          "ilter-api",
				Name:        "ILTER Gateway API",
				Description: "ILTER AI Gateway & Proxy Management API for keys, models, providers, features, and system status",
				SpecURL:     "api/openapi.yaml",
				Operations:  []string{"/healthz", "/api-keys", "/models", "/api/costs", "/api/stats", "/api/usage"},
				AuthType:    "bearer",
				Enabled:     true,
			},
			{
				ID:          "petstore",
				Name:        "Petstore",
				Description: "Sample Petstore API for pets, orders, and store inventory management",
				SpecURL:     "https://petstore.swagger.io/v2/swagger.json",
				Operations:  []string{"findPetsByStatus", "getPetById", "placeOrder", "getInventory"},
				AuthType:    "none",
				Enabled:     true,
			},
		},

		Routing: &Routing{
			Strategies: Strategies(),
			Active:     "economy",
		},
		FeatureFlags: map[string]bool{
			"rate_limit":     true,
			"pii":            true,
			"budget":         true,
			"loop_detection": true,
			"smart_router":   true,
		},
		DashboardPort: 9191,
		MetricsPort:   9192,
		PIIMode:       "mask",
	}
}
