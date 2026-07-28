package mcptransport

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/ilter-ai/ilter/internal/config"
	mcp "github.com/ilter-ai/ilter/internal/features/mcp"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

var transportLog = slog.With("component", "mcptransport")

// GatewayHandler serves the MCP Gateway endpoint.
// Supports two transports on the same URL:
//   - Streamable HTTP (POST, no sessionId): JSON-RPC request/response in HTTP body.
//   - Legacy SSE (GET + POST with sessionId): GET opens SSE stream, POST via ?sessionId= responds on stream.
type GatewayHandler struct {
	gateway  *mcp.Gateway
	sessions map[string]chan *mcp.JSONRPCResponse
	cfgCache *config.Cache
	mu       sync.RWMutex
}

func NewGatewayHandler(gateway *mcp.Gateway) *GatewayHandler {
	return &GatewayHandler{
		gateway:  gateway,
		sessions: make(map[string]chan *mcp.JSONRPCResponse),
	}
}

func (h *GatewayHandler) SetConfigCache(c *config.Cache) {
	h.cfgCache = c
}

func (h *GatewayHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.cfgCache != nil && !config.IsEnabled(h.cfgCache, "mcp") && !config.IsEnabled(h.cfgCache, "openapi") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		if err := json.NewEncoder(w).Encode(map[string]string{"error": "feature_disabled"}); err != nil {
			transportLog.Error("failed to encode feature_disabled response", "error", err)
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.handleSSE(w, r)
	case http.MethodPost:
		h.handleMessage(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleSSE establishes an SSE stream for legacy MCP clients.
// Generates a session ID, sends the endpoint event with ?sessionId=, and
// relays JSON-RPC responses as SSE message events until the client disconnects.
func (h *GatewayHandler) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	if h.sessions == nil {
		h.sessions = make(map[string]chan *mcp.JSONRPCResponse)
	}
	sessionID := uuid.NewString()
	ch := make(chan *mcp.JSONRPCResponse, 16)

	h.mu.Lock()
	h.sessions[sessionID] = ch
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.sessions, sessionID)
		h.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	endpoint := fmt.Sprintf("%s?sessionId=%s", r.URL.Path, sessionID)
	fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", endpoint)
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case resp, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(resp)
			if err != nil {
				transportLog.Error("failed to marshal response", "error", err)
				continue
			}
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func (h *GatewayHandler) handleMessage(w http.ResponseWriter, r *http.Request) {
	keyID := reqmeta.GetKeyID(r.Context())
	clientIP := extractClientIP(r)

	keyPrefix := ""
	if keyID != "" && !mcp.IsSyntheticKeyID(keyID) {
		var err error
		keyPrefix, err = mcp.ExtractKeyInfo(keyID, h.gateway.Store())
		if err != nil {
			transportLog.Debug("failed to resolve key prefix", "key_id", keyID, "error", err)
		}
	}

	rctx := &mcp.RequestContext{
		KeyID:     keyID,
		KeyPrefix: keyPrefix,
		ClientIP:  clientIP,
	}

	body, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		writeJSONRPCError(w, nil, mcp.ErrorCodeParse, "Failed to read request body")
		return
	}

	if len(body) == 0 {
		writeJSONRPCError(w, nil, mcp.ErrorCodeInvalidRequest, "Empty request body")
		return
	}

	var req mcp.JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		transportLog.Warn(
			"failed to parse JSON-RPC request",
			"error", err,
		)
		writeJSONRPCError(w, nil, mcp.ErrorCodeParse, "Parse error: "+err.Error())
		return
	}

	if req.JSONRPC != mcp.JSONRPCVersion {
		writeJSONRPCError(w, req.ID, mcp.ErrorCodeInvalidRequest,
			"Invalid jsonrpc version: expected '2.0'")
		return
	}
	if req.Method == "" {
		writeJSONRPCError(w, req.ID, mcp.ErrorCodeInvalidRequest,
			"Missing method")
		return
	}

	resp := h.gateway.Dispatch(&req, rctx)

	// Notifications: no response expected.
	if req.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// SSE session mode: sessionId in query → send response on SSE stream, return 202.
	if sessionID := r.URL.Query().Get("sessionId"); sessionID != "" {
		h.mu.RLock()
		ch, ok := h.sessions[sessionID]
		h.mu.RUnlock()
		if !ok {
			writeJSONRPCError(w, req.ID, mcp.ErrorCodeInvalidRequest, "Unknown session ID")
			return
		}
		select {
		case ch <- resp:
		default:
			transportLog.Warn("SSE session channel full, dropping response",
				"session", sessionID, "method", req.Method)
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// Streamable HTTP mode: return JSON directly.
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		transportLog.Error("failed to encode JSON-RPC response", "error", err)
	}
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		transportLog.Error("failed to encode JSON error response", "error", err)
	}
}

func writeJSONRPCError(w http.ResponseWriter, id *json.RawMessage, code int, msg string) {
	resp := mcp.NewErrorResponse(id, code, msg)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		transportLog.Error("failed to encode JSON-RPC error response", "error", err)
	}
}

func extractClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}
