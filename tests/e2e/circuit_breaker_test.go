// Circuit Breaker E2E tests — hit real dashboard API at :9191.
// makeRequest is shared from verification_test.go (same package).
package e2e

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCircuitBreakerE2E(t *testing.T) {
	requireDev(t)

	dashURL := "http://localhost:9191"
	headers := map[string]string{
		"Authorization": "Bearer test",
	}

	tests := []struct {
		name   string
		method string
		path   string
		body   []byte
	}{
		{
			name:   "Summary",
			method: "GET",
			path:   "/api/circuit-breaker/summary",
			body:   nil,
		},
		{
			name:   "Toggle",
			method: "POST",
			path:   "/api/circuit-breaker/toggle",
			body:   []byte(`{"provider":"openai"}`),
		},
		{
			name:   "ForceOpen",
			method: "POST",
			path:   "/api/circuit-breaker/force-open",
			body:   []byte(`{"provider":"openai"}`),
		},
		{
			name:   "Reset",
			method: "POST",
			path:   "/api/circuit-breaker/reset",
			body:   []byte(`{"provider":"openai"}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, bodyStr, _, err := makeRequest(tt.method, dashURL+tt.path, headers, tt.body)
			require.NoError(t, err, "%s %s request failed", tt.method, tt.path)
			require.Equal(t, 200, code, "%s %s expected 200, got %d", tt.method, tt.path, code)

			// Verify response is valid JSON
			var resp map[string]any
			require.NoError(t, json.Unmarshal([]byte(bodyStr), &resp),
				"%s %s response must be valid JSON", tt.method, tt.path)

			switch tt.name {
			case "Summary":
				// Summary returns nested summary + circuits objects
				assert.Contains(t, resp, "summary", "summary response must contain 'summary' field")
				assert.Contains(t, resp, "circuits", "summary response must contain 'circuits' field")
			default:
				// Toggle, ForceOpen, Reset all return {"status":"ok"}
				assert.Equal(t, "ok", resp["status"],
					"%s %s expected status 'ok'", tt.method, tt.path)
			}
		})
	}
}
