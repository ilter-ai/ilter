package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/model/catalog"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
	"github.com/ilter-ai/ilter/internal/provider"
)

// setupSmartRouterTest creates a standard test environment with:
//   - catalog.Models populated with gpt-4o-mini (economy), gpt-4o (standard), gpt-4.1 (premium)
//   - A load balancer configured with an openai stub provider serving those 3 models
//   - A SmartRouter with heuristic scorer at default thresholds (econ=15, std=50)
//   - A ConfigCache with routing enabled
//
// Models are cleaned up automatically via t.Cleanup.
func setupSmartRouterTest(t *testing.T) (*config.Cache, *smartrouter.SmartRouter) {
	t.Helper()

	catalog.ModelsMu.Lock()
	catalog.Models["gpt-4o-mini"] = []catalog.ModelInfo{{ID: "gpt-4o-mini", Provider: "openai", Tier: "economy"}}
	catalog.Models["gpt-4o"] = []catalog.ModelInfo{{ID: "gpt-4o", Provider: "openai", Tier: "standard"}}
	catalog.Models["gpt-4.1"] = []catalog.ModelInfo{{ID: "gpt-4.1", Provider: "openai", Tier: "premium"}}
	catalog.ModelsMu.Unlock()

	t.Cleanup(func() {
		catalog.ModelsMu.Lock()
		delete(catalog.Models, "gpt-4o-mini")
		delete(catalog.Models, "gpt-4o")
		delete(catalog.Models, "gpt-4.1")
		catalog.ModelsMu.Unlock()
	})

	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{
				Name: "openai",
				Type: "openai",
				Models: []config.ModelConfig{
					{Name: "gpt-4o-mini"},
					{Name: "gpt-4o"},
					{Name: "gpt-4.1"},
				},
			},
		},
		Routing: config.RoutingConfig{
			Enabled: true,
			ComplexityThresholds: config.ComplexityThresholdsConfig{
				Economy:  15.0,
				Standard: 50.0,
			},
		},
	}

	reg := provider.NewRegistry()
	reg.Register(provider.NewMockProvider("openai"))

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	require.NoError(t, err)

	sr := smartrouter.NewSmartRouter(cfg, lb)

	boot := &config.BootConfig{
		Routing: config.RoutingConfig{
			Enabled: true,
			ComplexityThresholds: config.ComplexityThresholdsConfig{
				Economy:  15.0,
				Standard: 50.0,
			},
		},
	}
	cache := config.NewConfigCache(boot)

	return cache, sr
}

// ─────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────

func TestMiddlewareEnabledRouting(t *testing.T) {
	cache, sr := setupSmartRouterTest(t)
	mid := NewSmartRouterMiddleware(cache, sr)

	var capturedCtx context.Context
	handler := mid.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	body := `{"messages":[{"role":"user","content":"Hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, capturedCtx)

	// Simple prompt → economy tier → gpt-4o-mini
	strategy := capturedCtx.Value(StrategyKey)
	require.NotNil(t, strategy)
	assert.Equal(t, "gpt-4o-mini", strategy)

	// No explicit ProviderPreference → defaults to "cheapest"
	preference := capturedCtx.Value(PreferenceKey)
	require.NotNil(t, preference)
	assert.Equal(t, "cheapest", preference)
}

func TestMiddlewareDisabledRouting(t *testing.T) {
	boot := &config.BootConfig{
		Routing: config.RoutingConfig{Enabled: false},
	}
	cache := config.NewConfigCache(boot)
	// SmartRouter won't be called — routing disabled at the cache level
	mid := NewSmartRouterMiddleware(cache, &smartrouter.SmartRouter{})

	var capturedCtx context.Context
	handler := mid.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"messages":[{"role":"user","content":"Hi"}]}`))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, capturedCtx)

	// No routing context keys when feature is disabled
	assert.Nil(t, capturedCtx.Value(StrategyKey))
	assert.Nil(t, capturedCtx.Value(PreferenceKey))
}

