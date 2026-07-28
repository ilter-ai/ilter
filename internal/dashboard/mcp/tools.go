package dashmcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ilter-ai/ilter/internal/model"

	"github.com/go-chi/chi/v5"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/mcp"
	mcptransport "github.com/ilter-ai/ilter/internal/platform/transport/mcp"
)

var mcpLog = slog.With("component", "mcp")

func (h *MCPHandler) TestServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Server ID is required")
		return
	}

	var name, transport, url, command, args, env, authType, authKeyEnv sql.NullString
	var timeoutMs, maxRetries sql.NullInt64
	err := h.store.DB.QueryRow(`SELECT name, transport, url, command, args, env, timeout_ms, max_retries, auth_type, auth_key_env
		FROM mcp_servers WHERE id = ?`, id).Scan(&name, &transport, &url, &command, &args, &env, &timeoutMs, &maxRetries, &authType, &authKeyEnv)
	if err != nil {
		if err == sql.ErrNoRows {
			model.WriteJSONError(w, http.StatusNotFound, "not_found", "Server not found")
			return
		}
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to query server")
		return
	}

	cfg := config.MCPServerConfig{
		Transport:  transport.String,
		URL:        url.String,
		Command:    command.String,
		AuthType:   authType.String,
		AuthKeyEnv: authKeyEnv.String,
		MaxRetries: int(maxRetries.Int64),
	}

	if args.Valid && args.String != "" {
		var parsedArgs []string
		if err = json.Unmarshal([]byte(args.String), &parsedArgs); err == nil {
			cfg.Args = parsedArgs
		}
	}

	if env.Valid && env.String != "" {
		var parsedEnv map[string]string
		if err = json.Unmarshal([]byte(env.String), &parsedEnv); err == nil {
			cfg.Env = parsedEnv
		}
	}

	timeout := 30 * time.Second
	useOAuthTimeout := 2 * time.Minute
	if timeoutMs.Valid && timeoutMs.Int64 > 0 {
		timeout = time.Duration(timeoutMs.Int64) * time.Millisecond
		useOAuthTimeout = timeout
	}
	cfg.Timeout = timeout.String()

	// Start a built-in OAuth callback server so MCP servers can use ilter's
	// callback URL instead of running their own. The URL is injected as
	// ILTER_OAUTH_CALLBACK_URL in the process environment.
	oauthSrv := mcptransport.NewOAuthCallbackServer("")
	oauthURL, oauthErr := oauthSrv.Start()
	if oauthErr == nil {
		if cfg.Env == nil {
			cfg.Env = make(map[string]string)
		}
		cfg.Env["ILTER_OAUTH_CALLBACK_URL"] = oauthURL
		defer oauthSrv.Stop()
	}

	serverInfo := &mcp.ServerInfo{
		ID:     id,
		Config: cfg,
	}

	client, err := mcp.NewTransportClient(serverInfo)
	if err != nil {
		model.WriteJSON(w, http.StatusOK, map[string]any{
			"status":      "error",
			"tools_count": 0,
			"error":       "Failed to create client: " + err.Error(),
		})
		return
	}

	// Use Background context so the MCP process stays alive after the HTTP handler
	// returns — the OAuth callback server runs inside the MCP process.
	ctx, cancel := context.WithTimeout(context.Background(), useOAuthTimeout)

	type startResult struct{ err error }
	startCh := make(chan startResult, 1)
	go func() {
		startCh <- startResult{client.Start(ctx)}
	}()

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	cleanup := func() { cancel(); client.Close() }
loop:
	for {
		select {
		case res := <-startCh:
			if res.err != nil {
				cleanup()
				stderr := extractStderr(client)
				resp := map[string]any{
					"status":      "error",
					"tools_count": 0,
					"error":       "Connection failed: " + res.err.Error(),
					"stderr":      stderr,
				}
				if u := extractOAuthURL(stderr); u != "" {
					resp["oauth_url"] = u
				}
				model.WriteJSON(w, http.StatusOK, resp)
				return
			}
			break loop
		case <-ticker.C:
			if u := extractOAuthURL(extractStderr(client)); u != "" {
				model.WriteJSON(w, http.StatusOK, map[string]any{
					"status":      "error",
					"tools_count": 0,
					"error":       "OAuth authorization required. Open the link below, authorize, then test again.",
					"oauth_url":   u,
				})
				// Keep client alive — OAuth callback port is in the MCP process.
				// Goroutine with client.Start(ctx) continues; token gets cached
				// after user authorizes. Second test will use cached token.
				return
			}
		case <-ctx.Done():
			cleanup()
			stderr := extractStderr(client)
			resp := map[string]any{
				"status":      "error",
				"tools_count": 0,
				"error":       "Connection timed out: " + ctx.Err().Error(),
				"stderr":      stderr,
			}
			if u := extractOAuthURL(stderr); u != "" {
				resp["oauth_url"] = u
			}
			model.WriteJSON(w, http.StatusOK, resp)
			return
		}
	}
	defer func() { cancel(); client.Close() }()

	initID := json.RawMessage(`"1"`)
	initReq := &mcp.JSONRPCRequest{
		JSONRPC: mcp.JSONRPCVersion,
		ID:      &initID,
		Method:  mcp.MethodInitialize,
		Params:  json.RawMessage(`{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"ilter","version":"1.0"}}`),
	}
	_, err = client.Call(ctx, initReq)
	if err != nil {
		resp := map[string]any{
			"status":      "error",
			"tools_count": 0,
			"error":       "Handshake failed: " + err.Error(),
			"stderr":      extractStderr(client),
		}
		if u := extractOAuthURL(extractStderr(client)); u != "" {
			resp["oauth_url"] = u
		}
		model.WriteJSON(w, http.StatusOK, resp)
		return
	}

	listID := json.RawMessage(`"2"`)
	listReq := &mcp.JSONRPCRequest{
		JSONRPC: mcp.JSONRPCVersion,
		ID:      &listID,
		Method:  mcp.MethodToolsList,
	}
	listResp, err := client.Call(ctx, listReq)
	if err != nil {
		model.WriteJSON(w, http.StatusOK, map[string]any{
			"status":      "error",
			"tools_count": 0,
			"error":       "tools/list failed: " + err.Error(),
			"stderr":      extractStderr(client),
		})
		return
	}

	if listResp.Error != nil {
		model.WriteJSON(w, http.StatusOK, map[string]any{
			"status":      "error",
			"tools_count": 0,
			"error":       fmt.Sprintf("Server error: %s (code %d)", listResp.Error.Message, listResp.Error.Code),
			"stderr":      extractStderr(client),
		})
		return
	}

	var listResult mcp.ListToolsResult
	if err = json.Unmarshal(listResp.Result, &listResult); err != nil {
		model.WriteJSON(w, http.StatusOK, map[string]any{
			"status":      "error",
			"tools_count": 0,
			"error":       "Failed to parse tools list: " + err.Error(),
			"stderr":      extractStderr(client),
		})
		return
	}

	tx, err := h.store.DB.Begin()
	if err == nil {
		if _, e := tx.Exec("DELETE FROM mcp_tools WHERE server_id = ?", id); e != nil {
			mcpLog.Warn("failed to clear old tools during sync", "server_id", id, "error", e)
			_ = tx.Rollback()
		} else {
			for _, tool := range listResult.Tools {
				toolID := tool.Name + "-" + id
				if _, e := tx.Exec(`INSERT OR REPLACE INTO mcp_tools (id, server_id, name, description, schema) VALUES (?, ?, ?, ?, ?)`,
					toolID, id, tool.Name, tool.Description, string(tool.InputSchema)); e != nil {
					mcpLog.Warn("failed to insert tool during sync", "server_id", id, "tool", tool.Name, "error", e)
					_ = tx.Rollback()
					break
				}
			}
			_ = tx.Commit()
		}
	}

	if _, err := h.store.DB.Exec("UPDATE mcp_servers SET updated_at = datetime('now') WHERE id = ?", id); err != nil {
		mcpLog.Warn("failed to update server timestamp", "server_id", id, "error", err)
	}

	if h.registry != nil {
		if err := h.registry.SyncTools(id, listResult.Tools); err != nil {
			mcpLog.Warn("failed to sync tools to registry after test",
				"server_id", id, "error", err)
		}
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"status":      "online",
		"tools_count": len(listResult.Tools),
	})
}

