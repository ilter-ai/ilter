package jobs

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/db/dbtest"
)

// ---------------------------------------------------------------------------
// JobRunner construction / lifecycle
// ---------------------------------------------------------------------------

func TestNewJobRunnerDefaults(t *testing.T) {
	runner := NewJobRunner(nil, nil, nil, nil, &RunnerConfig{}, slog.Default())
	require.NotNil(t, runner)
	assert.True(t, runner.accepting.Load())
	assert.Equal(t, 5, cap(runner.sem), "default semaphore capacity should be 5")
}

func TestNewJobRunnerCustomConcurrency(t *testing.T) {
	runner := NewJobRunner(nil, nil, nil, nil, &RunnerConfig{MaxConcurrentJobs: 2}, slog.Default())
	assert.Equal(t, 2, cap(runner.sem))
}

func TestStopAcceptingAndDrain(t *testing.T) {
	runner := NewJobRunner(nil, nil, nil, nil, &RunnerConfig{}, slog.Default())
	require.True(t, runner.accepting.Load())

	runner.StopAccepting()
	assert.False(t, runner.accepting.Load())

	// Drain should complete immediately (no in-flight runs).
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	n := runner.Drain(ctx)
	assert.Equal(t, 0, n, "Drain should return 0 when all goroutines complete")
}

func TestStopAcceptingPreventsEnqueue(t *testing.T) {
	store := NewJobStore(dbtest.NewFile(t).DB)
	ctx := context.Background()

	require.NoError(t, store.CreateJob(ctx, &Job{
		ID: "job-enq", Name: "Enq Test", StepsJSON: `[{"type":"llm","prompt_id":1,"model":"gpt-4o"}]`, Enabled: true,
	}))

	runner := NewJobRunner(store, nil, nil, nil, &RunnerConfig{}, slog.Default())
	runner.StopAccepting()

	_, err := runner.Enqueue(ctx, ExecutionRequest{JobID: "job-enq"})
	assert.ErrorContains(t, err, "not accepting")
}

// ---------------------------------------------------------------------------
// isLLMCallError
// ---------------------------------------------------------------------------

func TestIsLLMCallError(t *testing.T) {
	tests := []struct {
		err   string
		trans bool // true = transient LLM error (retryable)
	}{
		{"LLM call: rate limited (transient 429): slow down", true},
		{"LLM call: proxy returned status 500: upstream error", true},
		{"LLM call: connection refused", true},
		{`LLM call: monthly quota exceeded (permanent): over limit`, false},
		{`billing key "test" rejected by proxy (401): key may be deleted`, false},
		{"step 0 template: template variable name not found", false},
		{"render prompt: template error", false},
		{"some other random error", false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.err, func(t *testing.T) {
			got := isLLMCallError(fmt.Errorf("%s", tc.err))
			assert.Equal(t, tc.trans, got, "isLLMCallError(%q)", tc.err)
		})
	}
}

// ---------------------------------------------------------------------------
// retryOrFail
// ---------------------------------------------------------------------------

func TestRetryOrFail_RetryScheduled(t *testing.T) {
	store := NewJobStore(dbtest.NewFile(t).DB)
	require.NoError(t, store.CreateJob(context.Background(), &Job{
		ID: "job-retry", Name: "Retry Test", StepsJSON: `[{"type":"llm","prompt_id":1,"model":"gpt-4o"}]`, Enabled: true,
	}))

	runner := NewJobRunner(store, nil, nil, nil, &RunnerConfig{
		MaxAttempts:    3,
		RetryDelayBase: 24 * time.Hour, // long enough that AfterFunc won't fire during test
	}, slog.Default())

	run := &JobRun{
		ID:        "run-retry-1",
		JobID:     "job-retry",
		Status:    "running",
		Attempts:  1,
		LLMError:  sqlNullString("LLM call: rate limited (transient 429)"),
		StartedAt: sql.NullTime{Time: time.Now(), Valid: true},
	}
	start := time.Now().Add(-time.Minute)

	result := runner.retryOrFail(run, start)
	assert.True(t, result, "should return true when retry is scheduled")
	assert.Equal(t, "retrying", run.Status)
	assert.True(t, run.NextRetryAt.Valid, "NextRetryAt should be set")
	assert.True(t, run.NextRetryAt.Time.After(time.Now()), "NextRetryAt should be in the future")
	assert.True(t, run.LastError.Valid, "LastError should be set from LLMError")
	assert.Equal(t, run.LLMError.String, run.LastError.String)
	assert.False(t, run.FinishedAt.Valid, "retry should clear FinishedAt")
	assert.Greater(t, run.DurationMs, 0, "DurationMs should be set")
}

