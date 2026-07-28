package triggers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ilter-ai/ilter/internal/jobs"
)

// ErrJobDisabled is returned by Dispatch when the target job's Enabled flag
// is false. Callers (HTTP handlers) can match it with errors.Is to respond
// with a clear 409 rather than a generic 500.
var ErrJobDisabled = errors.New("job is disabled")

// ErrJobNotFound is returned by Dispatch when the target job doesn't exist.
var ErrJobNotFound = errors.New("job not found")

// Dispatcher is the central dedup + enqueue point for all trigger activations.
// It records every activation in the job_activations table (providing idempotency
// via UNIQUE(trigger_id, idem_key)) and submits new activations to the JobRunner.
type Dispatcher struct {
	store   *Store
	runner  *jobs.JobRunner
	idemTTL time.Duration
	logger  *slog.Logger
}

// NewDispatcher creates a new Dispatcher.
//
// Parameters:
//   - store: the trigger Store (used for job_activations table access via store.DB)
//   - runner: the JobRunner (nil means activations are recorded but not executed)
//   - idemTTL: how long to keep old activation records before cleanup (0 = default 24h)
//   - logger: structured logger (nil defaults to slog.Default())
func NewDispatcher(store *Store, runner *jobs.JobRunner, idemTTL time.Duration, logger *slog.Logger) *Dispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Dispatcher{
		store:   store,
		runner:  runner,
		idemTTL: idemTTL,
		logger:  logger,
	}
}

