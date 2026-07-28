package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db/dbtest"
	"github.com/ilter-ai/ilter/internal/features/budget"
	"github.com/ilter-ai/ilter/internal/features/loopdetect"
	"github.com/ilter-ai/ilter/internal/features/pii"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/middleware"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
	"github.com/ilter-ai/ilter/internal/provider"

	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

func TestMain(m *testing.M) {
	pii.LoadPatterns(pii.DefaultPIIPatterns)
	os.Exit(m.Run())
}

func TestCalculateCost(t *testing.T) {
	tests := []struct {
		name             string
		model            config.ModelConfig
		promptTokens     int
		completionTokens int
		want             float64
	}{
		{
			name: "gpt-4o-style pricing, 1000 in / 500 out",
			model: config.ModelConfig{
				Name:               "gpt-4o",
				CostPerInputToken:  0.0025,
				CostPerOutputToken: 0.01,
			},
			promptTokens:     1000,
			completionTokens: 500,
			want:             2.5 + 5.0, // 7.5
		},
		{
			name: "zero tokens yields zero cost",
			model: config.ModelConfig{
				Name:               "gpt-4o",
				CostPerInputToken:  0.0025,
				CostPerOutputToken: 0.01,
			},
			promptTokens:     0,
			completionTokens: 0,
			want:             0.0,
		},
		{
			name: "only prompt tokens",
			model: config.ModelConfig{
				Name:               "gpt-4o",
				CostPerInputToken:  0.0025,
				CostPerOutputToken: 0.01,
			},
			promptTokens:     100,
			completionTokens: 0,
			want:             0.25,
		},
		{
			name: "only completion tokens",
			model: config.ModelConfig{
				Name:               "gpt-4o",
				CostPerInputToken:  0.0025,
				CostPerOutputToken: 0.01,
			},
			promptTokens:     0,
			completionTokens: 100,
			want:             1.0,
		},
		{
			name: "result is rounded to 6 decimal places (no float noise)",
			model: config.ModelConfig{
				Name:               "test",
				CostPerInputToken:  0.0000001,
				CostPerOutputToken: 0.0000001,
			},
			promptTokens:     1234567,
			completionTokens: 7654321,
			// 1234567 * 1e-7 = 0.1234567
			// 7654321 * 1e-7 = 0.7654321
			// sum = 0.8888888
			want: 0.888889,
		},
		{
			name: "negative tokens should not happen in practice, but cost should clamp to non-negative",
			model: config.ModelConfig{
				Name:               "test",
				CostPerInputToken:  0.001,
				CostPerOutputToken: 0.002,
			},
			promptTokens:     -100,
			completionTokens: 50,
			// We don't actively clamp today; we just verify the math is consistent
			// so that future refactors don't silently change the formula.
			want: -0.1 + 0.1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateCost(tt.model, tt.promptTokens, tt.completionTokens)
			if got != tt.want {
				t.Errorf("CalculateCost(%+v, %d, %d) = %v, want %v",
					tt.model, tt.promptTokens, tt.completionTokens, got, tt.want)
			}
		})
	}
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type errorProvider struct {
	name string
}

type quotaStreamProvider struct {
	name string
}

func (e *errorProvider) Name() string { return e.name }
func (e *errorProvider) Type() string { return "openai" }
func (e *errorProvider) TransformRequest(ctx context.Context, _ *model.ChatCompletionRequest) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, "POST", "http://quota-error", nil)
}

func (e *errorProvider) TransformResponse(_ context.Context, _ *http.Response) (*model.ChatCompletionResponse, error) {
	return nil, fmt.Errorf("provider returned error: you exceeded your current quota")
}

func (e *errorProvider) Client() *http.Client {
	return &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       http.NoBody,
		}, nil
	})}
}
func (e *errorProvider) HealthCheck(_ context.Context) error { return nil }
func (e *errorProvider) DiscoverModels(_ context.Context) ([]catalog.ModelInfo, error) {
	return nil, nil
}

func (q *quotaStreamProvider) Name() string { return q.name }
func (q *quotaStreamProvider) Type() string { return "openai" }
func (q *quotaStreamProvider) TransformRequest(ctx context.Context, _ *model.ChatCompletionRequest) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, "POST", "http://quota-stream", nil)
}

