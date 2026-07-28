package db

import (
	"context"
	"encoding/json"

	"github.com/ilter-ai/ilter/internal/db/sqlc"
)

// MCPServerRow is a mcp_servers row, for loading supplemental (admin-created)
// servers into the runtime registry.
type MCPServerRow struct {
	ID          string
	Name        string
	Description string
	Transport   string
	URL         string
	Command     string
	Args        string // JSON-encoded []string
	Env         string // JSON-encoded map[string]string
	Handler     string
	Enabled     bool
	TimeoutMs   int
	MaxRetries  int
	AuthType    string
	AuthKeyEnv  string
}

// ListMCPServers returns every row in mcp_servers, enabled or not; callers
// filter by Enabled themselves (registry only wants enabled servers not
// already present from static config).
func (s *SQLiteStore) ListMCPServers() ([]MCPServerRow, error) {
	rows, err := s.queries.ListMCPServers(context.Background())
	if err != nil {
		return nil, err
	}
	result := make([]MCPServerRow, 0, len(rows))
	for _, r := range rows {
		result = append(result, MCPServerRow{
			ID:          r.ID,
			Name:        r.Name,
			Description: strDeref(r.Description),
			Transport:   r.Transport,
			URL:         strDeref(r.Url),
			Command:     strDeref(r.Command),
			Args:        strDeref(r.Args),
			Env:         strDeref(r.Env),
			Handler:     strDeref(r.Handler),
			Enabled:     r.Enabled != 0,
			TimeoutMs:   int(int64Deref(r.TimeoutMs)),
			MaxRetries:  int(r.MaxRetries),
			AuthType:    strDeref(r.AuthType),
			AuthKeyEnv:  strDeref(r.AuthKeyEnv),
		})
	}
	return result, nil
}

// MCPToolRow is a mcp_tools row for a single server.
type MCPToolRow struct {
	Name        string
	Description string
	Schema      json.RawMessage
}

// ListMCPTools returns the tools discovered for serverID, ordered by name.
func (s *SQLiteStore) ListMCPTools(serverID string) ([]MCPToolRow, error) {
	rows, err := s.queries.ListMCPTools(context.Background(), serverID)
	if err != nil {
		return nil, err
	}
	result := make([]MCPToolRow, 0, len(rows))
	for _, r := range rows {
		result = append(result, MCPToolRow{
			Name:        r.Name,
			Description: strDeref(r.Description),
			Schema:      r.Schema,
		})
	}
	return result, nil
}

// MCPToolInput is a single discovered tool to persist via SaveMCPTools.
type MCPToolInput struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// SaveMCPTools replaces all persisted tools for serverID with tools, in a
// single transaction (delete-then-bulk-insert, matching a tools/list
// discovery result exactly).
func (s *SQLiteStore) SaveMCPTools(serverID string, tools []MCPToolInput) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	ctx := context.Background()
	qtx := s.queries.WithTx(tx)

	if err := qtx.DeleteMCPToolsByServer(ctx, serverID); err != nil {
		return err
	}

	for _, t := range tools {
		schema := t.InputSchema
		if schema == nil {
			schema = json.RawMessage("")
		}
		if err := qtx.UpsertMCPTool(ctx, sqlc.UpsertMCPToolParams{
			ID:          serverID + ":" + t.Name,
			ServerID:    serverID,
			Name:        t.Name,
			Description: strPtr(t.Description),
			Schema:      schema,
		}); err != nil {
			return err
		}
	}

	return tx.Commit()
}
