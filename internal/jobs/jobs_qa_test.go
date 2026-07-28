package jobs

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/db/dbtest"
)

// Package-level: this file tests the internal JobStore engine and delivery logic (not HTTP handler layer).
//
// setupQAStore creates a temp SQLite database, runs all migrations,
// inserts a test prompt row, and returns a JobStore.
func setupQAStore(t *testing.T) *JobStore {
	t.Helper()
	store := dbtest.NewFile(t)

	// Insert a test prompt so LLM-step jobs can reference it.
	_, err := store.DB.Exec(`INSERT INTO prompts (id, name, content, version, is_active, created_at, updated_at) VALUES (1, 'qa-prompt', 'Hello {{.name}}', '1.0.0', 1, datetime('now'), datetime('now'))`)
	require.NoError(t, err)

	return NewJobStore(store.DB)
}

// TestJobStoreCRUD covers CreateJob, GetJob, ListJobs, ListEnabledJobs,
// UpdateJob, and DeleteJob.
func TestJobStoreCRUD(t *testing.T) {
	s := setupQAStore(t)
	ctx := context.Background()

	// Initially empty.
	jobs, err := s.ListJobs()
	require.NoError(t, err)
	assert.Empty(t, jobs)

	// Create a job.
	job := &Job{
		ID:          "job_test_1",
		Name:        "Test Job",
		Description: "A test job",
		StepsJSON:   `[{"type":"llm","prompt_id":1,"model":"gpt-4o"}]`,
		Enabled:     true,
		TimeoutMs:   60000,
	}
	err = s.CreateJob(ctx, job)
	require.NoError(t, err)

	// Get it back.
	got, err := s.GetJob("job_test_1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Test Job", got.Name)
	assert.True(t, got.Enabled)
	assert.Equal(t, 60000, got.TimeoutMs)

	// List returns one.
	jobs, err = s.ListJobs()
	require.NoError(t, err)
	assert.Len(t, jobs, 1)
	assert.Equal(t, "Test Job", jobs[0].Name)

	// ListEnabled finds it.
	jobs, err = s.ListEnabledJobs()
	require.NoError(t, err)
	assert.Len(t, jobs, 1)

	// Update: change name and disable.
	job.Name = "Updated Job"
	job.Enabled = false
	err = s.UpdateJob(ctx, job)
	require.NoError(t, err)

	got, err = s.GetJob("job_test_1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "Updated Job", got.Name)
	assert.False(t, got.Enabled)

	// ListEnabled should no longer include the disabled job.
	jobs, err = s.ListEnabledJobs()
	require.NoError(t, err)
	assert.Empty(t, jobs)

	// Delete.
	err = s.DeleteJob("job_test_1")
	require.NoError(t, err)

	// Get returns nil.
	got, err = s.GetJob("job_test_1")
	require.NoError(t, err)
	assert.Nil(t, got)

	// List is empty.
	jobs, err = s.ListJobs()
	require.NoError(t, err)
	assert.Empty(t, jobs)
}

// TestJobRunCRUD covers CreateRun, UpdateRun, GetRun, ListRuns, CountRuns,
// ListStuckRuns, and CleanupHistory.
func TestJobRunCRUD(t *testing.T) {
	s := setupQAStore(t)
	ctx := context.Background()

	// Create a parent job first.
	job := &Job{
		ID:        "job_run_test",
		Name:      "Run Test Job",
		StepsJSON: `[{"type":"llm","prompt_id":1,"model":"gpt-4o"}]`,
		Enabled:   true,
	}
	err := s.CreateJob(ctx, job)
	require.NoError(t, err)

	// Create a run with explicit ID.
	run := &JobRun{
		ID:        "run_test_1",
		JobID:     "job_run_test",
		TriggerID: "manual",
		Status:    "pending",
	}
	err = s.CreateRun(ctx, run)
	require.NoError(t, err)

	// Get the run.
	got, err := s.GetRun("run_test_1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "pending", got.Status)
	assert.Equal(t, "manual", got.TriggerID)

	// Update the run to running with token stats.
	run.Status = "running"
	run.PromptTokens = 100
	run.CompletionTokens = 50
	run.Cost = 0.002
	err = s.UpdateRun(ctx, run)
	require.NoError(t, err)

	got, err = s.GetRun("run_test_1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "running", got.Status)
	assert.Equal(t, 100, got.PromptTokens)
	assert.Equal(t, 50, got.CompletionTokens)
	assert.InDelta(t, 0.002, got.Cost, 0.0001)

	// List runs.
	runs, err := s.ListRuns("job_run_test", 1, 20)
	require.NoError(t, err)
	assert.Len(t, runs, 1)

	// Count runs.
	count, err := s.CountRuns("job_run_test")
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// ListStuckRuns checks only 'running' status older than 150s — pending is fine.
	stuck, err := s.ListStuckRuns()
	require.NoError(t, err)
	assert.Empty(t, stuck)

	// Create a stuck run: 'running' status with old timestamp.
	oldRun := &JobRun{
		ID:        "run_stuck_1",
		JobID:     "job_run_test",
		TriggerID: "manual",
		Status:    "running",
		StartedAt: sql.NullTime{Time: time.Now().Add(-200 * time.Second), Valid: true},
	}
	err = s.CreateRun(ctx, oldRun)
	require.NoError(t, err)

	// ListStuckRuns should now find the stuck run.
	stuck, err = s.ListStuckRuns()
	require.NoError(t, err)
	assert.Len(t, stuck, 1)
	assert.Equal(t, "run_stuck_1", stuck[0].ID)

	// Mark run_test_1 as success so it becomes eligible for cleanup.
	run.Status = "success"
	err = s.UpdateRun(ctx, run)
	require.NoError(t, err)

	// CleanupHistory: keep max 1 per job (only considers non-pending/running/dead_letter).
	err = s.CleanupHistory(ctx, 1, 0)
	require.NoError(t, err)

	// Exactly one run should remain (run_stuck_1 is 'running' so it's excluded).
	runs, err = s.ListRuns("job_run_test", 1, 20)
	require.NoError(t, err)
	assert.Len(t, runs, 1)
	assert.Equal(t, "run_stuck_1", runs[0].ID)

	// CleanupHistory with zero args is a no-op.
	err = s.CleanupHistory(ctx, 0, 0)
	require.NoError(t, err)
}

// TestJobStoreEdgeCases tests error paths and boundary conditions.
func TestJobStoreEdgeCases(t *testing.T) {
	s := setupQAStore(t)
	ctx := context.Background()

	// Get nonexistent job — returns nil, not error.
	got, err := s.GetJob("nonexistent")
	require.NoError(t, err)
	assert.Nil(t, got)

	// Update nonexistent job — returns error.
	err = s.UpdateJob(ctx, &Job{ID: "nonexistent", Name: "Nope"})
	require.Error(t, err)

	// Delete nonexistent job — returns error.
	err = s.DeleteJob("nonexistent")
	require.Error(t, err)

	// Get nonexistent run — returns nil, not error.
	run, err := s.GetRun("nonexistent_run")
	require.NoError(t, err)
	assert.Nil(t, run)

	// Update nonexistent run — returns error.
	err = s.UpdateRun(ctx, &JobRun{ID: "nonexistent_run", Status: "done"})
	require.Error(t, err)

	// CountRuns for nonexistent job — returns 0.
	count, err := s.CountRuns("absent_job")
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// ListRuns for nonexistent job — returns empty slice.
	runs, err := s.ListRuns("absent_job", 1, 20)
	require.NoError(t, err)
	assert.Empty(t, runs)

	// CleanupHistory with negative / zero values is safe.
	err = s.CleanupHistory(ctx, -1, -1)
	require.NoError(t, err)

	setupJob := &Job{
		ID:        "job_run_test",
		Name:      "Edge Cases Test Job",
		StepsJSON: `[{"type":"llm","prompt_id":1,"model":"gpt-4o"}]`,
		TimeoutMs: 60000,
	}
	err = s.CreateJob(ctx, setupJob)
	require.NoError(t, err)

	// CreateRun with empty ID auto-generates a run_ prefix ID.
	autoRun := &JobRun{
		JobID:     "job_run_test",
		TriggerID: "auto",
		Status:    "pending",
	}
	err = s.CreateRun(ctx, autoRun)
	require.NoError(t, err)
	assert.NotEmpty(t, autoRun.ID)
	assert.Contains(t, autoRun.ID, "run_")

	// The auto-generated run is retrievable.
	gotRun, err := s.GetRun(autoRun.ID)
	require.NoError(t, err)
	require.NotNil(t, gotRun)
	assert.Equal(t, "pending", gotRun.Status)
	assert.Equal(t, "auto", gotRun.TriggerID)

	// ListRuns with invalid pagination defaults gracefully.
	_, err = s.ListRuns("absent_job", 0, 0)
	require.NoError(t, err)
}

// TestDeterministicErrorGoesToDeadLetter verifies that template/render errors
// (deterministic — retrying won't help) go directly to dead_letter instead of
// being scheduled for retry.
func TestDeterministicErrorGoesToDeadLetter(t *testing.T) {
	s := setupQAStore(t)
	ctx := context.Background()

	// The test prompt (inserted by setupQAStore) is 'Hello {{.name}}'.
	// A job with empty variables_config has no "name" key → template render error.
	job := &Job{
		ID:              "job_dead_letter_test",
		Name:            "Dead Letter Test",
		StepsJSON:       `[{"type":"llm","prompt_id":1,"model":"gpt-4o"}]`,
		VariablesConfig: VariablesConfig{}, // no variables — template {{.name}} will fail
		TimeoutMs:       5000,
		Enabled:         true,
		APIKeyID:        "test-key",
	}
	err := s.CreateJob(ctx, job)
	require.NoError(t, err)

	runner := NewJobRunner(s, nil, nil, nil, &RunnerConfig{
		APIKey:              "test-system-key",
		DefaultTimeoutMs:    5000,
		MaxAttempts:         3,
		RetryDelayBase:      time.Second,
		DefaultBillingKeyID: "test-billing-key",
		ProxyURL:            "http://localhost:9999",
	}, slog.Default())

	runID, err := runner.TriggerRun(ctx, "job_dead_letter_test", "manual-test")
	require.NoError(t, err)

	run, err := s.GetRun(runID)
	require.NoError(t, err)
	require.NotNil(t, run)
	assert.Equal(t, StatusDeadLetter, run.Status, "deterministic template error should go to dead_letter")
	assert.True(t, run.FinishedAt.Valid, "dead_letter run should have finished_at set")
	assert.Contains(t, run.LLMError.String, "render prompt", "error should mention template rendering failure")
}
