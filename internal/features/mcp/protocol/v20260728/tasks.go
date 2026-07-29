package v20260728

import "encoding/json"

// Method names for the io.modelcontextprotocol/tasks extension
// (2026-07-28 only) — a tools/call that runs long is promoted to a
// background task; the client polls tasks/get and answers a paused
// (input_required) task via tasks/update.
const (
	MethodTasksGet    = "tasks/get"
	MethodTasksUpdate = "tasks/update"
)

// GetTaskParams is the params object for tasks/get.
type GetTaskParams struct {
	TaskID string `json:"taskId"`
}

// TaskResult is the tasks/get response: an MRTR-shaped result (resultType
// "complete" or "input_required") carrying the task's current state.
type TaskResult struct {
	ResultType    string          `json:"resultType"`
	TaskID        string          `json:"taskId"`
	Status        string          `json:"status"`
	Result        json.RawMessage `json:"result,omitempty"`
	InputRequests json.RawMessage `json:"inputRequests,omitempty"`
	Error         string          `json:"error,omitempty"`
}

// UpdateTaskParams is the params object for tasks/update: client-supplied
// input answering a task currently paused in the input_required state.
type UpdateTaskParams struct {
	TaskID string          `json:"taskId"`
	Input  json.RawMessage `json:"input"`
}

// UpdateTaskResult is the tasks/update response — a bare acknowledgement.
type UpdateTaskResult struct {
	ResultType string `json:"resultType"`
	TaskID     string `json:"taskId"`
}

// TaskHandle is what a tools/call response carries when ilter promotes
// the call to a background task instead of returning its result directly
// (an unsolicited task handle, per the spec's "servers may return task
// handles unsolicited without per-request opt-in").
type TaskHandle struct {
	ResultType string `json:"resultType"`
	Task       struct {
		TaskID string `json:"taskId"`
		Status string `json:"status"`
	} `json:"task"`
}

// BuildTaskHandle constructs the TaskHandle response for a newly-promoted
// task, in its initial "pending" status.
func BuildTaskHandle(taskID string) TaskHandle {
	h := TaskHandle{ResultType: resultTypeComplete}
	h.Task.TaskID = taskID
	h.Task.Status = "pending"
	return h
}
