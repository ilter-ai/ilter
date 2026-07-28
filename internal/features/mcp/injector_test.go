package mcp

import (
	"encoding/json"
	"testing"

	"github.com/ilter-ai/ilter/internal/config"
)

func TestConvertTool_WithSchema(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)
	td := ToolDefinition{
		Name:        "my-tool",
		Description: "Does something",
		InputSchema: schema,
	}
	tool := convertTool(td)
	if tool.Type != "function" {
		t.Fatalf("expected function type, got %s", tool.Type)
	}
	if tool.Function.Name != "my-tool" {
		t.Fatalf("expected my-tool, got %s", tool.Function.Name)
	}
	params := tool.Function.Parameters
	if params["type"] != "object" {
		t.Fatalf("expected type=object, got %v", params["type"])
	}
	props, ok := params["properties"].(map[string]any)
	if !ok || props["name"] == nil {
		t.Fatal("expected properties with name field")
	}
}

func TestConvertTool_EmptySchema(t *testing.T) {
	td := ToolDefinition{Name: "empty-schema"}
	tool := convertTool(td)
	params := tool.Function.Parameters
	if params["type"] != "object" {
		t.Fatalf("expected type=object, got %v", params["type"])
	}
}

func TestConvertTool_InvalidSchema(t *testing.T) {
	// Invalid JSON schema → falls back to empty object schema.
	td := ToolDefinition{
		Name:        "bad-schema",
		InputSchema: json.RawMessage(`{invalid}`),
	}
	tool := convertTool(td)
	params := tool.Function.Parameters
	if params["type"] != "object" {
		t.Fatalf("expected fallback type=object, got %v", params["type"])
	}
}

func TestConvertTool_MissingType(t *testing.T) {
	// Schema missing "type" field → should be injected.
	schema := json.RawMessage(`{"properties":{"x":{}}}`)
	td := ToolDefinition{Name: "no-type", InputSchema: schema}
	tool := convertTool(td)
	params := tool.Function.Parameters
	if params["type"] != "object" {
		t.Fatalf("expected injected type=object, got %v", params["type"])
	}
}

func TestGetAuthorizedOpenAITools_Empty(t *testing.T) {
	reg := &Registry{servers: map[string]*ServerInfo{}}
	auth := NewAuthorizer(nil, nil, "deny")
	inj := NewInjector(reg, auth, nil)

	tools := inj.GetAuthorizedOpenAITools("", nil)
	if len(tools) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(tools))
	}
}

func TestGetAuthorizedOpenAITools_AllAuthorized(t *testing.T) {
	reg := &Registry{
		servers: map[string]*ServerInfo{
			"s1": {
				ID: "s1", Config: config.MCPServerConfig{ID: "s1"},
				Tools: []ToolDefinition{
					{Name: "alpha", Description: "Alpha tool"},
					{Name: "beta", Description: "Beta tool"},
				},
			},
		},
	}
	// Wildcard rule authorizes everything.
	auth := NewAuthorizer(nil, []config.MCPAccessRule{
		{Tools: []string{"*/*"}},
	}, "deny")
	inj := NewInjector(reg, auth, nil)

	tools := inj.GetAuthorizedOpenAITools("", nil)
	if len(tools) != 2 {
		t.Fatalf("expected 2 authorized tools, got %d", len(tools))
	}
	// No other server offers "alpha"/"beta" — names stay bare, unprefixed.
	if tools[0].Function.Name != "alpha" {
		t.Fatalf("expected first tool 'alpha', got %s", tools[0].Function.Name)
	}
	if tools[1].Function.Name != "beta" {
		t.Fatalf("expected second tool 'beta', got %s", tools[1].Function.Name)
	}
}

func TestGetAuthorizedOpenAITools_NameCollisionAcrossServers(t *testing.T) {
	reg := &Registry{
		servers: map[string]*ServerInfo{
			"s1": {
				ID: "s1", Config: config.MCPServerConfig{ID: "s1"},
				Tools: []ToolDefinition{
					{Name: "fetch", Description: "Fetch from s1"},
					{Name: "unique-s1", Description: "Only on s1"},
				},
			},
			"s2": {
				ID: "s2", Config: config.MCPServerConfig{ID: "s2"},
				Tools: []ToolDefinition{
					{Name: "fetch", Description: "Fetch from s2"},
				},
			},
		},
	}
	auth := NewAuthorizer(nil, []config.MCPAccessRule{
		{Tools: []string{"*/*"}},
	}, "deny")
	inj := NewInjector(reg, auth, nil)

	tools := inj.GetAuthorizedOpenAITools("", nil)
	if len(tools) != 3 {
		t.Fatalf("expected 3 authorized tools, got %d", len(tools))
	}
	names := make(map[string]bool, len(tools))
	for _, tl := range tools {
		names[tl.Function.Name] = true
	}
	// "fetch" collides between s1 and s2 — both must be namespaced.
	if !names["s1__fetch"] || !names["s2__fetch"] {
		t.Fatalf("expected colliding tool namespaced as s1__fetch/s2__fetch, got %v", names)
	}
	// "unique-s1" has no collision — stays bare.
	if !names["unique-s1"] {
		t.Fatalf("expected non-colliding tool to stay bare as 'unique-s1', got %v", names)
	}
}

func TestGetAuthorizedOpenAITools_PartialAuth(t *testing.T) {
	reg := &Registry{
		servers: map[string]*ServerInfo{
			"s1": {
				ID: "s1", Config: config.MCPServerConfig{ID: "s1"},
				Tools: []ToolDefinition{
					{Name: "public", Description: "Public"},
					{Name: "secret", Description: "Secret"},
				},
			},
		},
	}
	// Only authorize "public".
	auth := NewAuthorizer(nil, []config.MCPAccessRule{
		{Tools: []string{"public"}},
	}, "deny")
	inj := NewInjector(reg, auth, nil)

	tools := inj.GetAuthorizedOpenAITools("", nil)
	if len(tools) != 1 {
		t.Fatalf("expected 1 authorized tool, got %d", len(tools))
	}
	// No other server offers "public" — name stays bare, unprefixed.
	if tools[0].Function.Name != "public" {
		t.Fatalf("expected 'public', got %s", tools[0].Function.Name)
	}
}

func TestResolveKeyPrefix_Admin(t *testing.T) {
	inj := NewInjector(nil, nil, nil)
	prefix := inj.resolveKeyPrefix("")
	if prefix != "" {
		t.Fatalf("expected empty prefix for admin, got %s", prefix)
	}
}

func TestResolveKeyPrefix_NilStore(t *testing.T) {
	inj := NewInjector(nil, nil, nil)
	prefix := inj.resolveKeyPrefix("42")
	if prefix != "" {
		t.Fatalf("expected empty prefix when store is nil, got %s", prefix)
	}
}
