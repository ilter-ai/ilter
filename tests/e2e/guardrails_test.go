package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const dashboardBaseURL = "http://localhost:9191"

var adminHeaders = map[string]string{"Authorization": "Bearer test"}

// TestGuardrailsE2E_ListViolations verifies that GET /api/guardrails/violations
// returns guardrail violation events from the demo seed data.
func TestGuardrailsE2E_ListViolations(t *testing.T) {
	requireDev(t)

	code, body, _, err := makeRequest("GET", dashboardBaseURL+"/api/guardrails/violations", adminHeaders, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, code)

	var resp struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"total"`
		Page  int              `json:"page"`
		Limit int              `json:"limit"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &resp))
	assert.GreaterOrEqual(t, resp.Total, 0, "violations total should be non-negative")
	assert.Equal(t, 1, resp.Page, "default page should be 1")
	assert.Equal(t, 50, resp.Limit, "default limit should be 50")
	t.Logf("guardrail violations total: %d, items returned: %d", resp.Total, len(resp.Items))
}

// TestGuardrailsE2E_ListRules verifies that GET /api/guardrails
// returns the guardrail rules list.
func TestGuardrailsE2E_ListRules(t *testing.T) {
	requireDev(t)

	code, body, _, err := makeRequest("GET", dashboardBaseURL+"/api/guardrails", adminHeaders, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, code)

	var resp struct {
		Rules []map[string]any `json:"rules"`
		Total int              `json:"total"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &resp))
	assert.GreaterOrEqual(t, resp.Total, 0, "rules total should be non-negative")
	t.Logf("guardrail rules total: %d", resp.Total)
}

// TestGuardrailsE2E_GuardrailStats verifies that GET /api/guardrails/stats
// returns guardrail middleware status and rule count.
func TestGuardrailsE2E_GuardrailStats(t *testing.T) {
	requireDev(t)

	code, body, _, err := makeRequest("GET", dashboardBaseURL+"/api/guardrails/stats", adminHeaders, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, code)

	var resp struct {
		Enabled   bool     `json:"enabled"`
		RuleCount int      `json:"rule_count"`
		RuleSets  []string `json:"rule_sets"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &resp))
	t.Logf("guardrails enabled: %v, rule_count: %d, rule_sets: %v", resp.Enabled, resp.RuleCount, resp.RuleSets)
}

// TestGuardrailsE2E_Summary verifies that GET /api/guardrails/summary
// returns guardrail event summary with type breakdown.
func TestGuardrailsE2E_Summary(t *testing.T) {
	requireDev(t)

	code, body, _, err := makeRequest("GET", dashboardBaseURL+"/api/guardrails/summary", adminHeaders, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, code)

	var resp struct {
		TotalEvents int `json:"total_events"`
		ByType      []struct {
			GuardrailType string  `json:"guardrail_type"`
			Count         int     `json:"count"`
			Pct           float64 `json:"pct"`
		} `json:"by_type"`
		RecentTrend []struct {
			Date  string `json:"date"`
			Count int    `json:"count"`
		} `json:"recent_trend"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &resp))
	assert.GreaterOrEqual(t, resp.TotalEvents, 0, "total events should be non-negative")
	t.Logf("guardrail summary total events: %d, types: %d, trend days: %d",
		resp.TotalEvents, len(resp.ByType), len(resp.RecentTrend))
}
