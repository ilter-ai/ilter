package jobs

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"
	"time"

	"github.com/ilter-ai/ilter/internal/features/mcp"
	"github.com/ilter-ai/ilter/internal/provider"
)

// RunnerConfig carries configuration for the job execution engine.
type RunnerConfig struct {
	APIKey              string
	MaxConcurrentJobs   int
	DefaultTimeoutMs    int
	MaxAttempts         int
	ProxyURL            string
	DefaultBillingKeyID string
	PollInterval        time.Duration
	RetryDelayBase      time.Duration
	MaxVarLength        int // max bytes per variable value (0 = use default 64KB)
}

// JobRunner executes Job definitions by running their pipeline steps
// (MCP tool calls and LLM prompts) and delivering results.
type JobRunner struct {
	store     *JobStore
	provReg   *provider.Registry
	mcpExec   *mcp.Executor
	lock      LockProvider
	sem       chan struct{}
	cfg       *RunnerConfig
	logger    *slog.Logger
	httpCli   *http.Client
	wg        sync.WaitGroup
	accepting atomic.Bool
}

// NewJobRunner creates a new JobRunner.
func NewJobRunner(store *JobStore, provReg *provider.Registry, mcpExec *mcp.Executor, lock LockProvider, cfg *RunnerConfig, logger *slog.Logger) *JobRunner {
	maxConcurrent := 5
	if cfg.MaxConcurrentJobs > 0 {
		maxConcurrent = cfg.MaxConcurrentJobs
	}
	r := &JobRunner{
		store:   store,
		provReg: provReg,
		mcpExec: mcpExec,
		lock:    lock,
		sem:     make(chan struct{}, maxConcurrent),
		cfg:     cfg,
		logger:  logger,
		httpCli: &http.Client{Timeout: 30 * time.Second},
	}
	r.accepting.Store(true)
	return r
}

// Enqueue creates a JobRun with status='pending' and starts execution
// asynchronously. Returns the run ID.
func (r *JobRunner) Enqueue(ctx context.Context, req ExecutionRequest) (string, error) {
	if !r.accepting.Load() {
		return "", fmt.Errorf("job runner is not accepting new runs")
	}

	job, err := r.store.GetJob(req.JobID)
	if err != nil {
		return "", fmt.Errorf("get job %s: %w", req.JobID, err)
	}
	if job == nil {
		return "", fmt.Errorf("job %s not found", req.JobID)
	}

	var runID string
	var start time.Time
	var run *JobRun

	if req.RunID != "" {
		// Run already created by caller (e.g. Dispatcher.Dispatch).
		// Re-fetch from DB so we have a fresh object to pass to runExecution.
		runID = req.RunID
		existing, err := r.store.GetRun(runID)
		if err != nil {
			return "", fmt.Errorf("get pre-created run %s: %w", runID, err)
		}
		run = existing
		start = existing.StartedAt.Time
	} else {
		runID = fmt.Sprintf("run_%d", time.Now().UnixNano())
		start = time.Now()
		run = &JobRun{
			ID:        runID,
			JobID:     job.ID,
			TriggerID: req.TriggerID,
			Status:    "pending",
			StartedAt: sqlNullTime(start),
		}
		if err := r.store.CreateRun(ctx, run); err != nil {
			return "", fmt.Errorf("create run: %w", err)
		}
	}

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()

		// Claim the run atomically — conditional UPDATE ensures only one
		// executor (us or the reconciler) claims this run.
		claimed, err := r.store.ClaimPendingRun(context.Background(), runID, r.cfg.MaxAttempts)
		if err != nil || !claimed {
			return // lost the race or exhausted; reconciler handles it
		}

		run.Status = "running"
		run.Attempts++ // mirror the DB increment
		r.runExecution(context.Background(), *job, run, start)
	}()
	return runID, nil
}

// StopAccepting prevents new runs from being enqueued. In-flight runs continue
// until they complete or the process shuts down. Call before Drain during
// graceful shutdown.
func (r *JobRunner) StopAccepting() {
	r.accepting.Store(false)
}

// Drain waits for all in-flight runs to complete, with a deadline.
// Returns 0 if all goroutines completed, or 1 if the deadline was reached
// (undrained goroutines will be caught by the reconciler on next boot).
func (r *JobRunner) Drain(ctx context.Context) int {
	done := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return 0
	case <-ctx.Done():
		return 1
	}
}

