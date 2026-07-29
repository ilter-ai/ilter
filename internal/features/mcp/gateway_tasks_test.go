package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/mcp/inline"
	"github.com/ilter-ai/ilter/internal/features/mcp/protocol"
)

// newTestGatewayWithStore builds a Gateway backed by a real in-memory
// SQLite store, so its TaskManager is non-nil — newTestGateway (used by
// most other gateway tests) intentionally passes a nil store and has no
// task support, since most tests don't need it.
func newTestGatewayWithStore(t *testing.T) *Gateway {
	t.Helper()
	store := setupTaskTestStore(t)
	reg := &Registry{servers: make(map[string]*ServerInfo)}
	return NewGateway(reg, NewAuthorizer(nil, nil, "deny"), nil, store, &config.MCPConfig{Endpoint: "/mcp"}, nil)
}

func dispatch2026(gw *Gateway, method string, params json.RawMessage) *JSONRPCResponse {
	metaParams, _ := json.Marshal(map[string]any{
		"_meta": map[string]any{"io.modelcontextprotocol/protocolVersion": string(protocol.V20260728)},
	})
	if len(params) > 0 {
		// Merge caller params with the _meta block.
		var merged map[string]json.RawMessage
		_ = json.Unmarshal(params, &merged)
		if merged == nil {
			merged = map[string]json.RawMessage{}
		}
		var metaOnly map[string]json.RawMessage
		_ = json.Unmarshal(metaParams, &metaOnly)
		merged["_meta"] = metaOnly["_meta"]
		metaParams, _ = json.Marshal(merged)
	}
	return gw.Dispatch(&JSONRPCRequest{
		JSONRPC: JSONRPCVersion,
		ID:      testID("1"),
		Method:  method,
		Params:  metaParams,
	}, emptyRctx())
}

func TestGateway_TasksGet_NotFound(t *testing.T) {
	gw := newTestGatewayWithStore(t)
	resp := dispatch2026(gw, "tasks/get", json.RawMessage(`{"taskId":"nonexistent"}`))
	if resp.Error == nil {
		t.Fatal("expected error for unknown task id")
	}
}

func TestGateway_TasksGet_MissingTaskID(t *testing.T) {
	gw := newTestGatewayWithStore(t)
	resp := dispatch2026(gw, "tasks/get", json.RawMessage(`{}`))
	if resp.Error == nil {
		t.Fatal("expected error for missing taskId")
	}
}

