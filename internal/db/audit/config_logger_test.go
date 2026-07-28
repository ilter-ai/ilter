package audit

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestDB creates a fresh SQLite database with the config_audit_log table.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "ilter-audit-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	dbPath := filepath.Join(tmpDir, "audit.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS config_audit_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			entity_type TEXT NOT NULL,
			entity_id TEXT NOT NULL,
			action TEXT NOT NULL CHECK(action IN ('create','update','delete')),
			old_values TEXT,
			new_values TEXT,
			performed_by TEXT,
			performed_at TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`)
	require.NoError(t, err)

	return db
}

func TestSQLiteConfigAuditor_LogCreate(t *testing.T) {
	db := setupTestDB(t)
	auditor := NewSQLiteConfigAuditor(db)

	err := auditor.LogCreate("api_key", "key_abc", map[string]any{
		"name":    "test-key",
		"api_key": "sk-1234567890abcdef",
	}, "admin@example.com")
	require.NoError(t, err)

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM config_audit_log WHERE entity_type = ? AND entity_id = ?", "api_key", "key_abc").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	var action, newVals, performedBy string
	var oldVals sql.NullString
	err = db.QueryRow(
		"SELECT action, old_values, new_values, performed_by FROM config_audit_log WHERE entity_type = ? AND entity_id = ?",
		"api_key", "key_abc",
	).Scan(&action, &oldVals, &newVals, &performedBy)
	require.NoError(t, err)
	assert.Equal(t, "create", action)
	assert.False(t, oldVals.Valid, "old_values should be NULL for create")
	assert.Equal(t, "admin@example.com", performedBy)

	var parsed map[string]any
	err = json.Unmarshal([]byte(newVals), &parsed)
	require.NoError(t, err)
	assert.Equal(t, "test-key", parsed["name"])
	assert.Equal(t, "***", parsed["api_key"], "api_key value must be masked")
}

func TestSQLiteConfigAuditor_LogUpdate(t *testing.T) {
	db := setupTestDB(t)
	auditor := NewSQLiteConfigAuditor(db)

	err := auditor.LogUpdate(
		"provider_config", "openai",
		map[string]any{
			"base_url": "https://old.api.com",
			"api_key":  "sk-old-secret",
		},
		map[string]any{
			"base_url": "https://new.api.com",
			"api_key":  "sk-new-secret",
		},
		"admin@example.com",
	)
	require.NoError(t, err)

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM config_audit_log").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	var action, oldVals, newVals string
	err = db.QueryRow(
		"SELECT action, old_values, new_values FROM config_audit_log WHERE entity_type = ? AND entity_id = ?",
		"provider_config", "openai",
	).Scan(&action, &oldVals, &newVals)
	require.NoError(t, err)
	assert.Equal(t, "update", action)

	var oldParsed, newParsed map[string]any
	err = json.Unmarshal([]byte(oldVals), &oldParsed)
	require.NoError(t, err)
	err = json.Unmarshal([]byte(newVals), &newParsed)
	require.NoError(t, err)

	assert.Equal(t, "https://old.api.com", oldParsed["base_url"])
	assert.Equal(t, "***", oldParsed["api_key"])
	assert.Equal(t, "https://new.api.com", newParsed["base_url"])
	assert.Equal(t, "***", newParsed["api_key"])
}

func TestSQLiteConfigAuditor_LogDelete(t *testing.T) {
	db := setupTestDB(t)
	auditor := NewSQLiteConfigAuditor(db)

	err := auditor.LogDelete("mcp_server", "server_xyz", map[string]any{
		"name":     "old-server",
		"auth_key": "supersecret",
	}, "admin@example.com")
	require.NoError(t, err)

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM config_audit_log").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	var action, oldVals, performedBy string
	err = db.QueryRow(
		"SELECT action, old_values, performed_by FROM config_audit_log WHERE entity_type = ? AND entity_id = ?",
		"mcp_server", "server_xyz",
	).Scan(&action, &oldVals, &performedBy)
	require.NoError(t, err)
	assert.Equal(t, "delete", action)
	assert.Equal(t, "admin@example.com", performedBy)

	var parsed map[string]any
	err = json.Unmarshal([]byte(oldVals), &parsed)
	require.NoError(t, err)
	assert.Equal(t, "old-server", parsed["name"])
	assert.Equal(t, "***", parsed["auth_key"])
}

func TestSecretMasking_MultiplePatterns(t *testing.T) {
	db := setupTestDB(t)
	auditor := NewSQLiteConfigAuditor(db)

	err := auditor.LogCreate(
		"config", "test",
		map[string]any{
			"name":           "test-item",
			"api_key":        "sk-abc",
			"secret_key":     "sec-123",
			"auth_token":     "tok-xyz",
			"password":       "p@ssw0rd",
			"super_secret":   "hidden",
			"normal_field":   "visible",
			"api_key_suffix": "also-hidden",
		},
		"",
	)
	require.NoError(t, err)

	var newVals string
	err = db.QueryRow("SELECT new_values FROM config_audit_log").Scan(&newVals)
	require.NoError(t, err)

	var parsed map[string]any
	err = json.Unmarshal([]byte(newVals), &parsed)
	require.NoError(t, err)

	// Non-sensitive fields preserved
	assert.Equal(t, "test-item", parsed["name"])
	assert.Equal(t, "visible", parsed["normal_field"])

	// Sensitive fields masked
	assert.Equal(t, "***", parsed["api_key"])
	assert.Equal(t, "***", parsed["secret_key"])
	assert.Equal(t, "***", parsed["auth_token"])
	assert.Equal(t, "***", parsed["password"])
	assert.Equal(t, "***", parsed["super_secret"])
	assert.Equal(t, "***", parsed["api_key_suffix"])
}

func TestNullValues(t *testing.T) {
	db := setupTestDB(t)
	auditor := NewSQLiteConfigAuditor(db)

	// nil values map should be stored as "null"
	err := auditor.LogCreate("provider", "test", nil, "")
	require.NoError(t, err)

	var newVals string
	err = db.QueryRow("SELECT new_values FROM config_audit_log").Scan(&newVals)
	require.NoError(t, err)
	assert.Equal(t, "null", newVals)
}

func TestMultipleEntries(t *testing.T) {
	db := setupTestDB(t)
	auditor := NewSQLiteConfigAuditor(db)

	require.NoError(t, auditor.LogCreate("api_key", "key_1", map[string]any{"name": "key1"}, "user1"))
	require.NoError(t, auditor.LogCreate("api_key", "key_2", map[string]any{"name": "key2"}, "user2"))
	require.NoError(t, auditor.LogUpdate("api_key", "key_1", map[string]any{"name": "key1"}, map[string]any{"name": "key1-updated"}, "user1"))
	require.NoError(t, auditor.LogDelete("api_key", "key_2", map[string]any{"name": "key2"}, "user2"))

	var total int
	err := db.QueryRow("SELECT COUNT(*) FROM config_audit_log").Scan(&total)
	require.NoError(t, err)
	assert.Equal(t, 4, total)

	var creates, updates, deletes int
	err = db.QueryRow("SELECT COUNT(*) FROM config_audit_log WHERE action = 'create'").Scan(&creates)
	require.NoError(t, err)
	err = db.QueryRow("SELECT COUNT(*) FROM config_audit_log WHERE action = 'update'").Scan(&updates)
	require.NoError(t, err)
	err = db.QueryRow("SELECT COUNT(*) FROM config_audit_log WHERE action = 'delete'").Scan(&deletes)
	require.NoError(t, err)
	assert.Equal(t, 2, creates)
	assert.Equal(t, 1, updates)
	assert.Equal(t, 1, deletes)
}
