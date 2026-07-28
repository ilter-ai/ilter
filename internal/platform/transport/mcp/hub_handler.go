package mcptransport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	mcp "github.com/ilter-ai/ilter/internal/features/mcp"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

// HubHandler serves the MCP Hub endpoints for external clients (e.g. VSCode/Cursor):
//   - GET  /mcp/sse — SSE stream (session handshake + keepalive)
//   - POST /mcp/sse — JSON-RPC message dispatch (via Hub.Dispatch)
type HubHandler struct {
	hub      *mcp.Hub
	sessions *mcp.SessionManager
}

func NewHubHandler(hub *mcp.Hub, sessions *mcp.SessionManager) *HubHandler {
	return &HubHandler{
		hub:      hub,
		sessions: sessions,
	}
}

func (h *HubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.handleSSE(w, r)
	case http.MethodPost:
		h.handleMessage(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *HubHandler) handleSSE(w http.ResponseWriter, r *http.Request) {
	keyID := reqmeta.GetKeyID(r.Context())
	clientIP := extractClientIP(r)

	session := h.sessions.Create(keyID, "")
	session.ClientIP = clientIP

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.sessions.Delete(session.ID)
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Send the endpoint event so the client knows where to POST messages.
	postURL := fmt.Sprintf("%s?sessionId=%s", r.URL.Path, session.ID)
	_, _ = fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", postURL)
	flusher.Flush()

	if mcp.ActiveConnections != nil {
		mcp.ActiveConnections.Record(r.Context(), int64(h.sessions.Count()))
	}

	transportLog.Debug(
		"SSE session started",
		"session_id", session.ID,
		"key_id", keyID,
	)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			h.cleanupSession(session)
			return

		case <-ticker.C:
			// SSE keepalive comment — prevents proxies from closing idle connections.
			_, _ = fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()

		case event, ok := <-session.NotifyCh:
			if !ok {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				transportLog.Warn("failed to marshal server event", "error", err)
				continue
			}
			_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func (h *HubHandler) handleMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		writeJSONError(w, http.StatusBadRequest, "Missing sessionId query parameter")
		return
	}

	session := h.sessions.Get(sessionID)
	if session == nil {
		writeJSONError(w, http.StatusNotFound, "Session not found or expired")
		return
	}

	session.ClientIP = extractClientIP(r)

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
			"session_id", sessionID,
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

	resp := h.hub.Dispatch(&req, session)

	// Notifications (no ID) get a 202 Accepted with no body.
	if req.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		transportLog.Error("failed to encode JSON-RPC response", "error", err)
	}
}

func (h *HubHandler) cleanupSession(session *mcp.Session) {
	h.sessions.Delete(session.ID)
	if mcp.ActiveConnections != nil {
		mcp.ActiveConnections.Record(context.Background(), int64(h.sessions.Count()))
	}

	transportLog.Debug(
		"SSE session ended",
		"session_id", session.ID,
	)
}
