package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model"
)

func TestNewOllamaProvider(t *testing.T) {
	cfg := config.ProviderConfig{
		Name:    "test-ollama",
		Type:    "ollama",
		BaseURL: "http://localhost:11434",
	}
	p := NewOllamaProvider(cfg)
	require.NotNil(t, p)
	assert.Equal(t, "test-ollama", p.Name())
	assert.Equal(t, "ollama", p.Type())
}

func TestOllamaProvider_DefaultBaseURLRewrite(t *testing.T) {
	// When BaseURL is the default localhost, it should be rewritten to add /v1
	cfg := config.ProviderConfig{
		Name:    "test-ollama",
		Type:    "ollama",
		BaseURL: "http://localhost:11434",
	}
	p := NewOllamaProvider(cfg)
	// After construction, the internal openAI provider should have baseURL with /v1
	assert.Equal(t, "http://localhost:11434/v1", p.openAI.config.BaseURL)
	// But the ollama's own config retains the original
	assert.Equal(t, "http://localhost:11434", p.config.BaseURL)
}

func TestOllamaProvider_CustomBaseURL(t *testing.T) {
	cfg := config.ProviderConfig{
		Name:    "test-ollama",
		Type:    "ollama",
		BaseURL: "http://my-ollama:8080",
	}
	p := NewOllamaProvider(cfg)
	assert.Equal(t, "http://my-ollama:8080", p.openAI.config.BaseURL)
	assert.Equal(t, "http://my-ollama:8080", p.config.BaseURL)
}

func TestOllamaProvider_Client(t *testing.T) {
	cfg := config.ProviderConfig{
		Name:    "test-ollama",
		Type:    "ollama",
		BaseURL: "http://localhost:11434",
	}
	p := NewOllamaProvider(cfg)
	client := p.Client()
	require.NotNil(t, client)
	assert.Same(t, p.openAI.Client(), client)
}

// TestOllamaProvider_HealthCheck_OpenAICompat tests detection of the OpenAI-compatible mode.
func TestOllamaProvider_HealthCheck_OpenAICompat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": [{"id": "llama3"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "test-ollama",
		Type:    "ollama",
		BaseURL: server.URL,
	}
	p := NewOllamaProvider(cfg)

	// HealthCheck triggers detectMode, which should succeed with /v1/models
	err := p.HealthCheck(context.Background())
	assert.NoError(t, err)
	// After detection, useNative should be false (OpenAI-compatible mode)
	assert.False(t, p.useNative, "should detect OpenAI-compatible mode")
}

// TestOllamaProvider_HealthCheck_Native tests detection of the native Ollama mode.
func TestOllamaProvider_HealthCheck_Native(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			// Return error to force fallback to native detection
			w.WriteHeader(http.StatusNotFound)
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models": [{"name": "llama3"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "test-ollama",
		Type:    "ollama",
		BaseURL: server.URL,
	}
	p := NewOllamaProvider(cfg)

	err := p.HealthCheck(context.Background())
	assert.NoError(t, err)
	assert.True(t, p.useNative, "should detect native Ollama mode")
}

// TestOllamaProvider_HealthCheck_BothFail tests when both endpoints fail.
func TestOllamaProvider_HealthCheck_BothFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "test-ollama",
		Type:    "ollama",
		BaseURL: server.URL,
	}
	p := NewOllamaProvider(cfg)

	err := p.HealthCheck(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "both ollama endpoints failed")
}

// TestOllamaProvider_TransformRequest_OpenAICompat tests TransformRequest in OpenAI-compatible mode.
func TestOllamaProvider_TransformRequest_OpenAICompat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 200 OK for /v1/models to signal OpenAI-compatible mode
		if r.URL.Path == "/v1/models" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": []}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "test-ollama",
		Type:    "ollama",
		BaseURL: server.URL,
	}
	p := NewOllamaProvider(cfg)

	req := &model.ChatCompletionRequest{
		Model: "llama3",
		Messages: []model.Message{
			{Role: "user", Content: "hello"},
		},
	}

	httpReq, err := p.TransformRequest(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, httpReq)

	// In OpenAI-compatible mode, it should use the openAI provider's TransformRequest
	// and strip the Authorization header
	assert.Equal(t, "POST", httpReq.Method)
	assert.Equal(t, fmt.Sprintf("%s/chat/completions", server.URL), httpReq.URL.String())
	assert.Equal(t, "application/json", httpReq.Header.Get("Content-Type"))
	// Authorization should be removed for Ollama
	assert.Empty(t, httpReq.Header.Get("Authorization"))
}

