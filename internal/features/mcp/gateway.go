package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/config/openapi"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/features/mcp/protocol"
	"github.com/ilter-ai/ilter/internal/features/mcp/protocol/v20260728"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/version"
)

const (
	mcpProtocolVersion = "2025-03-26"
	mcpServerName      = "ilter-mcp-gateway"
)

// mcpServerVersion is ilter's own application version — the same one
// reported by `ilter --version` — not a separately-versioned MCP server
// component.
var mcpServerVersion = version.Version

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

	// broker fans out change notifications to 2026-07-28
	// subscriptions/listen streams (see subscriptions.go); wired to the
	// registry's own tool-change events below so a real downstream change
	// drives a real notification.
	broker *SubscriptionBroker

	// taskManager backs the 2026-07-28 tasks/get + tasks/update methods
	// and the long-running-tools/call-promotion path in handleToolsCall.
	// nil when store is nil (e.g. in tests that don't need DB-backed
	// tasks) — every call site checks before using it.
	taskManager *TaskManager

	// taskPromotionThreshold overrides defaultTaskPromotionThreshold —
	// exposed for tests that need a short-and-fast slow-tool simulation.
	taskPromotionThreshold time.Duration
}

// SetTaskPromotionThreshold overrides how long a 2026-07-28 tools/call may
// run synchronously before being promoted to a background task. Intended
// for tests; production code relies on the default.
func (g *Gateway) SetTaskPromotionThreshold(d time.Duration) {
	g.taskPromotionThreshold = d
}

// Broker returns the Gateway's subscription broker, for the transport
// layer (handler.go) to use when serving subscriptions/listen.
func (g *Gateway) Broker() *SubscriptionBroker {
	return g.broker
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
	g := &Gateway{
		registry:    registry,
		authorizer:  authorizer,
		auditLogger: auditLogger,
		store:       store,
		config:      cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		executor:               executor,
		broker:                 NewSubscriptionBroker(),
		taskPromotionThreshold: defaultTaskPromotionThreshold,
	}
	if registry != nil {
		registry.OnToolsChanged(func() {
			g.broker.Publish(v20260728.NotifyToolsListChanged)
		})
	}
	if store != nil {
		g.taskManager = NewTaskManager(NewTaskStore(store))
	}
	return g
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

	// server/discover is version-agnostic by definition — a client calling
	// it hasn't picked a version yet — so it is answered before any
	// per-request Version resolution.
	if req.Method == protocol.MethodServerDiscover {
		resultJSON, err := protocol.MarshalDiscoverResult(protocol.ImplementationInfo{
			Name:    mcpServerName,
			Version: mcpServerVersion,
		})
		if err != nil {
			return NewErrorResponse(req.ID, ErrorCodeInternal, "server/discover failed")
		}
		return NewSuccessResponse(req.ID, resultJSON)
	}

	// `initialize` is the method that ESTABLISHES a request's/session's
	// version, so it runs before any method-support gate keyed on a
	// resolved Version — handleInitialize does its own negotiation,
	// graceful degradation, and (on success) writes the result back into
	// rctx.ProtocolVersion for the transport layer to persist.
	if req.Method == MethodInitialize {
		resp = g.handleInitialize(req, rctx)
		return g.finishDispatch(req, resp, rctx, start, &paramsMap)
	}

	version := g.resolveVersion(req, rctx)

	if !version.IsMethodSupported(req.Method) {
		resp = NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrMethodNotFound), "Method not found: "+req.Method)
		return g.finishDispatch(req, resp, rctx, start, &paramsMap)
	}

	switch req.Method {
	case MethodToolsList:
		resp = g.handleToolsList(req, rctx, version)
	case MethodToolsCall:
		resp = g.handleToolsCall(req, rctx, version)
	case MethodPing:
		resp = g.handlePing(req)
	case v20260728.MethodTasksGet:
		resp = g.handleTasksGet(req, version)
	case v20260728.MethodTasksUpdate:
		resp = g.handleTasksUpdate(req, version)
	default:
		resp = NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrMethodNotFound), "Method not found: "+req.Method)
	}

	return g.finishDispatch(req, resp, rctx, start, &paramsMap)
}

