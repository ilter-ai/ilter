package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/fallback"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/middleware"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
	"github.com/ilter-ai/ilter/internal/platform/cooldown"
	"github.com/ilter-ai/ilter/internal/provider"
	"github.com/ilter-ai/ilter/internal/proxy"
	testutil "github.com/ilter-ai/ilter/tests/utils"
)

type failingProvider struct {
	name string
}

func (f *failingProvider) Name() string { return f.name }
func (f *failingProvider) Type() string { return "failing" }
func (f *failingProvider) TransformRequest(ctx context.Context, _ *model.ChatCompletionRequest) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, "POST", "http://failing", nil)
}

func (f *failingProvider) TransformResponse(_ context.Context, _ *http.Response) (*model.ChatCompletionResponse, error) {
	return nil, errors.New("should not be called")
}
func (f *failingProvider) HealthCheck(_ context.Context) error { return nil }
func (f *failingProvider) DiscoverModels(_ context.Context) ([]catalog.ModelInfo, error) {
	return nil, nil
}

func (f *failingProvider) Client() *http.Client {
	return &http.Client{
		Transport: testutil.RoundTripFunc(func(_ *http.Request) (*http.Response, error) {
			// Simulate 500 error
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       http.NoBody,
			}, nil
		}),
	}
}

type workingProvider struct {
	name string
}

func (w *workingProvider) Name() string { return w.name }
func (w *workingProvider) Type() string { return "working" }
func (w *workingProvider) TransformRequest(ctx context.Context, _ *model.ChatCompletionRequest) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, "POST", "http://working", nil)
}

func (w *workingProvider) TransformResponse(_ context.Context, _ *http.Response) (*model.ChatCompletionResponse, error) {
	return &model.ChatCompletionResponse{
		ID:      "fallback-id",
		Model:   "working-model",
		Choices: []model.Choice{{Message: model.ChoiceMessage{Content: "fallback response"}}},
	}, nil
}
func (w *workingProvider) HealthCheck(_ context.Context) error { return nil }
func (w *workingProvider) DiscoverModels(_ context.Context) ([]catalog.ModelInfo, error) {
	return nil, nil
}

func (w *workingProvider) Client() *http.Client {
	return &http.Client{
		Transport: testutil.RoundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       http.NoBody,
			}, nil
		}),
	}
}

func TestChatCompletionsAutomaticFallback(t *testing.T) {
	// Configure failing provider first, working provider second
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{
			{Name: "failing-provider", Type: "failing", Models: []config.ModelConfig{{Name: "gpt-mock", Weight: 1}}},
			{Name: "working-provider", Type: "working", Models: []config.ModelConfig{{Name: "gpt-mock", Weight: 1}}},
		},
	}

	reg := provider.NewRegistry()
	reg.Register(&failingProvider{name: "failing-provider"})
	reg.Register(&workingProvider{name: "working-provider"})

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	if err != nil {
		t.Fatalf("Load balancer err: %v", err)
	}

	authMw := middleware.NewAuthMiddleware(cfg.Auth, nil)
	proxyHandler := proxy.NewHandler(lb, nil, nil, nil)
	fe := fallback.NewFallbackExecutor(cfg.Fallback, cooldown.NewInMemoryStore(), reg)
	proxyHandler.SetFallbackExecutor(fe, cooldown.NewInMemoryStore())

	r := chi.NewRouter()
	r.Use(authMw.Handler)
	r.Post("/v1/chat/completions", proxyHandler.ChatCompletions)

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-mock",
		Messages: []model.Message{{Role: "user", Content: "Hello"}},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+testutil.AdminKey)

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d. Body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var chatResp model.ChatCompletionResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &chatResp); err != nil {
		t.Fatalf("Failed to parse body: %v", err)
	}

	if len(chatResp.Choices) == 0 || chatResp.Choices[0].Message.Content != "fallback response" {
		t.Errorf("expected choice content 'fallback response', got %+v", chatResp.Choices)
	}
}
