package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/circuitbreaker"
	"github.com/ilter-ai/ilter/internal/model"
)

func TestNewOpenAIProvider(t *testing.T) {
	cfg := config.ProviderConfig{
		Name:   "test-openai",
		Type:   "openai",
		APIKey: "sk-test",
	}
	p := NewOpenAIProvider(cfg)
	require.NotNil(t, p)
	assert.Equal(t, "test-openai", p.Name())
	assert.Equal(t, "openai", p.Type())
}

func TestOpenAIProvider_DefaultType(t *testing.T) {
	// When provType is empty, Type() should return "openai"
	cfg := config.ProviderConfig{
		Name: "no-type",
	}
	p := NewOpenAIProvider(cfg)
	assert.Equal(t, "openai", p.Type())
}

func TestOpenAIProvider_HealthCheck_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/models", r.URL.Path)
		assert.Equal(t, "Bearer sk-test", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data": [{"id": "gpt-4"}]}`))
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "test-openai",
		Type:    "openai",
		BaseURL: server.URL,
		APIKey:  "sk-test",
	}
	p := NewOpenAIProvider(cfg)
	err := p.HealthCheck(context.Background())
	assert.NoError(t, err)
}

func TestOpenAIProvider_HealthCheck_Failure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "test-openai",
		Type:    "openai",
		BaseURL: server.URL,
		APIKey:  "sk-bad",
	}
	p := NewOpenAIProvider(cfg)
	err := p.HealthCheck(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestOpenAIProvider_HealthCheck_ConnectionError(t *testing.T) {
	// Use a non-routable address to trigger a connection error
	cfg := config.ProviderConfig{
		Name:    "test-openai",
		Type:    "openai",
		BaseURL: "http://127.0.0.1:1",
		APIKey:  "sk-test",
	}
	p := NewOpenAIProvider(cfg)
	err := p.HealthCheck(context.Background())
	assert.Error(t, err)
}

func TestOpenAIProvider_TransformRequest(t *testing.T) {
	cfg := config.ProviderConfig{
		Name:    "test-openai",
		Type:    "openai",
		BaseURL: "https://api.openai.com/v1",
		APIKey:  "sk-test",
	}
	p := NewOpenAIProvider(cfg)

	req := &model.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []model.Message{
			{Role: "user", Content: "hello"},
		},
		Stream: true,
	}

	httpReq, err := p.TransformRequest(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, httpReq)

	assert.Equal(t, "https://api.openai.com/v1/chat/completions", httpReq.URL.String())
	assert.Equal(t, "POST", httpReq.Method)
	assert.Equal(t, "Bearer sk-test", httpReq.Header.Get("Authorization"))
	assert.Equal(t, "application/json", httpReq.Header.Get("Content-Type"))

	// Verify body is correctly serialized
	var body model.ChatCompletionRequest
	err = json.NewDecoder(httpReq.Body).Decode(&body)
	require.NoError(t, err)
	assert.Equal(t, "gpt-4", body.Model)
	assert.Len(t, body.Messages, 1)
	assert.Equal(t, "user", body.Messages[0].Role)
	assert.Equal(t, "hello", body.Messages[0].Content)
	assert.True(t, body.Stream)
}

func TestOpenAIProvider_TransformRequest_OpenCodeZenUsesChatCompletions(t *testing.T) {
	cfg := config.ProviderConfig{
		Name:    "test-opencode-zen",
		Type:    "opencode_zen",
		BaseURL: "https://opencode.ai/zen/v1",
		APIKey:  "sk-test",
	}
	p := NewOpenAIProvider(cfg)

	req := &model.ChatCompletionRequest{
		Model:    "opencode/gpt-5.5",
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	}

	httpReq, err := p.TransformRequest(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "https://opencode.ai/zen/v1/chat/completions", httpReq.URL.String())
}

func TestOpenAIProvider_TransformRequest_WithCustomHeaders(t *testing.T) {
	cfg := config.ProviderConfig{
		Name:    "test-openai",
		Type:    "openai",
		BaseURL: "https://api.openai.com/v1",
		APIKey:  "sk-test",
		Headers: map[string]string{
			"X-Custom": "custom-value",
		},
	}
	p := NewOpenAIProvider(cfg)

	req := &model.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []model.Message{
			{Role: "user", Content: "hello"},
		},
	}

	httpReq, err := p.TransformRequest(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "custom-value", httpReq.Header.Get("X-Custom"))
}