func (q *quotaStreamProvider) TransformResponse(_ context.Context, _ *http.Response) (*model.ChatCompletionResponse, error) {
	return nil, fmt.Errorf("provider returned error: you exceeded your current quota")
}

func (q *quotaStreamProvider) Client() *http.Client {
	return &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(bytes.NewBufferString(`{"type":"error","error":{"type":"FreeUsageLimitError","message":"Rate limit exceeded. Please try again later."},"metadata":{}}`)),
		}, nil
	})}
}
func (q *quotaStreamProvider) HealthCheck(_ context.Context) error { return nil }
func (q *quotaStreamProvider) DiscoverModels(_ context.Context) ([]catalog.ModelInfo, error) {
	return nil, nil
}

func TestProviderErrorStatus_RecognizesZenQuotaMessages(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "current quota", err: fmt.Errorf("provider returned error: you exceeded your current quota"), want: http.StatusTooManyRequests},
		{name: "insufficient balance", err: fmt.Errorf("provider returned error: insufficient balance for this model"), want: http.StatusTooManyRequests},
		{name: "billing limit", err: fmt.Errorf("provider returned error: billing limit exceeded"), want: http.StatusTooManyRequests},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := providerErrorStatus(tt.err); got != tt.want {
				t.Fatalf("providerErrorStatus(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

func TestChatCompletionsStreamingQuotaErrorReturns429(t *testing.T) {
	cfg := &config.Config{
		Providers: []config.ProviderConfig{{
			Name:   "quota-stream-provider",
			Type:   "openai",
			Models: []config.ModelConfig{{Name: "gpt-4o-mini", Weight: 1}},
		}},
	}

	reg := provider.NewRegistry()
	reg.Register(&quotaStreamProvider{name: "quota-stream-provider"})

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	if err != nil {
		t.Fatalf("failed to create load balancer: %v", err)
	}

	h := NewHandler(lb, nil, nil, nil)

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-4o-mini",
		Stream:   true,
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	rr := httptest.NewRecorder()

	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status %d, got %d. Body: %s", http.StatusTooManyRequests, rr.Code, rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("quota")) {
		t.Fatalf("expected quota-related error message, got: %s", rr.Body.String())
	}
}

func TestChatCompletionsQuotaErrorMessageIsClean(t *testing.T) {
	cfg := &config.Config{
		Providers: []config.ProviderConfig{{
			Name:   "quota-provider-clean",
			Type:   "openai",
			Models: []config.ModelConfig{{Name: "gpt-4o-mini", Weight: 1}},
		}},
	}

	reg := provider.NewRegistry()
	reg.Register(&quotaStreamProvider{name: "quota-provider-clean"})

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	if err != nil {
		t.Fatalf("failed to create load balancer: %v", err)
	}

	h := NewHandler(lb, nil, nil, nil)

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-4o-mini",
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	rr := httptest.NewRecorder()

	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status %d, got %d. Body: %s", http.StatusTooManyRequests, rr.Code, rr.Body.String())
	}
	if got := rr.Body.String(); !bytes.Contains([]byte(got), []byte("Rate limit exceeded")) {
		t.Fatalf("expected clean rate-limit message, got: %s", got)
	}
	if got := rr.Body.String(); bytes.Contains([]byte(got), []byte(`\"type\":\"error\"`)) {
		t.Fatalf("expected raw upstream JSON to be removed from the client message, got: %s", got)
	}
}

func TestChatCompletionsQuotaErrorReturns429(t *testing.T) {
	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{
				Name:   "quota-provider",
				Type:   "openai",
				Models: []config.ModelConfig{{Name: "gpt-4o-mini", Weight: 1}},
			},
		},
	}

	reg := provider.NewRegistry()
	reg.Register(&errorProvider{name: "quota-provider"})

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	if err != nil {
		t.Fatalf("failed to create load balancer: %v", err)
	}

	h := NewHandler(lb, nil, nil, nil)

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-4o-mini",
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	rr := httptest.NewRecorder()

	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected status %d, got %d. Body: %s", http.StatusTooManyRequests, rr.Code, rr.Body.String())
	}

	if got := rr.Body.String(); !bytes.Contains([]byte(got), []byte("quota")) {
		t.Fatalf("expected quota-related error message, got: %s", got)
	}
}

