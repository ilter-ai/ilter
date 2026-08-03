package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
	"github.com/sony/gobreaker/v2"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/features/circuitbreaker"
	"github.com/ilter-ai/ilter/internal/features/loopdetect"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/middleware"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/provider"
	"github.com/ilter-ai/ilter/internal/proxy"

	"github.com/ilter-ai/ilter/internal/platform/rediskeys"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
	testutil "github.com/ilter-ai/ilter/tests/utils"
)

// setupBudgetE2E creates a router with BudgetEnforcer middleware and a simple OK handler.
func setupBudgetE2E(t *testing.T, rdb *redis.Client, monthlyLimit, dailyLimit float64) *chi.Mux {
	t.Helper()

	store, err := db.NewSQLiteStore(config.StorageConfig{Type: "sqlite", SqlitePath: ":memory:"})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	budgetMw := middleware.NewBudgetMiddleware(config.BudgetConfig{
		Enabled:             true,
		DefaultMonthlyLimit: monthlyLimit,
		DefaultDailyLimit:   dailyLimit,
	}, circuitbreaker.NewRedisBreaker(rdb, time.Second, gobreaker.Settings{}), store, nil)

	r := chi.NewRouter()
	r.Use(budgetMw.Handler)
	r.Post("/v1/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	return r
}

// setBudgetCtx injects API key, budget, and daily limit into a request context.
func setBudgetCtx(keyID string, monthlyBudget, dailyLimit float64) context.Context {
	ctx := context.WithValue(context.Background(), reqmeta.APIKeyIDContextKey, keyID)
	ctx = context.WithValue(ctx, reqmeta.APIKeyBudgetContextKey, monthlyBudget)
	ctx = context.WithValue(ctx, reqmeta.APIKeyDailyLimitContextKey, dailyLimit)
	return ctx
}

// TestBudgetE2E_MonthlyExceedAndReset verifies monthly budget rejection + reset.
func TestBudgetE2E_MonthlyExceedAndReset(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e budget test in short mode")
	}
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skip("Redis not available, skipping")
	}

	cleanKeys := func() {
		keys, _ := rdb.Keys(ctx, "ilter:budget:*").Result()
		for _, k := range keys {
			rdb.Del(ctx, k)
		}
	}
	cleanKeys()
	defer cleanKeys()

	r := setupBudgetE2E(t, rdb, 50.0, 10.0)

	// Pre-set monthly spent to exceed limit (60 > 50)
	now := time.Now()
	monthKey := rediskeys.BudgetKey("100", now)
	if err := rdb.Set(ctx, monthKey, "60.0", 0).Err(); err != nil {
		t.Fatalf("preset monthly budget: %v", err)
	}

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-mock",
		Messages: []model.Message{{Role: "user", Content: "Monthly budget test"}},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	// Request with key_id=100 and monthly budget 50.0
	req, _ := http.NewRequestWithContext(
		setBudgetCtx("100", 50.0, 10.0),
		"POST", "/v1/chat/completions", bytes.NewReader(bodyBytes),
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 for monthly exceed, got %d: %s", rr.Code, rr.Body.String())
	}
	var errResp map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &errResp)
	if e, _ := errResp["error"].(map[string]any); e["type"] != "budget_exceeded" {
		t.Errorf("expected budget_exceeded type, got %v", e["type"])
	}

	// Reset: clear monthly key → request passes
	rdb.Del(ctx, monthKey)
	rr2 := httptest.NewRecorder()
	r.ServeHTTP(rr2, req)
	if rr2.Code != http.StatusOK {
		t.Fatalf("expected 200 after reset, got %d: %s", rr2.Code, rr2.Body.String())
	}
}

// TestBudgetE2E_DailyExceedAndReset verifies daily budget rejection + reset.
func TestBudgetE2E_DailyExceedAndReset(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e budget test in short mode")
	}
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skip("Redis not available, skipping")
	}

	cleanKeys := func() {
		keys, _ := rdb.Keys(ctx, "ilter:budget:*").Result()
		for _, k := range keys {
			rdb.Del(ctx, k)
		}
	}
	cleanKeys()
	defer cleanKeys()

	r := setupBudgetE2E(t, rdb, 100.0, 5.0)

	now := time.Now()
	dayKey := rediskeys.DailyBudgetKey("200", now)
	if err := rdb.Set(ctx, dayKey, "8.0", 0).Err(); err != nil {
		t.Fatalf("preset daily budget: %v", err)
	}

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-mock",
		Messages: []model.Message{{Role: "user", Content: "Daily limit test"}},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequestWithContext(
		setBudgetCtx("200", 100.0, 5.0),
		"POST", "/v1/chat/completions", bytes.NewReader(bodyBytes),
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 for daily exceed, got %d: %s", rr.Code, rr.Body.String())
	}
	var errResp map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &errResp)
	if e, _ := errResp["error"].(map[string]any); e["type"] != "budget_exceeded" {
		t.Errorf("expected budget_exceeded type, got %v", e["type"])
	}

	// Reset daily key → passes
	rdb.Del(ctx, dayKey)
	rr2 := httptest.NewRecorder()
	r.ServeHTTP(rr2, req)
	if rr2.Code != http.StatusOK {
		t.Fatalf("expected 200 after daily reset, got %d: %s", rr2.Code, rr2.Body.String())
	}
}

