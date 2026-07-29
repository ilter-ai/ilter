package mcp

import (
	"encoding/json"
	"testing"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/mcp/protocol"
)

func newTestHub(servers map[string]*ServerInfo) (*Hub, *SessionManager) {
	reg := &Registry{servers: servers}
	if reg.servers == nil {
		reg.servers = make(map[string]*ServerInfo)
	}
	auth := NewAuthorizer(nil, nil, "deny")
	hub := NewHub(reg, auth, nil, nil, &config.MCPConfig{})
	return hub, NewSessionManager()
}

func TestHub_Dispatch_ServerDiscover_NoSessionRequired(t *testing.T) {
	hub, sm := newTestHub(nil)
	session := sm.Create("", "")
	defer sm.Delete(session.ID)

	resp := hub.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      testID("1"),
		Method:  protocol.MethodServerDiscover,
	}, session)
	if resp == nil || resp.Error != nil {
		t.Fatalf("unexpected response/error: %+v", resp)
	}

	var result protocol.DiscoverResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result.ProtocolVersions) != 3 || result.ProtocolVersions[0] != protocol.V20260728 {
		t.Errorf("ProtocolVersions = %v, want newest-first 3 versions", result.ProtocolVersions)
	}
	// server/discover must not pin/require a session version.
	if session.ProtocolVersion != "" {
		t.Errorf("session.ProtocolVersion = %q after server/discover, want unchanged (empty)", session.ProtocolVersion)
	}
}

// TestHub_Dispatch_Initialize_PinsEachVersion covers the two versions that
// actually define the `initialize` handshake (2024-11-05, 2025-03-26): a
// client requesting one of these gets pinned to exactly that version.
func TestHub_Dispatch_Initialize_PinsEachVersion(t *testing.T) {
	for _, id := range []protocol.ID{protocol.V20241105, protocol.V20250326} {
		hub, sm := newTestHub(nil)
		session := sm.Create("", "")

		params, _ := json.Marshal(InitializeParams{ProtocolVersion: string(id)})
		resp := hub.Dispatch(&JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			ID:      testID("1"),
			Method:  MethodInitialize,
			Params:  params,
		}, session)
		if resp == nil || resp.Error != nil {
			t.Fatalf("version %q: unexpected response/error: %+v", id, resp)
		}
		if session.ProtocolVersion != id {
			t.Errorf("version %q: session.ProtocolVersion = %q, want %q", id, session.ProtocolVersion, id)
		}

		var result struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			t.Fatalf("version %q: unmarshal: %v", id, err)
		}
		if result.ProtocolVersion != string(id) {
			t.Errorf("version %q: result.protocolVersion = %q, want %q", id, result.ProtocolVersion, id)
		}
		sm.Delete(session.ID)
	}
}

// TestHub_Dispatch_Initialize_2026RequestGracefullyDegrades covers the
// contradictory case of a client explicitly requesting "2026-07-28" via
// the legacy `initialize` method — that version removed the handshake
// entirely, so a real 2026-07-28-native client would never call this
// method at all. Rather than hard-erroring a client that's merely
// confused or a version behind, ilter degrades to the newest version that
// DOES define `initialize` (2025-03-26).
func TestHub_Dispatch_Initialize_2026RequestGracefullyDegrades(t *testing.T) {
	hub, sm := newTestHub(nil)
	session := sm.Create("", "")
	defer sm.Delete(session.ID)

	params, _ := json.Marshal(InitializeParams{ProtocolVersion: string(protocol.V20260728)})
	resp := hub.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      testID("1"),
		Method:  MethodInitialize,
		Params:  params,
	}, session)
	if resp == nil || resp.Error != nil {
		t.Fatalf("unexpected response/error: %+v", resp)
	}
	if session.ProtocolVersion != protocol.V20250326 {
		t.Errorf("session.ProtocolVersion = %q, want %q (newest version that still defines initialize)", session.ProtocolVersion, protocol.V20250326)
	}
}

