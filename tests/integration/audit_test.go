package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/db/dbtest"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/middleware"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
	"github.com/ilter-ai/ilter/internal/provider"
	"github.com/ilter-ai/ilter/internal/proxy"
	testutil "github.com/ilter-ai/ilter/tests/utils"

	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

// mockBodyProvider returns a deterministic response for audit body testing.
type mockBodyProvider struct{}

func (m *mockBodyProvider) Name() string { return "mock" }
func (m *mockBodyProvider) Type() string { return "mock" }
func (m *mockBodyProvider) TransformRequest(ctx context.Context, _ *model.ChatCompletionRequest) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, "POST", "http://mock", nil)
}

func (m *mockBodyProvider) TransformResponse(_ context.Context, _ *http.Response) (*model.ChatCompletionResponse, error) {
	return &model.ChatCompletionResponse{
		ID:      "resp-test-1",
		Model:   "mock-model",
		Choices: []model.Choice{{Message: model.ChoiceMessage{Role: "assistant", Content: "Mock response content here."}}},
		Usage:   &model.Usage{PromptTokens: 15, CompletionTokens: 12, TotalTokens: 27},
	}, nil
}
func (m *mockBodyProvider) HealthCheck(_ context.Context) error { return nil }
func (m *mockBodyProvider) DiscoverModels(_ context.Context) ([]catalog.ModelInfo, error) {
	return nil, nil
}

func (m *mockBodyProvider) Client() *http.Client {
	return &http.Client{
		Transport: testutil.RoundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
		}),
	}
}

// buildTestHarness creates a full test setup with proxy + auth + audit.
func buildTestHarness(t *testing.T, logBodies bool) (*db.SQLiteStore, *middleware.AuditLoggerMiddleware, *chi.Mux, func()) {
	t.Helper()

	store := dbtest.NewFile(t)
	al := middleware.NewAuditLoggerMiddleware(store)

	reg := provider.NewRegistry()
	reg.Register(&mockBodyProvider{})

	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{
			{Name: "mock", Type: "mock", Models: []config.ModelConfig{
				{Name: "gpt-mock", Weight: 1, CostPerInputToken: 0.001, CostPerOutputToken: 0.002},
			}},
		},
		Audit: config.AuditConfig{
			Enabled:       true,
			LogPrompts:    true,
			LogBodies:     logBodies,
			RetentionDays: 30,
		},
	}

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	require.NoError(t, err)

	h := proxy.NewHandler(lb, al, nil, nil)
	h.SetConfig(cfg)
	h.SetStore(store)

	authMw := middleware.NewAuthMiddleware(config.AuthConfig{AdminKey: testutil.AdminKey}, nil)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, _ := reqmeta.InitRequestMetadata(r.Context())
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Use(authMw.Handler)
	r.Post("/v1/chat/completions", h.ChatCompletions)

	cleanup := func() {
		al.Close()
	}
	return store, al, r, cleanup
}

// ─────────────────────────────────────────────────────────────
// TEST 1: LogBodies=true  → request_body AND response_body stored
// ─────────────────────────────────────────────────────────────
func TestAudit_LogBodiesEnabled(t *testing.T) {
	store, _, router, cleanup := buildTestHarness(t, true)
	defer cleanup()

	reqBody := model.ChatCompletionRequest{
		Model: "gpt-mock",
		Messages: []model.Message{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "What is the capital of France?"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+testutil.AdminKey)
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code, "proxy handler should return 200")

	// Wait for async audit logger to flush
	time.Sleep(300 * time.Millisecond)

	// ── Read from DB ──
	var (
		modelName     string
		reqBodyDB     sql.NullString
		respBodyDB    sql.NullString
		promptPrev    sql.NullString
		promptTok     int
		completionTok int
	)
	err := store.DB.QueryRow(
		`SELECT model, request_body, response_body, prompt_preview, prompt_tokens, completion_tokens
		 FROM audit_log ORDER BY id DESC LIMIT 1`,
	).Scan(&modelName, &reqBodyDB, &respBodyDB, &promptPrev, &promptTok, &completionTok)
	require.NoError(t, err, "audit_log should contain the proxied request")

	assert.Equal(t, "gpt-mock", modelName)

	// Tokens from mock provider response
	assert.Equal(t, 15, promptTok)
	assert.Equal(t, 12, completionTok)

	// Prompt preview (LogPrompts=true)
	assert.True(t, promptPrev.Valid)
	assert.Contains(t, promptPrev.String, "capital of France")

	// ═══ REQUEST BODY ═══
	assert.True(t, reqBodyDB.Valid, "request_body must be stored when LogBodies=true")
	require.True(t, reqBodyDB.Valid)
	t.Logf("  request_body: %s", reqBodyDB.String)

	var storedReq map[string]any
	err = json.Unmarshal([]byte(reqBodyDB.String), &storedReq)
	require.NoError(t, err, "request_body must be valid JSON")

	messages, ok := storedReq["messages"].([]any)
	require.True(t, ok, "request_body must contain 'messages' array")
	require.Len(t, messages, 2)

	msg0 := messages[0].(map[string]any)
	assert.Equal(t, "system", msg0["role"])
	assert.Equal(t, "You are a helpful assistant.", msg0["content"])

	msg1 := messages[1].(map[string]any)
	assert.Equal(t, "user", msg1["role"])
	assert.Equal(t, "What is the capital of France?", msg1["content"])

	// ═══ RESPONSE BODY ═══
	assert.True(t, respBodyDB.Valid, "response_body must be stored when LogBodies=true")
	require.True(t, respBodyDB.Valid)
	t.Logf("  response_body: %s", respBodyDB.String)

	var storedResp model.ChatCompletionResponse
	err = json.Unmarshal([]byte(respBodyDB.String), &storedResp)
	require.NoError(t, err, "response_body must be valid JSON")
	assert.Equal(t, "resp-test-1", storedResp.ID)
	require.Len(t, storedResp.Choices, 1)
	assert.Equal(t, "Mock response content here.", storedResp.Choices[0].Message.Content)
	require.NotNil(t, storedResp.Usage)
	assert.Equal(t, 15, storedResp.Usage.PromptTokens)
	assert.Equal(t, 12, storedResp.Usage.CompletionTokens)
}

