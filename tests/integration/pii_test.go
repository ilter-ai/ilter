package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/middleware"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
	"github.com/ilter-ai/ilter/internal/provider"
	"github.com/ilter-ai/ilter/internal/proxy"
	testutil "github.com/ilter-ai/ilter/tests/utils"
)

func setupPIIE2E(t *testing.T, mode string, patterns ...string) *chi.Mux {
	t.Helper()

	piiCfg := config.PIIConfig{
		Enabled:  true,
		Mode:     mode,
		Patterns: patterns,
	}
	piiMw := middleware.NewPIIMaskerMiddleware(nil, piiCfg, nil, nil)

	r := chi.NewRouter()
	r.Use(piiMw.Handler)

	r.Post("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req model.ChatCompletionRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		content := ""
		if str, ok := req.Messages[0].Content.(string); ok {
			content = str
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := fmt.Sprintf(`{"choices":[{"message":{"content":%q}}],"usage":{"prompt_tokens":5,"completion_tokens":5}}`, content)
		_, _ = w.Write([]byte(resp))
	})

	r.Get("/v1/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	r.Post("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	return r
}

// piiTestCase represents a single PII masking test case.
type piiTestCase struct {
	name          string
	inputContent  string
	expectedMatch string // substring expected in masked content (empty means skip content check)
	wantBlocked   bool   // expect 422 blocked
}

// TestPIIE2E_MaskMode verifies that all PII types are replaced with <MASKED_PII>.
func TestPIIE2E_MaskMode(t *testing.T) {
	r := setupPIIE2E(t, "mask")

	tests := []piiTestCase{
		{
			name:          "Turkish name (Ahmet)",
			inputContent:  "Benim adım Ahmet.",
			expectedMatch: "<MASKED_PII>",
		},
		{
			name:          "English name (John)",
			inputContent:  "My name is John.",
			expectedMatch: "<MASKED_PII>",
		},
		{
			name:          "Credit Card",
			inputContent:  "Here is my card: 4321-0987-6543-2107 please use it.",
			expectedMatch: "<MASKED_PII>",
		},
		{
			name:          "TCKN",
			inputContent:  "My TC is 50882654334, thanks.",
			expectedMatch: "<MASKED_PII>",
		},
		{
			name:          "US SSN",
			inputContent:  "My SSN number is 123-45-6789.",
			expectedMatch: "<MASKED_PII>",
		},
		{
			name:          "Email",
			inputContent:  "Contact me at user@example.com for details.",
			expectedMatch: "<MASKED_PII>",
		},
		{
			name:          "Turkish Phone",
			inputContent:  "Call me at 05321234567 or +905321234567.",
			expectedMatch: "<MASKED_PII>",
		},
		{
			name:          "IPv4 Address",
			inputContent:  "Local server ip is 192.168.1.100.",
			expectedMatch: "<MASKED_PII>",
		},
		{
			name:          "No PII",
			inputContent:  "Hello world, how are you?",
			expectedMatch: "Hello world, how are you?",
		},
		{
			name:          "Multiple PII types",
			inputContent:  "Ben Ali, email test@example.com, kart 4321-0987-6543-2107",
			expectedMatch: "<MASKED_PII>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody := model.ChatCompletionRequest{
				Model: "gpt-mock",
				Messages: []model.Message{
					{Role: "user", Content: tt.inputContent},
				},
			}
			bodyBytes, _ := json.Marshal(reqBody)

			req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200 in mask mode, got %d: %s", rr.Code, rr.Body.String())
			}

			if tt.expectedMatch == "" {
				return
			}

			// Check that the response body contains expected content
			respBody := rr.Body.String()
			if !strings.Contains(respBody, tt.expectedMatch) {
				t.Errorf("expected response to contain %q, got: %s", tt.expectedMatch, respBody)
			}
		})
	}
}