// Dispatch is the central enqueue point for all trigger activations.
//
//  1. Within a single transaction: INSERT activation (idempotency-guarded via
//     UNIQUE(trigger_id, idem_key)) and INSERT a job_runs row with status='pending'.
//  2. If the activation already exists (duplicate), the existing run_id is returned
//     without re-enqueueing.
//  3. After commit, Enqueue is called to submit the run to the JobRunner. If
//     Enqueue fails post-commit, the reconciler will pick up the pending run on
//     the next boot.
//  4. Returns the run ID.
//
// An empty IdempotencyKey on the Activation means the dispatcher generates a
// unique key so every call creates a new activation record.
func (d *Dispatcher) Dispatch(ctx context.Context, act Activation) (string, error) {
	// A trigger's own `enabled` flag only ever guards the trigger itself, not
	// its parent job — a "Disable" on the Job should stop cron/webhook/manual
	// activations too, not just cosmetically flip a badge.
	var jobEnabled bool
	switch err := d.store.DB().QueryRowContext(ctx, "SELECT enabled FROM jobs WHERE id = ?", act.JobID).Scan(&jobEnabled); {
	case err == sql.ErrNoRows:
		return "", fmt.Errorf("dispatcher: job %s: %w", act.JobID, ErrJobNotFound)
	case err != nil:
		return "", fmt.Errorf("dispatcher: check job enabled: %w", err)
	case !jobEnabled:
		return "", fmt.Errorf("dispatcher: job %s: %w", act.JobID, ErrJobDisabled)
	}

	idemKey := act.IdempotencyKey
	if idemKey == "" {
		idemKey = fmt.Sprintf("auto_%d", time.Now().UnixNano())
	}

	tx, err := d.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("dispatcher: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// INSERT activation with idempotency check (inside tx).
	res, err := tx.ExecContext(
		ctx,
		`INSERT INTO job_activations (trigger_id, job_id, idem_key, payload, status, created_at)
		 SELECT ?, ?, ?, ?, 'pending', datetime('now')
		 WHERE NOT EXISTS (
			 SELECT 1 FROM job_activations WHERE trigger_id = ? AND idem_key = ?
		 )`,
		act.TriggerID, act.JobID, idemKey, act.Payload,
		act.TriggerID, idemKey,
	)
	if err != nil {
		return "", fmt.Errorf("dispatcher: insert activation: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return "", fmt.Errorf("dispatcher: rows affected: %w", err)
	}

	if rows == 0 {
		// Duplicate activation — rollback tx and return existing run ID.
		if rbErr := tx.Rollback(); rbErr != nil {
			d.logger.ErrorContext(ctx, "dispatcher: rollback after duplicate",
				"error", rbErr)
		}
		return d.getExistingRunID(ctx, act.TriggerID, idemKey)
	}

	if d.runner == nil {
		d.logger.WarnContext(ctx, "dispatcher: no job runner configured, activation recorded but not executed",
			"trigger_id", act.TriggerID, "job_id", act.JobID, "idem_key", idemKey)
		// Commit the activation record so it's visible, but return an error.
		if cmtErr := tx.Commit(); cmtErr != nil {
			return "", fmt.Errorf("dispatcher: commit (no runner): %w", cmtErr)
		}
		return "", fmt.Errorf("dispatcher: job runner not configured")
	}

	// Generate run ID inside the tx.
	runID := fmt.Sprintf("run_%d", time.Now().UnixNano())

	// INSERT run with status='pending' — reconciler will pick it up if we crash
	// before enqueue completes.
	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO job_runs (id, job_id, trigger_id, status, started_at, attempts)
		 VALUES (?, ?, ?, 'pending', datetime('now'), 0)`,
		runID, act.JobID, act.TriggerID,
	)
	if err != nil {
		return "", fmt.Errorf("dispatcher: insert run: %w", err)
	}

	// Update activation with run_id and status='enqueued'.
	_, err = tx.ExecContext(
		ctx,
		`UPDATE job_activations SET status='enqueued', run_id=? WHERE trigger_id=? AND idem_key=?`,
		runID, act.TriggerID, idemKey,
	)
	if err != nil {
		return "", fmt.Errorf("dispatcher: update activation: %w", err)
	}

	// Commit the transaction — activation + run are now durable.
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("dispatcher: commit: %w", err)
	}

	// Enqueue for execution (outside tx — reconciler catches post-commit crashes).
	if _, enqErr := d.runner.Enqueue(ctx, jobs.ExecutionRequest{
		JobID:     act.JobID,
		TriggerID: act.TriggerID,
		Payload:   act.Payload,
		RunID:     runID,
	}); enqErr != nil {
		d.logger.WarnContext(ctx, "dispatcher: enqueue failed (reconciler will retry)",
			"run_id", runID, "error", enqErr)
	}

	return runID, nil
}

// FireFunc returns a function suitable for ActiveTrigger.Run.
// It wraps Dispatch and discards the run ID, returning only the error.
// This is the function that CronTrigger and other active triggers call
// when they fire.
func (d *Dispatcher) FireFunc() FireFunc {
	return func(ctx context.Context, act Activation) error {
		_, err := d.Dispatch(ctx, act)
		return err
	}
}

// HandleActivation is called by HTTP/webhook handlers when a passive trigger
// is activated by an external request. It delegates directly to Dispatch and
// returns the run ID so the handler can respond with it.
func (d *Dispatcher) HandleActivation(ctx context.Context, act Activation) (string, error) {
	return d.Dispatch(ctx, act)
}

// CleanupOldActivations deletes job_activation rows older than idemTTL.
// This prevents unbounded growth of the job_activations table.
// Call it periodically from a background goroutine or scheduler.
func (d *Dispatcher) CleanupOldActivations(ctx context.Context) error {
	ttl := d.idemTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	cutoff := time.Now().Add(-ttl)
	// Bind cutoff as time.Time, not a pre-formatted string: the SQLite DSN's
	// _time_format/_timezone conversion (see internal/db/sqlite.go) only
	// intercepts the time.Time Go type. A string built via cutoff.Format(...)
	// in the server's local zone would bypass that conversion entirely and
	// compare wrong once the server isn't running in UTC.
	res, err := d.store.DB().ExecContext(
		ctx,
		`DELETE FROM job_activations WHERE datetime(created_at) < datetime(?)`,
		cutoff,
	)
	if err != nil {
		return fmt.Errorf("dispatcher: cleanup: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		d.logger.DebugContext(ctx, "dispatcher: cleaned up old activations", "count", n)
	}
	return nil
}

// getExistingRunID queries an existing activation record by trigger_id and
// idem_key. It returns the run_id if populated, or a synthetic "act_<id>"
// identifier if the activation was recorded but never reached the runner.
func (d *Dispatcher) getExistingRunID(ctx context.Context, triggerID, idemKey string) (string, error) {
	var id int64
	var runID sql.NullString
	var status string
	err := d.store.DB().QueryRowContext(
		ctx,
		`SELECT id, run_id, status FROM job_activations WHERE trigger_id = ? AND idem_key = ?`,
		triggerID, idemKey,
	).Scan(&id, &runID, &status)
	if err != nil {
		return "", fmt.Errorf("dispatcher: query duplicate activation: %w", err)
	}
	if runID.Valid && runID.String != "" {
		return runID.String, nil
	}
	return fmt.Sprintf("act_%d", id), nil
}
