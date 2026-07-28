package model

import (
	"fmt"
	"slices"
)

// Guardrail rule type constants. These classify what kind of rule
// a GuardrailRule represents.
const (
	GuardrailTypePromptInjection = "prompt_injection"
	GuardrailTypeToxicity        = "toxicity"
	GuardrailTypeTopicBlock      = "topic_block"
	GuardrailTypePIIMask         = "pii_mask"
)

// Guardrail action constants. These define what happens when a rule matches.
const (
	GuardrailActionBlock = "block"
	GuardrailActionWarn  = "warn"
	GuardrailActionMask  = "mask"
)

// Guardrail severity constants. These describe the severity level of a match.
const (
	GuardrailSeverityLow      = "low"
	GuardrailSeverityMedium   = "medium"
	GuardrailSeverityHigh     = "high"
	GuardrailSeverityCritical = "critical"
)

// ValidGuardrailTypes is the set of known guardrail rule types.
var ValidGuardrailTypes = []string{
	GuardrailTypePromptInjection,
	GuardrailTypeToxicity,
	GuardrailTypeTopicBlock,
	GuardrailTypePIIMask,
}

// ValidGuardrailActions is the set of known guardrail rule actions.
var ValidGuardrailActions = []string{
	GuardrailActionBlock,
	GuardrailActionWarn,
	GuardrailActionMask,
}

// ValidGuardrailSeverities is the set of known guardrail rule severities.
var ValidGuardrailSeverities = []string{
	GuardrailSeverityLow,
	GuardrailSeverityMedium,
	GuardrailSeverityHigh,
	GuardrailSeverityCritical,
}

// GuardrailRule is the model for a single guardrail rule stored in the
// runtime_config table. Each rule captures one check (type + pattern) and
// the action to take when the pattern matches.
type GuardrailRule struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Pattern  string `json:"pattern"`
	Action   string `json:"action"`
	Priority int    `json:"priority"`
	Enabled  bool   `json:"enabled"`
	Severity string `json:"severity"`
}

// Validate returns a descriptive error if the rule has invalid fields.
// It checks type, action, and severity against known sets and ensures
// the pattern is non-empty.
func (r *GuardrailRule) Validate() error {
	if r.Name == "" {
		return fmt.Errorf("guardrail rule validation failed: name is required")
	}
	if !isValidEnum(r.Type, ValidGuardrailTypes) {
		return fmt.Errorf(
			"guardrail rule validation failed: invalid type %q; valid types: %v",
			r.Type, ValidGuardrailTypes,
		)
	}
	if !isValidEnum(r.Action, ValidGuardrailActions) {
		return fmt.Errorf(
			"guardrail rule validation failed: invalid action %q; valid actions: %v",
			r.Action, ValidGuardrailActions,
		)
	}
	if !isValidEnum(r.Severity, ValidGuardrailSeverities) {
		return fmt.Errorf(
			"guardrail rule validation failed: invalid severity %q; valid severities: %v",
			r.Severity, ValidGuardrailSeverities,
		)
	}
	if r.Pattern == "" {
		return fmt.Errorf("guardrail rule validation failed: pattern is required")
	}
	return nil
}

// isValidEnum checks whether value is in the allowed set.
func isValidEnum(value string, valid []string) bool {
	return slices.Contains(valid, value)
}
