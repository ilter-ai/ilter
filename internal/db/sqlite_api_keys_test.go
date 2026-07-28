package db

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/auth"
)

// ---------------------------------------------------------------------------
// CreateAPIKey
// ---------------------------------------------------------------------------

func TestCreateAPIKey_Basic(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	key, token, err := ts.store.CreateAPIKey("my-key", nil, nil, 100.0, 1000, 10, 50000, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, key)

	assert.Equal(t, "my-key", key.Name)
	assert.NotEmpty(t, key.ID)
	assert.True(t, strings.HasPrefix(token, "ilter_"))
	assert.Empty(t, key.GroupID)
	assert.Empty(t, key.UserID)
	assert.Equal(t, 100.0, key.MonthlyBudgetUSD)
	assert.Equal(t, int64(1000), key.MonthlyBudgetTokens)
	assert.Equal(t, 10, key.RateLimitRPM)
	assert.Equal(t, int64(50000), key.RateLimitTPM)
	assert.True(t, key.Enabled)
	assert.NotZero(t, key.CreatedAt)
	assert.NotZero(t, key.UpdatedAt)

	// Verify the returned key's ID matches the token prefix (first 12 chars).
	assert.Equal(t, token[:12], key.ID)
}

func TestCreateAPIKey_WithGroupUser(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	gid := 7
	uid := 42
	key, token, err := ts.store.CreateAPIKey("grouped-key", &gid, &uid, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, key)

	assert.Equal(t, 7, *key.GroupID)
	assert.Equal(t, 42, *key.UserID)
	assert.Equal(t, token[:12], key.ID)
}

func TestCreateAPIKey_WithTags(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	tags := map[string]string{"env": "prod", "team": "ml"}
	key, _, err := ts.store.CreateAPIKey("tagged-key", nil, nil, 0, 0, 0, 0, nil, nil, tags)
	require.NoError(t, err)
	require.NotNil(t, key)

	assert.Equal(t, "prod", key.Tags["env"])
	assert.Equal(t, "ml", key.Tags["team"])
}

func TestCreateAPIKey_WithAllowedModelsProviders(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	models := []string{"gpt-4", "gpt-3.5-turbo"}
	providers := []string{"openai", "anthropic"}
	key, _, err := ts.store.CreateAPIKey("restricted-key", nil, nil, 0, 0, 0, 0, models, providers, nil)
	require.NoError(t, err)
	require.NotNil(t, key)

	assert.Equal(t, models, key.AllowedModels)
	assert.Equal(t, providers, key.AllowedProviders)
}

