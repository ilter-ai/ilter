package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/features/mcp/protocol"
)

const hubToolSep = "__"

// Hub exposes installed MCP servers to external clients (e.g. VSCode/Cursor)
// over SSE + JSON-RPC. Tool names are prefixed as serverID__toolName when
// exposed to the client, and unprefixed before passing to the Executor.
type Hub struct {
	registry   *Registry
	authorizer *Authorizer
	executor   *Executor
	store      *db.SQLiteStore
	cfg        *config.MCPConfig
}

func NewHub(registry *Registry, authorizer *Authorizer, executor *Executor, store *db.SQLiteStore, cfg *config.MCPConfig) *Hub {
	return &Hub{
		registry:   registry,
		authorizer: authorizer,
		executor:   executor,
		store:      store,
		cfg:        cfg,
	}
}

func toolExposedName(serverID, toolName string) string {
	return serverID + hubToolSep + toolName
}

func splitExposedName(exposed string) (string, string) {
	before, after, ok := strings.Cut(exposed, hubToolSep)
	if !ok {
		return "", exposed
	}
	return before, after
}

func (hub *Hub) Dispatch(req *JSONRPCRequest, session *Session) *JSONRPCResponse {
	start := time.Now()

	if req.ID == nil {
		switch req.Method {
		case MethodNotificationsInitialized:
			hub.handleNotificationInitialized(session)
		default:
			mcpLog.Debug("received unknown notification", "method", req.Method)
		}
		return nil
	}

	var resp *JSONRPCResponse

	// server/discover is version-agnostic by definition — a client calling
	// it hasn't picked a version yet — so it is answered before any
	// per-session Version resolution.
	if req.Method == protocol.MethodServerDiscover {
		resultJSON, err := protocol.MarshalDiscoverResult(protocol.ImplementationInfo{
			Name:    "ilter-mcp-hub",
			Version: mcpServerVersion,
		})
		if err != nil {
			resp = NewErrorResponse(req.ID, ErrorCodeInternal, "server/discover failed")
		} else {
			resp = NewSuccessResponse(req.ID, resultJSON)
		}
		return hub.finish(resp, start)
	}

	// `initialize` is the method that ESTABLISHES a session's version, so
	// it must run before any method-support gate keyed on the session's
	// (pre-negotiation) version — handleInitialize does its own
	// negotiation and graceful degradation internally.
	if req.Method == MethodInitialize {
		resp = hub.handleInitialize(req, session)
		return hub.finish(resp, start)
	}

	// Every other method is dispatched against this session's already-
	// negotiated protocol.Version (or the newest ilter supports, as a
	// defensive default, if a client somehow calls something else before
	// ever calling initialize).
	version := session.protocolVersionOrDefault()

	if !version.IsMethodSupported(req.Method) {
		resp = NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrMethodNotFound), "Method not found: "+req.Method)
		return hub.finish(resp, start)
	}

	switch req.Method {
	case MethodToolsList:
		resp = hub.handleToolsList(req, session, version)
	case MethodToolsCall:
		resp = hub.handleToolsCall(req, session, version)
	case MethodPing:
		resp = NewSuccessResponse(req.ID, map[string]any{})
	default:
		resp = NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrMethodNotFound), "Method not found: "+req.Method)
	}

	return hub.finish(resp, start)
}

// finish records metrics for a completed request and returns resp
// unchanged — factored out so the server/discover and method-not-supported
// early-return paths in Dispatch record the same metrics as the main
// switch does.
func (hub *Hub) finish(resp *JSONRPCResponse, start time.Time) *JSONRPCResponse {
	if resp != nil {
		if MCPRequestsTotal != nil {
			MCPRequestsTotal.Add(context.Background(), 1)
		}
		if MCPRequestDuration != nil {
			MCPRequestDuration.Record(context.Background(), float64(time.Since(start).Microseconds())/1000.0)
		}
	}
	return resp
}

// handleInitialize negotiates a protocol.Version from the client's
// requested protocolVersion (falling back to the newest ilter supports if
// the request omits it or names something unrecognized — spec-compliant
// "server picks a version it supports" behavior), pins it on the session
// for the rest of its lifetime, and delegates the actual InitializeResult
// shape to that version's HandleInitialize.
//
// A client that reaches this method at all is, by definition, using the
// legacy stateful handshake — a genuinely 2026-07-28-native client
// wouldn't call `initialize` (that version removed it). If negotiation
// lands on a version that doesn't support this handshake
// (protocol.ErrNoInitializeHandshake), ilter gracefully degrades to the
// newest version that DOES, rather than erroring out a client that's
// merely a version or two behind ilter's newest support.
func (hub *Hub) handleInitialize(req *JSONRPCRequest, session *Session) *JSONRPCResponse {
	var params InitializeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewErrorResponse(req.ID, ErrorCodeInvalidParams, "Invalid initialize params: "+err.Error())
		}
	}

	version := protocol.Negotiate(protocol.ID(params.ProtocolVersion))
	resultJSON, err := version.HandleInitialize(req.Params, protocol.ImplementationInfo{
		Name:    "ilter-mcp-hub",
		Version: mcpServerVersion,
	})
	if errors.Is(err, protocol.ErrNoInitializeHandshake) {
		for _, id := range protocol.Supported {
			if id == version.ID() {
				continue
			}
			fallback := protocol.Negotiate(id)
			resultJSON, err = fallback.HandleInitialize(req.Params, protocol.ImplementationInfo{
				Name:    "ilter-mcp-hub",
				Version: mcpServerVersion,
			})
			if err == nil {
				version = fallback
				break
			}
		}
	}
	if err != nil {
		return NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrMethodNotFound), err.Error())
	}

	session.ProtocolVersion = version.ID()
	session.ClientName = params.ClientInfo.Name
	session.ClientVersion = params.ClientInfo.Version
	return NewSuccessResponse(req.ID, resultJSON)
}

