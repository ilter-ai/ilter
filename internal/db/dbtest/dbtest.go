// Package dbtest centralizes SQLiteStore test setup so callers stop
// hand-rolling db.NewSQLiteStore(config.StorageConfig{...}) + cleanup in
// every _test.go file.
package dbtest

import (
	"path/filepath"
	"testing"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
)

// New creates an in-memory SQLiteStore with all migrations applied.
// Registers t.Cleanup to close it. Fails the test on error.
//
// Use this by default. Reach for NewFile only when a test genuinely needs a
// real file path (e.g. exercising multiple *sql.DB handles against the same
// database, or WAL-file-on-disk behavior).
func New(t testing.TB) *db.SQLiteStore {
	t.Helper()
	store, err := db.NewSQLiteStore(config.StorageConfig{Type: "sqlite", SqlitePath: ":memory:"})
	if err != nil {
		t.Fatalf("dbtest.New: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// NewFile creates a file-backed SQLiteStore in a fresh temp directory (removed
// automatically at test end via t.TempDir), with all migrations applied.
// Registers t.Cleanup to close the store. Fails the test on error.
func NewFile(t testing.TB) *db.SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := db.NewSQLiteStore(config.StorageConfig{Type: "sqlite", SqlitePath: dbPath})
	if err != nil {
		t.Fatalf("dbtest.NewFile: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
