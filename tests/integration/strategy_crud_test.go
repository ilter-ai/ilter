// allow: SIZE_OK — single-concept CRUD lifecycle test with 17 subtests;
// splitting across files would harm coherence.

package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/dashboard"
	"github.com/ilter-ai/ilter/internal/db/dbtest"
	"github.com/ilter-ai/ilter/internal/model/catalog"
	testutil "github.com/ilter-ai/ilter/tests/utils"
)

// TestStrategyCRUD exercises every strategy management endpoint via the
// fully-built dashboard router (BuildServer — no listener started).
func TestStrategyCRUD(t *testing.T) {
	store := dbtest.New(t)

	cfg := &config.Config{
		Auth:      config.AuthConfig{AdminKey: testutil.AdminKey},
		Dashboard: config.DashboardConfig{Enabled: true, Port: 9099},
	}

	server := dashboard.NewServer(cfg, nil, store, nil, nil)
	srv, err := server.BuildServer()
	if err != nil {
		t.Fatalf("failed to build server: %v", err)
	}

	handler := srv.Handler

	makeRequest := func(method, path string, body []byte, auth bool) *httptest.ResponseRecorder {
		req, _ := http.NewRequest(method, path, bytes.NewReader(body))
		if auth {
			req.Header.Set("Authorization", "Bearer "+testutil.AdminKey)
		}
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		return rr
	}

	// -----------------------------------------------------------------------
	// Auth enforcement
	// -----------------------------------------------------------------------
	t.Run("AuthEnforcement", func(t *testing.T) {
		rr := makeRequest("GET", "/api/smart-router/strategies", nil, false)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rr.Code)
		}
	})

	t.Run("AuthEnforcement_InvalidToken", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/smart-router/strategies", nil)
		req.Header.Set("Authorization", "Bearer wrong-token")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rr.Code)
		}
	})

	// -----------------------------------------------------------------------
	// a. GET strategies — empty list initially
	// -----------------------------------------------------------------------
	t.Run("ListStrategies_Empty", func(t *testing.T) {
		rr := makeRequest("GET", "/api/smart-router/strategies", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp struct {
			Data   []any  `json:"data"`
			Active string `json:"active"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if len(resp.Data) != 0 {
			t.Errorf("expected empty data, got %d items", len(resp.Data))
		}
		if resp.Active != "" {
			t.Errorf("expected empty active, got %q", resp.Active)
		}
	})

	// -----------------------------------------------------------------------
	// b. PUT strategy — create
	// -----------------------------------------------------------------------
	t.Run("CreateStrategy", func(t *testing.T) {
		body := `{
			"description": "Test strategy for CRUD",
			"enabled": true,
			"provider_preference": "cheapest",
			"scorer": {"type": "heuristic"},
			"complexity_thresholds": {"economy": 15, "standard": 50},
			"rules": []
		}`
		rr := makeRequest("PUT", "/api/smart-router/strategies/test-strategy", []byte(body), true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp struct {
			Status string `json:"status"`
			Name   string `json:"name"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if resp.Status != "ok" {
			t.Errorf("expected status ok, got %q", resp.Status)
		}
		if resp.Name != "test-strategy" {
			t.Errorf("expected name test-strategy, got %q", resp.Name)
		}
	})

	// -----------------------------------------------------------------------
	// c. GET strategy — found
	// -----------------------------------------------------------------------
	t.Run("GetStrategy_Found", func(t *testing.T) {
		rr := makeRequest("GET", "/api/smart-router/strategies/test-strategy", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var strategy struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Enabled     bool   `json:"enabled"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &strategy); err != nil {
			t.Fatalf("failed to decode strategy: %v", err)
		}
		if strategy.Name != "test-strategy" {
			t.Errorf("expected name test-strategy, got %q", strategy.Name)
		}
		if strategy.Description != "Test strategy for CRUD" {
			t.Errorf("expected description %q, got %q", "Test strategy for CRUD", strategy.Description)
		}
		if !strategy.Enabled {
			t.Error("expected strategy to be enabled")
		}
	})

	// -----------------------------------------------------------------------
	// d. GET strategy — not found
	// -----------------------------------------------------------------------
	t.Run("GetStrategy_NotFound", func(t *testing.T) {
		rr := makeRequest("GET", "/api/smart-router/strategies/nonexistent", nil, true)
		if rr.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	// -----------------------------------------------------------------------
	// e. GET strategies — now contains data
	// -----------------------------------------------------------------------
	t.Run("ListStrategies_WithData", func(t *testing.T) {
		rr := makeRequest("GET", "/api/smart-router/strategies", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp struct {
			Data   []map[string]any `json:"data"`
			Active string           `json:"active"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if len(resp.Data) != 1 {
			t.Fatalf("expected 1 strategy, got %d", len(resp.Data))
		}
		name, ok := resp.Data[0]["name"].(string)
		if !ok || name != "test-strategy" {
			t.Errorf("expected name test-strategy, got %v", resp.Data[0]["name"])
		}
	})

	// -----------------------------------------------------------------------
	// f. PUT strategy — update
	// -----------------------------------------------------------------------
	t.Run("UpdateStrategy", func(t *testing.T) {
		body := `{
			"description": "Updated description",
			"enabled": false,
			"provider_preference": "round-robin",
			"scorer": {"type": "heuristic"},
			"complexity_thresholds": {"economy": 20, "standard": 60},
			"rules": []
		}`
		rr := makeRequest("PUT", "/api/smart-router/strategies/test-strategy", []byte(body), true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		// Verify the update was persisted
		rr2 := makeRequest("GET", "/api/smart-router/strategies/test-strategy", nil, true)
		if rr2.Code != http.StatusOK {
			t.Fatalf("expected 200 on GET after update, got %d: %s", rr2.Code, rr2.Body.String())
		}

		var strategy struct {
			Description        string `json:"description"`
			Enabled            bool   `json:"enabled"`
			ProviderPreference string `json:"provider_preference"`
		}
		if err := json.Unmarshal(rr2.Body.Bytes(), &strategy); err != nil {
			t.Fatalf("failed to decode updated strategy: %v", err)
		}
		if strategy.Description != "Updated description" {
			t.Errorf("expected updated description %q, got %q", "Updated description", strategy.Description)
		}
		if strategy.Enabled {
			t.Error("expected strategy to be disabled after update")
		}
		if strategy.ProviderPreference != "round-robin" {
			t.Errorf("expected provider_preference round-robin, got %q", strategy.ProviderPreference)
		}
	})

	// -----------------------------------------------------------------------
	// g. DELETE strategy
	// -----------------------------------------------------------------------
	t.Run("DeleteStrategy", func(t *testing.T) {
		rr := makeRequest("DELETE", "/api/smart-router/strategies/test-strategy", nil, true)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d: %s", rr.Code, rr.Body.String())
		}

		// Verify it is gone
		rr2 := makeRequest("GET", "/api/smart-router/strategies/test-strategy", nil, true)
		if rr2.Code != http.StatusNotFound {
			t.Errorf("expected 404 after delete, got %d: %s", rr2.Code, rr2.Body.String())
		}
	})

	// -----------------------------------------------------------------------
	// h. DELETE strategy — non-existent (code returns 204, SQL DELETE is no-op)
	// -----------------------------------------------------------------------
	t.Run("DeleteStrategy_NotFound", func(t *testing.T) {
		// rcStore.Delete does not error on missing keys — it returns 204.
		rr := makeRequest("DELETE", "/api/smart-router/strategies/never-existed", nil, true)
		if rr.Code != http.StatusNoContent {
			t.Errorf("expected 204 for non-existent key (DELETE is idempotent), got %d: %s", rr.Code, rr.Body.String())
		}
	})

	// -----------------------------------------------------------------------
	// i. PUT strategy — invalid JSON
	// -----------------------------------------------------------------------
	t.Run("CreateStrategy_InvalidJSON", func(t *testing.T) {
		rr := makeRequest("PUT", "/api/smart-router/strategies/bad-strategy", []byte(`{bad json}`), true)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for invalid JSON, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	// -----------------------------------------------------------------------
	// j. PUT strategy — with a rule targeting an unregistered model → 400
	// -----------------------------------------------------------------------
	t.Run("CreateStrategy_InvalidTargetModel", func(t *testing.T) {
		// Register a valid model so the test is realistic — the rule targets
		// a different, unregistered model.
		catalog.ModelsMu.Lock()
		catalog.Models["gpt-4o-mini"] = []catalog.ModelInfo{
			{ID: "gpt-4o-mini", Provider: "openai", Tier: "economy"},
		}
		catalog.ModelsMu.Unlock()
		t.Cleanup(func() {
			catalog.ModelsMu.Lock()
			delete(catalog.Models, "gpt-4o-mini")
			catalog.ModelsMu.Unlock()
		})

		// "nonexistent-model" is deliberately absent from the registry.
		body := `{
			"description": "Strategy with bad rule",
			"rules": [
				{"name": "bad-rule", "condition": "complexity > 50", "target_model": "nonexistent-model"}
			]
		}`
		rr := makeRequest("PUT", "/api/smart-router/strategies/bad-strategy", []byte(body), true)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for invalid target model, got %d: %s", rr.Code, rr.Body.String())
		}

		// Verify the error body mentions the offending rule/model
		var errResp struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err == nil {
			if !strings.Contains(errResp.Message, "nonexistent-model") {
				t.Errorf("expected error message to mention nonexistent-model, got %q", errResp.Message)
			}
		}
	})

	// -----------------------------------------------------------------------
	// k. GET active strategy — empty initially
	// -----------------------------------------------------------------------
	t.Run("GetActiveStrategy_Empty", func(t *testing.T) {
		rr := makeRequest("GET", "/api/smart-router/active", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp struct {
			Active string `json:"active"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode: %v", err)
		}
		if resp.Active != "" {
			t.Errorf("expected empty active, got %q", resp.Active)
		}
	})

	// -----------------------------------------------------------------------
	// l. PUT active strategy — set
	// -----------------------------------------------------------------------
	t.Run("SetActiveStrategy", func(t *testing.T) {
		// Create a strategy first so the name is valid (the endpoint stores
		// the name, it does not validate it against existing strategies).
		createBody := `{
			"description": "Active strategy candidate",
			"scorer": {"type": "heuristic"},
			"complexity_thresholds": {"economy": 15, "standard": 50},
			"rules": []
		}`
		cr := makeRequest("PUT", "/api/smart-router/strategies/test-strategy-2", []byte(createBody), true)
		if cr.Code != http.StatusOK {
			t.Fatalf("failed to create strategy: %d: %s", cr.Code, cr.Body.String())
		}

		body := `{"name": "test-strategy-2"}`
		rr := makeRequest("PUT", "/api/smart-router/active", []byte(body), true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp struct {
			Status string `json:"status"`
			Active string `json:"active"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode: %v", err)
		}
		if resp.Status != "ok" {
			t.Errorf("expected status ok, got %q", resp.Status)
		}
		if resp.Active != "test-strategy-2" {
			t.Errorf("expected active test-strategy-2, got %q", resp.Active)
		}
	})

	// -----------------------------------------------------------------------
	// m. GET active strategy — now returns the set value
	// -----------------------------------------------------------------------
	t.Run("GetActiveStrategy_Set", func(t *testing.T) {
		rr := makeRequest("GET", "/api/smart-router/active", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp struct {
			Active string `json:"active"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to decode: %v", err)
		}
		if resp.Active != "test-strategy-2" {
			t.Errorf("expected active test-strategy-2, got %q", resp.Active)
		}
	})

	// -----------------------------------------------------------------------
	// n. PUT active with empty name → 400
	// -----------------------------------------------------------------------
	t.Run("SetActiveStrategy_EmptyName", func(t *testing.T) {
		body := `{"name": ""}`
		rr := makeRequest("PUT", "/api/smart-router/active", []byte(body), true)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for empty name, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	// -----------------------------------------------------------------------
	// o. PUT active with missing/invalid body → 400
	// -----------------------------------------------------------------------
	t.Run("SetActiveStrategy_InvalidBody", func(t *testing.T) {
		rr := makeRequest("PUT", "/api/smart-router/active", []byte(`not json`), true)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for invalid body, got %d: %s", rr.Code, rr.Body.String())
		}
	})
}