func TestOpenRouterProvider_TransformRequest_SetsHeaders(t *testing.T) {
	cfg := config.ProviderConfig{
		Name:    "test-openrouter",
		Type:    "openrouter",
		BaseURL: "https://openrouter.ai/api/v1",
		APIKey:  "sk-test",
	}
	p := NewOpenRouterProvider(cfg)

	req := &model.ChatCompletionRequest{
		Model: "gpt-4",
		Messages: []model.Message{
			{Role: "user", Content: "hello"},
		},
	}

	httpReq, err := p.TransformRequest(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/ilter-ai/ilter", httpReq.Header.Get("HTTP-Referer"))
	assert.Equal(t, "ILTER Gateway", httpReq.Header.Get("X-Title"))
}

func TestOpenAIProvider_TransformRequest_NilMessages(t *testing.T) {
	cfg := config.ProviderConfig{
		Name:    "test-openai",
		Type:    "openai",
		BaseURL: "https://api.openai.com/v1",
		APIKey:  "sk-test",
	}
	p := NewOpenAIProvider(cfg)

	req := &model.ChatCompletionRequest{
		Model: "gpt-4",
	}

	httpReq, err := p.TransformRequest(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, httpReq)
	assert.Equal(t, "Bearer sk-test", httpReq.Header.Get("Authorization"))
}

func TestOpenAIProvider_TransformResponse_Success(t *testing.T) {
	responseJSON := `{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"created": 1677652288,
		"model": "gpt-4",
		"choices": [
			{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "Hello! How can I help you today?"
				},
				"finish_reason": "stop"
			}
		],
		"usage": {
			"prompt_tokens": 9,
			"completion_tokens": 12,
			"total_tokens": 21
		}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(responseJSON))
	}))
	defer server.Close()

	httpReq, _ := http.NewRequestWithContext(context.Background(), "POST", server.URL, nil)
	resp, err := http.DefaultClient.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()

	cfg := config.ProviderConfig{
		Name:   "test-openai",
		Type:   "openai",
		APIKey: "sk-test",
	}
	p := NewOpenAIProvider(cfg)
	chatResp, err := p.TransformResponse(context.Background(), resp)
	require.NoError(t, err)
	require.NotNil(t, chatResp)

	assert.Equal(t, "chatcmpl-123", chatResp.ID)
	assert.Equal(t, "chat.completion", chatResp.Object)
	assert.Equal(t, "gpt-4", chatResp.Model)
	assert.Len(t, chatResp.Choices, 1)
	assert.Equal(t, "assistant", chatResp.Choices[0].Message.Role)
	assert.Equal(t, "Hello! How can I help you today?", chatResp.Choices[0].Message.Content)
	assert.Equal(t, "stop", chatResp.Choices[0].FinishReason)
	require.NotNil(t, chatResp.Usage)
	assert.Equal(t, 9, chatResp.Usage.PromptTokens)
	assert.Equal(t, 12, chatResp.Usage.CompletionTokens)
	assert.Equal(t, 21, chatResp.Usage.TotalTokens)
}

func TestOpenAIProvider_TransformResponse_MultipleChoices(t *testing.T) {
	responseJSON := `{
		"id": "chatcmpl-456",
		"object": "chat.completion",
		"created": 1677652289,
		"model": "gpt-4",
		"choices": [
			{
				"index": 0,
				"message": {"role": "assistant", "content": "First response"},
				"finish_reason": "stop"
			},
			{
				"index": 1,
				"message": {"role": "assistant", "content": "Second response"},
				"finish_reason": "stop"
			}
		]
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(responseJSON))
	}))
	defer server.Close()

	httpReq, _ := http.NewRequestWithContext(context.Background(), "POST", server.URL, nil)
	resp, err := http.DefaultClient.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()

	p := NewOpenAIProvider(config.ProviderConfig{})
	chatResp, err := p.TransformResponse(context.Background(), resp)
	require.NoError(t, err)
	assert.Len(t, chatResp.Choices, 2)
	assert.Equal(t, "First response", chatResp.Choices[0].Message.Content)
	assert.Equal(t, "Second response", chatResp.Choices[1].Message.Content)
}

func TestOpenAIProvider_TransformResponse_ErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": {"message": "Bad Request", "type": "invalid_request_error"}}`))
	}))
	defer server.Close()

	httpReq, _ := http.NewRequestWithContext(context.Background(), "POST", server.URL, nil)
	resp, err := http.DefaultClient.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()

	p := NewOpenAIProvider(config.ProviderConfig{})
	chatResp, err := p.TransformResponse(context.Background(), resp)
	assert.Error(t, err)
	assert.Nil(t, chatResp)
	assert.Contains(t, err.Error(), "400")
	assert.Contains(t, err.Error(), "Bad Request")
}