// finishDispatch records metrics and audit-logs a completed request — the
// tail shared by every Dispatch return path (server/discover, initialize,
// unsupported-method, and the main method switch).
func (g *Gateway) finishDispatch(req *JSONRPCRequest, resp *JSONRPCResponse, rctx *RequestContext, start time.Time, paramsMap *map[string]any) *JSONRPCResponse {
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
				if err := json.Unmarshal(req.Params, paramsMap); err != nil {
					slog.Warn("failed to unmarshal MCP tool params", "error", err)
				}
			}
			if b, err := json.Marshal(*paramsMap); err == nil {
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

// resolveVersion determines the protocol.Version to use for a non-initialize
// request. A per-request `_meta.protocolVersion` (2026-07-28's stateless
// style) takes precedence when present; otherwise the session's already-
// pinned version (set by a prior initialize and persisted into rctx by the
// transport layer) is used.
//
// A request with NEITHER signal is exactly today's pre-refactor status
// quo — a client that never called initialize and never sends `_meta`
// (the only way that was possible before this version existed at all) —
// so it defaults to 2025-03-26, matching gateway.go's exact behavior
// before this refactor, rather than protocol.Newest(): defaulting an
// ambiguous/legacy request to the newest version would silently reject
// methods that version removed (`ping`, most notably) for callers that
// have done nothing wrong, they simply predate version negotiation
// existing at all.
func (g *Gateway) resolveVersion(req *JSONRPCRequest, rctx *RequestContext) protocol.Version {
	if len(req.Params) > 0 {
		var withMeta struct {
			Meta json.RawMessage `json:"_meta"`
		}
		if err := json.Unmarshal(req.Params, &withMeta); err == nil && len(withMeta.Meta) > 0 {
			if meta, err := protocol.ParseRequestMeta(withMeta.Meta); err == nil && meta.ProtocolVersion != "" {
				return protocol.Negotiate(meta.ProtocolVersion)
			}
		}
	}
	if rctx.ProtocolVersion == "" {
		return protocol.Negotiate(protocol.V20250326)
	}
	return protocol.Negotiate(rctx.ProtocolVersion)
}

// handleInitialize negotiates a protocol.Version from the client's
// requested protocolVersion (falling back to the newest ilter supports if
// the request omits it or names something unrecognized — spec-compliant
// "server picks a version it supports" behavior), and on success writes
// the negotiated version into rctx.ProtocolVersion so the transport layer
// (handler.go) can persist it for the rest of this session's requests.
//
// A client that reaches this method at all is, by definition, using the
// legacy stateful handshake — a genuinely 2026-07-28-native client
// wouldn't call `initialize` (that version removed it). If negotiation
// lands on a version that doesn't support this handshake
// (protocol.ErrNoInitializeHandshake), ilter gracefully degrades to the
// newest version that DOES, rather than erroring out a client that's
// merely a version or two behind ilter's newest support.
func (g *Gateway) handleInitialize(req *JSONRPCRequest, rctx *RequestContext) *JSONRPCResponse {
	var params InitializeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewErrorResponse(req.ID, ErrorCodeInvalidParams, "Invalid initialize params: "+err.Error())
		}
	}

	version := protocol.Negotiate(protocol.ID(params.ProtocolVersion))
	resultJSON, err := version.HandleInitialize(req.Params, protocol.ImplementationInfo{
		Name:    mcpServerName,
		Version: mcpServerVersion,
	})
	if errors.Is(err, protocol.ErrNoInitializeHandshake) {
		for _, id := range protocol.Supported {
			if id == version.ID() {
				continue
			}
			fallback := protocol.Negotiate(id)
			resultJSON, err = fallback.HandleInitialize(req.Params, protocol.ImplementationInfo{
				Name:    mcpServerName,
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

	rctx.ProtocolVersion = version.ID()

	if params.ProtocolVersion != "" && params.ProtocolVersion != string(version.ID()) {
		mcpLog.Warn(
			"client requested different protocol version, responding with negotiated version",
			"client_version", params.ProtocolVersion,
			"server_version", version.ID(),
		)
	}

	return NewSuccessResponse(req.ID, resultJSON)
}

func (g *Gateway) handleToolsList(req *JSONRPCRequest, rctx *RequestContext, version protocol.Version) *JSONRPCResponse {
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

func (g *Gateway) handleToolsCall(req *JSONRPCRequest, rctx *RequestContext, version protocol.Version) *JSONRPCResponse {
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
			return NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrToolNotFound), "Tool not found: "+params.Name)
		}
		if g.openapiProvider == nil {
			logOpenAPICall(404, false, "openapi provider not configured")
			return NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrToolNotFound), "Tool not found: "+params.Name)
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
			contentJSON, err := json.Marshal([]ToolContent{{Type: "text", Text: contentStr}})
			if err != nil {
				return NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrToolExecution), "failed to encode tool result")
			}
			resultJSON, err := version.WrapCallToolResult(contentJSON, isErr)
			if err != nil {
				return NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrToolExecution), "failed to build tools/call result")
			}
			return NewSuccessResponse(req.ID, resultJSON)
		}
		logOpenAPICall(500, false, "OpenAPI tool execution returned no response")
		return NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrToolExecution), "OpenAPI tool execution returned no response")
	}

	// Handle MCP Server tools
	if g.cfgCache != nil && !config.IsEnabled(g.cfgCache, "mcp") {
		return NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrToolNotFound), "Tool not found: "+params.Name)
	}

	if g.authorizer != nil {
		tool, server, err := g.registry.ResolveTool(params.Name)
		if err != nil || tool == nil {
			return NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrToolNotFound), "Tool not found: "+params.Name)
		}
		result := g.authorizer.CheckAccess(rctx.KeyPrefix, nil, rctx.KeyID, server.ID, tool.Name)
		if !result.Allowed {
			return NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrToolNotFound), "Tool not found")
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

	// For a 2026-07-28 session, a tool call that runs past
	// taskPromotionThreshold is promoted to a background task instead of
	// blocking this HTTP request — the client gets a task handle back and
	// polls tasks/get (see handleTasksGet), rather than the connection
	// simply hanging until the tool finishes. Older-version sessions (and
	// any session when taskManager is unavailable) keep today's exact
	// synchronous behavior.
	if version.ID() == protocol.V20260728 && g.taskManager != nil {
		return g.executeToolWithPromotion(req, rctx, version, execParams)
	}

	result := g.executor.ExecuteTool(context.Background(), execParams)
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

