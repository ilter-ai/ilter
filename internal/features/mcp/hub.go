package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
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
	idx := strings.Index(exposed, hubToolSep)
	if idx < 0 {
		return "", exposed
	}
	return exposed[:idx], exposed[idx+len(hubToolSep):]
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

	switch req.Method {
	case MethodInitialize:
		resp = hub.handleInitialize(req, session)
	case MethodToolsList:
		resp = hub.handleToolsList(req, session)
	case MethodToolsCall:
		resp = hub.handleToolsCall(req, session)
	case MethodPing:
		resp = NewSuccessResponse(req.ID, map[string]any{})
	default:
		resp = NewErrorResponse(req.ID, ErrorCodeMethodNotFound, "Method not found: "+req.Method)
	}

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

func (hub *Hub) handleInitialize(req *JSONRPCRequest, session *Session) *JSONRPCResponse {
	var params InitializeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewErrorResponse(req.ID, ErrorCodeInvalidParams, "Invalid initialize params: "+err.Error())
		}
	}

	if params.ProtocolVersion != "" && params.ProtocolVersion != mcpProtocolVersion {
		return NewErrorResponse(req.ID, ErrorCodeInvalidParams,
			fmt.Sprintf("Unsupported protocol version: %s", params.ProtocolVersion))
	}

	session.ClientName = params.ClientInfo.Name
	session.ClientVersion = params.ClientInfo.Version

	result := InitializeResult{
		ProtocolVersion: mcpProtocolVersion,
		Capabilities: ServerCapabilities{
			Tools: &ServerToolsCap{
				ListChanged: false,
			},
		},
		ServerInfo: ImplementationInfo{
			Name:    "ilter-mcp-hub",
			Version: mcpServerVersion,
		},
	}

	return NewSuccessResponse(req.ID, result)
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

func (hub *Hub) handleToolsList(req *JSONRPCRequest, session *Session) *JSONRPCResponse {
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

	result := ListToolsResult{Tools: tools}
	return NewSuccessResponse(req.ID, result)
}

func (hub *Hub) handleToolsCall(req *JSONRPCRequest, session *Session) *JSONRPCResponse {
	if !session.Initialized {
		return NewErrorResponse(req.ID, ErrorCodeNotInitialized,
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
		return NewErrorResponse(req.ID, ErrorCodeToolNotFound, "Tool not found: invalid name format")
	}

	if hub.authorizer != nil {
		result := hub.authorizer.CheckAccess(session.KeyPrefix, nil, session.KeyID, serverID, internalName)
		if !result.Allowed {
			return NewErrorResponse(req.ID, ErrorCodeToolNotFound, "Tool not found")
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
	if result != nil {
		return NewSuccessResponse(req.ID, result)
	}
	return NewErrorResponse(req.ID, ErrorCodeToolExecution, "Tool execution failed")
}
