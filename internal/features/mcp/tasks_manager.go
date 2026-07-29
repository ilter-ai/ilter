package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// defaultTaskTTL is how long a task record survives after creation before
// the expiry sweep removes it, regardless of whether it ever completed —
// bounds unbounded growth from abandoned/never-polled tasks.
const defaultTaskTTL = 24 * time.Hour

// taskExpirySweepInterval is how often the background sweep runs.
const taskExpirySweepInterval = 10 * time.Minute

// TaskManager is the real async execution engine behind the
// io.modelcontextprotocol/tasks extension (2026-07-28 only): a tools/call
// that runs long is promoted to a background task instead of blocking the
// HTTP request — RunAsync creates the task record and launches the actual
// work in a goroutine; the client polls Get (tasks/get) for the result,
// and — for a task that pauses mid-execution needing more information —
// answers via Update (tasks/update), which resumes the specific goroutine
// blocked in RequestInput.
//
// ilter has no existing tool that calls RequestInput yet (no
// roots/sampling/elicitation flow to retrofit — see the MCP gap analysis
// this project started from), so the input_required round trip has no
// real-world caller today. The mechanism itself is fully real and tested
// end-to-end (TestTaskManager_RequestInput_RoundTrip), not a stub: a
// future tool can call RequestInput and it will work exactly as tested.
type TaskManager struct {
	store *TaskStore

	pendingMu sync.Mutex
	pending   map[string]chan json.RawMessage

	stopSweep chan struct{}
	sweepDone chan struct{}
}

// NewTaskManager creates a TaskManager backed by store and starts its
// background expiry sweep goroutine. Call Close to stop the sweep.
func NewTaskManager(store *TaskStore) *TaskManager {
	tm := &TaskManager{
		store:     store,
		pending:   make(map[string]chan json.RawMessage),
		stopSweep: make(chan struct{}),
		sweepDone: make(chan struct{}),
	}
	go tm.sweepLoop()
	return tm
}

// Close stops the background expiry sweep. Does not cancel in-flight
// RunAsync goroutines (they run against context.Background() by design —
// a task must survive the HTTP request that created it).
func (tm *TaskManager) Close() {
	close(tm.stopSweep)
	<-tm.sweepDone
}

func (tm *TaskManager) sweepLoop() {
	defer close(tm.sweepDone)
	ticker := time.NewTicker(taskExpirySweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-tm.stopSweep:
			return
		case <-ticker.C:
			if err := tm.store.DeleteExpired(context.Background()); err != nil {
				mcpLog.Warn("task expiry sweep failed", "error", err)
			}
		}
	}
}

// RunAsync creates a task record and launches fn in a background
// goroutine, returning the task id immediately so the caller (Gateway's
// tools/call handling) can hand a task handle back to the client instead
// of blocking the HTTP request on fn's completion. fn runs against
// context.Background(), not the original request's context, since the
// whole point of a task is to outlive the request that created it. fn
// receives its own task id so it can call RequestInput against it if it
// needs to pause for client input mid-execution.
func (tm *TaskManager) RunAsync(keyID, serverID, toolName string, arguments json.RawMessage, fn func(ctx context.Context, taskID string) (json.RawMessage, error)) (string, error) {
	id := generateTaskID()
	task := &Task{
		ID:        id,
		KeyID:     keyID,
		ServerID:  serverID,
		ToolName:  toolName,
		Arguments: arguments,
		ExpiresAt: time.Now().Add(defaultTaskTTL),
	}
	if err := tm.store.Create(context.Background(), task); err != nil {
		return "", fmt.Errorf("create task: %w", err)
	}

	go func() {
		ctx := context.Background()
		if err := tm.store.SetRunning(ctx, id); err != nil {
			mcpLog.Warn("failed to mark task running", "task_id", id, "error", err)
		}

		result, err := fn(ctx, id)
		if err != nil {
			if ferr := tm.store.Fail(ctx, id, err.Error()); ferr != nil {
				mcpLog.Warn("failed to mark task failed", "task_id", id, "error", ferr)
			}
			return
		}
		if cerr := tm.store.Complete(ctx, id, result); cerr != nil {
			mcpLog.Warn("failed to mark task completed", "task_id", id, "error", cerr)
		}
	}()

	return id, nil
}

