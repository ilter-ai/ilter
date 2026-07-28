package jobs

import (
	"context"
	"fmt"
	"time"
)

// ListStuckRuns returns runs that have been in 'running' status beyond a
// reasonable threshold (150s = 120s default timeout + 30s buffer).
func (s *JobStore) ListStuckRuns() ([]JobRun, error) {
	threshold := 150 * time.Second
	cutoff := time.Now().Add(-threshold)
	rows, err := s.db.Query(
		`SELECT id, job_id, trigger_id, status, llm_result, llm_error,
		 delivery_result, delivery_error, prompt_tokens, completion_tokens, cost_usd,
		 started_at, completed_at, duration_ms, attempts, request_body, execution_key,
		 next_retry_at, last_error, steps
		 FROM job_runs WHERE status = 'running' AND started_at < ?`,
		cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("list stuck runs: %w", err)
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
			return nil, fmt.Errorf("scan stuck run row: %w", err)
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// StartupSweep marks runs orphaned by a crash or the duplicate-run bug
// as dead_letter so the dashboard shows the real state instead of stuck
// "pending" rows that will never execute.
//
//   - runs stuck in "running" => dead_letter (goroutine died with the process)
//   - runs stuck in "pending" with zero attempts => dead_letter (orphaned by
//     the Dispatch+Enqueue duplicate-run bug that created two runs per trigger)
func (s *JobStore) StartupSweep(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE job_runs SET status='dead_letter',
		 llm_error='interrupted by restart',
		 completed_at=datetime('now')
		 WHERE status='running'`)
	if err != nil {
		return fmt.Errorf("startup sweep (running): %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE job_runs SET status='dead_letter',
		 llm_error='orphaned by duplicate run bug',
		 completed_at=datetime('now')
		 WHERE status='pending' AND attempts=0 AND started_at IS NOT NULL
		 AND started_at < datetime('now', '-1 hour')`)
	if err != nil {
		return fmt.Errorf("startup sweep (orphaned): %w", err)
	}
	return nil
}

// CleanupHistory removes old run records based on retention and max-per-job policies.
func (s *JobStore) CleanupHistory(ctx context.Context, maxPerJob, retentionDays int) error {
	if retentionDays > 0 {
		_, err := s.db.ExecContext(
			ctx,
			`DELETE FROM job_runs WHERE status NOT IN ('pending', 'retrying', 'running', 'dead_letter')
			 AND started_at < datetime('now', ? || ' days')`,
			fmt.Sprintf("-%d", retentionDays),
		)
		if err != nil {
			return fmt.Errorf("cleanup history by retention: %w", err)
		}
	}
	if maxPerJob > 0 {
		_, err := s.db.ExecContext(ctx, `
			DELETE FROM job_runs WHERE status NOT IN ('pending', 'retrying', 'running', 'dead_letter')
			 AND id IN (
				SELECT id FROM (
					SELECT id, ROW_NUMBER() OVER (PARTITION BY job_id ORDER BY started_at DESC) AS rn
					FROM job_runs
				) WHERE rn > ?
			)`, maxPerJob)
		if err != nil {
			return fmt.Errorf("cleanup history by max per job: %w", err)
		}
	}
	return nil
}

// ResetStuckRuns resets orphaned 'running' runs back to 'pending' so the
// reconciler retries them instead of leaving them as terminal 'interrupted'.
// next_retry_at is set to now + exponential backoff based on attempts count
// so retries don't all fire immediately on restart.
func (s *JobStore) ResetStuckRuns(ctx context.Context, retryDelayBase time.Duration) error {
	delaySec := int(retryDelayBase.Seconds())
	if delaySec < 1 {
		delaySec = 10
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE job_runs SET status='pending',
		 next_retry_at=datetime('now', '+' || (attempts * ?) || ' seconds'),
		 last_error='interrupted: reset by reconciler',
		 completed_at=NULL
		 WHERE status='running' AND started_at < datetime('now', '-5 minutes')`,
		delaySec)
	if err != nil {
		return fmt.Errorf("reset stuck runs: %w", err)
	}
	return nil
}

// ClaimPendingRun atomically transitions a pending run to running by
// incrementing its attempts count. Returns true if the row was updated.
// The WHERE clause ensures only runs with attempts < maxAttempts are claimed,
// providing poison-run protection, and runs with next_retry_at in the future
// are deferred until the retry delay has elapsed.
func (s *JobStore) ClaimPendingRun(ctx context.Context, id string, maxAttempts int) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE job_runs SET status='running', attempts=attempts+1, started_at=datetime('now'),
		 next_retry_at=NULL, last_error=NULL
		 WHERE id=? AND status IN ('pending', 'retrying') AND attempts < ?
		 AND (next_retry_at IS NULL OR unixepoch(next_retry_at) <= unixepoch('now'))`,
		id, maxAttempts)
	if err != nil {
		return false, fmt.Errorf("claim pending run %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("claim pending run %s rows affected: %w", id, err)
	}
	return n > 0, nil
}

// ListPendingRuns returns all runs with status='pending' or 'retrying' and attempts < maxAttempts.
// Runs with next_retry_at in the future are excluded (retry scheduling).
func (s *JobStore) ListPendingRuns(ctx context.Context, maxAttempts int) ([]JobRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, job_id, trigger_id, status, llm_result, llm_error,
		 delivery_result, delivery_error, prompt_tokens, completion_tokens, cost_usd,
		 started_at, completed_at, duration_ms, attempts, request_body, execution_key,
		 next_retry_at, last_error, steps
		 FROM job_runs WHERE status IN ('pending', 'retrying') AND attempts < ?
		 AND (next_retry_at IS NULL OR unixepoch(next_retry_at) <= unixepoch('now'))`,
		maxAttempts)
	if err != nil {
		return nil, fmt.Errorf("list pending runs: %w", err)
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
			return nil, fmt.Errorf("scan pending run row: %w", err)
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// FailExhaustedRuns marks runs that have exceeded max attempts as 'dead_letter'.
// This prevents them from being retried indefinitely by the reconciler.
func (s *JobStore) FailExhaustedRuns(ctx context.Context, maxAttempts int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE job_runs SET status='dead_letter', llm_error='max attempts exceeded', next_retry_at=NULL
		 WHERE status IN ('pending', 'retrying') AND attempts >= ?`,
		maxAttempts)
	if err != nil {
		return fmt.Errorf("fail exhausted runs: %w", err)
	}
	return nil
}

// ListDeadLetterRuns returns all runs with status='dead_letter', newest first.
func (s *JobStore) ListDeadLetterRuns(ctx context.Context) ([]JobRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, job_id, trigger_id, status, llm_result, llm_error,
		 delivery_result, delivery_error, prompt_tokens, completion_tokens, cost_usd,
		 started_at, completed_at, duration_ms, attempts, request_body, execution_key,
		 next_retry_at, last_error, steps
		 FROM job_runs WHERE status='dead_letter'
		 ORDER BY started_at DESC LIMIT 50`)
	if err != nil {
		return nil, fmt.Errorf("list dead letter runs: %w", err)
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
			return nil, fmt.Errorf("scan dead letter run row: %w", err)
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}