func TestRetryOrFail_RetryCopiesLLMErrorToLastError(t *testing.T) {
	runner := NewJobRunner(nil, nil, nil, nil, &RunnerConfig{
		MaxAttempts: 3, RetryDelayBase: 24 * time.Hour,
	}, slog.Default())

	run := &JobRun{
		ID: "run-err-copy", Status: "running", Attempts: 1,
		LLMError:  sqlNullString("test error"),
		StartedAt: sql.NullTime{Time: time.Now(), Valid: true},
	}

	runner.retryOrFail(run, time.Now())
	assert.True(t, run.LastError.Valid)
	assert.Equal(t, "test error", run.LastError.String)
}

func TestRetryOrFail_NoLLMErrorDoesntSetLastError(t *testing.T) {
	runner := NewJobRunner(nil, nil, nil, nil, &RunnerConfig{
		MaxAttempts: 3, RetryDelayBase: 24 * time.Hour,
	}, slog.Default())

	run := &JobRun{
		ID: "run-no-llm-err", Status: "running", Attempts: 1,
		StartedAt: sql.NullTime{Time: time.Now(), Valid: true},
	}

	runner.retryOrFail(run, time.Now())
	assert.False(t, run.LastError.Valid, "LastError should not be set when LLMError is empty")
	assert.Equal(t, "retrying", run.Status)
}

func TestRetryOrFail_DeadLetter(t *testing.T) {
	runner := NewJobRunner(nil, nil, nil, nil, &RunnerConfig{
		MaxAttempts: 3,
	}, slog.Default())

	run := &JobRun{
		ID:        "run-dl-1",
		Status:    "running",
		Attempts:  3, // >= MaxAttempts
		StartedAt: sql.NullTime{Time: time.Now(), Valid: true},
	}
	start := time.Now().Add(-time.Minute)

	result := runner.retryOrFail(run, start)
	assert.False(t, result, "should return false when max attempts exhausted")
	assert.Equal(t, StatusDeadLetter, run.Status)
	assert.False(t, run.NextRetryAt.Valid, "NextRetryAt should be cleared")
	assert.True(t, run.FinishedAt.Valid, "dead_letter run should have FinishedAt set")
	assert.Greater(t, run.DurationMs, 0)
}

func TestRetryOrFail_DeadLetterExactBoundary(t *testing.T) {
	runner := NewJobRunner(nil, nil, nil, nil, &RunnerConfig{
		MaxAttempts: 1,
	}, slog.Default())

	// Attempts == MaxAttempts should go to dead_letter.
	run := &JobRun{
		ID: "run-boundary", Status: "running", Attempts: 1,
		StartedAt: sql.NullTime{Time: time.Now(), Valid: true},
	}
	assert.False(t, runner.retryOrFail(run, time.Now()))
	assert.Equal(t, StatusDeadLetter, run.Status)
}

// ---------------------------------------------------------------------------
// TriggerRun
// ---------------------------------------------------------------------------

func TestTriggerRunJobNotFound(t *testing.T) {
	s := NewJobStore(dbtest.NewFile(t).DB)
	runner := NewJobRunner(s, nil, nil, nil, &RunnerConfig{DefaultTimeoutMs: 5000}, slog.Default())

	_, err := runner.TriggerRun(context.Background(), "nonexistent", "test")
	assert.ErrorContains(t, err, "not found")
}