// defaultTaskPromotionThreshold is how long a 2026-07-28 tools/call is
// allowed to run synchronously before executeToolWithPromotion hands the
// client a task handle instead of continuing to block the request.
const defaultTaskPromotionThreshold = 3 * time.Second

// executeToolWithPromotion runs execParams via the Executor in a
// goroutine; if it finishes within taskPromotionThreshold, the result is
// returned synchronously exactly like the non-2026-07-28 path (same
// WrapCallToolResult shape). If it runs longer, the tool keeps executing
// in the background: a task record is created in the "running" state
// (TaskManager.PromoteRunning) and a TaskHandle is returned immediately;
// the same goroutine finalizes the task (Complete/Fail) whenever the
// Executor call actually returns.
func (g *Gateway) executeToolWithPromotion(req *JSONRPCRequest, rctx *RequestContext, version protocol.Version, execParams *ExecuteToolParams) *JSONRPCResponse {
	outcomeCh := make(chan TaskOutcome, 1)
	go func() {
		result := g.executor.ExecuteTool(context.Background(), execParams)
		if result == nil {
			outcomeCh <- TaskOutcome{Err: fmt.Errorf("tool execution failed")}
			return
		}
		contentJSON, err := json.Marshal(result.Content)
		if err != nil {
			outcomeCh <- TaskOutcome{Err: err}
			return
		}
		outcomeCh <- TaskOutcome{Result: contentJSON, IsError: result.IsError}
	}()

	select {
	case outcome := <-outcomeCh:
		if outcome.Err != nil {
			return NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrToolExecution), outcome.Err.Error())
		}
		resultJSON, err := version.WrapCallToolResult(outcome.Result, outcome.IsError)
		if err != nil {
			return NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrToolExecution), "failed to build tools/call result")
		}
		return NewSuccessResponse(req.ID, resultJSON)

	case <-time.After(g.taskPromotionThreshold):
		serverID := ""
		if tool, server, err := g.registry.ResolveTool(execParams.ToolName); err == nil && tool != nil {
			serverID = server.ID
		}
		taskID, err := g.taskManager.PromoteRunning(rctx.KeyID, serverID, execParams.ToolName, execParams.Arguments, outcomeCh)
		if err != nil {
			return NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrToolExecution), "failed to promote long-running tool call to a task: "+err.Error())
		}
		return NewSuccessResponse(req.ID, v20260728.BuildTaskHandle(taskID))
	}
}