// TestPIIE2E_ReversibleMode verifies reversible PII masking: placeholders in request, original values restored in response.
func TestPIIE2E_ReversibleMode(t *testing.T) {
	piiCfg := config.PIIConfig{
		Enabled: true,
		Mode:    "reversible",
	}
	piiMw := middleware.NewPIIMaskerMiddleware(nil, piiCfg, nil, nil)

	tests := []struct {
		name         string
		inputContent string
		// expectedPIIPlaceholderType is the PII type prefix (e.g. "NAMES", "EMAIL", "CREDIT_CARD")
		// that should appear as a placeholder prefix in the masked request
		expectedPIIPlaceholderType string
		// expectedOriginal is the original PII value that should be restored in the response
		expectedOriginal string
	}{
		{
			name:                       "English name",
			inputContent:               "My name is John.",
			expectedPIIPlaceholderType: ":NAMES:",
			expectedOriginal:           "John",
		},
		{
			name:                       "Turkish name",
			inputContent:               "Benim adım Ahmet.",
			expectedPIIPlaceholderType: ":NAMES:",
			expectedOriginal:           "Ahmet",
		},
		{
			name:                       "Email",
			inputContent:               "Contact me at user@example.com",
			expectedPIIPlaceholderType: ":EMAIL:",
			expectedOriginal:           "user@example.com",
		},
		{
			name:                       "Credit Card",
			inputContent:               "Card: 4321-0987-6543-2107",
			expectedPIIPlaceholderType: ":CREDIT_CARD:",
			expectedOriginal:           "4321-0987-6543-2107",
		},
		{
			name:                       "TCKN",
			inputContent:               "TCKN: 50882654334",
			expectedPIIPlaceholderType: ":TURKISH_ID:",
			expectedOriginal:           "50882654334",
		},
		{
			name:                       "Multiple PII types",
			inputContent:               "Ben Ali, email test@example.com, kart 4321-0987-6543-2107",
			expectedPIIPlaceholderType: ":NAMES:",
			expectedOriginal:           "Ali",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody := model.ChatCompletionRequest{
				Model: "gpt-mock",
				Messages: []model.Message{
					{Role: "user", Content: tt.inputContent},
				},
			}
			bodyBytes, _ := json.Marshal(reqBody)
			req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			var placeholder string

			// Wrap the next handler to capture the masked body and echo the placeholder back.
			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				var parsed model.ChatCompletionRequest
				if err := json.Unmarshal(body, &parsed); err != nil {
					t.Fatalf("Failed to parse masked body: %v", err)
				}
				content := parsed.Messages[0].Content.(string)

				// Find the placeholder
				for _, word := range strings.Fields(content) {
					if strings.HasPrefix(word, "PII:") {
						placeholder = strings.TrimRight(word, ".,;!")
						break
					}
				}

				if placeholder == "" {
					t.Fatalf("Expected placeholder with prefix PII%s in masked content, got: %q",
						tt.expectedPIIPlaceholderType, content)
				}
				if !strings.Contains(placeholder, tt.expectedPIIPlaceholderType) {
					t.Errorf("Expected placeholder to contain %q, got %q", tt.expectedPIIPlaceholderType, placeholder)
				}

				// Echo the placeholder back in response
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				resp := `{"choices":[{"message":{"content":"Your value is ` + placeholder + `"}}],"usage":{"prompt_tokens":5,"completion_tokens":5}}`
				_, _ = w.Write([]byte(resp))
			})

			rr := httptest.NewRecorder()
			// Create a fresh middleware and serve through it
			piiMw.Handler(nextHandler).ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
			}

			respBody := rr.Body.String()
			if strings.Contains(respBody, placeholder) {
				t.Errorf("Response should NOT contain placeholder %q, got: %s", placeholder, respBody)
			}
			if !strings.Contains(respBody, tt.expectedOriginal) {
				t.Errorf("Expected original value %q to be restored in response, got: %s",
					tt.expectedOriginal, respBody)
			}
		})
	}
}

