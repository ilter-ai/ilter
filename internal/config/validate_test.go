package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRuntimeConfig_UnknownSection(t *testing.T) {
	result, err := ValidateRuntimeConfig("bogus", []byte(`{}`))
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unknown config section")
}

func TestValidateRuntimeConfig_EmptyBody(t *testing.T) {
	result, err := ValidateRuntimeConfig("provider", nil)
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, "body", result.Errors[0].Field)
	assert.Contains(t, result.Errors[0].Message, "empty request body")
}

func TestValidateRuntimeConfig_Provider_Valid(t *testing.T) {
	data := `{"name":"my-openai","provider":"openai","base_url":"https://api.openai.com/v1","model":"gpt-4"}`
	result, err := ValidateRuntimeConfig("provider", []byte(data))
	require.NoError(t, err)
	assert.True(t, result.Valid)
	assert.Empty(t, result.Errors)
}

func TestValidateRuntimeConfig_Provider_MissingName(t *testing.T) {
	data := `{"provider":"openai"}`
	result, err := ValidateRuntimeConfig("provider", []byte(data))
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, "name", result.Errors[0].Field)
}

func TestValidateRuntimeConfig_Provider_MissingType(t *testing.T) {
	data := `{"name":"test"}`
	result, err := ValidateRuntimeConfig("provider", []byte(data))
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, "provider", result.Errors[0].Field)
}

func TestValidateRuntimeConfig_Provider_InvalidBaseURL(t *testing.T) {
	data := `{"name":"test","provider":"openai","base_url":"not-a-url"}`
	result, err := ValidateRuntimeConfig("provider", []byte(data))
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, "base_url", result.Errors[0].Field)
}

func TestValidateRuntimeConfig_MCPServer_Valid(t *testing.T) {
	data := `{"name":"my-server","endpoint_url":"https://example.com/sse"}`
	result, err := ValidateRuntimeConfig("mcp_server", []byte(data))
	require.NoError(t, err)
	assert.True(t, result.Valid)
	assert.Empty(t, result.Errors)
}

func TestValidateRuntimeConfig_MCPServer_MissingName(t *testing.T) {
	data := `{"endpoint_url":"https://example.com/sse"}`
	result, err := ValidateRuntimeConfig("mcp_server", []byte(data))
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, "name", result.Errors[0].Field)
}

func TestValidateRuntimeConfig_MCPServer_InvalidEndpoint(t *testing.T) {
	data := `{"name":"test","endpoint_url":"has space"}`
	result, err := ValidateRuntimeConfig("mcp_server", []byte(data))
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, "endpoint_url", result.Errors[0].Field)
}

func TestValidateRuntimeConfig_GuardrailRule_Valid(t *testing.T) {
	data := `{"name":"block-viagra","type":"topic_block","action":"block","severity":"high","pattern":"viagra","enabled":true}`
	result, err := ValidateRuntimeConfig("guardrail_rule", []byte(data))
	require.NoError(t, err)
	assert.True(t, result.Valid)
	assert.Empty(t, result.Errors)
}

func TestValidateRuntimeConfig_GuardrailRule_InvalidType(t *testing.T) {
	data := `{"name":"r1","type":"bogus","action":"block","severity":"low","pattern":"test","enabled":true}`
	result, err := ValidateRuntimeConfig("guardrail_rule", []byte(data))
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, "type", result.Errors[0].Field)
}

func TestValidateRuntimeConfig_GuardrailRule_InvalidAction(t *testing.T) {
	data := `{"name":"r1","type":"toxicity","action":"maybe","severity":"low","pattern":"test","enabled":true}`
	result, err := ValidateRuntimeConfig("guardrail_rule", []byte(data))
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, "action", result.Errors[0].Field)
}

func TestValidateRuntimeConfig_GuardrailRule_InvalidSeverity(t *testing.T) {
	data := `{"name":"r1","type":"toxicity","action":"block","severity":"extreme","pattern":"test","enabled":true}`
	result, err := ValidateRuntimeConfig("guardrail_rule", []byte(data))
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, "severity", result.Errors[0].Field)
}

func TestValidateRuntimeConfig_GuardrailRule_EmptyPattern(t *testing.T) {
	data := `{"name":"r1","type":"toxicity","action":"block","severity":"low","pattern":"","enabled":true}`
	result, err := ValidateRuntimeConfig("guardrail_rule", []byte(data))
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, "pattern", result.Errors[0].Field)
}

func TestValidateRuntimeConfig_GuardrailRule_MissingName(t *testing.T) {
	data := `{"type":"toxicity","action":"block","severity":"low","pattern":"test","enabled":true}`
	result, err := ValidateRuntimeConfig("guardrail_rule", []byte(data))
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, "name", result.Errors[0].Field)
}

func TestValidateRuntimeConfig_RoutingStrategy_Valid(t *testing.T) {
	data := `{"load_balancer_strategy":"weighted-random"}`
	result, err := ValidateRuntimeConfig("routing_strategy", []byte(data))
	require.NoError(t, err)
	assert.True(t, result.Valid)
	assert.Empty(t, result.Errors)
}

func TestValidateRuntimeConfig_RoutingStrategy_InvalidName(t *testing.T) {
	data := `{"load_balancer_strategy":"bogo_sort"}`
	result, err := ValidateRuntimeConfig("routing_strategy", []byte(data))
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, "load_balancer_strategy", result.Errors[0].Field)
}