func TestCreateAPIKey_UniqueID(t *testing.T) {
	// The UNIQUE constraint is on id (token prefix), not name.
	// Two keys with the same name but different tokens should succeed.
	ts := setupTestStore(t)
	defer ts.close()

	k1, t1, err := ts.store.CreateAPIKey("shared-name", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)
	k2, t2, err := ts.store.CreateAPIKey("shared-name", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	assert.NotEqual(t, k1.ID, k2.ID)
	assert.NotEqual(t, t1, t2)
}

// ---------------------------------------------------------------------------
// GetAPIKey
// ---------------------------------------------------------------------------

func TestGetAPIKey_Found(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	gid := 3
	models := []string{"gpt-4"}
	key, token, err := ts.store.CreateAPIKey("find-me", &gid, nil, 50.0, 500, 5, 10000, models, nil, nil)
	require.NoError(t, err)

	found, err := ts.store.GetAPIKey(key.ID)
	require.NoError(t, err)
	require.NotNil(t, found)

	assert.Equal(t, key.ID, found.ID)
	assert.Equal(t, "find-me", found.Name)
	assert.Equal(t, 3, *found.GroupID)
	assert.Nil(t, found.UserID)
	assert.Equal(t, 50.0, found.MonthlyBudgetUSD)
	assert.Equal(t, int64(500), found.MonthlyBudgetTokens)
	assert.Equal(t, 5, found.RateLimitRPM)
	assert.Equal(t, int64(10000), found.RateLimitTPM)
	assert.Equal(t, models, found.AllowedModels)
	assert.True(t, found.Enabled)
	// raw token should NOT be populated on read
	assert.Empty(t, found.Key)
	assert.Equal(t, token[:12], found.ID)
}

func TestGetAPIKey_NotFound(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	found, err := ts.store.GetAPIKey("nonexistent")
	require.Error(t, err)
	assert.Nil(t, found)
	assert.Contains(t, err.Error(), "not found")
}

// ---------------------------------------------------------------------------
// GetAPIKeyByHash
// ---------------------------------------------------------------------------

func TestGetAPIKeyByHash_Found(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	_, token, err := ts.store.CreateAPIKey("hash-test", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	found, err := ts.store.GetAPIKeyByHash(token)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "hash-test", found.Name)
	assert.Equal(t, token[:12], found.ID)
}

func TestGetAPIKeyByHash_NotFound(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	found, err := ts.store.GetAPIKeyByHash("ilter_bogus0000000000000000000000")
	require.Error(t, err)
	assert.Nil(t, found)
}

// ---------------------------------------------------------------------------
// GetActiveKeyByHash
// ---------------------------------------------------------------------------

func TestGetActiveKeyByHash_Found(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	_, token, err := ts.store.CreateAPIKey("active-test", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	found, err := ts.store.GetActiveKeyByHash(token)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "active-test", found.Name)
	assert.Equal(t, token[:12], found.ID)
	assert.True(t, found.Enabled)
}

func TestGetActiveKeyByHash_NotFound(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	found, err := ts.store.GetActiveKeyByHash("ilter_bogus0000000000000000000000")
	require.Error(t, err)
	assert.Nil(t, found)
}

// ---------------------------------------------------------------------------
// ListAPIKeys
// ---------------------------------------------------------------------------

func TestListAPIKeys_All(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	_, _, err := ts.store.CreateAPIKey("k1", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)
	_, _, err = ts.store.CreateAPIKey("k2", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	keys, err := ts.store.ListAPIKeys()
	require.NoError(t, err)
	assert.Len(t, keys, 2)
	// Both keys present regardless of order.
	names := []string{keys[0].Name, keys[1].Name}
	assert.ElementsMatch(t, []string{"k1", "k2"}, names)
}

func TestListAPIKeys_ByGroup(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	g1 := 1
	g2 := 2
	_, _, err := ts.store.CreateAPIKey("g1-key", &g1, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)
	_, _, err = ts.store.CreateAPIKey("g2-key", &g2, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)
	_, _, err = ts.store.CreateAPIKey("also-g1", &g1, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	keys, err := ts.store.ListAPIKeys(1)
	require.NoError(t, err)
	require.Len(t, keys, 2)
	for _, k := range keys {
		assert.Equal(t, 1, *k.GroupID)
	}
}

func TestListAPIKeys_ByGroup_Empty(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	keys, err := ts.store.ListAPIKeys(99)
	require.NoError(t, err)
	assert.Len(t, keys, 0)
}

func TestListAPIKeys_Empty(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	keys, err := ts.store.ListAPIKeys()
	require.NoError(t, err)
	assert.Len(t, keys, 0)
}

// ---------------------------------------------------------------------------
// UpdateAPIKey
// ---------------------------------------------------------------------------

func TestUpdateAPIKey_Partial(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	key, _, err := ts.store.CreateAPIKey("original", nil, nil, 100.0, 1000, 10, 50000, nil, nil, nil)
	require.NoError(t, err)

	updates := auth.APIKey{Name: "renamed", RateLimitRPM: 20}
	err = ts.store.UpdateAPIKey(key.ID, updates, false, false)
	require.NoError(t, err)

	updated, err := ts.store.GetAPIKey(key.ID)
	require.NoError(t, err)
	assert.Equal(t, "renamed", updated.Name)
	assert.Equal(t, 20, updated.RateLimitRPM)
	// Unchanged fields should keep their values.
	assert.Equal(t, 100.0, updated.MonthlyBudgetUSD)
	assert.Equal(t, int64(1000), updated.MonthlyBudgetTokens)
	assert.Equal(t, int64(50000), updated.RateLimitTPM)
}

func TestUpdateAPIKey_WithGroupUser(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	key, _, err := ts.store.CreateAPIKey("assign-me", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	gid := 10
	uid := 20
	updates := auth.APIKey{GroupID: &gid, UserID: &uid}
	err = ts.store.UpdateAPIKey(key.ID, updates, false, false)
	require.NoError(t, err)

	updated, err := ts.store.GetAPIKey(key.ID)
	require.NoError(t, err)
	assert.Equal(t, 10, *updated.GroupID)
	assert.Equal(t, 20, *updated.UserID)
}

func TestUpdateAPIKey_ClearGroupID(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	gid := 5
	key, _, err := ts.store.CreateAPIKey("clear-group", &gid, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	// clearGroupID=true should NULL the group_id.
	err = ts.store.UpdateAPIKey(key.ID, auth.APIKey{}, true, false)
	require.NoError(t, err)

	updated, err := ts.store.GetAPIKey(key.ID)
	require.NoError(t, err)
	assert.Nil(t, updated.GroupID)
	// UserID was never set so should stay nil
	assert.Nil(t, updated.UserID)
}

func TestUpdateAPIKey_ClearUserID(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	uid := 99
	key, _, err := ts.store.CreateAPIKey("clear-user", nil, &uid, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	err = ts.store.UpdateAPIKey(key.ID, auth.APIKey{}, false, true)
	require.NoError(t, err)

	updated, err := ts.store.GetAPIKey(key.ID)
	require.NoError(t, err)
	assert.Nil(t, updated.UserID)
}

func TestUpdateAPIKey_ToggleEnabled(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	key, _, err := ts.store.CreateAPIKey("toggle-me", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	err = ts.store.UpdateAPIKey(key.ID, auth.APIKey{Enabled: false}, false, false)
	require.NoError(t, err)

	updated, err := ts.store.GetAPIKey(key.ID)
	require.NoError(t, err)
	assert.False(t, updated.Enabled)
}

func TestUpdateAPIKey_NotFound(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	err := ts.store.UpdateAPIKey("no-such-key", auth.APIKey{Name: "x"}, false, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// ---------------------------------------------------------------------------
// DeleteAPIKey
// ---------------------------------------------------------------------------

func TestDeleteAPIKey_Exists(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	key, _, err := ts.store.CreateAPIKey("delete-me", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	err = ts.store.DeleteAPIKey(key.ID)
	require.NoError(t, err)

	_, err = ts.store.GetAPIKey(key.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestDeleteAPIKey_NotFound(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	err := ts.store.DeleteAPIKey("no-such-id")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// ---------------------------------------------------------------------------
// SetKeyRateLimit
// ---------------------------------------------------------------------------

func TestSetKeyRateLimit_Set(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	key, _, err := ts.store.CreateAPIKey("rate-test", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	err = ts.store.SetKeyRateLimit(key.ID, 50, 30)
	require.NoError(t, err)

	updated, err := ts.store.GetAPIKey(key.ID)
	require.NoError(t, err)
	assert.Equal(t, 50, updated.RateLimitRPM)
}

func TestSetKeyRateLimit_Update(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	key, _, err := ts.store.CreateAPIKey("rate-update", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	err = ts.store.SetKeyRateLimit(key.ID, 10, 0)
	require.NoError(t, err)

	err = ts.store.SetKeyRateLimit(key.ID, 100, 60)
	require.NoError(t, err)

	updated, err := ts.store.GetAPIKey(key.ID)
	require.NoError(t, err)
	assert.Equal(t, 100, updated.RateLimitRPM)
}

func TestSetKeyRateLimit_NotFound(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	err := ts.store.SetKeyRateLimit("no-such-id", 10, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// ---------------------------------------------------------------------------
// RecordKeyUsage / GetKeyUsage
// ---------------------------------------------------------------------------

func TestRecordKeyUsage_InsertAndQuery(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	key, _, err := ts.store.CreateAPIKey("usage-test", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	date := "2026-07-01"
	err = ts.store.RecordKeyUsage(key.ID, date, "gpt-4", "openai", 100, 200, 1, 0.005)
	require.NoError(t, err)

	usage, err := ts.store.GetKeyUsage(key.ID, date, date)
	require.NoError(t, err)
	require.Len(t, usage, 1)
	assert.Equal(t, key.ID, usage[0].KeyID)
	assert.Equal(t, int64(100), usage[0].TokensIn)
	assert.Equal(t, int64(200), usage[0].TokensOut)
	assert.Equal(t, 0.005, usage[0].CostUSD)
	assert.Equal(t, int64(1), usage[0].RequestCount)
	assert.Equal(t, "gpt-4", usage[0].Model)
	assert.Equal(t, "openai", usage[0].Provider)
	assert.Equal(t, "2026-07-01", usage[0].Date.Format("2006-01-02"))
}

func TestRecordKeyUsage_UpsertAccumulates(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	key, _, err := ts.store.CreateAPIKey("usage-upsert", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	date := "2026-07-15"
	err = ts.store.RecordKeyUsage(key.ID, date, "gpt-4", "openai", 100, 200, 1, 0.005)
	require.NoError(t, err)

	// Second record for the same key/date/model/provider — should accumulate.
	err = ts.store.RecordKeyUsage(key.ID, date, "gpt-4", "openai", 50, 100, 2, 0.002)
	require.NoError(t, err)

	usage, err := ts.store.GetKeyUsage(key.ID, date, date)
	require.NoError(t, err)
	require.Len(t, usage, 1)
	assert.Equal(t, int64(150), usage[0].TokensIn)   // 100 + 50
	assert.Equal(t, int64(300), usage[0].TokensOut)  // 200 + 100
	assert.Equal(t, 0.007, usage[0].CostUSD)         // 0.005 + 0.002
	assert.Equal(t, int64(3), usage[0].RequestCount) // 1 + 2
}

func TestGetKeyUsage_DateRange(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	key, _, err := ts.store.CreateAPIKey("usage-range", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	require.NoError(t, ts.store.RecordKeyUsage(key.ID, "2026-07-01", "gpt-4", "openai", 10, 20, 1, 0.001))
	require.NoError(t, ts.store.RecordKeyUsage(key.ID, "2026-07-15", "gpt-4", "openai", 10, 20, 1, 0.001))
	require.NoError(t, ts.store.RecordKeyUsage(key.ID, "2026-08-01", "gpt-4", "openai", 10, 20, 1, 0.001))

	// Only July records
	usage, err := ts.store.GetKeyUsage(key.ID, "2026-07-01", "2026-07-31")
	require.NoError(t, err)
	assert.Len(t, usage, 2)
}

func TestGetKeyUsage_Empty(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	usage, err := ts.store.GetKeyUsage("nonexistent", "2026-01-01", "2026-12-31")
	require.NoError(t, err)
	assert.Len(t, usage, 0)
}

// ---------------------------------------------------------------------------
// GetCurrentMonthUsage
// ---------------------------------------------------------------------------

func TestGetCurrentMonthUsage_WithData(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	key, _, err := ts.store.CreateAPIKey("month-usage", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	// Record usage on various days (within and outside current month).
	now := time.Now().UTC()
	thisMonth := now.Format("2006-01-02")
	lastMonth := now.AddDate(0, -1, 0).Format("2006-01-02")
	nextMonth := now.AddDate(0, 1, 0).Format("2006-01-02")

	require.NoError(t, ts.store.RecordKeyUsage(key.ID, thisMonth, "gpt-4", "openai", 100, 200, 1, 0.005))
	require.NoError(t, ts.store.RecordKeyUsage(key.ID, thisMonth, "claude-3", "anthropic", 50, 100, 1, 0.003))
	require.NoError(t, ts.store.RecordKeyUsage(key.ID, lastMonth, "gpt-4", "openai", 10, 20, 1, 0.001))
	require.NoError(t, ts.store.RecordKeyUsage(key.ID, nextMonth, "gpt-4", "openai", 10, 20, 1, 0.001))

	total, err := ts.store.GetCurrentMonthUsage(key.ID)
	require.NoError(t, err)
	assert.Equal(t, 0.008, total) // 0.005 + 0.003, only this month's records
}

func TestGetCurrentMonthUsage_NoData(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	total, err := ts.store.GetCurrentMonthUsage("nonexistent")
	require.NoError(t, err)
	assert.Equal(t, 0.0, total)
}

// ---------------------------------------------------------------------------
// GetAPIKeySummary
// ---------------------------------------------------------------------------

func TestGetAPIKeySummary_WithData(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	// Create keys and usage.
	k1, _, err := ts.store.CreateAPIKey("summary-1", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)
	k2, _, err := ts.store.CreateAPIKey("summary-2", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)
	k3, _, err := ts.store.CreateAPIKey("summary-3", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	// Disable k2.
	err = ts.store.UpdateAPIKey(k2.ID, auth.APIKey{Enabled: false}, false, false)
	require.NoError(t, err)

	// Add usage for k1 and k3.
	require.NoError(t, ts.store.RecordKeyUsage(k1.ID, "2026-07-01", "gpt-4", "openai", 100, 200, 5, 0.005))
	require.NoError(t, ts.store.RecordKeyUsage(k3.ID, "2026-07-01", "gpt-4", "openai", 50, 100, 2, 0.002))

	summary, err := ts.store.GetAPIKeySummary()
	require.NoError(t, err)
	require.NotNil(t, summary)

	assert.Equal(t, 3, summary.TotalKeys)
	assert.Equal(t, 2, summary.EnabledKeys)
	assert.Equal(t, int64(7), summary.TotalRequests)    // 5 + 2
	assert.Equal(t, 0.007, summary.TotalCostUSD)        // 0.005 + 0.002
	assert.Equal(t, int64(150), summary.TotalTokensIn)  // 100 + 50
	assert.Equal(t, int64(300), summary.TotalTokensOut) // 200 + 100
}

func TestGetAPIKeySummary_Empty(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	summary, err := ts.store.GetAPIKeySummary()
	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.Equal(t, 0, summary.TotalKeys)
	assert.Equal(t, 0, summary.EnabledKeys)
	assert.Equal(t, int64(0), summary.TotalRequests)
	assert.Equal(t, 0.0, summary.TotalCostUSD)
}

// ---------------------------------------------------------------------------
// SetKeyBudget (hand-written, verify it still works with migrated code)
// ---------------------------------------------------------------------------

func TestSetKeyBudget_SetBoth(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	key, _, err := ts.store.CreateAPIKey("budget-test", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	usd := 500.0
	tokens := int64(10000)
	err = ts.store.SetKeyBudget(key.ID, &usd, &tokens)
	require.NoError(t, err)

	updated, err := ts.store.GetAPIKey(key.ID)
	require.NoError(t, err)
	assert.Equal(t, 500.0, updated.MonthlyBudgetUSD)
	assert.Equal(t, int64(10000), updated.MonthlyBudgetTokens)
}

func TestSetKeyBudget_Partial(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	key, _, err := ts.store.CreateAPIKey("partial-budget", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	usd := 250.0
	err = ts.store.SetKeyBudget(key.ID, &usd, nil)
	require.NoError(t, err)

	updated, err := ts.store.GetAPIKey(key.ID)
	require.NoError(t, err)
	assert.Equal(t, 250.0, updated.MonthlyBudgetUSD)
	assert.Equal(t, int64(0), updated.MonthlyBudgetTokens)
}

func TestSetKeyBudget_NotFound(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	usd := 100.0
	err := ts.store.SetKeyBudget("no-such-id", &usd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestCreateAPIKey_EmptyAllowedLists(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	key, _, err := ts.store.CreateAPIKey("empty-lists", nil, nil, 0, 0, 0, 0, []string{}, []string{}, map[string]string{})
	require.NoError(t, err)

	// Should not be nil — empty slices are valid.
	assert.NotNil(t, key.AllowedModels)
	assert.NotNil(t, key.AllowedProviders)
	assert.NotNil(t, key.Tags)
	assert.Len(t, key.AllowedModels, 0)
	assert.Len(t, key.AllowedProviders, 0)
	assert.Len(t, key.Tags, 0)
}

func TestGetAPIKeyByHash_InvalidKey(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	// A key that doesn't exist
	found, err := ts.store.GetAPIKeyByHash("ilter_bogus0000000000000000000000")
	require.Error(t, err)
	require.Nil(t, found)
}

func TestMultipleCreateAPIKey_UniqueNames(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("unique-key-%d", i)
		_, _, err := ts.store.CreateAPIKey(name, nil, nil, float64(i)*10, int64(i)*100, i, int64(i)*1000, nil, nil, nil)
		require.NoError(t, err)
	}

	keys, err := ts.store.ListAPIKeys()
	require.NoError(t, err)
	assert.Len(t, keys, 5)
}