// Get returns the current state of a task (for tasks/get polling).
func (tm *TaskManager) Get(ctx context.Context, id string) (*Task, error) {
	return tm.store.Get(ctx, id)
}

// TaskOutcome is the result of a promoted tool call, delivered on the
// channel PromoteRunning consumes to finalize the task record.
type TaskOutcome struct {
	Result  json.RawMessage
	IsError bool
	Err     error
}

// PromoteRunning creates a task record already in the "running" state —
// used when the underlying work (a tools/call) started executing BEFORE
// the decision to promote it to a task was made (it ran past
// gateway.go's taskPromotionThreshold while still executing
// synchronously). ch must eventually receive exactly one TaskOutcome;
// PromoteRunning spawns a goroutine that waits for it and finalizes the
// task (Complete/Fail) accordingly.
func (tm *TaskManager) PromoteRunning(keyID, serverID, toolName string, arguments json.RawMessage, ch <-chan TaskOutcome) (string, error) {
	id := generateTaskID()
	task := &Task{
		ID:        id,
		KeyID:     keyID,
		ServerID:  serverID,
		ToolName:  toolName,
		Arguments: arguments,
		Status:    TaskStatusRunning,
		ExpiresAt: time.Now().Add(defaultTaskTTL),
	}
	if err := tm.store.Create(context.Background(), task); err != nil {
		return "", fmt.Errorf("create promoted task: %w", err)
	}

	go func() {
		outcome := <-ch
		ctx := context.Background()
		if outcome.Err != nil {
			if err := tm.store.Fail(ctx, id, outcome.Err.Error()); err != nil {
				mcpLog.Warn("failed to mark promoted task failed", "task_id", id, "error", err)
			}
			return
		}
		if err := tm.store.Complete(ctx, id, outcome.Result); err != nil {
			mcpLog.Warn("failed to mark promoted task completed", "task_id", id, "error", err)
		}
	}()

	return id, nil
}

// RequestInput is called BY a running task's fn (inside the goroutine
// RunAsync launched) to pause execution and wait for client input via
// tasks/update: it marks the task input_required (with payload describing
// what's needed) and blocks until Update delivers a matching response, or
// ctx is canceled.
func (tm *TaskManager) RequestInput(ctx context.Context, taskID string, payload json.RawMessage) (json.RawMessage, error) {
	ch := make(chan json.RawMessage, 1)

	tm.pendingMu.Lock()
	tm.pending[taskID] = ch
	tm.pendingMu.Unlock()

	defer func() {
		tm.pendingMu.Lock()
		delete(tm.pending, taskID)
		tm.pendingMu.Unlock()
	}()

	if err := tm.store.SetInputRequired(ctx, taskID, payload); err != nil {
		return nil, fmt.Errorf("mark task input_required: %w", err)
	}

	select {
	case input := <-ch:
		return input, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Update delivers client input to a task currently paused in
// RequestInput (tasks/update). Returns an error if no task with this ID
// is currently awaiting input — either it never paused, already resumed,
// or the id is unknown.
func (tm *TaskManager) Update(taskID string, input json.RawMessage) error {
	tm.pendingMu.Lock()
	ch, ok := tm.pending[taskID]
	if ok {
		delete(tm.pending, taskID)
	}
	tm.pendingMu.Unlock()

	if !ok {
		return fmt.Errorf("task %q is not awaiting input", taskID)
	}
	ch <- input
	return nil
}

func generateTaskID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(time.Now().String()))
	}
	return hex.EncodeToString(buf)
}
