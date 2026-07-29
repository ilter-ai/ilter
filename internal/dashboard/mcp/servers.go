package dashmcp

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/ilter-ai/ilter/internal/model"

	"github.com/go-chi/chi/v5"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

func (h *MCPHandler) ListServers(w http.ResponseWriter, _ *http.Request) {
	rows, err := h.store.DB.Query(`SELECT id, name, description, transport, url, command, args, env,
		handler, enabled, timeout_ms, max_retries, auth_type, auth_key_env, created_at, updated_at,
		protocol_version,
		(SELECT COUNT(*) FROM mcp_tools WHERE server_id = mcp_servers.id) AS tools_count
		FROM mcp_servers ORDER BY name`)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list servers")
		return
	}
	defer rows.Close()

	servers := make([]map[string]any, 0)
	for rows.Next() {
		var id, name, description, transport, url, command, args, env, handler, authType, authKeyEnv, createdAt, updatedAt, protocolVersion sql.NullString
		var enabled, timeoutMs, maxRetries, toolsCount sql.NullInt64

		if err := rows.Scan(&id, &name, &description, &transport, &url, &command, &args, &env,
			&handler, &enabled, &timeoutMs, &maxRetries, &authType, &authKeyEnv, &createdAt, &updatedAt,
			&protocolVersion, &toolsCount); err != nil {
			continue
		}

		srvStatus := "online"
		if enabled.Int64 != 1 {
			srvStatus = "offline"
		}
		srv := map[string]any{
			"id":                nullToEmpty(id),
			"name":              nullToEmpty(name),
			"description":       nullToEmpty(description),
			"transport":         nullToEmpty(transport),
			"url":               nullToEmpty(url),
			"command":           nullToEmpty(command),
			"args":              nullToEmpty(args),
			"env":               nullToEmpty(env),
			"handler":           nullToEmpty(handler),
			"enabled":           enabled.Int64 == 1,
			"status":            srvStatus,
			"last_health_check": "",
			"timeout_ms":        timeoutMs.Int64,
			"max_retries":       maxRetries.Int64,
			"auth_type":         nullToEmpty(authType),
			"auth_key_env":      nullToEmpty(authKeyEnv),
			"created_at":        nullToEmpty(createdAt),
			"updated_at":        nullToEmpty(updatedAt),
			"tools_count":       toolsCount.Int64,
			"protocol_version":  nullToEmptyOr(protocolVersion, "auto"),
		}
		servers = append(servers, srv)
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"servers": servers,
	})
}

func (h *MCPHandler) GetServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "id is required")
		return
	}

	var name, description, transport, url, command, args, env, handler, authType, authKeyEnv, createdAt, updatedAt, protocolVersion sql.NullString
	var enabled, timeoutMs, maxRetries, toolsCount sql.NullInt64

	err := h.store.DB.QueryRow(`SELECT id, name, description, transport, url, command, args, env,
		handler, enabled, timeout_ms, max_retries, auth_type, auth_key_env, created_at, updated_at,
		protocol_version,
		(SELECT COUNT(*) FROM mcp_tools WHERE server_id = mcp_servers.id) AS tools_count
		FROM mcp_servers WHERE id = ?`, id).Scan(&id, &name, &description, &transport, &url, &command, &args, &env,
		&handler, &enabled, &timeoutMs, &maxRetries, &authType, &authKeyEnv, &createdAt, &updatedAt,
		&protocolVersion, &toolsCount)
	if err != nil {
		if err == sql.ErrNoRows {
			model.WriteJSONError(w, http.StatusNotFound, "not_found", "Server not found")
			return
		}
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to fetch server")
		return
	}

	srvStatus := "online"
	if enabled.Int64 != 1 {
		srvStatus = "offline"
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"id":                id,
		"name":              nullToEmpty(name),
		"description":       nullToEmpty(description),
		"transport":         nullToEmpty(transport),
		"url":               nullToEmpty(url),
		"command":           nullToEmpty(command),
		"args":              nullToEmpty(args),
		"env":               nullToEmpty(env),
		"handler":           nullToEmpty(handler),
		"enabled":           enabled.Int64 == 1,
		"status":            srvStatus,
		"last_health_check": "",
		"timeout_ms":        timeoutMs.Int64,
		"max_retries":       maxRetries.Int64,
		"auth_type":         nullToEmpty(authType),
		"auth_key_env":      nullToEmpty(authKeyEnv),
		"created_at":        nullToEmpty(createdAt),
		"updated_at":        nullToEmpty(updatedAt),
		"tools_count":       toolsCount.Int64,
		"protocol_version":  nullToEmptyOr(protocolVersion, "auto"),
	})
}

