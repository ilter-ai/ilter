package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"github.com/pressly/goose/v3"
)

//go:generate go tool sqlc generate

//go:embed migrations/*.sql
var migrationFS embed.FS

// Migrate runs all pending schema migrations using goose.
func (s *SQLiteStore) Migrate() error {
	// Drop old hand-rolled schema_migrations table if it exists.
	// Goose uses its own goose_db_version table with a different schema.
	if _, err := s.DB.Exec("DROP TABLE IF EXISTS schema_migrations"); err != nil {
		return fmt.Errorf("drop old schema_migrations: %w", err)
	}

	subFS, err := fs.Sub(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("create migration sub-filesystem: %w", err)
	}

	provider, err := goose.NewProvider(
		goose.DialectSQLite3,
		s.DB,
		subFS,
	)
	if err != nil {
		return fmt.Errorf("create goose provider: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results, err := provider.Up(ctx)
	if err != nil {
		if partialErr, ok := errors.AsType[*goose.PartialError](err); ok {
			slog.Error(
				"migration failed",
				"applied", len(partialErr.Applied),
				"version", partialErr.Failed.Source.Version,
				"error", partialErr.Err,
			)
			return fmt.Errorf("migration version %d failed: %w", partialErr.Failed.Source.Version, partialErr.Err)
		}
		return fmt.Errorf("migration up: %w", err)
	}

	for _, r := range results {
		if r.Error != nil {
			slog.Error("migration errored", "version", r.Source.Version, "error", r.Error)
			continue
		}
		slog.Info("applied migration", "version", r.Source.Version, "duration", r.Duration)
	}

	return nil
}

// ApplyMigrations ensures all database tables and schema migrations are applied.
// This is kept to support tests that set up a transient database.
func ApplyMigrations(db *sql.DB) error {
	s := &SQLiteStore{DB: db}
	return s.Migrate()
}