func (g *Gateway) Store() *db.SQLiteStore {
	return g.store
}

func (g *Gateway) handlePing(req *JSONRPCRequest) *JSONRPCResponse {
	return NewSuccessResponse(req.ID, map[string]any{})
}

// handleTasksGet implements tasks/get (2026-07-28 only, gated upstream by
// version.IsMethodSupported): polls the current state of a task and
// returns it as an MRTR-shaped TaskResult (resultType "complete" once the
// task has a terminal result/error, or "input_required" while it's
// paused waiting on tasks/update).
func (g *Gateway) handleTasksGet(req *JSONRPCRequest, version protocol.Version) *JSONRPCResponse {
	if g.taskManager == nil {
		return NewErrorResponse(req.ID, ErrorCodeInternal, "Tasks extension not available")
	}

	var params v20260728.GetTaskParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewErrorResponse(req.ID, ErrorCodeInvalidParams, "Invalid tasks/get params: "+err.Error())
		}
	}
	if params.TaskID == "" {
		return NewErrorResponse(req.ID, ErrorCodeInvalidParams, "taskId is required")
	}

	task, err := g.taskManager.Get(context.Background(), params.TaskID)
	if err != nil {
		return NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrToolNotFound), "Task not found: "+params.TaskID)
	}

	result := v20260728.TaskResult{
		TaskID: task.ID,
		Status: string(task.Status),
	}
	if task.Status == TaskStatusInputRequired {
		result.ResultType = "input_required"
		result.InputRequests = task.InputRequiredPayload
	} else {
		result.ResultType = "complete"
		if task.Status == TaskStatusFailed {
			result.Error = task.ErrorMessage
		} else {
			result.Result = task.Result
		}
	}
	return NewSuccessResponse(req.ID, result)
}

// handleTasksUpdate implements tasks/update (2026-07-28 only): delivers
// client-supplied input to a task currently paused in the input_required
// state, resuming whichever goroutine is blocked in TaskManager.RequestInput.
func (g *Gateway) handleTasksUpdate(req *JSONRPCRequest, version protocol.Version) *JSONRPCResponse {
	if g.taskManager == nil {
		return NewErrorResponse(req.ID, ErrorCodeInternal, "Tasks extension not available")
	}

	var params v20260728.UpdateTaskParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return NewErrorResponse(req.ID, ErrorCodeInvalidParams, "Invalid tasks/update params: "+err.Error())
		}
	}
	if params.TaskID == "" {
		return NewErrorResponse(req.ID, ErrorCodeInvalidParams, "taskId is required")
	}

	if err := g.taskManager.Update(params.TaskID, params.Input); err != nil {
		return NewErrorResponse(req.ID, version.ErrorCode(protocol.ErrToolNotFound), err.Error())
	}
	return NewSuccessResponse(req.ID, v20260728.UpdateTaskResult{ResultType: "complete", TaskID: params.TaskID})
}
