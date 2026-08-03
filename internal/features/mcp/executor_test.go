package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sony/gobreaker/v2"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/mcp/protocol"
)

// mockTransport implements TransportClient for executor tests.
type mockTransportForExecutor struct {
	connected bool
	responses map[string]*JSONRPCResponse // keyed by method name or full body hash
	callCount atomic.Int32
	failCount int // number of times Call should fail before succeeding
}

func (m *mockTransportForExecutor) Start(_ context.Context) error  { return nil }
func (m *mockTransportForExecutor) Close() error                   { return nil }
func (m *mockTransportForExecutor) IsConnected() bool              { return m.connected }
func (m *mockTransportForExecutor) Tools() []ToolDefinition        { return nil }
func (m *mockTransportForExecutor) NegotiatedVersion() protocol.ID { return protocol.V20250326 }

func (m *mockTransportForExecutor) Call(_ context.Context, req *JSONRPCRequest) (*JSONRPCResponse, error) {
	m.callCount.Add(1)
	if m.failCount > 0 {
		m.failCount--
		return nil, fmt.Errorf("simulated failure")
	}
	if resp, ok := m.responses[req.Method]; ok {
		return resp, nil
	}
	return NewSuccessResponse(req.ID, map[string]any{"ok": true}), nil
}

func TestExecutor_ExecuteTool_Success(t *testing.T) {
	cleanBreakers()

	mock := &mockTransportForExecutor{connected: true}
	server := &ServerInfo{
		ID: "test-server",
		Config: config.MCPServerConfig{
			ID: "test-server", Name: "Test Server", Transport: "sse",
			URL: "http://localhost:9999", Timeout: "5s",
		},
		Tools: []ToolDefinition{{Name: "test-tool", Description: "A test tool"}},
	}

	clients := NewClientManager(nil)
	insertMockClient(clients, "test-server", "sse", mock)
	reg := &Registry{servers: map[string]*ServerInfo{"test-server": server}}

	ex := NewExecutor(reg, clients, nil, nil, nil)
	result := ex.ExecuteTool(context.Background(), &ExecuteToolParams{
		ToolName: "test-tool", Arguments: json.RawMessage(`{"foo":"bar"}`),
	})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].Text)
	}
}

func TestExecutor_ExecuteTool_ToolNotFound(t *testing.T) {
	reg := &Registry{servers: map[string]*ServerInfo{}}
	clients := NewClientManager(nil)
	ex := NewExecutor(reg, clients, nil, nil, nil)

	result := ex.ExecuteTool(context.Background(), &ExecuteToolParams{ToolName: "nonexistent"})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.IsError {
		t.Fatal("expected error result for unknown tool")
	}
}

func TestExecutor_ExecuteTool_AccessDenied(t *testing.T) {
	reg := &Registry{
		servers: map[string]*ServerInfo{
			"test-server": {
				ID: "test-server",
				Config: config.MCPServerConfig{
					ID: "test-server", Name: "Test Server", Transport: "inline", Timeout: "5s",
				},
				Tools: []ToolDefinition{{Name: "secret-tool", Description: "Secret"}},
			},
		},
	}
	clients := NewClientManager(nil)

	// Authorizer with only a rule that allows nothing by default (no rules = deny all? Actually empty rules means no rules to check, and CheckAccess returns not allowed.)
	// An empty rules slice means no rules match → Allowed: false.
	auth := NewAuthorizer(nil, []config.MCPAccessRule{}, "deny")

	ex := NewExecutor(reg, clients, auth, nil, nil)

	result := ex.ExecuteTool(context.Background(), &ExecuteToolParams{
		ToolName: "secret-tool", APIKeyID: "1", KeyPrefix: "abc123def456",
	})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.IsError {
		t.Fatal("expected access denied error")
	}
}

