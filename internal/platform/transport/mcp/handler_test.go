package mcptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ilter-ai/ilter/internal/config"
	mcp "github.com/ilter-ai/ilter/internal/features/mcp"
)

func testGateway() *mcp.Gateway {
	reg, err := mcp.NewRegistryFromCache(nil, nil)
	if err != nil {
		panic(err)
	}
	return mcp.NewGateway(
		reg,
		mcp.NewAuthorizer(nil, nil, "deny"),
		nil,
		nil,
		&config.MCPConfig{Endpoint: "/mcp"},
		nil,
	)
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
