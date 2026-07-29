package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/mcp/protocol"
)

// fakeDownstreamServer simulates a downstream MCP server's handshake
// behavior for negotiateOutbound tests: it accepts requests for methods in
// acceptMethods and rejects (JSON-RPC MethodNotFound) everything else,
// letting tests simulate a server that only understands an older
// version's handshake method.
type fakeDownstreamServer struct {
	acceptMethods map[string]bool
	notified      []string
}

func (f *fakeDownstreamServer) call(_ context.Context, method string, params json.RawMessage) (*JSONRPCResponse, error) {
	id := json.RawMessage(`"1"`)
	if !f.acceptMethods[method] {
		return NewErrorResponse(&id, ErrorCodeMethodNotFound, "Method not found: "+method), nil
	}
	switch method {
	case "server/discover":
		result, _ := protocol.MarshalDiscoverResult(protocol.ImplementationInfo{Name: "fake", Version: "1"})
		return NewSuccessResponse(&id, result), nil
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(params, &p)
		return NewSuccessResponse(&id, map[string]any{"protocolVersion": p.ProtocolVersion}), nil
	}
	return NewSuccessResponse(&id, json.RawMessage(`{}`)), nil
}

func (f *fakeDownstreamServer) notify(method string, _ json.RawMessage) {
	f.notified = append(f.notified, method)
}

func testServerInfo(protocolVersion string) *ServerInfo {
	return &ServerInfo{
		ID:     "fake-server",
		Config: config.MCPServerConfig{ID: "fake-server", ProtocolVersion: protocolVersion},
	}
}

func TestNegotiateOutbound_TriesNewestFirst(t *testing.T) {
	f := &fakeDownstreamServer{acceptMethods: map[string]bool{"server/discover": true}}
	v, err := negotiateOutbound(context.Background(), testServerInfo("auto"), f.call, f.notify)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.ID() != protocol.V20260728 {
		t.Errorf("negotiated %q, want newest %q (server accepts server/discover)", v.ID(), protocol.V20260728)
	}
}

func TestNegotiateOutbound_FallsBackWhenServerOnlyKnowsOlderVersion(t *testing.T) {
	// Server rejects server/discover (doesn't understand it) but accepts
	// the legacy initialize handshake — simulates a real 2025-03-26-only
	// downstream MCP server.
	f := &fakeDownstreamServer{acceptMethods: map[string]bool{"initialize": true}}
	v, err := negotiateOutbound(context.Background(), testServerInfo("auto"), f.call, f.notify)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.ID() != protocol.V20250326 {
		t.Errorf("negotiated %q, want %q (newest version whose handshake the server accepts)", v.ID(), protocol.V20250326)
	}
	if len(f.notified) != 1 || f.notified[0] != MethodNotificationsInitialized {
		t.Errorf("expected notifications/initialized to be sent after a successful initialize handshake, got %v", f.notified)
	}
}

func TestNegotiateOutbound_ManualPin(t *testing.T) {
	f := &fakeDownstreamServer{acceptMethods: map[string]bool{"server/discover": true, "initialize": true}}
	v, err := negotiateOutbound(context.Background(), testServerInfo("2024-11-05"), f.call, f.notify)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.ID() != protocol.V20241105 {
		t.Errorf("negotiated %q, want pinned %q even though server would accept newer", v.ID(), protocol.V20241105)
	}
}

func TestNegotiateOutbound_ManualPinRejectedByServer(t *testing.T) {
	f := &fakeDownstreamServer{acceptMethods: map[string]bool{"server/discover": true}}
	_, err := negotiateOutbound(context.Background(), testServerInfo("2024-11-05"), f.call, f.notify)
	if err == nil {
		t.Fatal("expected error: server doesn't accept the pinned version's handshake method")
	}
}

func TestNegotiateOutbound_NoVersionAccepted(t *testing.T) {
	f := &fakeDownstreamServer{acceptMethods: map[string]bool{}}
	_, err := negotiateOutbound(context.Background(), testServerInfo("auto"), f.call, f.notify)
	if err == nil {
		t.Fatal("expected error when no version's handshake is accepted")
	}
}
