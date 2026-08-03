package jobs

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"time"
)

// JobRun status constants.
const (
	StatusDeadLetter = "dead_letter"
)

// VariablesConfig stored as TEXT in SQLite, auto-scanned/valued.
type VariablesConfig map[string]any

func (v *VariablesConfig) Scan(src any) error {
	if src == nil {
		*v = nil
		return nil
	}
	var srcBytes []byte
	switch s := src.(type) {
	case []byte:
		srcBytes = s
	case string:
		srcBytes = []byte(s)
	default:
		*v = nil
		return nil
	}
	if len(srcBytes) == 0 {
		*v = VariablesConfig{}
		return nil
	}
	return json.Unmarshal(srcBytes, (*map[string]any)(v))
}

// Value implements driver.Valuer for writing to SQLite as TEXT.
func (v VariablesConfig) Value() (driver.Value, error) {
	if v == nil {
		return nil, nil
	}
	if len(v) == 0 {
		return []byte("{}"), nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// Job represents a job definition (formerly CronJob).
type Job struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	StepsJSON       string          `json:"steps"`            // JSON array of Step
	VariablesConfig VariablesConfig `json:"variables_config"` // VarSource map
	DeliveryConfig  string          `json:"delivery_config"`  // JSON DeliveryConfig
	TimeoutMs       int             `json:"timeout_ms"`
	Enabled         bool            `json:"enabled"`
	APIKeyID        string          `json:"api_key_id"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// Step is a pipeline step (MCP or LLM call).
type Step struct {
	Type      string          `json:"type"`                // "mcp" or "llm"
	Tool      string          `json:"tool,omitempty"`      // mcp step
	Arguments json.RawMessage `json:"arguments,omitempty"` // mcp step — Go-templated JSON
	PromptID  *int            `json:"prompt_id,omitempty"` // llm step; nil = unset
	Model     string          `json:"model,omitempty"`     // llm step
}

// JobRun represents a single execution of a job (formerly CronJobExecution).
type JobRun struct {
	ID               string         `json:"id"`
	JobID            string         `json:"job_id"`
	TriggerID        string         `json:"trigger_id,omitempty"` // which trigger fired
	Status           string         `json:"status"`
	LLMResult        sql.NullString `json:"llm_result"`
	LLMError         sql.NullString `json:"llm_error"`
	DeliveryResult   sql.NullString `json:"delivery_result"`
	DeliveryError    sql.NullString `json:"delivery_error"`
	PromptTokens     int            `json:"prompt_tokens"`
	CompletionTokens int            `json:"completion_tokens"`
	Cost             float64        `json:"cost"`
	StartedAt        sql.NullTime   `json:"started_at"`
	FinishedAt       sql.NullTime   `json:"finished_at"`
	DurationMs       int            `json:"duration_ms"`
	Attempts         int            `json:"attempts"`
	NextRetryAt      sql.NullTime   `json:"next_retry_at"`
	LastError        sql.NullString `json:"last_error"`
	RequestBody      sql.NullString `json:"request_body"`
	ExecutionKey     sql.NullString `json:"execution_key"`
	Steps            sql.NullString `json:"steps"`
}

// VarSource describes a single variable's origin.
type VarSource struct {
	Type  string `json:"type"`  // "static"
	Value string `json:"value"` // literal value
}

// DeliveryConfig describes how to deliver a job's result.
type DeliveryConfig struct {
	Type       string         `json:"type"` // "mcp" | "webhook"
	MCPServer  string         `json:"mcp_server,omitempty"`
	Tool       string         `json:"tool,omitempty"`
	Args       map[string]any `json:"args,omitempty"`
	WebhookURL string         `json:"webhook_url,omitempty"`
}

// ExecutionRequest is the input to execute a job.
type ExecutionRequest struct {
	JobID     string
	TriggerID string
	Payload   []byte // webhook body, empty for cron
	RunID     string // optional; when set, Enqueue skips CreateRun and uses this run ID
}

// ExecutionRequestLLM is the request body stored in a JobRun (marshaled to JSON).
// Kept for backward compatibility with the cron execution model.
type ExecutionRequestLLM struct {
	RenderedPrompt string         `json:"rendered_prompt"`
	Model          string         `json:"model"`
	Variables      map[string]any `json:"variables,omitempty"`
}

// StepProgress tracks per-step execution status, written incrementally to the DB.
type StepProgress struct {
	Index     int     `json:"index"`
	Type      string  `json:"type"`
	Model     string  `json:"model,omitempty"`
	Tool      string  `json:"tool,omitempty"`
	Status    string  `json:"status"`
	Output    string  `json:"output,omitempty"`
	Error     string  `json:"error,omitempty"`
	TokensIn  int     `json:"tokens_in,omitempty"`
	TokensOut int     `json:"tokens_out,omitempty"`
	Cost      float64 `json:"cost,omitempty"`
}
