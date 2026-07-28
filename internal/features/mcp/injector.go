package mcp

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/ilter-ai/ilter/internal/db"

	"github.com/ilter-ai/ilter/internal/model"
)

// Injector converts MCP ToolDefinitions into OpenAI function-calling tools
// and resolves which tools a given API key is authorized to use.
type Injector struct {
	registry   *Registry
	authorizer *Authorizer
	store      *db.SQLiteStore
}

func NewInjector(registry *Registry, authorizer *Authorizer, store *db.SQLiteStore) *Injector {
	return &Injector{
		registry:   registry,
		authorizer: authorizer,
		store:      store,
	}
}

// GetAuthorizedOpenAITools returns the list of MCP tools the caller is
// allowed to use, converted to OpenAI function-calling format (model.Tool).
// keyID may be "" (admin / anonymous).  groupIDs is optional; pass nil to skip.
func (inj *Injector) GetAuthorizedOpenAITools(keyID string, groupIDs []int) []model.Tool {
	allTools := inj.registry.ListTools()
	if len(allTools) == 0 {
		return nil
	}

	toolNames := make([]string, 0, len(allTools))
	for _, ti := range allTools {
		toolNames = append(toolNames, ti.Tool.Name)
	}

	keyPrefix := inj.resolveKeyPrefix(keyID)

	authorized := inj.authorizer.GetAuthorizedTools(keyPrefix, groupIDs, keyID, toolNames)
	if len(authorized) == 0 {
		return nil
	}
	authSet := make(map[string]bool, len(authorized))
	for _, name := range authorized {
		authSet[name] = true
	}

	// Only namespace a tool name with its server ID when the bare name
	// collides across 2+ distinct servers — matches Registry.ResolveTool's
	// own conflict detection (registry.go) and Gateway.handleToolsList's
	// native MCP tools/list behavior (gateway.go). Collisions are computed
	// over every registered tool (not just this key's authorized subset) so
	// the name shown to the LLM stays resolvable regardless of which key is
	// asking. Weak models frequently fail to reproduce compound
	// "server__tool" names correctly (emitting a malformed or empty function
	// name) — prefixing only when genuinely ambiguous keeps most tool names
	// short and reliable to call.
	nameServers := make(map[string]map[string]struct{}, len(allTools))
	for _, ti := range allTools {
		if nameServers[ti.Tool.Name] == nil {
			nameServers[ti.Tool.Name] = make(map[string]struct{})
		}
		nameServers[ti.Tool.Name][ti.ServerID] = struct{}{}
	}

	out := make([]model.Tool, 0, len(authorized))
	for _, ti := range allTools {
		if authSet[ti.Tool.Name] {
			t := convertTool(ti.Tool)
			if len(nameServers[ti.Tool.Name]) > 1 {
				t.Function.Name = SanitizeToolName(ti.ServerID, t.Function.Name)
			}
			out = append(out, t)
		}
	}

	if len(out) > 0 {
		if MCPToolsInjected != nil {
			MCPToolsInjected.Add(context.Background(), int64(len(out)))
		}
	}

	return out
}

func IsSyntheticKeyID(keyID string) bool {
	return keyID == "admin" || strings.HasPrefix(keyID, "dev:")
}

// resolveKeyPrefix queries the database for the key prefix of the given
// API key ID.  Returns "" on error, for admin/dev keys, or empty keyID.
func (inj *Injector) resolveKeyPrefix(keyID string) string {
	if keyID == "" || inj.store == nil || IsSyntheticKeyID(keyID) {
		return ""
	}
	prefix, err := ExtractKeyInfo(keyID, inj.store)
	if err != nil {
		mcpLog.Debug("failed to resolve key prefix", "key_id", keyID, "error", err)
		return ""
	}
	return prefix
}

func convertTool(td ToolDefinition) model.Tool {
	params := make(map[string]any)
	if len(td.InputSchema) > 0 {
		if err := json.Unmarshal(td.InputSchema, &params); err != nil {
			mcpLog.Warn("failed to parse tool input schema",
				"tool", td.Name, "error", err)
			// Fall back to an empty schema so the LLM still sees the tool.
			params = map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			}
		}
	} else {
		// Default to an empty object schema when none is provided.
		params = map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
	}

	// Ensure the top-level "type" field is set (it is required by OpenAI).
	if _, ok := params["type"]; !ok {
		params["type"] = "object"
	}

	return model.Tool{
		Type: "function",
		Function: model.ToolFunction{
			Name:        td.Name,
			Description: td.Description,
			Parameters:  params,
		},
	}
}
