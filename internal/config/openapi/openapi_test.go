package openapi

import (
	"context"
	"encoding/json"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// writeTempSpec writes YAML content to a temp file and returns the path.
func writeTempSpec(t *testing.T, yamlContent string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.yaml")
	err := os.WriteFile(path, []byte(yamlContent), 0o644)
	require.NoError(t, err)
	return path
}

// loadSpecFromYAML loads a spec from inline YAML via a temp file.
func loadSpecFromYAML(t *testing.T, yamlContent string) *openapi3.T {
	t.Helper()
	path := writeTempSpec(t, yamlContent)
	doc, err := LoadSpec(&config.OpenAPISpecConfig{
		Name:    "test",
		SpecURL: path,
	})
	require.NoError(t, err, "LoadSpec should succeed")
	return doc
}

// petstoreSpec returns YAML for a petstore-like API spec with 6 operations.
func petstoreSpec() string {
	return `openapi: 3.0.0
info:
  title: Petstore
  version: 1.0.0
servers:
  - url: http://localhost:9999
paths:
  /pets:
    get:
      operationId: listPets
      summary: List all pets
      tags: [pets]
      parameters:
        - name: limit
          in: query
          schema:
            type: integer
      responses:
        "200":
          description: A list of pets
    post:
      operationId: createPets
      summary: Create a pet
      tags: [pets]
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                name:
                  type: string
      responses:
        "201":
          description: Pet created
  /pets/{petId}:
    get:
      operationId: getPetById
      summary: Get a pet by ID
      tags: [pets]
      parameters:
        - name: petId
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: A single pet
    delete:
      operationId: deletePet
      summary: Delete a pet
      tags: [pets]
      parameters:
        - name: petId
          in: path
          required: true
          schema:
            type: string
      responses:
        "204":
          description: Pet deleted
  /pets/findByStatus:
    get:
      operationId: findPetsByStatus
      summary: Finds pets by status
      tags: [pets]
      parameters:
        - name: status
          in: query
          required: true
          schema:
            type: string
      responses:
        "200":
          description: Matching pets
  /store/inventory:
    get:
      operationId: getInventory
      summary: Returns pet inventories by status
      tags: [store]
      responses:
        "200":
          description: successful operation
`
}

// buildPetstoreIndex loads the petstore spec and builds its index.
func buildPetstoreIndex(t *testing.T) ([]Operation, map[string]*Operation) {
	t.Helper()
	doc := loadSpecFromYAML(t, petstoreSpec())
	cfg := &config.OpenAPISpecConfig{Name: "petstore"}
	ops, opMap, err := BuildIndex(doc, cfg)
	require.NoError(t, err)
	require.Len(t, ops, 6, "petstore spec should have 6 operations")
	return ops, opMap
}

// simpleOp returns a GET Operation with no parameters.
func simpleOp(id, path string) *Operation {
	return &Operation{
		ID:     id,
		API:    "test",
		Method: "GET",
		Path:   path,
	}
}

// pathParamOp returns a GET Operation with one required string path param.
func pathParamOp(id, path, paramName string) *Operation {
	return &Operation{
		ID:     id,
		API:    "test",
		Method: "GET",
		Path:   path,
		ParamSchema: &openapi3.SchemaRef{
			Value: &openapi3.Schema{
				Type: &openapi3.Types{"object"},
				Properties: map[string]*openapi3.SchemaRef{
					paramName: {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
				},
				Required: []string{paramName},
			},
		},
	}
}

// queryParamOp returns a GET Operation with one optional string query param.
func queryParamOp(id, path, paramName string) *Operation {
	return &Operation{
		ID:     id,
		API:    "test",
		Method: "GET",
		Path:   path,
		ParamSchema: &openapi3.SchemaRef{
			Value: &openapi3.Schema{
				Type: &openapi3.Types{"object"},
				Properties: map[string]*openapi3.SchemaRef{
					paramName: {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// 1. Search ranking: index a known spec, assert search returns correct top-1
// ---------------------------------------------------------------------------

func TestSearchRanking(t *testing.T) {
	ops, _ := buildPetstoreIndex(t)

	tests := []struct {
		name    string
		intent  string
		wantTop string // expected top-1 operation ID
	}{
		{"find pets by status", "find pets by status", "petstore_findPetsByStatus"},
		{"list all pets", "list all pets", "petstore_listPets"},
		{"create new pet", "create new pet", "petstore_createPets"},
		{"get pet by id", "get pet by id", "petstore_getPetById"},
		{"delete existing pet", "delete existing pet", "petstore_deletePet"},
		{"inventory status", "inventory status", "petstore_findPetsByStatus"},
		{"just status", "status", "petstore_findPetsByStatus"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results := Search(ops, tt.intent, 5)
			require.NotEmpty(t, results, "search for %q should return results", tt.intent)
			assert.Equal(t, tt.wantTop, results[0].OperationID,
				"top-1 result for intent %q", tt.intent)
			assert.Greater(t, results[0].Score, 0,
				"score should be positive for %q", tt.intent)
		})
	}
}

// ---------------------------------------------------------------------------
// 2. Empty search: query that matches nothing
// ---------------------------------------------------------------------------

func TestEmptySearch(t *testing.T) {
	ops, _ := buildPetstoreIndex(t)

	results := Search(ops, "zzzzz_nothing_matches_this_xyzzy", 5)
	assert.Empty(t, results, "non-matching query should return empty results")

	// Empty query should also return nil/empty
	results = Search(ops, "", 5)
	assert.Empty(t, results, "empty query should return empty results")
}

// ---------------------------------------------------------------------------
// 3. Describe: returns full param schema JSON for known operationIDs
// ---------------------------------------------------------------------------

func TestDescribe(t *testing.T) {
	_, opMap := buildPetstoreIndex(t)

	results, err := Describe(opMap, []string{"petstore_listPets", "petstore_getPetById"})
	require.NoError(t, err)
	require.Len(t, results, 2)

	// First result: listPets
	var first map[string]any
	err = json.Unmarshal(results[0], &first)
	require.NoError(t, err)
	assert.Equal(t, "petstore_listPets", first["operation_id"])
	assert.Equal(t, "GET", first["method"])
	assert.Equal(t, "/pets", first["path"])
	assert.Equal(t, "List all pets", first["summary"])
	assert.Contains(t, first, "param_schema",
		"listPets has a query param 'limit', should include param_schema")
	assert.NotContains(t, first, "body_schema",
		"GET listPets should not have a body_schema")

	// Second result: getPetById
	var second map[string]any
	err = json.Unmarshal(results[1], &second)
	require.NoError(t, err)
	assert.Equal(t, "petstore_getPetById", second["operation_id"])
	assert.Equal(t, "GET", second["method"])
	assert.Equal(t, "/pets/{petId}", second["path"])
	assert.Contains(t, second, "param_schema",
		"getPetById has a path param 'petId', should include param_schema")
}

// ---------------------------------------------------------------------------
// 4. Describe unknown: unknown operationID → structured error
// ---------------------------------------------------------------------------

func TestDescribeUnknown(t *testing.T) {
	_, opMap := buildPetstoreIndex(t)

	results, err := Describe(opMap, []string{"nonexistent_op"})
	require.NoError(t, err)
	require.Len(t, results, 1)

	var entry map[string]string
	err = json.Unmarshal(results[0], &entry)
	require.NoError(t, err)
	assert.Equal(t, "nonexistent_op", entry["operation_id"])
	assert.Equal(t, "operation not found", entry["error"])
}

// ---------------------------------------------------------------------------
// 5. Execute path traversal: path param ../../admin → URL-escaped
// ---------------------------------------------------------------------------

func TestExecutePathTraversal(t *testing.T) {
	var receivedEscapedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedEscapedPath = r.URL.EscapedPath()
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	op := pathParamOp("test_getById", "/items/{itemId}", "itemId")
	op.ServerURL = srv.URL
	cfg := &config.OpenAPISpecConfig{
		Name: "test",
	}

	msg := ExecuteToolCall(context.Background(), "call_1", op, cfg, map[string]any{
		"itemId": "../../admin",
	}, "")

	require.NotNil(t, msg)
	content, ok := msg.Content.(string)
	require.True(t, ok)
	assert.Equal(t, "tool", msg.Role)

	// CRITICAL: raw path traversal must not appear in the tool result
	assert.NotContains(t, content, "../../admin",
		"raw path traversal string must not appear in tool result")

	// The upstream path must have the incoming slash percent-encoded (%2F)
	// proving the request was NOT sent as raw ../../control.
	assert.Contains(t, receivedEscapedPath, "%2F",
		"path param must have encoded slashes (%2F) preventing traversal")
	assert.NotContains(t, receivedEscapedPath, "/../../admin",
		"upstream must NOT receive raw path traversal")
}

// ---------------------------------------------------------------------------
// 6. Execute auth leak: auth header sent, but NOT in tool result (CRITICAL)
// ---------------------------------------------------------------------------

func TestExecuteAuthLeak(t *testing.T) {
	var receivedAuthHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthHeader = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("secure data"))
	}))
	defer srv.Close()

	op := simpleOp("test_get", "/data")
	op.ServerURL = srv.URL
	token := "sk-secret-token-12345"
	cfg := &config.OpenAPISpecConfig{
		Name: "test",
		Auth: config.OpenAPIAuthConfig{
			Type:  "bearer",
			Value: token,
		},
	}

	msg := ExecuteToolCall(context.Background(), "call_1", op, cfg, nil, "")

	require.NotNil(t, msg)
	content, ok := msg.Content.(string)
	require.True(t, ok)

	// CRITICAL: auth token must NOT appear in the tool result
	assert.NotContains(t, content, token,
		"auth token must NOT appear in the tool result content")
	assert.NotContains(t, content, "Authorization",
		"Authorization header name must NOT appear in tool result")

	// Verify the auth header WAS sent to the upstream
	assert.Equal(t, "Bearer "+token, receivedAuthHeader,
		"auth header must be sent to the upstream server")
}

// ---------------------------------------------------------------------------
// 7. Execute truncation: response >8KB → truncated with marker
// ---------------------------------------------------------------------------

func TestExecuteTruncation(t *testing.T) {
	// ~12KB response
	largeBody := strings.Repeat("hello world ", 1000)
	require.Greater(t, len(largeBody), 8192, "test body must exceed 8KB")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(largeBody))
	}))
	defer srv.Close()

	op := simpleOp("test_get", "/large")
	op.ServerURL = srv.URL
	cfg := &config.OpenAPISpecConfig{
		Name: "test",
	}

	msg := ExecuteToolCall(context.Background(), "call_1", op, cfg, nil, "")

	require.NotNil(t, msg)
	content, ok := msg.Content.(string)
	require.True(t, ok)

	assert.Contains(t, content, "...[truncated]",
		"truncation marker must be present for responses >8KB")
	// content = truncated string (max 8192 bytes, UTF-8 safe) + "...[truncated]" (14 bytes)
	assert.LessOrEqual(t, len(content), 8300,
		"truncated content must be under ~8KB + marker")
}