// ─────────────────────────────────────────────────────────────
// TEST 2: LogBodies=false → request_body AND response_body NULL
// ─────────────────────────────────────────────────────────────
func TestAudit_LogBodiesDisabled(t *testing.T) {
	store, _, router, cleanup := buildTestHarness(t, false)
	defer cleanup()

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-mock",
		Messages: []model.Message{{Role: "user", Content: "Hi"}},
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+testutil.AdminKey)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	time.Sleep(300 * time.Millisecond)

	var reqBodyDB, respBodyDB sql.NullString
	err := store.DB.QueryRow(
		`SELECT request_body, response_body FROM audit_log ORDER BY id DESC LIMIT 1`,
	).Scan(&reqBodyDB, &respBodyDB)
	require.NoError(t, err)

	assert.False(t, reqBodyDB.Valid, "request_body must be NULL when LogBodies=false")
	assert.False(t, respBodyDB.Valid, "response_body must be NULL when LogBodies=false")
	t.Log("✓ LogBodies=false: both body columns are NULL in DB")
}

// ─────────────────────────────────────────────────────────────
// TEST 3: Multiple sequential requests all store bodies
// ─────────────────────────────────────────────────────────────
func TestAudit_LogBodiesMultipleRequests(t *testing.T) {
	store, _, router, cleanup := buildTestHarness(t, true)
	defer cleanup()

	prompts := []string{"First message", "Second message here", "Third and final"}
	for _, p := range prompts {
		reqBody := model.ChatCompletionRequest{
			Model:    "gpt-mock",
			Messages: []model.Message{{Role: "user", Content: p}},
		}
		bodyBytes, _ := json.Marshal(reqBody)
		req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
		req.Header.Set("Authorization", "Bearer "+testutil.AdminKey)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	}
	time.Sleep(300 * time.Millisecond)

	rows, err := store.DB.Query(
		`SELECT request_body, response_body FROM audit_log ORDER BY id ASC`,
	)
	require.NoError(t, err)
	defer rows.Close()

	count := 0
	for rows.Next() {
		var reqB, respB sql.NullString
		require.NoError(t, rows.Scan(&reqB, &respB))
		assert.True(t, reqB.Valid, "row %d: request_body must be valid", count)
		assert.True(t, respB.Valid, "row %d: response_body must be valid", count)
		assert.Contains(t, reqB.String, prompts[count], "row %d: request_body should contain prompt text", count)
		assert.Contains(t, respB.String, "Mock response content here.")
		count++
	}
	assert.Equal(t, 3, count, "should have 3 audit log entries")
	t.Logf("✓ All %d requests have valid request_body and response_body", count)
}

// ─────────────────────────────────────────────────────────────
// TEST 4: Metadata still logged correctly when bodies disabled
// ─────────────────────────────────────────────────────────────
func TestAudit_BodiesDisabledStillLogsMetadata(t *testing.T) {
	store, _, router, cleanup := buildTestHarness(t, false)
	defer cleanup()

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-mock",
		Messages: []model.Message{{Role: "user", Content: "Hello"}},
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+testutil.AdminKey)

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	time.Sleep(300 * time.Millisecond)

	var modelName string
	var promptTokens, completionTokens, latencyMs int
	var totalCost float64
	var statusCode int
	var cacheHit bool
	err := store.DB.QueryRow(
		`SELECT model, prompt_tokens, completion_tokens, total_cost, latency_ms, status_code, cache_hit
		 FROM audit_log ORDER BY id DESC LIMIT 1`,
	).Scan(&modelName, &promptTokens, &completionTokens, &totalCost, &latencyMs, &statusCode, &cacheHit)
	require.NoError(t, err)

	assert.Equal(t, "gpt-mock", modelName)
	assert.Equal(t, 15, promptTokens)
	assert.Equal(t, 12, completionTokens)
	assert.True(t, totalCost >= 0)
	assert.True(t, latencyMs >= 0)
	assert.Equal(t, 200, statusCode)
	assert.False(t, cacheHit)
	t.Logf("✓ Metadata logging works correctly when LogBodies=false (model=%s, tokens=%d/%d, cost=$%.6f)",
		modelName, promptTokens, completionTokens, totalCost)
}
