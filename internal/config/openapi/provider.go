// Package openapi provides OpenAPI spec loading, indexing, and HTTP execution
// for the ilter MCP tool system.
package openapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model"
)

// ToolProvider implements the mcp.Provider pattern for OpenAPI tools.
// It emits 3 meta-tools (openapi_search, openapi_describe, openapi_call) and
// routes execution by tool name.
type ToolProvider struct {
	specs    []config.OpenAPISpecConfig
	specMap  map[string]int        // spec name → index in specs
	ops      map[string]*Operation // full sanitized tool name → Operation
	opList   []Operation
	mu       sync.RWMutex
	AdminKey string // admin API key for loopback auth injection
}

// NewToolProvider loads OpenAPI specs, builds the operation index, and
// returns a ready-to-use provider. Specs are sourced from ConfigCache.
func NewToolProvider(specs []config.OpenAPISpecConfig) (*ToolProvider, error) {
	if len(specs) == 0 {
		return &ToolProvider{}, nil
	}

	allOps := make([]Operation, 0)
	opMap := make(map[string]*Operation)
	spMap := make(map[string]int, len(specs))

	for i := range specs {
		sc := &specs[i]
		if sc.Name == "" {
			return nil, fmt.Errorf("openapi: spec at index %d has empty name", i)
		}
		if _, dup := spMap[sc.Name]; dup {
			return nil, fmt.Errorf("openapi: duplicate spec name %q", sc.Name)
		}
		spMap[sc.Name] = i

		doc, err := LoadSpec(sc)
		if err != nil {
			return nil, fmt.Errorf("openapi: loading spec %q: %w", sc.Name, err)
		}

		ops, om, err := BuildIndex(doc, sc)
		if err != nil {
			return nil, fmt.Errorf("openapi: indexing spec %q: %w", sc.Name, err)
		}

		allOps = append(allOps, ops...)
		for k, v := range om {
			if _, dup := opMap[k]; dup {
				return nil, fmt.Errorf("openapi: duplicate operation name %q across specs", k)
			}
			opMap[k] = v
		}
	}

	return &ToolProvider{
		specs:   specs,
		specMap: spMap,
		ops:     opMap,
		opList:  allOps,
	}, nil
}

// Reload rebuilds the operation index from the given specs.
// Safe for concurrent use with GetAuthorizedTools/Execute.
func (p *ToolProvider) Reload(specs []config.OpenAPISpecConfig) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	loader := openapi3.NewLoader()
	loader.Context = context.Background()
	loader.IsExternalRefsAllowed = false
	allOps := make([]Operation, 0)
	opMap := make(map[string]*Operation)
	spMap := make(map[string]int, len(specs))

	for i := range specs {
		doc, err := LoadSpec(&specs[i])
		if err != nil {
			openapiLog.Warn("reload skipping spec", "name", specs[i].Name, "error", err)
			continue
		}
		ops, idx, err := BuildIndex(doc, &specs[i])
		if err != nil {
			openapiLog.Warn("reload indexing failed", "name", specs[i].Name, "error", err)
			continue
		}
		spMap[specs[i].Name] = i
		allOps = append(allOps, ops...)
		for k, v := range idx {
			opMap[k] = v
		}
	}

	p.specs = specs
	p.specMap = spMap
	p.ops = opMap
	p.opList = allOps
	openapiLog.Info("reloaded", "specs", len(specs), "operations", len(allOps))
	return nil
}

// OperationsByAPI returns the loaded operations grouped by spec name,
// formatted as "METHOD /path" strings, sorted for deterministic comparison.
func (p *ToolProvider) OperationsByAPI() map[string][]string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	groups := make(map[string][]string, len(p.specMap))
	for _, op := range p.opList {
		groups[op.API] = append(groups[op.API], strings.ToUpper(op.Method)+" "+op.Path)
	}
	for api := range groups {
		sort.Strings(groups[api])
	}
	return groups
}

// GetAuthorizedTools returns the 3 meta-tools for every authenticated user:
// openapi_search, openapi_describe, openapi_call.
func (p *ToolProvider) GetAuthorizedTools(_ string, _ []int) []model.Tool {
	p.mu.RLock()
	var apiNames []string
	for _, spec := range p.specs {
		if spec.Name != "" {
			apiNames = append(apiNames, spec.Name)
		}
	}
	p.mu.RUnlock()

	desc := "Search registered OpenAPI operations by intent."
	if len(apiNames) > 0 {
		desc += " Registered APIs: " + strings.Join(apiNames, ", ") + "."
	}

	searchTool := openAPISearchTool()
	searchTool.Function.Description = desc

	return []model.Tool{
		searchTool,
		openAPIDescribeTool(),
		openAPICallTool(),
	}
}

