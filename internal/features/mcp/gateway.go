package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/config/openapi"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/model"
)

const (
	mcpProtocolVersion = "2025-03-26"
	mcpServerName      = "ilter-mcp-gateway"
	mcpServerVersion   = "1.0.0"
)

// Gateway is the MCP protocol orchestrator.
type Gateway struct {
	registry        *Registry
	authorizer      *Authorizer
	auditLogger     *AuditLogger
	store           *db.SQLiteStore
	config          *config.MCPConfig
	httpClient      *http.Client
	executor        *Executor
	openapiProvider *openapi.ToolProvider
	cfgCache        *config.Cache
}

func (g *Gateway) SetOpenAPIProvider(p *openapi.ToolProvider) {
	g.openapiProvider = p
}

func (g *Gateway) SetConfigCache(c *config.Cache) {
	g.cfgCache = c
}

// NewGateway creates a fully wired MCP gateway.
func NewGateway(
	registry *Registry,
	authorizer *Authorizer,
	auditLogger *AuditLogger,
	store *db.SQLiteStore,
	cfg *config.MCPConfig,
	executor *Executor,
) *Gateway {
	return &Gateway{
		registry:    registry,
		authorizer:  authorizer,
		auditLogger: auditLogger,
		store:       store,
		config:      cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		executor: executor,
	}
}

// Dispatch routes an incoming JSON-RPC request to the appropriate handler.
// It performs auth checks, audit logging, and metric recording.
// rctx carries per-request auth info extracted by the HTTP handler from the auth middleware.
func (g *Gateway) Dispatch(req *JSONRPCRequest, rctx *RequestContext) *JSONRPCResponse {
	start := time.Now()

	if req.ID == nil {
		if req.Method == MethodNotificationsInitialized {
			mcpLog.Debug("client initialized")
		} else {
			mcpLog.Debug("received unknown notification", "method", req.Method)
		}
		return nil
	}

	var resp *JSONRPCResponse
	var paramsMap map[string]any

	switch req.Method {
	case MethodInitialize:
		resp = g.handleInitialize(req)
	case MethodToolsList:
		resp = g.handleToolsList(req, rctx)
	case MethodToolsCall:
		resp = g.handleToolsCall(req, rctx)
	case MethodPing:
		resp = g.handlePing(req)
	default:
		resp = NewErrorResponse(req.ID, ErrorCodeMethodNotFound, "Method not found: "+req.Method)
	}

	if resp != nil {
		duration := time.Since(start)

		if MCPRequestsTotal != nil {
			MCPRequestsTotal.Add(context.Background(), 1)
		}
		if MCPRequestDuration != nil {
			MCPRequestDuration.Record(context.Background(), duration.Seconds()*1000)
		}

		// tools/call is audited with tool-level detail further down the call
		// chain (Executor.logAudit for MCP server tools, the openapi_ branch
		// of handleToolsCall for OpenAPI meta-tools) — logging it here too
		// would double-insert every tool call into mcp_audit_log.
		if req.Method != MethodToolsCall {
			success := resp.Error == nil
			errorMsg := ""
			if resp.Error != nil {
				errorMsg = resp.Error.Message
			}
			paramsStr := ""
			if len(req.Params) > 0 {
				if err := json.Unmarshal(req.Params, &paramsMap); err != nil {
					slog.Warn("failed to unmarshal MCP tool params", "error", err)
				}
			}
			if b, err := json.Marshal(paramsMap); err == nil {
				paramsStr = string(b)
			}
			g.logAudit(AuditEntry{
				APIKeyID:   rctx.KeyID,
				Tool:       req.Method,
				ServerID:   "",
				Method:     req.Method,
				Params:     paramsStr,
				DurationMs: float64(duration.Microseconds()) / 1000.0,
				StatusCode: 200,
				Success:    success,
				ErrorMsg:   errorMsg,
				ClientIP:   rctx.ClientIP,
			})
		}
	}

	return resp
}

func (g *Gateway) logAudit(entry AuditEntry) {
	if g.auditLogger != nil {
		g.auditLogger.LogAsync(entry)
	}
}

func (g *Gateway) handleInitialize(req *JSONRPCRequest) *JSONRPCResponse {
	var params InitializeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewErrorResponse(req.ID, ErrorCodeInvalidParams, "Invalid initialize params: "+err.Error())
		}
	}

	if params.ProtocolVersion != "" && params.ProtocolVersion != mcpProtocolVersion {
		mcpLog.Warn(
			"client requested different protocol version, responding with server version",
			"client_version", params.ProtocolVersion,
			"server_version", mcpProtocolVersion,
		)
	}

	result := InitializeResult{
		ProtocolVersion: mcpProtocolVersion,
		Capabilities: ServerCapabilities{
			Tools: &ServerToolsCap{
				ListChanged: false,
			},
		},
		ServerInfo: ImplementationInfo{
			Name:    mcpServerName,
			Version: mcpServerVersion,
		},
	}

	return NewSuccessResponse(req.ID, result)
}