func (h *MCPHandler) CreateServer(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req struct {
		ID              string `json:"id"`
		Name            string `json:"name"`
		Description     string `json:"description"`
		Transport       string `json:"transport"`
		URL             string `json:"url,omitempty"`
		Command         string `json:"command,omitempty"`
		Args            string `json:"args,omitempty"`
		Env             string `json:"env,omitempty"`
		Handler         string `json:"handler,omitempty"`
		Enabled         bool   `json:"enabled"`
		TimeoutMs       int    `json:"timeout_ms"`
		MaxRetries      int    `json:"max_retries"`
		AuthType        string `json:"auth_type"`
		AuthKeyEnv      string `json:"auth_key_env"`
		ProtocolVersion string `json:"protocol_version,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}

	if req.Name == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "name is required")
		return
	}

	// Auto-generate ID from name when not provided (marketplace installs, etc.)
	if req.ID == "" {
		req.ID = slugify(req.Name)
	}

	if req.Transport == "" {
		req.Transport = "sse"
	}
	if req.TimeoutMs == 0 {
		req.TimeoutMs = 30000
	}
	if req.ProtocolVersion == "" {
		req.ProtocolVersion = "auto"
	}

	_, err := h.store.DB.Exec(`INSERT INTO mcp_servers
		(id, name, description, transport, url, command, args, env, handler, enabled, timeout_ms, max_retries, auth_type, auth_key_env, protocol_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		req.ID, req.Name, req.Description, req.Transport, req.URL, req.Command,
		req.Args, req.Env, req.Handler, boolToInt(req.Enabled), req.TimeoutMs,
		req.MaxRetries, req.AuthType, req.AuthKeyEnv, req.ProtocolVersion)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			model.WriteJSONError(w, http.StatusConflict, "duplicate_server", "Server ID already exists")
			return
		}
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to create server: "+err.Error())
		return
	}

	if h.registry != nil {
		cfg := mcpServerConfigFromRequest(req)
		h.registry.RegisterServer(req.ID, cfg, nil)
	}

	if h.configAuditor != nil {
		vals := map[string]any{
			"name":        req.Name,
			"description": req.Description,
			"transport":   req.Transport,
			"url":         req.URL,
			"command":     req.Command,
			"enabled":     req.Enabled,
			"timeout_ms":  req.TimeoutMs,
			"max_retries": req.MaxRetries,
			"auth_type":   req.AuthType,
		}
		if req.AuthKeyEnv != "" {
			vals["auth_key_env"] = "***"
		}
		if err := h.configAuditor.LogCreate("mcp_server", req.ID, vals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Error("failed to log audit create mcp_server", "error", err)
		}
	}

	model.WriteJSON(w, http.StatusCreated, map[string]any{
		"status": "ok",
		"id":     req.ID,
	})
}