func (h *MCPHandler) ListServerTools(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Server ID is required")
		return
	}

	rows, err := h.store.DB.Query(`SELECT id, name, description, schema, created_at
		FROM mcp_tools WHERE server_id = ? ORDER BY name`, id)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list tools")
		return
	}
	defer rows.Close()

	tools := make([]map[string]any, 0)
	for rows.Next() {
		var id, name, description, inputSchema, createdAt sql.NullString
		if err := rows.Scan(&id, &name, &description, &inputSchema, &createdAt); err != nil {
			continue
		}
		tools = append(tools, map[string]any{
			"id":           nullToEmpty(id),
			"name":         nullToEmpty(name),
			"description":  nullToEmpty(description),
			"input_schema": nullToEmpty(inputSchema),
			"created_at":   nullToEmpty(createdAt),
		})
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"server_id": id,
		"tools":     tools,
	})
}

func (h *MCPHandler) CallServerTool(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Server ID is required")
		return
	}

	var req struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}
	if req.Name == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Tool name is required")
		return
	}

	var name, transport, url, command, args, env, authType, authKeyEnv sql.NullString
	var timeoutMs, maxRetries sql.NullInt64
	err := h.store.DB.QueryRow(`SELECT name, transport, url, command, args, env, timeout_ms, max_retries, auth_type, auth_key_env
		FROM mcp_servers WHERE id = ?`, id).Scan(&name, &transport, &url, &command, &args, &env, &timeoutMs, &maxRetries, &authType, &authKeyEnv)
	if err != nil {
		if err == sql.ErrNoRows {
			model.WriteJSONError(w, http.StatusNotFound, "not_found", "Server not found")
			return
		}
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to query server")
		return
	}

	cfg := config.MCPServerConfig{
		Transport:  transport.String,
		URL:        url.String,
		Command:    command.String,
		AuthType:   authType.String,
		AuthKeyEnv: authKeyEnv.String,
		MaxRetries: int(maxRetries.Int64),
	}
	if args.Valid && args.String != "" {
		var parsedArgs []string
		if err = json.Unmarshal([]byte(args.String), &parsedArgs); err == nil {
			cfg.Args = parsedArgs
		}
	}
	if env.Valid && env.String != "" {
		var parsedEnv map[string]string
		if err = json.Unmarshal([]byte(env.String), &parsedEnv); err == nil {
			cfg.Env = parsedEnv
		}
	}

	timeout := 30 * time.Second
	if timeoutMs.Valid && timeoutMs.Int64 > 0 {
		timeout = time.Duration(timeoutMs.Int64) * time.Millisecond
	}
	cfg.Timeout = timeout.String()

	serverInfo := &mcp.ServerInfo{
		ID:     id,
		Config: cfg,
	}

	client, err := mcp.NewTransportClient(serverInfo)
	if err != nil {
		model.WriteJSON(w, http.StatusOK, map[string]any{
			"isError": true,
			"content": []map[string]any{{"type": "text", "text": "Failed to create client: " + err.Error()}},
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	if err = client.Start(ctx); err != nil {
		client.Close()
		model.WriteJSON(w, http.StatusOK, map[string]any{
			"isError": true,
			"content": []map[string]any{{"type": "text", "text": "Connection failed: " + err.Error()}},
			"stderr":  extractStderr(client),
		})
		return
	}
	defer client.Close()

	initID := json.RawMessage(`"1"`)
	initReq := &mcp.JSONRPCRequest{
		JSONRPC: mcp.JSONRPCVersion,
		ID:      &initID,
		Method:  mcp.MethodInitialize,
		Params:  json.RawMessage(`{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"ilter","version":"1.0"}}`),
	}
	_, err = client.Call(ctx, initReq)
	if err != nil {
		model.WriteJSON(w, http.StatusOK, map[string]any{
			"isError": true,
			"content": []map[string]any{{"type": "text", "text": "Handshake failed: " + err.Error()}},
		})
		return
	}

	callParams := mcp.CallToolParams{
		Name:      req.Name,
		Arguments: req.Arguments,
	}
	callBody, _ := json.Marshal(callParams)
	callID := json.RawMessage(`"2"`)
	callReq := &mcp.JSONRPCRequest{
		JSONRPC: mcp.JSONRPCVersion,
		ID:      &callID,
		Method:  mcp.MethodToolsCall,
		Params:  callBody,
	}
	resp, err := client.Call(ctx, callReq)
	if err != nil {
		model.WriteJSON(w, http.StatusOK, map[string]any{
			"isError": true,
			"content": []map[string]any{{"type": "text", "text": "Tool call failed: " + err.Error()}},
		})
		return
	}

	if resp.Error != nil {
		model.WriteJSON(w, http.StatusOK, map[string]any{
			"isError": true,
			"content": []map[string]any{{"type": "text", "text": resp.Error.Message}},
		})
		return
	}

	var result mcp.CallToolResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		model.WriteJSON(w, http.StatusOK, map[string]any{
			"isError": false,
			"content": []map[string]any{{"type": "text", "text": string(resp.Result)}},
		})
		return
	}

	content := make([]map[string]any, len(result.Content))
	for i, c := range result.Content {
		item := map[string]any{"type": c.Type}
		if c.Text != "" {
			item["text"] = c.Text
		}
		if c.Data != "" {
			item["data"] = c.Data
		}
		if c.MIMEType != "" {
			item["mimeType"] = c.MIMEType
		}
		if c.URI != "" {
			item["uri"] = c.URI
		}
		content[i] = item
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"isError": result.IsError,
		"content": content,
	})
}

