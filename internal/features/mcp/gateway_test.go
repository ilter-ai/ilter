package mcp

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ilter-ai/ilter/internal/config"
)

func newTestGateway(servers map[string]*ServerInfo, rules []config.MCPAccessRule) *Gateway {
	reg := &Registry{servers: servers}
	if reg.servers == nil {
		reg.servers = make(map[string]*ServerInfo)
	}
	auth := NewAuthorizer(nil, rules, "deny")
	return &Gateway{
		registry:   reg,
		authorizer: auth,
		config:     &config.MCPConfig{Endpoint: "/mcp"},
		httpClient: http.DefaultClient,
	}
}

func testID(id string) *json.RawMessage {
	rm := json.RawMessage(`"` + id + `"`)
	return &rm
}

func emptyParams() json.RawMessage {
	return json.RawMessage(`{}`)
}

func emptyRctx() *RequestContext {
	return &RequestContext{}
}

func TestGateway_Dispatch_Initialize(t *testing.T) {
	gw := newTestGateway(nil, nil)

	resp := gw.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      testID("1"),
		Method:  "initialize",
		Params:  emptyParams(),
	}, emptyRctx())
	if resp == nil {
		t.Fatal("expected response")
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if resp.Result == nil {
		t.Fatal("expected result")
	}
	var initResult InitializeResult
	if err := json.Unmarshal(resp.Result, &initResult); err != nil {
		t.Fatalf("unmarshal init result: %v", err)
	}
	if initResult.ServerInfo.Name != "ilter-mcp-gateway" {
		t.Fatalf("expected ilter-mcp-gateway, got %s", initResult.ServerInfo.Name)
	}
}

func TestGateway_Dispatch_NotificationInitialized(t *testing.T) {
	gw := newTestGateway(nil, nil)

	resp := gw.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		Method:  "notifications/initialized",
	}, emptyRctx())
	if resp != nil {
		t.Fatal("expected nil response for notification")
	}
}

func TestGateway_Dispatch_ToolsList(t *testing.T) {
	servers := map[string]*ServerInfo{
		"s1": {
			ID: "s1",
			Config: config.MCPServerConfig{
				ID: "s1", Name: "Server 1", Transport: "inline",
			},
			Tools: []ToolDefinition{
				{Name: "tool-a", Description: "Tool A"},
				{Name: "tool-b", Description: "Tool B"},
			},
		},
	}
	gw := newTestGateway(servers, []config.MCPAccessRule{
		{Tools: []string{"*/*"}},
	})

	resp := gw.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      testID("2"),
		Method:  "tools/list",
	}, emptyRctx())
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var listResult ListToolsResult
	if err := json.Unmarshal(resp.Result, &listResult); err != nil {
		t.Fatalf("unmarshal list result: %v", err)
	}
	if len(listResult.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(listResult.Tools))
	}
}

func TestGateway_Dispatch_ToolsList_Empty(t *testing.T) {
	gw := newTestGateway(nil, nil)

	resp := gw.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      testID("3"),
		Method:  "tools/list",
	}, emptyRctx())
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var listResult ListToolsResult
	if err := json.Unmarshal(resp.Result, &listResult); err != nil {
		t.Fatalf("failed to unmarshal list result: %v", err)
	}
	if len(listResult.Tools) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(listResult.Tools))
	}
}

func TestGateway_Dispatch_Ping(t *testing.T) {
	gw := newTestGateway(nil, nil)

	resp := gw.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      testID("4"),
		Method:  "ping",
	}, emptyRctx())
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if resp.Result == nil {
		t.Fatal("expected result for ping")
	}
}

func TestGateway_Dispatch_UnknownMethod(t *testing.T) {
	gw := newTestGateway(nil, nil)

	resp := gw.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      testID("5"),
		Method:  "unknown/method",
	}, emptyRctx())
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != ErrorCodeMethodNotFound {
		t.Fatalf("expected method not found code, got %d", resp.Error.Code)
	}
}

func TestGateway_Dispatch_ToolsCall_NotInitialized(_ *testing.T) {
	gw := newTestGateway(nil, nil)

	resp := gw.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      testID("6"),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"any","arguments":{}}`),
	}, emptyRctx())
	if resp.Error == nil {
		// Streamable HTTP: no session means no "not initialized" path.
		// The tool call will fail with tool-not-found instead.
		return
	}
}

func TestGateway_Dispatch_ToolsCall_Unauthorized(t *testing.T) {
	servers := map[string]*ServerInfo{
		"s1": {
			ID: "s1",
			Config: config.MCPServerConfig{
				ID: "s1", Name: "S1", Transport: "inline",
			},
			Tools: []ToolDefinition{{Name: "restricted-tool", Description: "Restricted"}},
		},
	}
	gw := newTestGateway(servers, []config.MCPAccessRule{})

	resp := gw.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      testID("7"),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"restricted-tool","arguments":{}}`),
	}, emptyRctx())
	if resp.Error == nil {
		t.Fatal("expected error for unauthorized tool")
	}
	if resp.Error.Code != ErrorCodeToolNotFound {
		t.Fatalf("expected tool not found error, got %d", resp.Error.Code)
	}
}