func TestOpenAIProvider_TransformResponse_ErrorEnvelopeWith200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"error":{"message":"Bad Request","type":"invalid_request_error","code":400}}`))
	}))
	defer server.Close()

	httpReq, _ := http.NewRequestWithContext(context.Background(), "POST", server.URL, nil)
	resp, err := http.DefaultClient.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()

	p := NewOpenAIProvider(config.ProviderConfig{})
	chatResp, err := p.TransformResponse(context.Background(), resp)
	require.Error(t, err)
	assert.Nil(t, chatResp)
	assert.Contains(t, err.Error(), "provider returned error: Bad Request")
}

func TestOpenAIProvider_TransformResponse_EmptyChoiceContentWith200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"test","object":"chat.completion","created":1,"model":"gpt-4","choices":[{"index":0,"message":{"role":"assistant","content":""},"finish_reason":""}]}`))
	}))
	defer server.Close()

	httpReq, _ := http.NewRequestWithContext(context.Background(), "POST", server.URL, nil)
	resp, err := http.DefaultClient.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()

	p := NewOpenAIProvider(config.ProviderConfig{})
	chatResp, err := p.TransformResponse(context.Background(), resp)
	require.Error(t, err)
	assert.Nil(t, chatResp)
	assert.Contains(t, err.Error(), "empty choice content")
}

func TestOpenAIProvider_TransformResponse_EmptyChoicesWith200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl-empty","object":"chat.completion","created":1,"model":"gpt-4","choices":[]}`))
	}))
	defer server.Close()

	httpReq, _ := http.NewRequestWithContext(context.Background(), "POST", server.URL, nil)
	resp, err := http.DefaultClient.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()

	p := NewOpenAIProvider(config.ProviderConfig{})
	chatResp, err := p.TransformResponse(context.Background(), resp)
	require.Error(t, err)
	assert.Nil(t, chatResp)
	assert.Contains(t, err.Error(), "no choices")
}

func TestOpenAIProvider_TransformResponse_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{invalid json`))
	}))
	defer server.Close()

	httpReq, _ := http.NewRequestWithContext(context.Background(), "POST", server.URL, nil)
	resp, err := http.DefaultClient.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()

	p := NewOpenAIProvider(config.ProviderConfig{})
	chatResp, err := p.TransformResponse(context.Background(), resp)
	assert.Error(t, err)
	assert.Nil(t, chatResp)
	assert.Contains(t, err.Error(), "failed to decode response")
}

func TestOpenAIProvider_TransformStreamChunk_Done(t *testing.T) {
	p := NewOpenAIProvider(config.ProviderConfig{})
	chunk, done, err := p.TransformStreamChunk([]byte("[DONE]"))
	assert.NoError(t, err)
	assert.True(t, done)
	assert.Nil(t, chunk)
}

func TestOpenAIProvider_TransformStreamChunk_Success(t *testing.T) {
	data := `{"id":"chunk123","object":"chat.completion.chunk","created":1677652288,"model":"gpt-4","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`

	p := NewOpenAIProvider(config.ProviderConfig{})
	chunk, done, err := p.TransformStreamChunk([]byte(data))
	assert.NoError(t, err)
	assert.False(t, done)
	require.NotNil(t, chunk)
	assert.Equal(t, "chunk123", chunk.ID)
	assert.Equal(t, "Hello", chunk.Choices[0].Delta.Content)
}

func TestOpenAIProvider_TransformStreamChunk_InvalidJSON(t *testing.T) {
	p := NewOpenAIProvider(config.ProviderConfig{})
	chunk, done, err := p.TransformStreamChunk([]byte("{invalid}"))
	assert.Error(t, err)
	assert.False(t, done)
	assert.Nil(t, chunk)
}

func TestOpenAIProvider_Client(t *testing.T) {
	cfg := config.ProviderConfig{
		Name: "test-openai",
	}
	p := NewOpenAIProvider(cfg)
	client := p.Client()
	require.NotNil(t, client)
	// Verify the client has a transport (circuit breaker wrapped)
	_, ok := client.Transport.(*circuitbreaker.HTTPBreaker)
	assert.True(t, ok, "client transport should be a circuit breaker Transport")
}

// TestOpenAIProvider_DiscoverModels_Success tests the DiscoverModels method
func TestOpenAIProvider_DiscoverModels_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data": [
				{"id": "gpt-4"},
				{"id": "gpt-3.5-turbo"},
				{"id": "gpt-4-turbo"}
			]
		}`))
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "test-openai",
		Type:    "openai",
		BaseURL: server.URL,
		APIKey:  "sk-test",
	}
	p := NewOpenAIProvider(cfg)

	models, err := p.DiscoverModels(context.Background())
	require.NoError(t, err)
	require.Len(t, models, 3)

	// Verify model IDs
	ids := make(map[string]bool)
	for _, m := range models {
		ids[m.ID] = true
	}
	assert.True(t, ids["gpt-4"])
	assert.True(t, ids["gpt-3.5-turbo"])
	assert.True(t, ids["gpt-4-turbo"])

	// Verify openai-specific heuristics
	for _, m := range models {
		if strings.Contains(m.ID, "gpt-4") {
			assert.Equal(t, "openai", m.Provider)
			assert.NotEmpty(t, m.Tier)
			assert.Contains(t, m.Capabilities, "vision")
		}
	}
}

func TestOpenAIProvider_DiscoverModels_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data": []}`))
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "test-openai",
		Type:    "openai",
		BaseURL: server.URL,
		APIKey:  "sk-test",
	}
	p := NewOpenAIProvider(cfg)

	models, err := p.DiscoverModels(context.Background())
	require.NoError(t, err)
	assert.Empty(t, models)
}

