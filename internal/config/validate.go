package config

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/ilter-ai/ilter/internal/model"
)

// KnownConfigSections is the set of recognized runtime_config section names
// that ValidateRuntimeConfig can validate.
var KnownConfigSections = []string{
	"provider",
	"mcp_server",
	"guardrail_rule",
	"routing_strategy",
	"feature_flag",
	"openapi_tool",
}

// ValidationError holds a single field-level validation error.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationResult is the structured outcome of ValidateRuntimeConfig.
type ValidationResult struct {
	Valid  bool              `json:"valid"`
	Errors []ValidationError `json:"errors,omitempty"`
}

// ValidateRuntimeConfig validates raw JSON for a given runtime_config section.
// It unmarshals the JSON into the appropriate model struct and calls its
// Validate() method, returning structured field-level errors.
//
// Supported sections and their model types:
//   - "provider"        → model.ProviderRegistration
//   - "mcp_server"      → model.MCPServer
//   - "guardrail_rule"  → model.GuardrailRule
//   - "routing_strategy" → model.RoutingStrategy
//   - "feature_flag"    → model.FeatureFlag
//   - "openapi_tool"    → model.OpenAPITool
func ValidateRuntimeConfig(section string, rawJSON []byte) (*ValidationResult, error) {
	if !slices.Contains(KnownConfigSections, section) {
		return nil, fmt.Errorf("unknown config section %q: must be one of %v", section, KnownConfigSections)
	}

	if len(rawJSON) == 0 {
		return &ValidationResult{
			Errors: []ValidationError{{Field: "body", Message: "empty request body"}},
		}, nil
	}

	switch section {
	case "provider":
		return validateProvider(rawJSON)
	case "mcp_server":
		return validateMCPServer(rawJSON)
	case "guardrail_rule":
		return validateGuardrailRule(rawJSON)
	case "routing_strategy":
		return validateRoutingStrategy(rawJSON)
	case "feature_flag":
		return validateFeatureFlag(rawJSON)
	case "openapi_tool":
		return validateOpenAPITool(rawJSON)
	default:
		return nil, fmt.Errorf("unknown config section %q", section)
	}
}

// ─────────────────────────────────────────────────────────────────────
// Per-section validators
// ─────────────────────────────────────────────────────────────────────

func validateProvider(raw []byte) (*ValidationResult, error) {
	var p model.ProviderRegistration
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("unmarshal provider: %w", err)
	}
	if err := p.Validate(); err != nil {
		return resultFromErr(err)
	}
	return &ValidationResult{Valid: true}, nil
}

func validateMCPServer(raw []byte) (*ValidationResult, error) {
	var s model.MCPServer
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("unmarshal mcp_server: %w", err)
	}
	if err := s.Validate(); err != nil {
		return resultFromErr(err)
	}
	return &ValidationResult{Valid: true}, nil
}

func validateGuardrailRule(raw []byte) (*ValidationResult, error) {
	var r model.GuardrailRule
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("unmarshal guardrail_rule: %w", err)
	}
	if err := r.Validate(); err != nil {
		return resultFromErr(err)
	}
	return &ValidationResult{Valid: true}, nil
}

func validateRoutingStrategy(raw []byte) (*ValidationResult, error) {
	var rs model.RoutingStrategy
	if err := json.Unmarshal(raw, &rs); err != nil {
		return nil, fmt.Errorf("unmarshal routing_strategy: %w", err)
	}
	if err := rs.Validate(); err != nil {
		return resultFromErr(err)
	}
	return &ValidationResult{Valid: true}, nil
}

func validateFeatureFlag(raw []byte) (*ValidationResult, error) {
	// Feature flags can be submitted either as a full FeatureFlag object
	// or as a raw boolean (for simple on/off toggles).
	var ff model.FeatureFlag
	if err := json.Unmarshal(raw, &ff); err != nil {
		// Try parsing as a bare boolean: {name: "...", enabled: true}
		// We can't distinguish from the unmarshal error alone, so try
		// the flattened store format: just a boolean value.
		var val bool
		if err2 := json.Unmarshal(raw, &val); err2 == nil {
			// Bare boolean — name unknown, but the value is valid.
			return &ValidationResult{Valid: true}, nil
		}
		return nil, fmt.Errorf("unmarshal feature_flag: %w", err)
	}
	if err := ff.Validate(); err != nil {
		return resultFromErr(err)
	}
	return &ValidationResult{Valid: true}, nil
}

func validateOpenAPITool(raw []byte) (*ValidationResult, error) {
	var t model.OpenAPITool
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, fmt.Errorf("unmarshal openapi_tool: %w", err)
	}
	if err := t.Validate(); err != nil {
		return resultFromErr(err)
	}
	return &ValidationResult{Valid: true}, nil
}

// resultFromErr converts a Validate() error into a ValidationResult
// with field-level error detail, extracting the field name from the
// error message when possible.
func resultFromErr(err error) (*ValidationResult, error) {
	msg := err.Error()
	field := extractField(msg)
	return &ValidationResult{
		Errors: []ValidationError{{Field: field, Message: msg}},
	}, nil
}

// extractField attempts to extract a field name from a validation error
// message. It returns common field names found in known patterns, or
// "body" as a fallback.
func extractField(msg string) string {
	// Check patterns from model Validate() error messages.
	// Order matters: more specific checks first.
	switch {
	case strings.Contains(msg, "provider name"):
		return "name"
	case strings.Contains(msg, "provider type"):
		return "provider"
	case strings.Contains(msg, "invalid base_url"):
		return "base_url"
	case strings.Contains(msg, "invalid endpoint_url"):
		return "endpoint_url"
	case strings.Contains(msg, "spec_url"):
		return "spec_url"
	case strings.Contains(msg, "invalid type"):
		return "type"
	case strings.Contains(msg, "invalid action"):
		return "action"
	case strings.Contains(msg, "invalid severity"):
		return "severity"
	case strings.Contains(msg, "pattern is required"):
		return "pattern"
	case strings.Contains(msg, "load balancer strategy"):
		return "load_balancer_strategy"
	case strings.Contains(msg, "invalid scorer type"):
		return "scorer.type"
	case strings.Contains(msg, "at least one operation"):
		return "operations"
	case strings.Contains(msg, "feature flag name"):
		return "name"
	case strings.Contains(msg, "unknown feature flag"):
		return "name"
	case strings.Contains(msg, "name is required"):
		return "name"
	case strings.Contains(msg, "empty request body"):
		return "body"
	default:
		return "body"
	}
}
