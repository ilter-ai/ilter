package jobs

import (
	"context"
	"fmt"
	"time"
)

// Reconcile recovers from crashes and re-dispatches pending runs at boot.
//
// Steps:
//  1. Reset all stuck 'running' runs to 'pending' with backoff (orphaned by crash).
//  2. Mark runs that have exhausted their max attempts as 'failed'.
//  3. Claim pending runs atomically and re-enqueue them for execution.
func (r *JobRunner) Reconcile(ctx context.Context, maxAttempts int) {
	// 1. Reset stuck running runs (orphaned from crashed goroutines).
	if err := r.store.ResetStuckRuns(ctx, r.cfg.RetryDelayBase); err != nil {
		r.logger.Error("reconciler: reset stuck runs", "error", err)
	}

	// 2. Fail runs that have exhausted max attempts.
	if err := r.store.FailExhaustedRuns(ctx, maxAttempts); err != nil {
		r.logger.Error("reconciler: fail exhausted runs", "error", err)
	}

	// 3. Claim and re-dispatch pending runs.
	pending, err := r.store.ListPendingRuns(ctx, maxAttempts)
	if err != nil {
		r.logger.Error("reconciler: list pending runs", "error", err)
		return
	}
	for _, run := range pending {
		claimed, err := r.store.ClaimPendingRun(ctx, run.ID, maxAttempts)
		if err != nil || !claimed {
			continue
		}
		run.Status = "running"
		run.Attempts++ // mirror DB increment
		r.wg.Add(1)
		go func(run JobRun) {
			defer r.wg.Done()
			job, err := r.store.GetJob(run.JobID)
			if err != nil || job == nil {
				run.Status = "failed"
				run.LLMError = sqlNullString("job not found during reconciliation")
				_ = r.store.UpdateRun(ctx, &run)
				return
			}
			r.runExecution(context.Background(), *job, &run, time.Now())
		}(run)
	}
}

// StartPeriodicReconciler runs the reconciler in a background goroutine at the
// configured PollInterval. It recovers stuck runs, fails exhausted runs, and
// re-dispatches pending runs. Kills itself and closes the returned channel
// when StopAccepting is called (for graceful shutdown).
func (r *JobRunner) StartPeriodicReconciler(ctx context.Context) chan struct{} {
	done := make(chan struct{})
	if r.cfg.PollInterval <= 0 {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		ticker := time.NewTicker(r.cfg.PollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if !r.accepting.Load() {
					return
				}
				r.Reconcile(ctx, r.cfg.MaxAttempts)
			case <-ctx.Done():
				return
			}
		}
	}()
	return done
}

// TriggerRun executes a job immediately (outside any schedule) and waits for it
// to finish. Returns the run ID. The caller controls the context timeout.
func (r *JobRunner) TriggerRun(ctx context.Context, jobID, triggerID string) (string, error) {
	job, err := r.store.GetJob(jobID)
	if err != nil {
		return "", fmt.Errorf("get job %s: %w", jobID, err)
	}
	if job == nil {
		return "", fmt.Errorf("job %s not found", jobID)
	}

	timeout := time.Duration(job.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = time.Duration(r.cfg.DefaultTimeoutMs) * time.Millisecond
	}
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	runID := fmt.Sprintf("run_%d", time.Now().UnixNano())
	start := time.Now()
	run := &JobRun{
		ID:        runID,
		JobID:     job.ID,
		TriggerID: triggerID,
		Status:    "running",
		StartedAt: sqlNullTime(start),
	}

	if err := r.store.CreateRun(ctx, run); err != nil {
		return "", fmt.Errorf("create run: %w", err)
	}

	execCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	r.runExecution(execCtx, *job, run, start)
	return runID, nil
}