func (g *Gateway) handleToolsList(req *JSONRPCRequest, rctx *RequestContext) *JSONRPCResponse {
	var tools []ToolDefinition

	// 1. MCP Server Tools (if mcp feature is enabled)
	if (g.cfgCache == nil || config.IsEnabled(g.cfgCache, "mcp")) && g.registry != nil {
		allTools := g.registry.ListTools()

		toolNames := make([]string, 0, len(allTools))
		for _, ti := range allTools {
			toolNames = append(toolNames, ti.Tool.Name)
		}

		authorized := g.authorizer.GetAuthorizedTools(rctx.KeyPrefix, nil, rctx.KeyID, toolNames)

		authSet := make(map[string]bool, len(authorized))
		for _, name := range authorized {
			authSet[name] = true
		}

		var authorizedTools []ToolInfo
		for _, ti := range allTools {
			if authSet[ti.Tool.Name] {
				authorizedTools = append(authorizedTools, ti)
			}
		}

		type void struct{}
		var voidVal void
		nameServers := make(map[string]map[string]void)
		for _, ti := range allTools {
			if nameServers[ti.Tool.Name] == nil {
				nameServers[ti.Tool.Name] = make(map[string]void)
			}
			nameServers[ti.Tool.Name][ti.ServerID] = voidVal
		}

		type namedEntry struct {
			name string
			ti   ToolInfo
		}
		entries := make([]namedEntry, 0, len(authorizedTools))
		emittedCount := make(map[string]int, len(authorizedTools))
		for _, ti := range authorizedTools {
			name := ti.Tool.Name
			if len(nameServers[name]) > 1 {
				name = SanitizeToolName(ti.ServerID, ti.Tool.Name)
			}
			entries = append(entries, namedEntry{name: name, ti: ti})
			emittedCount[name]++
		}

		for _, e := range entries {
			t := e.ti.Tool
			t.Name = e.name
			if emittedCount[e.name] > 1 {
				t.Name = SanitizeToolName(e.ti.ServerID, e.ti.Tool.Name)
			}
			tools = append(tools, t)
		}
	}

	// 2. OpenAPI Meta-Tools (if openapi feature is enabled)
	if (g.cfgCache == nil || config.IsEnabled(g.cfgCache, "openapi")) && g.openapiProvider != nil {
		openAPITools := g.openapiProvider.GetAuthorizedTools(rctx.KeyID, nil)
		for _, ot := range openAPITools {
			rawParams, _ := json.Marshal(ot.Function.Parameters)
			tools = append(tools, ToolDefinition{
				Name:        ot.Function.Name,
				Description: ot.Function.Description,
				InputSchema: json.RawMessage(rawParams),
			})
		}
	}

	result := ListToolsResult{Tools: tools}
	return NewSuccessResponse(req.ID, result)
}

// describeOpenAPICall extracts a human-meaningful, audit-log-friendly label
// for an OpenAPI meta-tool call from its raw JSON-RPC arguments: the target
// operation_id for openapi_call, the queried operation_ids for
// openapi_describe, or the search intent for openapi_search. Falls back to
// the bare meta-tool name if the arguments don't parse as expected.
func describeOpenAPICall(toolName string, args json.RawMessage) string {
	const maxLabelLen = 80
	truncate := func(s string) string {
		if len(s) > maxLabelLen {
			return s[:maxLabelLen-3] + "..."
		}
		return s
	}

	switch toolName {
	case "openapi_call":
		var a struct {
			OperationID string `json:"operation_id"`
		}
		if json.Unmarshal(args, &a) == nil && a.OperationID != "" {
			return a.OperationID
		}
	case "openapi_describe":
		var a struct {
			OperationIDs any `json:"operation_ids"`
		}
		if json.Unmarshal(args, &a) == nil {
			var ids []string
			switch v := a.OperationIDs.(type) {
			case string:
				ids = []string{v}
			case []any:
				for _, x := range v {
					if s, ok := x.(string); ok {
						ids = append(ids, s)
					}
				}
			}
			if len(ids) > 0 {
				return truncate("describe: " + strings.Join(ids, ", "))
			}
		}
	case "openapi_search":
		var a struct {
			Intent string `json:"intent"`
		}
		if json.Unmarshal(args, &a) == nil && a.Intent != "" {
			return truncate("search: " + a.Intent)
		}
	}
	return toolName
}

