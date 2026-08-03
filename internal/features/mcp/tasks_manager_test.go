package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func newTestTaskManager(t *testing.T) *TaskManager {
	t.Helper()
	tm := NewTaskManager(NewTaskStore(setupTaskTestStore(t)))
	t.Cleanup(tm.Close)
	return tm
}

func waitForStatus(t *testing.T, tm *TaskManager, id string, want TaskStatus, timeout time.Duration) *Task {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		task, err := tm.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("Get(%q): %v", id, err)
		}
		if task.Status == want {
			return task
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("task %q did not reach status %q within %v", id, want, timeout)
	return nil
}

func TestTaskManager_RunAsync_Success(t *testing.T) {
	tm := newTestTaskManager(t)

	id, err := tm.RunAsync("key1", "srv1", "slow-tool", json.RawMessage(`{}`), func(_ context.Context, _ string) (json.RawMessage, error) {
		return json.RawMessage(`{"content":[{"type":"text","text":"done"}]}`), nil
	})
	if err != nil {
		t.Fatalf("RunAsync: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty task id")
	}

	task := waitForStatus(t, tm, id, TaskStatusCompleted, time.Second)
	if string(task.Result) != `{"content":[{"type":"text","text":"done"}]}` {
		t.Errorf("Result = %s, want the tool's returned payload", task.Result)
	}
}

func TestTaskManager_RunAsync_Failure(t *testing.T) {
	tm := newTestTaskManager(t)

	id, err := tm.RunAsync("key1", "srv1", "broken-tool", nil, func(_ context.Context, _ string) (json.RawMessage, error) {
		return nil, errors.New("boom")
	})
	if err != nil {
		t.Fatalf("RunAsync: %v", err)
	}

	task := waitForStatus(t, tm, id, TaskStatusFailed, time.Second)
	if task.ErrorMessage != "boom" {
		t.Errorf("ErrorMessage = %q, want %q", task.ErrorMessage, "boom")
	}
}

// TestTaskManager_RequestInput_RoundTrip proves the input_required / Update
// mechanism is real and functional end-to-end, even though no tool in
// ilter today calls RequestInput — this is exactly the machinery a future
// tool would use, exercised directly rather than through a real caller.
func TestTaskManager_RequestInput_RoundTrip(t *testing.T) {
	tm := newTestTaskManager(t)

	gotInput := make(chan json.RawMessage, 1)
	id, err := tm.RunAsync("key1", "srv1", "interactive-tool", nil, func(ctx context.Context, taskID string) (json.RawMessage, error) {
		input, err := tm.RequestInput(ctx, taskID, json.RawMessage(`{"question":"which account?"}`))
		if err != nil {
			return nil, err
		}
		gotInput <- input
		return json.RawMessage(`{"content":[{"type":"text","text":"resumed"}]}`), nil
	})
	if err != nil {
		t.Fatalf("RunAsync: %v", err)
	}

	task := waitForStatus(t, tm, id, TaskStatusInputRequired, time.Second)
	if string(task.InputRequiredPayload) != `{"question":"which account?"}` {
		t.Errorf("InputRequiredPayload = %s, want the question payload", task.InputRequiredPayload)
	}

	if err := tm.Update(id, json.RawMessage(`{"account":"12345"}`)); err != nil {
		t.Fatalf("Update: %v", err)
	}

	select {
	case input := <-gotInput:
		if string(input) != `{"account":"12345"}` {
			t.Errorf("fn received input = %s, want the Update payload", input)
		}
	case <-time.After(time.Second):
		t.Fatal("fn never resumed after Update")
	}

	waitForStatus(t, tm, id, TaskStatusCompleted, time.Second)
}

func TestTaskManager_Update_UnknownTaskErrors(t *testing.T) {
	tm := newTestTaskManager(t)
	if err := tm.Update("never-existed", json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected error updating a task that never called RequestInput")
	}
}