// Execute routes each tool call by its name (stripping the "openapi_" prefix)
// and returns results in the original call order. Unknown tools are skipped.
func (p *ToolProvider) Execute(ctx context.Context, _, _ string, calls []model.ToolCall) ([]model.Message, []bool) {
	if len(calls) == 0 {
		return nil, nil
	}

	assistantMsg := model.Message{
		Role:      "assistant",
		ToolCalls: calls,
		Content:   "",
	}
	msgs := []model.Message{assistantMsg}
	errFlags := []bool{}

	for _, call := range calls {
		name := strings.TrimPrefix(call.Function.Name, "openapi_")

		var result *model.Message
		switch name {
		case "search":
			result = p.handleSearch(call)
		case "describe":
			result = p.handleDescribe(call)
		case "call":
			result = p.handleCall(ctx, call)
		default:
			openapiLog.Debug("unknown tool call", "name", call.Function.Name)
			continue
		}

		if result == nil {
			continue
		}

		msgs = append(msgs, *result)
		contentStr, _ := result.Content.(string)
		isErr := strings.HasPrefix(contentStr, "Error:") || strings.HasPrefix(contentStr, "HTTP 4") || strings.HasPrefix(contentStr, "HTTP 5")
		errFlags = append(errFlags, isErr)
	}

	return msgs, errFlags
}

// ---------------------------------------------------------------------------
// Meta-tool definitions
// ---------------------------------------------------------------------------

func openAPISearchTool() model.Tool {
	return model.Tool{
		Type: "function",
		Function: model.ToolFunction{
			Name:        "openapi_search",
			Description: "Search registered OpenAPI operations by intent to find matching operation_id values. Use openapi_call(operation_id, params) to execute an operation, or openapi_describe(operation_ids) to inspect its schema.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"intent": map[string]any{
						"type":        "string",
						"description": "Natural language description of the API operation you are looking for",
					},
					"api": map[string]any{
						"type":        "string",
						"description": "Optional API name to narrow search to a specific API spec",
					},
					"limit": map[string]any{
						"type":        "integer",
						"default":     10,
						"description": "Maximum number of results to return (default 10)",
					},
				},
				"required": []string{"intent"},
			},
		},
	}
}

func openAPIDescribeTool() model.Tool {
	return model.Tool{
		Type: "function",
		Function: model.ToolFunction{
			Name:        "openapi_describe",
			Description: "Get parameter and request body schemas for operation_ids found via openapi_search (e.g. operation_ids: ['Petstore_getInventory']).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"operation_ids": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Operation IDs to describe (1 to 3)",
						"minItems":    1,
						"maxItems":    3,
					},
				},
				"required": []string{"operation_ids"},
			},
		},
	}
}

