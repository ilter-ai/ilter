package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/auth"
	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/db/dbtest"

	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

// setupTargetingTestStore creates temp store for targeting tests.
func setupTargetingTestStore(t *testing.T) *db.SQLiteStore {
	t.Helper()
	return dbtest.NewFile(t)
}

// seedUserAPIKey creates a virtual API key owned by a user.
func seedUserAPIKey(t *testing.T, store *db.SQLiteStore, name string, userID int) string {
	t.Helper()
	vk, rawToken, err := store.CreateAPIKey(name, nil, &userID, 100.0, 0, 50, 0, nil, nil, nil)
	require.NoError(t, err)
	require.NotEmpty(t, vk.ID)
	return rawToken
}

func TestTargeting_AuthSetsUserContext(t *testing.T) {
	store := setupTargetingTestStore(t)

	// Create user first
	user, err := store.CreateUser(auth.CreateUserRequest{Name: "TestUser", Email: "test@test.com"})
	require.NoError(t, err)

	// Create API key owned by user
	rawToken := seedUserAPIKey(t, store, "user-ctx", user.ID)

	authCfg := config.AuthConfig{AdminKey: "admin-token"}
	auth := NewAuthMiddleware(authCfg, store)

	r := chi.NewRouter()
	r.Use(auth.Handler)

	r.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		userID := reqmeta.GetUserID(r.Context())
		groupIDs := reqmeta.GetGroupIDs(r.Context())
		_ = json.NewEncoder(w).Encode(map[string]any{
			"user_id":   userID,
			"group_ids": groupIDs,
		})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]any
	err = json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)

	// user with no group memberships should have user_id but empty/nil group_ids
	uid := resp["user_id"]
	require.NotNil(t, uid)
	assert.Equal(t, float64(user.ID), uid)

	// group_ids should be nil (no groups) or empty slice
	gids := resp["group_ids"]
	assert.Nil(t, gids, "expected nil group_ids for user with no group memberships")
}

func TestTargeting_LegacyKeyNoOwner(t *testing.T) {
	store := setupTargetingTestStore(t)

	// Create key WITHOUT owner (legacy pattern)
	var err error
	var rawToken string
	_, rawToken, err = store.CreateAPIKey("legacy-key", nil, nil, 100.0, 0, 50, 0, nil, nil, nil)
	require.NoError(t, err)

	authCfg := config.AuthConfig{AdminKey: "admin-token"}
	auth := NewAuthMiddleware(authCfg, store)

	r := chi.NewRouter()
	r.Use(auth.Handler)
	r.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		userID := reqmeta.GetUserID(r.Context())
		_ = json.NewEncoder(w).Encode(map[string]any{
			"user_id": userID,
		})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]any
	err = json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Nil(t, resp["user_id"])
}

func TestTargeting_AdminKeyNoUserContext(t *testing.T) {
	store := setupTargetingTestStore(t)

	adminToken := "admin-key-for-test"
	authCfg := config.AuthConfig{AdminKey: adminToken}
	auth := NewAuthMiddleware(authCfg, store)

	r := chi.NewRouter()
	r.Use(auth.Handler)
	r.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		userID := reqmeta.GetUserID(r.Context())
		_ = json.NewEncoder(w).Encode(map[string]any{
			"user_id": userID,
		})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var resp map[string]any
	err := json.Unmarshal(rr.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Nil(t, resp["user_id"])
}
