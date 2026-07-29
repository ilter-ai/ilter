package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/mcp/protocol"
)

// newFakeSSEServer starts a real HTTP server implementing just enough of
// the legacy SSE MCP transport to exercise SSEClient end-to-end: GET opens
// an SSE stream and emits an "endpoint" event; POST dispatches a JSON-RPC
// request through handler, and the result is pushed back as a "message"
// event on the GET stream (mirroring how a real MCP SSE server responds —
// the POST itself only ever returns 202 Accepted).
func newFakeSSEServer(t *testing.T, handler func(method string, params json.RawMessage) (json.RawMessage, *RPCError)) *httptest.Server {
	t.Helper()
	respCh := make(chan []byte, 8)

	mux := http.NewServeMux()
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseRecorder-less httptest server must support flushing")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "event: endpoint\ndata: http://%s/sse/post\n\n", r.Host)
		flusher.Flush()
		for {
			select {
			case <-r.Context().Done():
				return
			case b := <-respCh:
				fmt.Fprintf(w, "event: message\ndata: %s\n\n", b)
				flusher.Flush()
			}
		}
	})
	mux.HandleFunc("/sse/post", func(w http.ResponseWriter, r *http.Request) {
		var req JSONRPCRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		result, rpcErr := handler(req.Method, req.Params)
		resp := &JSONRPCResponse{JSONRPC: JSONRPCVersion, ID: req.ID, Result: result, Error: rpcErr}
		b, _ := json.Marshal(resp)
		respCh <- b
		w.WriteHeader(http.StatusAccepted)
	})
	return httptest.NewServer(mux)
}

// runStartWithTimeout guards against the exact self-deadlock bug that was
// fixed in SSEClient.Start() (it held a write lock for its whole body via
// defer while also calling RLock/Lock again from within that same call
// stack) — if that bug were ever reintroduced, this test would hang
// forever instead of failing cleanly, so it runs Start() on a goroutine
// and fails with a clear message on timeout rather than blocking the test
// suite.
func runStartWithTimeout(t *testing.T, c *SSEClient, timeout time.Duration) error {
	t.Helper()
	errCh := make(chan error, 1)
	go func() {
		errCh <- c.Start(context.Background())
	}()
	select {
	case err := <-errCh:
		return err
	case <-time.After(timeout):
		t.Fatal("SSEClient.Start() did not return within timeout — likely the self-deadlock bug is back (c.mu.Lock() held while RLock/Lock is called again on the same goroutine)")
		return nil
	}
}

func TestSSEClient_Start_NegotiatesNewestByDefault(t *testing.T) {
	srv := newFakeSSEServer(t, func(method string, params json.RawMessage) (json.RawMessage, *RPCError) {
		switch method {
		case protocol.MethodServerDiscover:
			result, _ := protocol.MarshalDiscoverResult(protocol.ImplementationInfo{Name: "fake", Version: "1"})
			return result, nil
		case MethodToolsList:
			return json.RawMessage(`{"tools":[]}`), nil
		}
		return nil, &RPCError{Code: ErrorCodeMethodNotFound, Message: "not found"}
	})
	defer srv.Close()

	server := &ServerInfo{
		ID:     "fake-sse",
		Config: config.MCPServerConfig{ID: "fake-sse", Transport: "sse", URL: srv.URL + "/sse", ProtocolVersion: "auto"},
	}
	c := NewSSEClient(server)
	defer c.Close()

	if err := runStartWithTimeout(t, c, 5*time.Second); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if !c.IsConnected() {
		t.Fatal("expected connected after successful Start")
	}
	if c.NegotiatedVersion() != protocol.V20260728 {
		t.Errorf("NegotiatedVersion() = %q, want newest %q", c.NegotiatedVersion(), protocol.V20260728)
	}
}

func TestSSEClient_Start_FallsBackToOlderVersion(t *testing.T) {
	srv := newFakeSSEServer(t, func(method string, params json.RawMessage) (json.RawMessage, *RPCError) {
		switch method {
		case "initialize":
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(params, &p)
			b, _ := json.Marshal(map[string]any{"protocolVersion": p.ProtocolVersion})
			return b, nil
		case MethodToolsList:
			return json.RawMessage(`{"tools":[]}`), nil
		}
		// server/discover unsupported — simulates a 2025-03-26-only server.
		return nil, &RPCError{Code: ErrorCodeMethodNotFound, Message: "not found"}
	})
	defer srv.Close()

	server := &ServerInfo{
		ID:     "fake-sse-old",
		Config: config.MCPServerConfig{ID: "fake-sse-old", Transport: "sse", URL: srv.URL + "/sse", ProtocolVersion: "auto"},
	}
	c := NewSSEClient(server)
	defer c.Close()

	if err := runStartWithTimeout(t, c, 5*time.Second); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if c.NegotiatedVersion() != protocol.V20250326 {
		t.Errorf("NegotiatedVersion() = %q, want %q", c.NegotiatedVersion(), protocol.V20250326)
	}
}

func TestSSEClient_Start_NoHandshakeAcceptedFails(t *testing.T) {
	srv := newFakeSSEServer(t, func(method string, params json.RawMessage) (json.RawMessage, *RPCError) {
		return nil, &RPCError{Code: ErrorCodeMethodNotFound, Message: "not found"}
	})
	defer srv.Close()

	server := &ServerInfo{
		ID:     "fake-sse-broken",
		Config: config.MCPServerConfig{ID: "fake-sse-broken", Transport: "sse", URL: srv.URL + "/sse", ProtocolVersion: "auto"},
	}
	c := NewSSEClient(server)
	defer c.Close()

	err := runStartWithTimeout(t, c, 5*time.Second)
	if err == nil {
		t.Fatal("expected Start() to fail when no protocol version's handshake is accepted")
	}
}