func TestOpenAIProvider_DiscoverModels_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "test-openai",
		Type:    "openai",
		BaseURL: server.URL,
		APIKey:  "sk-test",
	}
	p := NewOpenAIProvider(cfg)

	models, err := p.DiscoverModels(context.Background())
	assert.Error(t, err)
	assert.Nil(t, models)
}

// TestOpenAIProvider_ModelHeuristics tests the discoverModelHeuristics helper
func TestOpenAIProvider_ModelHeuristics(t *testing.T) {
	p := NewOpenAIProvider(config.ProviderConfig{Type: "openai"})

	tests := []struct {
		modelID  string
		wantTier string
	}{
		{"gpt-4o-mini", "economy"},
		{"gpt-4o", "standard"},        // gpt-4 prefix → standard
		{"gpt-5", "standard"},         // gpt-5 prefix → standard
		{"gpt-3.5-turbo", "standard"}, // default standard
		{"unknown-model", "standard"}, // default
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			tier, _, _, _, _, _ := p.discoverModelHeuristics(tt.modelID)
			assert.Equal(t, tt.wantTier, tier)
		})
	}
}

// TestOpenAIProvider_ModelHeuristics_DeepSeek tests deepseek model heuristics
func TestOpenAIProvider_ModelHeuristics_DeepSeek(t *testing.T) {
	p := NewOpenAIProvider(config.ProviderConfig{Type: "deepseek"})

	tier, _, _, maxCtx, _, _ := p.discoverModelHeuristics("deepseek-reasoner")
	assert.Equal(t, "standard", tier)
	assert.Equal(t, 64000, maxCtx)

	tier, costIn, _, _, _, _ := p.discoverModelHeuristics("deepseek-chat")
	assert.Equal(t, "economy", tier)
	assert.Equal(t, 0.00000014, costIn)
}

// TestOpenAIProvider_ModelHeuristics_Qwen tests qwen model heuristics
func TestOpenAIProvider_ModelHeuristics_Qwen(t *testing.T) {
	p := NewOpenAIProvider(config.ProviderConfig{Type: "qwen"})

	tier, _, _, _, _, _ := p.discoverModelHeuristics("qwen-turbo")
	assert.Equal(t, "economy", tier)

	tier, _, _, _, _, _ = p.discoverModelHeuristics("qwen-plus")
	assert.Equal(t, "standard", tier)

	tier, _, _, _, _, _ = p.discoverModelHeuristics("qwen-max")
	assert.Equal(t, "premium", tier)
}

// TestOpenAIProvider_ModelHeuristics_Gemini tests gemini model heuristics
func TestOpenAIProvider_ModelHeuristics_Gemini(t *testing.T) {
	p := NewOpenAIProvider(config.ProviderConfig{Type: "gemini"})

	tier, _, _, maxCtx, _, _ := p.discoverModelHeuristics("gemini-2.0-flash-lite")
	assert.Equal(t, "economy", tier)
	assert.Equal(t, 1048576, maxCtx)

	tier, _, _, _, _, _ = p.discoverModelHeuristics("gemini-2.0-flash")
	assert.Equal(t, "economy", tier)

	tier, _, _, _, _, _ = p.discoverModelHeuristics("gemini-2.0-pro")
	assert.Equal(t, "premium", tier)
}
