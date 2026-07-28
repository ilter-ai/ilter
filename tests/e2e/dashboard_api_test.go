package e2e

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

const dashboardBase = "http://localhost:9191"

func dashboardURL(path string) string {
	return dashboardBase + "/api" + path
}

func dashHeaders() map[string]string {
	return map[string]string{"Authorization": "Bearer test"}
}

// requireOK makes a GET request and asserts a 200 response.
func requireOK(t *testing.T, url string) (int, string) {
	t.Helper()
	code, body, _, err := makeRequest("GET", url, dashHeaders(), nil)
	require.NoError(t, err, "GET %s", url)
	require.Equal(t, 200, code, "GET %s: expected 200, got %d: %s", url, code, body)
	return code, body
}

func TestDashboardE2E_Stats(t *testing.T) {
	requireDev(t)
	_, body := requireOK(t, dashboardURL("/stats"))
	// Verify it parses as a JSON object
	var obj map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &obj), "stats response must be a JSON object")
	t.Logf("Stats keys: %d", len(obj))
}

func TestDashboardE2E_Requests(t *testing.T) {
	requireDev(t)
	_, body := requireOK(t, dashboardURL("/requests"))
	var obj map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &obj), "requests response must be a JSON object")
}

func TestDashboardE2E_Costs(t *testing.T) {
	requireDev(t)
	_, body := requireOK(t, dashboardURL("/costs"))
	var obj map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &obj), "costs response must be a JSON object")
	// Expect top-level fields from CostAttributionResponse
	for _, key := range []string{"total_cost", "total_requests", "by_provider", "by_model"} {
		require.Contains(t, obj, key, "costs response missing %q", key)
	}
}

func TestDashboardE2E_Models(t *testing.T) {
	requireDev(t)
	_, body := requireOK(t, dashboardURL("/models"))
	var models []map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &models), "models response must be a JSON array")
	require.NotEmpty(t, models, "models array must not be empty")
	for i, m := range models {
		for _, field := range []string{"name", "provider", "configured", "active"} {
			require.Contains(t, m, field, "models[%d] missing %q", i, field)
		}
	}
	configured := 0
	for _, m := range models {
		if v, ok := m["configured"].(bool); ok && v {
			configured++
		}
	}
	t.Logf("Models: %d total (%d configured)", len(models), configured)
}

func TestDashboardE2E_Providers(t *testing.T) {
	requireDev(t)
	_, body := requireOK(t, dashboardURL("/providers"))
	var providers []map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &providers), "providers response must be a JSON array")
	require.NotEmpty(t, providers, "providers array must not be empty")
	for i, p := range providers {
		for _, field := range []string{"name", "type", "models"} {
			require.Contains(t, p, field, "providers[%d] missing %q", i, field)
		}
	}
	t.Logf("Providers: %d", len(providers))
}

func TestDashboardE2E_PIIEvents(t *testing.T) {
	requireDev(t)
	_, body := requireOK(t, dashboardURL("/pii-events"))
	// May be object or array depending on whether there are events
	var asArr []any
	if err := json.Unmarshal([]byte(body), &asArr); err == nil {
		t.Logf("PII events count: %d", len(asArr))
		return
	}
	var asObj map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &asObj), "pii-events response must be JSON (array or object)")
	t.Logf("PII events object keys: %d", len(asObj))
}

func TestDashboardE2E_PIIStats(t *testing.T) {
	requireDev(t)
	_, body := requireOK(t, dashboardURL("/pii-stats"))
	var obj map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &obj), "pii-stats response must be a JSON object")
	t.Logf("PII stats keys: %d", len(obj))
}

func TestDashboardE2E_CostByProvider(t *testing.T) {
	requireDev(t)
	_, body := requireOK(t, dashboardURL("/costs/by-provider"))
	// Shared handler handleCosts returns CostAttributionResponse
	var obj map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &obj), "costs/by-provider response must be a JSON object")
	require.Contains(t, obj, "by_provider", "costs/by-provider missing by_provider")
}