// ---------------------------------------------------------------------------
// 8. Execute timeout: upstream that sleeps 60s → times out within 5s
// ---------------------------------------------------------------------------

func TestExecuteTimeout(t *testing.T) {
	// Use a raw TCP listener that accepts but never responds (no httptest to avoid
	// cleanup hanging when the handler sleeps).
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { listener.Close() })

	go func() {
		conn, err := listener.Accept()
		if err == nil {
			// Accept the connection but never write a response.
			time.Sleep(10 * time.Second)
			conn.Close()
		}
	}()

	op := simpleOp("test_get", "/slow")
	baseURL := "http://" + listener.Addr().String()
	op.ServerURL = baseURL
	cfg := &config.OpenAPISpecConfig{
		Name:    "test",
		Timeout: 100 * time.Millisecond, // very short timeout
	}

	start := time.Now()
	msg := ExecuteToolCall(context.Background(), "call_1", op, cfg, nil, "")

	elapsed := time.Since(start)

	require.NotNil(t, msg)
	content, ok := msg.Content.(string)
	require.True(t, ok)
	assert.True(t, strings.HasPrefix(content, "Error:"),
		"timeout should return an error, got: %s", content)
	assert.Less(t, elapsed, 5*time.Second,
		"timeout should happen well within 5s, took %v", elapsed)
}

