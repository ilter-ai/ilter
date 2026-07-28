package db

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/config"
)

func TestInitConfigResolvers_Hierarchy(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	// 1. Initialize DB-backed config resolvers
	InitConfigResolvers(ts.store)

	db := ts.store.DB

	// Seed organizations and teams
	_, err := db.Exec("INSERT INTO orgs (id, name) VALUES ('org-1', 'Org 1')")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO teams (id, name) VALUES ('team-1', 'Team 1')")
	require.NoError(t, err)

	// Create test API key linked to team-1 and org-1
	vk, _, err := ts.store.CreateAPIKey("test-key-resolver", nil, nil, 100, 0, 60, 0, nil, nil, nil)
	require.NoError(t, err)
	_, err = db.Exec("UPDATE api_keys SET team_id = 'team-1', org_id = 'org-1' WHERE id = ?", vk.ID)
	require.NoError(t, err)

	// Helpers to seed settings
	insertSetting := func(scope, scopeID, field string, val any) {
		valJSON, err := json.Marshal(val)
		require.NoError(t, err)
		_, err = db.Exec(`
			INSERT INTO config_settings (scope, scope_id, field, value)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(scope, scope_id, field) DO UPDATE SET value = excluded.value
		`, scope, scopeID, field, string(valJSON))
		require.NoError(t, err)
	}

	clearSettings := func() {
		_, _ = db.Exec("DELETE FROM config_settings")
	}

	t.Run("all scopes set — perKey wins", func(t *testing.T) {
		clearSettings()
		insertSetting("key", vk.ID, "test.field", "key-val")
		insertSetting("team", "team-1", "test.field", "team-val")
		insertSetting("org", "org-1", "test.field", "org-val")
		insertSetting("global", "", "test.field", "global-val")

		val := config.Resolve(vk.ID, "test.field")
		assert.Equal(t, "key-val", val)
	})

	t.Run("team wins when key unset", func(t *testing.T) {
		clearSettings()
		insertSetting("team", "team-1", "test.field", "team-val")
		insertSetting("org", "org-1", "test.field", "org-val")
		insertSetting("global", "", "test.field", "global-val")

		val := config.Resolve(vk.ID, "test.field")
		assert.Equal(t, "team-val", val)
	})

	t.Run("org wins when key and team unset", func(t *testing.T) {
		clearSettings()
		insertSetting("org", "org-1", "test.field", "org-val")
		insertSetting("global", "", "test.field", "global-val")

		val := config.Resolve(vk.ID, "test.field")
		assert.Equal(t, "org-val", val)
	})

	t.Run("global wins as last resort", func(t *testing.T) {
		clearSettings()
		insertSetting("global", "", "test.field", "global-val")

		val := config.Resolve(vk.ID, "test.field")
		assert.Equal(t, "global-val", val)
	})

	t.Run("returns nil when all unset", func(t *testing.T) {
		clearSettings()
		val := config.Resolve(vk.ID, "test.field")
		assert.Nil(t, val)
	})

	t.Run("falsy value (false) beats global (true)", func(t *testing.T) {
		clearSettings()
		insertSetting("team", "team-1", "test.boolean", false)
		insertSetting("global", "", "test.boolean", true)

		val := config.Resolve(vk.ID, "test.boolean")
		assert.Equal(t, false, val)
	})

	t.Run("falsy value (0.0) beats global (10.0)", func(t *testing.T) {
		clearSettings()
		insertSetting("org", "org-1", "test.float", 0.0)
		insertSetting("global", "", "test.float", 10.0)

		val := config.Resolve(vk.ID, "test.float")
		assert.Equal(t, 0.0, val)
	})

	t.Run("key with no team and org falls back to global", func(t *testing.T) {
		clearSettings()
		// Create a key with NULL team and org
		vk2, _, err := ts.store.CreateAPIKey("test-key-no-owner", nil, nil, 100, 0, 60, 0, nil, nil, nil)
		require.NoError(t, err)

		insertSetting("team", "team-1", "test.field", "team-val")
		insertSetting("global", "", "test.field", "global-val")

		val := config.Resolve(vk2.ID, "test.field")
		assert.Equal(t, "global-val", val)
	})
}
