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
	"github.com/ilter-ai/ilter/internal/features/loopdetect"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/middleware"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/provider"
	"github.com/ilter-ai/ilter/internal/proxy"
	testutil "github.com/ilter-ai/ilter/tests/utils"

	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

func TestLoopDetectorIntegration(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{
			{Name: "mock", Type: "mock", Models: []config.ModelConfig{{Name: "gpt-mock", Weight: 1}}},
		},
		CostGuard: config.CostGuardConfig{LoopDetection: true},
	}

	reg := provider.NewRegistry()
	reg.Register(testutil.NewMockProvider())

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	if err != nil {
		t.Fatalf("Load balancer err: %v", err)
	}

	// Initialize Loop Detector with low thresholds:
	// Rate threshold: 2 requests (so 2nd request triggers rate signal)
	// Fingerprint repetitions: 2 (so 2nd identical request triggers fingerprint)
	// Session max requests: 1 (so 2nd request in session triggers session signal)
	detector := loopdetect.NewDetector(config.LoopSettingsConfig{
		RateThreshold:         2,
		FingerprintWindow:     5,
		FingerprintDuplicates: 2,
		SessionMaxRequests:    1,
	})

	boot := config.DefaultBootConfig()
	boot.CostGuard = cfg.CostGuard
	cfgCache := config.NewConfigCache(&boot)

	authMw := middleware.NewAuthMiddleware(cfg.Auth, nil)
	loopMw := middleware.NewLoopDetectorMiddleware(detector, nil, cfgCache)
	proxyHandler := proxy.NewHandler(lb, nil, nil, detector)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, _ := reqmeta.InitRequestMetadata(r.Context())
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Use(authMw.Handler)
	r.Use(loopMw.Handler)
	r.Post("/v1/chat/completions", proxyHandler.ChatCompletions)

	// --- Request 1 ---
	// No signals active yet. Count: Rate=1, Fingerprint=1, Session=1.
	reqBody1 := model.ChatCompletionRequest{
		Model:    "gpt-mock",
		Messages: []model.Message{{Role: "user", Content: "Hello Loop"}},
	}
	bodyBytes1, _ := json.Marshal(reqBody1)

	req1, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes1))
	req1.Header.Set("Authorization", "Bearer "+testutil.AdminKey)
	req1.Header.Set("X-Ilter-Session-Id", "session-456")

	rr1 := httptest.NewRecorder()
	r.ServeHTTP(rr1, req1)

	if rr1.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr1.Code)
	}
	if rr1.Header().Get("X-Ilter-Loop-Warning") != "" {
		t.Error("Expected no loop warning header on first request")
	}

	// --- Request 2 ---
	// Active signals:
	// - Rate (len is 2 >= 2 threshold)
	// - Fingerprint (occurrence is 2 >= 2 duplicates)
	// - Session (count is 2 > 1 max)
	// Total: 3 active signals -> Blocked!
	reqBody2 := model.ChatCompletionRequest{
		Model:    "gpt-mock",
		Messages: []model.Message{{Role: "user", Content: "Hello Loop"}},
	}
	bodyBytes2, _ := json.Marshal(reqBody2)

	req2, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes2))
	req2.Header.Set("Authorization", "Bearer "+testutil.AdminKey)
	req2.Header.Set("X-Ilter-Session-Id", "session-456")

	rr2 := httptest.NewRecorder()
	r.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusTooManyRequests {
		t.Errorf("Expected status 429 for blocked request, got %d", rr2.Code)
	}

	var jsonErr map[string]any
	if err := json.Unmarshal(rr2.Body.Bytes(), &jsonErr); err != nil {
		t.Fatalf("Failed to parse error response: %v", err)
	}
	errSub, ok := jsonErr["error"].(map[string]any)
	if !ok {
		t.Fatalf("Malformed error structure: %+v", jsonErr)
	}
	if errSub["code"] != "429" || errSub["type"] != "loop_detected" {
		t.Errorf("Expected code '429' and type 'loop_detected', got code '%v' and type '%v'", errSub["code"], errSub["type"])
	}
}

func TestLoopDetectorIntegrationWarningsAndDelays(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{
			{Name: "mock", Type: "mock", Models: []config.ModelConfig{{Name: "gpt-mock", Weight: 1}}},
		},
		CostGuard: config.CostGuardConfig{LoopDetection: true},
	}

	reg := provider.NewRegistry()
	reg.Register(testutil.NewMockProvider())

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	if err != nil {
		t.Fatalf("Load balancer err: %v", err)
	}

	// Initialize Loop Detector to trigger exactly 1 signal or 2 signals:
	// Rate threshold: 2 (triggers rate signal on 2nd request)
	// We do NOT pass session ID, and use distinct messages, and cost is 0.
	// So 2nd request will only have 1 active signal (Rate).
	// 3rd request will only have 1 active signal (Rate).
	detector := loopdetect.NewDetector(config.LoopSettingsConfig{
		RateThreshold:         2,
		FingerprintWindow:     5,
		FingerprintDuplicates: 5,
		SessionMaxRequests:    100,
	})

	boot := config.DefaultBootConfig()
	boot.CostGuard = cfg.CostGuard
	cfgCache := config.NewConfigCache(&boot)

	authMw := middleware.NewAuthMiddleware(cfg.Auth, nil)
	loopMw := middleware.NewLoopDetectorMiddleware(detector, nil, cfgCache)
	proxyHandler := proxy.NewHandler(lb, nil, nil, detector)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, _ := reqmeta.InitRequestMetadata(r.Context())
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	r.Use(authMw.Handler)
	r.Use(loopMw.Handler)
	r.Post("/v1/chat/completions", proxyHandler.ChatCompletions)

	// Request 1: Count Rate=1
	reqBody1 := model.ChatCompletionRequest{
		Model:    "gpt-mock",
		Messages: []model.Message{{Role: "user", Content: "Msg A"}},
	}
	bodyBytes1, _ := json.Marshal(reqBody1)
	req1, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes1))
	req1.Header.Set("Authorization", "Bearer "+testutil.AdminKey)
	rr1 := httptest.NewRecorder()
	r.ServeHTTP(rr1, req1)

	// Request 2: Count Rate=2 (triggers Rate signal). Active signals = 1.
	// Should return warning header.
	reqBody2 := model.ChatCompletionRequest{
		Model:    "gpt-mock",
		Messages: []model.Message{{Role: "user", Content: "Msg B"}},
	}
	bodyBytes2, _ := json.Marshal(reqBody2)
	req2, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes2))
	req2.Header.Set("Authorization", "Bearer "+testutil.AdminKey)
	rr2 := httptest.NewRecorder()
	r.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr2.Code)
	}
	if rr2.Header().Get("X-Ilter-Loop-Warning") != "true" {
		t.Error("Expected X-Ilter-Loop-Warning header to be 'true'")
	}
}