// ---------------------------------------------------------------------------
// 9. Execute 4xx/5xx: upstream returns 404 → "HTTP 404: ..."
// ---------------------------------------------------------------------------

func TestExecute4xx5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not found"}`))
	}))
	defer srv.Close()

	op := simpleOp("test_get", "/missing")
	op.ServerURL = srv.URL
	cfg := &config.OpenAPISpecConfig{
		Name: "test",
	}

	msg := ExecuteToolCall(context.Background(), "call_1", op, cfg, nil, "")

	require.NotNil(t, msg)
	content, ok := msg.Content.(string)
	require.True(t, ok)
	assert.Contains(t, content, "HTTP 404:",
		"4xx response should include HTTP status code")
	assert.Contains(t, content, `{"error":"not found"}`,
		"4xx response body should be preserved after the status prefix")
}

// ---------------------------------------------------------------------------
// 10. Provider meta-tools: GetAuthorizedTools returns exactly 3 tools
// ---------------------------------------------------------------------------

func TestProviderMetaTools(t *testing.T) {
	specs := []config.OpenAPISpecConfig{
		{
			Name:    "petstore",
			SpecURL: writeTempSpec(t, petstoreSpec()),
		},
	}

	p, err := NewToolProvider(specs)
	require.NoError(t, err)
	require.NotNil(t, p)

	tools := p.GetAuthorizedTools("", nil)
	require.Len(t, tools, 3, "GetAuthorizedTools should return exactly 3 tools")

	names := make([]string, len(tools))
	for i, tool := range tools {
		names[i] = tool.Function.Name
	}
	assert.Contains(t, names, "openapi_search")
	assert.Contains(t, names, "openapi_describe")
	assert.Contains(t, names, "openapi_call")

	// Verify tool types
	for _, tool := range tools {
		assert.Equal(t, "function", tool.Type)
		assert.NotEmpty(t, tool.Function.Name)
		assert.NotEmpty(t, tool.Function.Description)
		assert.NotNil(t, tool.Function.Parameters)
	}
}

