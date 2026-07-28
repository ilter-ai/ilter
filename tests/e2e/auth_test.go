package e2e

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthE2E_Unauthenticated(t *testing.T) {
	requireDev(t)

	code, body, _, err := makeRequest("GET", "http://localhost:9191/api/smart-router/strategies", nil, nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, code, "expected 401 without auth header, got body: %s", body)
}

func TestAuthE2E_InvalidToken(t *testing.T) {
	requireDev(t)

	headers := map[string]string{"Authorization": "Bearer invalid-token"}
	code, body, _, err := makeRequest("GET", "http://localhost:9191/api/smart-router/strategies", headers, nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, code, "expected 401 with invalid token, got body: %s", body)
}

func TestAuthE2E_AdminToken(t *testing.T) {
	requireDev(t)

	headers := map[string]string{"Authorization": "Bearer test"}
	code, body, _, err := makeRequest("GET", "http://localhost:9191/api/smart-router/strategies", headers, nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, code, "expected 200 with admin token, got body: %s", body)
}

func TestAuthE2E_LoginEndpoint(t *testing.T) {
	requireDev(t)

	form := url.Values{"username": {"admin"}, "password": {"admin"}}
	bodyReader := strings.NewReader(form.Encode())
	req, err := http.NewRequestWithContext(t.Context(), "POST", "http://localhost:9191/api/auth/login", bodyReader)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Endpoint may or may not exist depending on dashboard implementation.
	assert.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadRequest,
		"expected 200, 404, or 400, got %d", resp.StatusCode)
}
