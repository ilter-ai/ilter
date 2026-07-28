package mcp

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
)

// ToolInfo associates a tool definition with the server that provides it.
type ToolInfo struct {
	Tool     ToolDefinition
	ServerID string
}

// ServerInfo holds runtime state for a single MCP server registration.
type ServerInfo struct {
	ID       string
	Config   config.MCPServerConfig
	Tools    []ToolDefinition
	Healthy  bool
	LastSync time.Time
}

// Registry manages discovered and configured MCP servers and their tools.
type Registry struct {
	mu      sync.RWMutex
	servers map[string]*ServerInfo // server ID → info

	store *db.SQLiteStore
}

// NewRegistryFromCache creates a Registry populated from the given server
// configs (sourced from ConfigCache) and loads supplemental servers and tools
// from the database.
func NewRegistryFromCache(servers []config.MCPServerConfig, store *db.SQLiteStore) (*Registry, error) {
	r := &Registry{
		servers: make(map[string]*ServerInfo, len(servers)+4),
		store:   store,
	}

	for _, sc := range servers {
		if !sc.Enabled {
			continue
		}
		r.servers[sc.ID] = &ServerInfo{
			ID:     sc.ID,
			Config: sc,
		}
	}

	// Load supplemental servers from the mcp_servers table (admin CRUD).
	if err := r.loadServersFromDB(); err != nil {
		mcpLog.Warn("failed to load servers from database", "error", err)
	}

	for id := range r.servers {
		if err := r.loadToolsFromDB(id); err != nil {
			mcpLog.Warn("failed to load tools for server", "server_id", id, "error", err)
		}
	}

	r.mu.RLock()
	serverCount := len(r.servers)
	toolCount := 0
	for _, s := range r.servers {
		toolCount += len(s.Tools)
	}
	r.mu.RUnlock()
	slog.Info(
		"registry initialized from cache",
		"servers", serverCount,
		"tools", toolCount,
	)

	return r, nil
}

// InitFromCache reloads the registry from a ConfigCache servers list.
// It clears existing servers, loads the new list, then merges DB records.
// Safe for concurrent use (acquires write lock).
func (r *Registry) InitFromCache(servers []config.MCPServerConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.servers = make(map[string]*ServerInfo, len(servers)+4)
	for _, sc := range servers {
		if !sc.Enabled {
			continue
		}
		r.servers[sc.ID] = &ServerInfo{
			ID:     sc.ID,
			Config: sc,
		}
	}

	if err := r.loadServersFromDB(); err != nil {
		mcpLog.Warn("failed to load servers from database on refresh", "error", err)
	}

	for id := range r.servers {
		if err := r.loadToolsFromDB(id); err != nil {
			mcpLog.Warn("failed to load tools for server on refresh", "server_id", id, "error", err)
		}
	}

	serverCount := len(r.servers)
	toolCount := 0
	for _, s := range r.servers {
		toolCount += len(s.Tools)
	}
	slog.Info(
		"MCP registry reloaded from cache",
		"servers", serverCount,
		"tools", toolCount,
	)
	return nil
}

// ListServers returns all registered servers (read-only snapshot).
func (r *Registry) ListServers() []*ServerInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*ServerInfo, 0, len(r.servers))
	for _, s := range r.servers {
		out = append(out, s)
	}
	return out
}

// ListTools returns all tools across all servers.
func (r *Registry) ListTools() []ToolInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []ToolInfo
	for id, s := range r.servers {
		for _, t := range s.Tools {
			out = append(out, ToolInfo{Tool: t, ServerID: id})
		}
	}
	return out
}

// ResolveTool looks up a tool by name and returns the tool info along with its server.
// Supports both bare names (search all servers) and namespaced "server__toolname" format.
// If a tool name exists on multiple servers (conflict), it MUST be called with the namespaced form.
func (r *Registry) ResolveTool(name string) (*ToolDefinition, *ServerInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	conflicts := r.conflictingToolNamesUnlocked()

	// 1. Try namespaced form first (server__toolname) - always works.
	for _, s := range r.servers {
		for _, t := range s.Tools {
			if SanitizeToolName(s.ID, t.Name) == name {
				return &t, s, nil
			}
		}
	}

	// 2. Bare name lookup.
	// If this bare name has conflicts, reject - must use namespaced form.
	if conflicts[name] {
		return nil, nil, fmt.Errorf("tool %q is ambiguous (exists on multiple servers); use namespaced form server__%s", name, name)
	}

	// No conflicts - bare name is fine.
	var found *ToolDefinition
	var foundServer *ServerInfo
	for _, s := range r.servers {
		for _, t := range s.Tools {
			if t.Name == name {
				found, foundServer = &t, s
				break
			}
		}
		if found != nil {
			break
		}
	}
	if found != nil {
		return found, foundServer, nil
	}
	return nil, nil, fmt.Errorf("tool %q not found in any server", name)
}