// TestOllamaProvider_TransformRequest_Native tests TransformRequest in native Ollama mode.
func TestOllamaProvider_TransformRequest_Native(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.WriteHeader(http.StatusNotFound)
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models": [{"name": "llama3"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "test-ollama",
		Type:    "ollama",
		BaseURL: server.URL,
	}
	p := NewOllamaProvider(cfg)

	temp := 0.7
	topP := 0.9
	maxTokens := 2048
	req := &model.ChatCompletionRequest{
		Model: "llama3",
		Messages: []model.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi"},
		},
		Temperature:      &temp,
		TopP:             &topP,
		MaxTokens:        &maxTokens,
		Stop:             []string{"\n"},
		PresencePenalty:  &temp,
		FrequencyPenalty: &temp,
	}

	httpReq, err := p.TransformRequest(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, httpReq)

	assert.Equal(t, "POST", httpReq.Method)
	assert.Equal(t, fmt.Sprintf("%s/api/chat", server.URL), httpReq.URL.String())
	assert.Equal(t, "application/json", httpReq.Header.Get("Content-Type"))
	assert.Empty(t, httpReq.Header.Get("Authorization"))

	// Verify the body is a valid ollama native request
	var nativeReq ollamaNativeRequest
	err = json.NewDecoder(httpReq.Body).Decode(&nativeReq)
	require.NoError(t, err)
	assert.Equal(t, "llama3", nativeReq.Model)
	assert.Len(t, nativeReq.Messages, 2)
	assert.Equal(t, "user", nativeReq.Messages[0].Role)
	assert.Equal(t, "hello", nativeReq.Messages[0].Content)
	assert.Equal(t, "assistant", nativeReq.Messages[1].Role)
	assert.Equal(t, "hi", nativeReq.Messages[1].Content)
	require.NotNil(t, nativeReq.Options)
	assert.Equal(t, 0.7, *nativeReq.Options.Temperature)
	assert.Equal(t, 0.9, *nativeReq.Options.TopP)
	assert.Equal(t, 2048, *nativeReq.Options.NumPredict)
	assert.Equal(t, []string{"\n"}, nativeReq.Options.Stop)
}

// TestOllamaProvider_TransformResponse_Native tests TransformResponse in native mode.
func TestOllamaProvider_TransformResponse_Native(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.WriteHeader(http.StatusNotFound)
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models": [{"name": "llama3"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "test-ollama",
		Type:    "ollama",
		BaseURL: server.URL,
	}
	p := NewOllamaProvider(cfg)

	// Trigger detectMode first
	err := p.HealthCheck(context.Background())
	require.NoError(t, err)
	require.True(t, p.useNative)

	// Simulate an Ollama native response body
	respBody := `{
		"model": "llama3",
		"created_at": "2024-01-01T00:00:00Z",
		"message": {"role": "assistant", "content": "Hello from Ollama!"},
		"done": true,
		"prompt_eval_count": 10,
		"eval_count": 20
	}`

	// Use a test server to get a real response body
	bodyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(respBody))
	}))
	defer bodyServer.Close()

	httpReq, _ := http.NewRequestWithContext(context.Background(), "POST", bodyServer.URL, nil)
	realResp, err := http.DefaultClient.Do(httpReq)
	require.NoError(t, err)
	defer realResp.Body.Close()

	chatResp, err := p.TransformResponse(context.Background(), realResp)
	require.NoError(t, err)
	require.NotNil(t, chatResp)

	assert.Equal(t, "chat.completion", chatResp.Object)
	assert.Equal(t, "llama3", chatResp.Model)
	assert.Len(t, chatResp.Choices, 1)
	assert.Equal(t, "assistant", chatResp.Choices[0].Message.Role)
	assert.Equal(t, "Hello from Ollama!", chatResp.Choices[0].Message.Content)
	assert.Equal(t, "stop", chatResp.Choices[0].FinishReason)
	require.NotNil(t, chatResp.Usage)
	assert.Equal(t, 10, chatResp.Usage.PromptTokens)
	assert.Equal(t, 20, chatResp.Usage.CompletionTokens)
	assert.Equal(t, 30, chatResp.Usage.TotalTokens)
	assert.Contains(t, chatResp.ID, "ollama-")
}

