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
	"github.com/ilter-ai/ilter/internal/features/mcp/protocol"
	v20260728 "github.com/ilter-ai/ilter/internal/features/mcp/protocol/v20260728"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

var transportLog = slog.With("component", "mcptransport")

// gatewaySession pairs an SSE-mode session's response channel with the
// protocol.Version negotiated for it — set on that session's first
// `initialize` call (mcp.Gateway.handleInitialize writes the negotiated
// version into the RequestContext it's given; handleMessage below persists
// it here so subsequent requests on the same ?sessionId= reuse it instead
// of renegotiating from nothing every time).
type gatewaySession struct {
	ch      chan *mcp.JSONRPCResponse
	version protocol.ID
}

// GatewayHandler serves the MCP Gateway endpoint.
// Supports two transports on the same URL:
//   - Streamable HTTP (POST, no sessionId): JSON-RPC request/response in HTTP body.
//   - Legacy SSE (GET + POST with sessionId): GET opens SSE stream, POST via ?sessionId= responds on stream.
type GatewayHandler struct {
	gateway  *mcp.Gateway
	sessions map[string]*gatewaySession
	cfgCache *config.Cache
	mu       sync.RWMutex
}

func NewGatewayHandler(gateway *mcp.Gateway) *GatewayHandler {
	return &GatewayHandler{
		gateway:  gateway,
		sessions: make(map[string]*gatewaySession),
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

// handleSubscriptionsListen serves the 2026-07-28 `subscriptions/listen`
// method: a single long-lived POST-response stream, replacing the legacy
// SSE-GET change-notification model. It parses the requested notification
// types, registers a real subscription with the Gateway's
// mcp.SubscriptionBroker, writes an initial JSON-RPC result (ListenAck)
// carrying the subscription id, then streams every subsequent notification
// as newline-delimited JSON until the client disconnects.
func (h *GatewayHandler) handleSubscriptionsListen(w http.ResponseWriter, r *http.Request, req *mcp.JSONRPCRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONRPCError(w, req.ID, mcp.ErrorCodeInternal, "streaming not supported")
		return
	}

	var params v20260728.ListenParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			writeJSONRPCError(w, req.ID, mcp.ErrorCodeInvalidParams, "invalid subscriptions/listen params: "+err.Error())
			return
		}
	}

	broker := h.gateway.Broker()
	subID, ch := broker.Subscribe(params.Types)
	defer broker.Unsubscribe(subID)

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	ack := mcp.NewSuccessResponse(req.ID, v20260728.ListenAck{SubscriptionID: subID})
	ackBody, err := json.Marshal(ack)
	if err != nil {
		transportLog.Error("failed to marshal subscriptions/listen ack", "error", err)
		return
	}
	if _, err := w.Write(append(ackBody, '\n')); err != nil {
		return
	}
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case notif, ok := <-ch:
			if !ok {
				return
			}
			b, err := json.Marshal(notif)
			if err != nil {
				transportLog.Warn("failed to marshal subscription notification", "error", err)
				continue
			}
			if _, err := w.Write(append(b, '\n')); err != nil {
				return
			}
			flusher.Flush()
		}
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
		h.sessions = make(map[string]*gatewaySession)
	}
	sessionID := uuid.NewString()
	ch := make(chan *mcp.JSONRPCResponse, 16)

	h.mu.Lock()
	h.sessions[sessionID] = &gatewaySession{ch: ch}
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

	// If this request belongs to an existing SSE-mode session, prime
	// rctx.ProtocolVersion from whatever that session already negotiated
	// (empty on the session's very first request, e.g. its initialize
	// call) so Gateway.Dispatch reuses it instead of renegotiating fresh
	// on every request.
	sessionID := r.URL.Query().Get("sessionId")
	var session *gatewaySession
	if sessionID != "" {
		h.mu.RLock()
		session = h.sessions[sessionID]
		h.mu.RUnlock()
		if session != nil {
			rctx.ProtocolVersion = session.version
		}
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

	if errResp := checkTransportHeaders(&req, rctx, r); errResp != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(errResp); err != nil {
			transportLog.Error("failed to encode JSON-RPC error response", "error", err)
		}
		return
	}

	// subscriptions/listen is not a normal request/response call — it
	// opens a long-lived streaming response, so it's served here directly
	// rather than through Gateway.Dispatch (which always returns a single
	// *mcp.JSONRPCResponse).
	if req.Method == v20260728.MethodSubscriptionsListen {
		h.handleSubscriptionsListen(w, r, &req)
		return
	}

	resp := h.gateway.Dispatch(&req, rctx)

	// Persist whatever version Dispatch negotiated (set on a successful
	// initialize; unchanged otherwise) back onto the session so the next
	// request on this sessionId reuses it instead of renegotiating.
	if session != nil && rctx.ProtocolVersion != "" {
		h.mu.Lock()
		session.version = rctx.ProtocolVersion
		h.mu.Unlock()
	}

	// Notifications: no response expected.
	if req.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// SSE session mode: sessionId in query → send response on SSE stream, return 202.
	if sessionID != "" {
		if session == nil {
			writeJSONRPCError(w, req.ID, mcp.ErrorCodeInvalidRequest, "Unknown session ID")
			return
		}
		select {
		case session.ch <- resp:
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

// checkTransportHeaders enforces the 2026-07-28 spec's requirement that
// Streamable HTTP POST requests carry Mcp-Method/Mcp-Name headers so
// load-balancers/gateways can route on the operation without inspecting
// the body. Returns nil (no error) for the two older versions, which never
// defined this requirement, and for a 2026-07-28 request that satisfies
// it. The version hint is read the same way Gateway.resolveVersion reads
// it (per-request `_meta.protocolVersion`, falling back to whatever the
// session already negotiated) since this check runs before Dispatch is
// called at all.
func checkTransportHeaders(req *mcp.JSONRPCRequest, rctx *mcp.RequestContext, r *http.Request) *mcp.JSONRPCResponse {
	hinted := rctx.ProtocolVersion
	if len(req.Params) > 0 {
		var withMeta struct {
			Meta json.RawMessage `json:"_meta"`
		}
		if err := json.Unmarshal(req.Params, &withMeta); err == nil && len(withMeta.Meta) > 0 {
			if meta, err := protocol.ParseRequestMeta(withMeta.Meta); err == nil && meta.ProtocolVersion != "" {
				hinted = meta.ProtocolVersion
			}
		}
	}
	if hinted != protocol.V20260728 {
		return nil
	}

	version := protocol.Negotiate(hinted)
	reqs := version.Transport()

	mcpMethod := r.Header.Get("Mcp-Method")
	if reqs.RequiresMcpMethodHeader && mcpMethod == "" {
		return mcp.NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrHeaderMismatch), "Missing required Mcp-Method header")
	}
	if reqs.RequiresMcpNameHeader && r.Header.Get("Mcp-Name") == "" {
		return mcp.NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrHeaderMismatch), "Missing required Mcp-Name header")
	}
	if mcpMethod != "" && mcpMethod != req.Method {
		return mcp.NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrHeaderMismatch), "Mcp-Method header does not match request method")
	}
	return nil
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
