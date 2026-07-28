package triggers

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/ilter-ai/ilter/internal/jobs"
)

// cronLogger adapts *slog.Logger to robfig/cron/v3's Logger interface.
type cronLogger struct {
	logger *slog.Logger
}

func (l *cronLogger) Info(msg string, keysAndValues ...any) {
	l.logger.Info(msg, keysAndValues...)
}

func (l *cronLogger) Error(err error, msg string, keysAndValues ...any) {
	l.logger.Error(msg, append(keysAndValues, "error", err)...)
}

// CronTrigger manages cron-based triggers. It wraps robfig/cron/v3
// to schedule and fire activations according to each trigger's cron expression.
//
// The dispatcher sets a FireFunc via SetFireFunc, calls Start to begin
// scheduling, and calls Stop on shutdown. Refresh is used for hot-reload
// after CRUD operations on triggers.
type CronTrigger struct {
	cron     *cron.Cron
	store    *Store
	logger   *slog.Logger
	fireFunc FireFunc

	// lock is a distributed fire-time lock. Before firing, TryLock on key
	// "cron:fire:{triggerID}:{slot}" with TTL matching the minute-level cron
	// granularity. If another instance holds the lock, this fire is skipped —
	// the DB UNIQUE(trigger_id, idem_key) constraint is the authoritative
	// deduplication mechanism; the lock is a best-effort optimisation.
	lock jobs.LockProvider

	mu      sync.RWMutex
	entries map[cron.EntryID]string // entryID → triggerID
	trigger map[string]cron.EntryID // triggerID → entryID
}

// NewCronTrigger creates a new CronTrigger. It uses 5-field cron expressions
// (no cron.WithSeconds()) matching the existing project convention, and
// chains SkipIfStillRunning to prevent overlapping executions.
//
// lock is a distributed fire-time lock used to prevent multi-instance
// double-firing. Pass a local lock for single-instance deployments or a
// Redis-based lock for multi-replica deployments.
func NewCronTrigger(store *Store, logger *slog.Logger, lock jobs.LockProvider) *CronTrigger {
	cl := &cronLogger{logger: logger}
	return &CronTrigger{
		cron: cron.New(
			cron.WithChain(cron.SkipIfStillRunning(cl)),
		),
		store:   store,
		logger:  logger,
		lock:    lock,
		entries: make(map[cron.EntryID]string),
		trigger: make(map[string]cron.EntryID),
	}
}

// SetFireFunc sets the function called when a cron trigger fires.
// Must be set before Start.
func (ct *CronTrigger) SetFireFunc(fn FireFunc) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.fireFunc = fn
}

// Start loads all enabled cron triggers from the store and starts the
// scheduler. It is non-blocking — the cron scheduler runs in its own
// goroutines. Only triggers with kind='cron' are registered; others
// are silently skipped.
func (ct *CronTrigger) Start(_ context.Context) error {
	all, err := ct.store.ListEnabled()
	if err != nil {
		return fmt.Errorf("cron: list enabled triggers: %w", err)
	}

	var count int
	for i := range all {
		if all[i].Kind != TriggerKindCron {
			continue
		}
		if err := ct.register(all[i]); err != nil {
			ct.logger.Warn(
				"cron: skipping trigger with invalid config",
				"trigger_id", all[i].ID,
				"job_id", all[i].JobID,
				"error", err,
			)
			continue
		}
		count++
	}

	ct.cron.Start()
	ct.logger.Info("cron: scheduler started", "triggers_enabled", count)
	return nil
}

// Stop stops the cron scheduler and waits for all running jobs to complete.
func (ct *CronTrigger) Stop() {
	done := ct.cron.Stop()
	<-done.Done()
	ct.logger.Info("cron: scheduler stopped")
}

// Refresh reloads all cron triggers from the store. It stops the scheduler,
// clears all registered entries, and starts again with the latest
// configuration. This is used for hot-reload after CRUD operations.
func (ct *CronTrigger) Refresh(ctx context.Context) error {
	done := ct.cron.Stop()
	<-done.Done()

	ct.mu.Lock()
	ct.entries = make(map[cron.EntryID]string)
	ct.trigger = make(map[string]cron.EntryID)
	ct.mu.Unlock()

	ct.logger.Info("cron: scheduler refreshed")
	return ct.Start(ctx)
}

