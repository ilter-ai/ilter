package mcptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ilter-ai/ilter/internal/config"
	mcp "github.com/ilter-ai/ilter/internal/features/mcp"
)

func testGateway() *mcp.Gateway {
	gw, _ := testGatewayWithRegistry()
	return gw
}

func testGatewayWithRegistry() (*mcp.Gateway, *mcp.Registry) {
	reg, err := mcp.NewRegistryFromCache(nil, nil)
	if err != nil {
		panic(err)
	}
	gw := mcp.NewGateway(
		reg,
		mcp.NewAuthorizer(nil, nil, "deny"),
		nil,
		nil,
		&config.MCPConfig{Endpoint: "/mcp"},
		nil,
	)
	return gw, reg
}

func testID(id string) *json.RawMessage {
	v := json.RawMessage(`"` + id + `"`)
	return &v
}

func TestHandler_POST_Initialize(t *testing.T) {
	h := &GatewayHandler{gateway: testGateway()}

	body := mcp.JSONRPCRequest{
		JSONRPC: mcp.JSONRPCVersion,
		ID:      testID("1"),
		Method:  "initialize",
		Params:  json.RawMessage(`{}`),
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var rpcResp mcp.JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %+v", rpcResp.Error)
	}
	if rpcResp.Result == nil {
		t.Fatal("expected result")
	}
}

func TestHandler_POST_UnknownMethod(t *testing.T) {
	h := &GatewayHandler{gateway: testGateway()}

	body := mcp.JSONRPCRequest{
		JSONRPC: mcp.JSONRPCVersion,
		ID:      testID("2"),
		Method:  "bogus",
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(b))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var rpcResp mcp.JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
}

func TestHandler_POST_EmptyBody(t *testing.T) {
	h := &GatewayHandler{gateway: testGateway()}

	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(nil))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for JSON-RPC error, got %d", resp.StatusCode)
	}
	var rpcResp mcp.JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error for empty body")
	}
}

func TestHandler_POST_InvalidJSON(t *testing.T) {
	h := &GatewayHandler{gateway: testGateway()}

	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader([]byte(`{invalid}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 (JSON-RPC error response), got %d", resp.StatusCode)
	}
	var rpcResp mcp.JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if rpcResp.Error == nil || rpcResp.Error.Code != mcp.ErrorCodeParse {
		t.Fatalf("expected parse error, got %+v", rpcResp.Error)
	}
}

func TestHandler_GET_SSE(t *testing.T) {
	h := &GatewayHandler{gateway: testGateway()}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so ServeHTTP returns after sending headers

	req := httptest.NewRequest("GET", "/mcp", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("expected text/event-stream, got %q", ct)
	}
}

func TestHandler_POST_InitializeThenListTools(t *testing.T) {
	h := &GatewayHandler{gateway: testGateway()}

	initBody := mcp.JSONRPCRequest{
		JSONRPC: mcp.JSONRPCVersion,
		ID:      testID("init"),
		Method:  "initialize",
		Params:  json.RawMessage(`{}`),
	}
	b, _ := json.Marshal(initBody)
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	resp := w.Result()
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("init failed: %d", resp.StatusCode)
	}

	listBody := mcp.JSONRPCRequest{
		JSONRPC: mcp.JSONRPCVersion,
		ID:      testID("list"),
		Method:  "tools/list",
	}
	b, _ = json.Marshal(listBody)
	req2 := httptest.NewRequest("POST", "/mcp", bytes.NewReader(b))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)

	resp2 := w2.Result()
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("list tools failed: %d", resp2.StatusCode)
	}
	var rpcResp mcp.JSONRPCResponse
	if err := json.NewDecoder(resp2.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %+v", rpcResp.Error)
	}
}

// TestHandler_SessionVersionPersistence exercises the SSE-session-mode
// version negotiation added for multi-protocol-version support: an
// initialize call on a given ?sessionId= pins a protocol.Version onto that
// session (GatewayHandler.sessions), and subsequent requests on the same
// sessionId reuse it — a v20241105-pinned session must accept `ping`
// (removed only in 2026-07-28), without needing to renegotiate every call.
func TestHandler_SessionVersionPersistence(t *testing.T) {
	h := &GatewayHandler{gateway: testGateway(), sessions: make(map[string]*gatewaySession)}
	const sessionID = "test-session-1"
	h.sessions[sessionID] = &gatewaySession{ch: make(chan *mcp.JSONRPCResponse, 4)}

	initBody := mcp.JSONRPCRequest{
		JSONRPC: mcp.JSONRPCVersion,
		ID:      testID("init"),
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion":"2024-11-05"}`),
	}
	b, _ := json.Marshal(initBody)
	req := httptest.NewRequest("POST", "/mcp?sessionId="+sessionID, bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Result().StatusCode != http.StatusAccepted {
		t.Fatalf("init (session mode) status = %d, want 202 Accepted", w.Result().StatusCode)
	}
	<-h.sessions[sessionID].ch // drain the SSE-pushed response

	if h.sessions[sessionID].version != "2024-11-05" {
		t.Fatalf("session.version = %q, want 2024-11-05", h.sessions[sessionID].version)
	}

	pingBody := mcp.JSONRPCRequest{
		JSONRPC: mcp.JSONRPCVersion,
		ID:      testID("ping"),
		Method:  "ping",
	}
	b, _ = json.Marshal(pingBody)
	req2 := httptest.NewRequest("POST", "/mcp?sessionId="+sessionID, bytes.NewReader(b))
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req2)
	if w2.Result().StatusCode != http.StatusAccepted {
		t.Fatalf("ping (session mode) status = %d, want 202 Accepted", w2.Result().StatusCode)
	}
	pingResp := <-h.sessions[sessionID].ch
	if pingResp.Error != nil {
		t.Fatalf("ping on 2024-11-05-pinned session should succeed, got error: %+v", pingResp.Error)
	}
}

