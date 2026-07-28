package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/middleware"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
	"github.com/ilter-ai/ilter/internal/provider"
	"github.com/ilter-ai/ilter/internal/proxy"
	testutil "github.com/ilter-ai/ilter/tests/utils"
)

// mockProvider2 is a second mock provider for testing multi-provider preference scenarios.
// It implements the same provider.Provider interface as mockProvider (defined in
// chat_completions_test.go) but with Name() returning "mock2" so both can coexist
// in the same registry.
type mockProvider2 struct{}

func (m *mockProvider2) Name() string { return "mock2" }

func (m *mockProvider2) Type() string                        { return "mock" }
func (m *mockProvider2) HealthCheck(_ context.Context) error { return nil }
func (m *mockProvider2) DiscoverModels(_ context.Context) ([]catalog.ModelInfo, error) {
	return nil, nil
}

func (m *mockProvider2) TransformRequest(ctx context.Context, _ *model.ChatCompletionRequest) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, "POST", "http://mock2", nil)
}

func (m *mockProvider2) TransformResponse(_ context.Context, _ *http.Response) (*model.ChatCompletionResponse, error) {
	return &model.ChatCompletionResponse{
		ID:      "test-id-2",
		Model:   "mock2-model",
		Choices: []model.Choice{{Message: model.ChoiceMessage{Content: "mock2 response"}}},
	}, nil
}

func (m *mockProvider2) Client() *http.Client {
	return &http.Client{
		Transport: testutil.RoundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       http.NoBody,
			}, nil
		}),
	}
}

// setupSmartRouterE2EMultiProvider is like NewSmartRouterFixture but registers two
// providers (mock and mock2) so the load balancer has multiple routes per model.
func setupSmartRouterE2EMultiProvider(t *testing.T, cfg *config.Config) *chi.Mux {
	t.Helper()

	catalog.ModelsMu.Lock()
	catalog.Models["gpt-4o-mini"] = []catalog.ModelInfo{{ID: "gpt-4o-mini", Provider: "openai", Tier: "economy"}}
	catalog.Models["gpt-4o"] = []catalog.ModelInfo{{ID: "gpt-4o", Provider: "openai", Tier: "standard"}}
	catalog.Models["gpt-4.1"] = []catalog.ModelInfo{{ID: "gpt-4.1", Provider: "openai", Tier: "premium"}}
	catalog.Models["deepseek-chat"] = []catalog.ModelInfo{{ID: "deepseek-chat", Provider: "deepseek", Tier: "economy"}}
	catalog.ModelsMu.Unlock()

	t.Cleanup(func() {
		catalog.ModelsMu.Lock()
		delete(catalog.Models, "gpt-4o-mini")
		delete(catalog.Models, "gpt-4o")
		delete(catalog.Models, "gpt-4.1")
		delete(catalog.Models, "deepseek-chat")
		catalog.ModelsMu.Unlock()
	})

	reg := provider.NewRegistry()
	reg.Register(testutil.NewMockProvider())
	reg.Register(&mockProvider2{})

	boot := config.DefaultBootConfig()
	boot.Auth = cfg.Auth
	boot.Routing = cfg.Routing
	cfgCache := config.NewConfigCache(&boot)

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, cfgCache)
	require.NoError(t, err, "Load balancer err")

	proxyHandler := proxy.NewHandler(lb, nil, nil, nil)
	proxyHandler.SetConfig(cfg)
	proxyHandler.SetConfigCache(cfgCache)

	sr := smartrouter.NewSmartRouter(cfg, lb)
	srMw := middleware.NewSmartRouterMiddleware(cfgCache, sr)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	})
	r.Use(middleware.NewAuthMiddleware(cfg.Auth, nil).Handler)
	r.Use(srMw.Handler)
	r.Post("/v1/chat/completions", proxyHandler.ChatCompletions)
	return r
}

// ---------------------------------------------------------------------------
// 7 provider preference tests
// ---------------------------------------------------------------------------

// TestProviderPreference_CheapestDefault verifies that when ProviderPreference is
// empty (or "cheapest"), the first available provider route is selected. With a
// single provider serving the economy tier, a simple prompt always selects
// the economy model (gpt-4o-mini).
func TestProviderPreference_CheapestDefault(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{
			{
				Name: "mock",
				Type: "mock",
				Models: []config.ModelConfig{
					{Name: "gpt-4o-mini", Weight: 1},
					{Name: "gpt-4o", Weight: 1},
				},
			},
		},
		Routing: config.RoutingConfig{
			Enabled: true,
			// ProviderPreference empty → defaults to "cheapest" (first route)
			ComplexityThresholds: config.ComplexityThresholdsConfig{
				Economy:  15.0,
				Standard: 50.0,
			},
		},
	}

	fixt := testutil.NewSmartRouterFixture(t, cfg, testutil.DefaultModels)
	rr := testutil.Serve(t, fixt, testutil.NewTestRequest(t, model.ChatCompletionRequest{
		Model:    "",
		Messages: []model.Message{{Role: "user", Content: "Hi"}},
	}))

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "gpt-4o-mini", rr.Header().Get("X-Ilter-Model-Selected"))
	assert.NotEmpty(t, rr.Header().Get("X-Ilter-Complexity-Score"))
}

