package db

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model/catalog"
)

// Test helpers

// testStore bundles a store, its DB path, and the temp directory for cleanup.
type testStore struct {
	store  *SQLiteStore
	dbPath string
	tmpDir string
}

// setupTestStore creates a new SQLiteStore backed by a temporary file.
func setupTestStore(t *testing.T) testStore {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "ilter-storage-test-*")
	require.NoError(t, err)

	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := NewSQLiteStore(config.StorageConfig{
		Type:       "sqlite",
		SqlitePath: dbPath,
	})
	if err != nil {
		os.RemoveAll(tmpDir)
		require.NoError(t, err)
	}
	return testStore{store: store, dbPath: dbPath, tmpDir: tmpDir}
}

// close cleans up the store and removes the temp directory.
func (ts testStore) close() {
	if ts.store != nil {
		_ = ts.store.Close()
	}
	if ts.tmpDir != "" {
		_ = os.RemoveAll(ts.tmpDir)
	}
}

// Store initialization tests

func TestNewSQLiteStore(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	assert.NotNil(t, ts.store)
	assert.NotNil(t, ts.store.DB)
	assert.NoError(t, ts.store.DB.Ping())
}

func TestNewSQLiteStore_InvalidType(t *testing.T) {
	_, err := NewSQLiteStore(config.StorageConfig{
		Type:       "postgres",
		SqlitePath: ":memory:",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported storage type")
}

func TestNewSQLiteStore_InvalidPath(t *testing.T) {
	_, err := NewSQLiteStore(config.StorageConfig{
		Type:       "sqlite",
		SqlitePath: "/nonexistent/dir/test.db",
	})
	assert.Error(t, err)
}

// Migration tests

func TestMigration_CreatesTables(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	expectedTables := []string{"audit_log", "usage_daily", "loop_events", "pii_events"}
	for _, table := range expectedTables {
		var exists bool
		err := ts.store.DB.QueryRow(
			"SELECT COUNT(*) > 0 FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&exists)
		require.NoError(t, err, "checking existence of table %q", table)
		assert.True(t, exists, "table %q should exist after migration", table)
	}
}

func TestMigration_IsIdempotent(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	err := ts.store.Migrate()
	assert.NoError(t, err, "second migration should be idempotent")

	expectedTables := []string{"audit_log", "usage_daily", "loop_events", "pii_events"}
	for _, table := range expectedTables {
		var exists bool
		err := ts.store.DB.QueryRow(
			"SELECT COUNT(*) > 0 FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&exists)
		require.NoError(t, err, "checking existence of table %q", table)
		assert.True(t, exists, "table %q should still exist after idempotent migration", table)
	}
}

func TestMigration_CreatesTableWithAllColumns(t *testing.T) {
	store := setupTestStore(t)
	defer store.close()

	// Open a separate connection to verify schema.
	db, err := sql.Open("sqlite", "file:"+store.dbPath+"?_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	defer db.Close()

	expectedCols := []string{"complexity_score", "request_body", "response_body", "prompt_preview"}
	for _, col := range expectedCols {
		var exists bool
		err := db.QueryRow(`SELECT COUNT(*) > 0 FROM pragma_table_info('audit_log') WHERE name = ?`, col).Scan(&exists)
		require.NoError(t, err)
		assert.True(t, exists, "audit_log should have column %q at CREATE TABLE level", col)
	}
}

// RecordDailyUsage tests

func TestRecordDailyUsage_InsertAndQuery(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	err := ts.store.RecordDailyUsage("v1", "2026-06-10", "gpt-4o", "openai", 100, 200, 0, 0.0045)
	require.NoError(t, err)

	// Verify the row was inserted correctly
	var tokens, promptTokens, completionTokens, cacheHits, requestCount int
	var cost float64
	var keyID string
	var date, model, provider string
	err = ts.store.DB.QueryRow(
		`SELECT key_id, date, model, provider, tokens, cost, request_count, prompt_tokens, completion_tokens, cache_hits
		 FROM usage_daily WHERE key_id = 'v1' AND date = '2026-06-10'`,
	).Scan(&keyID, &date, &model, &provider, &tokens, &cost, &requestCount, &promptTokens, &completionTokens, &cacheHits)
	require.NoError(t, err)

	assert.Equal(t, "v1", keyID)
	assert.Equal(t, "2026-06-10", date)
	assert.Equal(t, "gpt-4o", model)
	assert.Equal(t, "openai", provider)
	assert.Equal(t, 300, tokens) // 100 + 200
	assert.Equal(t, 0.0045, cost)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, 100, promptTokens)
	assert.Equal(t, 200, completionTokens)
	assert.Equal(t, 0, cacheHits)
}

func TestRecordDailyUsage_UpsertAccumulates(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	// First insert
	err := ts.store.RecordDailyUsage("v1", "2026-06-10", "gpt-4o", "openai", 100, 200, 0, 0.0045)
	require.NoError(t, err)

	// Second insert — same key, should UPSERT and accumulate
	err = ts.store.RecordDailyUsage("v1", "2026-06-10", "gpt-4o", "openai", 50, 100, 2, 0.0020)
	require.NoError(t, err)

	// Verify accumulated values
	var tokens, promptTokens, completionTokens, cacheHits, requestCount int
	var cost float64
	err = ts.store.DB.QueryRow(
		`SELECT tokens, cost, request_count, prompt_tokens, completion_tokens, cache_hits
		 FROM usage_daily WHERE key_id = 'v1' AND date = '2026-06-10' AND model = 'gpt-4o'`,
	).Scan(&tokens, &cost, &requestCount, &promptTokens, &completionTokens, &cacheHits)
	require.NoError(t, err)

	assert.Equal(t, 450, tokens)  // (100+200) + (50+100) = 450
	assert.Equal(t, 0.0065, cost) // 0.0045 + 0.0020
	assert.Equal(t, 2, requestCount)
	assert.Equal(t, 150, promptTokens)     // 100 + 50
	assert.Equal(t, 300, completionTokens) // 200 + 100
	assert.Equal(t, 2, cacheHits)          // 0 + 2
}

func TestRecordDailyUsage_MultipleKeyCombos(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	// Different key_id
	err := ts.store.RecordDailyUsage("v1", "2026-06-10", "gpt-4o", "openai", 100, 200, 0, 0.0045)
	require.NoError(t, err)

	err = ts.store.RecordDailyUsage("v2", "2026-06-10", "gpt-4o", "openai", 50, 100, 0, 0.0020)
	require.NoError(t, err)

	// Different date
	err = ts.store.RecordDailyUsage("v1", "2026-06-11", "gpt-4o", "openai", 30, 60, 0, 0.0010)
	require.NoError(t, err)

	// Different model
	err = ts.store.RecordDailyUsage("v1", "2026-06-10", "claude-3-sonnet", "anthropic", 200, 400, 0, 0.0090)
	require.NoError(t, err)

	// All 4 rows should exist independently
	var count int
	err = ts.store.DB.QueryRow("SELECT COUNT(*) FROM usage_daily").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 4, count)
}

func TestRecordDailyUsage_ZeroValues(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	err := ts.store.RecordDailyUsage("v1", "2026-06-10", "gpt-4o", "openai", 0, 0, 0, 0.0)
	require.NoError(t, err)

	var requestCount int
	err = ts.store.DB.QueryRow(
		"SELECT request_count FROM usage_daily WHERE key_id = 'v1'",
	).Scan(&requestCount)
	require.NoError(t, err)
	assert.Equal(t, 1, requestCount, "even with zero tokens, request_count should be 1")
}

func TestStore_Close(t *testing.T) {
	ts := setupTestStore(t)
	defer func() {
		_ = os.RemoveAll(ts.tmpDir)
	}()

	// Closing the store should succeed
	err := ts.store.Close()
	assert.NoError(t, err)

	// The store is already closed; we already defer the dir removal above
	// and the store.Close is already called above. We just used defer on
	// dir removal to ensure cleanup even if assertions fail.
}

func TestSaveDiscoveredModels(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	models := []catalog.ModelInfo{
		{ID: "model-a", Provider: "test-provider", DisplayName: "Model A", Tier: "free", CostPerInputToken: 0, CostPerOutputToken: 0},
		{ID: "model-b", Provider: "test-provider", DisplayName: "Model B", Tier: "economy", CostPerInputToken: 0.0001, CostPerOutputToken: 0.0002},
	}

	err := ts.store.SaveDiscoveredModels("test-provider", models)
	require.NoError(t, err)

	all, err := ts.store.GetAllProviderModels()
	require.NoError(t, err)
	assert.Len(t, all, 2)

	provModels, err := ts.store.GetProviderModels("test-provider")
	require.NoError(t, err)
	require.Len(t, provModels, 2)
	assert.Equal(t, "model-a", provModels[0].Model)
	assert.True(t, provModels[0].Active)
	assert.Equal(t, "free", provModels[0].Tier)
	assert.Equal(t, 0.0, provModels[0].CostIn)

	models2 := []catalog.ModelInfo{
		{ID: "model-c", Provider: "test-provider", Tier: "standard"},
	}
	err = ts.store.SaveDiscoveredModels("test-provider", models2)
	require.NoError(t, err)

	all2, err := ts.store.GetAllProviderModels()
	require.NoError(t, err)
	assert.Len(t, all2, 3)
	assert.Equal(t, "model-c", all2[2].Model)
}

func TestGetActiveProviderModels(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	models := []catalog.ModelInfo{
		{ID: "active-model", Provider: "prov1", Tier: "free"},
		{ID: "inactive-model", Provider: "prov1", Tier: "economy"},
	}
	err := ts.store.SaveDiscoveredModels("prov1", models)
	require.NoError(t, err)

	err = ts.store.SaveModelStatus("inactive-model", false)
	require.NoError(t, err)

	active, err := ts.store.GetActiveProviderModels()
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, "active-model", active[0].Model)

	// GetInactiveModels should return the deactivated one.
	inactive, err := ts.store.GetInactiveModels()
	require.NoError(t, err)
	assert.Contains(t, inactive, "inactive-model")
}

func TestProviderModelCount(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	count, err := ts.store.ProviderModelCount("nonexistent")
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	models := []catalog.ModelInfo{
		{ID: "m1", Provider: "prov2", Tier: "free"},
		{ID: "m2", Provider: "prov2", Tier: "standard"},
	}
	_ = ts.store.SaveDiscoveredModels("prov2", models)

	count2, err := ts.store.ProviderModelCount("prov2")
	require.NoError(t, err)
	assert.Equal(t, 2, count2)
}

func TestGetModelStatusesWithProviderModels(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	models := []catalog.ModelInfo{
		{ID: "model-x", Provider: "p", Tier: "free"},
		{ID: "model-y", Provider: "p", Tier: "economy"},
	}
	err := ts.store.SaveDiscoveredModels("p", models)
	require.NoError(t, err)

	_ = ts.store.SaveModelStatus("model-y", false)

	statuses, err := ts.store.GetModelStatuses()
	require.NoError(t, err)
	assert.True(t, statuses["model-x"])
	assert.False(t, statuses["model-y"])
}
