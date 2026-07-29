package mcp

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ilter-ai/ilter/internal/db"
)

// TaskStatus is the lifecycle state of an MCP Tasks-extension task
// (io.modelcontextprotocol/tasks, 2026-07-28 only).
type TaskStatus string

const (
	TaskStatusPending       TaskStatus = "pending"
	TaskStatusRunning       TaskStatus = "running"
	TaskStatusInputRequired TaskStatus = "input_required"
	TaskStatusCompleted     TaskStatus = "completed"
	TaskStatusFailed        TaskStatus = "failed"
)

// Task is the in-memory/API view of an mcp_tasks row.
type Task struct {
	ID                   string
	KeyID                string
	ServerID             string
	ToolName             string
	Arguments            json.RawMessage
	Status               TaskStatus
	Result               json.RawMessage
	InputRequiredPayload json.RawMessage
	ErrorMessage         string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	ExpiresAt            time.Time
}

// TaskStore persists Tasks-extension tasks. A thin wrapper over
// db.SQLiteStore's MCPTask* methods, giving the mcp package its own Task/
// TaskStatus vocabulary independent of the DB row shape.
type TaskStore struct {
	store *db.SQLiteStore
}

// NewTaskStore creates a TaskStore backed by store.
func NewTaskStore(store *db.SQLiteStore) *TaskStore {
	return &TaskStore{store: store}
}

// Create persists a new task, defaulting Status to "pending" if unset.
func (s *TaskStore) Create(ctx context.Context, t *Task) error {
	if t.Status == "" {
		t.Status = TaskStatusPending
	}
	return s.store.CreateMCPTask(ctx, db.MCPTaskRow{
		ID:        t.ID,
		KeyID:     t.KeyID,
		ServerID:  t.ServerID,
		ToolName:  t.ToolName,
		Arguments: t.Arguments,
		Status:    string(t.Status),
		ExpiresAt: t.ExpiresAt,
	})
}

// Get fetches a task by ID.
func (s *TaskStore) Get(ctx context.Context, id string) (*Task, error) {
	row, err := s.store.GetMCPTask(ctx, id)
	if err != nil {
		return nil, err
	}
	return &Task{
		ID:                   row.ID,
		KeyID:                row.KeyID,
		ServerID:             row.ServerID,
		ToolName:             row.ToolName,
		Arguments:            row.Arguments,
		Status:               TaskStatus(row.Status),
		Result:               row.Result,
		InputRequiredPayload: row.InputRequiredPayload,
		ErrorMessage:         row.ErrorMessage,
		CreatedAt:            row.CreatedAt,
		UpdatedAt:            row.UpdatedAt,
		ExpiresAt:            row.ExpiresAt,
	}, nil
}

// Complete marks a task completed with the given result.
func (s *TaskStore) Complete(ctx context.Context, id string, result json.RawMessage) error {
	return s.store.UpdateMCPTaskStatus(ctx, id, string(TaskStatusCompleted), result, "")
}

// Fail marks a task failed with an error message.
func (s *TaskStore) Fail(ctx context.Context, id string, errMsg string) error {
	return s.store.UpdateMCPTaskStatus(ctx, id, string(TaskStatusFailed), nil, errMsg)
}

// SetRunning marks a task running (from pending).
func (s *TaskStore) SetRunning(ctx context.Context, id string) error {
	return s.store.UpdateMCPTaskStatus(ctx, id, string(TaskStatusRunning), nil, "")
}

// SetInputRequired pauses a task pending client input (MRTR pattern).
func (s *TaskStore) SetInputRequired(ctx context.Context, id string, payload json.RawMessage) error {
	return s.store.UpdateMCPTaskInputRequired(ctx, id, payload)
}

// DeleteExpired removes every task past its expiry — the expiry sweep's
// backing operation.
func (s *TaskStore) DeleteExpired(ctx context.Context) error {
	return s.store.DeleteExpiredMCPTasks(ctx)
}