func (h *MCPHandler) UpdateServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Server ID is required")
		return
	}

	defer r.Body.Close()
	var req struct {
		Name            string `json:"name"`
		Description     string `json:"description"`
		Transport       string `json:"transport"`
		URL             string `json:"url,omitempty"`
		Command         string `json:"command,omitempty"`
		Args            string `json:"args,omitempty"`
		Env             string `json:"env,omitempty"`
		Handler         string `json:"handler,omitempty"`
		Enabled         *bool  `json:"enabled,omitempty"`
		TimeoutMs       *int   `json:"timeout_ms,omitempty"`
		MaxRetries      *int   `json:"max_retries,omitempty"`
		AuthType        string `json:"auth_type,omitempty"`
		AuthKeyEnv      string `json:"auth_key_env,omitempty"`
		ProtocolVersion string `json:"protocol_version,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}

	// Capture old values for audit
	var oldName, oldDesc, oldTransport, oldURL, oldCmd, oldArgs, oldEnv, oldHandler, oldAuthType, oldAuthKeyEnv string
	var oldEnabled bool
	var oldTimeoutMs, oldMaxRetries int
	if err := h.store.DB.QueryRow(
		`SELECT name, description, transport, url, command, args, env, handler,
		       enabled, timeout_ms, max_retries, auth_type, auth_key_env
		 FROM mcp_servers WHERE id = ?`, id,
	).Scan(&oldName, &oldDesc, &oldTransport, &oldURL, &oldCmd, &oldArgs, &oldEnv, &oldHandler,
		&oldEnabled, &oldTimeoutMs, &oldMaxRetries, &oldAuthType, &oldAuthKeyEnv); err != nil {
		slog.Warn("Failed to read old server values for audit", "server_id", id, "error", err)
	}

	sets := []string{}
	args := []any{}

	if req.Name != "" {
		sets = append(sets, "name = ?")
		args = append(args, req.Name)
	}
	if req.Transport != "" {
		sets = append(sets, "transport = ?")
		args = append(args, req.Transport)
	}
	sets = append(sets, "description = ?")
	args = append(args, req.Description)
	if req.URL != "" {
		sets = append(sets, "url = ?")
		args = append(args, req.URL)
	}
	if req.Command != "" {
		sets = append(sets, "command = ?")
		args = append(args, req.Command)
	}
	if req.Args != "" {
		sets = append(sets, "args = ?")
		args = append(args, req.Args)
	}
	if req.Env != "" {
		sets = append(sets, "env = ?")
		args = append(args, req.Env)
	}
	if req.Handler != "" {
		sets = append(sets, "handler = ?")
		args = append(args, req.Handler)
	}

	if req.Enabled != nil {
		sets = append(sets, "enabled = ?")
		args = append(args, boolToInt(*req.Enabled))
	}
	if req.TimeoutMs != nil {
		sets = append(sets, "timeout_ms = ?")
		args = append(args, *req.TimeoutMs)
	}
	if req.MaxRetries != nil {
		sets = append(sets, "max_retries = ?")
		args = append(args, *req.MaxRetries)
	}
	if req.AuthType != "" {
		sets = append(sets, "auth_type = ?")
		args = append(args, req.AuthType)
	}
	if req.AuthKeyEnv != "" {
		sets = append(sets, "auth_key_env = ?")
		args = append(args, req.AuthKeyEnv)
	}
	if req.ProtocolVersion != "" {
		sets = append(sets, "protocol_version = ?")
		args = append(args, req.ProtocolVersion)
	}

	sets = append(sets, "updated_at = datetime('now')")
	args = append(args, id)

	sql := "UPDATE mcp_servers SET " + strings.Join(sets, ", ") + " WHERE id = ?"
	res, err := h.store.DB.Exec(sql, args...)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to update server: "+err.Error())
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Server not found")
		return
	}

	if h.registry != nil {
		if cfg := readServerConfigFromDB(h.store, id); cfg != nil {
			h.registry.RegisterServer(id, *cfg, nil)
		}
	}

	if h.configAuditor != nil {
		oldVals := auditServerVals(oldName, oldDesc, oldTransport, oldURL, oldCmd, oldEnabled, oldTimeoutMs, oldMaxRetries, oldAuthType, oldAuthKeyEnv)
		newVals := auditServerVals(req.Name, req.Description, req.Transport, req.URL, req.Command,
			(req.Enabled != nil && *req.Enabled) || (req.Enabled == nil && oldEnabled),
			timeoutOrDefault(req.TimeoutMs, oldTimeoutMs),
			maxRetriesOrDefault(req.MaxRetries, oldMaxRetries),
			req.AuthType, req.AuthKeyEnv)
		if err := h.configAuditor.LogUpdate("mcp_server", id, oldVals, newVals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Error("failed to log audit update mcp_server", "error", err)
		}
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (h *MCPHandler) ToggleServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Server ID is required")
		return
	}

	res, err := h.store.DB.Exec(
		"UPDATE mcp_servers SET enabled = CASE WHEN enabled = 1 THEN 0 ELSE 1 END, updated_at = datetime('now') WHERE id = ?",
		id,
	)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to toggle server")
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Server not found")
		return
	}

	var enabled bool
	err = h.store.DB.QueryRow("SELECT enabled FROM mcp_servers WHERE id = ?", id).Scan(&enabled)
	if err != nil {
		model.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
		return
	}

	if h.registry != nil {
		if cfg := readServerConfigFromDB(h.store, id); cfg != nil {
			h.registry.RegisterServer(id, *cfg, nil)
		}
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{"enabled": enabled})
}

func (h *MCPHandler) DeleteServer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Server ID is required")
		return
	}

	var oldName, oldDesc, oldTransport, oldURL, oldCmd, oldAuthType, oldAuthKeyEnv string
	var oldEnabled bool
	var oldTimeoutMs, oldMaxRetries int
	fetchErr := h.store.DB.QueryRow(
		`SELECT name, description, transport, url, command, enabled, timeout_ms, max_retries, auth_type, auth_key_env
		 FROM mcp_servers WHERE id = ?`, id,
	).Scan(&oldName, &oldDesc, &oldTransport, &oldURL, &oldCmd, &oldEnabled, &oldTimeoutMs, &oldMaxRetries, &oldAuthType, &oldAuthKeyEnv)

	res, err := h.store.DB.Exec("DELETE FROM mcp_servers WHERE id = ?", id)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to delete server")
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Server not found")
		return
	}

	if h.registry != nil {
		h.registry.UnregisterServer(id)
	}

	if h.configAuditor != nil && fetchErr == nil {
		vals := auditServerVals(oldName, oldDesc, oldTransport, oldURL, oldCmd, oldEnabled, oldTimeoutMs, oldMaxRetries, oldAuthType, oldAuthKeyEnv)
		if err := h.configAuditor.LogDelete("mcp_server", id, vals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Error("failed to log audit delete mcp_server", "error", err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

func mcpServerConfigFromRequest(req struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	Transport       string `json:"transport"`
	URL             string `json:"url,omitempty"`
	Command         string `json:"command,omitempty"`
	Args            string `json:"args,omitempty"`
	Env             string `json:"env,omitempty"`
	Handler         string `json:"handler,omitempty"`
	Enabled         bool   `json:"enabled"`
	TimeoutMs       int    `json:"timeout_ms"`
	MaxRetries      int    `json:"max_retries"`
	AuthType        string `json:"auth_type"`
	AuthKeyEnv      string `json:"auth_key_env"`
	ProtocolVersion string `json:"protocol_version,omitempty"`
},
) config.MCPServerConfig {
	timeout := "30s"
	if req.TimeoutMs > 0 {
		timeout = fmt.Sprintf("%dms", req.TimeoutMs)
	}
	protocolVersion := req.ProtocolVersion
	if protocolVersion == "" {
		protocolVersion = "auto"
	}
	cfg := config.MCPServerConfig{
		ID:              req.ID,
		Name:            req.Name,
		Description:     req.Description,
		Transport:       req.Transport,
		URL:             req.URL,
		Command:         req.Command,
		Handler:         req.Handler,
		Enabled:         req.Enabled,
		Timeout:         timeout,
		MaxRetries:      req.MaxRetries,
		AuthType:        req.AuthType,
		AuthKeyEnv:      req.AuthKeyEnv,
		ProtocolVersion: protocolVersion,
	}
	if req.Args != "" {
		if err := json.Unmarshal([]byte(req.Args), &cfg.Args); err != nil {
			slog.Warn("Failed to parse server args JSON", "error", err)
		}
	}
	if req.Env != "" {
		if err := json.Unmarshal([]byte(req.Env), &cfg.Env); err != nil {
			slog.Warn("Failed to parse server env JSON", "error", err)
		}
	}
	return cfg
}

func readServerConfigFromDB(store *db.SQLiteStore, id string) *config.MCPServerConfig {
	var name, desc, transport, url, command, args, env, handler, authType, authKeyEnv, protocolVersion sql.NullString
	var enabled, timeoutMs, maxRetries sql.NullInt64

	err := store.DB.QueryRow(
		`SELECT id, name, description, transport, url, command, args, env,
		       handler, enabled, timeout_ms, max_retries, auth_type, auth_key_env, protocol_version
		 FROM mcp_servers WHERE id = ?`, id,
	).Scan(&id, &name, &desc, &transport, &url, &command,
		&args, &env, &handler, &enabled, &timeoutMs, &maxRetries,
		&authType, &authKeyEnv, &protocolVersion)
	if err != nil {
		slog.Warn("Failed to read server from DB for registry sync", "server_id", id, "error", err)
		return nil
	}

	timeout := "30s"
	if timeoutMs.Int64 > 0 {
		timeout = fmt.Sprintf("%dms", timeoutMs.Int64)
	}

	cfg := &config.MCPServerConfig{
		ID:              id,
		Name:            nullToEmpty(name),
		Description:     nullToEmpty(desc),
		Transport:       nullToEmpty(transport),
		URL:             nullToEmpty(url),
		Command:         nullToEmpty(command),
		Handler:         nullToEmpty(handler),
		Enabled:         enabled.Int64 == 1,
		Timeout:         timeout,
		MaxRetries:      int(maxRetries.Int64),
		AuthType:        nullToEmpty(authType),
		AuthKeyEnv:      nullToEmpty(authKeyEnv),
		ProtocolVersion: nullToEmptyOr(protocolVersion, "auto"),
	}

	if argsStr := nullToEmpty(args); argsStr != "" {
		if err := json.Unmarshal([]byte(argsStr), &cfg.Args); err != nil {
			slog.Warn("Failed to parse args JSON from DB", "error", err)
		}
	}
	if envStr := nullToEmpty(env); envStr != "" {
		if err := json.Unmarshal([]byte(envStr), &cfg.Env); err != nil {
			slog.Warn("Failed to parse env JSON from DB", "error", err)
		}
	}

	return cfg
}

func auditServerVals(name, desc, transport, url, cmd string, enabled bool, timeoutMs, maxRetries int, authType, authKeyEnv string) map[string]any {
	v := map[string]any{
		"name":        name,
		"description": desc,
		"transport":   transport,
		"url":         url,
		"command":     cmd,
		"enabled":     enabled,
		"timeout_ms":  timeoutMs,
		"max_retries": maxRetries,
		"auth_type":   authType,
	}
	if authKeyEnv != "" {
		v["auth_key_env"] = "***"
	}
	return v
}

func timeoutOrDefault(req *int, fallback int) int {
	if req != nil {
		return *req
	}
	return fallback
}

func maxRetriesOrDefault(req *int, fallback int) int {
	if req != nil {
		return *req
	}
	return fallback
}