// register adds a single cron trigger to the scheduler. The cron expression
// is validated and must use the 5-field format. Invalid expressions return
// an error so the caller can log and skip.
func (ct *CronTrigger) register(tr TriggerRow) error {
	if tr.Config.Expr == "" {
		return fmt.Errorf("cron expression is empty")
	}

	expr := tr.Config.Expr
	if tr.Config.Timezone != "" {
		if _, err := time.LoadLocation(tr.Config.Timezone); err != nil {
			ct.logger.Warn("invalid cron timezone, using server local", "timezone", tr.Config.Timezone, "err", err)
		} else {
			expr = "CRON_TZ=" + tr.Config.Timezone + " " + expr
		}
	}

	if _, err := cron.ParseStandard(expr); err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", tr.Config.Expr, err)
	}

	triggerID := tr.ID
	jobID := tr.JobID

	entryID, err := ct.cron.AddFunc(expr, func() {
		ct.fire(triggerID, jobID)
	})
	if err != nil {
		return fmt.Errorf("add cron entry: %w", err)
	}

	ct.mu.Lock()
	ct.entries[entryID] = tr.ID
	ct.trigger[tr.ID] = entryID
	ct.mu.Unlock()

	ct.logger.Debug(
		"cron: registered trigger",
		"trigger_id", tr.ID,
		"job_id", tr.JobID,
		"expr", tr.Config.Expr,
	)
	return nil
}

// fire is called when a cron trigger fires. It constructs an Activation
// and calls the registered FireFunc.
//
// The idempotency key is "{triggerID}:{scheduledSlotUnix}" where
// scheduledSlot is rounded to the minute boundary (5-field cron has
// minute-level granularity).
//
// Before firing, it attempts a distributed fire-time lock
// ("cron:fire:{triggerID}:{scheduledSlot}") with a 65-second TTL.
// If the lock is already held by another instance, the fire is skipped.
// The authoritative deduplication is the DB UNIQUE(trigger_id, idem_key)
// constraint — the lock is a best-effort optimisation to reduce wasted work
// on multi-replica deployments.
func (ct *CronTrigger) fire(triggerID, jobID string) {
	scheduledSlot := time.Now().Truncate(time.Minute)
	lockKey := fmt.Sprintf("cron:fire:%s:%d", triggerID, scheduledSlot.Unix())

	// Best-effort lock: if another instance already holds it, skip.
	// Fall open on error so a Redis outage doesn't silence cron fires.
	if ok, err := ct.lock.TryLock(context.Background(), lockKey, 65*time.Second); err != nil {
		ct.logger.Warn("cron: fire lock error (falling open)", "trigger_id", triggerID, "error", err)
	} else if !ok {
		ct.logger.Debug(
			"cron: fire skipped — another instance holds the lock",
			"trigger_id", triggerID,
			"job_id", jobID,
			"slot", scheduledSlot.Unix(),
		)
		return
	}
	defer func() {
		if err := ct.lock.Unlock(context.Background(), lockKey); err != nil {
			ct.logger.Warn("cron: fire unlock error", "trigger_id", triggerID, "error", err)
		}
	}()

	act := Activation{
		TriggerID:      triggerID,
		JobID:          jobID,
		IdempotencyKey: fmt.Sprintf("%s:%d", triggerID, scheduledSlot.Unix()),
		Payload:        nil, // cron triggers carry no payload
		ReceivedAt:     time.Now(),
	}

	ct.mu.RLock()
	fn := ct.fireFunc
	ct.mu.RUnlock()

	if fn == nil {
		ct.logger.Warn(
			"cron: no FireFunc registered, dropping activation",
			"trigger_id", triggerID,
			"job_id", jobID,
		)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := fn(ctx, act); err != nil {
		ct.logger.Error(
			"cron: FireFunc failed",
			"trigger_id", triggerID,
			"job_id", jobID,
			"idempotency_key", act.IdempotencyKey,
			"error", err,
		)
	}
}