func TestMiddlewareNilConfigCache(t *testing.T) {
	// &config.Cache{} has a zero-valued atomic.Pointer → Get() returns nil
	mid := NewSmartRouterMiddleware(&config.Cache{}, nil)

	var capturedCtx context.Context
	handler := mid.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"Hi"}]}`))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, capturedCtx)

	assert.Nil(t, capturedCtx.Value(StrategyKey))
	assert.Nil(t, capturedCtx.Value(PreferenceKey))
}

func TestMiddlewareExplicitModel(t *testing.T) {
	cache, sr := setupSmartRouterTest(t)
	mid := NewSmartRouterMiddleware(cache, sr)

	var capturedCtx context.Context
	handler := mid.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"Hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, capturedCtx)

	// Explicit model bypasses routing → StrategyKey set to requested model
	strategy := capturedCtx.Value(StrategyKey)
	require.NotNil(t, strategy)
	assert.Equal(t, "gpt-4o", strategy)

	// PreferenceKey is NOT set on the explicit-model path
	assert.Nil(t, capturedCtx.Value(PreferenceKey))
}

func TestMiddlewareRouteRequestError(t *testing.T) {
	// Register models for tier lookups but use an empty load balancer
	// so RouteRequest returns "no models configured".
	catalog.ModelsMu.Lock()
	catalog.Models["gpt-4o-mini"] = []catalog.ModelInfo{{ID: "gpt-4o-mini", Provider: "openai", Tier: "economy"}}
	catalog.ModelsMu.Unlock()
	t.Cleanup(func() {
		catalog.ModelsMu.Lock()
		delete(catalog.Models, "gpt-4o-mini")
		catalog.ModelsMu.Unlock()
	})

	// Empty config = no providers → empty load balancer
	cfg := &config.Config{Routing: config.RoutingConfig{Enabled: true}}
	reg := provider.NewRegistry()
	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	require.NoError(t, err)

	sr := smartrouter.NewSmartRouter(cfg, lb)

	boot := &config.BootConfig{Routing: config.RoutingConfig{Enabled: true}}
	cache := config.NewConfigCache(boot)
	mid := NewSmartRouterMiddleware(cache, sr)

	var capturedCtx context.Context
	handler := mid.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"messages":[{"role":"user","content":"Hi"}]}`))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, capturedCtx)

	// RouteRequest error → StrategyKey is empty string, no PreferenceKey
	strategy := capturedCtx.Value(StrategyKey)
	require.NotNil(t, strategy)
	assert.Equal(t, "", strategy)
	assert.Nil(t, capturedCtx.Value(PreferenceKey))
}

func TestMiddlewareBadJSONBody(t *testing.T) {
	boot := &config.BootConfig{Routing: config.RoutingConfig{Enabled: true}}
	cache := config.NewConfigCache(boot)
	mid := NewSmartRouterMiddleware(cache, nil)

	var capturedCtx context.Context
	handler := mid.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`not json`))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, capturedCtx)

	// Invalid JSON → passthrough, no routing context keys set
	assert.Nil(t, capturedCtx.Value(StrategyKey))
	assert.Nil(t, capturedCtx.Value(PreferenceKey))
}

