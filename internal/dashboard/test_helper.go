package dashboard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
)

func setupTestStore(t *testing.T) (*db.SQLiteStore, string) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "ilter-admin-test-*")
	require.NoError(t, err)
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := db.NewSQLiteStore(config.StorageConfig{
		Type:       "sqlite",
		SqlitePath: dbPath,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		store.Close()
		os.RemoveAll(tmpDir)
	})
	return store, tmpDir
}
