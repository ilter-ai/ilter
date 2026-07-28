package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/db/dbtest"
)

// setupFeatureServer creates a Server with a fresh SQLite store and a chi router
// with feature flag routes registered (no auth middleware).
func setupFeatureServer(t *testing.T) (*db.SQLiteStore, *Server, chi.Router) {
	t.Helper()

	store := dbtest.NewFile(t)

	// Bootstrap a config cache with known feature flags.
	boot := config.DefaultBootConfig()
	cache := config.NewConfigCache(&boot)

	cfg := config.DefaultConfig()
	srv := NewServer(&cfg, cache, store, nil, nil)

	// Register only feature flag routes
	r := chi.NewRouter()
	r.Get("/features", srv.featuresHandler.HandleFeatures)
	r.Post("/features/toggle", srv.featuresHandler.HandleToggleFeature)

	return store, srv, r
}

func TestFeatures_ListAll(t *testing.T) {
	_, _, router := setupFeatureServer(t)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/features", nil))
	require.Equal(t, http.StatusOK, rr.Code)

	var items []FeatureItem
	err := json.Unmarshal(rr.Body.Bytes(), &items)
	require.NoError(t, err)
	// Should return at least boot-default feature flags
	assert.NotEmpty(t, items, "should have feature flags from boot config")
}

func TestFeatures_ToggleGlobal(t *testing.T) {
	store, _, router := setupFeatureServer(t)

	// Toggle a known feature flag
	toggleBody := map[string]interface{}{
		"feature_key": "pii",
		"enabled":     false,
	}
	body, _ := json.Marshal(toggleBody)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("POST", "/features/toggle", bytes.NewReader(body)))
	require.Equal(t, http.StatusOK, rr.Code, "toggle should succeed")

	// Verify it persisted in runtime_config
	var value string
	err := store.DB.QueryRow(
		`SELECT value FROM runtime_config WHERE (section = 'feature' OR section = 'feature_flag') AND key = 'pii'`,
	).Scan(&value)
	require.NoError(t, err)
	assert.Equal(t, "false", value)
}