func TestProviderStringifiedArgs(t *testing.T) {
	_, opMap := buildPetstoreIndex(t)
	specs := []config.OpenAPISpecConfig{
		{Name: "petstore", SpecURL: writeTempSpec(t, petstoreSpec())},
	}

	p, err := NewToolProvider(specs)
	require.NoError(t, err)
	p.ops = opMap

	// Test handleDescribe with stringified array
	describeMsg := p.handleDescribe(model.ToolCall{
		ID:   "call_desc",
		Type: "function",
		Function: model.ToolCallFunctionData{
			Name:      "openapi_describe",
			Arguments: `{"operation_ids": "[\"petstore_getPetById\"]"}`,
		},
	})
	require.NotNil(t, describeMsg)
	contentStr, ok := describeMsg.Content.(string)
	require.True(t, ok)
	assert.False(t, strings.HasPrefix(contentStr, "Error: parsing arguments"))

	// Test handleCall with stringified empty params object
	callMsg := p.handleCall(context.Background(), model.ToolCall{
		ID:   "call_exec",
		Type: "function",
		Function: model.ToolCallFunctionData{
			Name:      "openapi_call",
			Arguments: `{"operation_id": "petstore_getPetById", "params": "{}"}`,
		},
	})
	require.NotNil(t, callMsg)
	callContent, ok := callMsg.Content.(string)
	require.True(t, ok)
	assert.False(t, strings.HasPrefix(callContent, "Error: parsing arguments"))
}

// ---------------------------------------------------------------------------
// 11. Duplicate operationId: BuildIndex with duplicate → dedup works
// ---------------------------------------------------------------------------

func TestDuplicateOperationID(t *testing.T) {
	// Build a spec with two operations sharing the same operationId.
	spec := &openapi3.T{
		OpenAPI: "3.0.0",
		Info:    &openapi3.Info{Title: "DupSpec", Version: "1.0.0"},
	}
	spec.Paths = openapi3.NewPaths(
		openapi3.WithPath("/foo", &openapi3.PathItem{
			Get: &openapi3.Operation{
				OperationID: "sameOp",
				Responses:   &openapi3.Responses{},
			},
		}),
		openapi3.WithPath("/bar", &openapi3.PathItem{
			Get: &openapi3.Operation{
				OperationID: "sameOp",
				Responses:   &openapi3.Responses{},
			},
		}),
	)

	cfg := &config.OpenAPISpecConfig{Name: "test"}
	ops, opMap, err := BuildIndex(spec, cfg)
	require.NoError(t, err, "BuildIndex should dedup without error")
	require.Len(t, ops, 2, "both duplicate operations should be indexed")

	// They should have different sanitized names
	assert.NotEqual(t, ops[0].ID, ops[1].ID,
		"duplicate operation IDs should be deduped to different names")

	// Verify the dedup suffix convention: second gets _1 suffix
	ids := make([]string, 0, 2)
	ids = append(ids, ops[0].ID, ops[1].ID)
	assert.Contains(t, ids, "test_sameOp", "first op should keep the base name")
	assert.Contains(t, ids, "test_sameOp_1", "second op should get _1 suffix")
	assert.Len(t, opMap, 2, "opMap should contain both operations")
}

// ---------------------------------------------------------------------------
// 12. Name sanitization: long/unicode/colliding names → valid tool names
// ---------------------------------------------------------------------------

