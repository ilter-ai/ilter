package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// JobStore provides CRUD operations for jobs and job_runs.
type JobStore struct {
	db *sql.DB
}

// NewJobStore creates a new JobStore.
func NewJobStore(db *sql.DB) *JobStore {
	return &JobStore{db: db}
}

// DB returns the underlying database connection.
func (s *JobStore) DB() *sql.DB {
	return s.db
}

// CreateJob inserts a new job into the jobs table.
func (s *JobStore) CreateJob(ctx context.Context, job *Job) error {
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO jobs (id, name, description,
		 steps, variables_config, delivery_config, timeout_ms, enabled, api_key_id,
		 created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))`,
		job.ID, job.Name, job.Description,
		job.StepsJSON, job.VariablesConfig, job.DeliveryConfig, job.TimeoutMs,
		boolToInt(job.Enabled), job.APIKeyID,
	)
	if err != nil {
		return fmt.Errorf("create job %s: %w", job.ID, err)
	}
	return nil
}

// GetJob retrieves a job by ID.
func (s *JobStore) GetJob(id string) (*Job, error) {
	var j Job
	var enabled int
	err := s.db.QueryRow(
		`SELECT id, name, description,
		 steps, variables_config, delivery_config, timeout_ms, enabled, api_key_id,
		 created_at, updated_at FROM jobs WHERE id = ?`, id,
	).Scan(&j.ID, &j.Name, &j.Description,
		&j.StepsJSON, &j.VariablesConfig, &j.DeliveryConfig, &j.TimeoutMs,
		&enabled, &j.APIKeyID, &j.CreatedAt, &j.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get job %s: %w", id, err)
	}
	j.Enabled = enabled != 0
	return &j, nil
}

// ListJobs returns all jobs ordered by name.
func (s *JobStore) ListJobs() ([]Job, error) {
	rows, err := s.db.Query(
		`SELECT j.id, j.name, j.description,
		 j.steps, j.variables_config, j.delivery_config, j.timeout_ms, j.enabled,
		 j.api_key_id, j.created_at, j.updated_at
		 FROM jobs j ORDER BY j.name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()

	jobs := make([]Job, 0)
	for rows.Next() {
		var j Job
		var enabled int
		if err := rows.Scan(&j.ID, &j.Name, &j.Description,
			&j.StepsJSON, &j.VariablesConfig, &j.DeliveryConfig, &j.TimeoutMs,
			&enabled, &j.APIKeyID, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan job row: %w", err)
		}
		j.Enabled = enabled != 0
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return jobs, nil
}

