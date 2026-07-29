package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/mcp/inline"
	"github.com/ilter-ai/ilter/internal/features/mcp/protocol"
)

func TestNewInlineClient(t *testing.T) {
	serverID := "test-inline-client"
	_ = inline.RegisterTools(serverID, testHandler, nil)
	t.Cleanup(func() {
		// inline has no deregister; unique ID avoids collision.
	})

	client, err := NewInlineClient(&ServerInfo{ID: serverID})
	if err != nil {
		t.Fatalf("NewInlineClient failed: %v", err)
	}
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.IsConnected() {
		t.Error("expected client to start disconnected")
	}
}

func TestNewInlineClientMissingHandler(t *testing.T) {
	_, err := NewInlineClient(&ServerInfo{ID: "no-such-server"})
	if err == nil {
		t.Fatal("expected error for missing handler")
	}
}

func TestInlineClientStartAndClose(t *testing.T) {
	serverID := "test-inline-start"
	_ = inline.RegisterTools(serverID, testHandler, nil)
	t.Cleanup(func() {})

	client, _ := NewInlineClient(&ServerInfo{ID: serverID})

	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if !client.IsConnected() {
		t.Error("expected client to be connected after Start")
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if client.IsConnected() {
		t.Error("expected client to be disconnected after Close")
	}
}

func TestInlineClientInitialize(t *testing.T) {
	serverID := "test-inline-init"
	_ = inline.RegisterTools(serverID, testHandler, nil)
	t.Cleanup(func() {})

	client, _ := NewInlineClient(&ServerInfo{ID: serverID})
	_ = client.Start(context.Background())

	id := json.RawMessage(`"1"`)
	resp, err := client.Call(context.Background(), &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      &id,
		Method:  "initialize",
	})
	if err != nil {
		t.Fatalf("Call(initialize) failed: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("expected no error, got %+v", resp.Error)
	}
	if resp.Result == nil {
		t.Fatal("expected non-nil result")
	}

	var result struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	// The client's NegotiatedVersion() defaults to newest (2026-07-28), but
	// that version removed the `initialize` method entirely — calling it
	// explicitly gracefully degrades to the newest version that still
	// defines it (2025-03-26), mirroring gateway.go/hub.go's behavior.
	if result.ProtocolVersion != string(protocol.V20250326) {
		t.Errorf("initialize protocolVersion = %q, want %q (graceful degradation from newest)", result.ProtocolVersion, protocol.V20250326)
	}
}

func TestInlineClient_NegotiatedVersion_Default(t *testing.T) {
	serverID := "test-inline-negotiated-default"
	_ = inline.RegisterTools(serverID, testHandler, nil)

	client, _ := NewInlineClient(&ServerInfo{ID: serverID})
	if client.NegotiatedVersion() != "" {
		t.Errorf("NegotiatedVersion() before Start = %q, want empty", client.NegotiatedVersion())
	}
	_ = client.Start(context.Background())
	if client.NegotiatedVersion() != protocol.V20260728 {
		t.Errorf("NegotiatedVersion() after Start = %q, want newest %q", client.NegotiatedVersion(), protocol.V20260728)
	}
}

func TestInlineClient_NegotiatedVersion_ManualPin(t *testing.T) {
	serverID := "test-inline-negotiated-pinned"
	_ = inline.RegisterTools(serverID, testHandler, nil)

	client, _ := NewInlineClient(&ServerInfo{
		ID:     serverID,
		Config: config.MCPServerConfig{ID: serverID, ProtocolVersion: "2024-11-05"},
	})
	_ = client.Start(context.Background())
	if client.NegotiatedVersion() != protocol.V20241105 {
		t.Errorf("NegotiatedVersion() = %q, want pinned %q", client.NegotiatedVersion(), protocol.V20241105)
	}
}

func TestInlineClientToolsList(t *testing.T) {
	serverID := "test-inline-toolslist"
	tools := []inline.ToolDef{
		{Name: "greet", Description: "Greets the user", InputSchema: map[string]interface{}{"type": "object"}},
	}
	_ = inline.RegisterTools(serverID, testHandler, tools)
	t.Cleanup(func() {})

	client, _ := NewInlineClient(&ServerInfo{ID: serverID})
	_ = client.Start(context.Background())

	id := json.RawMessage(`"2"`)
	resp, err := client.Call(context.Background(), &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      &id,
		Method:  "tools/list",
	})
	if err != nil {
		t.Fatalf("Call(tools/list) failed: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("expected no error, got %+v", resp.Error)
	}
	if resp.Result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestInlineClientToolsCall(t *testing.T) {
	serverID := "test-inline-toolscall"
	_ = inline.RegisterTools(serverID, testHandler, nil)
	t.Cleanup(func() {})

	client, _ := NewInlineClient(&ServerInfo{ID: serverID})
	_ = client.Start(context.Background())

	params, _ := json.Marshal(map[string]interface{}{
		"name": "test_tool",
		"arguments": map[string]interface{}{
			"input": "hello",
		},
	})
	id := json.RawMessage(`"3"`)
	resp, err := client.Call(context.Background(), &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      &id,
		Method:  "tools/call",
		Params:  params,
	})
	if err != nil {
		t.Fatalf("Call(tools/call) failed: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("expected no error, got %+v", resp.Error)
	}
	if resp.Result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestInlineClientPing(t *testing.T) {
	serverID := "test-inline-ping"
	_ = inline.RegisterTools(serverID, testHandler, nil)
	t.Cleanup(func() {})

	client, _ := NewInlineClient(&ServerInfo{ID: serverID})
	_ = client.Start(context.Background())

	id := json.RawMessage(`"4"`)
	resp, err := client.Call(context.Background(), &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      &id,
		Method:  "ping",
	})
	if err != nil {
		t.Fatalf("Call(ping) failed: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("expected no error, got %+v", resp.Error)
	}
}

func TestInlineClientUnknownMethod(t *testing.T) {
	serverID := "test-inline-unknown"
	_ = inline.RegisterTools(serverID, testHandler, nil)
	t.Cleanup(func() {})

	client, _ := NewInlineClient(&ServerInfo{ID: serverID})
	_ = client.Start(context.Background())

	id := json.RawMessage(`"5"`)
	resp, err := client.Call(context.Background(), &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      &id,
		Method:  "bogus_method",
	})
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != ErrorCodeMethodNotFound {
		t.Errorf("expected code %d, got %d", ErrorCodeMethodNotFound, resp.Error.Code)
	}
}

func TestInlineClientInvalidParams(t *testing.T) {
	serverID := "test-inline-invalid"
	_ = inline.RegisterTools(serverID, testHandler, nil)
	t.Cleanup(func() {})

	client, _ := NewInlineClient(&ServerInfo{ID: serverID})
	_ = client.Start(context.Background())

	id := json.RawMessage(`"6"`)
	resp, err := client.Call(context.Background(), &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      &id,
		Method:  "tools/call",
		Params:  json.RawMessage(`invalid json`),
	})
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("expected error for invalid params")
	}
	if resp.Error.Code != ErrorCodeInvalidParams {
		t.Errorf("expected code %d, got %d", ErrorCodeInvalidParams, resp.Error.Code)
	}
}

// testHandler is a simple inline handler used across tests.
func testHandler(_ context.Context, args map[string]interface{}) (interface{}, error) {
	return map[string]interface{}{"echo": args}, nil
}
