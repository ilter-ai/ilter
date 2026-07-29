package v20260728

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/ilter-ai/ilter/internal/features/mcp/protocol"
)

func TestHandleInitialize_NotSupported(t *testing.T) {
	v := New()
	_, err := v.HandleInitialize(json.RawMessage(`{}`), protocol.ImplementationInfo{})
	if !errors.Is(err, protocol.ErrNoInitializeHandshake) {
		t.Errorf("HandleInitialize error = %v, want ErrNoInitializeHandshake", err)
	}
}

func TestValidateRequestMeta(t *testing.T) {
	v := New()

	if err := v.ValidateRequestMeta(nil); err != nil {
		t.Errorf("empty _meta should be accepted (absence is meaningful, not malformed), got %v", err)
	}

	ok := json.RawMessage(`{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}`)
	if err := v.ValidateRequestMeta(ok); err != nil {
		t.Errorf("matching protocolVersion should be accepted, got %v", err)
	}

	mismatched := json.RawMessage(`{"io.modelcontextprotocol/protocolVersion":"2025-03-26"}`)
	if err := v.ValidateRequestMeta(mismatched); err == nil {
		t.Error("mismatched protocolVersion should be rejected, got nil")
	}

	if err := v.ValidateRequestMeta(json.RawMessage(`not json`)); err == nil {
		t.Error("malformed _meta should error, got nil")
	}
}

func TestIsMethodSupported(t *testing.T) {
	v := New()
	supported := []string{"tools/list", "tools/call", "server/discover", "tasks/get", "tasks/update", "subscriptions/listen"}
	for _, m := range supported {
		if !v.IsMethodSupported(m) {
			t.Errorf("IsMethodSupported(%q) = false, want true", m)
		}
	}
	// Explicitly removed by the 2026-07-28 spec.
	removed := []string{"ping", "logging/setLevel", "notifications/roots/list_changed", "initialize", "notifications/initialized"}
	for _, m := range removed {
		if v.IsMethodSupported(m) {
			t.Errorf("IsMethodSupported(%q) = true, want false (removed by 2026-07-28)", m)
		}
	}
}

func TestWrapToolsListResult_CacheableResult(t *testing.T) {
	v := New()
	out, err := v.WrapToolsListResult(json.RawMessage(`[{"name":"x"}]`), "cursor1")
	if err != nil {
		t.Fatalf("WrapToolsListResult error: %v", err)
	}
	var decoded cacheableToolsListResult
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.TTLMs <= 0 {
		t.Errorf("TTLMs = %d, want > 0", decoded.TTLMs)
	}
	if decoded.CacheScope != "private" {
		t.Errorf("CacheScope = %q, want %q (tools/list is authorization-filtered per key)", decoded.CacheScope, "private")
	}
	if decoded.NextCursor != "cursor1" {
		t.Errorf("NextCursor = %q, want %q", decoded.NextCursor, "cursor1")
	}
}

func TestWrapCallToolResult_ResultTypeComplete(t *testing.T) {
	v := New()
	out, err := v.WrapCallToolResult(json.RawMessage(`[{"type":"text","text":"hi"}]`), false)
	if err != nil {
		t.Fatalf("WrapCallToolResult error: %v", err)
	}
	var decoded struct {
		ResultType string `json:"resultType"`
	}
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.ResultType != "complete" {
		t.Errorf("resultType = %q, want %q", decoded.ResultType, "complete")
	}
}

func TestErrorCode_RenumberedRange(t *testing.T) {
	v := New()
	cases := map[protocol.ErrorKind]int{
		protocol.ErrToolNotFound:                    -32000, // grandfathered, unchanged
		protocol.ErrToolExecution:                   -32001, // grandfathered, unchanged
		protocol.ErrHeaderMismatch:                  -32020,
		protocol.ErrMissingRequiredClientCapability: -32021,
		protocol.ErrUnsupportedProtocolVersion:      -32022,
		protocol.ErrResourceNotFound:                -32602,
		protocol.ErrMethodNotFound:                  -32601,
	}
	for kind, want := range cases {
		if got := v.ErrorCode(kind); got != want {
			t.Errorf("ErrorCode(%v) = %d, want %d", kind, got, want)
		}
	}
}

func TestTransport_StatelessStreamable(t *testing.T) {
	tr := New().Transport()
	if tr.StatefulSessions {
		t.Error("StatefulSessions = true, want false (2026-07-28 is stateless)")
	}
	if !tr.RequiresMcpMethodHeader || !tr.RequiresMcpNameHeader {
		t.Error("2026-07-28 must require Mcp-Method/Mcp-Name headers")
	}
	if tr.SupportsLegacySSEGet {
		t.Error("SupportsLegacySSEGet = true, want false (replaced by subscriptions/listen)")
	}
	if !tr.SupportsSubscriptionsListen {
		t.Error("SupportsSubscriptionsListen = false, want true")
	}
}

func TestBuildClientHandshake_ServerDiscover(t *testing.T) {
	v := New()
	method, params, needsInit := v.BuildClientHandshake(protocol.ImplementationInfo{Name: "ilter", Version: "0.1.0"})
	if method != protocol.MethodServerDiscover {
		t.Errorf("method = %q, want %q", method, protocol.MethodServerDiscover)
	}
	if needsInit {
		t.Error("needsInitialize = true, want false (stateless)")
	}
	if len(params) == 0 {
		t.Error("expected non-empty params carrying clientInfo")
	}
}

func TestParseServerHandshake(t *testing.T) {
	v := New()

	accepted := json.RawMessage(`{"protocolVersions":["2026-07-28","2025-03-26"],"serverInfo":{"name":"x","version":"1"}}`)
	if err := v.ParseServerHandshake(accepted); err != nil {
		t.Errorf("expected accept when 2026-07-28 is listed, got %v", err)
	}

	rejected := json.RawMessage(`{"protocolVersions":["2025-03-26","2024-11-05"]}`)
	if err := v.ParseServerHandshake(rejected); !errors.Is(err, protocol.ErrHandshakeRejected) {
		t.Errorf("expected ErrHandshakeRejected when 2026-07-28 is absent, got %v", err)
	}
}

func TestOAuthPolicy_2026Additions(t *testing.T) {
	p := New().OAuthPolicy()
	if !p.SupportsCIMD() {
		t.Error("SupportsCIMD() = false, want true")
	}
	if !p.RequiresIssuerValidation() {
		t.Error("RequiresIssuerValidation() = false, want true")
	}
	if !p.RequiresApplicationType() {
		t.Error("RequiresApplicationType() = false, want true")
	}
}