func sqlNullTime(t time.Time) sql.NullTime {
	return sql.NullTime{Time: t, Valid: true}
}

// sqlNullString creates a valid sql.NullString.
func sqlNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: true}
}

func failRun(run *JobRun, start time.Time) {
	now := time.Now()
	if !run.StartedAt.Valid || run.StartedAt.Time.IsZero() {
		run.StartedAt = sqlNullTime(start)
	}
	run.FinishedAt = sqlNullTime(now)
	run.DurationMs = int(now.Sub(start).Milliseconds())
}

// buildTemplateCtx builds a flat template context from step results and job variables.
// Variables and step results are placed directly at the top level so users write
// {{.url}} instead of {{.vars.url}}, and {{.step0.title}} instead of {{index .steps 0}}.
func buildTemplateCtx(texts []string, raw []json.RawMessage, vars map[string]any) map[string]any {
	ctx := make(map[string]any, len(vars)+len(texts)+3)
	// Step results first — {{.step0}}, {{.step0.id}}, {{.prev}}
	for i := range texts {
		var v any
		if i < len(raw) && len(raw[i]) > 0 {
			if err := json.Unmarshal(raw[i], &v); err != nil {
				slog.Warn("jobs: step result not valid JSON, falling back to text", "step", i, "error", err)
				v = nil
			}
		}
		if v == nil {
			v = texts[i]
		}
		ctx[fmt.Sprintf("step%d", i)] = v
	}
	if len(texts) > 0 {
		ctx["prev"] = ctx[fmt.Sprintf("step%d", len(texts)-1)]
	}
	// Variables — skip names that shadow step/prev keys.
	for k, v := range vars {
		if _, reserved := ctx[k]; !reserved {
			ctx[k] = v
		} else {
			slog.Warn("jobs: job variable shadows a step/prev key and will be ignored", "name", k)
		}
	}
	return ctx
}

func renderTemplate(src string, ctx map[string]any) (string, error) {
	tmpl, err := template.New("step").Funcs(template.FuncMap{
		"json": func(v any) (string, error) {
			b, err := json.Marshal(v)
			return string(b), err
		},
		"v": func(k string) (any, error) {
			v, ok := ctx[k]
			if !ok {
				return nil, fmt.Errorf("template variable %q not found", k)
			}
			return v, nil
		},
	}).Option("missingkey=error").Parse(src)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func ValidateSteps(raw string, varsConfig VariablesConfig) error {
	var steps []Step
	if err := json.Unmarshal([]byte(raw), &steps); err != nil {
		return fmt.Errorf("invalid steps JSON: %w", err)
	}
	if len(steps) == 0 {
		return fmt.Errorf("steps array is empty")
	}
	dummyText := make([]string, len(steps))
	dummyRaw := make([]json.RawMessage, len(steps))
	for i := range steps {
		dummyText[i] = `sample "quoted" output`
		dummyRaw[i] = json.RawMessage(`{}`)
	}
	dummyVars := map[string]any(varsConfig)
	if dummyVars == nil {
		dummyVars = map[string]any{}
	}
	// Reject reserved variable names that shadow step/prev keys.
	stepKeyRe := regexp.MustCompile(`^step\d+$`)
	for k := range dummyVars {
		if k == "prev" || stepKeyRe.MatchString(k) {
			return fmt.Errorf("job variable %q is reserved (shadows step or prev key)", k)
		}
	}
	for i, s := range steps {
		switch s.Type {
		case "mcp":
			if s.Tool == "" {
				return fmt.Errorf("step %d: MCP tool name is required", i)
			}
			ctx := buildTemplateCtx(dummyText[:i], dummyRaw[:i], dummyVars)
			out, err := renderTemplate(string(s.Arguments), ctx)
			if err != nil {
				return fmt.Errorf("step %d template: %w", i, err)
			}
			if !json.Valid([]byte(out)) {
				return fmt.Errorf("step %d: arguments render to invalid JSON (missing `| json`?): %s", i, out)
			}
		case "llm":
			if s.PromptID == nil {
				return fmt.Errorf("step %d: prompt_id is required for LLM steps", i)
			}
			if s.Model == "" {
				return fmt.Errorf("step %d: model is required for LLM steps", i)
			}
		default:
			return fmt.Errorf("step %d: unknown step type %q (must be \"mcp\" or \"llm\")", i, s.Type)
		}
	}
	return nil
}

func flattenContent(content []mcp.ToolContent) string {
	var b strings.Builder
	for _, c := range content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}