func TestExecutor_ExecuteTool_RetryThenSuccess(t *testing.T) {
	breakersMu.Lock()
	breakers = make(map[string]*gobreaker.CircuitBreaker[*JSONRPCResponse])
	breakersMu.Unlock()

	mock := &mockTransportForExecutor{
		connected: true,
		failCount: 2, // fail twice, succeed on 3rd
	}

	clients := NewClientManager(nil)
	server := &ServerInfo{
		ID: "retry-server",
		Config: config.MCPServerConfig{
			ID: "retry-server", Name: "Retry", Transport: "sse", URL: "http://localhost:9999",
			Timeout: "5s", MaxRetries: 3,
		},
		Tools: []ToolDefinition{{Name: "retry-tool", Description: "Retry test"}},
	}

	reg := &Registry{servers: map[string]*ServerInfo{"retry-server": server}}

	// Register mock transport in clients map directly so GetOrCreate returns it.
	key := clientKey{serverID: "retry-server", transport: "sse"}
	clients.mu.Lock()
	clients.clients[key] = mock
	clients.mu.Unlock()

	ex := NewExecutor(reg, clients, nil, nil, nil)

	result := ex.ExecuteTool(context.Background(), &ExecuteToolParams{
		ToolName: "retry-tool",
	})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.IsError {
		t.Fatalf("expected success after retries, got: %s", result.Content[0].Text)
	}
	if n := mock.callCount.Load(); n != 3 {
		t.Fatalf("expected 3 call attempts, got %d", n)
	}
}

func TestParseDurationOrDefault(t *testing.T) {
	tests := []struct {
		input string
		def   time.Duration
		want  time.Duration
	}{
		{"5s", time.Minute, 5 * time.Second},
		{"100ms", time.Minute, 100 * time.Millisecond},
		{"", time.Minute, time.Minute},
		{"invalid", time.Minute, time.Minute},
	}
	for _, tc := range tests {
		got := parseDurationOrDefault(tc.input, tc.def)
		if got != tc.want {
			t.Errorf("parseDurationOrDefault(%q, %v) = %v, want %v", tc.input, tc.def, got, tc.want)
		}
	}
}

func TestParseToolResult(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		r := parseToolResult(nil)
		if len(r.Content) != 1 || r.Content[0].Text != "ok" {
			t.Fatalf("unexpected result: %+v", r)
		}
	})

	t.Run("CallToolResult input", func(t *testing.T) {
		ctr := &CallToolResult{Content: []ToolContent{{Type: "text", Text: "hello"}}}
		r := parseToolResult(ctr)
		if r.Content[0].Text != "hello" {
			t.Fatalf("expected hello, got %s", r.Content[0].Text)
		}
	})

	t.Run("map input", func(t *testing.T) {
		m := map[string]any{
			"content": []any{
				map[string]any{"type": "text", "text": "from map"},
			},
		}
		r := parseToolResult(m)
		if r.Content[0].Text != "from map" {
			t.Fatalf("expected 'from map', got %s", r.Content[0].Text)
		}
	})

	t.Run("map with empty content", func(t *testing.T) {
		m := map[string]any{"isError": false}
		r := parseToolResult(m)
		if len(r.Content) != 1 || r.Content[0].Text != "ok" {
			t.Fatalf("expected fallback 'ok', got %+v", r.Content)
		}
	})

	t.Run("raw string fallback", func(t *testing.T) {
		r := parseToolResult("raw string")
		if len(r.Content) != 1 || r.Content[0].Text != `"raw string"` {
			t.Fatalf("unexpected fallback: %+v", r.Content[0])
		}
	})
}

func TestErrorResult(t *testing.T) {
	r := errorResult("my-tool", "something went wrong")
	if !r.IsError {
		t.Fatal("expected IsError")
	}
	if r.Content[0].Text != "something went wrong" {
		t.Fatalf("unexpected message: %s", r.Content[0].Text)
	}
}

