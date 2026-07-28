package mcp

import (
	"testing"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
)

// setupRegistryTestStore creates an in-memory store with a single mcp_servers
// row ("srv1") pre-inserted, since mcp_tools.server_id is a foreign key into
// mcp_servers — SyncTools can't persist tools for a server that was never
// created via the (separate) admin server-CRUD path.
func setupRegistryTestStore(t *testing.T) *db.SQLiteStore {
	t.Helper()
	store, err := db.NewSQLiteStore(config.StorageConfig{Type: "sqlite", SqlitePath: ":memory:"})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if _, err := store.DB.Exec(
		`INSERT INTO mcp_servers (id, name, transport, enabled) VALUES (?, ?, ?, 1)`,
		"srv1", "Server 1", "sse",
	); err != nil {
		t.Fatalf("seed mcp_servers: %v", err)
	}
	return store
}

func TestRegistry_SyncToolsPersistsAndReloads(t *testing.T) {
	store := setupRegistryTestStore(t)

	servers := []config.MCPServerConfig{
		{ID: "srv1", Name: "Server 1", Enabled: true, Transport: "sse"},
	}

	reg, err := NewRegistryFromCache(servers, store)
	if err != nil {
		t.Fatalf("NewRegistryFromCache: %v", err)
	}

	tools := []ToolDefinition{
		{Name: "tool-a", Description: "does a thing", InputSchema: []byte(`{"type":"object"}`)},
		{Name: "tool-b", Description: "does another thing"},
	}
	if syncErr := reg.SyncTools("srv1", tools); syncErr != nil {
		t.Fatalf("SyncTools: %v", syncErr)
	}

	// Reload a fresh registry from the same store — tools must survive.
	reg2, err := NewRegistryFromCache(servers, store)
	if err != nil {
		t.Fatalf("NewRegistryFromCache (reload): %v", err)
	}
	list := reg2.ListTools()
	if len(list) != 2 {
		t.Fatalf("expected 2 tools after reload, got %d", len(list))
	}
	byName := make(map[string]ToolInfo, len(list))
	for _, ti := range list {
		byName[ti.Tool.Name] = ti
	}

	toolA, ok := byName["tool-a"]
	if !ok {
		t.Fatal("tool-a missing after reload")
	}
	if toolA.ServerID != "srv1" || toolA.Tool.Description != "does a thing" {
		t.Errorf("unexpected tool-a: %+v", toolA)
	}
	if string(toolA.Tool.InputSchema) != `{"type":"object"}` {
		t.Errorf("expected schema preserved, got %q", string(toolA.Tool.InputSchema))
	}

	toolB, ok := byName["tool-b"]
	if !ok {
		t.Fatal("tool-b missing after reload")
	}
	if len(toolB.Tool.InputSchema) != 0 {
		t.Errorf("expected empty schema for tool-b, got %q", string(toolB.Tool.InputSchema))
	}

	// Re-sync with a shorter tool list — delete-then-reinsert must drop tool-b.
	if shrinkErr := reg.SyncTools("srv1", []ToolDefinition{tools[0]}); shrinkErr != nil {
		t.Fatalf("SyncTools (shrink): %v", shrinkErr)
	}
	reg3, err := NewRegistryFromCache(servers, store)
	if err != nil {
		t.Fatalf("NewRegistryFromCache (reload 2): %v", err)
	}
	list3 := reg3.ListTools()
	if len(list3) != 1 || list3[0].Tool.Name != "tool-a" {
		t.Fatalf("expected only tool-a after shrink, got %+v", list3)
	}
}

func TestRegistry_LoadServersFromDB_ConfigTakesPrecedence(t *testing.T) {
	store := setupRegistryTestStore(t)

	// Static config registers srv1 with a different name than the DB row;
	// config must win, the DB row must not overwrite it.
	servers := []config.MCPServerConfig{
		{ID: "srv1", Name: "Config Name", Enabled: true, Transport: "sse"},
	}

	reg, err := NewRegistryFromCache(servers, store)
	if err != nil {
		t.Fatalf("NewRegistryFromCache: %v", err)
	}

	found := reg.ListServers()
	if len(found) != 1 {
		t.Fatalf("expected 1 server, got %d", len(found))
	}
	if found[0].Config.Name != "Config Name" {
		t.Errorf("expected static config to take precedence, got name %q", found[0].Config.Name)
	}
}

func TestRegistry_LoadServersFromDB_SupplementalServer(t *testing.T) {
	store := setupRegistryTestStore(t)

	// No static config at all — srv1 must be loaded purely from the DB.
	reg, err := NewRegistryFromCache(nil, store)
	if err != nil {
		t.Fatalf("NewRegistryFromCache: %v", err)
	}

	found := reg.ListServers()
	if len(found) != 1 {
		t.Fatalf("expected 1 server loaded from DB, got %d", len(found))
	}
	if found[0].ID != "srv1" || found[0].Config.Name != "Server 1" {
		t.Errorf("unexpected server: %+v", found[0])
	}
}