func TestTriggerRunCreatesRun(t *testing.T) {
	s := NewJobStore(dbtest.NewFile(t).DB)
	ctx := context.Background()

	require.NoError(t, s.CreateJob(ctx, &Job{
		ID: "job-trigger", Name: "Trigger Test",
		StepsJSON: `[{"type":"llm","prompt_id":1,"model":"gpt-4o"}]`,
		Enabled:   true,
		TimeoutMs: 5000,
	}))

	runner := NewJobRunner(s, nil, nil, nil, &RunnerConfig{DefaultTimeoutMs: 5000}, slog.Default())

	// TriggerRun is synchronous — it will fail because there's no proxy
	// running. We just verify the run was created.
	runID, err := runner.TriggerRun(ctx, "job-trigger", "manual-test")
	require.NoError(t, err, "TriggerRun should create the run even if execution fails")

	run, err := s.GetRun(runID)
	require.NoError(t, err)
	require.NotNil(t, run)
	assert.Equal(t, "job-trigger", run.JobID)
	assert.Equal(t, "manual-test", run.TriggerID)
}

// ---------------------------------------------------------------------------
// Reconcile / periodic reconciler
// ---------------------------------------------------------------------------

func TestStartPeriodicReconciler_ZeroInterval(t *testing.T) {
	runner := NewJobRunner(nil, nil, nil, nil, &RunnerConfig{PollInterval: 0}, slog.Default())
	ch := runner.StartPeriodicReconciler(context.Background())
	_, ok := <-ch
	assert.False(t, ok, "zero interval should return closed channel immediately")
}

func TestStartPeriodicReconciler_StopsOnContextCancel(t *testing.T) {
	store := NewJobStore(dbtest.NewFile(t).DB)
	ctx := context.Background()
	require.NoError(t, store.CreateJob(ctx, &Job{
		ID: "job-rec", Name: "Rec Test", StepsJSON: `[{"type":"llm","prompt_id":1,"model":"gpt-4o"}]`, Enabled: true,
	}))

	runner := NewJobRunner(store, nil, nil, nil, &RunnerConfig{
		PollInterval: 10 * time.Millisecond,
	}, slog.Default())

	reconcilerCtx, cancel := context.WithCancel(context.Background())
	ch := runner.StartPeriodicReconciler(reconcilerCtx)

	// Let it run one tick.
	time.Sleep(20 * time.Millisecond)

	cancel()
	select {
	case <-ch:
		// OK - channel closed
	case <-time.After(time.Second):
		t.Fatal("channel not closed within 1s after context cancel")
	}
}

func TestStartPeriodicReconciler_StopsOnStopAccepting(t *testing.T) {
	store := NewJobStore(dbtest.NewFile(t).DB)
	ctx := context.Background()
	require.NoError(t, store.CreateJob(ctx, &Job{
		ID: "job-rec2", Name: "Rec Test 2", StepsJSON: `[{"type":"llm","prompt_id":1,"model":"gpt-4o"}]`, Enabled: true,
	}))

	runner := NewJobRunner(store, nil, nil, nil, &RunnerConfig{
		PollInterval: 10 * time.Millisecond,
	}, slog.Default())

	ch := runner.StartPeriodicReconciler(ctx)

	time.Sleep(20 * time.Millisecond) // let one tick pass

	runner.StopAccepting()
	select {
	case <-ch:
		// OK - channel closed
	case <-time.After(time.Second):
		t.Fatal("channel not closed within 1s after StopAccepting")
	}
}

// ---------------------------------------------------------------------------
// store_sql.go — retry / dead-letter paths
// ---------------------------------------------------------------------------

func TestClaimPendingRunSuccess(t *testing.T) {
	s := NewJobStore(dbtest.NewFile(t).DB)
	ctx := context.Background()

	require.NoError(t, s.CreateJob(ctx, &Job{ID: "job-claim-success", Name: "Claim", StepsJSON: `[{"type":"llm","prompt_id":1,"model":"gpt-4o"}]`, Enabled: true}))
	require.NoError(t, s.CreateRun(ctx, &JobRun{ID: "run-claim-ok", JobID: "job-claim-success", Status: "pending"}))

	claimed, err := s.ClaimPendingRun(ctx, "run-claim-ok", 3)
	require.NoError(t, err)
	assert.True(t, claimed)

	got, err := s.GetRun("run-claim-ok")
	require.NoError(t, err)
	assert.Equal(t, "running", got.Status)
	assert.Equal(t, 1, got.Attempts, "first claim should increment to 1")
}

