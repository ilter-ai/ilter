package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/auth"
	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/db/dbtest"

	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

// setupAuthTestStore creates an in-memory SQLiteStore for auth tests.
func setupAuthTestStore(t *testing.T) *db.SQLiteStore {
	t.Helper()
	return dbtest.New(t)
}

// seedAuthAPIKey creates an API key with the given name and returns the key ID and raw token.
func seedAuthAPIKey(t *testing.T, store *db.SQLiteStore, name string) (string, string) {
	t.Helper()
	vk, rawToken, err := store.CreateAPIKey(name, nil, nil, 100.0, 0, 50, 0, nil, nil, nil)
	require.NoError(t, err)
	require.NotEmpty(t, vk.ID)
	return vk.ID, rawToken
}

func TestGetUserID_NotSet(t *testing.T) {
	ctx := context.Background()
	assert.Nil(t, reqmeta.GetUserID(ctx))
}

func TestGetGroupIDs_NotSet(t *testing.T) {
	ctx := context.Background()
	assert.Nil(t, reqmeta.GetGroupIDs(ctx))
}

func TestGetUserID_Set(t *testing.T) {
	ctx := context.WithValue(context.Background(), reqmeta.UserIDContextKey, 42)
	id := reqmeta.GetUserID(ctx)
	require.NotNil(t, id)
	assert.Equal(t, 42, *id)
}

func TestGetGroupIDs_Set(t *testing.T) {
	ctx := context.WithValue(context.Background(), reqmeta.GroupIDsContextKey, []int{10, 20})
	ids := reqmeta.GetGroupIDs(ctx)
	assert.Equal(t, []int{10, 20}, ids)
}

func TestAuthMiddleware_KeyOwnedByUser(t *testing.T) {
	store := setupAuthTestStore(t)

	// Create a user
	user, err := store.CreateUser(auth.CreateUserRequest{
		Name:  "Test User",
		Email: "test@example.com",
	})
	require.NoError(t, err)

	// Create a group
	group, err := store.CreateGroup(auth.CreateGroupRequest{
		Name:        "Test Group",
		Description: "A test group",
	})
	require.NoError(t, err)

	// Add user to group
	err = store.AddUserToGroup(user.ID, group.ID, "member")
	require.NoError(t, err)

	// Create an API key
	keyID, rawToken := seedAuthAPIKey(t, store, "user-owned-key")

	// Set owner on the key
	_, err = store.DB.Exec("UPDATE api_keys SET user_id = ? WHERE id = ?", user.ID, keyID)
	require.NoError(t, err)

	// Build minimal middleware chain
	auth := NewAuthMiddleware(config.AuthConfig{}, store)
	var capturedCtx context.Context

	handler := auth.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("GET", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, capturedCtx)

	// Verify user ID is set
	uid := reqmeta.GetUserID(capturedCtx)
	require.NotNil(t, uid)
	assert.Equal(t, user.ID, *uid)

	// Verify group IDs are set (from user membership)
	gids := reqmeta.GetGroupIDs(capturedCtx)
	require.NotNil(t, gids)
	assert.Equal(t, []int{group.ID}, gids)

	// Verify existing context values still work
	assert.Equal(t, keyID, reqmeta.GetKeyID(capturedCtx))
}

func TestAuthMiddleware_KeyOwnedByGroup(t *testing.T) {
	store := setupAuthTestStore(t)

	// Create a group
	group, err := store.CreateGroup(auth.CreateGroupRequest{
		Name:        "Group-Owned",
		Description: "A group that owns an API key",
	})
	require.NoError(t, err)

	// Create an API key
	keyID, rawToken := seedAuthAPIKey(t, store, "group-owned-key")

	// Set owner on the key
	_, err = store.DB.Exec("UPDATE api_keys SET group_id = ? WHERE id = ?", group.ID, keyID)
	require.NoError(t, err)

	// Build minimal middleware chain
	auth := NewAuthMiddleware(config.AuthConfig{}, store)
	var capturedCtx context.Context

	handler := auth.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("GET", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, capturedCtx)

	// Verify user ID is NOT set
	assert.Nil(t, reqmeta.GetUserID(capturedCtx))

	// Verify group IDs are set (from group ownership)
	gids := reqmeta.GetGroupIDs(capturedCtx)
	require.NotNil(t, gids)
	assert.Equal(t, []int{group.ID}, gids)

	// Verify existing context values still work
	assert.Equal(t, keyID, reqmeta.GetKeyID(capturedCtx))
}