func TestNameSanitization(t *testing.T) {
	tests := []struct {
		input string
		want  string // empty means only verify regex and length
	}{
		{"petstore_listPets", "petstore_listPets"},
		{"simple", "simple"},
		{"", "tool"},
		{"___", "tool"},
		{"___hello___", "hello"},
		{"  spaces  ", "spaces"},
		{"My API! Get%Data", "My_API_Get_Data"},
		{"unicode数据", "unicode"},
		{"mixed-CASE", "mixed-CASE"}, // hyphen retained, it's in [a-zA-Z0-9_-]
		{"dots.and.dashes", "dots_and_dashes"},
		{"a-b-c-d", "a-b-c-d"}, // hyphens allowed in [a-zA-Z0-9_-]
		// Long name (>64 chars) — should be truncated
		{"a-very-long-name-that-exceeds-sixty-four-characters-and-should-be-truncated", ""},
		// Collision patterns
		{"tool_1_tool_2_tool_3", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := SanitizeToolName(tt.input)
			assert.Regexp(t, `^[a-zA-Z0-9_-]+$`, got,
				"sanitized name must match ^[a-zA-Z0-9_-]+$")
			assert.LessOrEqual(t, len(got), 64,
				"sanitized name must be <= 64 chars, got %d", len(got))
			assert.NotEmpty(t, got, "sanitized name must not be empty")
			if tt.want != "" {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 13. CRLF rejection: string param with \r\n → error
// ---------------------------------------------------------------------------

func TestCRLFRejection(t *testing.T) {
	op := queryParamOp("test_post", "/submit", "message")
	cfg := &config.OpenAPISpecConfig{
		Name: "test",
	}

	tests := []struct {
		name    string
		message string
	}{
		{"CRLF in message", "hello\r\nX-Injected: true"},
		{"CR only", "hello\rcr-only"},
		{"LF only", "hello\nlf-only"},
		{"Both CRLF multiple", "a\r\nb\nc\rd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := ExecuteToolCall(context.Background(), "call_1", op, cfg, map[string]any{
				"message": tt.message,
			}, "")

			require.NotNil(t, msg)
			content, ok := msg.Content.(string)
			require.True(t, ok)
			assert.Contains(t, content, "CRLF",
				"CRLF rejection should contain 'CRLF'")
			assert.Contains(t, content, "message",
				"error should mention the parameter name")
		})
	}
}

// ---------------------------------------------------------------------------
// 14. Provider Execute: openapi_search returns results
// ---------------------------------------------------------------------------

func TestProviderSearch(t *testing.T) {
	specs := []config.OpenAPISpecConfig{
		{
			Name:    "petstore",
			SpecURL: writeTempSpec(t, petstoreSpec()),
		},
	}
	p, err := NewToolProvider(specs)
	require.NoError(t, err)

	calls := []model.ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: model.ToolCallFunctionData{
				Name:      "openapi_search",
				Arguments: `{"intent": "list all pets"}`,
			},
		},
	}

	msgs, errFlags := p.Execute(context.Background(), "", "", calls)
	require.NotEmpty(t, msgs)

	// Find tool message
	var toolMsg *model.Message
	for i := range msgs {
		if msgs[i].Role == "tool" {
			toolMsg = &msgs[i]
			break
		}
	}
	require.NotNil(t, toolMsg, "should have a tool message")
	content, ok := toolMsg.Content.(string)
	require.True(t, ok)

	// Should be a JSON array with results
	var results []SearchResult
	err = json.Unmarshal([]byte(content), &results)
	require.NoError(t, err, "search result should be valid JSON array")
	require.NotEmpty(t, results, "should have search results")
	assert.Equal(t, "petstore_listPets", results[0].OperationID,
		"top result for 'list all pets' should be listPets")
	require.Len(t, errFlags, 1, "errFlags should have 1 entry per call")
	assert.False(t, errFlags[0], "search should not set error flag")
}

// ---------------------------------------------------------------------------
// 15. Provider Execute: openapi_describe returns schemas
// ---------------------------------------------------------------------------

func TestProviderDescribe(t *testing.T) {
	specs := []config.OpenAPISpecConfig{
		{
			Name:    "petstore",
			SpecURL: writeTempSpec(t, petstoreSpec()),
		},
	}
	p, err := NewToolProvider(specs)
	require.NoError(t, err)

	calls := []model.ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: model.ToolCallFunctionData{
				Name:      "openapi_describe",
				Arguments: `{"operation_ids": ["petstore_listPets"]}`,
			},
		},
	}

	msgs, errFlags := p.Execute(context.Background(), "", "", calls)
	require.NotEmpty(t, msgs)

	var toolMsg *model.Message
	for i := range msgs {
		if msgs[i].Role == "tool" {
			toolMsg = &msgs[i]
			break
		}
	}
	require.NotNil(t, toolMsg)
	content, ok := toolMsg.Content.(string)
	require.True(t, ok)

	var results []json.RawMessage
	err = json.Unmarshal([]byte(content), &results)
	require.NoError(t, err, "describe result should be valid JSON")
	require.Len(t, results, 1)

	var desc map[string]any
	err = json.Unmarshal(results[0], &desc)
	require.NoError(t, err)
	assert.Equal(t, "petstore_listPets", desc["operation_id"])
	assert.Contains(t, desc, "param_schema")
	require.Len(t, errFlags, 1, "errFlags should have 1 entry per call")
	assert.False(t, errFlags[0], "describe should not set error flag")
}

// ---------------------------------------------------------------------------
// 16. Provider Execute: openapi_call with unknown operation_id
// ---------------------------------------------------------------------------

func TestProviderCallUnknown(t *testing.T) {
	specs := []config.OpenAPISpecConfig{
		{
			Name:    "petstore",
			SpecURL: writeTempSpec(t, petstoreSpec()),
		},
	}
	p, err := NewToolProvider(specs)
	require.NoError(t, err)

	calls := []model.ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: model.ToolCallFunctionData{
				Name:      "openapi_call",
				Arguments: `{"operation_id": "nonexistent_op", "params": {}}`,
			},
		},
	}

	msgs, errFlags := p.Execute(context.Background(), "", "", calls)
	require.NotEmpty(t, msgs)

	var toolMsg *model.Message
	for i := range msgs {
		if msgs[i].Role == "tool" {
			toolMsg = &msgs[i]
			break
		}
	}
	require.NotNil(t, toolMsg)
	content, ok := toolMsg.Content.(string)
	require.True(t, ok)
	assert.Contains(t, content, "Error:")
	assert.Contains(t, content, "nonexistent_op",
		"error should mention the unknown operation ID")
	assert.Len(t, errFlags, 1, "should have 1 error flag")
	assert.True(t, errFlags[0], "error flag should be true")
}