func TestClaimPendingRunExhausted(t *testing.T) {
	s := NewJobStore(dbtest.NewFile(t).DB)
	ctx := context.Background()

	require.NoError(t, s.CreateJob(ctx, &Job{ID: "job-claim-exhaust", Name: "Exhaust", StepsJSON: `[{"type":"llm","prompt_id":1,"model":"gpt-4o"}]`, Enabled: true}))
	require.NoError(t, s.CreateRun(ctx, &JobRun{ID: "run-claim-exhaust", JobID: "job-claim-exhaust", Status: "pending", Attempts: 3}))

	claimed, err := s.ClaimPendingRun(ctx, "run-claim-exhaust", 3)
	require.NoError(t, err)
	assert.False(t, claimed, "run with attempts >= maxAttempts should not be claimable")
}

func TestClaimPendingRunNotPending(t *testing.T) {
	s := NewJobStore(dbtest.NewFile(t).DB)
	ctx := context.Background()

	require.NoError(t, s.CreateJob(ctx, &Job{ID: "job-claim-np", Name: "NotPending", StepsJSON: `[{"type":"llm","prompt_id":1,"model":"gpt-4o"}]`, Enabled: true}))
	require.NoError(t, s.CreateRun(ctx, &JobRun{ID: "run-claim-np", JobID: "job-claim-np", Status: "success"}))

	claimed, err := s.ClaimPendingRun(ctx, "run-claim-np", 3)
	require.NoError(t, err)
	assert.False(t, claimed, "non-pending run should not be claimable")
}

func TestClaimPendingRunDeferredByRetryAt(t *testing.T) {
	s := NewJobStore(dbtest.NewFile(t).DB)
	ctx := context.Background()

	require.NoError(t, s.CreateJob(ctx, &Job{ID: "job-claim-def", Name: "Deferred", StepsJSON: `[{"type":"llm","prompt_id":1,"model":"gpt-4o"}]`, Enabled: true}))
	require.NoError(t, s.CreateRun(ctx, &JobRun{
		ID: "run-claim-def", JobID: "job-claim-def", Status: "retrying", Attempts: 1,
		NextRetryAt: sql.NullTime{Time: time.Now().Add(time.Hour), Valid: true},
	}))

	claimed, err := s.ClaimPendingRun(ctx, "run-claim-def", 3)
	require.NoError(t, err)
	assert.False(t, claimed, "run with future NextRetryAt should not be claimable yet")
}

func TestFailExhaustedRuns(t *testing.T) {
	s := NewJobStore(dbtest.NewFile(t).DB)
	ctx := context.Background()

	require.NoError(t, s.CreateJob(ctx, &Job{ID: "job-exhaust", Name: "Exhaust Fail", StepsJSON: `[{"type":"llm","prompt_id":1,"model":"gpt-4o"}]`, Enabled: true}))
	require.NoError(t, s.CreateRun(ctx, &JobRun{ID: "run-exhaust-1", JobID: "job-exhaust", Status: "pending", Attempts: 3}))
	require.NoError(t, s.CreateRun(ctx, &JobRun{ID: "run-exhaust-2", JobID: "job-exhaust", Status: "retrying", Attempts: 5}))

	err := s.FailExhaustedRuns(ctx, 3)
	require.NoError(t, err)

	for _, id := range []string{"run-exhaust-1", "run-exhaust-2"} {
		got, err := s.GetRun(id)
		require.NoError(t, err)
		assert.Equal(t, StatusDeadLetter, got.Status, "run %s should be dead_letter", id)
	}
}

func TestFailExhaustedRunsSkipsUnderLimit(t *testing.T) {
	s := NewJobStore(dbtest.NewFile(t).DB)
	ctx := context.Background()

	require.NoError(t, s.CreateJob(ctx, &Job{ID: "job-exhaust-skip", Name: "Skip", StepsJSON: `[{"type":"llm","prompt_id":1,"model":"gpt-4o"}]`, Enabled: true}))
	require.NoError(t, s.CreateRun(ctx, &JobRun{ID: "run-exhaust-skip", JobID: "job-exhaust-skip", Status: "pending", Attempts: 1}))

	require.NoError(t, s.FailExhaustedRuns(ctx, 3))

	got, err := s.GetRun("run-exhaust-skip")
	require.NoError(t, err)
	assert.Equal(t, "pending", got.Status, "run with attempts < max should not be affected")
}

