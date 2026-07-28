// allow: SIZE_OK — 5 independent e2e test functions for MCP Gateway Dashboard
// API. Each test has its own requireDev + makeRequest call; the test URL and
// header setup cannot be deduplicated without obscuring per-endpoint context.
package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	mcpDashBase = "http://localhost:9191/api"
)

// dashAuth returns the standard Bearer auth header for dashboard requests.
func dashAuth() map[string]string {
	return map[string]string{"Authorization": "Bearer test"}
}

func TestMCPE2E_ListServers(t *testing.T) {
	requireDev(t)

	code, body, _, err := makeRequest("GET", mcpDashBase+"/mcp-servers", dashAuth(), nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, code)

	var resp struct {
		Servers []map[string]any `json:"servers"`
	}
	err = json.Unmarshal([]byte(body), &resp)
	require.NoError(t, err, "response should be valid JSON")

	assert.GreaterOrEqual(t, len(resp.Servers), 2, "expected at least 2 seeded MCP servers")
	if len(resp.Servers) > 0 {
		s := resp.Servers[0]
		assert.Contains(t, s, "id")
		assert.Contains(t, s, "name")
		assert.Contains(t, s, "transport")
		assert.Contains(t, s, "enabled")
		assert.Contains(t, s, "tools_count")
	}
}

func TestMCPE2E_ServerDetail(t *testing.T) {
	requireDev(t)

	code, body, _, err := makeRequest("GET", mcpDashBase+"/mcp-servers/sqlite", dashAuth(), nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, code)

	var s map[string]any
	err = json.Unmarshal([]byte(body), &s)
	require.NoError(t, err, "response should be valid JSON")

	assert.Equal(t, "sqlite", s["id"])
	assert.Equal(t, "SQLite", s["name"])
	assert.Equal(t, "stdio", s["transport"])
	assert.NotEmpty(t, s["description"])
	assert.Equal(t, true, s["enabled"])
	assert.Contains(t, s, "timeout_ms")
	assert.Contains(t, s, "max_retries")
}

func TestMCPE2E_ListServerTools(t *testing.T) {
	requireDev(t)

	code, body, _, err := makeRequest("GET", mcpDashBase+"/mcp-servers/sqlite/tools", dashAuth(), nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, code)

	var resp struct {
		ServerID string           `json:"server_id"`
		Tools    []map[string]any `json:"tools"`
	}
	err = json.Unmarshal([]byte(body), &resp)
	require.NoError(t, err, "response should be valid JSON")

	assert.Equal(t, "sqlite", resp.ServerID)
	assert.GreaterOrEqual(t, len(resp.Tools), 1, "expected at least 1 tool for sqlite server")
	if len(resp.Tools) > 0 {
		tool := resp.Tools[0]
		assert.Contains(t, tool, "id")
		assert.Contains(t, tool, "name")
		assert.Contains(t, tool, "description")
	}
}

func TestMCPE2E_MCPStats(t *testing.T) {
	requireDev(t)

	code, body, _, err := makeRequest("GET", mcpDashBase+"/mcp/stats", dashAuth(), nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, code)

	var stats map[string]any
	err = json.Unmarshal([]byte(body), &stats)
	require.NoError(t, err, "response should be valid JSON")

	assert.Contains(t, stats, "servers")
	assert.Contains(t, stats, "tools")
	assert.Contains(t, stats, "usage")

	servers, ok := stats["servers"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, servers, "total")
	assert.Contains(t, servers, "enabled")
}

func TestMCPE2E_AuditLog(t *testing.T) {
	requireDev(t)

	code, body, _, err := makeRequest("GET", mcpDashBase+"/mcp/audit", dashAuth(), nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, code)

	var logResp struct {
		Items  []map[string]any `json:"items"`
		Total  int              `json:"total"`
		Limit  int              `json:"limit"`
		Offset int              `json:"offset"`
	}
	err = json.Unmarshal([]byte(body), &logResp)
	require.NoError(t, err, "response should be valid JSON")

	assert.GreaterOrEqual(t, logResp.Total, 1, "expected at least 1 audit log entry from seed data")
	assert.GreaterOrEqual(t, len(logResp.Items), 1, "expected at least 1 item in response")
	assert.Equal(t, 50, logResp.Limit)
	assert.Equal(t, 0, logResp.Offset)
	if len(logResp.Items) > 0 {
		entry := logResp.Items[0]
		assert.Contains(t, entry, "tool")
		assert.Contains(t, entry, "server_id")
		assert.Contains(t, entry, "method")
		assert.Contains(t, entry, "success")
	}
}