// TestOllamaProvider_TransformResponse_OpenAICompat tests TransformResponse in OpenAI-compatible mode.
func TestOllamaProvider_TransformResponse_OpenAICompat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": []}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "test-ollama",
		Type:    "ollama",
		BaseURL: server.URL,
	}
	p := NewOllamaProvider(cfg)

	// Trigger detectMode first
	err := p.HealthCheck(context.Background())
	require.NoError(t, err)
	require.False(t, p.useNative)

	// In OpenAI-compatible mode, TransformResponse delegates to openAI provider
	bodyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-123",
			"object": "chat.completion",
			"created": 1677652288,
			"model": "llama3",
			"choices": [{"index": 0, "message": {"role": "assistant", "content": "Hello!"}, "finish_reason": "stop"}]
		}`))
	}))
	defer bodyServer.Close()

	httpReq, _ := http.NewRequestWithContext(context.Background(), "POST", bodyServer.URL, nil)
	resp, err := http.DefaultClient.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()

	chatResp, err := p.TransformResponse(context.Background(), resp)
	require.NoError(t, err)
	require.NotNil(t, chatResp)
	assert.Equal(t, "chatcmpl-123", chatResp.ID)
	assert.Equal(t, "Hello!", chatResp.Choices[0].Message.Content)
}

// TestOllamaProvider_TransformResponse_Error tests error handling in native mode.
func TestOllamaProvider_TransformResponse_NativeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.WriteHeader(http.StatusNotFound)
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models": [{"name": "llama3"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "test-ollama",
		Type:    "ollama",
		BaseURL: server.URL,
	}
	p := NewOllamaProvider(cfg)

	// Trigger detectMode
	err := p.HealthCheck(context.Background())
	require.NoError(t, err)
	require.True(t, p.useNative)

	// Create a mock error response
	errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "invalid model"}`))
	}))
	defer errServer.Close()

	httpReq, _ := http.NewRequestWithContext(context.Background(), "POST", errServer.URL, nil)
	resp, err := http.DefaultClient.Do(httpReq)
	require.NoError(t, err)
	defer resp.Body.Close()

	chatResp, err := p.TransformResponse(context.Background(), resp)
	assert.Error(t, err)
	assert.Nil(t, chatResp)
	assert.Contains(t, err.Error(), "ollama native returned")
}

// TestOllamaProvider_TransformStreamChunk_NativeDone tests native stream chunk with Done=true.
func TestOllamaProvider_TransformStreamChunk_NativeDone(t *testing.T) {
	// Set up the provider in native mode
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.WriteHeader(http.StatusNotFound)
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models": [{"name": "llama3"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "test-ollama",
		Type:    "ollama",
		BaseURL: server.URL,
	}
	p := NewOllamaProvider(cfg)

	// Trigger detectMode to set useNative=true
	_ = p.HealthCheck(context.Background())
	require.True(t, p.useNative)

	// Test with a done chunk
	data := []byte(`{"model":"llama3","created_at":"2024-01-01T00:00:00Z","message":{"role":"assistant","content":""},"done":true,"prompt_eval_count":10,"eval_count":20}`)
	chunk, done, err := p.TransformStreamChunk(data)
	assert.NoError(t, err)
	assert.True(t, done)
	require.NotNil(t, chunk)
	assert.Equal(t, "stop", *chunk.Choices[0].FinishReason)
}

// TestOllamaProvider_TransformStreamChunk_NativeContent tests native stream chunk with content.
func TestOllamaProvider_TransformStreamChunk_NativeContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.WriteHeader(http.StatusNotFound)
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models": [{"name": "llama3"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "test-ollama",
		Type:    "ollama",
		BaseURL: server.URL,
	}
	p := NewOllamaProvider(cfg)

	_ = p.HealthCheck(context.Background())
	require.True(t, p.useNative)

	data := []byte(`{"model":"llama3","created_at":"2024-01-01T00:00:00Z","message":{"role":"assistant","content":"Hello"},"done":false}`)
	chunk, done, err := p.TransformStreamChunk(data)
	assert.NoError(t, err)
	assert.False(t, done)
	require.NotNil(t, chunk)
	assert.Equal(t, "Hello", chunk.Choices[0].Delta.Content)
	assert.Nil(t, chunk.Choices[0].FinishReason)
}

// TestOllamaProvider_TransformStreamChunk_OpenAICompat tests stream chunk in OpenAI-compatible mode.
func TestOllamaProvider_TransformStreamChunk_OpenAICompat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": []}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "test-ollama",
		Type:    "ollama",
		BaseURL: server.URL,
	}
	p := NewOllamaProvider(cfg)

	_ = p.HealthCheck(context.Background())
	require.False(t, p.useNative)

	// In OpenAI-compat mode, should delegate to openAI's TransformStreamChunk
	chunk, done, err := p.TransformStreamChunk([]byte("[DONE]"))
	assert.NoError(t, err)
	assert.True(t, done)
	assert.Nil(t, chunk)
}

// TestOllamaProvider_TransformStreamChunk_NativeStripsDataPrefix tests stripping "data: " prefix.
func TestOllamaProvider_TransformStreamChunk_NativeStripsDataPrefix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.WriteHeader(http.StatusNotFound)
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models": [{"name": "llama3"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "test-ollama",
		Type:    "ollama",
		BaseURL: server.URL,
	}
	p := NewOllamaProvider(cfg)

	_ = p.HealthCheck(context.Background())
	require.True(t, p.useNative)

	// With "data: " prefix (as might be injected by proxy)
	data := []byte(`data: {"model":"llama3","created_at":"2024-01-01T00:00:00Z","message":{"role":"assistant","content":"Hello"},"done":false}`)
	chunk, done, err := p.TransformStreamChunk(data)
	assert.NoError(t, err)
	assert.False(t, done)
	require.NotNil(t, chunk)
	assert.Equal(t, "Hello", chunk.Choices[0].Delta.Content)
}