// ---------------------------------------------------------------------------
// 17. Execute missing required param
// ---------------------------------------------------------------------------

func TestExecuteMissingRequiredParam(t *testing.T) {
	op := pathParamOp("test_getById", "/items/{itemId}", "itemId")
	cfg := &config.OpenAPISpecConfig{
		Name: "test",
	}

	// Call without the required param
	msg := ExecuteToolCall(context.Background(), "call_1", op, cfg, map[string]any{}, "")
	require.NotNil(t, msg)
	content, ok := msg.Content.(string)
	require.True(t, ok)
	assert.Contains(t, content, "Error:")
	assert.Contains(t, content, "missing required parameter")
	assert.Contains(t, content, "itemId")
}

// ---------------------------------------------------------------------------
// 18. Execute nil operation
// ---------------------------------------------------------------------------

func TestExecuteNilOperation(t *testing.T) {
	msg := ExecuteToolCall(context.Background(), "call_1", nil, nil, nil, "")

	require.NotNil(t, msg)
	content, ok := msg.Content.(string)
	require.True(t, ok)
	assert.Equal(t, "Error: nil operation", content)
}

// ---------------------------------------------------------------------------
// 19. Search with limit
// ---------------------------------------------------------------------------

func TestSearchWithLimit(t *testing.T) {
	ops, _ := buildPetstoreIndex(t)

	// With limit=1
	results := Search(ops, "pet", 1)
	require.Len(t, results, 1, "limit=1 should return exactly 1 result")

	// Without limit (defaults to 10)
	results = Search(ops, "pet", 0)
	require.GreaterOrEqual(t, len(results), 1, "default limit should return results")
	require.LessOrEqual(t, len(results), 10, "default limit should be at most 10")
}

// ---------------------------------------------------------------------------
// 20. Execute non-text response
// ---------------------------------------------------------------------------

func TestExecuteNonTextResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{0x89, 0x50, 0x4E, 0x47}) // PNG header bytes
	}))
	defer srv.Close()

	op := simpleOp("test_get", "/image")
	op.ServerURL = srv.URL
	cfg := &config.OpenAPISpecConfig{
		Name: "test",
	}

	msg := ExecuteToolCall(context.Background(), "call_1", op, cfg, nil, "")

	require.NotNil(t, msg)
	content, ok := msg.Content.(string)
	require.True(t, ok)
	assert.Contains(t, content, "[non-text response: image/png")
}

// ---------------------------------------------------------------------------
// 21. Provider empty search returns [] (not an error)
// ---------------------------------------------------------------------------

func TestProviderEmptySearch(t *testing.T) {
	specs := []config.OpenAPISpecConfig{
		{
			Name:    "petstore",
			SpecURL: writeTempSpec(t, petstoreSpec()),
		},
	}
	p, err := NewToolProvider(specs)
	require.NoError(t, err)

	calls := []model.ToolCall{
		{
			ID:   "call_1",
			Type: "function",
			Function: model.ToolCallFunctionData{
				Name:      "openapi_search",
				Arguments: `{"intent": "zzzzz_totally_nonexistent"}`,
			},
		},
	}

	msgs, errFlags := p.Execute(context.Background(), "", "", calls)
	require.NotEmpty(t, msgs)

	var toolMsg *model.Message
	for i := range msgs {
		if msgs[i].Role == "tool" {
			toolMsg = &msgs[i]
			break
		}
	}
	require.NotNil(t, toolMsg)
	content, ok := toolMsg.Content.(string)
	require.True(t, ok)
	assert.Equal(t, "[]", content, "empty search should return empty JSON array")
	require.Len(t, errFlags, 1, "errFlags should have 1 entry per call")
	assert.False(t, errFlags[0], "empty search should not set error flag")
}

// ---------------------------------------------------------------------------
// 22. Execute with basic auth (non-bearer auth type)
// ---------------------------------------------------------------------------
// ---------------------------------------------------------------------------

func TestExecuteBasicAuth(t *testing.T) {
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	op := simpleOp("test_get", "/data")
	op.ServerURL = srv.URL
	cfg := &config.OpenAPISpecConfig{
		Name: "test",
		Auth: config.OpenAPIAuthConfig{
			Type:  "basic",
			Value: "user:pass",
		},
	}

	msg := ExecuteToolCall(context.Background(), "call_1", op, cfg, nil, "")

	require.NotNil(t, msg)
	content, ok := msg.Content.(string)
	require.True(t, ok)
	assert.NotContains(t, content, "user:pass",
		"basic auth credentials must not leak in tool result")
	assert.NotContains(t, content, "Basic",
		"auth type must not appear in tool result")
	assert.Contains(t, receivedAuth, "Basic ",
		"Basic auth header should be sent to upstream")
}

