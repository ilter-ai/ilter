// Package testutil provides shared test helpers and mocks for ILTER tests.
package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/middleware"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
	"github.com/ilter-ai/ilter/internal/provider"
	"github.com/ilter-ai/ilter/internal/proxy"
)

const AdminKey = "admin"

// DefaultModels is the standard model tier map used across tests.
var DefaultModels = map[string]string{
	"gpt-4o-mini":   "economy",
	"gpt-4o":        "standard",
	"gpt-4.1":       "premium",
	"deepseek-chat": "economy",
}

// RoundTripFunc adapts a function to http.RoundTripper.
type RoundTripFunc func(*http.Request) (*http.Response, error)

func (f RoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// MockProvider is a minimal provider.Provider stub for smart router tests.
// Use NewMockProvider for the default 200-returning instance, or set Transport
// to inject custom round-trip behavior.
type MockProvider struct {
	Transport http.RoundTripper
}

func NewMockProvider() *MockProvider {
	return &MockProvider{
		Transport: RoundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
		}),
	}
}

func (m *MockProvider) Name() string                        { return "mock" }
func (m *MockProvider) Type() string                        { return "mock" }
func (m *MockProvider) HealthCheck(_ context.Context) error { return nil }
func (m *MockProvider) DiscoverModels(_ context.Context) ([]catalog.ModelInfo, error) {
	return nil, nil
}

func (m *MockProvider) TransformRequest(_ context.Context, _ *model.ChatCompletionRequest) (*http.Request, error) {
	return http.NewRequestWithContext(context.Background(), "POST", "http://mock", nil)
}

func (m *MockProvider) TransformResponse(_ context.Context, _ *http.Response) (*model.ChatCompletionResponse, error) {
	return &model.ChatCompletionResponse{
		ID:    "test-id",
		Model: "mock-model",
		Choices: []model.Choice{
			{Message: model.ChoiceMessage{Content: "mock response"}},
		},
	}, nil
}

func (m *MockProvider) Client() *http.Client {
	return &http.Client{Transport: m.Transport}
}

// TestFixture holds all wired-up components created by NewSmartRouterFixture.
type TestFixture struct {
	Router      *chi.Mux
	SmartRouter *smartrouter.SmartRouter
	LB          *smartrouter.LoadBalancer
	Provider    *MockProvider
	ConfigCache *config.Cache
}

// SeedModelRegistry registers model IDs with their tiers into catalog.Models
// and registers a cleanup callback to remove them when the test completes.
func SeedModelRegistry(t *testing.T, models map[string]string) {
	t.Helper()
	catalog.ModelsMu.Lock()
	for id, tier := range models {
		catalog.Models[id] = []catalog.ModelInfo{{
			ID: id, Provider: "mock", Tier: tier,
		}}
	}
	catalog.ModelsMu.Unlock()
	t.Cleanup(func() {
		catalog.ModelsMu.Lock()
		for id := range models {
			delete(catalog.Models, id)
		}
		catalog.ModelsMu.Unlock()
	})
}

// NewSmartRouterFixture creates a fully wired test fixture: chi router with auth
// middleware, smart router middleware, and proxy handler ready for httptest requests.
// It populates catalog.Models with the given model map, creates a config cache,
// load balancer, smart router, and proxy handler.
//
// Use fallbackOpts for tests that need a different boot config (e.g. Routing disabled).
func NewSmartRouterFixture(t *testing.T, cfg *config.Config, models map[string]string) *TestFixture {
	t.Helper()
	SeedModelRegistry(t, models)

	reg := provider.NewRegistry()
	mp := NewMockProvider()
	reg.Register(mp)

	boot := config.DefaultBootConfig()
	boot.Auth = cfg.Auth
	boot.Routing = cfg.Routing
	cc := config.NewConfigCache(&boot)

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, cc)
	require.NoError(t, err, "load balancer")

	ph := proxy.NewHandler(lb, nil, nil, nil)
	ph.SetConfig(cfg)
	ph.SetConfigCache(cc)

	sr := smartrouter.NewSmartRouter(cfg, lb)
	mw := middleware.NewSmartRouterMiddleware(cc, sr)

	r := chi.NewRouter()
	r.Use(middleware.NewAuthMiddleware(cfg.Auth, nil).Handler)
	r.Use(mw.Handler)
	r.Post("/v1/chat/completions", ph.ChatCompletions)

	return &TestFixture{
		Router:      r,
		SmartRouter: sr,
		LB:          lb,
		Provider:    mp,
		ConfigCache: cc,
	}
}

// NewTestRequest creates a POST /v1/chat/completions request with the given
// body and authenticated with the test AdminKey. The body is encoded as JSON.
func NewTestRequest(t *testing.T, reqBody any) *http.Request {
	t.Helper()
	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err, "marshal request body")
	req, err := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	require.NoError(t, err, "create request")
	req.Header.Set("Authorization", "Bearer "+AdminKey)
	return req
}

// NewTestRequestUnauthenticated creates a POST /v1/chat/completions request
// without any Authorization header, for testing 401 paths.
func NewTestRequestUnauthenticated(t *testing.T, reqBody any) *http.Request {
	t.Helper()
	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err, "marshal request body")
	req, err := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	require.NoError(t, err, "create request")
	return req
}

// Serve conveniently wraps httptest.NewRecorder + fixt.Router.ServeHTTP.
func Serve(t *testing.T, fixt *TestFixture, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	fixt.Router.ServeHTTP(rr, req)
	return rr
}