func openAPICallTool() model.Tool {
	return model.Tool{
		Type: "function",
		Function: model.ToolFunction{
			Name:        "openapi_call",
			Description: "Execute an HTTP API operation using an operation_id found via openapi_search (e.g. operation_id: 'Petstore_getInventory', params: {}).",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"operation_id": map[string]any{
						"type":        "string",
						"description": "The operation_id of the API operation to execute (obtained via openapi_search)",
					},
					"params": map[string]any{
						"type":                 "object",
						"description":          "Parameter values matching the operation's parameter and body schemas",
						"additionalProperties": true,
					},
				},
				"required": []string{"operation_id", "params"},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Execution handlers
// ---------------------------------------------------------------------------

func (p *ToolProvider) handleSearch(call model.ToolCall) *model.Message {
	var args struct {
		Intent string `json:"intent"`
		API    string `json:"api,omitempty"`
		Limit  int    `json:"limit,omitempty"`
	}
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return toolMsg(call.ID, call.Function.Name, fmt.Sprintf("Error: parsing arguments: %v", err))
	}
	if args.Intent == "" {
		return toolMsg(call.ID, call.Function.Name, "Error: missing required parameter 'intent'")
	}

	limit := args.Limit
	if limit <= 0 {
		limit = 10
	}

	p.mu.RLock()
	ops := p.opList
	p.mu.RUnlock()

	// Filter by API name when specified.
	if args.API != "" {
		filtered := make([]Operation, 0, len(ops))
		for _, op := range ops {
			if strings.EqualFold(op.API, args.API) {
				filtered = append(filtered, op)
			}
		}
		ops = filtered
	}

	results := Search(ops, args.Intent, limit)
	data, err := json.Marshal(results)
	if err != nil {
		return toolMsg(call.ID, call.Function.Name, fmt.Sprintf("Error: serializing search results: %v", err))
	}

	return toolMsg(call.ID, call.Function.Name, string(data))
}

func (p *ToolProvider) resolveOperation(opID string) (*Operation, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if op, ok := p.ops[opID]; ok {
		return op, true
	}

	normTarget := strings.ToLower(strings.NewReplacer("_", "", "-", "", "/", "").Replace(opID))
	for id, op := range p.ops {
		normID := strings.ToLower(strings.NewReplacer("_", "", "-", "", "/", "").Replace(id))
		if normID == normTarget {
			return op, true
		}
	}

	for id, op := range p.ops {
		normID := strings.ToLower(strings.NewReplacer("_", "", "-", "", "/", "").Replace(id))
		normPath := strings.ToLower(strings.NewReplacer("_", "", "-", "", "/", "").Replace(op.Path))
		if (len(normTarget) >= 4 && (strings.Contains(normID, normTarget) || strings.Contains(normTarget, normID))) ||
			(len(normTarget) >= 4 && (strings.Contains(normPath, normTarget) || strings.Contains(normTarget, normPath))) {
			return op, true
		}
	}

	return nil, false
}

func (p *ToolProvider) handleDescribe(call model.ToolCall) *model.Message {
	var rawArgs struct {
		OperationIDs any `json:"operation_ids"`
	}
	if err := json.Unmarshal([]byte(call.Function.Arguments), &rawArgs); err != nil {
		return toolMsg(call.ID, call.Function.Name, fmt.Sprintf("Error: parsing arguments: %v", err))
	}

	var opIDs []string
	switch v := rawArgs.OperationIDs.(type) {
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				s = strings.Trim(strings.TrimSpace(s), "\"'` \t\r\n")
				if s != "" {
					opIDs = append(opIDs, s)
				}
			}
		}
	case string:
		v = strings.TrimSpace(v)
		if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
			var parsed []string
			if err := json.Unmarshal([]byte(v), &parsed); err == nil {
				for _, item := range parsed {
					item = strings.Trim(strings.TrimSpace(item), "\"'` \t\r\n")
					if item != "" {
						opIDs = append(opIDs, item)
					}
				}
			}
		} else if v != "" {
			v = strings.Trim(v, "\"'` \t\r\n")
			if v != "" {
				opIDs = []string{v}
			}
		}
	}

	if len(opIDs) == 0 {
		return toolMsg(call.ID, call.Function.Name, "Error: missing required parameter 'operation_ids'")
	}

	p.mu.RLock()
	opMap := p.ops
	p.mu.RUnlock()

	resolvedIDs := make([]string, 0, len(opIDs))
	for _, id := range opIDs {
		if resolved, ok := p.resolveOperation(id); ok {
			resolvedIDs = append(resolvedIDs, resolved.ID)
		} else {
			resolvedIDs = append(resolvedIDs, id)
		}
	}

	results, err := Describe(opMap, resolvedIDs)
	if err != nil {
		return toolMsg(call.ID, call.Function.Name, fmt.Sprintf("Error: describing operations: %v", err))
	}

	data, err := json.Marshal(results)
	if err != nil {
		return toolMsg(call.ID, call.Function.Name, fmt.Sprintf("Error: serializing describe results: %v", err))
	}

	return toolMsg(call.ID, call.Function.Name, string(data))
}

func (p *ToolProvider) handleCall(ctx context.Context, call model.ToolCall) *model.Message {
	var rawArgs struct {
		OperationID string `json:"operation_id"`
		Params      any    `json:"params"`
	}
	if err := json.Unmarshal([]byte(call.Function.Arguments), &rawArgs); err != nil {
		return toolMsg(call.ID, call.Function.Name, fmt.Sprintf("Error: parsing arguments: %v", err))
	}

	opID := strings.Trim(strings.TrimSpace(rawArgs.OperationID), "\"'` \t\r\n")
	if opID == "" {
		return toolMsg(call.ID, call.Function.Name, "Error: missing required parameter 'operation_id'")
	}

	params := make(map[string]any)
	switch p := rawArgs.Params.(type) {
	case map[string]any:
		params = p
	case string:
		p = strings.TrimSpace(p)
		if p != "" && p != "{}" {
			var parsed map[string]any
			if err := json.Unmarshal([]byte(p), &parsed); err == nil {
				params = parsed
			}
		}
	}

	op, ok := p.resolveOperation(opID)
	if !ok {
		return toolMsg(call.ID, call.Function.Name, fmt.Sprintf("Error: Operation %q not found. Use openapi_search to find valid operations.", opID))
	}

	p.mu.RLock()
	idx, cfgOK := p.specMap[op.API]
	p.mu.RUnlock()

	if !cfgOK {
		return toolMsg(call.ID, call.Function.Name, fmt.Sprintf("Error: spec config for %q not found", op.API))
	}

	return ExecuteToolCall(ctx, call.ID, op, &p.specs[idx], params, p.AdminKey)
}
