package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v7"
	"github.com/sony/gobreaker/v2"
)

// Executor resolves the server for a tool call, delegates to the appropriate
// TransportClient, and applies retry/backoff + circuit circuitbreaker.
type Executor struct {
	registry        *Registry
	clients         *ClientManager
	authorizer      *Authorizer
	auditLog        *AuditLogger
	db              *sql.DB
	toolConfigCache sync.Map // toolName → *ToolConfig
	rateLimits      sync.Map // toolName → *rateLimitWindow
}

func NewExecutor(registry *Registry, clients *ClientManager, authorizer *Authorizer, auditLog *AuditLogger, db *sql.DB) *Executor {
	return &Executor{
		registry:   registry,
		clients:    clients,
		authorizer: authorizer,
		auditLog:   auditLog,
		db:         db,
	}
}

// ExecuteToolParams carries all context needed by ExecuteTool.
type ExecuteToolParams struct {
	ToolName  string
	Arguments json.RawMessage
	APIKeyID  string
	KeyPrefix string
	ClientIP  string
}

// ExecuteTool finds the server that owns toolName, obtains a transport client,
// dispatches a tools/call JSON-RPC request, and returns the result.
func (ex *Executor) ExecuteTool(ctx context.Context, p *ExecuteToolParams) *CallToolResult {
	start := time.Now()

	// 1. Look up the tool in the catalog.
	tool, server, err := ex.registry.ResolveTool(p.ToolName)
	if err != nil {
		return errorResult(p.ToolName, err.Error())
	}

	// 2. Check access (with resolved server and bare tool name).
	if ex.authorizer != nil {
		result := ex.authorizer.CheckAccess(p.KeyPrefix, nil, p.APIKeyID, server.ID, tool.Name)
		if !result.Allowed {
			ex.logAudit(p, tool.Name, server.ID, "tools/call", start, 403, false, "access denied")
			return errorResult(p.ToolName, "Access denied by MCP access rules")
		}
	}

	// 3. Security checks (destructive, confirmation, rate limit).
	tc := ex.getToolConfig(p.ToolName)
	if tc != nil {
		if tc.Destructive {
			ex.logAudit(p, tool.Name, server.ID, "tools/call", start, 403, false, "destructive tool blocked")
			return errorResult(p.ToolName, "Tool call blocked: destructive tool not allowed")
		}
		if tc.RequiresConfirmation {
			ex.logAudit(p, tool.Name, server.ID, "tools/call", start, 403, false, "tool requires confirmation")
			return errorResult(p.ToolName, "Tool call blocked: tool requires manual confirmation")
		}
		if ex.isRateLimited(p.ToolName, tc.RateLimitRPM) {
			ex.logAudit(p, tool.Name, server.ID, "tools/call", start, 403, false, "rate limit exceeded")
			return errorResult(p.ToolName, "Tool call blocked: rate limit exceeded")
		}
	}

	// 4. Build the JSON-RPC request.
	params := &CallToolParams{
		Name:      tool.Name,
		Arguments: p.Arguments,
	}

	req := &JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		Method:  "tools/call",
		Params:  *toRawMessage(params),
	}
	id := json.RawMessage(fmt.Sprintf(`"%d"`, time.Now().UnixNano()))
	req.ID = &id

	// 5. Obtain a transport client.
	client, err := ex.clients.GetOrCreate(ctx, server)
	if err != nil {
		ex.logAudit(p, tool.Name, server.ID, "tools/call", start, 500, false, err.Error())
		return errorResult(p.ToolName, fmt.Sprintf("Failed to connect to server %q: %v", server.Config.Name, err))
	}

	// 6. Execute with circuit breaker + retry.
	var resp *JSONRPCResponse
	cb := getBreaker(server.ID)
	timeout := parseDurationOrDefault(server.Config.Timeout, 30*time.Second)
	if tc != nil && tc.TimeoutMs > 0 {
		timeout = time.Duration(tc.TimeoutMs) * time.Millisecond
	}
	maxRetries := server.Config.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 1
	}
	maxRetries++ // at least one attempt

	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	b := backoff.NewExponentialBackOff()
	b.InitialInterval = 100 * time.Millisecond
	b.MaxInterval = 3 * time.Second
	// no MaxElapsedTime (v7 removed it, default 0 is correct — callCtx handles deadline)

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			d := b.NextBackOff()
			mcpLog.Debug("retrying tool call",
				"tool", tool.Name, "server", server.ID, "attempt", attempt+1, "backoff_ms", d.Milliseconds())
			select {
			case <-callCtx.Done():
				resp = nil
				goto done
			case <-time.After(d):
			}
		}

		result, cbErr := cb.Execute(func() (*JSONRPCResponse, error) {
			return client.Call(callCtx, req)
		})

		if cbErr != nil {
			err = cbErr
			continue
		}
		resp = result
		err = nil
		break
	}

