package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────
// LookupSchema
// ─────────────────────────────────────────────────────────────────────

func TestLookupSchema_KnownKey(t *testing.T) {
	entry := LookupSchema("feature", "rate_limit")
	require.NotNil(t, entry)
	assert.Equal(t, "feature", entry.Section)
	assert.Equal(t, "rate_limit", entry.Key)
	assert.Equal(t, TypeBool, entry.Type)
}

func TestLookupSchema_UnknownKey(t *testing.T) {
	entry := LookupSchema("bogus", "key")
	assert.Nil(t, entry)
}

func TestLookupSchema_AllRegisteredKeysAreFindable(t *testing.T) {
	for _, e := range ConfigSchema {
		entry := LookupSchema(e.Section, e.Key)
		require.NotNil(t, entry, "schema entry %s/%s should be findable", e.Section, e.Key)
		assert.Equal(t, e.Type, entry.Type)
	}
}

// ─────────────────────────────────────────────────────────────────────
// ValidateConfig — type validation
// ─────────────────────────────────────────────────────────────────────

func TestValidateConfig_UnknownKey_ReturnsNil(t *testing.T) {
	err := ValidateConfig("unknown", "key", "anything")
	assert.NoError(t, err)
}

func TestValidateConfig_Bool_Valid(t *testing.T) {
	for _, v := range []string{"true", "false", "True", "FALSE", "1", "0"} {
		err := ValidateConfig("feature", "rate_limit", v)
		assert.NoError(t, err, "value %q should be valid bool", v)
	}
}

func TestValidateConfig_Bool_Invalid(t *testing.T) {
	err := ValidateConfig("feature", "rate_limit", "not_a_bool")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expects bool")
}

func TestValidateConfig_Int_Valid(t *testing.T) {
	for _, v := range []string{"0", "42", "-1", "999999999"} {
		err := ValidateConfig("audit", "retention_days", v)
		assert.NoError(t, err, "value %q should be valid int", v)
	}
}

func TestValidateConfig_Int_Invalid(t *testing.T) {
	err := ValidateConfig("audit", "retention_days", "not_an_int")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expects int")
}

func TestValidateConfig_Float64_Valid(t *testing.T) {
	for _, v := range []string{"0.70", "1.0", "0", "3.14159", "-0.5"} {
		err := ValidateConfig("cache", "similarity_threshold", v)
		assert.NoError(t, err, "value %q should be valid float64", v)
	}
}

func TestValidateConfig_Float64_Invalid(t *testing.T) {
	err := ValidateConfig("cache", "similarity_threshold", "not_a_float")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expects float64")
}

func TestValidateConfig_String_AlwaysValid(t *testing.T) {
	err := ValidateConfig("guardrails", "mode", "block")
	assert.NoError(t, err)
	err = ValidateConfig("guardrails", "mode", "")
	assert.NoError(t, err)
	err = ValidateConfig("guardrails", "mode", "anything_at_all")
	assert.NoError(t, err)
}

// ─────────────────────────────────────────────────────────────────────
// ValidateTypedValue — pipeline-compatible validation
// ─────────────────────────────────────────────────────────────────────

func TestValidateTypedValue_Valid(t *testing.T) {
	result := ValidateTypedValue("feature", "cache", "true")
	require.NotNil(t, result)
	assert.True(t, result.Valid)
	assert.Empty(t, result.Errors)
}

func TestValidateTypedValue_Invalid(t *testing.T) {
	result := ValidateTypedValue("feature", "cache", "wrong")
	require.NotNil(t, result)
	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, "value", result.Errors[0].Field)
}

func TestValidateTypedValue_UnknownKey(t *testing.T) {
	result := ValidateTypedValue("bogus", "key", "anything")
	require.NotNil(t, result)
	assert.True(t, result.Valid)
	assert.Empty(t, result.Errors)
}

// ─────────────────────────────────────────────────────────────────────
// ParseConfigValue
// ─────────────────────────────────────────────────────────────────────

func TestParseConfigValue_Bool(t *testing.T) {
	assert.Equal(t, true, ParseConfigValue("feature", "rate_limit", "true"))
	assert.Equal(t, false, ParseConfigValue("feature", "rate_limit", "false"))
	assert.Equal(t, false, ParseConfigValue("feature", "rate_limit", "0"))
	assert.Equal(t, true, ParseConfigValue("feature", "rate_limit", "1"))
}

func TestParseConfigValue_Int(t *testing.T) {
	assert.Equal(t, int64(90), ParseConfigValue("audit", "retention_days", "90"))
	assert.Equal(t, int64(0), ParseConfigValue("audit", "retention_days", "0"))
	assert.Equal(t, int64(-1), ParseConfigValue("audit", "retention_days", "-1"))
}

func TestParseConfigValue_Float64(t *testing.T) {
	assert.InDelta(t, 0.70, ParseConfigValue("cache", "similarity_threshold", "0.70"), 0.001)
	assert.InDelta(t, 1.0, ParseConfigValue("cache", "similarity_threshold", "1.0"), 0.001)
}

func TestParseConfigValue_String(t *testing.T) {
	assert.Equal(t, "block", ParseConfigValue("guardrails", "mode", "block"))
	assert.Equal(t, "warn", ParseConfigValue("guardrails", "mode", "warn"))
}