// TestPIIE2E_BlockMode verifies that PII content is rejected with 422 in block mode.
func TestPIIE2E_BlockMode(t *testing.T) {
	r := setupPIIE2E(t, "block")

	tests := []piiTestCase{
		{
			name:         "Turkish name (Ahmet)",
			inputContent: "Benim adım Ahmet.",
			wantBlocked:  true,
		},
		{
			name:         "English name (John)",
			inputContent: "My name is John.",
			wantBlocked:  true,
		},
		{
			name:         "Credit Card",
			inputContent: "Card: 4321-0987-6543-2107",
			wantBlocked:  true,
		},
		{
			name:         "TCKN",
			inputContent: "TCKN: 50882654334",
			wantBlocked:  true,
		},
		{
			name:         "US SSN",
			inputContent: "SSN: 123-45-6789",
			wantBlocked:  true,
		},
		{
			name:         "Email",
			inputContent: "Email: test@example.com",
			wantBlocked:  true,
		},
		{
			name:         "Turkish Phone",
			inputContent: "Phone: 05321234567",
			wantBlocked:  true,
		},
		{
			name:         "IPv4",
			inputContent: "Server: 192.168.1.100",
			wantBlocked:  true,
		},
		{
			name:         "Multiple PII types",
			inputContent: "Ben Ali, email test@example.com, kart 4321-0987-6543-2107",
			wantBlocked:  true,
		},
		{
			name:         "No PII (passes through)",
			inputContent: "Hello world, how are you?",
			wantBlocked:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody := model.ChatCompletionRequest{
				Model: "gpt-mock",
				Messages: []model.Message{
					{Role: "user", Content: tt.inputContent},
				},
			}
			bodyBytes, _ := json.Marshal(reqBody)

			req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)

			if tt.wantBlocked {
				if rr.Code != http.StatusUnprocessableEntity {
					t.Fatalf("expected 422 for blocked PII, got %d: %s", rr.Code, rr.Body.String())
				}
				var errResp map[string]map[string]interface{}
				if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
					t.Fatalf("Failed to parse error response: %v", err)
				}
				code, _ := errResp["error"]["code"].(string)
				if code != "pii_blocked" {
					t.Errorf("expected error code 'pii_blocked', got %q", code)
				}
			} else {
				if rr.Code != http.StatusOK {
					t.Fatalf("expected 200 for non-PII content, got %d: %s", rr.Code, rr.Body.String())
				}
			}
		})
	}
}

// TestPIIE2E_ReversibleResponseUnmask verifies that the full response flow unmask works end-to-end
// through the chi router middleware chain.
func TestPIIE2E_ReversibleResponseUnmask(t *testing.T) {
	piiCfg := config.PIIConfig{
		Enabled: true,
		Mode:    "reversible",
	}
	piiMw := middleware.NewPIIMaskerMiddleware(nil, piiCfg, nil, nil)

	tests := []struct {
		name           string
		inputContent   string
		echoContent    string // what the downstream handler echoes with placeholder
		expectedInResp string // what should appear in the final response
	}{
		{
			name:           "Email unmasked in response",
			inputContent:   "my email is user@example.com",
			echoContent:    "Your email is %s",
			expectedInResp: "user@example.com",
		},
		{
			name:           "Name unmasked in response",
			inputContent:   "My name is John.",
			echoContent:    "Hello %s!",
			expectedInResp: "John",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody := model.ChatCompletionRequest{
				Model: "gpt-mock",
				Messages: []model.Message{
					{Role: "user", Content: tt.inputContent},
				},
			}
			bodyBytes, _ := json.Marshal(reqBody)
			req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			var placeholder string

			nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				var parsed model.ChatCompletionRequest
				if err := json.Unmarshal(body, &parsed); err != nil {
					t.Fatalf("Failed to parse masked body: %v", err)
				}
				content := parsed.Messages[0].Content.(string)

				for _, word := range strings.Fields(content) {
					if strings.HasPrefix(word, "PII:") {
						placeholder = strings.TrimRight(word, ".,;!")
						break
					}
				}
				if placeholder == "" {
					t.Fatalf("Expected placeholder in masked content, got: %q", content)
				}

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				msg := tt.echoContent
				if strings.Contains(msg, "%s") {
					msg = strings.ReplaceAll(msg, "%s", placeholder)
				}
				resp := `{"choices":[{"message":{"content":"` + msg + `"}}],"usage":{"prompt_tokens":5,"completion_tokens":5}}`
				_, _ = w.Write([]byte(resp))
			})

			rr := httptest.NewRecorder()
			piiMw.Handler(nextHandler).ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
			}

			respBody := rr.Body.String()
			if strings.Contains(respBody, "PII:") {
				t.Errorf("Response should NOT contain any placeholder, got: %s", respBody)
			}
			if !strings.Contains(respBody, tt.expectedInResp) {
				t.Errorf("Expected %q in response, got: %s", tt.expectedInResp, respBody)
			}
		})
	}
}

