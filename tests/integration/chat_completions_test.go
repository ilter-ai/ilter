package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/middleware"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/provider"
	"github.com/ilter-ai/ilter/internal/proxy"
	testutil "github.com/ilter-ai/ilter/tests/utils"
)

// Simple test setup to ensure routing and basic handling works
func TestChatCompletionsRoute(t *testing.T) {
	// Setup test environment
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
		t.Fatalf("Load balancer err: %v", err)
	}

	// Disable SQLite for quick test
	authMw := middleware.NewAuthMiddleware(cfg.Auth, nil)
	proxyHandler := proxy.NewHandler(lb, nil, nil, nil)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Bypass the actual SQLite query for admin keys inside the middleware test
			next.ServeHTTP(w, r)
		})
	})
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
		t.Errorf("expected status %d, got %d. Body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}
}