done:
	if err != nil {
		ex.logAudit(p, tool.Name, server.ID, "tools/call", start, 500, false, err.Error())
		return errorResult(p.ToolName, fmt.Sprintf("Tool call failed after %d attempt(s): %v", maxRetries, err))
	}

	if resp.Error != nil {
		ex.logAudit(p, tool.Name, server.ID, "tools/call", start, 200, false, resp.Error.Message)
		return &CallToolResult{
			IsError: true,
			Content: []ToolContent{{Type: "text", Text: resp.Error.Message}},
		}
	}

	ex.logAudit(p, tool.Name, server.ID, "tools/call", start, 200, true, "")

	return parseToolResult(resp.Result)
}

func (ex *Executor) logAudit(p *ExecuteToolParams, toolName, serverID, method string, start time.Time, statusCode int, success bool, errMsg string) {
	if ex.auditLog == nil {
		return
	}
	paramsStr := ""
	if p.Arguments != nil {
		if b, err := json.Marshal(p.Arguments); err == nil {
			paramsStr = string(b)
		}
	}
	ex.auditLog.LogAsync(AuditEntry{
		APIKeyID:   p.APIKeyID,
		Tool:       toolName,
		ServerID:   serverID,
		Method:     method,
		Params:     paramsStr,
		DurationMs: float64(time.Since(start).Microseconds()) / 1000.0,
		StatusCode: statusCode,
		Success:    success,
		ErrorMsg:   errMsg,
		ClientIP:   p.ClientIP,
	})
	if MCPToolCallsTotal != nil {
		MCPToolCallsTotal.Add(context.Background(), 1)
	}
}

// Circuit breaker registry
var (
	breakersMu sync.RWMutex
	breakers   = make(map[string]*gobreaker.CircuitBreaker[*JSONRPCResponse])
)

func getBreaker(serverID string) *gobreaker.CircuitBreaker[*JSONRPCResponse] {
	breakersMu.Lock()
	defer breakersMu.Unlock()
	if cb, ok := breakers[serverID]; ok {
		return cb
	}
	cb := gobreaker.NewCircuitBreaker[*JSONRPCResponse](gobreaker.Settings{
		Name:        "mcp-" + serverID,
		MaxRequests: 3,
		Interval:    10 * time.Second,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return counts.Requests >= 5 && failureRatio >= 0.5
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			mcpLog.Debug("circuit breaker state change",
				"name", name, "from", from, "to", to)
		},
	})
	breakers[serverID] = cb
	return cb
}

// Helpers

func toRawMessage(v any) *json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	rm := json.RawMessage(b)
	return &rm
}

func parseToolResult(raw any) *CallToolResult {
	if raw == nil {
		return &CallToolResult{
			Content: []ToolContent{{Type: "text", Text: "ok"}},
		}
	}

	// json.RawMessage is what every external MCP server sends via StdioClient/SSEClient.
	// Unmarshal it as CallToolResult first before falling back to raw text.
	if rawMsg, ok := raw.(json.RawMessage); ok && len(rawMsg) > 0 {
		var r CallToolResult
		if err := json.Unmarshal(rawMsg, &r); err == nil && len(r.Content) > 0 {
			return &r
		}
		// Not a CallToolResult object — use raw text directly (no double-marshal).
		return &CallToolResult{
			Content: []ToolContent{{Type: "text", Text: string(rawMsg)}},
		}
	}

	switch v := raw.(type) {
	case *CallToolResult:
		return v
	case map[string]any:
		b, _ := json.Marshal(v)
		var r CallToolResult
		if err := json.Unmarshal(b, &r); err == nil {
			if len(r.Content) == 0 {
				r.Content = []ToolContent{{Type: "text", Text: "ok"}}
			}
			return &r
		}
	}
	// Fallback to text representation.
	b, _ := json.Marshal(raw)
	return &CallToolResult{
		Content: []ToolContent{{Type: "text", Text: string(b)}},
	}
}

func parseDurationOrDefault(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return def
	}
	return d
}

func errorResult(_, msg string) *CallToolResult {
	return &CallToolResult{
		IsError: true,
		Content: []ToolContent{{Type: "text", Text: msg}},
	}
}