// TestPIIE2E_NonPOSTPaths verifies that non-POST and non-/v1/chat/completions paths bypass PII masking.
func TestPIIE2E_NonPOSTPaths(t *testing.T) {
	r := setupPIIE2E(t, "mask")

	t.Run("GET request bypasses PII", func(t *testing.T) {
		req, _ := http.NewRequestWithContext(context.Background(), "GET", "/v1/chat/completions", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 for GET, got %d", rr.Code)
		}
	})

	t.Run("non-completions path bypasses PII", func(t *testing.T) {
		req, _ := http.NewRequestWithContext(context.Background(), "POST", "/v1/models", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("expected 200 for non-completions path, got %d", rr.Code)
		}
	})
}

type piiRoundTripFunc func(req *http.Request) (*http.Response, error)

func (f piiRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type mockCombinationsProvider struct {
	name             string
	responseTemplate func(placeholder string) string
	lastReqBody      []byte
}

func (p *mockCombinationsProvider) Name() string { return p.name }
func (p *mockCombinationsProvider) Type() string { return "openai" }
func (p *mockCombinationsProvider) TransformRequest(ctx context.Context, req *model.ChatCompletionRequest) (*http.Request, error) {
	body, _ := json.Marshal(req)
	return http.NewRequestWithContext(ctx, "POST", "http://mock-openai/v1/chat/completions", bytes.NewReader(body))
}

func (p *mockCombinationsProvider) TransformResponse(_ context.Context, resp *http.Response) (*model.ChatCompletionResponse, error) {
	var openAIResp model.ChatCompletionResponse
	body, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(body, &openAIResp)
	return &openAIResp, nil
}
func (p *mockCombinationsProvider) HealthCheck(_ context.Context) error { return nil }
func (p *mockCombinationsProvider) DiscoverModels(_ context.Context) ([]catalog.ModelInfo, error) {
	return nil, nil
}

func (p *mockCombinationsProvider) Client() *http.Client {
	return &http.Client{
		Transport: piiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			bodyBytes, _ := io.ReadAll(req.Body)
			p.lastReqBody = bodyBytes

			var parsedReq model.ChatCompletionRequest
			_ = json.Unmarshal(bodyBytes, &parsedReq)

			content := parsedReq.Messages[0].Content.(string)

			// Find the placeholder in the content (starts with "PII:")
			placeholder := ""
			words := strings.Fields(content)
			for _, w := range words {
				w = strings.Trim(w, ".,!?<>")
				if strings.HasPrefix(strings.ToUpper(w), "PII:") {
					placeholder = w
					break
				}
			}

			responseText := p.responseTemplate(placeholder)

			respData := model.ChatCompletionResponse{
				ID:      "chatcmpl-123",
				Object:  "chat.completion",
				Created: 1677652288,
				Model:   "gpt-mock",
				Choices: []model.Choice{
					{
						Index: 0,
						Message: model.ChoiceMessage{
							Role:    "assistant",
							Content: responseText,
						},
						FinishReason: "stop",
					},
				},
				Usage: &model.Usage{
					PromptTokens:     10,
					CompletionTokens: 15,
					TotalTokens:      25,
				},
			}
			respBytes, _ := json.Marshal(respData)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewReader(respBytes)),
			}, nil
		}),
	}
}

