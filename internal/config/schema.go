// Package config provides configuration types and resolution for the ilter proxy.
//
// This file defines the runtime_config schema registry — a typed catalog of
// known configuration keys that can be set at runtime through the dashboard API.
// The schema enforces type safety at write time, preventing invalid values from
// being persisted to the stringly-typed runtime_config table.
package config

import (
	"fmt"
	"strconv"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────
// ValueType — the allowed types for schema-registered config keys.
// ─────────────────────────────────────────────────────────────────────

// ValueType enumerates the scalar types supported by the runtime_config schema.
type ValueType int

const (
	TypeString ValueType = iota
	TypeBool
	TypeInt
	TypeFloat64
)

func (vt ValueType) String() string {
	switch vt {
	case TypeString:
		return "string"
	case TypeBool:
		return "bool"
	case TypeInt:
		return "int"
	case TypeFloat64:
		return "float64"
	default:
		return fmt.Sprintf("ValueType(%d)", vt)
	}
}

// ─────────────────────────────────────────────────────────────────────
// SchemaEntry — a single registered runtime_config key.
// ─────────────────────────────────────────────────────────────────────

// SchemaEntry describes one known runtime_config key: its section, key name,
// expected value type, default value, and a human-readable description.
type SchemaEntry struct {
	Section     string    `json:"section"`
	Key         string    `json:"key"`
	Type        ValueType `json:"type"`
	Default     string    `json:"default,omitempty"`
	Description string    `json:"description,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────
// ConfigSchema — the canonical registry of known runtime_config keys.
// ─────────────────────────────────────────────────────────────────────

// ConfigSchema lists every runtime_config entry that the system recognizes.
// Keys not in this list are stored and round-tripped verbatim but receive
// no type validation.
var ConfigSchema = []SchemaEntry{
	// ── Feature toggles ──
	{Section: "feature", Key: "rate_limit", Type: TypeBool, Default: "true", Description: "Enable rate-limiting middleware"},
	{Section: "feature", Key: "pii", Type: TypeBool, Default: "true", Description: "Enable PII-masking middleware"},
	{Section: "feature", Key: "cache", Type: TypeBool, Default: "true", Description: "Enable semantic-cache middleware"},
	{Section: "feature", Key: "budget", Type: TypeBool, Default: "true", Description: "Enable budget-enforcer middleware"},
	{Section: "feature", Key: "loop_detection", Type: TypeBool, Default: "true", Description: "Enable loop-detection middleware"},

	// ── Cache overrides ──
	{Section: "cache", Key: "similarity_threshold", Type: TypeFloat64, Default: "0.70", Description: "Semantic-cache similarity threshold"},

	// ── Audit overrides ──
	{Section: "audit", Key: "retention_days", Type: TypeInt, Default: "90", Description: "Audit-log retention in days"},

	// ── Guardrails ──
	{Section: "guardrails", Key: "mode", Type: TypeString, Default: "block", Description: "Guardrails blocking mode (block/warn/mask)"},

	// ── Jobs ──
	{Section: "jobs", Key: "max_concurrent", Type: TypeInt, Default: "10", Description: "Maximum concurrent jobs"},
	{Section: "jobs", Key: "timeout_ms", Type: TypeInt, Default: "120000", Description: "Default job timeout in milliseconds"},

	// ── Server ──
	{Section: "server", Key: "port", Type: TypeInt, Default: "8181", Description: "HTTP server port (env: ILTER_SERVER_PORT)"},

	// ── Dashboard ──
	{Section: "dashboard", Key: "port", Type: TypeInt, Default: "9191", Description: "Dashboard HTTP port (ilter init)"},

	// ── Metrics ──
	{Section: "metrics", Key: "port", Type: TypeInt, Default: "9192", Description: "Metrics server port (ilter init)"},

	// ── PII ──
	{Section: "pii", Key: "mode", Type: TypeString, Default: "mask", Description: "PII masking mode (mask/block/reversible)"},

	// ── Logging ──
	{Section: "logging", Key: "format", Type: TypeString, Default: "console", Description: "Log output format (console/json)"},
}

// schemaIndex provides O(1) lookup of schema entries by "section:key".
var schemaIndex map[string]*SchemaEntry

func init() {
	schemaIndex = make(map[string]*SchemaEntry, len(ConfigSchema))
	for i := range ConfigSchema {
		idx := ConfigSchema[i].Section + ":" + ConfigSchema[i].Key
		schemaIndex[idx] = &ConfigSchema[i]
	}
}

// ─────────────────────────────────────────────────────────────────────
// LookupSchema returns the schema entry for a (section, key) pair.
// Returns nil when the key is not registered.
// ─────────────────────────────────────────────────────────────────────

func LookupSchema(section, key string) *SchemaEntry {
	return schemaIndex[section+":"+key]
}

// ─────────────────────────────────────────────────────────────────────
// ValidateConfig checks that value can be parsed to the type declared by
// the schema entry for (section, key). Unknown keys always pass (nil).
// ─────────────────────────────────────────────────────────────────────

func ValidateConfig(section, key, value string) error {
	entry := LookupSchema(section, key)
	if entry == nil {
		return nil // unknown key — allow through
	}
	return validateType(entry.Type, section, key, value)
}

func validateType(vt ValueType, section, key, value string) error {
	switch vt {
	case TypeString:
		return nil // any string is valid
	case TypeBool:
		switch strings.ToLower(value) {
		case "true", "false", "1", "0":
			return nil
		default:
			return fmt.Errorf("schema: %s/%s expects bool, got %q", section, key, value)
		}
	case TypeInt:
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			return fmt.Errorf("schema: %s/%s expects int, got %q", section, key, value)
		}
		return nil
	case TypeFloat64:
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			return fmt.Errorf("schema: %s/%s expects float64, got %q", section, key, value)
		}
		return nil
	default:
		return fmt.Errorf("schema: %s/%s has unsupported type %v", section, key, vt)
	}
}

// ─────────────────────────────────────────────────────────────────────
// ValidateTypedValue returns a ValidationResult for the value of a
// schema-registered key.  It fits the existing validation-pipeline
// contract so callers can use it interchangeably with the section-level
// ValidateRuntimeConfig.
// ─────────────────────────────────────────────────────────────────────

func ValidateTypedValue(section, key, value string) *ValidationResult {
	if err := ValidateConfig(section, key, value); err != nil {
		return &ValidationResult{
			Valid: false,
			Errors: []ValidationError{
				{Field: "value", Message: err.Error()},
			},
		}
	}
	return &ValidationResult{Valid: true}
}

// ─────────────────────────────────────────────────────────────────────
// ParseConfigValue parses the raw string from runtime_config into the
// Go type declared by the schema.  Unknown keys return the raw string.
// ─────────────────────────────────────────────────────────────────────

func ParseConfigValue(section, key, value string) any {
	entry := LookupSchema(section, key)
	if entry == nil {
		return value
	}
	switch entry.Type {
	case TypeString:
		return value
	case TypeBool:
		switch strings.ToLower(value) {
		case "true", "1":
			return true
		default:
			return false
		}
	case TypeInt:
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return value
		}
		return n
	case TypeFloat64:
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return value
		}
		return f
	default:
		return value
	}
}