// ---------------------------------------------------------------------------
// 24. SanitizeToolName - ensure SanitizeToolName edge cases
// ---------------------------------------------------------------------------

func TestSanitizeToolNameEdgeCases(t *testing.T) {
	// Very long string with special chars
	long := make([]byte, 200)
	for i := range long {
		long[i] = byte(rand.Intn(26) + 'a')
	}
	name := string(long)
	got := SanitizeToolName(name)
	assert.Regexp(t, `^[a-zA-Z0-9_-]+$`, got)
	assert.LessOrEqual(t, len(got), 64)
	assert.Contains(t, got, name[:30], "should keep prefix")
	assert.Contains(t, got, name[len(name)-33:], "should keep suffix")

	// Name with only special chars
	got = SanitizeToolName("!!!@@@###")
	assert.Equal(t, "tool", got)

	// Very long with leading/trailing specials
	got = SanitizeToolName("___" + strings.Repeat("a", 100) + "___")
	assert.Regexp(t, `^[a-zA-Z0-9_-]+$`, got)
	assert.LessOrEqual(t, len(got), 64)
	assert.True(t, strings.HasPrefix(got, strings.Repeat("a", 30)), "should keep leading content")
}

// deepObjectOp returns a GET Operation with one deepObject query param.
func deepObjectOp(id, path, paramName string) *Operation {
	return &Operation{
		ID:     id,
		API:    "test",
		Method: "GET",
		Path:   path,
		ParamSchema: &openapi3.SchemaRef{
			Value: &openapi3.Schema{
				Type: &openapi3.Types{"object"},
				Properties: map[string]*openapi3.SchemaRef{
					paramName: {
						Value: &openapi3.Schema{
							Type: &openapi3.Types{"object"},
							Properties: map[string]*openapi3.SchemaRef{
								"status": {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
								"type":   {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
							},
						},
					},
				},
			},
		},
		ParamStyles: map[string]string{paramName: openapi3.SerializationDeepObject},
	}
}

// ---------------------------------------------------------------------------
// 25. Describe returns body_schema for POST operations
// ---------------------------------------------------------------------------

func TestDescribeWithBodySchema(t *testing.T) {
	_, opMap := buildPetstoreIndex(t)

	results, err := Describe(opMap, []string{"petstore_createPets"})
	require.NoError(t, err)
	require.Len(t, results, 1)

	var entry map[string]any
	err = json.Unmarshal(results[0], &entry)
	require.NoError(t, err)
	assert.Equal(t, "petstore_createPets", entry["operation_id"])
	assert.Equal(t, "POST", entry["method"])
	assert.Contains(t, entry, "body_schema",
		"createPets POST should have body_schema")
	assert.NotContains(t, entry, "param_schema",
		"createPets has no defined URL params")
}

// ---------------------------------------------------------------------------
// 26. Execute deepObject: object param → ?filter[status]=available&filter[type]=pet
// ---------------------------------------------------------------------------

func TestExecuteDeepObjectSerialization(t *testing.T) {
	var receivedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`ok`))
	}))
	defer srv.Close()

	op := deepObjectOp("test_search", "/items", "filter")
	op.ServerURL = srv.URL
	cfg := &config.OpenAPISpecConfig{
		Name: "test",
	}

	msg := ExecuteToolCall(context.Background(), "call_1", op, cfg, map[string]any{
		"filter": map[string]any{
			"status": "available",
			"type":   "pet",
		},
	}, "")

	require.NotNil(t, msg)
	content, ok := msg.Content.(string)
	require.True(t, ok)
	assert.NotContains(t, content, "Error:", "deepObject serialization should succeed")

	// Verify the query string uses deepObject style: paramName[prop]=value
	// Brackets are URL-encoded by url.Values.Encode as %5B / %5D.
	assert.Contains(t, receivedQuery, "filter%5Bstatus%5D=available",
		"deepObject should serialize as paramName[prop]=value")
	assert.Contains(t, receivedQuery, "filter%5Btype%5D=pet",
		"deepObject should serialize all object properties")
}

// ---------------------------------------------------------------------------
// 27. Execute deepObject with scalar value → falls back to form serialization
// ---------------------------------------------------------------------------

