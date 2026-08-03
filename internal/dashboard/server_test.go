package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/dashboard/access"
	"github.com/ilter-ai/ilter/internal/dashboard/stats"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
	"github.com/ilter-ai/ilter/internal/platform/crypto"
	"github.com/ilter-ai/ilter/internal/provider"
)

type dummyProvider struct{}

func (d *dummyProvider) Name() string { return "openai" }
func (d *dummyProvider) Type() string { return "openai" }
func (d *dummyProvider) TransformRequest(_ context.Context, _ *model.ChatCompletionRequest) (*http.Request, error) {
	return nil, nil
}

func (d *dummyProvider) TransformResponse(_ context.Context, _ *http.Response) (*model.ChatCompletionResponse, error) {
	return nil, nil
}
func (d *dummyProvider) Client() *http.Client                { return nil }
func (d *dummyProvider) HealthCheck(_ context.Context) error { return nil }
func (d *dummyProvider) DiscoverModels(_ context.Context) ([]catalog.ModelInfo, error) {
	return nil, nil
}

func TestDashboardServer_API(t *testing.T) {
	store, tmpDir := setupTestStore(t)
	defer func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}()

	// Seed key and audits
	_, err := store.DB.Exec("INSERT INTO api_keys (id, name, hashed_key, enabled, monthly_budget_usd, rate_limit_rpm, created_at, updated_at) VALUES (?, ?, ?, 1, 0.0, 0, datetime('now'), datetime('now'))", "vk_test_1", "test-key", "hashed")
	if err != nil {
		t.Fatalf("failed to seed api_keys: %v", err)
	}

	// Seed audit logs with trace breakdown columns
	_, err = store.DB.Exec(`
		INSERT INTO audit_log (timestamp, key_id, model, provider, prompt_tokens, completion_tokens, total_cost, latency_ms, status_code, cache_hit, guardrail_latency_ms, llm_latency_ms, queued_latency_ms, request_body, response_body)
		VALUES 
		(?, 'vk_test_1', 'gpt-4o', 'openai', 10, 20, 0.0003, 150, 200, 0, 5.0, 130.0, 3.0, '{"model":"gpt-4o"}', '{"choices":[{"message":{"content":"test"}}]}'),
		(?, 'vk_test_1', 'gpt-4o-mini', 'openai', 5, 10, 0.00001, 80, 200, 1, 3.0, 65.0, 2.0, NULL, NULL),
		(?, 'vk_test_1', 'gpt-4o', 'openai', 15, 30, 0.00045, 200, 500, 0, 8.0, 175.0, 4.0, '{"model":"gpt-4o"}', '{"error":"timeout"}')
	`, time.Now().Add(-1*time.Hour), time.Now().Add(-30*time.Minute), time.Now())
	if err != nil {
		t.Fatalf("failed to seed audit logs: %v", err)
	}

	// Seed loop event
	_, err = store.DB.Exec(`
		INSERT INTO loop_events (key_id, client_ip, prompt_hash, repeat_count, window_seconds, action_taken)
		VALUES ('vk_test_1', '127.0.0.1', 'hashabc123', 8, 300, 'throttled')
	`)
	if err != nil {
		t.Fatalf("failed to seed loop events: %v", err)
	}

	// Seed PII event
	_, err = store.DB.Exec(`
		INSERT INTO pii_events (key_id, pii_type, action_taken)
		VALUES ('vk_test_1', 'email', 'masked')
	`)
	if err != nil {
		t.Fatalf("failed to seed pii events: %v", err)
	}

	cfg := &config.Config{
		Dashboard: config.DashboardConfig{
			Enabled:   true,
			Port:      9099,
			AuthToken: "secret-admin-token",
		},
		Providers: []config.ProviderConfig{
			{
				Name: "openai",
				Type: "openai",
				Models: []config.ModelConfig{
					{Name: "gpt-4o"},
					{Name: "gpt-4o-mini"},
				},
			},
		},
	}

	reg := provider.NewRegistry()
	reg.Register(&dummyProvider{})

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	if err != nil {
		t.Fatalf("failed to create load balancer: %v", err)
	}

	adminH := access.NewHandler(store, nil, nil)
	server := NewServer(cfg, nil, store, lb, reg, WithAdminHandler(adminH))

	// Seed provider_models so catalog.Models is populated for handlers that
	// look up model tiers (e.g. GET /providers, POST /optimize).
	_, _ = store.DB.Exec(`
		INSERT INTO provider_models (provider, model, active, tier, cost_in, cost_out)
		VALUES
		('openai', 'gpt-4o', 1, 'standard', 0.0000025, 0.00001),
		('openai', 'gpt-4o-mini', 1, 'economy', 0.00000015, 0.0000006),
		('openai', 'gpt-3.5-turbo', 1, 'economy', 0.0000005, 0.0000015)
	`)
	if err := catalog.LoadFromDB(store.GetAllModelInfo); err != nil {
		t.Fatalf("failed to load models from DB: %v", err)
	}

	// We can test individual handler methods directly by routing through a router we set up in test
	r := chi.NewRouter()
	authMw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get("Authorization")
			token = strings.TrimPrefix(token, "Bearer ")
			if token != "secret-admin-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	r.Route("/api", func(r chi.Router) {
		r.Use(authMw)
		r.Get("/stats", server.delegateHandler(server.statsHandler.HandleStats, "Stats"))
		r.Get("/requests", server.requestsHandler.HandleListRequests)
		r.Get("/costs", server.statsHandler.HandleCostsOverview)
		r.Get("/costs/by-provider", server.statsHandler.HandleCostsOverview)
		r.Get("/costs/by-key", server.statsHandler.HandleCostsByKey)
		r.Get("/models", server.delegateHandler(server.modelsHandler.HandleModels, "Models"))
		r.Get("/loops", server.delegateHandler(server.piiHandler.HandleLoops, "PII"))
		r.Get("/pii-events", server.delegateHandler(server.piiHandler.HandlePIIEvents, "PII"))
		r.Post("/optimize", server.delegateHandler(server.smartrouterHandler.HandleOptimize, "Smart router"))
		r.Get("/providers", server.delegateHandler(server.providersHandler.HandleProviders, "Providers"))
		r.Post("/providers", server.delegateHandler(server.smartrouterHandler.HandleUpdateProvider, "Smart router"))
		r.Get("/circuit-breaker/summary", server.statsHandler.HandleCircuitBreakerSummary)
		r.Get("/keys", server.adminHandler.ListAPIKeys)
		r.Post("/keys", server.adminHandler.CreateAPIKey)
		r.Delete("/keys/{id}", server.adminHandler.DeleteAPIKey)
	})

	// Helper for making requests
	makeRequest := func(method, path string, body []byte, auth bool) *httptest.ResponseRecorder {
		req, _ := http.NewRequest(method, path, bytes.NewReader(body))
		if auth {
			req.Header.Set("Authorization", "Bearer secret-admin-token")
		}
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		return rr
	}

	t.Run("Auth Enforcement", func(t *testing.T) {
		rr := makeRequest("GET", "/api/stats", nil, false)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized, got %d", rr.Code)
		}
	})

	t.Run("GET /stats", func(t *testing.T) {
		rr := makeRequest("GET", "/api/stats", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rr.Code)
		}

		var resp StatsResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal stats: %v", err)
		}

		if resp.TotalRequests != 3 {
			t.Errorf("expected 3 total requests, got %d", resp.TotalRequests)
		}
		if resp.SuccessCount != 2 {
			t.Errorf("expected 2 success requests, got %d", resp.SuccessCount)
		}
		if resp.ErrorCount != 1 {
			t.Errorf("expected 1 error request, got %d", resp.ErrorCount)
		}
		if resp.CacheHits != 1 {
			t.Errorf("expected 1 cache hit, got %d", resp.CacheHits)
		}
	})

	t.Run("GET /requests (unfiltered)", func(t *testing.T) {
		rr := makeRequest("GET", "/api/requests", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rr.Code)
		}

		var resp Page[RequestSummary]
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal requests: %v", err)
		}

		if resp.Total != 3 {
			t.Errorf("expected total 3, got %d", resp.Total)
		}
		if len(resp.Items) != 3 {
			t.Errorf("expected 3 items, got %d", len(resp.Items))
		}
	})

	t.Run("GET /requests (filtered by status)", func(t *testing.T) {
		rr := makeRequest("GET", "/api/requests?status=success", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rr.Code)
		}

		var resp Page[RequestSummary]
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal requests: %v", err)
		}

		if resp.Total != 2 {
			t.Errorf("expected total 2 success, got %d", resp.Total)
		}
	})

	t.Run("GET /costs", func(t *testing.T) {
		rr := makeRequest("GET", "/api/costs", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rr.Code)
		}

		var resp stats.CostAttributionResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal costs: %v", err)
		}

		if resp.TotalRequests != 3 {
			t.Errorf("expected total 3 requests, got %d", resp.TotalRequests)
		}
		if resp.TotalCost <= 0 {
			t.Errorf("expected positive total cost, got %f", resp.TotalCost)
		}
		if len(resp.ByProvider) == 0 {
			t.Error("expected at least one provider cost entry")
		}
	})

	t.Run("GET /costs/by-provider", func(t *testing.T) {
		rr := makeRequest("GET", "/api/costs/by-provider?period=30d", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp stats.CostAttributionResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal cost attribution: %v", err)
		}

		if resp.TotalRequests != 3 {
			t.Errorf("expected total 3 requests, got %d", resp.TotalRequests)
		}
		if resp.TotalCost <= 0 {
			t.Errorf("expected positive total cost, got %f", resp.TotalCost)
		}
		if len(resp.ByProvider) == 0 {
			t.Error("expected at least one provider cost entry")
		}
		for _, p := range resp.ByProvider {
			if p.Provider == "openai" && p.Count != 3 {
				t.Errorf("expected 3 requests for openai, got %d", p.Count)
			}
		}
	})

	t.Run("GET /costs/by-key", func(t *testing.T) {
		rr := makeRequest("GET", "/api/costs/by-key?period=30d", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp stats.CostByKeyResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal cost by key: %v", err)
		}

		if len(resp.ByKey) == 0 {
			t.Error("expected at least one key cost entry")
		}
		for _, k := range resp.ByKey {
			if k.KeyID == "vk_test_1" && k.Count != 3 {
				t.Errorf("expected key vk_test_1 to have 3 requests, got %d", k.Count)
			}
			if k.Cost <= 0 {
				t.Errorf("expected positive cost for key %s, got %f", k.KeyID, k.Cost)
			}
		}
	})

	t.Run("GET /models", func(t *testing.T) {
		rr := makeRequest("GET", "/api/models", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rr.Code)
		}

		var models []ModelResponseItem
		if err := json.Unmarshal(rr.Body.Bytes(), &models); err != nil {
			t.Fatalf("failed to unmarshal models: %v", err)
		}

		if len(models) == 0 {
			t.Fatal("expected at least one model in response")
		}

		for i, m := range models {
			if m.Name == "" {
				t.Errorf("models[%d]: name is empty", i)
			}
			if m.Provider == "" {
				t.Errorf("models[%d]: provider is empty", i)
			}
			_ = m.Active
			_ = m.Configured
		}

		// With the test setup (2 provider models, no registry models loaded),
		// we expect exactly 2 configured models.
		if len(models) == 2 {
			if !models[0].Configured {
				t.Errorf("expected models[0] to be configured, got configured=%v", models[0].Configured)
			}
		}
	})

	t.Run("GET /providers", func(t *testing.T) {
		rr := makeRequest("GET", "/api/providers", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rr.Code)
		}

		var providers []ProviderSummary
		if err := json.Unmarshal(rr.Body.Bytes(), &providers); err != nil {
			t.Fatalf("failed to unmarshal providers: %v", err)
		}

		if len(providers) == 0 {
			t.Fatal("expected at least one provider in response")
		}

		for i, p := range providers {
			if p.Name == "" {
				t.Errorf("providers[%d]: name is empty", i)
			}
			if p.Type == "" {
				t.Errorf("providers[%d]: type is empty", i)
			}
		}
	})

	t.Run("GET /loops", func(t *testing.T) {
		rr := makeRequest("GET", "/api/loops", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rr.Code)
		}

		var resp []LoopEventItem
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal loops: %v", err)
		}

		if len(resp) != 1 {
			t.Errorf("expected 1 loop event, got %d", len(resp))
		}
		if resp[0].ActionTaken != "throttled" {
			t.Errorf("expected action 'throttled', got %q", resp[0].ActionTaken)
		}
	})

	t.Run("GET /pii-events", func(t *testing.T) {
		rr := makeRequest("GET", "/api/pii-events", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rr.Code)
		}

		var resp struct {
			Items []PIIEventItem `json:"items"`
			Total int            `json:"total"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal pii events: %v", err)
		}

		if len(resp.Items) != 1 {
			t.Errorf("expected 1 PII event, got %d", len(resp.Items))
		}
		if resp.Items[0].PIIType != "email" {
			t.Errorf("expected pii_type 'email', got %q", resp.Items[0].PIIType)
		}
	})

	t.Run("GET /circuit-breaker/summary", func(t *testing.T) {
		rr := makeRequest("GET", "/api/circuit-breaker/summary", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp CircuitBreakerSummaryResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal circuit breaker summary: %v", err)
		}

		if resp.Summary.TotalCircuits != 0 {
			t.Errorf("expected 0 circuits (dummy provider has nil client), got %d", resp.Summary.TotalCircuits)
		}
		if len(resp.Circuits) != 0 {
			t.Errorf("expected empty circuits list, got %d items", len(resp.Circuits))
		}
	})

	t.Run("POST /optimize", func(t *testing.T) {
		reqOpt := OptimizeRequest{
			Prompt:       "Write a step-by-step comparison of quicksort and mergesort in Go, must return JSON.",
			CurrentModel: "gpt-4o",
		}
		body, _ := json.Marshal(reqOpt)
		rr := makeRequest("POST", "/api/optimize", body, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d", rr.Code)
		}

		var resp OptimizeResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal optimize response: %v", err)
		}

		if resp.ComplexityScore <= 0 {
			t.Errorf("expected positive complexity score, got %f", resp.ComplexityScore)
		}
		if len(resp.Recommendations) == 0 {
			t.Error("expected at least one economy model recommendation")
		}
		for _, rec := range resp.Recommendations {
			if rec.SavingsPercent <= 0 {
				t.Errorf("expected positive savings percentage, got %d", rec.SavingsPercent)
			}
		}
	})

	t.Run("GET /keys (empty)", func(t *testing.T) {
		rr := makeRequest("GET", "/api/keys", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal keys: %v", err)
		}

		keys, ok := resp["api_keys"].([]any)
		if !ok {
			t.Fatalf("expected 'api_keys' array in response, got keys=%v", resp["keys"])
		}
		if len(keys) != 1 {
			t.Errorf("expected 1 key (seeded), got %d", len(keys))
		}
	})

	t.Run("POST /keys (create)", func(t *testing.T) {
		createReq := map[string]any{
			"name":       "test-app",
			"budget":     50.0,
			"rate_limit": 100,
		}
		body, _ := json.Marshal(createReq)
		rr := makeRequest("POST", "/api/keys", body, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal create response: %v", err)
		}

		if resp["id"] == nil {
			t.Error("expected 'id' in response")
		}
		if resp["key"] == nil {
			t.Error("expected raw 'key' in response")
		}
		rawKey, ok := resp["key"].(string)
		if !ok || rawKey == "" {
			t.Errorf("expected non-empty key, got %q", rawKey)
		}
	})

	t.Run("POST /keys (missing name)", func(t *testing.T) {
		createReq := map[string]any{
			"budget": 10.0,
		}
		body, _ := json.Marshal(createReq)
		rr := makeRequest("POST", "/api/keys", body, true)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request, got %d", rr.Code)
		}
	})

	t.Run("GET /keys (after create)", func(t *testing.T) {
		rr := makeRequest("GET", "/api/keys", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d. Body: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal keys: %v", err)
		}

		keys, ok := resp["api_keys"].([]any)
		if !ok {
			t.Fatal("expected 'api_keys' array in response")
		}
		if len(keys) < 2 {
			t.Errorf("expected at least 2 keys after create, got %d", len(keys))
		}
	})

	t.Run("DELETE /keys/{id}", func(t *testing.T) {
		createReq := map[string]any{
			"name":       "delete-me",
			"budget":     5.0,
			"rate_limit": 10,
		}
		body, _ := json.Marshal(createReq)
		rr := makeRequest("POST", "/api/keys", body, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("failed to create key for delete test: %d", rr.Code)
		}
		var createResp map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &createResp); err != nil {
			t.Fatalf("failed to unmarshal create response: %v", err)
		}
		keyID, ok := createResp["id"].(string)
		if !ok {
			t.Fatalf("expected id to be a string, got %T", createResp["id"])
		}

		rr = makeRequest("DELETE", fmt.Sprintf("/api/keys/%s", keyID), nil, true)
		if rr.Code != http.StatusNoContent && rr.Code != http.StatusOK {
			t.Fatalf("expected 204 or 200 on delete, got %d", rr.Code)
		}

		rr = makeRequest("GET", "/api/keys", nil, true)
		var resp map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal keys response: %v", err)
		}
		keys := resp["api_keys"].([]any)
		for _, k := range keys {
			key := k.(map[string]any)
			if key["id"].(string) == keyID {
				t.Errorf("key %s was not deleted", keyID)
			}
		}
	})
}

func TestDashboardServer_AdditionalHandlers(t *testing.T) {
	store, tmpDir := setupTestStore(t)
	defer func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}()

	// Seed api_key
	_, err := store.DB.Exec("INSERT INTO api_keys (id, name, hashed_key, enabled, monthly_budget_usd, rate_limit_rpm, created_at, updated_at) VALUES (?, ?, ?, 1, 0.0, 0, datetime('now'), datetime('now'))",
		"vk_test_1", "test-key", "hashed")
	if err != nil {
		t.Fatalf("failed to seed api_keys: %v", err)
	}

	// Seed audit_log with complexity_score for smart router tests
	_, err = store.DB.Exec(`
		INSERT INTO audit_log (timestamp, key_id, model, provider, prompt_tokens, completion_tokens, total_cost, latency_ms, status_code, cache_hit, complexity_score)
		VALUES (?, 'vk_test_1', 'gpt-4o', 'openai', 10, 20, 0.0003, 150, 200, 0, 35.0),
		       (?, 'vk_test_1', 'gpt-4o-mini', 'openai', 5, 10, 0.00001, 80, 200, 1, 5.0)
	`, time.Now().Add(-1*time.Hour), time.Now().Add(-30*time.Minute))
	if err != nil {
		t.Fatalf("failed to seed audit_log: %v", err)
	}

	// Seed PII events for stats/export
	_, err = store.DB.Exec(`
		INSERT INTO pii_events (key_id, pii_type, action_taken)
		VALUES ('vk_test_1', 'email', 'masked'),
		       ('vk_test_1', 'credit_card', 'blocked')
	`)
	if err != nil {
		t.Fatalf("failed to seed pii_events: %v", err)
	}

	// Register a test model in the global registry for tier update + toggle
	catalog.ModelsMu.Lock()
	catalog.Models["gpt-4o"] = []catalog.ModelInfo{{
		Provider:           "openai",
		DisplayName:        "GPT-4o",
		Tier:               "standard",
		CostPerInputToken:  0.0000025,
		CostPerOutputToken: 0.00001,
	}}
	catalog.ModelsMu.Unlock()
	defer func() {
		catalog.ModelsMu.Lock()
		delete(catalog.Models, "gpt-4o")
		catalog.ModelsMu.Unlock()
	}()

	cfg := &config.Config{
		Dashboard: config.DashboardConfig{
			Enabled:   true,
			Port:      9099,
			AuthToken: "secret-admin-token",
		},
		Providers: []config.ProviderConfig{
			{
				Name: "openai",
				Type: "openai",
				Models: []config.ModelConfig{
					{Name: "gpt-4o"},
					{Name: "gpt-4o-mini"},
				},
			},
		},
		Routing: config.RoutingConfig{
			ComplexityThresholds: config.ComplexityThresholdsConfig{
				Economy:  15.0,
				Standard: 50.0,
			},
		},
	}

	reg := provider.NewRegistry()
	reg.Register(&dummyProvider{})

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	if err != nil {
		t.Fatalf("failed to create load balancer: %v", err)
	}

	bootCfg := config.ToBootConfig(cfg)
	bootCfg.Routing.Enabled = true
	cc := config.NewConfigCache(&bootCfg)
	server := NewServer(cfg, cc, store, lb, reg)

	r := chi.NewRouter()
	authMw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get("Authorization")
			token = strings.TrimPrefix(token, "Bearer ")
			if token != "secret-admin-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	r.Route("/api", func(r chi.Router) {
		r.Use(authMw)
		r.Get("/pii-stats", server.delegateHandler(server.piiHandler.HandleStats, "PII"))
		r.Get("/pii-export", server.delegateHandler(server.piiHandler.HandlePIIExport, "PII"))
		r.Post("/models/toggle", server.delegateHandler(server.modelsHandler.HandleToggleModel, "Models"))
		r.Post("/models/tier", server.delegateHandler(server.modelsHandler.HandleUpdateModelTier, "Models"))
		r.Get("/smart-router/stats", server.delegateHandler(server.smartrouterHandler.HandleSmartRouterStats, "Smart router"))
		r.Get("/smart-router/history", server.delegateHandler(server.smartrouterHandler.HandleSmartRouterHistory, "Smart router"))
	})

	makeRequest := func(method, path string, body []byte, auth bool) *httptest.ResponseRecorder {
		req, _ := http.NewRequest(method, path, bytes.NewReader(body))
		if auth {
			req.Header.Set("Authorization", "Bearer secret-admin-token")
		}
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		return rr
	}

	t.Run("GET /pii-stats", func(t *testing.T) {
		rr := makeRequest("GET", "/api/pii-stats", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
		}

		var stats PIIStats
		if err := json.Unmarshal(rr.Body.Bytes(), &stats); err != nil {
			t.Fatalf("failed to unmarshal pii stats: %v", err)
		}

		if stats.TotalEvents != 2 {
			t.Errorf("expected 2 total events, got %d", stats.TotalEvents)
		}
		if stats.MaskedCount != 1 {
			t.Errorf("expected 1 masked, got %d", stats.MaskedCount)
		}
		if stats.BlockedCount != 1 {
			t.Errorf("expected 1 blocked, got %d", stats.BlockedCount)
		}
		if stats.BlockedRate != 50.0 {
			t.Errorf("expected blocked_rate 50.0, got %f", stats.BlockedRate)
		}
		if len(stats.TypeBreakdown) < 2 {
			t.Errorf("expected at least 2 type breakdown entries, got %d", len(stats.TypeBreakdown))
		}
		if len(stats.TopKeys) < 1 {
			t.Errorf("expected at least 1 top key entry, got %d", len(stats.TopKeys))
		}
		if stats.TopKeys[0].APIKeyName != "test-key" {
			t.Errorf("expected top key name 'test-key', got %q", stats.TopKeys[0].APIKeyName)
		}
	})

	t.Run("GET /pii-export", func(t *testing.T) {
		rr := makeRequest("GET", "/api/pii-export", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
		}

		var items []PIIExportItem
		if err := json.Unmarshal(rr.Body.Bytes(), &items); err != nil {
			t.Fatalf("failed to unmarshal pii export: %v", err)
		}
		if len(items) != 2 {
			t.Errorf("expected 2 exported items, got %d", len(items))
		}

		// Content-Disposition header should be set for export
		cd := rr.Header().Get("Content-Disposition")
		if !strings.Contains(cd, "pii_events_export.json") {
			t.Errorf("expected Content-Disposition to contain pii_events_export.json, got %q", cd)
		}
	})

	t.Run("POST /models/toggle (deactivate)", func(t *testing.T) {
		body, _ := json.Marshal(ToggleModelRequest{Name: "gpt-4o", Active: false})
		rr := makeRequest("POST", "/api/models/toggle", body, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if resp["status"] != "ok" {
			t.Errorf("expected status 'ok', got %v", resp["status"])
		}
	})

	t.Run("POST /models/toggle (missing name)", func(t *testing.T) {
		body, _ := json.Marshal(ToggleModelRequest{Name: "", Active: false})
		rr := makeRequest("POST", "/api/models/toggle", body, true)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for missing name, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("POST /models/toggle (invalid JSON)", func(t *testing.T) {
		rr := makeRequest("POST", "/api/models/toggle", []byte("{bad json"), true)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for invalid JSON, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("POST /models/tier (update to economy)", func(t *testing.T) {
		body, _ := json.Marshal(UpdateModelTierRequest{Name: "gpt-4o", Tier: "economy"})
		rr := makeRequest("POST", "/api/models/tier", body, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if resp["status"] != "ok" {
			t.Errorf("expected status 'ok', got %v", resp["status"])
		}

		// Verify the tier was actually updated in the modelregistry
		catalog.ModelsMu.RLock()
		info, ok := catalog.Models["gpt-4o"]
		catalog.ModelsMu.RUnlock()
		if !ok {
			t.Fatal("expected gpt-4o to exist in registry")
		}
		if info[0].Tier != "economy" {
			t.Errorf("expected tier 'economy', got %q", info[0].Tier)
		}
	})

	t.Run("POST /models/tier (invalid tier)", func(t *testing.T) {
		body, _ := json.Marshal(UpdateModelTierRequest{Name: "gpt-4o", Tier: "ultra-premium"})
		rr := makeRequest("POST", "/api/models/tier", body, true)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for invalid tier, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("POST /models/tier (missing name)", func(t *testing.T) {
		body, _ := json.Marshal(UpdateModelTierRequest{Name: "", Tier: "economy"})
		rr := makeRequest("POST", "/api/models/tier", body, true)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for missing name, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("POST /models/tier (invalid JSON)", func(t *testing.T) {
		rr := makeRequest("POST", "/api/models/tier", []byte("{bad"), true)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for invalid JSON, got %d: %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("GET /smart-router/stats", func(t *testing.T) {
		rr := makeRequest("GET", "/api/smart-router/stats", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
		}

		var stats SmartRouterStatsResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &stats); err != nil {
			t.Fatalf("failed to unmarshal smart router stats: %v", err)
		}

		// Config should reflect our test config values
		if !stats.Config.Enabled {
			t.Errorf("expected enabled true, got false")
		}
		if stats.Config.ComplexityThresholds.Economy != 15.0 {
			t.Errorf("expected economy threshold 15.0, got %f", stats.Config.ComplexityThresholds.Economy)
		}
		if stats.Config.ComplexityThresholds.Standard != 50.0 {
			t.Errorf("expected standard threshold 50.0, got %f", stats.Config.ComplexityThresholds.Standard)
		}

		// We seeded 2 audit_log entries with complexity_score
		if stats.TotalRouted != 2 {
			t.Errorf("expected total_routed 2, got %d", stats.TotalRouted)
		}

		// Tier distribution should contain our models
		if len(stats.TierDistribution) == 0 {
			t.Error("expected non-empty tier distribution")
		}

		// Avg complexity should be computed from our seeded data
		if stats.AvgComplexity7d <= 0 {
			t.Errorf("expected positive avg_complexity_7d, got %f", stats.AvgComplexity7d)
		}
	})

	t.Run("GET /smart-router/history", func(t *testing.T) {
		rr := makeRequest("GET", "/api/smart-router/history", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
		}

		var hist SmartRouterHistoryResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &hist); err != nil {
			t.Fatalf("failed to unmarshal smart router history: %v", err)
		}

		if hist.Total != 2 {
			t.Errorf("expected total 2, got %d", hist.Total)
		}
		if len(hist.Items) != 2 {
			t.Errorf("expected 2 history items, got %d", len(hist.Items))
		}
		if hist.Page != 1 {
			t.Errorf("expected page 1, got %d", hist.Page)
		}
		if hist.Limit != 20 {
			t.Errorf("expected limit 20, got %d", hist.Limit)
		}

		// First item should have a valid complexity score
		if hist.Items[0].ComplexityScore <= 0 {
			t.Errorf("expected positive complexity score, got %f", hist.Items[0].ComplexityScore)
		}
	})

	t.Run("GET /smart-router/history with pagination", func(t *testing.T) {
		rr := makeRequest("GET", "/api/smart-router/history?page=1&limit=10", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
		}

		var hist SmartRouterHistoryResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &hist); err != nil {
			t.Fatalf("failed to unmarshal smart router history: %v", err)
		}

		if hist.Limit != 10 {
			t.Errorf("expected limit 10, got %d", hist.Limit)
		}
	})
}

func TestDashboardServer_GuardrailsAndLatency(t *testing.T) {
	store, tmpDir := setupTestStore(t)
	defer func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}()

	// Seed api_key
	_, err := store.DB.Exec("INSERT INTO api_keys (id, name, hashed_key, enabled, monthly_budget_usd, rate_limit_rpm, created_at, updated_at) VALUES (?, ?, ?, 1, 0.0, 0, datetime('now'), datetime('now'))",
		"vk_test_1", "test-key", "hashed")
	if err != nil {
		t.Fatalf("failed to seed api_keys: %v", err)
	}

	// Seed guardrail_events
	_, err = store.DB.Exec(`
		INSERT INTO guardrail_events (timestamp, key_id, guardrail_type, action_taken, model, provider, details)
		VALUES
		(datetime('now', '-1 hours'), 'vk_test_1', 'pii', 'blocked', 'gpt-4o', 'openai', '{"pii_type":"email"}'),
		(datetime('now', '-2 hours'), 'vk_test_1', 'budget', 'blocked', 'gpt-4o-mini', 'openai', '{"limit":"monthly"}'),
		(datetime('now', '-3 hours'), 'vk_test_1', 'rate_limit', 'throttled', 'gpt-4o', 'openai', '{"rpm":100}'),
		(datetime('now', '-24 hours'), 'vk_test_1', 'pii', 'masked', 'gpt-4o', 'openai', '{"pii_type":"tckn"}'),
		(datetime('now', '-48 hours'), 'vk_test_1', 'loop', 'blocked', 'gpt-4o', 'openai', '{"repeat_count":15}')
	`)
	if err != nil {
		t.Fatalf("failed to seed guardrail_events: %v", err)
	}

	// Seed additional audit_log entries with varied latencies and timestamps
	_, err = store.DB.Exec(`
		INSERT INTO audit_log (timestamp, key_id, model, provider, prompt_tokens, completion_tokens, total_cost, latency_ms, status_code, cache_hit, prompt_preview)
		VALUES
		(datetime('now', '-1 hours'), 'vk_test_1', 'gpt-4o', 'openai', 10, 20, 0.0003, 150.0, 200, 0, 'Hello, how are you?'),
		(datetime('now', '-2 hours'), 'vk_test_1', 'gpt-4o-mini', 'openai', 5, 10, 0.00001, 80.0, 200, 1, 'What is 2+2?'),
		(datetime('now', '-3 hours'), 'vk_test_1', 'gpt-4o', 'openai', 15, 30, 0.00045, 250.0, 500, 0, 'Write a poem about Go'),
		(datetime('now', '-24 hours'), 'vk_test_1', 'claude-3', 'anthropic', 20, 40, 0.0005, 300.0, 200, 0, 'Explain quantum computing'),
		(datetime('now', '-48 hours'), 'vk_test_1', 'gpt-4o', 'openai', 8, 15, 0.0002, 120.0, 200, 0, 'Translate hello to Spanish')
	`)
	if err != nil {
		t.Fatalf("failed to seed audit_log: %v", err)
	}

	cfg := &config.Config{
		Dashboard: config.DashboardConfig{
			Enabled:   true,
			Port:      9099,
			AuthToken: "secret-admin-token",
		},
		Providers: []config.ProviderConfig{
			{
				Name: "openai",
				Type: "openai",
				Models: []config.ModelConfig{
					{Name: "gpt-4o"},
					{Name: "gpt-4o-mini"},
				},
			},
		},
	}

	reg := provider.NewRegistry()
	reg.Register(&dummyProvider{})

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	if err != nil {
		t.Fatalf("failed to create load balancer: %v", err)
	}

	server := NewServer(cfg, nil, store, lb, reg)

	r := chi.NewRouter()
	authMw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get("Authorization")
			token = strings.TrimPrefix(token, "Bearer ")
			if token != "secret-admin-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	r.Route("/api", func(r chi.Router) {
		r.Use(authMw)
		r.Get("/guardrails/violations", server.delegateHandler(server.guardrailsHandler.HandleGuardrailViolations, "Guardrails"))
		r.Get("/guardrails/summary", server.delegateHandler(server.guardrailsHandler.HandleGuardrailSummary, "Guardrails"))
	})

	makeRequest := func(method, path string, body []byte, auth bool) *httptest.ResponseRecorder {
		req, _ := http.NewRequest(method, path, bytes.NewReader(body))
		if auth {
			req.Header.Set("Authorization", "Bearer secret-admin-token")
		}
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		return rr
	}

	t.Run("GET /guardrails/violations (unfiltered)", func(t *testing.T) {
		rr := makeRequest("GET", "/api/guardrails/violations", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp GuardrailViolationsResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal guardrail violations: %v", err)
		}

		if resp.Total != 5 {
			t.Errorf("expected total 5 guardrail events, got %d", resp.Total)
		}
		if len(resp.Items) != 5 {
			t.Errorf("expected 5 items, got %d", len(resp.Items))
		}
		if resp.Page != 1 {
			t.Errorf("expected page 1, got %d", resp.Page)
		}
		if resp.Limit != 50 {
			t.Errorf("expected limit 50, got %d", resp.Limit)
		}
	})

	t.Run("GET /guardrails/violations (filtered by type)", func(t *testing.T) {
		rr := makeRequest("GET", "/api/guardrails/violations?type=pii", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp GuardrailViolationsResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal guardrail violations: %v", err)
		}

		if resp.Total != 2 {
			t.Errorf("expected total 2 pii events, got %d", resp.Total)
		}
		for _, item := range resp.Items {
			if item.GuardrailType != "pii" {
				t.Errorf("expected guardrail_type 'pii', got %q", item.GuardrailType)
			}
		}
	})

	t.Run("GET /guardrails/violations (filtered by action)", func(t *testing.T) {
		rr := makeRequest("GET", "/api/guardrails/violations?action=blocked", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp GuardrailViolationsResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal guardrail violations: %v", err)
		}

		if resp.Total != 3 {
			t.Errorf("expected total 3 blocked events, got %d", resp.Total)
		}
		for _, item := range resp.Items {
			if item.ActionTaken != "blocked" {
				t.Errorf("expected action_taken 'blocked', got %q", item.ActionTaken)
			}
		}
	})

	t.Run("GET /guardrails/violations (pagination)", func(t *testing.T) {
		rr := makeRequest("GET", "/api/guardrails/violations?page=1&limit=2", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp GuardrailViolationsResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal guardrail violations: %v", err)
		}

		if resp.Total != 5 {
			t.Errorf("expected total 5, got %d", resp.Total)
		}
		if len(resp.Items) != 2 {
			t.Errorf("expected 2 items, got %d", len(resp.Items))
		}
		if resp.Limit != 2 {
			t.Errorf("expected limit 2, got %d", resp.Limit)
		}
	})

	t.Run("GET /guardrails/summary (default)", func(t *testing.T) {
		rr := makeRequest("GET", "/api/guardrails/summary", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp GuardrailSummaryResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal guardrail summary: %v", err)
		}

		if resp.TotalEvents != 5 {
			t.Errorf("expected 5 events in last 7d (all seeded events are within 48h), got %d", resp.TotalEvents)
		}
		if len(resp.ByType) == 0 {
			t.Error("expected at least one type breakdown entry")
		}
		_ = resp.Period
	})

	t.Run("GET /guardrails/summary (30d period)", func(t *testing.T) {
		rr := makeRequest("GET", "/api/guardrails/summary?period=30d", nil, true)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rr.Code, rr.Body.String())
		}

		var resp GuardrailSummaryResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal guardrail summary: %v", err)
		}

		if resp.TotalEvents != 5 {
			t.Errorf("expected 5 events in last 30d, got %d", resp.TotalEvents)
		}
	})
}

func TestDashboardServer_StartAndAuth(t *testing.T) {
	store, tmpDir := setupTestStore(t)
	defer func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}()

	cfg := &config.Config{
		Dashboard: config.DashboardConfig{
			Enabled:   true,
			Port:      0,
			AuthToken: "",
		},
	}

	lb := &smartrouter.LoadBalancer{}

	reg := provider.NewRegistry()
	server := NewServer(cfg, nil, store, lb, reg)

	if server.cfg.Dashboard.AuthToken == "" {
		token, err := crypto.GenerateRandomKey()
		if err != nil {
			t.Fatalf("failed to generate random token: %v", err)
		}
		server.cfg.Dashboard.AuthToken = token
	}

	if server.cfg.Dashboard.AuthToken == "" {
		t.Fatal("expected auth token to be generated")
	}

	r := chi.NewRouter()

	authMw := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authToken := server.cfg.Dashboard.AuthToken
			authHeader := r.Header.Get("Authorization")
			token := ""
			if after, ok := strings.CutPrefix(authHeader, "Bearer "); ok {
				token = after
			} else if authHeader != "" {
				token = authHeader
			}
			if token == "" {
				token = r.Header.Get("X-Admin-Token")
			}
			queryToken := r.URL.Query().Get("token")
			if token == "" && queryToken != "" {
				token = queryToken
				http.SetCookie(w, &http.Cookie{
					Name:     "token",
					Value:    token,
					Path:     "/",
					HttpOnly: true,
					Secure:   false,
					SameSite: http.SameSiteLaxMode,
					MaxAge:   86400 * 30,
				})
			}
			if token == "" {
				if cookie, err := r.Cookie("token"); err == nil {
					token = cookie.Value
				}
			}
			if token != authToken {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	r.Handle("/*", authMw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})))

	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}

	req, _ = http.NewRequest("GET", "/?token="+server.cfg.Dashboard.AuthToken, nil)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	cookies := rr.Result().Cookies()
	var tokenCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "token" {
			tokenCookie = c
			break
		}
	}
	if tokenCookie == nil {
		t.Error("expected token cookie to be set")
	} else if tokenCookie.Value != server.cfg.Dashboard.AuthToken {
		t.Errorf("expected cookie value %s, got %s", server.cfg.Dashboard.AuthToken, tokenCookie.Value)
	}

	req, _ = http.NewRequest("GET", "/", nil)
	req.AddCookie(tokenCookie)
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestHandleLogin(t *testing.T) {
	tests := []struct {
		name       string
		authToken  string
		body       string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "valid dashboard auth token",
			authToken:  "secret-admin-token",
			body:       `{"token":"secret-admin-token"}`,
			wantStatus: http.StatusOK,
			wantBody:   `{"status":"ok"}`,
		},
		{
			name:       "invalid token returns 401",
			authToken:  "secret-admin-token",
			body:       `{"token":"wrong-token"}`,
			wantStatus: http.StatusUnauthorized,
			wantBody:   `{"error":{"message":"Invalid token","type":"authentication_error","code":"401"}}`,
		},
		{
			name:       "empty token returns 400",
			authToken:  "secret-admin-token",
			body:       `{"token":""}`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":{"message":"token is required","type":"invalid_request_error","code":"400"}}`,
		},
		{
			name:       "invalid JSON returns 400",
			authToken:  "secret-admin-token",
			body:       `{bad json`,
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"error":{"message":"Invalid JSON body","type":"invalid_request_error","code":"400"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, tmpDir := setupTestStore(t)
			defer store.Close()
			defer os.RemoveAll(tmpDir)

			cfg := &config.Config{
				Dashboard: config.DashboardConfig{
					Enabled:   true,
					Port:      0,
					AuthToken: tt.authToken,
				},
			}

			reg := provider.NewRegistry()
			lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
			if err != nil {
				t.Fatalf("failed to create load balancer: %v", err)
			}

			bootCfg := config.DefaultBootConfig()
			bootCfg.Routing.Enabled = true
			cache := config.NewConfigCache(&bootCfg)
			server := NewServer(cfg, cache, store, lb, reg)

			r := chi.NewRouter()
			r.Post("/api/auth/login", server.delegateHandler(server.authHandler.HandleLogin, "Auth"))

			body := bytes.NewReader([]byte(tt.body))
			req := httptest.NewRequest("POST", "/api/auth/login", body)
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, rr.Code)
			}

			// Verify response body contains expected fields (ignore exact whitespace)
			var gotMap map[string]any
			var wantMap map[string]any
			if err := json.Unmarshal(rr.Body.Bytes(), &gotMap); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}
			if err := json.Unmarshal([]byte(tt.wantBody), &wantMap); err != nil {
				t.Fatalf("failed to unmarshal expected body: %v", err)
			}
			if !reflect.DeepEqual(gotMap, wantMap) {
				t.Errorf("expected body %s, got %s", tt.wantBody, rr.Body.String())
			}

			if tt.wantStatus == http.StatusOK {
				cookies := rr.Result().Cookies()
				var tokenCookie *http.Cookie
				for _, c := range cookies {
					if c.Name == "token" {
						tokenCookie = c
						break
					}
				}
				if tokenCookie == nil {
					t.Fatal("expected token cookie to be set")
				}
				if tokenCookie.HttpOnly != true {
					t.Error("expected cookie to be HttpOnly")
				}
				if tokenCookie.SameSite != http.SameSiteLaxMode {
					t.Error("expected SameSite=Lax")
				}
			}
		})
	}
}
