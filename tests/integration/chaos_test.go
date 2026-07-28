package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
	"github.com/sony/gobreaker/v2"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/circuitbreaker"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/middleware"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/provider"
	"github.com/ilter-ai/ilter/internal/proxy"
	testutil "github.com/ilter-ai/ilter/tests/utils"
)

// newDeadRedis creates a miniredis, a go-redis client wired to it, and a
// guard. The caller must call mini.Close() to kill Redis before the request
// so every middleware operation fails open.
func newDeadRedis(t *testing.T) (*redis.Client, *circuitbreaker.RedisBreaker, *miniredis.Miniredis) {
	t.Helper()

	mini, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:         mini.Addr(),
		DialTimeout:  100 * time.Millisecond,
		ReadTimeout:  100 * time.Millisecond,
		WriteTimeout: 100 * time.Millisecond,
		MaxRetries:   0,
	})

	guard := circuitbreaker.NewRedisBreaker(rdb, 200*time.Millisecond, gobreaker.Settings{})
	return rdb, guard, mini
}

// newTestSetup creates the common plumbing: provider registry, load balancer,
// auth middleware, and proxy handler.  Tests add the middleware they care
// about on top before serving.
func newTestSetup(t *testing.T) (*config.Config, *smartrouter.LoadBalancer, *middleware.AuthMiddleware, *proxy.Handler) {
	t.Helper()

	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{
			{Name: "mock", Type: "mock", Models: []config.ModelConfig{{Name: "gpt-mock", Weight: 1}}},
		},
	}

	reg := provider.NewRegistry()
	reg.Register(testutil.NewMockProvider())

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	if err != nil {
		t.Fatalf("load balancer: %v", err)
	}

	authMw := middleware.NewAuthMiddleware(cfg.Auth, nil)
	proxyHandler := proxy.NewHandler(lb, nil, nil, nil)
	return cfg, lb, authMw, proxyHandler
}

// newRequest builds a POST /v1/chat/completions with the given model name
// and authenticates it with the admin key.
func newRequest(modelName string) *http.Request {
	reqBody := model.ChatCompletionRequest{
		Model:    modelName,
		Messages: []model.Message{{Role: "user", Content: "Hello"}},
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+testutil.AdminKey)
	return req
}

// ─────────────────────────────────────────────────────────────────────────
// Test 1: Rate limiter fails open when Redis dies mid-request.
// ─────────────────────────────────────────────────────────────────────────

func TestChaos_RateLimiterRedisDown(t *testing.T) {
	t.Parallel()

	_, guard, mini := newDeadRedis(t)
	_, _, authMw, proxyHandler := newTestSetup(t)

	rlMw, err := middleware.NewRateLimitMiddleware(
		&config.RateLimitConfig{Enabled: true, AdminBypass: false, DefaultRPM: 10},
		guard, nil,
	)
	if err != nil {
		t.Fatalf("rate limiter: %v", err)
	}

	r := chi.NewRouter()
	r.Use(authMw.Handler)
	r.Use(rlMw.Handler)
	r.Post("/v1/chat/completions", proxyHandler.ChatCompletions)

	// Kill Redis — rate limiter must fail open.
	mini.Close()

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, newRequest("gpt-mock"))

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 (fail-open), got %d: %s", rr.Code, rr.Body.String())
	}
}

// ─────────────────────────────────────────────────────────────────────────
// Test 2: Budget enforcer fails open when Redis dies mid-request.
// ─────────────────────────────────────────────────────────────────────────

func TestChaos_BudgetRedisDown(t *testing.T) {
	t.Parallel()

	_, guard, mini := newDeadRedis(t)
	_, _, authMw, proxyHandler := newTestSetup(t)

	budgetMw := middleware.NewBudgetMiddleware(
		config.BudgetConfig{Enabled: true, DefaultMonthlyLimit: 100.0, DefaultDailyLimit: 10.0},
		guard, nil, nil,
	)

	r := chi.NewRouter()
	r.Use(authMw.Handler)
	r.Use(budgetMw.Handler)
	r.Post("/v1/chat/completions", proxyHandler.ChatCompletions)

	// Kill Redis — budget enforcer must fail open.
	mini.Close()

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, newRequest("gpt-mock"))

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 (fail-open), got %d: %s", rr.Code, rr.Body.String())
	}
}

// ─────────────────────────────────────────────────────────────────────────
// Test 3: Semantic cache fails open when Redis is down.
// ─────────────────────────────────────────────────────────────────────────

func TestChaos_SemanticCacheRedisDown(t *testing.T) {
	t.Parallel()

	_, guard, mini := newDeadRedis(t)
	_, _, authMw, proxyHandler := newTestSetup(t)

	cacheMw := middleware.NewSemanticCacheMiddleware(
		config.CacheConfig{Enabled: true, Type: "exact"},
		guard, nil,
	)

	r := chi.NewRouter()
	r.Use(authMw.Handler)
	r.Use(cacheMw.Handler)
	r.Post("/v1/chat/completions", proxyHandler.ChatCompletions)

	// Kill Redis — semantic cache must fail open (cache miss → proxy).
	mini.Close()

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, newRequest("gpt-mock"))

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 (fail-open), got %d: %s", rr.Code, rr.Body.String())
	}
}