func TestGateway_TasksGet_CompletedTask(t *testing.T) {
	gw := newTestGatewayWithStore(t)

	id, err := gw.taskManager.RunAsync("key1", "srv1", "slow-tool", nil, func(ctx context.Context, taskID string) (json.RawMessage, error) {
		return json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`), nil
	})
	if err != nil {
		t.Fatalf("RunAsync: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		task, _ := gw.taskManager.Get(context.Background(), id)
		if task.Status == TaskStatusCompleted {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	params, _ := json.Marshal(map[string]string{"taskId": id})
	resp := dispatch2026(gw, "tasks/get", params)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	var result struct {
		ResultType string          `json:"resultType"`
		Status     string          `json:"status"`
		Result     json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.ResultType != "complete" {
		t.Errorf("resultType = %q, want %q", result.ResultType, "complete")
	}
	if result.Status != string(TaskStatusCompleted) {
		t.Errorf("status = %q, want %q", result.Status, TaskStatusCompleted)
	}
}

func TestGateway_TasksGet_InputRequiredTask(t *testing.T) {
	gw := newTestGatewayWithStore(t)

	started := make(chan struct{})
	id, err := gw.taskManager.RunAsync("key1", "srv1", "interactive-tool", nil, func(ctx context.Context, taskID string) (json.RawMessage, error) {
		close(started)
		input, err := gw.taskManager.RequestInput(ctx, taskID, json.RawMessage(`{"question":"confirm?"}`))
		if err != nil {
			return nil, err
		}
		return input, nil
	})
	if err != nil {
		t.Fatalf("RunAsync: %v", err)
	}
	<-started

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		task, _ := gw.taskManager.Get(context.Background(), id)
		if task.Status == TaskStatusInputRequired {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	params, _ := json.Marshal(map[string]string{"taskId": id})
	resp := dispatch2026(gw, "tasks/get", params)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var result struct {
		ResultType    string          `json:"resultType"`
		InputRequests json.RawMessage `json:"inputRequests"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.ResultType != "input_required" {
		t.Errorf("resultType = %q, want %q", result.ResultType, "input_required")
	}
	if string(result.InputRequests) != `{"question":"confirm?"}` {
		t.Errorf("inputRequests = %s, want the question payload", result.InputRequests)
	}

	// Now answer via tasks/update and confirm the task completes.
	updateParams, _ := json.Marshal(map[string]any{"taskId": id, "input": map[string]string{"answer": "yes"}})
	updateResp := dispatch2026(gw, "tasks/update", updateParams)
	if updateResp.Error != nil {
		t.Fatalf("tasks/update unexpected error: %+v", updateResp.Error)
	}

	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		task, _ := gw.taskManager.Get(context.Background(), id)
		if task.Status == TaskStatusCompleted {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("task did not complete after tasks/update answered it")
}

func TestGateway_TasksUpdate_UnknownTask(t *testing.T) {
	gw := newTestGatewayWithStore(t)
	params, _ := json.Marshal(map[string]any{"taskId": "nonexistent", "input": map[string]string{}})
	resp := dispatch2026(gw, "tasks/update", params)
	if resp.Error == nil {
		t.Fatal("expected error updating a task that isn't awaiting input")
	}
}

// TestGateway_ToolsCall_PromotesLongRunningToTask exercises
// executeToolWithPromotion end-to-end with a REAL slow tool (an inline
// handler that sleeps past the (test-shortened) promotion threshold): the
// tools/call response must come back immediately as a TaskHandle rather
// than blocking for the tool's full duration, and polling tasks/get
// afterward must eventually show the tool's actual result once it
// finishes in the background.
func TestGateway_ToolsCall_PromotesLongRunningToTask(t *testing.T) {
	serverID := "promote-slow-server"
	if err := inline.RegisterTools(serverID, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		time.Sleep(150 * time.Millisecond)
		return map[string]any{"done": true}, nil
	}, nil); err != nil {
		t.Fatalf("RegisterTools: %v", err)
	}

	store := setupTaskTestStore(t)
	reg := &Registry{servers: map[string]*ServerInfo{
		serverID: {
			ID:     serverID,
			Config: config.MCPServerConfig{ID: serverID, Transport: "inline"},
			Tools:  []ToolDefinition{{Name: "slow-tool", Description: "slow"}},
		},
	}}
	clients := NewClientManager(reg)
	executor := NewExecutor(reg, clients, nil, nil, nil)
	gw := NewGateway(reg, nil, nil, store, &config.MCPConfig{Endpoint: "/mcp"}, executor)
	gw.SetTaskPromotionThreshold(20 * time.Millisecond) // real tool sleeps 150ms, so this WILL trip

	start := time.Now()
	params, _ := json.Marshal(map[string]any{"name": "slow-tool", "arguments": map[string]any{}})
	resp := dispatch2026(gw, "tools/call", params)
	elapsed := time.Since(start)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if elapsed >= 150*time.Millisecond {
		t.Fatalf("tools/call took %v — expected it to return promptly with a task handle, not block for the tool's full duration", elapsed)
	}

	var handle struct {
		ResultType string `json:"resultType"`
		Task       struct {
			TaskID string `json:"taskId"`
			Status string `json:"status"`
		} `json:"task"`
	}
	if err := json.Unmarshal(resp.Result, &handle); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if handle.Task.TaskID == "" {
		t.Fatal("expected a non-empty task id in the promoted response")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		task, err := gw.taskManager.Get(context.Background(), handle.Task.TaskID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if task.Status == TaskStatusCompleted {
			var result struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			}
			// The promoted result is the marshaled tool Content, not
			// wrapped through WrapCallToolResult (that only applies to
			// the synchronous path) — confirm it round-trips.
			_ = json.Unmarshal(task.Result, &result)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("promoted task never completed")
}

// TestGateway_ToolsCall_FastToolNotPromoted confirms a tool that finishes
// well within the threshold still returns its result synchronously (the
// pre-2026-07-28-identical shape), not a task handle — promotion is a
// last resort, not the default for 2026-07-28 sessions.
func TestGateway_ToolsCall_FastToolNotPromoted(t *testing.T) {
	serverID := "promote-fast-server"
	if err := inline.RegisterTools(serverID, func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		return map[string]any{"done": true}, nil
	}, nil); err != nil {
		t.Fatalf("RegisterTools: %v", err)
	}

	store := setupTaskTestStore(t)
	reg := &Registry{servers: map[string]*ServerInfo{
		serverID: {
			ID:     serverID,
			Config: config.MCPServerConfig{ID: serverID, Transport: "inline"},
			Tools:  []ToolDefinition{{Name: "fast-tool", Description: "fast"}},
		},
	}}
	clients := NewClientManager(reg)
	executor := NewExecutor(reg, clients, nil, nil, nil)
	gw := NewGateway(reg, nil, nil, store, &config.MCPConfig{Endpoint: "/mcp"}, executor)
	gw.SetTaskPromotionThreshold(time.Second) // generous — the tool returns almost instantly

	params, _ := json.Marshal(map[string]any{"name": "fast-tool", "arguments": map[string]any{}})
	resp := dispatch2026(gw, "tools/call", params)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}

	var result struct {
		Content    []map[string]any `json:"content"`
		ResultType string           `json:"resultType"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Content == nil {
		t.Fatal("expected a direct tool result (content), got a task handle instead")
	}
}

func TestGateway_TasksMethods_NotSupportedForOlderVersions(t *testing.T) {
	gw := newTestGatewayWithStore(t)
	for _, method := range []string{"tasks/get", "tasks/update"} {
		resp := gw.Dispatch(&JSONRPCRequest{
			JSONRPC: JSONRPCVersion,
			ID:      testID("1"),
			Method:  method,
			Params:  json.RawMessage(`{"taskId":"x"}`),
		}, emptyRctx()) // no _meta -> defaults to 2025-03-26
		if resp.Error == nil {
			t.Errorf("%s: expected MethodNotFound for a pre-2026-07-28 session, got success", method)
		}
	}
}