func TestValidateRuntimeConfig_RoutingStrategy_ValidEnabled(t *testing.T) {
	data := `{"enabled":true,"load_balancer_strategy":"weighted-random","scorer":{"type":"heuristic"}}`
	result, err := ValidateRuntimeConfig("routing_strategy", []byte(data))
	require.NoError(t, err)
	assert.True(t, result.Valid)
	assert.Empty(t, result.Errors)
}

func TestValidateRuntimeConfig_RoutingStrategy_InvalidScorer(t *testing.T) {
	data := `{"enabled":true,"load_balancer_strategy":"weighted-random","scorer":{"type":"astrology"}}`
	result, err := ValidateRuntimeConfig("routing_strategy", []byte(data))
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, "scorer.type", result.Errors[0].Field)
}

func TestValidateRuntimeConfig_FeatureFlag_Valid(t *testing.T) {
	data := `{"name":"rate_limit","enabled":true}`
	result, err := ValidateRuntimeConfig("feature_flag", []byte(data))
	require.NoError(t, err)
	assert.True(t, result.Valid)
	assert.Empty(t, result.Errors)
}

func TestValidateRuntimeConfig_FeatureFlag_UnknownName(t *testing.T) {
	data := `{"name":"bogus_flag","enabled":true}`
	result, err := ValidateRuntimeConfig("feature_flag", []byte(data))
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, "name", result.Errors[0].Field)
}

func TestValidateRuntimeConfig_FeatureFlag_EmptyName(t *testing.T) {
	data := `{"name":"","enabled":true}`
	result, err := ValidateRuntimeConfig("feature_flag", []byte(data))
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 1)
}

func TestValidateRuntimeConfig_FeatureFlag_BareBoolean(t *testing.T) {
	// When the store persists a feature flag, it stores the raw boolean value.
	// Validate should accept bare booleans.
	result, err := ValidateRuntimeConfig("feature_flag", []byte(`true`))
	require.NoError(t, err)
	assert.True(t, result.Valid)
	assert.Empty(t, result.Errors)
}

func TestValidateRuntimeConfig_OpenAPITool_Valid(t *testing.T) {
	data := `{"name":"weather","spec_url":"https://api.example.com/openapi.json","operations":["getWeather"]}`
	result, err := ValidateRuntimeConfig("openapi_tool", []byte(data))
	require.NoError(t, err)
	assert.True(t, result.Valid)
	assert.Empty(t, result.Errors)
}

func TestValidateRuntimeConfig_OpenAPITool_MissingName(t *testing.T) {
	data := `{"spec_url":"https://example.com/spec.json","operations":["op1"]}`
	result, err := ValidateRuntimeConfig("openapi_tool", []byte(data))
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, "name", result.Errors[0].Field)
}

func TestValidateRuntimeConfig_OpenAPITool_MissingSpecURL(t *testing.T) {
	data := `{"name":"tool1","operations":["op1"]}`
	result, err := ValidateRuntimeConfig("openapi_tool", []byte(data))
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, "spec_url", result.Errors[0].Field)
}

func TestValidateRuntimeConfig_OpenAPITool_EmptyOperations(t *testing.T) {
	data := `{"name":"tool1","spec_url":"https://example.com/spec.json","operations":[]}`
	result, err := ValidateRuntimeConfig("openapi_tool", []byte(data))
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, "operations", result.Errors[0].Field)
}

func TestValidateRuntimeConfig_OpenAPITool_InvalidSpecURL(t *testing.T) {
	data := `{"name":"tool1","spec_url":"\t\n","operations":["op1"]}`
	result, err := ValidateRuntimeConfig("openapi_tool", []byte(data))
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, "spec_url", result.Errors[0].Field)
}

func TestValidationResult_JSON(t *testing.T) {
	r := ValidationResult{
		Valid:  false,
		Errors: []ValidationError{{Field: "name", Message: "name is required"}},
	}
	b, err := json.Marshal(r)
	require.NoError(t, err)

	var decoded ValidationResult
	err = json.Unmarshal(b, &decoded)
	require.NoError(t, err)
	assert.False(t, decoded.Valid)
	assert.Len(t, decoded.Errors, 1)
	assert.Equal(t, "name", decoded.Errors[0].Field)
}

func TestValidationResult_ValidJSON(t *testing.T) {
	r := ValidationResult{Valid: true}
	b, err := json.Marshal(r)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"valid":true`)
}

func TestValidateRuntimeConfig_AllSectionsAcceptValidInput(t *testing.T) {
	tests := []struct {
		section string
		json    string
	}{
		{"provider", `{"name":"p1","provider":"openai"}`},
		{"mcp_server", `{"name":"m1"}`},
		{"guardrail_rule", `{"name":"g1","type":"topic_block","action":"block","severity":"low","pattern":"test","enabled":true}`},
		{"routing_strategy", `{"load_balancer_strategy":"latency-optimized"}`},
		{"feature_flag", `{"name":"semantic_cache","enabled":true}`},
		{"openapi_tool", `{"name":"o1","spec_url":"https://example.com/spec.json","operations":["op1"]}`},
	}

	for _, tt := range tests {
		t.Run(tt.section, func(t *testing.T) {
			result, err := ValidateRuntimeConfig(tt.section, []byte(tt.json))
			require.NoError(t, err)
			assert.True(t, result.Valid, "section %q should be valid", tt.section)
			assert.Empty(t, result.Errors)
		})
	}
}