// TestOllamaProvider_TransformStreamChunk_BadChunk silently ignored
func TestOllamaProvider_TransformStreamChunk_BadChunk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.WriteHeader(http.StatusNotFound)
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models": [{"name": "llama3"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "test-ollama",
		Type:    "ollama",
		BaseURL: server.URL,
	}
	p := NewOllamaProvider(cfg)

	_ = p.HealthCheck(context.Background())
	require.True(t, p.useNative)

	// Bad JSON should be silently ignored (returns nil chunk, no error)
	chunk, done, err := p.TransformStreamChunk([]byte(`not json`))
	assert.NoError(t, err)
	assert.False(t, done)
	assert.Nil(t, chunk)
}

// TestOllamaProvider_DiscoverModels_OpenAICompat tests model discovery via OpenAI endpoint.
func TestOllamaProvider_DiscoverModels_OpenAICompat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			// detectMode hits this first
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": [{"id": "llama3"}, {"id": "mistral"}]}`))
		case "/models":
			// DiscoverModels on the internal openAI provider hits this
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": [{"id": "llama3"}, {"id": "mistral"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "test-ollama",
		Type:    "ollama",
		BaseURL: server.URL,
	}
	p := NewOllamaProvider(cfg)

	models, err := p.DiscoverModels(context.Background())
	require.NoError(t, err)
	require.NotEmpty(t, models)

	// Models should be mapped to ollama provider with zero cost
	for _, m := range models {
		assert.Equal(t, "ollama", m.Provider)
		assert.Equal(t, 0.0, m.CostPerInputToken)
		assert.Equal(t, 0.0, m.CostPerOutputToken)
		assert.Equal(t, "free", m.Tier)
	}
}

// TestOllamaProvider_DiscoverModels_Native tests model discovery via native tags endpoint.
func TestOllamaProvider_DiscoverModels_Native(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.WriteHeader(http.StatusNotFound)
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models": [{"name": "llama3:latest"}, {"name": "mistral:7b"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "test-ollama",
		Type:    "ollama",
		BaseURL: server.URL,
	}
	p := NewOllamaProvider(cfg)

	models, err := p.DiscoverModels(context.Background())
	require.NoError(t, err)
	require.Len(t, models, 2)

	assert.Equal(t, "llama3:latest", models[0].ID)
	assert.Equal(t, "ollama", models[0].Provider)
	assert.Equal(t, 0.0, models[0].CostPerInputToken)
	assert.Equal(t, "free", models[0].Tier)
	assert.Equal(t, 8192, models[0].MaxContextTokens)
}

// TestOllamaProvider_DiscoverModels_Error tests model discovery when server is unavailable.
func TestOllamaProvider_DiscoverModels_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "test-ollama",
		Type:    "ollama",
		BaseURL: server.URL,
	}
	p := NewOllamaProvider(cfg)

	models, err := p.DiscoverModels(context.Background())
	assert.Error(t, err)
	assert.Nil(t, models)
}

func TestOllamaProvider_DiscoverModels_FiltersEmbeddingModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.WriteHeader(http.StatusNotFound)
		case "/api/tags":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"models": [
				{"name": "llama3:latest", "details": {"family": "llama", "families": ["llama"]}},
				{"name": "nomic-embed-text:latest", "details": {"family": "nomic-bert", "families": ["nomic-bert"]}},
				{"name": "all-minilm:latest", "details": {"family": "bert", "families": ["bert"]}}
			]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "test-ollama",
		Type:    "ollama",
		BaseURL: server.URL,
	}
	p := NewOllamaProvider(cfg)

	models, err := p.DiscoverModels(context.Background())
	require.NoError(t, err)
	require.Len(t, models, 1)
	assert.Equal(t, "llama3:latest", models[0].ID)
}

func TestOllamaProvider_DiscoverModels_OpenAICompat_FiltersEmbeddingModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models", "/models":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data": [
				{"id": "llama3"},
				{"id": "nomic-embed-text"}
			]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "test-ollama",
		Type:    "ollama",
		BaseURL: server.URL,
	}
	p := NewOllamaProvider(cfg)

	models, err := p.DiscoverModels(context.Background())
	require.NoError(t, err)
	require.Len(t, models, 1)
	assert.Equal(t, "llama3", models[0].ID)
}