func TestListDeadLetterRuns(t *testing.T) {
	s := NewJobStore(dbtest.NewFile(t).DB)
	ctx := context.Background()

	require.NoError(t, s.CreateJob(ctx, &Job{ID: "job-dl", Name: "DL", StepsJSON: `[{"type":"llm","prompt_id":1,"model":"gpt-4o"}]`, Enabled: true}))
	require.NoError(t, s.CreateRun(ctx, &JobRun{ID: "run-dl-1", JobID: "job-dl", Status: StatusDeadLetter}))
	require.NoError(t, s.CreateRun(ctx, &JobRun{ID: "run-dl-2", JobID: "job-dl", Status: StatusDeadLetter}))
	require.NoError(t, s.CreateRun(ctx, &JobRun{ID: "run-not-dl", JobID: "job-dl", Status: "success"}))

	runs, err := s.ListDeadLetterRuns(ctx)
	require.NoError(t, err)
	assert.Len(t, runs, 2, "should return only dead_letter runs")

	ids := make(map[string]bool)
	for _, r := range runs {
		ids[r.ID] = true
	}
	assert.True(t, ids["run-dl-1"])
	assert.True(t, ids["run-dl-2"])
	assert.False(t, ids["run-not-dl"])
}

func TestResetStuckRuns(t *testing.T) {
	s := NewJobStore(dbtest.NewFile(t).DB)
	ctx := context.Background()

	require.NoError(t, s.CreateJob(ctx, &Job{ID: "job-stuck", Name: "Stuck", StepsJSON: `[{"type":"llm","prompt_id":1,"model":"gpt-4o"}]`, Enabled: true}))
	require.NoError(t, s.CreateRun(ctx, &JobRun{
		ID: "run-stuck-1", JobID: "job-stuck", Status: "running",
		StartedAt: sql.NullTime{Time: time.Now().Add(-10 * time.Minute), Valid: true},
	}))

	err := s.ResetStuckRuns(ctx, 10*time.Second)
	require.NoError(t, err)

	got, err := s.GetRun("run-stuck-1")
	require.NoError(t, err)
	assert.Equal(t, "pending", got.Status, "stuck running run should be reset to pending")
	assert.True(t, got.NextRetryAt.Valid, "reset run should have NextRetryAt set for backoff")
	assert.True(t, got.LastError.Valid, "reset run should have LastError set")
}

func TestResetStuckRunsSkipsRecent(t *testing.T) {
	s := NewJobStore(dbtest.NewFile(t).DB)
	ctx := context.Background()

	require.NoError(t, s.CreateJob(ctx, &Job{ID: "job-stuck-recent", Name: "Recent", StepsJSON: `[{"type":"llm","prompt_id":1,"model":"gpt-4o"}]`, Enabled: true}))
	require.NoError(t, s.CreateRun(ctx, &JobRun{
		ID: "run-stuck-recent", JobID: "job-stuck-recent", Status: "running",
		StartedAt: sql.NullTime{Time: time.Now(), Valid: true},
	}))

	require.NoError(t, s.ResetStuckRuns(ctx, 10*time.Second))

	got, err := s.GetRun("run-stuck-recent")
	require.NoError(t, err)
	assert.Equal(t, "running", got.Status, "recently started run should not be reset")
}