func TestDashboardE2E_CostByKey(t *testing.T) {
	requireDev(t)
	_, body := requireOK(t, dashboardURL("/costs/by-key"))
	var obj map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &obj), "costs/by-key response must be a JSON object")
	require.Contains(t, obj, "by_key", "costs/by-key missing by_key")
	require.Contains(t, obj, "period", "costs/by-key missing period")
}

func TestDashboardE2E_CostTrend(t *testing.T) {
	requireDev(t)
	_, body := requireOK(t, dashboardURL("/insights/cost-trend?range=7d"))
	var arr []any
	require.NoError(t, json.Unmarshal([]byte(body), &arr), "cost-trend response must be a JSON array")
	t.Logf("Cost trend entries: %d", len(arr))
}

func TestDashboardE2E_TopExpensive(t *testing.T) {
	requireDev(t)
	_, body := requireOK(t, dashboardURL("/insights/top-expensive"))
	var arr []any
	require.NoError(t, json.Unmarshal([]byte(body), &arr), "top-expensive response must be a JSON array")
	t.Logf("Top expensive entries: %d", len(arr))
}

// Example subtables using t.Run for cleaner organization.
//
//	This tests all dashboard endpoints in one function via subtable.
func TestDashboardE2E_AllEndpoints(t *testing.T) {
	requireDev(t)

	tests := []struct {
		name string
		path string
	}{
		{name: "Stats", path: "/stats"},
		{name: "Requests", path: "/requests"},
		{name: "Costs", path: "/costs"},
		{name: "Models", path: "/models"},
		{name: "Providers", path: "/providers"},
		{name: "PIIEvents", path: "/pii-events"},
		{name: "PIIStats", path: "/pii-stats"},
		{name: "CostByProvider", path: "/costs/by-provider"},
		{name: "CostByKey", path: "/costs/by-key"},
		{name: "CostTrend", path: "/insights/cost-trend?range=7d"},
		{name: "TopExpensive", path: "/insights/top-expensive"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := dashboardURL(tt.path)
			code, body, _, err := makeRequest("GET", url, dashHeaders(), nil)
			require.NoError(t, err, "GET %s", tt.path)
			require.Equal(t, 200, code, "GET %s: expected 200, got %d", tt.path, code)
			t.Logf("%s: %d bytes", tt.name, len(body))
		})
	}
}

// Example subtable with finer-grained assertions.
func TestDashboardE2E_EndpointJSONStructure(t *testing.T) {
	requireDev(t)

	// NOTE: models require deeper structure checks; they have their own test above.
	type endpointCheck struct {
		name    string
		path    string
		isArray bool
	}

	checks := []endpointCheck{
		{name: "Stats", path: "/stats", isArray: false},
		{name: "Requests", path: "/requests", isArray: false},
		{name: "Costs", path: "/costs", isArray: false},
		{name: "Models", path: "/models", isArray: true},
		{name: "Providers", path: "/providers", isArray: true},
		{name: "PIIEvents", path: "/pii-events", isArray: false},
		{name: "PIIStats", path: "/pii-stats", isArray: false},
		{name: "CostByProvider", path: "/costs/by-provider", isArray: false},
		{name: "CostByKey", path: "/costs/by-key", isArray: false},
		{name: "CostTrend", path: "/insights/cost-trend?range=7d", isArray: true},
		{name: "TopExpensive", path: "/insights/top-expensive", isArray: true},
	}

	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			code, body, hdrs, err := makeRequest("GET", dashboardURL(c.path), dashHeaders(), nil)
			require.NoError(t, err, "GET %s", c.path)
			require.Equal(t, 200, code, "GET %s: expected 200, got %d", c.path, code)
			require.Equal(t, "application/json", hdrs.Get("Content-Type"), "%s: Content-Type must be application/json", c.path)

			if c.isArray {
				var arr []any
				require.NoError(t, json.Unmarshal([]byte(body), &arr), "%s: expected JSON array", c.path)
			} else {
				var obj map[string]any
				require.NoError(t, json.Unmarshal([]byte(body), &obj), "%s: expected JSON object", c.path)
			}

			t.Logf("%s: %d bytes", c.name, len(body))
		})
	}
}
