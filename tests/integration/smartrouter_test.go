// allow: SIZE_OK — 1 shared setup helper kept for package-level cross-file
// compatibility (chaos_redis_down_test.go imports it).
package integration

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db/dbtest"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/middleware"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
	"github.com/ilter-ai/ilter/internal/provider"
	"github.com/ilter-ai/ilter/internal/proxy"
	testutil "github.com/ilter-ai/ilter/tests/utils"
)

// setupSmartRouterE2EDisabledRouting creates a router with routing disabled.
// Kept for compatibility with chaos_redis_down_test.go.
func setupSmartRouterE2EDisabledRouting(t *testing.T, cfg *config.Config) *chi.Mux {
	t.Helper()
	boot := config.DefaultBootConfig()
	boot.Auth = cfg.Auth
	boot.Routing = cfg.Routing
	boot.Routing.Enabled = false
	cc := config.NewConfigCache(&boot)

	reg := provider.NewRegistry()
	mp := testutil.NewMockProvider()
	reg.Register(mp)

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, cc)
	require.NoError(t, err)

	ph := proxy.NewHandler(lb, nil, nil, nil)
	ph.SetConfig(cfg)
	ph.SetConfigCache(cc)

	sr := smartrouter.NewSmartRouter(cfg, lb)
	mw := middleware.NewSmartRouterMiddleware(cc, sr)

	r := chi.NewRouter()
	r.Use(middleware.NewAuthMiddleware(cfg.Auth, nil).Handler)
	r.Use(mw.Handler)
	r.Post("/v1/chat/completions", ph.ChatCompletions)
	return r
}

// ---------------------------------------------------------------------------
// 12 full-chain smart router tests (10 original + 2 unique from full_test.go)
// ---------------------------------------------------------------------------

func TestSmartRouterE2E_EconomyTier(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{{
			Name: "mock", Type: "mock",
			Models: []config.ModelConfig{
				{Name: "gpt-4o-mini", Weight: 1},
				{Name: "gpt-4o", Weight: 1},
				{Name: "gpt-4.1", Weight: 1},
			},
		}},
		Routing: config.RoutingConfig{
			Enabled:              true,
			ComplexityThresholds: config.ComplexityThresholdsConfig{Economy: 15, Standard: 50},
		},
	}
	fixt := testutil.NewSmartRouterFixture(t, cfg, testutil.DefaultModels)
	rr := testutil.Serve(t, fixt, testutil.NewTestRequest(t, model.ChatCompletionRequest{
		Messages: []model.Message{{Role: "user", Content: "Hi"}},
	}))
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "gpt-4o-mini", rr.Header().Get("X-Ilter-Model-Selected"))
	assert.NotEmpty(t, rr.Header().Get("X-Ilter-Complexity-Score"))
}

func TestSmartRouterE2E_StandardTier(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{{
			Name: "mock", Type: "mock",
			Models: []config.ModelConfig{
				{Name: "gpt-4o-mini", Weight: 1},
				{Name: "gpt-4o", Weight: 1},
				{Name: "gpt-4.1", Weight: 1},
			},
		}},
		Routing: config.RoutingConfig{
			Enabled:              true,
			ComplexityThresholds: config.ComplexityThresholdsConfig{Economy: 15, Standard: 50},
		},
	}
	fixt := testutil.NewSmartRouterFixture(t, cfg, testutil.DefaultModels)
	rr := testutil.Serve(t, fixt, testutil.NewTestRequest(t, model.ChatCompletionRequest{
		Messages: []model.Message{{Role: "user", Content: "Think step-by-step and write a python script. You must strictly avoid using external packages."}},
	}))
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "gpt-4o", rr.Header().Get("X-Ilter-Model-Selected"))
	assert.NotEmpty(t, rr.Header().Get("X-Ilter-Complexity-Score"))
}