func TestStartupSweep(t *testing.T) {
	s := NewJobStore(dbtest.NewFile(t).DB)
	ctx := context.Background()

	require.NoError(t, s.CreateJob(ctx, &Job{ID: "job-sweep", Name: "Sweep", StepsJSON: `[{"type":"llm","prompt_id":1,"model":"gpt-4o"}]`, Enabled: true}))
	require.NoError(t, s.CreateRun(ctx, &JobRun{ID: "run-sweep-running", JobID: "job-sweep", Status: "running"}))
	require.NoError(t, s.CreateRun(ctx, &JobRun{ID: "run-sweep-success", JobID: "job-sweep", Status: "success"}))

	err := s.StartupSweep(ctx)
	require.NoError(t, err)

	got, err := s.GetRun("run-sweep-running")
	require.NoError(t, err)
	assert.Equal(t, StatusDeadLetter, got.Status, "stuck running should be swept to dead_letter")

	got, err = s.GetRun("run-sweep-success")
	require.NoError(t, err)
	assert.Equal(t, "success", got.Status, "success runs should not be affected")
}

func TestStartupSweepNoSideEffectsOnCleanDB(t *testing.T) {
	s := NewJobStore(dbtest.NewFile(t).DB)
	ctx := context.Background()

	require.NoError(t, s.CreateJob(ctx, &Job{ID: "job-sweep-clean", Name: "Clean", StepsJSON: `[{"type":"llm","prompt_id":1,"model":"gpt-4o"}]`, Enabled: true}))
	require.NoError(t, s.CreateRun(ctx, &JobRun{ID: "run-sweep-ok", JobID: "job-sweep-clean", Status: "success"}))

	require.NoError(t, s.StartupSweep(ctx))

	got, err := s.GetRun("run-sweep-ok")
	require.NoError(t, err)
	assert.Equal(t, "success", got.Status)
}

// ---------------------------------------------------------------------------
// ListPendingRuns
// ---------------------------------------------------------------------------

func TestListPendingRuns(t *testing.T) {
	s := NewJobStore(dbtest.NewFile(t).DB)
	ctx := context.Background()

	require.NoError(t, s.CreateJob(ctx, &Job{ID: "job-pending", Name: "Pending List", StepsJSON: `[{"type":"llm","prompt_id":1,"model":"gpt-4o"}]`, Enabled: true}))
	require.NoError(t, s.CreateRun(ctx, &JobRun{ID: "run-pend-1", JobID: "job-pending", Status: "pending"}))
	require.NoError(t, s.CreateRun(ctx, &JobRun{ID: "run-pend-2", JobID: "job-pending", Status: "retrying", Attempts: 1}))
	require.NoError(t, s.CreateRun(ctx, &JobRun{ID: "run-pend-skip", JobID: "job-pending", Status: "success"}))

	runs, err := s.ListPendingRuns(ctx, 3)
	require.NoError(t, err)
	assert.Len(t, runs, 2, "pending + retrying = 2, success should be excluded")
}

func TestListPendingRunsExcludesExhausted(t *testing.T) {
	s := NewJobStore(dbtest.NewFile(t).DB)
	ctx := context.Background()

	require.NoError(t, s.CreateJob(ctx, &Job{ID: "job-pend-ex", Name: "Pend Ex", StepsJSON: `[{"type":"llm","prompt_id":1,"model":"gpt-4o"}]`, Enabled: true}))
	require.NoError(t, s.CreateRun(ctx, &JobRun{ID: "run-pend-ex", JobID: "job-pend-ex", Status: "pending", Attempts: 3}))

	runs, err := s.ListPendingRuns(ctx, 3)
	require.NoError(t, err)
	assert.Empty(t, runs, "run with attempts >= max should be excluded")
}

func TestListPendingRunsExcludesFutureRetry(t *testing.T) {
	s := NewJobStore(dbtest.NewFile(t).DB)
	ctx := context.Background()

	require.NoError(t, s.CreateJob(ctx, &Job{ID: "job-pend-future", Name: "Future", StepsJSON: `[{"type":"llm","prompt_id":1,"model":"gpt-4o"}]`, Enabled: true}))
	require.NoError(t, s.CreateRun(ctx, &JobRun{
		ID: "run-pend-future", JobID: "job-pend-future", Status: "retrying", Attempts: 1,
		NextRetryAt: sql.NullTime{Time: time.Now().Add(time.Hour), Valid: true},
	}))

	runs, err := s.ListPendingRuns(ctx, 3)
	require.NoError(t, err)
	assert.Empty(t, runs, "run with future NextRetryAt should be excluded")
}