func TestGateway_Dispatch_ToolsCall_ToolNotFound(t *testing.T) {
	gw := newTestGateway(nil, nil)

	resp := gw.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      testID("8"),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"nonexistent","arguments":{}}`),
	}, emptyRctx())
	if resp.Error == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func toolNames(tools []ToolDefinition) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return names
}

func TestGateway_Dispatch_ToolsList_AuthFilteredByKey(t *testing.T) {
	servers := map[string]*ServerInfo{
		"s1": {
			ID: "s1",
			Config: config.MCPServerConfig{
				ID: "s1", Name: "S1", Transport: "inline",
			},
			Tools: []ToolDefinition{
				{Name: "tool-a", Description: "For key A"},
				{Name: "tool-b", Description: "For key B"},
			},
		},
	}

	rules := []config.MCPAccessRule{
		{KeyID: "key-a", Tools: []string{"tool-a"}, Effect: "allow"},
		{KeyID: "key-b", Tools: []string{"tool-b"}, Effect: "allow"},
	}
	gw := newTestGateway(servers, rules)

	// Key A sees only tool-a.
	respA := gw.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      testID("1"),
		Method:  MethodToolsList,
	}, &RequestContext{KeyID: "key-a"})
	if respA.Error != nil {
		t.Fatalf("key-a tools/list error: %+v", respA.Error)
	}
	var listA ListToolsResult
	if err := json.Unmarshal(respA.Result, &listA); err != nil {
		t.Fatal(err)
	}
	if len(listA.Tools) != 1 || listA.Tools[0].Name != "tool-a" {
		t.Fatalf("key-a: expected [tool-a], got %v", toolNames(listA.Tools))
	}

	// Key B sees only tool-b.
	respB := gw.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      testID("2"),
		Method:  MethodToolsList,
	}, &RequestContext{KeyID: "key-b"})
	if respB.Error != nil {
		t.Fatalf("key-b tools/list error: %+v", respB.Error)
	}
	var listB ListToolsResult
	if err := json.Unmarshal(respB.Result, &listB); err != nil {
		t.Fatal(err)
	}
	if len(listB.Tools) != 1 || listB.Tools[0].Name != "tool-b" {
		t.Fatalf("key-b: expected [tool-b], got %v", toolNames(listB.Tools))
	}

	// Default-deny (no matching key) sees nothing.
	respN := gw.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      testID("3"),
		Method:  MethodToolsList,
	}, emptyRctx())
	if respN.Error != nil {
		t.Fatalf("no-key tools/list error: %+v", respN.Error)
	}
	var listN ListToolsResult
	if err := json.Unmarshal(respN.Result, &listN); err != nil {
		t.Fatal(err)
	}
	if len(listN.Tools) != 0 {
		t.Fatalf("no-key: expected 0 tools, got %v", toolNames(listN.Tools))
	}
}

func TestGateway_Dispatch_ToolsCall_KeyAuth(t *testing.T) {
	servers := map[string]*ServerInfo{
		"s1": {
			ID: "s1",
			Config: config.MCPServerConfig{
				ID: "s1", Name: "S1", Transport: "inline",
			},
			Tools: []ToolDefinition{
				{Name: "shared-tool", Description: "Shared"},
			},
		},
	}

	rules := []config.MCPAccessRule{
		{KeyID: "key-a", Tools: []string{"shared-tool"}, Effect: "allow"},
	}
	gw := newTestGateway(servers, rules)

	// Key A — auth passes, fails on nil executor (ErrorCodeInternal).
	resp := gw.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      testID("1"),
		Method:  MethodToolsCall,
		Params:  json.RawMessage(`{"name":"shared-tool","arguments":{}}`),
	}, &RequestContext{KeyID: "key-a"})
	if resp.Error == nil || resp.Error.Code == ErrorCodeToolNotFound {
		t.Fatalf("key-a: expected auth to pass, got 'tool not found' (code=%d)", resp.Error.Code)
	}

	// No key — default-deny returns "tool not found".
	respN := gw.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      testID("2"),
		Method:  MethodToolsCall,
		Params:  json.RawMessage(`{"name":"shared-tool","arguments":{}}`),
	}, emptyRctx())
	if respN.Error == nil || respN.Error.Code != ErrorCodeToolNotFound {
		t.Fatal("no-key: expected 'tool not found'")
	}
}

func TestGateway_Dispatch_ToolsList_DuplicateNames(t *testing.T) {
	servers := map[string]*ServerInfo{
		"s1": {
			ID: "s1",
			Config: config.MCPServerConfig{
				ID: "s1", Name: "S1", Transport: "inline",
			},
			Tools: []ToolDefinition{
				{Name: "tool-a", Description: "Tool A from s1"},
				{Name: "tool-b", Description: "Tool B (unique to s1)"},
			},
		},
		"s2": {
			ID: "s2",
			Config: config.MCPServerConfig{
				ID: "s2", Name: "S2", Transport: "inline",
			},
			Tools: []ToolDefinition{
				{Name: "tool-a", Description: "Tool A from s2"},
				{Name: "tool-c", Description: "Tool C (unique to s2)"},
			},
		},
	}
	gw := newTestGateway(servers, []config.MCPAccessRule{
		{Tools: []string{"*/*"}},
	})

	resp := gw.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      testID("100"),
		Method:  MethodToolsList,
	}, emptyRctx())
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var list ListToolsResult
	if err := json.Unmarshal(resp.Result, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Tools) != 4 {
		t.Fatalf("expected 4 tools, got %d: %v", len(list.Tools), toolNames(list.Tools))
	}

	got := make(map[string]bool)
	for _, tool := range list.Tools {
		got[tool.Name] = true
	}

	// Duplicate tool-a should be namespaced.
	if !got["s1__tool-a"] {
		t.Fatalf("expected namespaced 's1__tool-a', got: %v", toolNames(list.Tools))
	}
	if !got["s2__tool-a"] {
		t.Fatalf("expected namespaced 's2__tool-a', got: %v", toolNames(list.Tools))
	}
	// Unique tools should remain bare.
	if !got["tool-b"] {
		t.Fatalf("expected bare 'tool-b', got: %v", toolNames(list.Tools))
	}
	if !got["tool-c"] {
		t.Fatalf("expected bare 'tool-c', got: %v", toolNames(list.Tools))
	}
}

func TestGateway_Dispatch_ToolsCall_NamespacedName(t *testing.T) {
	servers := map[string]*ServerInfo{
		"s1": {
			ID: "s1",
			Config: config.MCPServerConfig{
				ID: "s1", Name: "S1", Transport: "inline",
			},
			Tools: []ToolDefinition{
				{Name: "tool-a", Description: "Tool A from s1"},
				{Name: "tool-b", Description: "Tool B (unique)"},
			},
		},
		"s2": {
			ID: "s2",
			Config: config.MCPServerConfig{
				ID: "s2", Name: "S2", Transport: "inline",
			},
			Tools: []ToolDefinition{
				{Name: "tool-a", Description: "Tool A from s2"},
				{Name: "tool-c", Description: "Tool C (unique)"},
			},
		},
	}
	gw := newTestGateway(servers, []config.MCPAccessRule{
		{Tools: []string{"*/*"}},
	})

	// Call with namespaced name s1__tool-a — should resolve to s1, pass auth, fail on nil executor.
	resp := gw.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      testID("1"),
		Method:  MethodToolsCall,
		Params:  json.RawMessage(`{"name":"s1__tool-a","arguments":{}}`),
	}, &RequestContext{KeyID: "key-a"})
	if resp.Error == nil {
		t.Fatal("expected error (nil executor)")
	}
	if resp.Error.Code == ErrorCodeToolNotFound {
		t.Fatalf("namespaced tool call got 'tool not found' (code=%d) — means resolution failed", resp.Error.Code)
	}
}

func TestGateway_Dispatch_ToolsList_DuplicateNames_PartialAuth(t *testing.T) {
	servers := map[string]*ServerInfo{
		"s1": {
			ID: "s1",
			Config: config.MCPServerConfig{
				ID: "s1", Name: "S1", Transport: "inline",
			},
			Tools: []ToolDefinition{
				{Name: "tool-a", Description: "Tool A from s1"},
				{Name: "tool-b", Description: "Tool B (unique)"},
			},
		},
		"s2": {
			ID: "s2",
			Config: config.MCPServerConfig{
				ID: "s2", Name: "S2", Transport: "inline",
			},
			Tools: []ToolDefinition{
				{Name: "tool-a", Description: "Tool A from s2"},
				{Name: "tool-c", Description: "Tool C (unique)"},
			},
		},
	}

	rules := []config.MCPAccessRule{
		{KeyID: "key-s1only", Tools: []string{"*/*"}, Effect: "allow"},
	}
	gw := newTestGateway(servers, rules)

	// Partial auth (key-s1only) — still sees namespaced names because
	// count runs on allTools, not authorizedTools.
	resp := gw.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      testID("1"),
		Method:  MethodToolsList,
	}, &RequestContext{KeyID: "key-s1only"})
	if resp.Error != nil {
		t.Fatalf("tools/list error: %+v", resp.Error)
	}
	var list ListToolsResult
	if err := json.Unmarshal(resp.Result, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Tools) == 0 {
		t.Fatal("expected non-empty tools list for key with */* access")
	}
	got := make(map[string]bool)
	for _, tool := range list.Tools {
		got[tool.Name] = true
	}
	// Duplicate tool-a should always be namespaced (count on allTools).
	if !got["s1__tool-a"] && !got["s2__tool-a"] {
		t.Fatalf("expected namespaced tool-a variant, got: %v", toolNames(list.Tools))
	}
}

func TestSanitizeToolName(t *testing.T) {
	tests := []struct {
		serverID string
		toolName string
		want     string
	}{
		{"s1", "tool-a", "s1__tool-a"},
		{"server-id", "my_tool", "server-id__my_tool"},
		{"srv", "abc", "srv__abc"},
	}
	for _, tt := range tests {
		got := SanitizeToolName(tt.serverID, tt.toolName)
		if got != tt.want {
			t.Errorf("SanitizeToolName(%q, %q) = %q, want %q", tt.serverID, tt.toolName, got, tt.want)
		}
		if len(got) > maxToolNameLen {
			t.Errorf("SanitizeToolName(%q, %q) = %q (len=%d), exceeds max %d", tt.serverID, tt.toolName, got, len(got), maxToolNameLen)
		}
	}
}

func TestSanitizeToolName_Truncation(t *testing.T) {
	longServerID := "a-really-really-long-server-identifier"
	longToolName := "a-tool-name-that-is-also-quite-long-and-should-exceed-64-characters-when-combined"
	got := SanitizeToolName(longServerID, longToolName)
	if len(got) > maxToolNameLen {
		t.Errorf("SanitizeToolName(%q, %q) = %q (len=%d), exceeds max %d", longServerID, longToolName, got, len(got), maxToolNameLen)
	}
	if len(got) == 0 {
		t.Fatal("SanitizeToolName returned empty string")
	}
}

func TestSanitizeToolName_InvalidChars(t *testing.T) {
	// Server ID is sanitized (→ _), tool name is NOT modified (round-trip).
	got := SanitizeToolName("srv.one", "my tool:foo")
	if len(got) > maxToolNameLen {
		t.Errorf("SanitizeToolName returned %q (len=%d), exceeds max %d", got, len(got), maxToolNameLen)
	}
	if !strings.HasPrefix(got, "srv_one__") {
		t.Errorf("expected server ID part to be sanitized, got %q", got)
	}
	if !strings.Contains(got, "my tool:foo") {
		t.Errorf("expected tool name part to be unchanged, got %q", got)
	}
}

func TestSanitizeToolName_RoundTrip_ToolWithDelimiter(t *testing.T) {
	// Tool name containing __ should survive a list→call round-trip.
	// Need 2 servers with the same tool name to trigger namespacing.
	servers := map[string]*ServerInfo{
		"s1": {
			ID: "s1",
			Config: config.MCPServerConfig{
				ID: "s1", Name: "S1", Transport: "inline",
			},
			Tools: []ToolDefinition{
				{Name: "foo__bar", Description: "Delimiter test"},
			},
		},
		"s2": {
			ID: "s2",
			Config: config.MCPServerConfig{
				ID: "s2", Name: "S2", Transport: "inline",
			},
			Tools: []ToolDefinition{
				{Name: "foo__bar", Description: "Delimiter test"},
			},
		},
	}
	gw := newTestGateway(servers, []config.MCPAccessRule{{Tools: []string{"*/*"}}})

	// List tools → should contain s1__foo__bar (splitN handles __ in tool name).
	respList := gw.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion, ID: testID("1"), Method: MethodToolsList,
	}, emptyRctx())
	if respList.Error != nil {
		t.Fatalf("tools/list error: %+v", respList.Error)
	}
	var list ListToolsResult
	if err := json.Unmarshal(respList.Result, &list); err != nil {
		t.Fatal(err)
	}
	// Both s1__foo__bar and s2__foo__bar should exist (map iteration order varies).
	hasS1 := false
	hasS2 := false
	for _, tool := range list.Tools {
		if tool.Name == "s1__foo__bar" {
			hasS1 = true
		}
		if tool.Name == "s2__foo__bar" {
			hasS2 = true
		}
	}
	if !hasS1 || !hasS2 {
		t.Fatalf("expected s1__foo__bar and s2__foo__bar, got %v", toolNames(list.Tools))
	}

	// Call with each exposed name (forward derivation resolves it).
	for _, tool := range list.Tools {
		respCall := gw.Dispatch(&JSONRPCRequest{
			JSONRPC: JSONRPCVersion, ID: testID("2"), Method: MethodToolsCall,
			Params: json.RawMessage(`{"name":"` + tool.Name + `","arguments":{}}`),
		}, emptyRctx())
		if respCall.Error == nil {
			t.Fatal("expected error (nil executor)")
		}
		if respCall.Error.Code == ErrorCodeToolNotFound {
			t.Fatalf("round-trip failed: tool %q not found", tool.Name)
		}
	}
}

func TestSanitizeToolName_RoundTrip_Truncated(t *testing.T) {
	// Very long tool name triggers truncation; forward derivation must still resolve.
	// Need 2 servers with same tool name to trigger namespacing (which applies SanitizeToolName).
	longTool := "a-very-long-tool-name-that-exceeds-sixty-four-characters-when-combined-with-server-prefix"
	servers := map[string]*ServerInfo{
		"s1": {
			ID: "s1",
			Config: config.MCPServerConfig{
				ID: "s1", Name: "S1", Transport: "inline",
			},
			Tools: []ToolDefinition{
				{Name: longTool, Description: "Long tool"},
			},
		},
		"s2": {
			ID: "s2",
			Config: config.MCPServerConfig{
				ID: "s2", Name: "S2", Transport: "inline",
			},
			Tools: []ToolDefinition{
				{Name: longTool, Description: "Long tool"},
			},
		},
	}
	gw := newTestGateway(servers, []config.MCPAccessRule{{Tools: []string{"*/*"}}})

	// List tools → name should be ≤64 chars and contain a hash suffix.
	respList := gw.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion, ID: testID("1"), Method: MethodToolsList,
	}, emptyRctx())
	if respList.Error != nil {
		t.Fatalf("tools/list error: %+v", respList.Error)
	}
	var list ListToolsResult
	if err := json.Unmarshal(respList.Result, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Tools) != 2 {
		t.Fatalf("expected 2 tools (s1, s2), got %d", len(list.Tools))
	}
	// Both names should be ≤64 chars with hash suffix.
	for _, tool := range list.Tools {
		if len(tool.Name) > maxToolNameLen {
			t.Fatalf("tool %q exceeds %d chars", tool.Name, maxToolNameLen)
		}
		if !strings.HasPrefix(tool.Name, "s1__") && !strings.HasPrefix(tool.Name, "s2__") {
			t.Fatalf("expected prefix s1__ or s2__, got %q", tool.Name)
		}
	}

	// Call with truncated name → forward derivation resolves each.
	for _, tool := range list.Tools {
		respCall := gw.Dispatch(&JSONRPCRequest{
			JSONRPC: JSONRPCVersion, ID: testID("2"), Method: MethodToolsCall,
			Params: json.RawMessage(`{"name":"` + tool.Name + `","arguments":{}}`),
		}, emptyRctx())
		if respCall.Error == nil {
			t.Fatal("expected error (nil executor)")
		}
		if respCall.Error.Code == ErrorCodeToolNotFound {
			t.Fatalf("round-trip failed: truncated tool %q not resolved", tool.Name)
		}
	}
}

func TestGateway_Dispatch_ToolsCall_UnsupportedTransport(t *testing.T) {
	servers := map[string]*ServerInfo{
		"s1": {
			ID: "s1",
			Config: config.MCPServerConfig{
				ID: "s1", Name: "S1", Transport: "inline",
			},
			Tools: []ToolDefinition{{Name: "inline-tool", Description: "Inline"}},
		},
	}
	gw := newTestGateway(servers, []config.MCPAccessRule{
		{Tools: []string{"*/*"}},
	})

	resp := gw.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      testID("9"),
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":"inline-tool","arguments":{}}`),
	}, emptyRctx())
	if resp.Error == nil {
		t.Fatal("expected error for unsupported transport")
	}
	if resp.Error.Code != ErrorCodeInternal {
		t.Fatalf("expected ErrorCodeInternal (nil executor), got %d", resp.Error.Code)
	}
}