func TestMiddlewareUpdateSmartRouter(t *testing.T) {
	// Shared model registry entries for the second SmartRouter.
	catalog.ModelsMu.Lock()
	catalog.Models["gpt-4o-mini"] = []catalog.ModelInfo{{ID: "gpt-4o-mini", Provider: "openai", Tier: "economy"}}
	catalog.Models["gpt-4o"] = []catalog.ModelInfo{{ID: "gpt-4o", Provider: "openai", Tier: "standard"}}
	catalog.Models["gpt-4.1"] = []catalog.ModelInfo{{ID: "gpt-4.1", Provider: "openai", Tier: "premium"}}
	catalog.ModelsMu.Unlock()
	t.Cleanup(func() {
		catalog.ModelsMu.Lock()
		delete(catalog.Models, "gpt-4o-mini")
		delete(catalog.Models, "gpt-4o")
		delete(catalog.Models, "gpt-4.1")
		catalog.ModelsMu.Unlock()
	})

	// Shared cache — routing enabled
	boot := &config.BootConfig{
		Routing: config.RoutingConfig{
			Enabled: true,
			ComplexityThresholds: config.ComplexityThresholdsConfig{
				Economy:  15.0,
				Standard: 50.0,
			},
		},
	}
	cache := config.NewConfigCache(boot)

	// ── SmartRouter 1: empty load balancer → RouteRequest fails ──
	emptyCfg := &config.Config{Routing: config.RoutingConfig{Enabled: true}}
	emptyReg := provider.NewRegistry()
	emptyLB, err := smartrouter.NewLoadBalancer(emptyCfg, emptyReg, nil)
	require.NoError(t, err)
	sr1 := smartrouter.NewSmartRouter(emptyCfg, emptyLB)

	// ── SmartRouter 2: has providers → RouteRequest succeeds ──
	fullCfg := &config.Config{
		Providers: []config.ProviderConfig{
			{
				Name: "openai",
				Type: "openai",
				Models: []config.ModelConfig{
					{Name: "gpt-4o-mini"},
					{Name: "gpt-4o"},
					{Name: "gpt-4.1"},
				},
			},
		},
		Routing: config.RoutingConfig{Enabled: true},
	}
	fullReg := provider.NewRegistry()
	fullReg.Register(provider.NewMockProvider("openai"))
	fullLB, err := smartrouter.NewLoadBalancer(fullCfg, fullReg, nil)
	require.NoError(t, err)
	sr2 := smartrouter.NewSmartRouter(fullCfg, fullLB)

	// Create middleware with sr1
	mid := NewSmartRouterMiddleware(cache, sr1)

	// First request — sr1 fails → empty strategy
	var capturedCtx1 context.Context
	handler1 := mid.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx1 = r.Context()
		w.WriteHeader(http.StatusOK)
	}))
	req1 := httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"messages":[{"role":"user","content":"Hi"}]}`))
	handler1.ServeHTTP(httptest.NewRecorder(), req1)
	require.NotNil(t, capturedCtx1)
	assert.Equal(t, "", capturedCtx1.Value(StrategyKey), "sr1 should fail → empty strategy")

	// Atomic swap
	mid.UpdateSmartRouter(sr2)

	// Second request — sr2 succeeds → gpt-4o-mini
	var capturedCtx2 context.Context
	handler2 := mid.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx2 = r.Context()
		w.WriteHeader(http.StatusOK)
	}))
	req2 := httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"messages":[{"role":"user","content":"Hi"}]}`))
	handler2.ServeHTTP(httptest.NewRecorder(), req2)
	require.NotNil(t, capturedCtx2)
	assert.Equal(t, "gpt-4o-mini", capturedCtx2.Value(StrategyKey), "sr2 should succeed → gpt-4o-mini")
}

func TestMiddlewareProviderPreferenceExplicit(t *testing.T) {
	// Create cache with an explicit ProviderPreference
	boot := &config.BootConfig{
		Routing: config.RoutingConfig{
			Enabled:            true,
			ProviderPreference: "round-robin",
			ComplexityThresholds: config.ComplexityThresholdsConfig{
				Economy:  15.0,
				Standard: 50.0,
			},
		},
	}
	cache := config.NewConfigCache(boot)

	// Reuse the standard setup (models, lb, sr) but replace the cache
	_, sr := setupSmartRouterTest(t)
	mid := NewSmartRouterMiddleware(cache, sr)

	var capturedCtx context.Context
	handler := mid.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	body := `{"messages":[{"role":"user","content":"Hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, capturedCtx)

	strategy := capturedCtx.Value(StrategyKey)
	require.NotNil(t, strategy)
	assert.Equal(t, "gpt-4o-mini", strategy)

	// PreferenceKey should match the explicit ProviderPreference
	preference := capturedCtx.Value(PreferenceKey)
	require.NotNil(t, preference)
	assert.Equal(t, "round-robin", preference)
}

func TestMiddlewareRequestMetadata(t *testing.T) {
	cache, sr := setupSmartRouterTest(t)
	mid := NewSmartRouterMiddleware(cache, sr)

	// Seed request metadata into context before the handler processes the request
	body := `{"messages":[{"role":"user","content":"Hi"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx, meta := reqmeta.InitRequestMetadata(req.Context())
	req = req.WithContext(ctx)

	var capturedCtx context.Context
	handler := mid.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCtx = r.Context()
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.NotNil(t, capturedCtx)

	// Middleware should have updated the metadata with routing info
	require.NotNil(t, meta)
	require.NotNil(t, meta.SmartRouter)
	assert.True(t, *meta.SmartRouter)
	assert.Equal(t, "gpt-4o-mini", meta.SmartRoutedTo)
	assert.Greater(t, meta.ComplexityScore, 0.0,
		"complexity score should be set to a positive value")
}