func TestToRawMessage(t *testing.T) {
	r := toRawMessage(map[string]string{"a": "b"})
	if r == nil {
		t.Fatal("expected non-nil")
	}
	var decoded map[string]string
	if err := json.Unmarshal(*r, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if decoded["a"] != "b" {
		t.Fatalf("unexpected value: %v", decoded)
	}
}

func TestToRawMessage_Nil(t *testing.T) {
	r := toRawMessage(make(chan int))
	if r != nil {
		t.Fatal("expected nil for unjsonable input")
	}
}

func cleanBreakers() {
	breakersMu.Lock()
	breakers = make(map[string]*gobreaker.CircuitBreaker[*JSONRPCResponse])
	breakersMu.Unlock()
}

func insertMockClient(m *ClientManager, serverID, transport string, cl TransportClient) {
	key := clientKey{serverID: serverID, transport: transport}
	m.mu.Lock()
	m.clients[key] = cl
	m.mu.Unlock()
}

// --- Security check tests ---

func TestExecutor_ExecuteTool_DestructiveBlocked(t *testing.T) {
	cleanBreakers()
	mock := &mockTransportForExecutor{connected: true}
	clients := NewClientManager(nil)
	insertMockClient(clients, "test-server", "sse", mock)

	reg := &Registry{servers: map[string]*ServerInfo{
		"test-server": {
			ID:     "test-server",
			Config: config.MCPServerConfig{ID: "test-server", Transport: "sse", URL: "http://localhost:9999"},
			Tools:  []ToolDefinition{{Name: "danger-tool"}},
		},
	}}

	ex := NewExecutor(reg, clients, nil, nil, nil)
	ex.toolConfigCache.Store("danger-tool", &ToolConfig{Destructive: true})

	result := ex.ExecuteTool(context.Background(), &ExecuteToolParams{ToolName: "danger-tool"})
	if !result.IsError {
		t.Fatal("expected error for destructive tool")
	}
	if result.Content[0].Text != "Tool call blocked: destructive tool not allowed" {
		t.Fatalf("unexpected message: %s", result.Content[0].Text)
	}
}

func TestExecutor_ExecuteTool_RequiresConfirmationBlocked(t *testing.T) {
	cleanBreakers()
	mock := &mockTransportForExecutor{connected: true}
	clients := NewClientManager(nil)
	insertMockClient(clients, "test-server", "sse", mock)

	reg := &Registry{servers: map[string]*ServerInfo{
		"test-server": {
			ID:     "test-server",
			Config: config.MCPServerConfig{ID: "test-server", Transport: "sse", URL: "http://localhost:9999"},
			Tools:  []ToolDefinition{{Name: "confirm-tool"}},
		},
	}}

	ex := NewExecutor(reg, clients, nil, nil, nil)
	ex.toolConfigCache.Store("confirm-tool", &ToolConfig{RequiresConfirmation: true})

	result := ex.ExecuteTool(context.Background(), &ExecuteToolParams{ToolName: "confirm-tool"})
	if !result.IsError {
		t.Fatal("expected error for confirmation-required tool")
	}
	if result.Content[0].Text != "Tool call blocked: tool requires manual confirmation" {
		t.Fatalf("unexpected message: %s", result.Content[0].Text)
	}
}

func TestExecutor_ExecuteTool_RateLimited(t *testing.T) {
	cleanBreakers()
	mock := &mockTransportForExecutor{connected: true}
	clients := NewClientManager(nil)
	insertMockClient(clients, "test-server", "sse", mock)

	reg := &Registry{servers: map[string]*ServerInfo{
		"test-server": {
			ID:     "test-server",
			Config: config.MCPServerConfig{ID: "test-server", Transport: "sse", URL: "http://localhost:9999"},
			Tools:  []ToolDefinition{{Name: "freq-tool"}},
		},
	}}

	ex := NewExecutor(reg, clients, nil, nil, nil)
	ex.toolConfigCache.Store("freq-tool", &ToolConfig{RateLimitRPM: 2})

	// First two calls should succeed.
	for i := range 2 {
		result := ex.ExecuteTool(context.Background(), &ExecuteToolParams{ToolName: "freq-tool"})
		if result.IsError {
			t.Fatalf("call %d should succeed, got: %s", i+1, result.Content[0].Text)
		}
	}

	// Third call should be rate-limited.
	result := ex.ExecuteTool(context.Background(), &ExecuteToolParams{ToolName: "freq-tool"})
	if !result.IsError {
		t.Fatal("expected rate limit error on third call")
	}
	if result.Content[0].Text != "Tool call blocked: rate limit exceeded" {
		t.Fatalf("unexpected message: %s", result.Content[0].Text)
	}
}

func TestExecutor_ExecuteTool_NoConfigMeansNoRestriction(t *testing.T) {
	cleanBreakers()
	mock := &mockTransportForExecutor{connected: true}
	clients := NewClientManager(nil)
	insertMockClient(clients, "test-server", "sse", mock)

	reg := &Registry{servers: map[string]*ServerInfo{
		"test-server": {
			ID:     "test-server",
			Config: config.MCPServerConfig{ID: "test-server", Transport: "sse", URL: "http://localhost:9999"},
			Tools:  []ToolDefinition{{Name: "plain-tool"}},
		},
	}}

	// No tool config stored → no restrictions.
	ex := NewExecutor(reg, clients, nil, nil, nil)

	result := ex.ExecuteTool(context.Background(), &ExecuteToolParams{ToolName: "plain-tool"})
	if result.IsError {
		t.Fatalf("expected success, got: %s", result.Content[0].Text)
	}
}