// TestBudgetE2E_AdminBypass verifies admin key_id=0 bypasses budget.
func TestBudgetE2E_AdminBypass(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e budget test in short mode")
	}
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skip("Redis not available, skipping")
	}
	defer func() {
		keys, _ := rdb.Keys(ctx, "ilter:budget:*").Result()
		for _, k := range keys {
			rdb.Del(ctx, k)
		}
	}()

	r := setupBudgetE2E(t, rdb, 0.01, 0.01)

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-mock",
		Messages: []model.Message{{Role: "user", Content: "Admin bypass"}},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	// key_id=0 means admin → budget middleware skips
	req, _ := http.NewRequestWithContext(
		setBudgetCtx("0", 0.01, 0.01),
		"POST", "/v1/chat/completions", bytes.NewReader(bodyBytes),
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin bypass, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestBudgetE2E_BudgetHeaders verifies budget headers are set correctly.
func TestBudgetE2E_BudgetHeaders(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e budget test in short mode")
	}
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skip("Redis not available, skipping")
	}
	defer func() {
		keys, _ := rdb.Keys(ctx, "ilter:budget:*").Result()
		for _, k := range keys {
			rdb.Del(ctx, k)
		}
	}()

	r := setupBudgetE2E(t, rdb, 100.0, 10.0)

	now := time.Now()
	monthKey := rediskeys.BudgetKey("300", now)
	rdb.Set(ctx, monthKey, "30.0", 0)

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-mock",
		Messages: []model.Message{{Role: "user", Content: "Headers test"}},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequestWithContext(
		setBudgetCtx("300", 100.0, 10.0),
		"POST", "/v1/chat/completions", bytes.NewReader(bodyBytes),
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("X-Budget-Limit") != "100.0000" {
		t.Errorf("expected X-Budget-Limit 100.0000, got %s", rr.Header().Get("X-Budget-Limit"))
	}
	if rr.Header().Get("X-Budget-Remaining") != "70.0000" {
		t.Errorf("expected X-Budget-Remaining 70.0000, got %s", rr.Header().Get("X-Budget-Remaining"))
	}
	if rr.Header().Get("X-Budget-Daily-Limit") != "10.0000" {
		t.Errorf("expected X-Budget-Daily-Limit 10.0000, got %s", rr.Header().Get("X-Budget-Daily-Limit"))
	}
	if rr.Header().Get("X-Budget-Daily-Remaining") != "10.0000" {
		t.Errorf("expected X-Budget-Daily-Remaining 10.0000, got %s", rr.Header().Get("X-Budget-Daily-Remaining"))
	}

	rdb.Del(ctx, monthKey)
}

// contextInjector bypasses real auth by injecting API key context values directly.
func contextInjector(keyID string, monthlyBudget, dailyLimit float64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), reqmeta.APIKeyIDContextKey, keyID)
			ctx = context.WithValue(ctx, reqmeta.APIKeyBudgetContextKey, monthlyBudget)
			ctx = context.WithValue(ctx, reqmeta.APIKeyDailyLimitContextKey, dailyLimit)
			ctx = context.WithValue(ctx, reqmeta.APIKeyRateLimitContextKey, 1000)
			ctx, _ = reqmeta.InitRequestMetadata(ctx)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// TestBudgetLoopPII_CombinedE2E validates the full middleware chain:
//
//	Auth (bypassed via contextInjector) → BudgetEnforcer → PIIMasker → ChatCompletions handler (with loop detector)
//
// All four scenarios run independently with unique API key IDs to prevent state pollution.
func TestBudgetLoopPII_CombinedE2E(t *testing.T) {
	// ── Setup: miniredis (no real Redis needed) ──
	mr, err := miniredis.Run()
	require.NoError(t, err, "miniredis must start")
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })

	// ── Setup: in-memory SQLite store ──
	store, err := db.NewSQLiteStore(config.StorageConfig{Type: "sqlite", SqlitePath: ":memory:"})
	require.NoError(t, err, "in-memory SQLite store must start")
	t.Cleanup(func() { store.Close() })

	// ── Setup: ConfigCache with all feature flags enabled ──
	bootCfg := config.DefaultBootConfig()
	bootCfg.Budget.Enabled = true
	bootCfg.CostGuard.LoopDetection = true
	cfgCache := config.NewConfigCache(&bootCfg)

	// ── Setup: config ──
	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{Name: "mock", Type: "mock", Models: []config.ModelConfig{{Name: "gpt-mock", Weight: 1}}},
		},
		PII:     config.PIIConfig{Enabled: true, Mode: "reversible"},
		Routing: config.RoutingConfig{},
	}

	// ── Setup: provider registry + load balancer ──
	reg := provider.NewRegistry()
	reg.Register(testutil.NewMockProvider())
	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	require.NoError(t, err, "load balancer must initialize")

	// ── Setup: shared middleware (stateless across request-scoped keys) ──
	budgetMw := middleware.NewBudgetMiddleware(config.BudgetConfig{
		Enabled:             true,
		DefaultMonthlyLimit: 100.0,
		DefaultDailyLimit:   50.0,
	}, circuitbreaker.NewRedisBreaker(rdb, time.Second, gobreaker.Settings{}), store, cfgCache)

	piiMw := middleware.NewPIIMaskerMiddleware(store, cfg.PII, nil, nil)

	// ════════════════════════════════════════════════════════
	// Scenario 1: Normal flow – PII masking works alongside budget
	// ════════════════════════════════════════════════════════
	t.Run("Normal flow with PII reversible masking", func(t *testing.T) {
		detector := loopdetect.NewDetector(config.LoopSettingsConfig{
			SessionMaxRequests: 100, // high threshold – won't trigger
		})
		h := proxy.NewHandler(lb, nil, budgetMw.Enforcer(), detector)
		h.SetStore(store)
		h.SetConfig(cfg)

		r := chi.NewRouter()
		r.Use(contextInjector("10", 100.0, 50.0))
		r.Use(budgetMw.Handler)
		r.Use(piiMw.Handler)
		r.Post("/v1/chat/completions", h.ChatCompletions)

		reqBody := model.ChatCompletionRequest{
			Model:    "gpt-mock",
			Messages: []model.Message{{Role: "user", Content: "My email is user@example.com"}},
		}
		bodyBytes, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		require.Equal(t, http.StatusOK, rr.Code, "normal flow should return 200")
		// Budget headers prove the budget middleware ran
		require.NotEmpty(t, rr.Header().Get("X-Budget-Limit"), "X-Budget-Limit must be set")
		require.NotEmpty(t, rr.Header().Get("X-Budget-Remaining"), "X-Budget-Remaining must be set")
		// Proxy handler returned the mock provider response
		require.True(t, strings.Contains(rr.Body.String(), "mock response"),
			"response body should contain the provider response")
	})

	// ════════════════════════════════════════════════════════
	// Scenario 2: Budget exceeded – 429 before reaching handler
	// ════════════════════════════════════════════════════════
	t.Run("Budget exceeded returns 429", func(t *testing.T) {
		detector := loopdetect.NewDetector(config.LoopSettingsConfig{})
		h := proxy.NewHandler(lb, nil, budgetMw.Enforcer(), detector)
		h.SetStore(store)
		h.SetConfig(cfg)

		// Preset monthly spend above limit for key_id=20 (micro-dollars: $150)
		now := time.Now()
		monthKey := rediskeys.BudgetKey("20", now)
		spentMicro := int64(150.0 * 1_000_000)
		require.NoError(t, rdb.Set(context.Background(), monthKey, spentMicro, 0).Err())
		t.Cleanup(func() { rdb.Del(context.Background(), monthKey) })

		r := chi.NewRouter()
		r.Use(contextInjector("20", 100.0, 50.0))
		r.Use(budgetMw.Handler)
		r.Use(piiMw.Handler)
		r.Post("/v1/chat/completions", h.ChatCompletions)

		reqBody := model.ChatCompletionRequest{
			Model:    "gpt-mock",
			Messages: []model.Message{{Role: "user", Content: "Hello world"}},
		}
		bodyBytes, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		require.Equal(t, http.StatusTooManyRequests, rr.Code, "budget exceeded must return 429")
		var errResp struct {
			Error struct {
				Type string `json:"type"`
			} `json:"error"`
		}
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &errResp))
		require.Equal(t, "budget_exceeded", errResp.Error.Type)
	})

	// ════════════════════════════════════════════════════════
	// Scenario 3: Loop detection – repeated identical requests blocked
	// ════════════════════════════════════════════════════════
	t.Run("Loop detection blocks on repeated requests", func(t *testing.T) {
		// Aggressive thresholds: 2nd identical request in same session triggers 3+ signals → blocked
		detector := loopdetect.NewDetector(config.LoopSettingsConfig{
			RateThreshold:         2, // 2 requests in 1 min window
			FingerprintWindow:     5,
			FingerprintDuplicates: 2, // 2 duplicates of same message hash
			SessionMaxRequests:    1, // 2nd request exceeds session max
		})
		loopCfg := &config.Config{
			Providers: cfg.Providers,
			PII:       cfg.PII,
			Routing:   cfg.Routing,
			CostGuard: config.CostGuardConfig{LoopDetection: true},
		}
		loopMw := middleware.NewLoopDetectorMiddleware(detector, nil, cfgCache)
		h := proxy.NewHandler(lb, nil, budgetMw.Enforcer(), detector)
		h.SetStore(store)
		h.SetConfig(loopCfg)

		r := chi.NewRouter()
		r.Use(contextInjector("30", 100.0, 50.0))
		r.Use(budgetMw.Handler)
		r.Use(loopMw.Handler)
		r.Use(piiMw.Handler)
		r.Post("/v1/chat/completions", h.ChatCompletions)

		reqBody := model.ChatCompletionRequest{
			Model:    "gpt-mock",
			Messages: []model.Message{{Role: "user", Content: "Hello Loop"}},
		}
		bodyBytes, _ := json.Marshal(reqBody)
		sessionID := "session-loop-30"

		// Request 1 – should pass through
		req1 := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
		req1.Header.Set("Content-Type", "application/json")
		req1.Header.Set("X-Ilter-Session-Id", sessionID)
		rr1 := httptest.NewRecorder()
		r.ServeHTTP(rr1, req1)
		require.Equal(t, http.StatusOK, rr1.Code, "first request should succeed")

		// Request 2 – same content, same session → triggers 3 signals → blocked
		req2 := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
		req2.Header.Set("Content-Type", "application/json")
		req2.Header.Set("X-Ilter-Session-Id", sessionID)
		rr2 := httptest.NewRecorder()
		r.ServeHTTP(rr2, req2)

		require.Equal(t, http.StatusTooManyRequests, rr2.Code, "repeated request must return 429")
		var errResp struct {
			Error struct {
				Type string `json:"type"`
			} `json:"error"`
		}
		require.NoError(t, json.Unmarshal(rr2.Body.Bytes(), &errResp))
		require.Equal(t, "loop_detected", errResp.Error.Type)
	})

	// ═══════════════════════════════════════════════════════════════
	// Scenario 4: Budget exceeded with PII in body – budget wins first
	// ═══════════════════════════════════════════════════════════════
	t.Run("Budget exceeded with PII content returns 429 before PII processing", func(t *testing.T) {
		detector := loopdetect.NewDetector(config.LoopSettingsConfig{})
		h := proxy.NewHandler(lb, nil, budgetMw.Enforcer(), detector)
		h.SetStore(store)
		h.SetConfig(cfg)

		// Preset monthly spend above limit for key_id=40 (micro-dollars: $150)
		now := time.Now()
		monthKey := rediskeys.BudgetKey("40", now)
		spentMicro := int64(150.0 * 1_000_000)
		require.NoError(t, rdb.Set(context.Background(), monthKey, spentMicro, 0).Err())
		t.Cleanup(func() { rdb.Del(context.Background(), monthKey) })

		r := chi.NewRouter()
		r.Use(contextInjector("40", 100.0, 50.0))
		r.Use(budgetMw.Handler)
		r.Use(piiMw.Handler)
		r.Post("/v1/chat/completions", h.ChatCompletions)

		reqBody := model.ChatCompletionRequest{
			Model:    "gpt-mock",
			Messages: []model.Message{{Role: "user", Content: "My email is john@test.com and SSN is 123-45-6789"}},
		}
		bodyBytes, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		require.Equal(t, http.StatusTooManyRequests, rr.Code, "should get 429 from budget, not PII")
		var errResp struct {
			Error struct {
				Type string `json:"type"`
			} `json:"error"`
		}
		require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &errResp))
		require.Equal(t, "budget_exceeded", errResp.Error.Type,
			"error type must be budget_exceeded (budget middleware runs before PII)")
	})
}