func TestExecuteDeepObjectNonObject(t *testing.T) {
	var receivedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`ok`))
	}))
	defer srv.Close()

	op := deepObjectOp("test_search", "/items", "filter")
	op.ServerURL = srv.URL
	cfg := &config.OpenAPISpecConfig{
		Name: "test",
	}

	// Pass a scalar value for a deepObject param — fallback to form
	msg := ExecuteToolCall(context.Background(), "call_1", op, cfg, map[string]any{
		"filter": "just-a-string",
	}, "")

	require.NotNil(t, msg)
	content, ok := msg.Content.(string)
	require.True(t, ok)
	assert.NotContains(t, content, "Error:", "scalar with deepObject style should not error")
	assert.Equal(t, "filter=just-a-string", receivedQuery,
		"non-object value with deepObject style should fall back to form/simple")
}

// ---------------------------------------------------------------------------
// 28. Execute form object: object param with default form style still works
// ---------------------------------------------------------------------------

func TestExecuteFormObjectStillWorks(t *testing.T) {
	var receivedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`ok`))
	}))
	defer srv.Close()

	op := queryParamOp("test_filter", "/items", "filter")
	op.ServerURL = srv.URL
	// Inject a schema that marks filter as an object
	op.ParamSchema = &openapi3.SchemaRef{
		Value: &openapi3.Schema{
			Type: &openapi3.Types{"object"},
			Properties: map[string]*openapi3.SchemaRef{
				"filter": {
					Value: &openapi3.Schema{
						Type: &openapi3.Types{"object"},
						Properties: map[string]*openapi3.SchemaRef{
							"status": {Value: &openapi3.Schema{Type: &openapi3.Types{"string"}}},
						},
					},
				},
			},
		},
	}
	cfg := &config.OpenAPISpecConfig{
		Name: "test",
	}

	msg := ExecuteToolCall(context.Background(), "call_1", op, cfg, map[string]any{
		"filter": map[string]any{"status": "available"},
	}, "")

	require.NotNil(t, msg)
	content, ok := msg.Content.(string)
	require.True(t, ok)
	assert.NotContains(t, content, "Error:", "form object serialization should succeed")
	assert.Contains(t, receivedQuery, "filter=status%2Cavailable",
		"form style should serialize object as key,value pair (comma URL-encoded)")
}

// ---------------------------------------------------------------------------
// 29. buildParamSchema populates ParamStyles from OpenAPI parameter styles
// ---------------------------------------------------------------------------

func TestBuildParamSchemaStyles(t *testing.T) {
	specYAML := `openapi: 3.0.0
info:
  title: StyleTest
  version: 1.0.0
servers:
  - url: http://localhost:9999
paths:
  /search:
    get:
      operationId: search
      parameters:
        - name: filter
          in: query
          style: deepObject
          schema:
            type: object
            properties:
              status:
                type: string
        - name: limit
          in: query
          schema:
            type: integer
        - name: sort
          in: query
          style: form
          schema:
            type: string
        - name: petId
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: OK
`
	doc := loadSpecFromYAML(t, specYAML)
	cfg := &config.OpenAPISpecConfig{Name: "styletest"}
	ops, _, err := BuildIndex(doc, cfg)
	require.NoError(t, err)
	require.Len(t, ops, 1, "should have 1 operation")

	op := ops[0]
	require.NotNil(t, op.ParamStyles, "ParamStyles should be populated")

	// Explicit deepObject style
	assert.Equal(t, openapi3.SerializationDeepObject, op.ParamStyles["filter"],
		"explicit deepObject style should be preserved")

	// Default for query params with no explicit style
	assert.Equal(t, openapi3.SerializationForm, op.ParamStyles["limit"],
		"query param without explicit style should default to form")

	// Explicit form style
	assert.Equal(t, openapi3.SerializationForm, op.ParamStyles["sort"],
		"explicit form style should be preserved")

	// Default for path params with no explicit style
	assert.Equal(t, openapi3.SerializationSimple, op.ParamStyles["petId"],
		"path param without explicit style should default to simple")
}

// ---------------------------------------------------------------------------
// 30. Execute deepObject with multiple properties
// ---------------------------------------------------------------------------

func TestExecuteDeepObjectMultipleProps(t *testing.T) {
	var receivedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`ok`))
	}))
	defer srv.Close()

	op := deepObjectOp("test_multi", "/items", "filter")
	op.ServerURL = srv.URL
	cfg := &config.OpenAPISpecConfig{
		Name: "test",
	}

	msg := ExecuteToolCall(context.Background(), "call_1", op, cfg, map[string]any{
		"filter": map[string]any{
			"status": "active",
			"type":   "gadget",
			"page":   "2",
		},
	}, "")

	require.NotNil(t, msg)
	content, ok := msg.Content.(string)
	require.True(t, ok)
	assert.NotContains(t, content, "Error:", "deepObject should succeed with 3 properties")

	// Brackets URL-encoded by url.Values.Encode as %5B / %5D.
	assert.Contains(t, receivedQuery, "filter%5Bstatus%5D=active")
	assert.Contains(t, receivedQuery, "filter%5Btype%5D=gadget")
	assert.Contains(t, receivedQuery, "filter%5Bpage%5D=2")
}