func (g *Gateway) handleToolsCall(req *JSONRPCRequest, rctx *RequestContext) *JSONRPCResponse {
	var params CallToolParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewErrorResponse(req.ID, ErrorCodeInvalidParams, "Invalid call_tool params: "+err.Error())
		}
	}

	// Handle OpenAPI meta-tools (openapi_search, openapi_describe, openapi_call)
	if strings.HasPrefix(params.Name, "openapi_") {
		start := time.Now()
		// Audit under the actual operation (e.g. "Petstore_getPetById") rather
		// than the generic meta-tool name — every openapi_call row would
		// otherwise say "openapi_call" and be indistinguishable from every
		// other one. ServerID stays the "openapi" sentinel so the Logs page
		// can still tell these apart from real MCP server tool calls.
		toolLabel := describeOpenAPICall(params.Name, params.Arguments)
		logOpenAPICall := func(statusCode int, success bool, errMsg string) {
			g.logAudit(AuditEntry{
				APIKeyID:   rctx.KeyID,
				Tool:       toolLabel,
				ServerID:   "openapi",
				Method:     MethodToolsCall,
				Params:     string(params.Arguments),
				DurationMs: float64(time.Since(start).Microseconds()) / 1000.0,
				StatusCode: statusCode,
				Success:    success,
				ErrorMsg:   errMsg,
				ClientIP:   rctx.ClientIP,
			})
		}

		if g.cfgCache != nil && !config.IsEnabled(g.cfgCache, "openapi") {
			logOpenAPICall(404, false, "openapi feature disabled")
			return NewErrorResponse(req.ID, ErrorCodeToolNotFound, "Tool not found: "+params.Name)
		}
		if g.openapiProvider == nil {
			logOpenAPICall(404, false, "openapi provider not configured")
			return NewErrorResponse(req.ID, ErrorCodeToolNotFound, "Tool not found: "+params.Name)
		}
		toolCall := model.ToolCall{
			ID:   "mcp-call-" + uuid.NewString(),
			Type: "function",
			Function: model.ToolCallFunctionData{
				Name:      params.Name,
				Arguments: string(params.Arguments),
			},
		}
		msgs, errFlags := g.openapiProvider.Execute(context.Background(), rctx.KeyID, rctx.KeyPrefix, []model.ToolCall{toolCall})
		if len(msgs) > 1 {
			contentStr, _ := msgs[1].Content.(string)
			isErr := len(errFlags) > 0 && errFlags[0]
			errMsg := ""
			if isErr {
				errMsg = contentStr
			}
			logOpenAPICall(200, !isErr, errMsg)
			res := CallToolResult{
				Content: []ToolContent{
					{Type: "text", Text: contentStr},
				},
				IsError: isErr,
			}
			return NewSuccessResponse(req.ID, res)
		}
		logOpenAPICall(500, false, "OpenAPI tool execution returned no response")
		return NewErrorResponse(req.ID, ErrorCodeToolExecution, "OpenAPI tool execution returned no response")
	}

	// Handle MCP Server tools
	if g.cfgCache != nil && !config.IsEnabled(g.cfgCache, "mcp") {
		return NewErrorResponse(req.ID, ErrorCodeToolNotFound, "Tool not found: "+params.Name)
	}

	if g.authorizer != nil {
		tool, server, err := g.registry.ResolveTool(params.Name)
		if err != nil || tool == nil {
			return NewErrorResponse(req.ID, ErrorCodeToolNotFound, "Tool not found: "+params.Name)
		}
		result := g.authorizer.CheckAccess(rctx.KeyPrefix, nil, rctx.KeyID, server.ID, tool.Name)
		if !result.Allowed {
			return NewErrorResponse(req.ID, ErrorCodeToolNotFound, "Tool not found")
		}
	}

	if g.executor == nil {
		return NewErrorResponse(req.ID, ErrorCodeInternal, "Executor not available")
	}

	execParams := &ExecuteToolParams{
		ToolName:  params.Name,
		Arguments: params.Arguments,
		APIKeyID:  rctx.KeyID,
		KeyPrefix: rctx.KeyPrefix,
		ClientIP:  rctx.ClientIP,
	}

	result := g.executor.ExecuteTool(context.Background(), execParams)
	if result != nil {
		return NewSuccessResponse(req.ID, result)
	}
	return NewErrorResponse(req.ID, ErrorCodeToolExecution, "Tool execution failed")
}

func (g *Gateway) Store() *db.SQLiteStore {
	return g.store
}

func (g *Gateway) handlePing(req *JSONRPCRequest) *JSONRPCResponse {
	return NewSuccessResponse(req.ID, map[string]any{})
}