func TestAuthMiddleware_KeyWithoutOwner(t *testing.T) {
	store := setupAuthTestStore(t)

	// Create an API key with no owner
	keyID, rawToken := seedAuthAPIKey(t, store, "unowned-key")

	// Build minimal middleware chain
	auth := NewAuthMiddleware(config.AuthConfig{}, store)
	var capturedCtx context.Context

	handler := auth.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("GET", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, capturedCtx)

	// Verify user ID is not set
	assert.Nil(t, reqmeta.GetUserID(capturedCtx))

	// Verify group IDs are not set
	assert.Nil(t, reqmeta.GetGroupIDs(capturedCtx))

	// Verify existing context values still work
	assert.Equal(t, keyID, reqmeta.GetKeyID(capturedCtx))
}

func TestAuthMiddleware_AdminKeyBillingKeyOverride(t *testing.T) {
	store := setupAuthTestStore(t)
	keyID, _ := seedAuthAPIKey(t, store, "admin-billing-key")

	auth := NewAuthMiddleware(config.AuthConfig{AdminKey: "admin-secret"}, store)
	var capturedCtx context.Context

	handler := auth.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("GET", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer admin-secret")
	req.Header.Set("X-Ilter-Billing-Key-ID", keyID)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, capturedCtx)
	assert.Equal(t, keyID, reqmeta.GetKeyID(capturedCtx), "KeyID should be the billing key, not 'admin'")
}

func TestAuthMiddleware_NormalKeyBillingKeyIgnored(t *testing.T) {
	store := setupAuthTestStore(t)
	attackerKeyID, attackerToken := seedAuthAPIKey(t, store, "attacker")
	victimKeyID, _ := seedAuthAPIKey(t, store, "victim")

	auth := NewAuthMiddleware(config.AuthConfig{}, store)
	var capturedCtx context.Context

	handler := auth.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("GET", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer "+attackerToken)
	req.Header.Set("X-Ilter-Billing-Key-ID", victimKeyID)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, capturedCtx)
	assert.Equal(t, attackerKeyID, reqmeta.GetKeyID(capturedCtx),
		"normal key auth must ignore X-Ilter-Billing-Key-ID header")
}

func TestAuthMiddleware_AdminKeyBypass(t *testing.T) {
	store := setupAuthTestStore(t)

	// Admin key should skip owner resolution and not set user/group context
	auth := NewAuthMiddleware(config.AuthConfig{AdminKey: "admin-secret"}, store)
	var capturedCtx context.Context

	handler := auth.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("GET", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer admin-secret")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, capturedCtx)

	// Admin key: key_id = "admin", no user/group context
	assert.Equal(t, "admin", reqmeta.GetKeyID(capturedCtx))
	assert.Nil(t, reqmeta.GetUserID(capturedCtx))
	assert.Nil(t, reqmeta.GetGroupIDs(capturedCtx))
}

func TestAuthMiddleware_XAPIKeyHeader(t *testing.T) {
	store := setupAuthTestStore(t)
	keyID, rawToken := seedAuthAPIKey(t, store, "xapikey-key")

	auth := NewAuthMiddleware(config.AuthConfig{AdminKey: "admin-secret"}, store)

	tests := []struct {
		name      string
		setHeader func(*http.Request)
		wantCode  int
		wantKeyID string
	}{
		{
			name: "Auth via x-api-key header",
			setHeader: func(req *http.Request) {
				req.Header.Set("x-api-key", rawToken)
			},
			wantCode:  http.StatusOK,
			wantKeyID: keyID,
		},
		{
			name: "Auth via X-API-Key header (case insensitive)",
			setHeader: func(req *http.Request) {
				req.Header.Set("X-API-Key", rawToken)
			},
			wantCode:  http.StatusOK,
			wantKeyID: keyID,
		},
		{
			name: "Auth via x-api-key header with admin key",
			setHeader: func(req *http.Request) {
				req.Header.Set("x-api-key", "admin-secret")
			},
			wantCode:  http.StatusOK,
			wantKeyID: "admin",
		},
		{
			name: "Invalid token via x-api-key header",
			setHeader: func(req *http.Request) {
				req.Header.Set("x-api-key", "invalid-token")
			},
			wantCode:  http.StatusUnauthorized,
			wantKeyID: "",
		},
		{
			name:      "Missing Authorization header",
			setHeader: func(_ *http.Request) {},
			wantCode:  http.StatusUnauthorized,
			wantKeyID: "",
		},
		{
			name: "Empty Bearer token",
			setHeader: func(req *http.Request) {
				req.Header.Set("Authorization", "Bearer ")
			},
			wantCode:  http.StatusUnauthorized,
			wantKeyID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedCtx context.Context
			handler := auth.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedCtx = r.Context()
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest("GET", "/v1/chat/completions", nil)
			tt.setHeader(req)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			assert.Equal(t, tt.wantCode, rr.Code)
			if tt.wantCode == http.StatusOK {
				require.NotNil(t, capturedCtx)
				assert.Equal(t, tt.wantKeyID, reqmeta.GetKeyID(capturedCtx))
			}
		})
	}
}
