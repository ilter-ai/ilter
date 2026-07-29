package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // SQLite driver registration

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db/sqlc"
)

type SQLiteStore struct {
	DB      *sql.DB
	queries *sqlc.Queries
}

func NewSQLiteStore(cfg config.StorageConfig) (*SQLiteStore, error) {
	if cfg.Type != "sqlite" {
		return nil, fmt.Errorf("unsupported storage type: %s", cfg.Type)
	}

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(cfg.SqlitePath), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create sqlite directory %q: %w", filepath.Dir(cfg.SqlitePath), err)
	}

	// _time_format=datetime: modernc.org/sqlite defaults to Go's time.Time.String()
	// (e.g. "2026-07-26 15:13:23.022675 +0300 +03 m=+1491.571096001") when writing
	// bound time.Time/sql.NullTime parameters, which SQLite's own date functions
	// (unixepoch(), datetime()) cannot parse — any WHERE clause comparing such a
	// column silently never matches. This forces the driver to write
	// "YYYY-MM-DD HH:MM:SS", matching the format datetime('now') itself produces.
	// _timezone=UTC: that written string carries no zone offset, and SQLite's date
	// functions always interpret zone-less strings as UTC. Without this, a
	// time.Time in local server time (e.g. +03:00) gets written as local wall-clock
	// digits but read back as UTC, skewing every stored timestamp by the server's
	// UTC offset relative to datetime('now').
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_time_format=datetime&_timezone=UTC",
		cfg.SqlitePath,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db: %w", err)
	}

	if cfg.SqlitePath == ":memory:" {
		// An in-memory SQLite database is private to the single connection
		// that created it — database/sql's connection pool opening a
		// SECOND connection (which it will, under any concurrent access
		// from more than one goroutine, e.g. a background task goroutine
		// alongside the test's main goroutine) gets a completely separate,
		// empty database with no schema at all ("no such table" errors
		// that only reproduce under concurrency, never in a purely
		// sequential test). Forcing a single pooled connection makes every
		// query — from any goroutine — hit the same underlying database.
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	} else {
		db.SetMaxOpenConns(25)
		db.SetMaxIdleConns(5)
	}
	db.SetConnMaxLifetime(15 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping sqlite db: %w", err)
	}

	store := &SQLiteStore{
		DB:      db,
		queries: sqlc.New(db),
	}
	if err := store.Migrate(); err != nil {
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	return store, nil
}

// NewSQLiteStoreFromDB wraps an already-open *sql.DB (migrated and connected
// by the caller) as a SQLiteStore. Used where a connection's lifecycle is
// owned elsewhere (e.g. the seed package), so NewSQLiteStore's own
// open+migrate sequence would be redundant or wrong.
func NewSQLiteStoreFromDB(db *sql.DB) *SQLiteStore {
	return &SQLiteStore{DB: db, queries: sqlc.New(db)}
}

func (s *SQLiteStore) Close() error {
	return s.DB.Close()
}

// RecordDailyUsage uses key_id (TEXT) to record usage in usage_daily.
// The key_id is the API key ID (e.g. "abc123" or "legacy_5").
func (s *SQLiteStore) RecordDailyUsage(keyID string, date, model, provider string, promptTokens, completionTokens, cacheHits int, cost float64) error {
	totalTokens := int64(promptTokens + completionTokens)
	prompt := int64(promptTokens)
	completion := int64(completionTokens)
	hits := int64(cacheHits)

	return s.queries.RecordDailyUsage(context.Background(), sqlc.RecordDailyUsageParams{
		KeyID:            &keyID,
		Date:             &date,
		Model:            &model,
		Provider:         &provider,
		Tokens:           &totalTokens,
		Cost:             &cost,
		PromptTokens:     &prompt,
		CompletionTokens: &completion,
		CacheHits:        &hits,
	})
}