func TestChatCompletionsSmartRouting(t *testing.T) {
	// Populate catalog.Models so the smart router can categorize by tier.
	catalog.ModelsMu.Lock()
	catalog.Models["gpt-4o-mini"] = []catalog.ModelInfo{{ID: "gpt-4o-mini", Provider: "openai", Tier: "economy"}}
	catalog.Models["gpt-4o"] = []catalog.ModelInfo{{ID: "gpt-4o", Provider: "openai", Tier: "standard"}}
	catalog.ModelsMu.Unlock()
	t.Cleanup(func() {
		catalog.ModelsMu.Lock()
		delete(catalog.Models, "gpt-4o-mini")
		delete(catalog.Models, "gpt-4o")
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
				},
			},
		},
		Routing: config.RoutingConfig{
			Enabled: true,
		},
		Headers: config.HeadersConfig{
			EmitStandard: true,
		},
	}

	reg := provider.NewRegistry()
	reg.Register(provider.NewMockProvider("openai"))

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	if err != nil {
		t.Fatalf("failed to create load balancer: %v", err)
	}

	h := NewHandler(lb, nil, nil, nil)
	h.SetConfig(cfg)

	// Send an economy request (short prompt) with explicit model
	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-4o-mini",
		Messages: []model.Message{{Role: "user", Content: "Hello"}},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	rr := httptest.NewRecorder()

	h.ChatCompletions(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d. Body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	complexityScoreHeader := rr.Header().Get("X-Ilter-Complexity-Score")
	modelSelectedHeader := rr.Header().Get("X-Ilter-Model-Selected")

	if complexityScoreHeader == "" {
		t.Error("expected X-Ilter-Complexity-Score header to be present")
	}
	if modelSelectedHeader != "gpt-4o-mini" {
		t.Errorf("expected X-Ilter-Model-Selected to be 'gpt-4o-mini', got '%s'", modelSelectedHeader)
	}
}

func TestResolveRequestedModel_StripsProviderPrefix(t *testing.T) {
	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{Name: "opencode_zen", Type: "openai", Models: []config.ModelConfig{{Name: "deepseek-v4-flash-free"}}},
			{Name: "openrouter", Type: "openai", Models: []config.ModelConfig{{Name: "anthropic/claude-sonnet-4"}}},
		},
	}

	h := NewHandler(nil, nil, nil, nil)
	h.SetConfig(cfg)

	tests := []struct {
		name  string
		model string
		want  string
	}{
		{
			name:  "known provider prefix is stripped (single slash)",
			model: "opencode_zen/deepseek-v4-flash-free",
			want:  "deepseek-v4-flash-free",
		},
		{
			name:  "known provider prefix is stripped (double slash after prefix)",
			model: "openrouter/anthropic/claude-sonnet-4",
			want:  "anthropic/claude-sonnet-4",
		},
		{
			name:  "model with slash has prefix stripped",
			model: "anthropic/claude-sonnet-4",
			want:  "claude-sonnet-4",
		},
		{
			name:  "model without any slash is preserved",
			model: "deepseek-v4-flash-free",
			want:  "deepseek-v4-flash-free",
		},
		{
			name:  "any prefix before slash is stripped regardless of case",
			model: "OPencode_zen/deepseek-v4-flash-free",
			want:  "deepseek-v4-flash-free",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &model.ChatCompletionRequest{Model: tt.model}
			got, _, ok := h.resolveRequestedModel(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil), req, nil)
			if !ok {
				t.Fatal("resolveRequestedModel returned ok=false unexpectedly")
			}
			if got != tt.want {
				t.Errorf("resolveRequestedModel(model=%q) = %q, want %q", tt.model, got, tt.want)
			}
		})
	}
}

func TestResolveRequestedModel_PreservesWithoutConfig(t *testing.T) {
	h := NewHandler(nil, nil, nil, nil)

	req := &model.ChatCompletionRequest{Model: "opencode_zen/deepseek-v4-flash-free"}
	got, _, ok := h.resolveRequestedModel(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil), req, nil)
	if !ok {
		t.Fatal("resolveRequestedModel returned ok=false unexpectedly")
	}
	// CanonicalModelID strips any provider prefix (everything before '/').
	if got != "deepseek-v4-flash-free" {
		t.Errorf("expected stripped model name, got %q", got)
	}
}

