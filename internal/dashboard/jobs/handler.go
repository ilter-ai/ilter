package dashjobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	robfigcron "github.com/robfig/cron/v3"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db/audit"
	"github.com/ilter-ai/ilter/internal/jobs"
	"github.com/ilter-ai/ilter/internal/jobs/triggers"
)

// LockProvider provides distributed locking for job execution.
type LockProvider interface {
	TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error)
	Unlock(ctx context.Context, key string) error
}

// JobsHandler serves the Jobs API.
type JobsHandler struct {
	store       *jobs.JobStore
	trigStore   *triggers.Store
	lock        LockProvider
	cfg         *config.JobsConfig
	logger      *slog.Logger
	auditor     *audit.SQLiteConfigAuditor
	dispatcher  *triggers.Dispatcher
	cronTrigger *triggers.CronTrigger
}

// NewJobsHandler creates a new JobsHandler.
func NewJobsHandler(
	store *jobs.JobStore,
	trigStore *triggers.Store,
	lock LockProvider,
	cfg *config.JobsConfig,
	logger *slog.Logger,
	auditor *audit.SQLiteConfigAuditor,
	dispatcher *triggers.Dispatcher,
	cronTrigger *triggers.CronTrigger,
) *JobsHandler {
	return &JobsHandler{
		store:       store,
		trigStore:   trigStore,
		lock:        lock,
		cfg:         cfg,
		logger:      logger,
		auditor:     auditor,
		dispatcher:  dispatcher,
		cronTrigger: cronTrigger,
	}
}

// refreshCron hot-reloads the cron scheduler after a trigger CRUD operation.
func (h *JobsHandler) refreshCron() {
	if h.cronTrigger == nil {
		return
	}
	if err := h.cronTrigger.Refresh(context.Background()); err != nil {
		h.logger.Error("jobs: cron refresh after trigger change failed", "error", err)
	}
}

// ---------------------------------------------------------------------------
// Response conversion helpers
// ---------------------------------------------------------------------------

func (h *JobsHandler) jobToResponse(job jobs.Job, trigs []triggers.TriggerRow, revealTokenIDs map[string]bool) JobResponse {
	resp := JobResponse{
		ID:             job.ID,
		Name:           job.Name,
		Description:    job.Description,
		Steps:          job.StepsJSON,
		DeliveryConfig: job.DeliveryConfig,
		TimeoutMs:      job.TimeoutMs,
		Enabled:        job.Enabled,
		APIKeyID:       job.APIKeyID,
		CreatedAt:      job.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      job.UpdatedAt.Format(time.RFC3339),
	}
	if len(job.VariablesConfig) > 0 {
		resp.VariablesConfig = job.VariablesConfig
	}
	if len(trigs) > 0 {
		resp.Triggers = make([]TriggerResponse, 0, len(trigs))
		for _, t := range trigs {
			resp.Triggers = append(resp.Triggers, h.triggerToResponse(t, revealTokenIDs[t.ID]))
			if t.Kind == triggers.TriggerKindCron && t.Config.Expr != "" && resp.CronExpr == "" {
				resp.CronExpr = t.Config.Expr
				expr := t.Config.Expr
				if t.Config.Timezone != "" {
					if _, err := time.LoadLocation(t.Config.Timezone); err != nil {
						slog.Warn("invalid cron timezone, using server local", "timezone", t.Config.Timezone, "err", err)
					} else {
						expr = "CRON_TZ=" + t.Config.Timezone + " " + expr
					}
				}
				if sched, err := robfigcron.ParseStandard(expr); err == nil {
					resp.NextRun = sched.Next(time.Now()).Format(time.RFC3339)
				}
			}
		}
	}
	return resp
}

func (h *JobsHandler) triggerToResponse(t triggers.TriggerRow, showToken bool) TriggerResponse {
	resp := TriggerResponse{
		ID:        t.ID,
		Kind:      string(t.Kind),
		Enabled:   t.Enabled,
		CreatedAt: t.CreatedAt.Format(time.RFC3339),
	}
	if cfgJSON, err := json.Marshal(t.Config); err == nil {
		resp.Config = string(cfgJSON)
	}
	if showToken && t.Token != "" {
		resp.Token = t.Token
		resp.Secret = t.Secret
	}
	return resp
}

func runToResponse(r jobs.JobRun) map[string]any {
	m := map[string]any{
		"id":                r.ID,
		"job_id":            r.JobID,
		"trigger_id":        r.TriggerID,
		"status":            r.Status,
		"prompt_tokens":     r.PromptTokens,
		"completion_tokens": r.CompletionTokens,
		"cost":              r.Cost,
		"duration_ms":       r.DurationMs,
	}
	if r.LLMResult.Valid {
		m["llm_result"] = r.LLMResult.String
	}
	if r.LLMError.Valid {
		m["llm_error"] = r.LLMError.String
	}
	if r.DeliveryResult.Valid {
		m["delivery_result"] = r.DeliveryResult.String
	}
	if r.DeliveryError.Valid {
		m["delivery_error"] = r.DeliveryError.String
	}
	if r.StartedAt.Valid {
		m["started_at"] = r.StartedAt.Time.Format(time.RFC3339)
	}
	if r.FinishedAt.Valid {
		m["finished_at"] = r.FinishedAt.Time.Format(time.RFC3339)
	}
	if r.RequestBody.Valid {
		m["request_body"] = r.RequestBody.String
	}
	if r.Attempts > 0 {
		m["attempts"] = r.Attempts
	}
	if r.NextRetryAt.Valid {
		m["next_retry_at"] = r.NextRetryAt.Time.Format(time.RFC3339)
	}
	if r.LastError.Valid {
		m["last_error"] = r.LastError.String
	}
	if r.ExecutionKey.Valid {
		m["execution_key"] = r.ExecutionKey.String
	}
	if r.Steps.Valid {
		var steps []any
		if json.Unmarshal([]byte(r.Steps.String), &steps) == nil {
			m["steps"] = steps
		}
	}
	return m
}