func (h *MCPHandler) syncServerByID(ctx context.Context, id string) error {
	var name, transport, url, command, args, env, authType, authKeyEnv sql.NullString
	var timeoutMs, maxRetries sql.NullInt64
	err := h.store.DB.QueryRowContext(ctx, `SELECT name, transport, url, command, args, env, auth_type, auth_key_env, timeout_ms, max_retries
		FROM mcp_servers WHERE id = ?`, id).Scan(&name, &transport, &url, &command, &args, &env, &authType, &authKeyEnv, &timeoutMs, &maxRetries)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("server %q not found", id)
		}
		return fmt.Errorf("failed to query server: %w", err)
	}

	syncTimeout := 30 * time.Second
	if timeoutMs.Valid && timeoutMs.Int64 > 0 {
		syncTimeout = time.Duration(timeoutMs.Int64) * time.Millisecond
	}

	cfg := config.MCPServerConfig{
		Transport:  transport.String,
		URL:        url.String,
		Command:    command.String,
		AuthType:   authType.String,
		AuthKeyEnv: authKeyEnv.String,
		Timeout:    syncTimeout.String(),
		MaxRetries: int(maxRetries.Int64),
	}

	if args.Valid && args.String != "" {
		var parsedArgs []string
		if err = json.Unmarshal([]byte(args.String), &parsedArgs); err == nil {
			cfg.Args = parsedArgs
		}
	}

	if env.Valid && env.String != "" {
		var parsedEnv map[string]string
		if err = json.Unmarshal([]byte(env.String), &parsedEnv); err == nil {
			cfg.Env = parsedEnv
		}
	}

	serverInfo := &mcp.ServerInfo{
		ID:     id,
		Config: cfg,
	}

	client, err := mcp.NewTransportClient(serverInfo)
	if err != nil {
		return fmt.Errorf("Failed to connect: %w", err)
	}
	syncCtx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()

	if err = client.Start(syncCtx); err != nil {
		client.Close()
		return fmt.Errorf("Failed to connect to MCP server: %w", err)
	}
	defer client.Close()

	initID := json.RawMessage(`"1"`)
	initReq := &mcp.JSONRPCRequest{
		JSONRPC: mcp.JSONRPCVersion,
		ID:      &initID,
		Method:  mcp.MethodInitialize,
		Params:  json.RawMessage(`{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"ilter","version":"1.0"}}`),
	}
	initResp, err := client.Call(syncCtx, initReq)
	if err != nil {
		return fmt.Errorf("Failed to initialize: %w", err)
	}
	if initResp.Error != nil {
		return fmt.Errorf("Failed to initialize: %s (code %d)", initResp.Error.Message, initResp.Error.Code)
	}

	listID := json.RawMessage(`"2"`)
	listReq := &mcp.JSONRPCRequest{
		JSONRPC: mcp.JSONRPCVersion,
		ID:      &listID,
		Method:  mcp.MethodToolsList,
	}
	listResp, err := client.Call(syncCtx, listReq)
	if err != nil {
		return fmt.Errorf("Failed to call tools: %w", err)
	}
	if listResp.Error != nil {
		return fmt.Errorf("Failed to call tools: %s (code %d)", listResp.Error.Message, listResp.Error.Code)
	}

	var listResult mcp.ListToolsResult
	if err = json.Unmarshal(listResp.Result, &listResult); err != nil {
		return fmt.Errorf("Failed to parse tools list: %w", err)
	}

	tx, err := h.store.DB.BeginTx(syncCtx, nil)
	if err != nil {
		return fmt.Errorf("Failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(syncCtx, "DELETE FROM mcp_tools WHERE server_id = ?", id); err != nil {
		return fmt.Errorf("Failed to clear old tools: %w", err)
	}

	for _, tool := range listResult.Tools {
		toolID := tool.Name + "-" + id
		_, err = tx.ExecContext(
			syncCtx,
			`INSERT OR REPLACE INTO mcp_tools (id, server_id, name, description, schema)
			 VALUES (?, ?, ?, ?, ?)`,
			toolID, id, tool.Name, tool.Description, string(tool.InputSchema),
		)
		if err != nil {
			return fmt.Errorf("Failed to insert tool: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("Failed to commit transaction: %w", err)
	}

	if h.registry != nil {
		if err := h.registry.SyncTools(id, listResult.Tools); err != nil {
			mcpLog.Warn("failed to sync tools to registry after sync",
				"server_id", id, "error", err)
		}
	}

	mcpLog.Info("synced MCP server",
		"server_id", id, "server_name", name.String, "tool_count", len(listResult.Tools))
	return nil
}

func (h *MCPHandler) SyncServerTools(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Server ID is required")
		return
	}

	if err := h.syncServerByID(r.Context(), id); err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "not found") {
			model.WriteJSONError(w, http.StatusNotFound, "not_found", errMsg)
		} else if strings.Contains(errMsg, "Failed to connect") || strings.Contains(errMsg, "Failed to initialize") || strings.Contains(errMsg, "Failed to call tools") {
			model.WriteJSONError(w, http.StatusBadGateway, "connection_error", errMsg)
		} else {
			model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", errMsg)
		}
		return
	}

	var name string
	_ = h.store.DB.QueryRowContext(r.Context(), "SELECT name FROM mcp_servers WHERE id = ?", id).Scan(&name)

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"message": fmt.Sprintf("Synced MCP server %q", name),
	})
}