// conflictingToolNamesUnlocked returns the set of bare tool names that exist on multiple servers.
// Must be called with r.mu held.
func (r *Registry) conflictingToolNamesUnlocked() map[string]bool {
	counts := make(map[string]int)
	for _, s := range r.servers {
		for _, t := range s.Tools {
			counts[t.Name]++
		}
	}
	conflicts := make(map[string]bool, len(counts))
	for name, cnt := range counts {
		if cnt > 1 {
			conflicts[name] = true
		}
	}
	return conflicts
}

// RegisterServer adds or updates a server in the in-memory registry.
func (r *Registry) RegisterServer(id string, cfg config.MCPServerConfig, tools []ToolDefinition) {
	if err := ValidateMCPServerID(id); err != nil {
		mcpLog.Error("server registration rejected", "server_id", id, "error", err)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.servers[id] = &ServerInfo{
		ID:      id,
		Config:  cfg,
		Tools:   tools,
		Healthy: false,
	}
	mcpLog.Debug("server registered in registry", "server_id", id)
}

// UnregisterServer removes a server from the in-memory registry.
func (r *Registry) UnregisterServer(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.servers, id)
	mcpLog.Debug("server unregistered from registry", "server_id", id)
}

// SyncTools updates the in-memory tool list for a server and persists to DB.
// This is called after a successful tools/list discovery.
func (r *Registry) SyncTools(serverID string, tools []ToolDefinition) error {
	r.mu.Lock()
	s, ok := r.servers[serverID]
	if ok {
		s.Tools = tools
		s.LastSync = time.Now()
	}
	r.mu.Unlock()

	if !ok {
		return fmt.Errorf("server %q not found in registry", serverID)
	}

	// Persist to DB.
	if r.store != nil {
		if err := r.saveToolsToDB(serverID, tools); err != nil {
			return fmt.Errorf("persist tools: %w", err)
		}
	}

	return nil
}

func (r *Registry) saveToolsToDB(serverID string, tools []ToolDefinition) error {
	inputs := make([]db.MCPToolInput, 0, len(tools))
	for _, tool := range tools {
		inputs = append(inputs, db.MCPToolInput{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	}
	return r.store.SaveMCPTools(serverID, inputs)
}

func (r *Registry) loadServersFromDB() error {
	if r.store == nil {
		return nil
	}

	rows, err := r.store.ListMCPServers()
	if err != nil {
		return fmt.Errorf("query servers: %w", err)
	}

	for _, row := range rows {
		if !row.Enabled {
			continue
		}

		timeout := fmt.Sprintf("%dms", row.TimeoutMs)
		if row.TimeoutMs <= 0 {
			timeout = "30s"
		}

		sc := config.MCPServerConfig{
			ID:          row.ID,
			Name:        row.Name,
			Description: row.Description,
			Transport:   row.Transport,
			URL:         row.URL,
			Command:     row.Command,
			Handler:     row.Handler,
			Enabled:     true,
			Timeout:     timeout,
			MaxRetries:  row.MaxRetries,
			AuthType:    row.AuthType,
			AuthKeyEnv:  row.AuthKeyEnv,
		}

		// Unmarshal JSON args and env.
		if row.Args != "" {
			if err := json.Unmarshal([]byte(row.Args), &sc.Args); err != nil {
				mcpLog.Warn("failed to unmarshal args for server", "server_id", row.ID, "error", err)
			}
		}
		if row.Env != "" {
			if err := json.Unmarshal([]byte(row.Env), &sc.Env); err != nil {
				mcpLog.Warn("failed to unmarshal env for server", "server_id", row.ID, "error", err)
			}
		}

		// Only add if not already registered (config takes precedence).
		if _, exists := r.servers[row.ID]; !exists {
			r.servers[row.ID] = &ServerInfo{
				ID:     row.ID,
				Config: sc,
			}
		}
	}

	return nil
}

func (r *Registry) loadToolsFromDB(serverID string) error {
	if r.store == nil {
		return nil
	}

	rows, err := r.store.ListMCPTools(serverID)
	if err != nil {
		return fmt.Errorf("query tools for server %s: %w", serverID, err)
	}

	var tools []ToolDefinition
	for _, row := range rows {
		t := ToolDefinition{
			Name:        row.Name,
			Description: row.Description,
		}
		if len(row.Schema) > 0 {
			t.InputSchema = row.Schema
		}
		tools = append(tools, t)
	}

	mcpLog.Debug(
		"loaded tools from DB",
		"server_id", serverID,
		"count", len(tools),
	)

	server, ok := r.servers[serverID]
	if ok {
		server.Tools = tools
	}
	return nil
}