func TestParseConfigValue_UnknownKey_ReturnsRawString(t *testing.T) {
	assert.Equal(t, "myvalue", ParseConfigValue("unknown", "key", "myvalue"))
}

// ─────────────────────────────────────────────────────────────────────
// ConfigSchema snapshot
// ─────────────────────────────────────────────────────────────────────

func TestConfigSchema_AllEntriesHaveSectionAndKey(t *testing.T) {
	for _, e := range ConfigSchema {
		assert.NotEmpty(t, e.Section, "entry %.10v... has empty section", e)
		assert.NotEmpty(t, e.Key, "entry in section %q has empty key", e.Section)
	}
}

func TestConfigSchema_JSONSerialization(t *testing.T) {
	data, err := json.Marshal(ConfigSchema)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"section":"feature"`)
	assert.Contains(t, string(data), `"type":1`) // TypeBool = 1

	var decoded []SchemaEntry
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Len(t, decoded, len(ConfigSchema))
}

// ─────────────────────────────────────────────────────────────────────
// Schema integration with config loading pipeline
// ─────────────────────────────────────────────────────────────────────

func TestSchemaIntegration_ResolveRuntime_MergesOverrideValues(t *testing.T) {
	boot := DefaultBootConfig()

	state := &StateConfig{
		RuntimeConfigValues: map[string]string{
			"cache:similarity_threshold": "0.85",
			"audit:retention_days":       "180",
			"guardrails:mode":            "warn",
			"feature:rate_limit":         "false",
			"feature:cache":              "true",
		},
	}

	snap := ResolveRuntime(&boot, state)
	require.NotNil(t, snap)

	assert.InDelta(t, 0.85, snap.CacheSimilarityThreshold, 0.001)
	assert.Equal(t, int64(180), snap.AuditRetentionDays)
	assert.Equal(t, "warn", snap.GuardrailsMode)
}

func TestSchemaIntegration_UnsetRuntimeConfig_LeavesBootDefaults(t *testing.T) {
	boot := DefaultBootConfig()

	// State with no RuntimeConfigValues — should not touch boot defaults.
	state := &StateConfig{}

	snap := ResolveRuntime(&boot, state)
	require.NotNil(t, snap)

	// These should still be the boot defaults since RuntimeConfigValues is nil/empty.
	assert.InDelta(t, 0.70, snap.CacheSimilarityThreshold, 0.001)
	assert.Equal(t, int64(90), snap.AuditRetentionDays)
	assert.Equal(t, "", snap.GuardrailsMode) // DefaultBootConfig does not set Guardrails.Mode
}

func TestSchemaIntegration_TypedPointersStillWin(t *testing.T) {
	boot := DefaultBootConfig()

	// Both typed pointers and RuntimeConfigValues set.
	val := 0.95
	days := int64(365)
	mode := "mask"
	state := &StateConfig{
		CacheSimilarityThreshold: &val,
		AuditRetentionDays:       &days,
		GuardrailsMode:           &mode,
		RuntimeConfigValues: map[string]string{
			"cache:similarity_threshold": "0.50",
			"audit:retention_days":       "30",
			"guardrails:mode":            "block",
		},
	}

	snap := ResolveRuntime(&boot, state)

	// Typed pointers win over RuntimeConfigValues (typed pointers are checked first).
	assert.InDelta(t, 0.95, snap.CacheSimilarityThreshold, 0.001)
	assert.Equal(t, int64(365), snap.AuditRetentionDays)
	assert.Equal(t, "mask", snap.GuardrailsMode)
}

// ─────────────────────────────────────────────────────────────────────
// Schema Validation on API create/update body parsing
// ─────────────────────────────────────────────────────────────────────

func TestValidateConfig_AllRegisteredTypes(t *testing.T) {
	tests := []struct {
		section string
		key     string
		valid   string
		invalid string
	}{
		{section: "feature", key: "rate_limit", valid: "true", invalid: "maybe"},
		{section: "feature", key: "pii", valid: "false", invalid: "yes"},
		{section: "cache", key: "similarity_threshold", valid: "0.75", invalid: "high"},
		{section: "audit", key: "retention_days", valid: "30", invalid: "many"},
		{section: "guardrails", key: "mode", valid: "block", invalid: ""}, // strings always valid
		{section: "jobs", key: "max_concurrent", valid: "5", invalid: "lots"},
		{section: "server", key: "port", valid: "8080", invalid: "eighteen"},
		{section: "dashboard", key: "port", valid: "9191", invalid: "-1a"},
		{section: "metrics", key: "port", valid: "9192", invalid: "twenty"},
	}

	for _, tt := range tests {
		t.Run(tt.section+"/"+tt.key+"_valid", func(t *testing.T) {
			err := ValidateConfig(tt.section, tt.key, tt.valid)
			assert.NoError(t, err, "%s/%s should accept %q", tt.section, tt.key, tt.valid)
		})

		if tt.invalid != "" {
			t.Run(tt.section+"/"+tt.key+"_invalid", func(t *testing.T) {
				err := ValidateConfig(tt.section, tt.key, tt.invalid)
				assert.Error(t, err, "%s/%s should reject %q", tt.section, tt.key, tt.invalid)
			})
		}
	}
}