func TestPIIUnmaskCombinations(t *testing.T) {
	tests := []struct {
		name             string
		prompt           string
		originalValue    string
		responseTemplate func(placeholder string) string
		expectedContains string
	}{
		{
			name:          "Combination 1: Exact placeholder case",
			prompt:        "My email is user@example.com",
			originalValue: "user@example.com",
			responseTemplate: func(placeholder string) string {
				return "Your email is " + placeholder
			},
			expectedContains: "user@example.com",
		},
		{
			name:          "Combination 2: Lowercase prefix and type",
			prompt:        "My email is user@example.com",
			originalValue: "user@example.com",
			responseTemplate: func(placeholder string) string {
				return "Your email is " + strings.ToLower(placeholder)
			},
			expectedContains: "user@example.com",
		},
		{
			name:          "Combination 3: Mixed case format Pii:Email:hash",
			prompt:        "My email is user@example.com",
			originalValue: "user@example.com",
			responseTemplate: func(placeholder string) string {
				parts := strings.Split(placeholder, ":")
				if len(parts) == 3 {
					return "Your email is Pii:Email:" + parts[2]
				}
				return "Your email is " + placeholder
			},
			expectedContains: "user@example.com",
		},
		{
			name:          "Combination 4: Underscore separated lowercase",
			prompt:        "My email is user@example.com",
			originalValue: "user@example.com",
			responseTemplate: func(placeholder string) string {
				underscored := strings.ReplaceAll(placeholder, ":", "_")
				return "Your email is " + strings.ToLower(underscored)
			},
			expectedContains: "user@example.com",
		},
		{
			name:          "Combination 5: Underscore separated mixed case",
			prompt:        "My email is user@example.com",
			originalValue: "user@example.com",
			responseTemplate: func(placeholder string) string {
				parts := strings.Split(placeholder, ":")
				if len(parts) == 3 {
					return "Your email is Pii_Email_" + parts[2]
				}
				return "Your email is " + placeholder
			},
			expectedContains: "user@example.com",
		},
		{
			name:          "Combination 6: Enclosed in angle brackets <pii:email:hash>",
			prompt:        "My email is user@example.com",
			originalValue: "user@example.com",
			responseTemplate: func(placeholder string) string {
				return "Your email is <" + strings.ToLower(placeholder) + ">"
			},
			expectedContains: "user@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize LoadBalancer and modelregistry
			cfg := &config.Config{
				Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
				Providers: []config.ProviderConfig{
					{
						Name: "mock-openai-combinations",
						Type: "openai",
						Models: []config.ModelConfig{
							{Name: "gpt-mock", Weight: 1},
						},
					},
				},
				PII: config.PIIConfig{
					Enabled:  true,
					Mode:     "reversible",
					Patterns: []string{"email"},
				},
			}

			reg := provider.NewRegistry()
			prov := &mockCombinationsProvider{
				name:             "mock-openai-combinations",
				responseTemplate: tt.responseTemplate,
			}
			reg.Register(prov)

			lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
			if err != nil {
				t.Fatalf("Failed to create load balancer: %v", err)
			}

			authMw := middleware.NewAuthMiddleware(cfg.Auth, nil)
			piiMasker := middleware.NewPIIMaskerMiddleware(nil, cfg.PII, nil, nil)
			proxyHandler := proxy.NewHandler(lb, nil, nil, nil)

			r := chi.NewRouter()
			r.Use(authMw.Handler)
			r.Use(piiMasker.Handler)
			r.Post("/v1/chat/completions", proxyHandler.ChatCompletions)

			// Spawn local test HTTP server to simulate the API endpoint over HTTP client/server socket connection
			ts := httptest.NewServer(r)
			defer ts.Close()

			// Prepare request
			reqBody := model.ChatCompletionRequest{
				Model:    "gpt-mock",
				Messages: []model.Message{{Role: "user", Content: tt.prompt}},
			}
			reqBytes, _ := json.Marshal(reqBody)

			req, err := http.NewRequestWithContext(context.Background(), "POST", ts.URL+"/v1/chat/completions", bytes.NewReader(reqBytes))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			req.Header.Set("Authorization", "Bearer "+testutil.AdminKey)
			req.Header.Set("Content-Type", "application/json")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("HTTP request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				respBody, _ := io.ReadAll(resp.Body)
				t.Fatalf("Expected status 200, got %d. Body: %s", resp.StatusCode, respBody)
			}

			var chatResp model.ChatCompletionResponse
			err = json.NewDecoder(resp.Body).Decode(&chatResp)
			if err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			content := chatResp.Choices[0].Message.Content
			if !strings.Contains(content, tt.expectedContains) {
				t.Errorf("Expected content to contain %q, but got %q", tt.expectedContains, content)
			}

			// Verify masking happened at provider level
			var maskedReq model.ChatCompletionRequest
			_ = json.Unmarshal(prov.lastReqBody, &maskedReq)
			maskedPrompt := maskedReq.Messages[0].Content.(string)
			if strings.Contains(maskedPrompt, tt.originalValue) {
				t.Errorf("PII was not masked in request to provider: %s", maskedPrompt)
			}
		})
	}
}