func TestSmartRouterE2E_PremiumTier(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{{
			Name: "mock", Type: "mock",
			Models: []config.ModelConfig{
				{Name: "gpt-4o-mini", Weight: 1},
				{Name: "gpt-4o", Weight: 1},
				{Name: "gpt-4.1", Weight: 1},
			},
		}},
		Routing: config.RoutingConfig{
			Enabled:              true,
			ComplexityThresholds: config.ComplexityThresholdsConfig{Economy: 15, Standard: 50},
		},
	}
	fixt := testutil.NewSmartRouterFixture(t, cfg, testutil.DefaultModels)
	rr := testutil.Serve(t, fixt, testutil.NewTestRequest(t, model.ChatCompletionRequest{
		Messages: []model.Message{{Role: "user", Content: "Analyze the performance of this database schema and provide step-by-step reasoning. You must strictly ensure that there are no table scans. Output must be in json. ```sql SELECT * FROM users ```"}},
	}))
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "gpt-4.1", rr.Header().Get("X-Ilter-Model-Selected"))
	assert.NotEmpty(t, rr.Header().Get("X-Ilter-Complexity-Score"))
}

func TestSmartRouterE2E_ToolCallingForcesPremium(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{{
			Name: "mock", Type: "mock",
			Models: []config.ModelConfig{
				{Name: "gpt-4o-mini", Weight: 1},
				{Name: "gpt-4o", Weight: 1},
				{Name: "gpt-4.1", Weight: 1},
			},
		}},
		Routing: config.RoutingConfig{
			Enabled:              true,
			ComplexityThresholds: config.ComplexityThresholdsConfig{Economy: 15, Standard: 50},
		},
	}
	fixt := testutil.NewSmartRouterFixture(t, cfg, testutil.DefaultModels)
	rr := testutil.Serve(t, fixt, testutil.NewTestRequest(t, model.ChatCompletionRequest{
		Messages: []model.Message{{Role: "user", Content: "What's the weather?"}},
		Tools:    []model.Tool{{Type: "function", Function: model.ToolFunction{Name: "get_weather", Description: "Get current weather"}}},
	}))
	require.Equal(t, http.StatusOK, rr.Code)
	// With tools + short prompt, complexity should land in standard tier → gpt-4o
	assert.Equal(t, "gpt-4o", rr.Header().Get("X-Ilter-Model-Selected"))
}

func TestSmartRouterE2E_FallbackToDefaultModel(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{{
			Name: "mock", Type: "mock",
			Models: []config.ModelConfig{
				{Name: "gpt-4o-mini", Weight: 1},
			},
		}},
		Routing: config.RoutingConfig{
			Enabled:              true,
			ComplexityThresholds: config.ComplexityThresholdsConfig{Economy: 15, Standard: 50},
		},
	}
	fixt := testutil.NewSmartRouterFixture(t, cfg, map[string]string{
		"gpt-4o-mini": "economy",
	})
	rr := testutil.Serve(t, fixt, testutil.NewTestRequest(t, model.ChatCompletionRequest{
		Messages: []model.Message{{Role: "user", Content: "Analyze the performance of this database schema and provide step-by-step reasoning. You must strictly ensure that there are no table scans. Output must be in json. ```sql SELECT * FROM users ```"}},
	}))
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "gpt-4o-mini", rr.Header().Get("X-Ilter-Model-Selected"))
}

func TestSmartRouterE2E_EmptyModelTriggersSmartRouting(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{{
			Name: "mock", Type: "mock",
			Models: []config.ModelConfig{
				{Name: "gpt-4o-mini", Weight: 1},
				{Name: "gpt-4o", Weight: 1},
				{Name: "gpt-4.1", Weight: 1},
			},
		}},
		Routing: config.RoutingConfig{
			Enabled:              true,
			ComplexityThresholds: config.ComplexityThresholdsConfig{Economy: 15, Standard: 50},
		},
	}
	fixt := testutil.NewSmartRouterFixture(t, cfg, testutil.DefaultModels)
	rr := testutil.Serve(t, fixt, testutil.NewTestRequest(t, model.ChatCompletionRequest{
		Messages: []model.Message{{Role: "user", Content: "Hi"}},
	}))
	require.Equal(t, http.StatusOK, rr.Code)
	assert.NotEmpty(t, rr.Header().Get("X-Ilter-Model-Selected"))
}