func (hub *Hub) handleNotificationInitialized(session *Session) {
	session.Initialized = true
	mcpLog.Debug(
		"session initialized",
		"session_id", session.ID,
		"client", session.ClientName,
		"version", session.ClientVersion,
	)
}

func (hub *Hub) handleToolsList(req *JSONRPCRequest, session *Session, version protocol.Version) *JSONRPCResponse {
	allTools := hub.registry.ListTools()

	allExposed := make([]ToolDefinition, 0, len(allTools))
	exposedNames := make([]string, 0, len(allTools))
	for _, ti := range allTools {
		exposed := ToolDefinition{
			Name:        toolExposedName(ti.ServerID, ti.Tool.Name),
			Description: ti.Tool.Description,
			InputSchema: ti.Tool.InputSchema,
		}
		allExposed = append(allExposed, exposed)
		exposedNames = append(exposedNames, exposed.Name)
	}

	keyPrefix := session.KeyPrefix
	keyID := session.KeyID
	authorized := hub.authorizer.GetAuthorizedTools(keyPrefix, nil, keyID, exposedNames)

	authSet := make(map[string]bool, len(authorized))
	for _, name := range authorized {
		authSet[name] = true
	}

	tools := make([]ToolDefinition, 0, len(authorized))
	for _, t := range allExposed {
		if authSet[t.Name] {
			tools = append(tools, t)
		}
	}

	toolsJSON, err := json.Marshal(tools)
	if err != nil {
		return NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrToolExecution), "failed to encode tools list")
	}
	resultJSON, err := version.WrapToolsListResult(toolsJSON, "")
	if err != nil {
		return NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrToolExecution), "failed to build tools/list result")
	}
	return NewSuccessResponse(req.ID, resultJSON)
}

func (hub *Hub) handleToolsCall(req *JSONRPCRequest, session *Session, version protocol.Version) *JSONRPCResponse {
	// Unconditional, matching today's exact behavior: Hub is a stateful,
	// session/SSE-based transport regardless of negotiated version — a
	// genuinely stateless 2026-07-28 client wouldn't be connecting through
	// this transport at all (that's enforced at the HTTP layer in Phase 2's
	// transport-requirements wiring, ilter-yyil.3.5), so relaxing this
	// check based on Transport().StatefulSessions here would just let an
	// old client that skipped the handshake slip through.
	if !session.Initialized {
		return NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrNotInitialized),
			"Session must be initialized before calling tools")
	}

	var params CallToolParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewErrorResponse(req.ID, ErrorCodeInvalidParams, "Invalid call_tool params: "+err.Error())
		}
	}

	serverID, internalName := splitExposedName(params.Name)
	if serverID == "" || internalName == "" || internalName == params.Name {
		// No separator found — not a valid exposed name.
		return NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrToolNotFound), "Tool not found: invalid name format")
	}

	if hub.authorizer != nil {
		result := hub.authorizer.CheckAccess(session.KeyPrefix, nil, session.KeyID, serverID, internalName)
		if !result.Allowed {
			return NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrToolNotFound), "Tool not found")
		}
	}

	if hub.executor == nil {
		return NewErrorResponse(req.ID, ErrorCodeInternal, "Executor not available")
	}

	execParams := &ExecuteToolParams{
		ToolName:  internalName,
		Arguments: params.Arguments,
		APIKeyID:  session.KeyID,
		KeyPrefix: session.KeyPrefix,
		ClientIP:  session.ClientIP,
	}

	result := hub.executor.ExecuteTool(context.Background(), execParams)
	if result == nil {
		return NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrToolExecution), "Tool execution failed")
	}

	contentJSON, err := json.Marshal(result.Content)
	if err != nil {
		return NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrToolExecution), "failed to encode tool result")
	}
	resultJSON, err := version.WrapCallToolResult(contentJSON, result.IsError)
	if err != nil {
		return NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrToolExecution), "failed to build tools/call result")
	}
	return NewSuccessResponse(req.ID, resultJSON)
}
