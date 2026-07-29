package v20241105

import (
	"encoding/json"
	"testing"

	"github.com/ilter-ai/ilter/internal/features/mcp/protocol"
)

// TestBuildClientHandshake_MatchesHardcodedClientStdio pins the exact byte
// output that internal/features/mcp/client_stdio.go:122 hardcoded before
// this refactor:
//
//	json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"ilter","version":"0.1.0"}}`)
//
// This is the regression safety net for the "pure extraction, zero
// behavior change" requirement — if this test ever needs to change, that
// is a real, deliberate protocol behavior change, not a refactor.
func TestBuildClientHandshake_MatchesHardcodedClientStdio(t *testing.T) {
	v := New()
	method, params, needsInit := v.BuildClientHandshake(protocol.ImplementationInfo{Name: "ilter", Version: "0.1.0"})

	if method != "initialize" {
		t.Errorf("method = %q, want %q", method, "initialize")
	}
	if !needsInit {
		t.Error("needsInitialize = false, want true (2024-11-05 uses the initialize handshake)")
	}

	want := `{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"ilter","version":"0.1.0"}}`
	if string(params) != want {
		t.Errorf("params = %s, want %s", params, want)
	}
}

func TestHandleInitialize(t *testing.T) {
	v := New()
	serverInfo := protocol.ImplementationInfo{Name: "ilter-mcp-gateway", Version: "1.0.0"}
	resultJSON, err := v.HandleInitialize(json.RawMessage(`{}`), serverInfo)
	if err != nil {
		t.Fatalf("HandleInitialize error: %v", err)
	}

	var result initializeResult
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.ProtocolVersion != "2024-11-05" {
		t.Errorf("ProtocolVersion = %q, want 2024-11-05", result.ProtocolVersion)
	}
	if result.Capabilities.Tools == nil || result.Capabilities.Tools.ListChanged {
		t.Errorf("Capabilities.Tools = %+v, want non-nil with ListChanged=false", result.Capabilities.Tools)
	}
	if result.ServerInfo != serverInfo {
		t.Errorf("ServerInfo = %+v, want %+v", result.ServerInfo, serverInfo)
	}
}

func TestHandleInitialize_InvalidParams(t *testing.T) {
	v := New()
	_, err := v.HandleInitialize(json.RawMessage(`not json`), protocol.ImplementationInfo{})
	if err == nil {
		t.Error("expected error for malformed params, got nil")
	}
}

func TestIsMethodSupported(t *testing.T) {
	v := New()
	supported := []string{"initialize", "notifications/initialized", "tools/list", "tools/call", "ping"}
	for _, m := range supported {
		if !v.IsMethodSupported(m) {
			t.Errorf("IsMethodSupported(%q) = false, want true", m)
		}
	}
	unsupported := []string{"server/discover", "tasks/get", "resources/list", "logging/setLevel"}
	for _, m := range unsupported {
		if v.IsMethodSupported(m) {
			t.Errorf("IsMethodSupported(%q) = true, want false (not part of 2024-11-05)", m)
		}
	}
}

func TestErrorCode_MatchesLegacyConstants(t *testing.T) {
	v := New()
	cases := map[protocol.ErrorKind]int{
		protocol.ErrToolNotFound:   -32000,
		protocol.ErrToolExecution:  -32001,
		protocol.ErrNotInitialized: -32002,
		protocol.ErrMethodNotFound: -32601,
	}
	for kind, want := range cases {
		if got := v.ErrorCode(kind); got != want {
			t.Errorf("ErrorCode(%v) = %d, want %d", kind, got, want)
		}
	}
}

func TestTransport(t *testing.T) {
	tr := New().Transport()
	if !tr.StatefulSessions {
		t.Error("StatefulSessions = false, want true")
	}
	if !tr.SupportsLegacySSEGet {
		t.Error("SupportsLegacySSEGet = false, want true")
	}
	if tr.RequiresMcpMethodHeader || tr.RequiresMcpNameHeader || tr.SupportsSubscriptionsListen {
		t.Error("2024-11-05 must not require 2026-07-28-only transport features")
	}
}

func TestParseServerHandshake(t *testing.T) {
	v := New()
	if err := v.ParseServerHandshake(json.RawMessage(`{"protocolVersion":"2024-11-05"}`)); err != nil {
		t.Errorf("expected accept for matching version, got %v", err)
	}
	if err := v.ParseServerHandshake(json.RawMessage(`{"protocolVersion":"2025-03-26"}`)); err == nil {
		t.Error("expected rejection for mismatched version, got nil")
	}
}

func TestOAuthPolicy(t *testing.T) {
	p := New().OAuthPolicy()
	if p.SupportsCIMD() || p.RequiresIssuerValidation() || p.RequiresApplicationType() {
		t.Error("2024-11-05 predates OAuth entirely; policy must not claim any 2026-07-28-only behavior")
	}
}