// TestProviderPreference_RoundRobin verifies that with ProviderPreference="round-robin"
// the load balancer rotates through available providers. Two providers serve the same
// model (gpt-4o-mini). The test makes three consecutive requests and verifies each
// succeeds with the expected economy model.
func TestProviderPreference_RoundRobin(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{
			{
				Name: "mock",
				Type: "mock",
				Models: []config.ModelConfig{
					{Name: "gpt-4o-mini", Weight: 1},
					{Name: "gpt-4o", Weight: 1},
				},
			},
			{
				Name: "mock2",
				Type: "mock",
				Models: []config.ModelConfig{
					{Name: "gpt-4o-mini", Weight: 1},
					{Name: "gpt-4o", Weight: 1},
				},
			},
		},
		Routing: config.RoutingConfig{
			Enabled:            true,
			ProviderPreference: "round-robin",
			ComplexityThresholds: config.ComplexityThresholdsConfig{
				Economy:  15.0,
				Standard: 50.0,
			},
		},
	}

	r := setupSmartRouterE2EMultiProvider(t, cfg)

	for i := range 3 {
		rr := testutil.Serve(t, &testutil.TestFixture{Router: r}, testutil.NewTestRequest(t, model.ChatCompletionRequest{
			Model:    "",
			Messages: []model.Message{{Role: "user", Content: "Hi"}},
		}))

		require.Equal(t, http.StatusOK, rr.Code, "request %d", i+1)
		assert.Equal(t, "gpt-4o-mini", rr.Header().Get("X-Ilter-Model-Selected"), "request %d", i+1)
		assert.NotEmpty(t, rr.Header().Get("X-Ilter-Complexity-Score"), "request %d", i+1)
	}
}

// TestProviderPreference_ExplicitModelBypass verifies that when an explicit model is
// provided in the request body, ProviderPreference has no effect — the model is used
// as-is and the smart router bypasses routing.
func TestProviderPreference_ExplicitModelBypass(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{
			{
				Name: "mock",
				Type: "mock",
				Models: []config.ModelConfig{
					{Name: "gpt-4o-mini", Weight: 1},
					{Name: "gpt-4o", Weight: 1},
				},
			},
		},
		Routing: config.RoutingConfig{
			Enabled:            true,
			ProviderPreference: "round-robin",
			ComplexityThresholds: config.ComplexityThresholdsConfig{
				Economy:  15.0,
				Standard: 50.0,
			},
		},
	}

	fixt := testutil.NewSmartRouterFixture(t, cfg, testutil.DefaultModels)
	rr := testutil.Serve(t, fixt, testutil.NewTestRequest(t, model.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []model.Message{{Role: "user", Content: "Hi"}},
	}))

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "gpt-4o", rr.Header().Get("X-Ilter-Model-Selected"))
}

// TestProviderPreference_RoutingDisabled verifies that when Routing.Enabled is false,
// ProviderPreference has no effect — the smart router middleware passes through and
// the explicit model from the request body is used directly.
func TestProviderPreference_RoutingDisabled(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{
			{
				Name: "mock",
				Type: "mock",
				Models: []config.ModelConfig{
					{Name: "gpt-4o-mini", Weight: 1},
					{Name: "gpt-4o", Weight: 1},
				},
			},
		},
		Routing: config.RoutingConfig{
			Enabled:            false,
			ProviderPreference: "round-robin",
			ComplexityThresholds: config.ComplexityThresholdsConfig{
				Economy:  15.0,
				Standard: 50.0,
			},
		},
	}

	fixt := testutil.NewSmartRouterFixture(t, cfg, testutil.DefaultModels)
	rr := testutil.Serve(t, fixt, testutil.NewTestRequest(t, model.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []model.Message{{Role: "user", Content: "Hi"}},
	}))

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "gpt-4o", rr.Header().Get("X-Ilter-Model-Selected"))
}

// TestProviderPreference_ProviderFallback_EconomyOnly verifies that when only
// economy-tier models are configured, complex prompts fall back to the available
// economy model. ProviderPreference does not change this fallback behavior —
// the smart router degrades through tiers regardless of preference.
func TestProviderPreference_ProviderFallback_EconomyOnly(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{
			{
				Name: "mock",
				Type: "mock",
				Models: []config.ModelConfig{
					{Name: "gpt-4o-mini", Weight: 1},
				},
			},
		},
		Routing: config.RoutingConfig{
			Enabled:            true,
			ProviderPreference: "round-robin",
			ComplexityThresholds: config.ComplexityThresholdsConfig{
				Economy:  15.0,
				Standard: 50.0,
			},
		},
	}

	fixt := testutil.NewSmartRouterFixture(t, cfg, testutil.DefaultModels)
	rr := testutil.Serve(t, fixt, testutil.NewTestRequest(t, model.ChatCompletionRequest{
		Model:    "",
		Messages: []model.Message{{Role: "user", Content: "Analyze the performance of this database schema and provide step-by-step reasoning. You must strictly ensure that there are no table scans. Output must be in json. ```sql SELECT * FROM users ```"}},
	}))

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "gpt-4o-mini", rr.Header().Get("X-Ilter-Model-Selected"))
	assert.NotEmpty(t, rr.Header().Get("X-Ilter-Complexity-Score"))
}