func TestHandler_POST_ServerDiscover(t *testing.T) {
	h := &GatewayHandler{gateway: testGateway()}

	body := mcp.JSONRPCRequest{
		JSONRPC: mcp.JSONRPCVersion,
		ID:      testID("1"),
		Method:  "server/discover",
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(b))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var rpcResp mcp.JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %+v", rpcResp.Error)
	}
	var result struct {
		ProtocolVersions []string `json:"protocolVersions"`
	}
	if err := json.Unmarshal(rpcResp.Result, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(result.ProtocolVersions) != 3 {
		t.Errorf("ProtocolVersions = %v, want 3 entries", result.ProtocolVersions)
	}
}

// TestHandler_2026RequiresTransportHeaders exercises the 2026-07-28
// spec's Mcp-Method/Mcp-Name header requirement: a request whose `_meta`
// pins 2026-07-28 must carry both headers, and Mcp-Method must match the
// JSON-RPC method — none of this applies to the two older versions, which
// never defined the requirement.
// TestHandler_SubscriptionsListen exercises the full HTTP-level
// subscriptions/listen flow: POST opens a long-lived streaming response;
// the first line is a JSON-RPC ack carrying the subscription id; a
// registry change (SyncTools) then triggers a real toolsListChanged
// notification, delivered as a second newline-delimited JSON line tagged
// with that same subscription id.
func TestHandler_SubscriptionsListen(t *testing.T) {
	gw, reg := testGatewayWithRegistry()
	h := &GatewayHandler{gateway: gw}

	body := mcp.JSONRPCRequest{
		JSONRPC: mcp.JSONRPCVersion,
		ID:      testID("1"),
		Method:  "subscriptions/listen",
		Params:  json.RawMessage(`{"types":["toolsListChanged"]}`),
	}
	b, _ := json.Marshal(body)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(b)).WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		h.ServeHTTP(w, req)
		close(done)
	}()

	// Give the handler a moment to write the ack line, then trigger a
	// real registry change (RegisterServer fires Registry.OnToolsChanged,
	// which Gateway wired to broker.Publish in NewGateway) that should
	// produce a real notification on the open stream.
	time.Sleep(100 * time.Millisecond)
	reg.RegisterServer("new-server", config.MCPServerConfig{ID: "new-server"}, nil)

	cancel()
	<-done

	lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
	if len(lines) < 1 {
		t.Fatal("expected at least the ack line in the response body")
	}

	var ack mcp.JSONRPCResponse
	if err := json.Unmarshal([]byte(lines[0]), &ack); err != nil {
		t.Fatalf("unmarshal ack line: %v", err)
	}
	if ack.Error != nil {
		t.Fatalf("unexpected error in ack: %+v", ack.Error)
	}
	var ackResult struct {
		SubscriptionID string `json:"subscriptionId"`
	}
	if err := json.Unmarshal(ack.Result, &ackResult); err != nil {
		t.Fatalf("unmarshal ack result: %v", err)
	}
	if ackResult.SubscriptionID == "" {
		t.Fatal("expected non-empty subscriptionId in ack")
	}

	if len(lines) < 2 {
		t.Fatal("expected a toolsListChanged notification after the registry change, got only the ack line")
	}
	var notif struct {
		Method string `json:"method"`
		Meta   struct {
			SubscriptionID string `json:"io.modelcontextprotocol/subscriptionId"`
		} `json:"_meta"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &notif); err != nil {
		t.Fatalf("unmarshal notification line: %v", err)
	}
	if notif.Method != "notifications/toolsListChanged" {
		t.Errorf("notification method = %q, want %q", notif.Method, "notifications/toolsListChanged")
	}
	if notif.Meta.SubscriptionID != ackResult.SubscriptionID {
		t.Errorf("notification subscriptionId = %q, want %q (matching the ack)", notif.Meta.SubscriptionID, ackResult.SubscriptionID)
	}
}

func TestHandler_2026RequiresTransportHeaders(t *testing.T) {
	newReq := func(headers map[string]string) *http.Request {
		body := mcp.JSONRPCRequest{
			JSONRPC: mcp.JSONRPCVersion,
			ID:      testID("1"),
			Method:  "tools/list",
			Params:  json.RawMessage(`{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}`),
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(b))
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		return req
	}

	t.Run("missing both headers is rejected", func(t *testing.T) {
		h := &GatewayHandler{gateway: testGateway()}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, newReq(nil))

		var rpcResp mcp.JSONRPCResponse
		if err := json.NewDecoder(w.Result().Body).Decode(&rpcResp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if rpcResp.Error == nil {
			t.Fatal("expected HeaderMismatch error for missing Mcp-Method/Mcp-Name, got success")
		}
	})

	t.Run("mismatched Mcp-Method is rejected", func(t *testing.T) {
		h := &GatewayHandler{gateway: testGateway()}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, newReq(map[string]string{"Mcp-Method": "tools/call", "Mcp-Name": "x"}))

		var rpcResp mcp.JSONRPCResponse
		if err := json.NewDecoder(w.Result().Body).Decode(&rpcResp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if rpcResp.Error == nil {
			t.Fatal("expected HeaderMismatch error for Mcp-Method not matching the request method, got success")
		}
	})

	t.Run("correct headers are accepted", func(t *testing.T) {
		h := &GatewayHandler{gateway: testGateway()}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, newReq(map[string]string{"Mcp-Method": "tools/list", "Mcp-Name": "ilter"}))

		var rpcResp mcp.JSONRPCResponse
		if err := json.NewDecoder(w.Result().Body).Decode(&rpcResp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if rpcResp.Error != nil {
			t.Fatalf("unexpected error with correct headers: %+v", rpcResp.Error)
		}
	})

	t.Run("2025-03-26 request needs no headers at all", func(t *testing.T) {
		h := &GatewayHandler{gateway: testGateway()}
		body := mcp.JSONRPCRequest{
			JSONRPC: mcp.JSONRPCVersion,
			ID:      testID("1"),
			Method:  "tools/list",
		}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(b))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		var rpcResp mcp.JSONRPCResponse
		if err := json.NewDecoder(w.Result().Body).Decode(&rpcResp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if rpcResp.Error != nil {
			t.Fatalf("unexpected error for a version that never required these headers: %+v", rpcResp.Error)
		}
	})
}
