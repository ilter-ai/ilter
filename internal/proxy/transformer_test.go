package proxy

import (
	"fmt"
	"math"
	"net/http"
	"testing"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
)

func TestTransformerCalculateCost(t *testing.T) {
	tests := []struct {
		name             string
		model            config.ModelConfig
		promptTokens     int
		completionTokens int
		want             float64
	}{
		{
			name: "very small costs round correctly",
			model: config.ModelConfig{
				CostPerInputToken:  0.000001,
				CostPerOutputToken: 0.000002,
			},
			promptTokens:     1,
			completionTokens: 1,
			want:             0.000003,
		},
		{
			name: "large token counts produce no float noise",
			model: config.ModelConfig{
				CostPerInputToken:  0.00015,
				CostPerOutputToken: 0.0006,
			},
			promptTokens:     123456,
			completionTokens: 654321,
			// 123456 * 0.00015 = 18.5184
			// 654321 * 0.0006 = 392.5926
			// sum = 411.111
			want: 411.111,
		},
		{
			name: "six decimal rounding preserves precision",
			model: config.ModelConfig{
				CostPerInputToken:  0.0000001,
				CostPerOutputToken: 0.0000001,
			},
			promptTokens:     3333333,
			completionTokens: 6666667,
			// 3333333 * 1e-7 = 0.3333333
			// 6666667 * 1e-7 = 0.6666667
			// sum = 1.0000000
			want: 1.0,
		},
		{
			name: "zero cost per token",
			model: config.ModelConfig{
				CostPerInputToken:  0,
				CostPerOutputToken: 0,
			},
			promptTokens:     1000,
			completionTokens: 500,
			want:             0.0,
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

func TestTransformerEstimateInputTokens(t *testing.T) {
	tests := []struct {
		name     string
		messages []model.Message
		want     int
	}{
		{
			name:     "nil messages slice",
			messages: nil,
			want:     5, // 0 words → floored to 4, 4*1.3 = 5.2 → int = 5
		},
		{
			name: "message with nil content",
			messages: []model.Message{
				{Role: "user", Content: nil},
			},
			want: 5, // skipped, floor 4 → 4*1.3 = 5.2 → int = 5
		},
		{
			name: "mixed content types — string and array",
			messages: []model.Message{
				{Role: "system", Content: "You are a helpful assistant."},                // 5 words (period attached)
				{Role: "user", Content: []any{"hello", map[string]any{"text": "world"}}}, // 2 words
			},
			want: 9, // 7 words * 1.3 = 9.1 → int = 9
		},
		{
			name: "empty string content",
			messages: []model.Message{
				{Role: "user", Content: ""},
			},
			want: 5, // 0 words → floored to 4, 4*1.3 = 5
		},
		{
			name: "array content with non-string non-map items",
			messages: []model.Message{
				{Role: "user", Content: []any{42, true, 3.14}},
			},
			want: 5, // none contribute words → floor 4 → 4*1.3 = 5
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := estimateInputTokens(tt.messages)
			if got != tt.want {
				t.Errorf("estimateInputTokens() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestTransformerFindCheapestAlternativeCost(t *testing.T) {
	// Register test models in a unique tier
	catalog.ModelsMu.Lock()
	catalog.Models["transformer-test-alpha"] = []catalog.ModelInfo{{
		ID:                 "transformer-test-alpha",
		Tier:               "transformer-test-tier",
		Provider:           "test-provider",
		CostPerInputToken:  0.0005,
		CostPerOutputToken: 0.002,
	}}
	catalog.Models["transformer-test-beta"] = []catalog.ModelInfo{{
		ID:                 "transformer-test-beta",
		Tier:               "transformer-test-tier",
		Provider:           "test-provider",
		CostPerInputToken:  0.0003,
		CostPerOutputToken: 0.001,
	}}
	catalog.Models["transformer-test-gamma"] = []catalog.ModelInfo{{
		ID:                 "transformer-test-gamma",
		Tier:               "transformer-test-tier",
		Provider:           "test-provider",
		CostPerInputToken:  0.0001,
		CostPerOutputToken: 0.0005,
	}}
	catalog.ModelsMu.Unlock()

	defer func() {
		catalog.ModelsMu.Lock()
		delete(catalog.Models, "transformer-test-alpha")
		delete(catalog.Models, "transformer-test-beta")
		delete(catalog.Models, "transformer-test-gamma")
		catalog.ModelsMu.Unlock()
	}()

	tests := []struct {
		name          string
		selectedModel string
		inputTokens   int
		outputTokens  int
		want          float64
	}{
		{
			name:          "finds cheapest among same tier",
			selectedModel: "transformer-test-alpha", // 0.0005/0.002
			inputTokens:   100,
			outputTokens:  50,
			// gamma: 100*0.0001 + 50*0.0005 = 0.01 + 0.025 = 0.035
			want: 0.035,
		},
		{
			name:          "already cheapest returns same",
			selectedModel: "transformer-test-gamma", // already the cheapest
			inputTokens:   100,
			outputTokens:  50,
			// gamma: 0.035
			// beta:  0.03 + 0.05 = 0.08
			// alpha: 0.05 + 0.1 = 0.15
			// cheapest alternative = beta = 0.08
			want: 0.08,
		},
		{
			name:          "unknown model returns 0",
			selectedModel: "nonexistent-model",
			inputTokens:   100,
			outputTokens:  50,
			want:          0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findCheapestAlternativeCost(tt.selectedModel, tt.inputTokens, tt.outputTokens)
			if math.Abs(got-tt.want) > 0.000001 {
				t.Errorf("findCheapestAlternativeCost(%q, %d, %d) = %v, want %v",
					tt.selectedModel, tt.inputTokens, tt.outputTokens, got, tt.want)
			}
		})
	}
}

func TestTransformerProviderErrorStatus(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{
			name: "nil error returns 502",
			err:  nil,
			want: http.StatusBadGateway,
		},
		{
			name: "quota exceeded returns 429",
			err:  fmt.Errorf("quota exceeded for this model"),
			want: http.StatusTooManyRequests,
		},
		{
			name: "rate limit returns 429",
			err:  fmt.Errorf("rate limit exceeded"),
			want: http.StatusTooManyRequests,
		},
		{
			name: "insufficient balance returns 429",
			err:  fmt.Errorf("insufficient balance"),
			want: http.StatusTooManyRequests,
		},
		{
			name: "billing error returns 429",
			err:  fmt.Errorf("billing error"),
			want: http.StatusTooManyRequests,
		},
		{
			name: "credit limit returns 429",
			err:  fmt.Errorf("credit limit reached"),
			want: http.StatusTooManyRequests,
		},
		{
			name: "HTTP 429 in message returns 429",
			err:  fmt.Errorf("provider returned status 429"),
			want: http.StatusTooManyRequests,
		},
		{
			name: "payment required returns 429",
			err:  fmt.Errorf("payment required"),
			want: http.StatusTooManyRequests,
		},
		{
			name: "unauthorized returns 401",
			err:  fmt.Errorf("unauthorized: invalid API key"),
			want: http.StatusUnauthorized,
		},
		{
			name: "HTTP 401 returns 401",
			err:  fmt.Errorf("provider returned status 401"),
			want: http.StatusUnauthorized,
		},
		{
			name: "forbidden returns 401",
			err:  fmt.Errorf("forbidden: access denied"),
			want: http.StatusUnauthorized,
		},
		{
			name: "HTTP 403 returns 401",
			err:  fmt.Errorf("provider returned status 403"),
			want: http.StatusUnauthorized,
		},
		{
			name: "bad request returns 400",
			err:  fmt.Errorf("400 bad request"),
			want: http.StatusBadRequest,
		},
		{
			name: "invalid request returns 400",
			err:  fmt.Errorf("invalid_request: unknown parameter"),
			want: http.StatusBadRequest,
		},
		{
			name: "generic provider error returns 502",
			err:  fmt.Errorf("provider returned status 500: internal server error"),
			want: http.StatusBadGateway,
		},
		{
			name: "connection refused returns 502",
			err:  fmt.Errorf("connection refused"),
			want: http.StatusBadGateway,
		},
		{
			name: "context deadline returns 502",
			err:  fmt.Errorf("context deadline exceeded"),
			want: http.StatusBadGateway,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := providerErrorStatus(tt.err)
			if got != tt.want {
				t.Errorf("providerErrorStatus(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

func TestTransformerSanitizeProviderErrorMessage(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "empty string",
			raw:  "",
			want: "",
		},
		{
			name: "whitespace only",
			raw:  "  \n\t  ",
			want: "",
		},
		{
			name: "valid JSON with error.message",
			raw:  `{"type":"error","error":{"type":"FreeUsageLimitError","message":"Rate limit exceeded. Please try again later."},"metadata":{}}`,
			want: "Rate limit exceeded. Please try again later.",
		},
		{
			name: "valid JSON with top-level message",
			raw:  `{"type":"error","message":"Bad request: invalid model"}`,
			want: "Bad request: invalid model",
		},
		{
			name: "JSON with both message and error.message — error.message wins",
			raw:  `{"type":"error","message":"top-level","error":{"type":"AuthError","message":"unauthorized"}}`,
			want: "unauthorized",
		},
		{
			name: "valid JSON without message fields returns original",
			raw:  `{"id":"123","object":"model"}`,
			want: `{"id":"123","object":"model"}`,
		},
		{
			name: "non-JSON string returned as-is",
			raw:  "Internal Server Error",
			want: "Internal Server Error",
		},
		{
			name: "non-JSON with leading/trailing whitespace is trimmed",
			raw:  "  Service Unavailable  ",
			want: "Service Unavailable",
		},
		{
			name: "malformed JSON prefix",
			raw:  `{"type":"error","message"`,
			want: `{"type":"error","message"`,
		},
		{
			name: "nested error with empty message",
			raw:  `{"error":{"message":""}}`,
			want: `{"error":{"message":""}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeProviderErrorMessage(tt.raw)
			if got != tt.want {
				t.Errorf("sanitizeProviderErrorMessage(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestTransformerComputeCostEstimates(t *testing.T) {
	// Register a test model for alternative cost lookup
	catalog.ModelsMu.Lock()
	catalog.Models["transformer-est-test-model"] = []catalog.ModelInfo{{
		ID:                 "transformer-est-test-model",
		Tier:               "transformer-est-tier",
		Provider:           "test-provider",
		CostPerInputToken:  0.001,
		CostPerOutputToken: 0.002,
	}}
	catalog.Models["transformer-est-cheaper"] = []catalog.ModelInfo{{
		ID:                 "transformer-est-cheaper",
		Tier:               "transformer-est-tier",
		Provider:           "test-provider",
		CostPerInputToken:  0.0001,
		CostPerOutputToken: 0.0002,
	}}
	catalog.ModelsMu.Unlock()

	defer func() {
		catalog.ModelsMu.Lock()
		delete(catalog.Models, "transformer-est-test-model")
		delete(catalog.Models, "transformer-est-cheaper")
		catalog.ModelsMu.Unlock()
	}()

	tests := []struct {
		name          string
		messages      []model.Message
		modelCfg      config.ModelConfig
		selectedModel string
		maxTokens     *int
		checkAltCost  bool // whether altCost is expected to be > 0
	}{
		{
			name: "short prompt, default output tokens",
			messages: []model.Message{
				{Role: "user", Content: "Hello"},
			},
			modelCfg: config.ModelConfig{
				CostPerInputToken:  0.00015,
				CostPerOutputToken: 0.0006,
			},
			selectedModel: "transformer-est-test-model",
			maxTokens:     nil,
			checkAltCost:  true,
		},
		{
			name:     "empty messages, custom max tokens",
			messages: []model.Message{},
			modelCfg: config.ModelConfig{
				CostPerInputToken:  0.01,
				CostPerOutputToken: 0.03,
			},
			selectedModel: "transformer-est-test-model",
			maxTokens:     new(500),
			checkAltCost:  true,
		},
		{
			name: "maxTokens of zero uses default",
			messages: []model.Message{
				{Role: "user", Content: "Hi"},
			},
			modelCfg: config.ModelConfig{
				CostPerInputToken:  0.01,
				CostPerOutputToken: 0.03,
			},
			selectedModel: "transformer-est-test-model",
			maxTokens:     new(0),
			checkAltCost:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			costEstimate, altCost, savingsPotential := computeCostEstimates(tt.messages, tt.modelCfg, tt.selectedModel, tt.maxTokens)

			if costEstimate < 0 {
				t.Errorf("costEstimate should be non-negative, got %f", costEstimate)
			}
			if tt.checkAltCost && altCost <= 0 {
				t.Errorf("altCost should be positive when cheaper alternatives exist, got %f", altCost)
			}
			if savingsPotential < 0 {
				t.Errorf("savingsPotential should be non-negative, got %f", savingsPotential)
			}
			if savingsPotential > 100 {
				t.Errorf("savingsPotential should not exceed 100, got %f", savingsPotential)
			}
			if altCost > 0 && costEstimate > 0 && altCost < costEstimate {
				if savingsPotential <= 0 {
					t.Errorf("savingsPotential should be > 0 when altCost < costEstimate, got %f", savingsPotential)
				}
			}
			// Verify 6-decimal rounding
			rounded := math.Round(costEstimate*1e6) / 1e6
			if costEstimate != rounded {
				t.Errorf("costEstimate %f is not rounded to 6 decimal places", costEstimate)
			}
		})
	}
}
