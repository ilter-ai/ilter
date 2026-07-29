package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
)

func setupTaskTestStore(t *testing.T) *db.SQLiteStore {
	t.Helper()
	store, err := db.NewSQLiteStore(config.StorageConfig{Type: "sqlite", SqlitePath: ":memory:"})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestTaskStore_CreateAndGet(t *testing.T) {
	ts := NewTaskStore(setupTaskTestStore(t))
	ctx := context.Background()

	task := &Task{
		ID:        "task-1",
		KeyID:     "key1",
		ServerID:  "server1",
		ToolName:  "slow-tool",
		Arguments: json.RawMessage(`{"x":1}`),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := ts.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := ts.Get(ctx, "task-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != TaskStatusPending {
		t.Errorf("Status = %q, want %q (default)", got.Status, TaskStatusPending)
	}
	if got.ToolName != "slow-tool" {
		t.Errorf("ToolName = %q, want %q", got.ToolName, "slow-tool")
	}
	if got.KeyID != "key1" || got.ServerID != "server1" {
		t.Errorf("KeyID/ServerID = %q/%q, want key1/server1", got.KeyID, got.ServerID)
	}
}

func TestTaskStore_SetRunningThenComplete(t *testing.T) {
	ts := NewTaskStore(setupTaskTestStore(t))
	ctx := context.Background()

	task := &Task{ID: "task-2", ToolName: "t", ExpiresAt: time.Now().Add(time.Hour)}
	if err := ts.Create(ctx, task); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := ts.SetRunning(ctx, "task-2"); err != nil {
		t.Fatalf("SetRunning: %v", err)
	}
	got, _ := ts.Get(ctx, "task-2")
	if got.Status != TaskStatusRunning {
		t.Errorf("Status = %q, want %q", got.Status, TaskStatusRunning)
	}

	result := json.RawMessage(`{"content":[{"type":"text","text":"done"}]}`)
	if err := ts.Complete(ctx, "task-2", result); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	got, _ = ts.Get(ctx, "task-2")
	if got.Status != TaskStatusCompleted {
		t.Errorf("Status = %q, want %q", got.Status, TaskStatusCompleted)
	}
	if string(got.Result) != string(result) {
		t.Errorf("Result = %s, want %s", got.Result, result)
	}
}

func TestTaskStore_Fail(t *testing.T) {
	ts := NewTaskStore(setupTaskTestStore(t))
	ctx := context.Background()

	task := &Task{ID: "task-3", ToolName: "t", ExpiresAt: time.Now().Add(time.Hour)}
	_ = ts.Create(ctx, task)

	if err := ts.Fail(ctx, "task-3", "boom"); err != nil {
		t.Fatalf("Fail: %v", err)
	}
	got, _ := ts.Get(ctx, "task-3")
	if got.Status != TaskStatusFailed {
		t.Errorf("Status = %q, want %q", got.Status, TaskStatusFailed)
	}
	if got.ErrorMessage != "boom" {
		t.Errorf("ErrorMessage = %q, want %q", got.ErrorMessage, "boom")
	}
}

func TestTaskStore_SetInputRequired(t *testing.T) {
	ts := NewTaskStore(setupTaskTestStore(t))
	ctx := context.Background()

	task := &Task{ID: "task-4", ToolName: "t", ExpiresAt: time.Now().Add(time.Hour)}
	_ = ts.Create(ctx, task)

	payload := json.RawMessage(`{"question":"which account?"}`)
	if err := ts.SetInputRequired(ctx, "task-4", payload); err != nil {
		t.Fatalf("SetInputRequired: %v", err)
	}
	got, _ := ts.Get(ctx, "task-4")
	if got.Status != TaskStatusInputRequired {
		t.Errorf("Status = %q, want %q", got.Status, TaskStatusInputRequired)
	}
	if string(got.InputRequiredPayload) != string(payload) {
		t.Errorf("InputRequiredPayload = %s, want %s", got.InputRequiredPayload, payload)
	}
}

func TestTaskStore_DeleteExpired(t *testing.T) {
	store := setupTaskTestStore(t)
	ts := NewTaskStore(store)
	ctx := context.Background()

	expired := &Task{ID: "task-old", ToolName: "t", ExpiresAt: time.Now().Add(-time.Hour)}
	fresh := &Task{ID: "task-new", ToolName: "t", ExpiresAt: time.Now().Add(time.Hour)}
	_ = ts.Create(ctx, expired)
	_ = ts.Create(ctx, fresh)

	if err := ts.DeleteExpired(ctx); err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}

	if _, err := ts.Get(ctx, "task-old"); err == nil {
		t.Error("expected expired task to be deleted")
	}
	if _, err := ts.Get(ctx, "task-new"); err != nil {
		t.Errorf("expected fresh task to survive, got error: %v", err)
	}
}
