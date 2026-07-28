package openapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model"
)

// TestLLMToolCallPayloads verifies that ToolProvider.Execute correctly parses
// all variations of tool call arguments emitted by LLM models (DeepSeek, Claude, GPT-4, Llama),
// executes the calls, and returns messages properly formatted for LLM consumption.
func TestLLMToolCallPayloads(t *testing.T) {
	// 1. Setup mock upstream HTTP server for petstore API
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/store/inventory":
			_, _ = w.Write([]byte(`{"available": 10, "pending": 2, "sold": 5}`))
		case "/pets/123":
			_, _ = w.Write([]byte(`{"id": 123, "name": "Fluffy", "status": "available"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error": "not found"}`))
		}
	}))
	defer srv.Close()

	// Load test spec using mock server URL
	specYaml := strings.ReplaceAll(petstoreSpec(), "http://localhost:9999", srv.URL)
	specPath := writeTempSpec(t, specYaml)

	specs := []config.OpenAPISpecConfig{
		{
			Name:    "Petstore",
			SpecURL: specPath,
		},
	}

	provider, err := NewToolProvider(specs)
	require.NoError(t, err)
	require.NotNil(t, provider)

	// -------------------------------------------------------------------------
	// Test Cases: LLM Argument Formats & Variations
	// -------------------------------------------------------------------------
	tests := []struct {
		name              string
		toolName          string
		argumentsJSON     string
		wantErrorPrefix   bool
		validateContentFn func(t *testing.T, content string)
	}{
		// --- openapi_search variations ---
		{
			name:          "openapi_search: standard raw JSON object",
			toolName:      "openapi_search",
			argumentsJSON: `{"intent": "inventory", "api": "Petstore"}`,
			validateContentFn: func(t *testing.T, content string) {
				var results []SearchResult
				err := json.Unmarshal([]byte(content), &results)
				require.NoError(t, err)
				require.NotEmpty(t, results)
				assert.Equal(t, "Petstore_getInventory", results[0].OperationID)
			},
		},
		{
			name:            "openapi_search: missing required intent parameter",
			toolName:        "openapi_search",
			argumentsJSON:   `{"api": "Petstore"}`,
			wantErrorPrefix: true,
			validateContentFn: func(t *testing.T, content string) {
				assert.Contains(t, content, "missing required parameter 'intent'")
			},
		},

		// --- openapi_describe variations ---
		{
			name:          "openapi_describe: standard JSON array",
			toolName:      "openapi_describe",
			argumentsJSON: `{"operation_ids": ["Petstore_getInventory"]}`,
			validateContentFn: func(t *testing.T, content string) {
				var results []map[string]any
				err := json.Unmarshal([]byte(content), &results)
				require.NoError(t, err)
				require.Len(t, results, 1)
				assert.Equal(t, "Petstore_getInventory", results[0]["operation_id"])
			},
		},
		{
			name:          "openapi_describe: LLM stringified JSON array",
			toolName:      "openapi_describe",
			argumentsJSON: `{"operation_ids": "[\"Petstore_getInventory\"]"}`,
			validateContentFn: func(t *testing.T, content string) {
				var results []map[string]any
				err := json.Unmarshal([]byte(content), &results)
				require.NoError(t, err)
				require.Len(t, results, 1)
				assert.Equal(t, "Petstore_getInventory", results[0]["operation_id"])
			},
		},
		{
			name:          "openapi_describe: LLM single string operation ID",
			toolName:      "openapi_describe",
			argumentsJSON: `{"operation_ids": "Petstore_getInventory"}`,
			validateContentFn: func(t *testing.T, content string) {
				var results []map[string]any
				err := json.Unmarshal([]byte(content), &results)
				require.NoError(t, err)
				require.Len(t, results, 1)
				assert.Equal(t, "Petstore_getInventory", results[0]["operation_id"])
			},
		},
		{
			name:          "openapi_describe: non-existent operation ID",
			toolName:      "openapi_describe",
			argumentsJSON: `{"operation_ids": ["NonExistentOp"]}`,
			validateContentFn: func(t *testing.T, content string) {
				var results []map[string]string
				err := json.Unmarshal([]byte(content), &results)
				require.NoError(t, err)
				require.Len(t, results, 1)
				assert.Equal(t, "NonExistentOp", results[0]["operation_id"])
				assert.Equal(t, "operation not found", results[0]["error"])
			},
		},

		// --- openapi_call variations ---
		{
			name:          "openapi_call: standard raw JSON params object",
			toolName:      "openapi_call",
			argumentsJSON: `{"operation_id": "Petstore_getInventory", "params": {}}`,
			validateContentFn: func(t *testing.T, content string) {
				assert.Contains(t, content, `"available": 10`)
			},
		},
		{
			name:          "openapi_call: LLM stringified empty params object \"{}\"",
			toolName:      "openapi_call",
			argumentsJSON: `{"operation_id": "Petstore_getInventory", "params": "{}"}`,
			validateContentFn: func(t *testing.T, content string) {
				assert.Contains(t, content, `"available": 10`)
			},
		},
		{
			name:          "openapi_call: LLM missing params parameter",
			toolName:      "openapi_call",
			argumentsJSON: `{"operation_id": "Petstore_getInventory"}`,
			validateContentFn: func(t *testing.T, content string) {
				assert.Contains(t, content, `"available": 10`)
			},
		},
		{
			name:          "openapi_call: LLM stringified non-empty params object",
			toolName:      "openapi_call",
			argumentsJSON: `{"operation_id": "Petstore_getPetById", "params": "{\"petId\": \"123\"}"}`,
			validateContentFn: func(t *testing.T, content string) {
				assert.Contains(t, content, `"name": "Fluffy"`)
			},
		},
		{
			name:            "openapi_call: invalid operation ID",
			toolName:        "openapi_call",
			argumentsJSON:   `{"operation_id": "InvalidOp", "params": {}}`,
			wantErrorPrefix: true,
			validateContentFn: func(t *testing.T, content string) {
				assert.Contains(t, content, `Operation "InvalidOp" not found`)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callID := "call_test_1"
			calls := []model.ToolCall{
				{
					ID:   callID,
					Type: "function",
					Function: model.ToolCallFunctionData{
						Name:      tt.toolName,
						Arguments: tt.argumentsJSON,
					},
				},
			}

			msgs, errFlags := provider.Execute(context.Background(), "user_1", "key_1", calls)

			// Assertions for OpenAI compatibility & LLM consumption
			require.Len(t, msgs, 2, "Execute should return assistant msg + tool result msg")
			require.Len(t, errFlags, 1)

			toolResponseMsg := msgs[1]
			assert.Equal(t, "tool", toolResponseMsg.Role, "LLM expects role='tool' for tool responses")
			assert.Equal(t, callID, toolResponseMsg.ToolCallID, "LLM expects tool_call_id matching request ID")

			contentStr, ok := toolResponseMsg.Content.(string)
			require.True(t, ok, "tool response content must be a string")
			assert.NotEmpty(t, contentStr, "tool response content must not be empty")

			if tt.wantErrorPrefix {
				assert.True(t, errFlags[0], "errFlags should be true for error responses")
				assert.True(t, strings.HasPrefix(contentStr, "Error:"), "error content should start with 'Error:'")
			} else {
				assert.False(t, errFlags[0], "errFlags should be false for successful responses")
			}

			tt.validateContentFn(t, contentStr)
		})
	}
}