func TestSmartRouterE2E_ExplicitModelBypassesRouting(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{{
			Name: "mock", Type: "mock",
			Models: []config.ModelConfig{
				{Name: "gpt-4o-mini", Weight: 1},
				{Name: "gpt-4o", Weight: 1},
				{Name: "gpt-4.1", Weight: 1},
			},
		}},
		Routing: config.RoutingConfig{
			Enabled:              true,
			ComplexityThresholds: config.ComplexityThresholdsConfig{Economy: 15, Standard: 50},
		},
	}
	fixt := testutil.NewSmartRouterFixture(t, cfg, testutil.DefaultModels)
	rr := testutil.Serve(t, fixt, testutil.NewTestRequest(t, model.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []model.Message{{Role: "user", Content: "Hi"}},
	}))
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "gpt-4o", rr.Header().Get("X-Ilter-Model-Selected"))
	assert.NotEmpty(t, rr.Header().Get("X-Ilter-Complexity-Score"))
}

func TestSmartRouterE2E_ComplexityScoreHeader(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{{
			Name: "mock", Type: "mock",
			Models: []config.ModelConfig{
				{Name: "gpt-4o-mini", Weight: 1},
			},
		}},
		Routing: config.RoutingConfig{Enabled: true},
	}
	fixt := testutil.NewSmartRouterFixture(t, cfg, map[string]string{"gpt-4o-mini": "economy"})

	tests := []struct {
		name     string
		messages []model.Message
	}{
		{"simple prompt", []model.Message{{Role: "user", Content: "Hello"}}},
		{"reasoning prompt", []model.Message{{Role: "user", Content: "Think step-by-step about this problem and analyze it carefully. You must consider all constraints."}}},
		{"code and format prompt", []model.Message{{Role: "user", Content: "Write a python script that outputs json. Use step-by-step reasoning. ```python\nprint('hello')\n```"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := testutil.Serve(t, fixt, testutil.NewTestRequest(t, model.ChatCompletionRequest{
				Messages: tt.messages,
			}))
			require.Equal(t, http.StatusOK, rr.Code)
			scoreHeader := rr.Header().Get("X-Ilter-Complexity-Score")
			require.NotEmpty(t, scoreHeader)
			var score float64
			require.NoError(t, json.Unmarshal([]byte(scoreHeader), &score))
			assert.GreaterOrEqual(t, score, 0.0)
		})
	}
}

func TestSmartRouterE2E_CostEstimateHeaders(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{{
			Name: "mock", Type: "mock",
			Models: []config.ModelConfig{
				{Name: "gpt-4o-mini", Weight: 1, CostPerInputToken: 0.00000015, CostPerOutputToken: 0.00000060},
			},
		}},
		Routing: config.RoutingConfig{
			Enabled:              true,
			ComplexityThresholds: config.ComplexityThresholdsConfig{Economy: 15, Standard: 50},
		},
	}
	fixt := testutil.NewSmartRouterFixture(t, cfg, map[string]string{"gpt-4o-mini": "economy"})
	rr := testutil.Serve(t, fixt, testutil.NewTestRequest(t, model.ChatCompletionRequest{
		Messages: []model.Message{{Role: "user", Content: "Hello"}},
	}))
	require.Equal(t, http.StatusOK, rr.Code)
	assert.NotEmpty(t, rr.Header().Get("X-Ilter-Model-Selected"))
	assert.NotEmpty(t, rr.Header().Get("X-Ilter-Complexity-Score"))
	assert.NotEmpty(t, rr.Header().Get("X-Ilter-Cost-Estimate"))
}