// ---------------------------------------------------------------------------
// Webhook credential generation
// ---------------------------------------------------------------------------

func generateWebhookToken() (token string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate webhook token: %w", err)
	}
	return "wht_" + hex.EncodeToString(b), nil
}

func generateWebhookSecret() (secret string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate webhook secret: %w", err)
	}
	return "whsec_" + hex.EncodeToString(b), nil
}

func generateWebhookCredentials() (token, secret string, err error) {
	token, err = generateWebhookToken()
	if err != nil {
		return "", "", err
	}
	secret, err = generateWebhookSecret()
	if err != nil {
		return "", "", err
	}
	return token, secret, nil
}

// ---------------------------------------------------------------------------
// Trigger validation
// ---------------------------------------------------------------------------

func validateTriggerConfig(input triggerInput) (triggers.TriggerConfig, error) {
	var tc triggers.TriggerConfig
	if err := json.Unmarshal([]byte(input.Config), &tc); err != nil {
		return tc, fmt.Errorf("invalid trigger config JSON: %w", err)
	}
	switch input.Kind {
	case "cron":
		if tc.Expr == "" {
			return tc, fmt.Errorf("cron expression (expr) is required for cron triggers")
		}
		parser := robfigcron.NewParser(robfigcron.Minute | robfigcron.Hour | robfigcron.Dom | robfigcron.Month | robfigcron.Dow)
		if _, err := parser.Parse(tc.Expr); err != nil {
			return tc, fmt.Errorf("invalid cron expression: %w", err)
		}
	case "webhook":
		if tc.Provider == "" {
			tc.Provider = "generic"
		}
	default:
		return tc, fmt.Errorf("unsupported trigger kind: %q (must be \"cron\" or \"webhook\")", input.Kind)
	}
	return tc, nil
}

// ---------------------------------------------------------------------------
// Request / Response types
// ---------------------------------------------------------------------------

type createJobRequest struct {
	Name            string         `json:"name"`
	Description     string         `json:"description,omitempty"`
	Steps           string         `json:"steps,omitempty"`
	VariablesConfig map[string]any `json:"variables_config,omitempty"`
	DeliveryConfig  string         `json:"delivery_config,omitempty"`
	TimeoutMs       int            `json:"timeout_ms,omitempty"`
	Enabled         *bool          `json:"enabled,omitempty"`
	APIKeyID        string         `json:"api_key_id,omitempty"`
	Triggers        []triggerInput `json:"triggers,omitempty"`
}

type updateJobRequest struct {
	Name            string         `json:"name,omitempty"`
	Description     string         `json:"description,omitempty"`
	Steps           string         `json:"steps,omitempty"`
	VariablesConfig map[string]any `json:"variables_config,omitempty"`
	DeliveryConfig  string         `json:"delivery_config,omitempty"`
	TimeoutMs       *int           `json:"timeout_ms,omitempty"`
	Enabled         *bool          `json:"enabled,omitempty"`
	APIKeyID        string         `json:"api_key_id,omitempty"`
	Triggers        []triggerInput `json:"triggers,omitempty"`
}

type triggerInput struct {
	ID     string `json:"id,omitempty"`
	Kind   string `json:"kind"`
	Config string `json:"config"`
}

// JobResponse is the API response shape for a job.
type JobResponse struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Description       string            `json:"description,omitempty"`
	Steps             string            `json:"steps,omitempty"`
	VariablesConfig   map[string]any    `json:"variables_config,omitempty"`
	DeliveryConfig    string            `json:"delivery_config,omitempty"`
	TimeoutMs         int               `json:"timeout_ms,omitempty"`
	Enabled           bool              `json:"enabled"`
	APIKeyID          string            `json:"api_key_id,omitempty"`
	Triggers          []TriggerResponse `json:"triggers,omitempty"`
	CreatedAt         string            `json:"created_at"`
	UpdatedAt         string            `json:"updated_at"`
	LastExecStatus    string            `json:"last_exec_status,omitempty"`
	LastExecStartedAt string            `json:"last_exec_started_at,omitempty"`
	CronExpr          string            `json:"cron_expr,omitempty"`
	NextRun           string            `json:"next_run,omitempty"`
}

// TriggerResponse is the API response shape for a trigger.
type TriggerResponse struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Token     string `json:"token,omitempty"`
	Secret    string `json:"secret,omitempty"`
	Config    string `json:"config,omitempty"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
}