// ListEnabledJobs returns all enabled jobs.
func (s *JobStore) ListEnabledJobs() ([]Job, error) {
	rows, err := s.db.Query(
		`SELECT j.id, j.name, j.description,
		 j.steps, j.variables_config, j.delivery_config, j.timeout_ms, j.enabled,
		 j.api_key_id, j.created_at, j.updated_at
		 FROM jobs j WHERE j.enabled = 1 ORDER BY j.name`,
	)
	if err != nil {
		return nil, fmt.Errorf("list enabled jobs: %w", err)
	}
	defer rows.Close()

	jobs := make([]Job, 0)
	for rows.Next() {
		var j Job
		var enabled int
		if err := rows.Scan(&j.ID, &j.Name, &j.Description,
			&j.StepsJSON, &j.VariablesConfig, &j.DeliveryConfig, &j.TimeoutMs,
			&enabled, &j.APIKeyID, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan enabled job row: %w", err)
		}
		j.Enabled = enabled != 0
		jobs = append(jobs, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return jobs, nil
}

// UpdateJob updates an existing job.
func (s *JobStore) UpdateJob(ctx context.Context, job *Job) error {
	res, err := s.db.ExecContext(
		ctx,
		`UPDATE jobs SET name=?, description=?,
		 steps=?, variables_config=?, delivery_config=?, timeout_ms=?,
		 enabled=?, api_key_id=?, updated_at=datetime('now')
		 WHERE id=?`,
		job.Name, job.Description,
		job.StepsJSON, job.VariablesConfig, job.DeliveryConfig, job.TimeoutMs,
		boolToInt(job.Enabled), job.APIKeyID, job.ID,
	)
	if err != nil {
		return fmt.Errorf("update job %s: %w", job.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("job %s not found", job.ID)
	}
	return nil
}

// DeleteJob deletes a job by ID.
func (s *JobStore) DeleteJob(id string) error {
	res, err := s.db.Exec("DELETE FROM jobs WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete job %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("job %s not found", id)
	}
	return nil
}

// CreateRun inserts a new job run record.
func (s *JobStore) CreateRun(ctx context.Context, run *JobRun) error {
	if run.ID == "" {
		run.ID = fmt.Sprintf("run_%d", time.Now().UnixNano())
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO job_runs (id, job_id, trigger_id, status, llm_result, llm_error,
		 delivery_result, delivery_error, prompt_tokens, completion_tokens, cost_usd,
		 started_at, completed_at, duration_ms, attempts, request_body, execution_key,
		 next_retry_at, last_error, steps)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.JobID, run.TriggerID, run.Status,
		nullStringPtr(run.LLMResult), nullStringPtr(run.LLMError),
		nullStringPtr(run.DeliveryResult), nullStringPtr(run.DeliveryError),
		run.PromptTokens, run.CompletionTokens, run.Cost,
		nullTimePtr(run.StartedAt), nullTimePtr(run.FinishedAt), run.DurationMs,
		run.Attempts,
		nullStringPtr(run.RequestBody), nullStringPtr(run.ExecutionKey),
		nullTimePtr(run.NextRetryAt), nullStringPtr(run.LastError),
		nullStringPtr(run.Steps),
	)
	if err != nil {
		return fmt.Errorf("create run for job %s: %w", run.JobID, err)
	}
	return nil
}

// UpdateRun updates an existing job run.
func (s *JobStore) UpdateRun(ctx context.Context, run *JobRun) error {
	res, err := s.db.ExecContext(
		ctx,
		`UPDATE job_runs SET status=?, llm_result=?, llm_error=?,
		 delivery_result=?, delivery_error=?, prompt_tokens=?, completion_tokens=?,
		 cost_usd=?, started_at=?, completed_at=?, duration_ms=?, request_body=?,
		 execution_key=?, next_retry_at=?, last_error=?, steps=?
		 WHERE id=?`,
		run.Status,
		nullStringPtr(run.LLMResult), nullStringPtr(run.LLMError),
		nullStringPtr(run.DeliveryResult), nullStringPtr(run.DeliveryError),
		run.PromptTokens, run.CompletionTokens, run.Cost,
		nullTimePtr(run.StartedAt), nullTimePtr(run.FinishedAt), run.DurationMs,
		nullStringPtr(run.RequestBody), nullStringPtr(run.ExecutionKey),
		nullTimePtr(run.NextRetryAt), nullStringPtr(run.LastError),
		nullStringPtr(run.Steps),
		run.ID,
	)
	if err != nil {
		return fmt.Errorf("update run %s: %w", run.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("run %s not found", run.ID)
	}
	return nil
}

// GetRun retrieves a run by ID.
func (s *JobStore) GetRun(id string) (*JobRun, error) {
	var r JobRun
	err := s.db.QueryRow(
		`SELECT id, job_id, trigger_id, status, llm_result, llm_error,
		 delivery_result, delivery_error, prompt_tokens, completion_tokens, cost_usd,
		 started_at, completed_at, duration_ms, attempts, request_body, execution_key,
		 next_retry_at, last_error, steps
		 FROM job_runs WHERE id = ?`, id,
	).Scan(&r.ID, &r.JobID, &r.TriggerID, &r.Status,
		&r.LLMResult, &r.LLMError, &r.DeliveryResult, &r.DeliveryError,
		&r.PromptTokens, &r.CompletionTokens, &r.Cost,
		&r.StartedAt, &r.FinishedAt, &r.DurationMs,
		&r.Attempts,
		&r.RequestBody, &r.ExecutionKey,
		&r.NextRetryAt, &r.LastError, &r.Steps)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get run %s: %w", id, err)
	}
	return &r, nil
}

// ListRuns returns runs for a job with pagination.
func (s *JobStore) ListRuns(jobID string, page, perPage int) ([]JobRun, error) {
	offset := max((page-1)*perPage, 0)
	if perPage <= 0 {
		perPage = 20
	}
	rows, err := s.db.Query(
		`SELECT id, job_id, trigger_id, status, llm_result, llm_error,
		 delivery_result, delivery_error, prompt_tokens, completion_tokens, cost_usd,
		 started_at, completed_at, duration_ms, attempts, request_body, execution_key,
		 next_retry_at, last_error, steps
		 FROM job_runs WHERE job_id = ? ORDER BY started_at DESC LIMIT ? OFFSET ?`,
		jobID, perPage, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("list runs for job %s: %w", jobID, err)
	}
	defer rows.Close()

	runs := make([]JobRun, 0)
	for rows.Next() {
		var r JobRun
		if err := rows.Scan(&r.ID, &r.JobID, &r.TriggerID, &r.Status,
			&r.LLMResult, &r.LLMError, &r.DeliveryResult, &r.DeliveryError,
			&r.PromptTokens, &r.CompletionTokens, &r.Cost,
			&r.StartedAt, &r.FinishedAt, &r.DurationMs, &r.Attempts,
			&r.RequestBody, &r.ExecutionKey,
			&r.NextRetryAt, &r.LastError, &r.Steps); err != nil {
			return nil, fmt.Errorf("scan run row: %w", err)
		}
		runs = append(runs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return runs, nil
}

// CountRuns returns the total number of runs for a job.
func (s *JobStore) CountRuns(jobID string) (int, error) {
	var count int
	err := s.db.QueryRow("SELECT COUNT(*) FROM job_runs WHERE job_id = ?", jobID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count runs for job %s: %w", jobID, err)
	}
	return count, nil
}

// GetPrompt retrieves a prompt's content by ID.
func (s *JobStore) GetPrompt(id int) (string, error) {
	var content string
	err := s.db.QueryRow(
		`SELECT content FROM prompts WHERE id = ?`, id,
	).Scan(&content)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("query prompt %d: %w", id, err)
	}
	return content, nil
}

// --- helpers ---

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullStringPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	return &ns.String
}

func nullTimePtr(nt sql.NullTime) *time.Time {
	if !nt.Valid {
		return nil
	}
	return &nt.Time
}

// ScanRequestBody deserializes raw JSON bytes into an ExecutionRequestLLM.
func ScanRequestBody(raw []byte) *ExecutionRequestLLM {
	if len(raw) == 0 {
		return nil
	}
	var req ExecutionRequestLLM
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil
	}
	return &req
}