func TestSmartRouterE2E_AuditLogRecordsRouting(t *testing.T) {
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

	store := dbtest.New(t)

	_, _, err := store.CreateAPIKey("smart-router-test-key", nil, nil, 1000.0, 0, 100, 0, nil, nil, nil)
	require.NoError(t, err)

	auditLogger := middleware.NewAuditLoggerMiddleware(store)

	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{{
			Name: "mock", Type: "mock",
			Models: []config.ModelConfig{
				{Name: "gpt-4o-mini", Weight: 1},
				{Name: "gpt-4o", Weight: 1},
				{Name: "gpt-4.1", Weight: 1},
			},
		}},
		Routing: config.RoutingConfig{
			Enabled:              true,
			ComplexityThresholds: config.ComplexityThresholdsConfig{Economy: 15, Standard: 50},
		},
	}

	reg := provider.NewRegistry()
	reg.Register(testutil.NewMockProvider())

	boot := config.DefaultBootConfig()
	boot.Auth = cfg.Auth
	boot.Routing = cfg.Routing
	cfgCache := config.NewConfigCache(&boot)

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, cfgCache)
	require.NoError(t, err)

	ph := proxy.NewHandler(lb, auditLogger, nil, nil)
	ph.SetConfig(cfg)
	ph.SetStore(store)
	ph.SetConfigCache(cfgCache)

	sr := smartrouter.NewSmartRouter(cfg, lb)
	srMw := middleware.NewSmartRouterMiddleware(cfgCache, sr)

	r := chi.NewRouter()
	r.Use(middleware.RequestLogger)
	r.Use(middleware.NewAuthMiddleware(cfg.Auth, store).Handler)
	r.Use(srMw.Handler)
	r.Post("/v1/chat/completions", ph.ChatCompletions)

	rr := testutil.Serve(t, &testutil.TestFixture{Router: r}, testutil.NewTestRequest(t, model.ChatCompletionRequest{
		Messages: []model.Message{{Role: "user", Content: "Analyze the key factors that led to the decline of the Roman Empire, including economic troubles, military overreach, and political corruption. Compare each factor's relative importance using specific historical examples."}},
	}))
	require.Equal(t, http.StatusOK, rr.Code)
	assert.NotEmpty(t, rr.Header().Get("X-Ilter-Model-Selected"))

	auditLogger.Close()

	var auditCount int
	err = store.DB.QueryRow("SELECT COUNT(*) FROM audit_log WHERE complexity_score IS NOT NULL AND (key_id IS NOT NULL AND key_id != '')").Scan(&auditCount)
	require.NoError(t, err)
	assert.NotZero(t, auditCount, "expected audit_log entries with complexity_score")

	var complexityScore float64
	err = store.DB.QueryRow("SELECT complexity_score FROM audit_log LIMIT 1").Scan(&complexityScore)
	require.NoError(t, err)
	assert.Greater(t, complexityScore, 0.0, "expected positive complexity_score")
}

// ---------------------------------------------------------------------------
// Tests absorbed from e2e_smart_router_full_test.go (unique tests only)
// ---------------------------------------------------------------------------

func TestFullChain_RoutingDisabled(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{{
			Name: "mock", Type: "mock",
			Models: []config.ModelConfig{
				{Name: "gpt-4o-mini", Weight: 1},
				{Name: "gpt-4o", Weight: 1},
				{Name: "gpt-4.1", Weight: 1},
			},
		}},
		Routing: config.RoutingConfig{
			Enabled:              false,
			ComplexityThresholds: config.ComplexityThresholdsConfig{Economy: 15, Standard: 50},
		},
	}
	r := setupSmartRouterE2EDisabledRouting(t, cfg)
	rr := testutil.Serve(t, &testutil.TestFixture{Router: r}, testutil.NewTestRequest(t, model.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []model.Message{{Role: "user", Content: "Hi"}},
	}))
	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "gpt-4o", rr.Header().Get("X-Ilter-Model-Selected"))
	assert.NotEmpty(t, rr.Header().Get("X-Ilter-Complexity-Score"))
}

func TestFullChain_WithAuth(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{{
			Name: "mock", Type: "mock",
			Models: []config.ModelConfig{
				{Name: "gpt-4o-mini", Weight: 1},
				{Name: "gpt-4o", Weight: 1},
				{Name: "gpt-4.1", Weight: 1},
			},
		}},
		Routing: config.RoutingConfig{
			Enabled:              true,
			ComplexityThresholds: config.ComplexityThresholdsConfig{Economy: 15, Standard: 50},
		},
	}
	fixt := testutil.NewSmartRouterFixture(t, cfg, testutil.DefaultModels)
	rr := testutil.Serve(t, fixt, testutil.NewTestRequest(t, model.ChatCompletionRequest{
		Messages: []model.Message{{Role: "user", Content: "Hi"}},
	}))
	require.Equal(t, http.StatusOK, rr.Code)
	assert.NotEmpty(t, rr.Header().Get("X-Ilter-Model-Selected"))
	assert.NotEmpty(t, rr.Header().Get("X-Ilter-Complexity-Score"))
}
