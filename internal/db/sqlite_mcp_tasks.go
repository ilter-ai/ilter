package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ilter-ai/ilter/internal/db/sqlc"
)

// MCPTaskRow is an mcp_tasks row — the backing store for the MCP Tasks
// extension (io.modelcontextprotocol/tasks, 2026-07-28 only).
type MCPTaskRow struct {
	ID                   string
	KeyID                string
	ServerID             string
	ToolName             string
	Arguments            json.RawMessage
	Status               string
	Result               json.RawMessage
	InputRequiredPayload json.RawMessage
	ErrorMessage         string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	ExpiresAt            time.Time
}

// CreateMCPTask inserts a new task row in the "pending" state (or whatever
// status is passed).
func (s *SQLiteStore) CreateMCPTask(ctx context.Context, t MCPTaskRow) error {
	args := t.Arguments
	if args == nil {
		args = json.RawMessage("{}")
	}
	// result/input_required_payload are explicitly inserted as empty
	// []byte rather than left to the column's SQL-level DEFAULT '' — the
	// sqlite driver returns a DEFAULT-applied TEXT value as a native Go
	// string on read-back, which json.RawMessage (a []byte alias) cannot
	// Scan directly; inserting real (empty) bytes here keeps every
	// read-back of these columns byte-typed and Scan-compatible.
	return s.queries.CreateMCPTask(ctx, sqlc.CreateMCPTaskParams{
		ID:                   t.ID,
		KeyID:                strPtr(t.KeyID),
		ServerID:             strPtr(t.ServerID),
		ToolName:             t.ToolName,
		Arguments:            args,
		Status:               t.Status,
		Result:               json.RawMessage(""),
		InputRequiredPayload: json.RawMessage(""),
		ExpiresAt:            t.ExpiresAt,
	})
}

// GetMCPTask fetches a single task by ID.
func (s *SQLiteStore) GetMCPTask(ctx context.Context, id string) (*MCPTaskRow, error) {
	row, err := s.queries.GetMCPTask(ctx, id)
	if err != nil {
		return nil, err
	}
	return &MCPTaskRow{
		ID:                   row.ID,
		KeyID:                strDeref(row.KeyID),
		ServerID:             strDeref(row.ServerID),
		ToolName:             row.ToolName,
		Arguments:            row.Arguments,
		Status:               row.Status,
		Result:               row.Result,
		InputRequiredPayload: row.InputRequiredPayload,
		ErrorMessage:         strDeref(row.ErrorMessage),
		CreatedAt:            row.CreatedAt,
		UpdatedAt:            row.UpdatedAt,
		ExpiresAt:            row.ExpiresAt,
	}, nil
}

// UpdateMCPTaskStatus transitions a task to a terminal or intermediate
// status (running/completed/failed), optionally attaching a result or
// error message.
func (s *SQLiteStore) UpdateMCPTaskStatus(ctx context.Context, id, status string, result json.RawMessage, errMsg string) error {
	if result == nil {
		result = json.RawMessage("")
	}
	return s.queries.UpdateMCPTaskStatus(ctx, sqlc.UpdateMCPTaskStatusParams{
		Status:       status,
		Result:       result,
		ErrorMessage: strPtr(errMsg),
		ID:           id,
	})
}

// UpdateMCPTaskInputRequired transitions a task into the input_required
// state (MRTR pattern) with the payload describing what input is needed.
func (s *SQLiteStore) UpdateMCPTaskInputRequired(ctx context.Context, id string, payload json.RawMessage) error {
	if payload == nil {
		payload = json.RawMessage("")
	}
	return s.queries.UpdateMCPTaskInputRequired(ctx, sqlc.UpdateMCPTaskInputRequiredParams{
		InputRequiredPayload: payload,
		ID:                   id,
	})
}

// DeleteExpiredMCPTasks removes every task whose expires_at has passed —
// the backing operation for the Tasks engine's expiry sweep.
func (s *SQLiteStore) DeleteExpiredMCPTasks(ctx context.Context) error {
	return s.queries.DeleteExpiredMCPTasks(ctx)
}