// If a provider named "anthropic" exists and a user sends "anthropic/claude-opus"
// (intending OpenRouter's vendor/model syntax), the prefix is stripped and routing
// goes to the configured Anthropic provider, not OpenRouter. This is by design.
func TestResolveRequestedModel_ConfiguredPrefixCollision(t *testing.T) {
	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{Name: "anthropic", BaseURL: "https://api.anthropic.com", APIKey: "sk-test"},
			{Name: "openrouter", BaseURL: "https://openrouter.ai/api/v1", APIKey: "sk-test"},
		},
	}
	h := NewHandler(nil, nil, nil, nil)
	h.SetConfig(cfg)

	req := &model.ChatCompletionRequest{Model: "anthropic/claude-opus"}
	got, _, ok := h.resolveRequestedModel(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil), req, nil)
	if !ok {
		t.Fatal("resolveRequestedModel returned ok=false unexpectedly")
	}
	if got != "claude-opus" {
		t.Errorf("expected prefix-stripped model %q, got %q", "claude-opus", got)
	}
}

func TestEstimateInputTokens(t *testing.T) {
	tests := []struct {
		name     string
		messages []model.Message
		wantMin  int
		wantMax  int
	}{
		{
			name:     "empty messages returns minimum token floor",
			messages: []model.Message{},
			wantMin:  4, // 0 words → floored to 4, 4 * 1.3 = 5.2 → int = 5
			wantMax:  7,
		},
		{
			name: "single short message",
			messages: []model.Message{
				{Role: "user", Content: "Hello"},
			},
			wantMin: 4, // 1 word → floored to 4, 4 * 1.3 = 5.2 → int = 5
			wantMax: 8,
		},
		{
			name: "longer message with multiple words",
			messages: []model.Message{
				{Role: "user", Content: "What is the capital of France?"},
			},
			wantMin: 7, // 6 words * 1.3 = 7.8 → int = 7
			wantMax: 15,
		},
		{
			name: "message with no content skips gracefully",
			messages: []model.Message{
				{Role: "user"},
			},
			wantMin: 4,
			wantMax: 7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateInputTokens(tt.messages)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("estimateInputTokens() = %d, want between %d and %d", got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestFindCheapestAlternativeCost(t *testing.T) {
	// The registry is populated from the provider_models DB table at startup.
	// We just need to find a model we know exists and verify the lookup works.
	catalog.ModelsMu.RLock()
	knownModel := ""
	for name := range catalog.Models {
		knownModel = name
		break
	}
	catalog.ModelsMu.RUnlock()

	if knownModel == "" {
		t.Skip("no models in registry, skipping test")
	}

	altCost := findCheapestAlternativeCost(knownModel, 100, 50)
	if altCost < 0 {
		t.Errorf("findCheapestAlternativeCost(%q) = %v, expected non-negative cost", knownModel, altCost)
	}
}

func TestChatCompletionsCostEstimateHeaders(t *testing.T) {
	// Register test models with a unique tier so findCheapestAlternativeCost
	// doesn't pick embedded ultra-cheap models whose tiny costs round to 0.
	catalog.ModelsMu.Lock()
	catalog.Models["test-model-a"] = []catalog.ModelInfo{{
		ID:                 "test-model-a",
		Tier:               "internal-test-tier",
		Provider:           "test-provider",
		CostPerInputToken:  0.00015,
		CostPerOutputToken: 0.0006,
	}}
	catalog.Models["test-model-b"] = []catalog.ModelInfo{{
		ID:                 "test-model-b",
		Tier:               "internal-test-tier",
		Provider:           "test-provider",
		CostPerInputToken:  0.00010,
		CostPerOutputToken: 0.0004,
	}}
	catalog.ModelsMu.Unlock()

	// Clean up test models after test
	defer func() {
		catalog.ModelsMu.Lock()
		delete(catalog.Models, "test-model-a")
		delete(catalog.Models, "test-model-b")
		catalog.ModelsMu.Unlock()
	}()

	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{
				Name: "test-provider",
				Type: "openai",
				Models: []config.ModelConfig{
					{
						Name:               "test-model-a",
						Weight:             1,
						CostPerInputToken:  0.00015,
						CostPerOutputToken: 0.0006,
					},
				},
			},
		},
		Routing: config.RoutingConfig{
			Enabled: true,
		},
		Headers: config.HeadersConfig{
			EmitStandard: true,
		},
	}

	reg := provider.NewRegistry()
	reg.Register(provider.NewMockProvider("test-provider"))

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	if err != nil {
		t.Fatalf("failed to create load balancer: %v", err)
	}

	h := NewHandler(lb, nil, nil, nil)
	h.SetConfig(cfg)

	tests := []struct {
		name     string
		messages []model.Message
	}{
		{
			name:     "short prompt gets cost estimate headers",
			messages: []model.Message{{Role: "user", Content: "Hello world"}},
		},
		{
			name: "longer prompt gets cost estimate headers",
			messages: []model.Message{
				{Role: "system", Content: "You are a helpful assistant."},
				{Role: "user", Content: "Write a comprehensive analysis of the economic impacts of artificial intelligence on global labor markets."},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody := model.ChatCompletionRequest{
				Model:    "test-model-a",
				Messages: tt.messages,
			}
			bodyBytes, _ := json.Marshal(reqBody)

			req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
			rr := httptest.NewRecorder()

			h.ChatCompletions(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d. Body: %s", http.StatusOK, rr.Code, rr.Body.String())
			}

			costEstimate := rr.Header().Get("X-Ilter-Cost-Estimate")
			altCost := rr.Header().Get("X-Ilter-Alternative-Cost")
			savings := rr.Header().Get("X-Ilter-Savings-Potential")

			if costEstimate == "" {
				t.Error("expected X-Ilter-Cost-Estimate header to be present")
			}
			if altCost == "" {
				t.Error("expected X-Ilter-Alternative-Cost header to be present")
			}
			if savings == "" {
				t.Error("expected X-Ilter-Savings-Potential header to be present")
			}

			var costVal float64
			if _, err := fmt.Sscanf(costEstimate, "%f", &costVal); err != nil {
				t.Errorf("X-Ilter-Cost-Estimate is not a valid float: %s", costEstimate)
			}
			if costVal < 0 {
				t.Errorf("X-Ilter-Cost-Estimate should be non-negative, got %f", costVal)
			}

			var altVal float64
			if _, err := fmt.Sscanf(altCost, "%f", &altVal); err != nil {
				t.Errorf("X-Ilter-Alternative-Cost is not a valid float: %s", altCost)
			}
			if altVal < 0 {
				t.Errorf("X-Ilter-Alternative-Cost should be non-negative, got %f", altVal)
			}

			if !strings.HasSuffix(savings, "%") {
				t.Errorf("X-Ilter-Savings-Potential should end with %%%%, got %s", savings)
			}

			complexityScore := rr.Header().Get("X-Ilter-Complexity-Score")
			if complexityScore == "" {
				t.Error("expected X-Ilter-Complexity-Score header to be present")
			}

			modelSelected := rr.Header().Get("X-Ilter-Model-Selected")
			if modelSelected == "" {
				t.Error("expected X-Ilter-Model-Selected header to be present")
			}

			// Standard-compatible headers (emitted by default)
			reqCost := rr.Header().Get("X-Request-Cost")
			if reqCost == "" {
				t.Error("expected X-Request-Cost header to be present (EmitStandard=true by default)")
			}

			// X-Request-Pricing requires Usage data from the provider response,
			// which the basic mockProvider doesn't return. It's tested separately
			// in the usage recording test with usageMockProvider.
		})
	}
}

func TestChatCompletionsXIlterCostHeader(t *testing.T) {
	// The proxy CalculateCost is tested in transformer_test.go.
	catalog.ModelsMu.Lock()
	catalog.Models["free-cost-model"] = []catalog.ModelInfo{{
		ID:                 "free-cost-model",
		Provider:           "test-provider",
		CostPerInputToken:  0,
		CostPerOutputToken: 0,
	}}
	catalog.ModelsMu.Unlock()
	defer func() {
		catalog.ModelsMu.Lock()
		delete(catalog.Models, "free-cost-model")
		catalog.ModelsMu.Unlock()
	}()

	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{
				Name: "test-provider",
				Type: "openai",
				Models: []config.ModelConfig{
					{
						Name:               "free-cost-model",
						Weight:             1,
						CostPerInputToken:  0,
						CostPerOutputToken: 0,
					},
				},
			},
		},
	}

	reg := provider.NewRegistry()
	reg.Register(&usageMockProvider{MockProvider: provider.NewMockProvider("test-provider")})

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	if err != nil {
		t.Fatalf("failed to create load balancer: %v", err)
	}

	h := NewHandler(lb, nil, nil, nil)
	h.SetConfig(cfg)

	reqBody := model.ChatCompletionRequest{
		Model:    "free-cost-model",
		Messages: []model.Message{{Role: "user", Content: "Hello"}},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	rr := httptest.NewRecorder()

	h.ChatCompletions(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	costHeader := rr.Header().Get("X-Ilter-Cost")
	if costHeader == "" {
		t.Fatal("X-Ilter-Cost header is missing")
	}
	cost, err := strconv.ParseFloat(costHeader, 64)
	if err != nil {
		t.Fatalf("X-Ilter-Cost is not a valid float: %s", costHeader)
	}
	assert.Equal(t, 0.0, cost, "free model should have zero cost")
}

type usageMockProvider struct {
	*provider.MockProvider
}

func (m *usageMockProvider) TransformResponse(_ context.Context, _ *http.Response) (*model.ChatCompletionResponse, error) {
	return &model.ChatCompletionResponse{
		ID:      "usage-mock-id",
		Model:   "gpt-4o-mini",
		Choices: []model.Choice{{Message: model.ChoiceMessage{Content: "mock response"}}},
		Usage: &model.Usage{
			PromptTokens:     50,
			CompletionTokens: 100,
			TotalTokens:      150,
		},
	}, nil
}

type chainMockProvider struct {
	*provider.MockProvider
}

func (m *chainMockProvider) TransformResponse(_ context.Context, _ *http.Response) (*model.ChatCompletionResponse, error) {
	return &model.ChatCompletionResponse{
		ID:      "chain-mock-id",
		Model:   "gpt-4o-mini",
		Choices: []model.Choice{{Message: model.ChoiceMessage{Content: "mock response"}}},
		Usage: &model.Usage{
			PromptTokens:     50,
			CompletionTokens: 100,
			TotalTokens:      150,
		},
	}, nil
}

func TestChatCompletionsFullChainWithPII(t *testing.T) {
	store := dbtest.NewFile(t)

	pii := middleware.NewPIIMaskerMiddleware(store, config.PIIConfig{
		Enabled: true,
		Mode:    "mask",
	}, nil, nil)

	budget := budget.NewEnforcer(config.BudgetConfig{Enabled: true}, nil, store, nil)
	loop := loopdetect.NewDetector(config.LoopSettingsConfig{})

	cfg := &config.Config{
		Providers: []config.ProviderConfig{{
			Name: "chain-test-provider",
			Type: "openai",
			Models: []config.ModelConfig{{
				Name:               "gpt-4o-mini",
				Weight:             1,
				CostPerInputToken:  0.00015,
				CostPerOutputToken: 0.0006,
			}},
		}},
	}

	reg := provider.NewRegistry()
	reg.Register(&chainMockProvider{MockProvider: provider.NewMockProvider("chain-test-provider")})

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	require.NoError(t, err)

	h := NewHandler(lb, nil, budget, loop)
	h.SetConfig(cfg)
	h.SetStore(store)

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-4o-mini",
		Messages: []model.Message{{Role: "user", Content: "My email is test@example.com"}},
	}
	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")

	ctx := context.WithValue(req.Context(), reqmeta.KeyIDContextKey, "legacy_1")
	ctx, meta := reqmeta.InitRequestMetadata(ctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	piiHandler := pii.Handler(http.HandlerFunc(h.ChatCompletions))
	piiHandler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code, "handler should return 200 OK")
	assert.True(t, meta.PIIMasked != nil && *meta.PIIMasked, "PII should have been masked")

	var usageCount int
	err = store.DB.QueryRow("SELECT COUNT(*) FROM usage_daily").Scan(&usageCount)
	assert.NoError(t, err)
	assert.Equal(t, 1, usageCount, "usage_daily should have 1 row")
	var promptTokens, completionTokens, tokens, requestCount int
	var modelName, providerName string
	err = store.DB.QueryRow(
		"SELECT model, provider, prompt_tokens, completion_tokens, tokens, request_count FROM usage_daily WHERE key_id = 'legacy_1'",
	).Scan(&modelName, &providerName, &promptTokens, &completionTokens, &tokens, &requestCount)
	assert.NoError(t, err)
	assert.Equal(t, "gpt-4o-mini", modelName)
	assert.Equal(t, "chain-test-provider", providerName)
	assert.Equal(t, 50, promptTokens, "prompt_tokens should add up across requests")
	assert.Equal(t, 100, completionTokens, "completion_tokens should add up across requests")
	assert.Equal(t, 150, tokens, "tokens should add up across requests")
	assert.Equal(t, 1, requestCount, "request_count should be 1")
}

func TestChatCompletionsRecordsUsageDaily(t *testing.T) {
	store := dbtest.NewFile(t)

	cfg := &config.Config{
		Providers: []config.ProviderConfig{{
			Name: "test-provider",
			Type: "openai",
			Models: []config.ModelConfig{{
				Name:               "gpt-4o-mini",
				Weight:             1,
				CostPerInputToken:  0.00015,
				CostPerOutputToken: 0.0006,
			}},
		}},
		Headers: config.HeadersConfig{
			EmitStandard: true,
		},
	}

	reg := provider.NewRegistry()
	reg.Register(&usageMockProvider{MockProvider: provider.NewMockProvider("test-provider")})

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	if err != nil {
		t.Fatalf("failed to create load balancer: %v", err)
	}

	h := NewHandler(lb, nil, nil, nil)
	h.SetConfig(cfg)
	h.SetStore(store)

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-4o-mini",
		Messages: []model.Message{{Role: "user", Content: "Hello"}},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	rr := httptest.NewRecorder()
	h.ChatCompletions(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	// X-Request-Pricing should be present when provider returns Usage data
	assert.NotEmpty(t, rr.Header().Get("X-Request-Cost"), "X-Request-Cost should be present")
	assert.NotEmpty(t, rr.Header().Get("X-Request-Pricing"), "X-Request-Pricing should be present when Usage is available")

	req = httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	rr = httptest.NewRecorder()
	h.ChatCompletions(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	rows, err := store.DB.Query("SELECT key_id, date, model, provider, prompt_tokens, completion_tokens, tokens, cost, request_count FROM usage_daily")
	if err != nil {
		t.Fatalf("failed to query usage_daily: %v", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var keyID string
		var date, model, provider string
		var promptTokens, completionTokens, tokens, requestCount int
		var cost float64
		if err := rows.Scan(&keyID, &date, &model, &provider, &promptTokens, &completionTokens, &tokens, &cost, &requestCount); err != nil {
			t.Fatalf("failed to scan row: %v", err)
		}
		count++
		assert.Equal(t, "", keyID)
		assert.Equal(t, "gpt-4o-mini", model)
		assert.Equal(t, "test-provider", provider)
		assert.Equal(t, 100, promptTokens)
		assert.Equal(t, 200, completionTokens)
		assert.Equal(t, 300, tokens)
		assert.Equal(t, 2, requestCount)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 usage_daily row, got %d", count)
	}
}

func TestCostEstimateHeadersConsistency(t *testing.T) {
	// Verify that when alternative cost < cost estimate, savings potential is positive
	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{
				Name: "test-provider",
				Type: "openai",
				Models: []config.ModelConfig{
					{
						Name:               "gpt-4o",
						Weight:             1,
						CostPerInputToken:  0.01,
						CostPerOutputToken: 0.03,
					},
				},
			},
		},
		Routing: config.RoutingConfig{
			Enabled: true,
		},
	}

	reg := provider.NewRegistry()
	reg.Register(provider.NewMockProvider("test-provider"))

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	if err != nil {
		t.Fatalf("failed to create load balancer: %v", err)
	}

	h := NewHandler(lb, nil, nil, nil)
	h.SetConfig(cfg)

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: []model.Message{{Role: "user", Content: "Hello"}},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	rr := httptest.NewRecorder()

	h.ChatCompletions(rr, req)

	costEstimate := rr.Header().Get("X-Ilter-Cost-Estimate")
	savings := rr.Header().Get("X-Ilter-Savings-Potential")

	// Parse cost
	var costVal float64
	_, _ = fmt.Sscanf(costEstimate, "%f", &costVal)

	// Parse savings percentage
	var savingsPct int
	_, _ = fmt.Sscanf(savings, "%d%%", &savingsPct)

	if costVal > 0 && savings != "" {
		// If there are cheaper alternatives in the registry, savings should reflect that
		if savingsPct < 0 {
			t.Errorf("savings potential should be non-negative, got %d%%", savingsPct)
		}
		if savingsPct > 100 {
			t.Errorf("savings potential should not exceed 100%%, got %d%%", savingsPct)
		}
	}
}
