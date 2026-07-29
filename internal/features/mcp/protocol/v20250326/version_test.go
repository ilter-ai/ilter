package v20250326

import (
	"encoding/json"
	"testing"

	"github.com/ilter-ai/ilter/internal/features/mcp/protocol"
)

// TestHandleInitialize_MatchesGatewayGo pins the exact InitializeResult
// shape internal/features/mcp/gateway.go's handleInitialize produced
// before this refactor: ProtocolVersion echoed as "2025-03-26"
// unconditionally, Capabilities{Tools:{ListChanged:false}} only, ServerInfo
// passed through as-is. A mismatched/unrecognized client protocolVersion in
// the request is NOT an error — gateway.go only logged a warning and
// proceeded; this method's return value must be identical either way.
func TestHandleInitialize_MatchesGatewayGo(t *testing.T) {
	v := New()
	serverInfo := protocol.ImplementationInfo{Name: "ilter-mcp-gateway", Version: "1.0.0"}

	for _, requestedVersion := range []string{"", "2025-03-26", "2099-01-01", "garbage"} {
		paramsJSON, _ := json.Marshal(initializeParams{ProtocolVersion: requestedVersion})
		resultJSON, err := v.HandleInitialize(paramsJSON, serverInfo)
		if err != nil {
			t.Fatalf("HandleInitialize(requested=%q) error: %v", requestedVersion, err)
		}

		var result initializeResult
		if err := json.Unmarshal(resultJSON, &result); err != nil {
			t.Fatalf("unmarshal result: %v", err)
		}
		if result.ProtocolVersion != "2025-03-26" {
			t.Errorf("requested=%q: ProtocolVersion = %q, want 2025-03-26 (server always responds with its own version)", requestedVersion, result.ProtocolVersion)
		}
		if result.Capabilities.Tools == nil || result.Capabilities.Tools.ListChanged {
			t.Errorf("requested=%q: Capabilities.Tools = %+v, want non-nil with ListChanged=false", requestedVersion, result.Capabilities.Tools)
		}
		if result.Capabilities.Resources != nil || result.Capabilities.Prompts != nil || result.Capabilities.Logging != nil {
			t.Errorf("requested=%q: only Tools capability should be set, matching gateway.go today", requestedVersion)
		}
		if result.ServerInfo != serverInfo {
			t.Errorf("requested=%q: ServerInfo = %+v, want %+v", requestedVersion, result.ServerInfo, serverInfo)
		}
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
	for _, m := range []string{"initialize", "notifications/initialized", "tools/list", "tools/call", "ping"} {
		if !v.IsMethodSupported(m) {
			t.Errorf("IsMethodSupported(%q) = false, want true", m)
		}
	}
	for _, m := range []string{"server/discover", "tasks/get", "subscriptions/listen"} {
		if v.IsMethodSupported(m) {
			t.Errorf("IsMethodSupported(%q) = true, want false (2026-07-28-only)", m)
		}
	}
}

func TestWrapToolsListResult_PlainShape(t *testing.T) {
	v := New()
	out, err := v.WrapToolsListResult(json.RawMessage(`[{"name":"x"}]`), "cursor123")
	if err != nil {
		t.Fatalf("WrapToolsListResult error: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := decoded["ttlMs"]; ok {
		t.Error("2025-03-26 must not include ttlMs (CacheableResult is 2026-07-28-only)")
	}
	if _, ok := decoded["cacheScope"]; ok {
		t.Error("2025-03-26 must not include cacheScope (CacheableResult is 2026-07-28-only)")
	}
	if string(decoded["nextCursor"]) != `"cursor123"` {
		t.Errorf("nextCursor = %s, want %q", decoded["nextCursor"], "cursor123")
	}
}

func TestWrapCallToolResult_NoResultType(t *testing.T) {
	v := New()
	out, err := v.WrapCallToolResult(json.RawMessage(`[{"type":"text","text":"hi"}]`), false)
	if err != nil {
		t.Fatalf("WrapCallToolResult error: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := decoded["resultType"]; ok {
		t.Error("2025-03-26 must not include resultType (MRTR is 2026-07-28-only)")
	}
}

func TestErrorCode_MatchesTodaysConstants(t *testing.T) {
	v := New()
	cases := map[protocol.ErrorKind]int{
		protocol.ErrToolNotFound:               -32000,
		protocol.ErrToolExecution:              -32001,
		protocol.ErrNotInitialized:             -32002,
		protocol.ErrUnsupportedProtocolVersion: -32602,
		protocol.ErrMethodNotFound:             -32601,
	}
	for kind, want := range cases {
		if got := v.ErrorCode(kind); got != want {
			t.Errorf("ErrorCode(%v) = %d, want %d", kind, got, want)
		}
	}
}

func TestTransport(t *testing.T) {
	tr := New().Transport()
	if !tr.StatefulSessions || !tr.SupportsLegacySSEGet {
		t.Error("2025-03-26 must be stateful with legacy SSE-GET support")
	}
	if tr.RequiresMcpMethodHeader || tr.RequiresMcpNameHeader || tr.SupportsSubscriptionsListen {
		t.Error("2025-03-26 must not require 2026-07-28-only transport features")
	}
}

func TestOAuthPolicy_TodaysDCROnlyBehavior(t *testing.T) {
	p := New().OAuthPolicy()
	if p.SupportsCIMD() {
		t.Error("today's oauth_endpoints.go has no CIMD support — must stay false for this version")
	}
	if p.RequiresIssuerValidation() {
		t.Error("today's oauth_endpoints.go does not validate iss — must stay false for this version")
	}
	if p.RequiresApplicationType() {
		t.Error("today's oauth_endpoints.go does not require application_type — must stay false for this version")
	}
}