func TestHub_Dispatch_Initialize_UnknownVersionFallsBackToNewestThatSupportsIt(t *testing.T) {
	hub, sm := newTestHub(nil)
	session := sm.Create("", "")
	defer sm.Delete(session.ID)

	params, _ := json.Marshal(InitializeParams{ProtocolVersion: "2099-01-01"})
	resp := hub.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      testID("1"),
		Method:  MethodInitialize,
		Params:  params,
	}, session)
	if resp == nil || resp.Error != nil {
		t.Fatalf("unexpected response/error: %+v", resp)
	}
	// protocol.Negotiate("2099-01-01") falls back to protocol.Newest()
	// (2026-07-28), which doesn't support initialize, so handleInitialize
	// must degrade one more step to 2025-03-26.
	if session.ProtocolVersion != protocol.V20250326 {
		t.Errorf("session.ProtocolVersion = %q, want %q", session.ProtocolVersion, protocol.V20250326)
	}
}

func TestHub_Dispatch_ToolsCall_NotInitialized(t *testing.T) {
	hub, sm := newTestHub(nil)
	session := sm.Create("", "")
	defer sm.Delete(session.ID)

	params, _ := json.Marshal(CallToolParams{Name: "srv__tool"})
	resp := hub.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      testID("1"),
		Method:  MethodToolsCall,
		Params:  params,
	}, session)
	if resp == nil || resp.Error == nil {
		t.Fatalf("expected NotInitialized error, got %+v", resp)
	}
	if resp.Error.Code != ErrorCodeNotInitialized {
		t.Errorf("error code = %d, want %d (ErrorCodeNotInitialized, session never called initialize)", resp.Error.Code, ErrorCodeNotInitialized)
	}
}

func TestHub_Dispatch_ToolsList_CacheableResultOnlyFor2026(t *testing.T) {
	for _, tc := range []struct {
		version    protocol.ID
		wantTTLKey bool
	}{
		{protocol.V20241105, false},
		{protocol.V20250326, false},
		{protocol.V20260728, true},
	} {
		hub, sm := newTestHub(nil)
		session := sm.Create("", "")
		session.ProtocolVersion = tc.version

		resp := hub.Dispatch(&JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			ID:      testID("1"),
			Method:  MethodToolsList,
		}, session)
		if resp == nil || resp.Error != nil {
			t.Fatalf("version %q: unexpected response/error: %+v", tc.version, resp)
		}

		var decoded map[string]json.RawMessage
		if err := json.Unmarshal(resp.Result, &decoded); err != nil {
			t.Fatalf("version %q: unmarshal: %v", tc.version, err)
		}
		_, hasTTL := decoded["ttlMs"]
		if hasTTL != tc.wantTTLKey {
			t.Errorf("version %q: ttlMs present = %v, want %v", tc.version, hasTTL, tc.wantTTLKey)
		}
		sm.Delete(session.ID)
	}
}

func TestHub_Dispatch_MethodNotSupportedForVersion(t *testing.T) {
	hub, sm := newTestHub(nil)
	session := sm.Create("", "")
	session.ProtocolVersion = protocol.V20260728 // ping was removed in 2026-07-28
	defer sm.Delete(session.ID)

	resp := hub.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      testID("1"),
		Method:  MethodPing,
	}, session)
	if resp == nil || resp.Error == nil {
		t.Fatalf("expected MethodNotFound-style error for ping on a 2026-07-28 session, got %+v", resp)
	}
}

func TestHub_Dispatch_MethodSupportedForOlderVersions(t *testing.T) {
	for _, id := range []protocol.ID{protocol.V20241105, protocol.V20250326} {
		hub, sm := newTestHub(nil)
		session := sm.Create("", "")
		session.ProtocolVersion = id

		resp := hub.Dispatch(&JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			ID:      testID("1"),
			Method:  MethodPing,
		}, session)
		if resp == nil || resp.Error != nil {
			t.Errorf("version %q: ping should be supported, got %+v", id, resp)
		}
		sm.Delete(session.ID)
	}
}
