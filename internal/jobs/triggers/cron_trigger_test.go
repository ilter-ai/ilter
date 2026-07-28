package triggers

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/jobs"
)

// setupTestDB creates a fresh SQLite database in a temp directory with the
// triggers table created. Returns the DB handle and a cleanup function.
func setupTestDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "ilter-triggers-test-*")
	require.NoError(t, err)

	dbPath := filepath.Join(tmpDir, "test.db")
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("open sqlite: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS triggers (
			id TEXT PRIMARY KEY,
			job_id TEXT NOT NULL,
			kind TEXT NOT NULL CHECK(kind IN ('cron', 'webhook')),
			enabled INTEGER NOT NULL DEFAULT 1,
			config TEXT NOT NULL DEFAULT '{}',
			token TEXT,
			secret_hash TEXT,
			last_used_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		db.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("create triggers table: %v", err)
	}

	// ListEnabled() joins against jobs.enabled, so cron scheduling tests need
	// a (minimal) jobs table too — see insertTrigger, which upserts a row here.
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS jobs (
			id TEXT PRIMARY KEY,
			enabled INTEGER NOT NULL DEFAULT 1
		)
	`)
	if err != nil {
		db.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("create jobs table: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}
	return db, cleanup
}

// insertTrigger inserts a trigger row for testing, upserting an enabled
// parent job row first (ListEnabled() requires one — see setupTestDB).
func insertTrigger(t *testing.T, db *sql.DB, id, jobID, kind string, enabled bool, config string) {
	t.Helper()
	en := 0
	if enabled {
		en = 1
	}
	_, err := db.Exec(`INSERT OR IGNORE INTO jobs (id, enabled) VALUES (?, 1)`, jobID)
	require.NoError(t, err, "insert job %s", jobID)
	_, err = db.Exec(
		`INSERT INTO triggers (id, job_id, kind, enabled, config, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, datetime('now'), datetime('now'))`,
		id, jobID, kind, en, config,
	)
	require.NoError(t, err, "insert trigger %s", id)
}

func TestNewCronTrigger(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	ct := NewCronTrigger(store, logger, jobs.NewLocalLock())
	require.NotNil(t, ct)
	require.NotNil(t, ct.cron)
	require.NotNil(t, ct.store)
	require.NotNil(t, ct.logger)
	assert.Empty(t, ct.entries)
	assert.Empty(t, ct.trigger)
	assert.Nil(t, ct.fireFunc)

	ct.Stop() // clean stop of empty scheduler
}

func TestCronTrigger_RegisterAndFire(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// Insert a cron trigger with a valid expression.
	insertTrigger(t, db, "trig-1", "job-1", "cron", true, `{"expr":"0 * * * *","timezone":"UTC"}`)

	ct := NewCronTrigger(store, logger, jobs.NewLocalLock())

	// Collect activations via channel.
	activations := make(chan Activation, 4)
	fireFunc := func(_ context.Context, act Activation) error {
		activations <- act
		return nil
	}
	ct.SetFireFunc(fireFunc)

	// Simulate a cron fire by calling the internal method.
	ct.fire("trig-1", "job-1")

	select {
	case act := <-activations:
		assert.Equal(t, "trig-1", act.TriggerID)
		assert.Equal(t, "job-1", act.JobID)
		assert.Nil(t, act.Payload)
		assert.Contains(t, act.IdempotencyKey, "trig-1:")
		assert.False(t, act.ReceivedAt.IsZero())
	case <-time.After(time.Second):
		t.Fatal("expected activation within 1s")
	}
}

func TestCronTrigger_FireReturnsError(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	ct := NewCronTrigger(store, logger, jobs.NewLocalLock())
	ct.SetFireFunc(func(_ context.Context, _ Activation) error {
		return fmt.Errorf("simulated error")
	})

	// Should not panic; error is logged internally.
	ct.fire("trig-err", "job-err")
}

func TestCronTrigger_FireWithNoFireFunc(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	ct := NewCronTrigger(store, logger, jobs.NewLocalLock())
	// No fireFunc set — should log warning and not panic.
	ct.fire("trig-nil", "job-nil")
}

func TestCronTrigger_IdempotencyKeyFormat(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	ct := NewCronTrigger(store, logger, jobs.NewLocalLock())

	activations := make(chan Activation, 4)
	ct.SetFireFunc(func(_ context.Context, act Activation) error {
		activations <- act
		return nil
	})

	// Fire twice in the same minute — idempotency keys should match.
	ct.fire("trig-idem", "job-idem")
	ct.fire("trig-idem", "job-idem")

	act1 := <-activations
	act2 := <-activations

	assert.Equal(t, act1.IdempotencyKey, act2.IdempotencyKey, "same-minute fires should share idempotency key")

	// Key format: triggerID:unixTimestamp
	assert.Contains(t, act1.IdempotencyKey, "trig-idem:")
}

func TestCronTrigger_StartStop(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// Insert a valid cron trigger.
	insertTrigger(t, db, "trig-2", "job-2", "cron", true, `{"expr":"0 * * * *","timezone":"UTC"}`)

	ct := NewCronTrigger(store, logger, jobs.NewLocalLock())

	// Crank the fire counter synchronously via a wrapper.
	var fireCount atomic.Int32
	ct.SetFireFunc(func(_ context.Context, _ Activation) error {
		fireCount.Add(1)
		return nil
	})

	ctx := context.Background()
	err := ct.Start(ctx)
	require.NoError(t, err)

	// Verify the trigger is registered.
	ct.mu.RLock()
	assert.Len(t, ct.entries, 1)
	assert.Len(t, ct.trigger, 1)
	// Verify mapping is correct.
	triggerID, ok := ct.entries[ct.trigger["trig-2"]]
	assert.True(t, ok)
	assert.Equal(t, "trig-2", triggerID)
	ct.mu.RUnlock()

	// Stop.
	ct.Stop()
	assert.Len(t, ct.entries, 1) // entries map is not cleared on stop, only on refresh
}

func TestCronTrigger_StartSkipsNonCron(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// Insert both a cron and a webhook trigger (both enabled).
	insertTrigger(t, db, "trig-cron", "job-1", "cron", true, `{"expr":"0 * * * *","timezone":"UTC"}`)
	insertTrigger(t, db, "trig-webhook", "job-2", "webhook", true, `{"provider":"generic"}`)

	ct := NewCronTrigger(store, logger, jobs.NewLocalLock())
	ct.SetFireFunc(func(_ context.Context, _ Activation) error { return nil })

	err := ct.Start(context.Background())
	require.NoError(t, err)
	defer ct.Stop()

	ct.mu.RLock()
	assert.Len(t, ct.entries, 1, "only cron triggers should be registered")
	_, cronExists := ct.trigger["trig-cron"]
	assert.True(t, cronExists, "cron trigger should be registered")
	_, webhookExists := ct.trigger["trig-webhook"]
	assert.False(t, webhookExists, "webhook trigger should NOT be registered")
	ct.mu.RUnlock()
}

func TestCronTrigger_StartSkipsInvalidExpression(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// Insert a trigger with an invalid cron expression.
	insertTrigger(t, db, "trig-bad", "job-bad", "cron", true, `{"expr":"not-a-cron","timezone":"UTC"}`)
	// Insert a valid one too.
	insertTrigger(t, db, "trig-good", "job-good", "cron", true, `{"expr":"0 * * * *","timezone":"UTC"}`)

	ct := NewCronTrigger(store, logger, jobs.NewLocalLock())
	ct.SetFireFunc(func(_ context.Context, _ Activation) error { return nil })

	err := ct.Start(context.Background())
	require.NoError(t, err)
	defer ct.Stop()

	ct.mu.RLock()
	assert.Len(t, ct.entries, 1, "only the valid trigger should be registered")
	_, badExists := ct.trigger["trig-bad"]
	assert.False(t, badExists, "invalid trigger should NOT be registered")
	_, goodExists := ct.trigger["trig-good"]
	assert.True(t, goodExists, "valid trigger should be registered")
	ct.mu.RUnlock()
}

func TestCronTrigger_StartSkipsEmptyExpression(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	insertTrigger(t, db, "trig-empty", "job-empty", "cron", true, `{"expr":""}`)

	ct := NewCronTrigger(store, logger, jobs.NewLocalLock())
	ct.SetFireFunc(func(_ context.Context, _ Activation) error { return nil })

	err := ct.Start(context.Background())
	require.NoError(t, err)
	defer ct.Stop()

	ct.mu.RLock()
	assert.Len(t, ct.entries, 0, "trigger with empty expression should be skipped")
	ct.mu.RUnlock()
}

func TestCronTrigger_Refresh(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// Start with one trigger.
	insertTrigger(t, db, "trig-1", "job-1", "cron", true, `{"expr":"0 * * * *","timezone":"UTC"}`)

	ct := NewCronTrigger(store, logger, jobs.NewLocalLock())
	ct.SetFireFunc(func(_ context.Context, _ Activation) error { return nil })

	err := ct.Start(context.Background())
	require.NoError(t, err)

	ct.mu.RLock()
	assert.Len(t, ct.entries, 1, "first load should have 1 trigger")
	ct.mu.RUnlock()

	// Add a second trigger to the DB.
	insertTrigger(t, db, "trig-2", "job-2", "cron", true, `{"expr":"*/30 * * * *","timezone":"UTC"}`)

	// Refresh — should pick up the new trigger.
	err = ct.Refresh(context.Background())
	require.NoError(t, err)

	ct.mu.RLock()
	assert.Len(t, ct.entries, 2, "after refresh should have 2 triggers")
	_, t1 := ct.trigger["trig-1"]
	_, t2 := ct.trigger["trig-2"]
	ct.mu.RUnlock()
	assert.True(t, t1, "trig-1 should be registered")
	assert.True(t, t2, "trig-2 should be registered")
}

func TestCronTrigger_RefreshRemovesDeleted(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	store := NewStore(db)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// Start with two triggers.
	insertTrigger(t, db, "trig-1", "job-1", "cron", true, `{"expr":"0 * * * *","timezone":"UTC"}`)
	insertTrigger(t, db, "trig-2", "job-2", "cron", true, `{"expr":"*/30 * * * *","timezone":"UTC"}`)

	ct := NewCronTrigger(store, logger, jobs.NewLocalLock())
	ct.SetFireFunc(func(_ context.Context, _ Activation) error { return nil })

	err := ct.Start(context.Background())
	require.NoError(t, err)
	assert.Len(t, ct.entries, 2)

	// Delete one trigger from the DB.
	_, err = db.Exec("DELETE FROM triggers WHERE id = 'trig-1'")
	require.NoError(t, err)

	// Refresh — should only have trig-2.
	err = ct.Refresh(context.Background())
	require.NoError(t, err)

	ct.mu.RLock()
	assert.Len(t, ct.entries, 1)
	_, t2 := ct.trigger["trig-2"]
	ct.mu.RUnlock()
	assert.True(t, t2, "trig-2 should still be registered")
}
