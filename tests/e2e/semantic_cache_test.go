package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type semanticCacheSummaryResponse struct {
	CacheHits24h      int              `json:"cache_hits_24h"`
	CacheMisses24h    int              `json:"cache_misses_24h"`
	HitRatePct        float64          `json:"hit_rate_pct"`
	CacheSizeEntries  int              `json:"cache_size_entries"`
	CacheSizeMB       float64          `json:"cache_size_mb"`
	AvgLatencySavedMs float64          `json:"avg_latency_saved_ms"`
	RedisConnected    bool             `json:"redis_connected"`
	Mode              string           `json:"mode"`
	TopQueries        []map[string]any `json:"top_queries"`
	HourlyData        []map[string]any `json:"hourly_data"`
}

// TestSemanticCacheE2E_Summary verifies the GET /api/semantic-cache/summary
// endpoint on the Dashboard API returns 200 and all expected fields.
func TestSemanticCacheE2E_Summary(t *testing.T) {
	requireDev(t)
	if testing.Short() {
		t.Skip("skipping e2e semantic cache test in short mode")
	}

	dashHeaders := map[string]string{
		"Authorization": "Bearer test",
	}
	code, body, _, err := makeRequest("GET", "http://localhost:9191/api/semantic-cache/summary", dashHeaders, nil)
	require.NoError(t, err, "request must not error")
	require.Equal(t, http.StatusOK, code, "expected 200 OK, got %d", code)

	var resp semanticCacheSummaryResponse
	err = json.Unmarshal([]byte(body), &resp)
	require.NoError(t, err, "response must be valid JSON")

	require.IsType(t, int(0), resp.CacheHits24h, "cache_hits_24h must be int")
	require.IsType(t, int(0), resp.CacheMisses24h, "cache_misses_24h must be int")
	require.IsType(t, float64(0), resp.HitRatePct, "hit_rate_pct must be float64")
	require.IsType(t, int(0), resp.CacheSizeEntries, "cache_size_entries must be int")
	require.IsType(t, float64(0), resp.CacheSizeMB, "cache_size_mb must be float64")
	require.IsType(t, float64(0), resp.AvgLatencySavedMs, "avg_latency_saved_ms must be float64")
	require.IsType(t, true, resp.RedisConnected, "redis_connected must be bool")
	require.IsType(t, "", resp.Mode, "mode must be string")

	require.NotNil(t, resp.TopQueries, "top_queries must be present")
	require.NotNil(t, resp.HourlyData, "hourly_data must be present")

	// Mode must be a known value
	require.Contains(t, []string{"enabled", "disabled", "exact", "semantic"}, resp.Mode, "mode must be enabled, disabled, exact, or semantic")
}
