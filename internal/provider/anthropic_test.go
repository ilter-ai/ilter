package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model/catalog"
)

func TestAnthropicProvider_DiscoverModels_Success(t *testing.T) {
	// Setup mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("x-api-key") != "test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"data": [
				{"type": "model", "id": "claude-3-5-sonnet-20241022", "display_name": "Claude 3.5 Sonnet"},
				{"type": "model", "id": "claude-3-opus-20240229", "display_name": "Claude 3 Opus"}
			],
			"has_more": false
		}`))
	}))
	defer server.Close()

	cfg := config.ProviderConfig{
		Name:    "anthropic",
		Type:    "anthropic",
		BaseURL: server.URL,
		APIKey:  "test-key",
	}
	p := NewAnthropicProvider(cfg)

	models, err := p.DiscoverModels(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}

	sonnetFound := false
	opusFound := false
	for _, m := range models {
		if m.ID == "claude-3-5-sonnet-20241022" {
			sonnetFound = true
			if m.Tier != "standard" {
				t.Errorf("expected standard tier for sonnet, got %s", m.Tier)
			}
			if m.CostPerInputToken != 0.000003 {
				t.Errorf("expected 0.000003 input cost for sonnet, got %f", m.CostPerInputToken)
			}
		}
		if m.ID == "claude-3-opus-20240229" {
			opusFound = true
			if m.Tier != "premium" {
				t.Errorf("expected premium tier for opus, got %s", m.Tier)
			}
			if m.CostPerInputToken != 0.000015 {
				t.Errorf("expected 0.000015 input cost for opus, got %f", m.CostPerInputToken)
			}
		}
	}

	if !sonnetFound || !opusFound {
		t.Errorf("did not find expected sonnet or opus models, got: %+v", models)
	}
}

func TestAnthropicProvider_DiscoverModels_Fallback(t *testing.T) {
	// 1. Setup mock server that returns an error status
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	// Ensure registry has some Anthropic models loaded
	catalog.ModelsMu.Lock()
	catalog.Models["claude-fallback-test-sonnet"] = []catalog.ModelInfo{{
		ID:       "claude-fallback-test-sonnet",
		Provider: "anthropic",
	}}
	catalog.ModelsMu.Unlock()

	defer func() {
		catalog.ModelsMu.Lock()
		delete(catalog.Models, "claude-fallback-test-sonnet")
		catalog.ModelsMu.Unlock()
	}()

	cfg := config.ProviderConfig{
		Name:    "anthropic",
		Type:    "anthropic",
		BaseURL: server.URL,
		APIKey:  "bad-key",
	}
	p := NewAnthropicProvider(cfg)

	models, err := p.DiscoverModels(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// We expect fallback models to be returned, which should include claude-fallback-test-sonnet
	found := false
	for _, m := range models {
		if m.ID == "claude-fallback-test-sonnet" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected fallback models to include 'claude-fallback-test-sonnet', got %v", models)
	}
}

func TestAnthropicProvider_TransformStreamChunk_TextDelta(t *testing.T) {
	p := NewAnthropicProvider(config.ProviderConfig{Name: "anthropic", Type: "anthropic"})

	chunk, done, err := p.TransformStreamChunk([]byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if done {
		t.Errorf("expected done=false for text delta")
	}
	if chunk == nil {
		t.Fatal("expected non-nil chunk")
	}
	if len(chunk.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(chunk.Choices))
	}
	if chunk.Choices[0].Delta.Content != "Hello" {
		t.Errorf("expected content 'Hello', got %q", chunk.Choices[0].Delta.Content)
	}
}

func TestAnthropicProvider_TransformStreamChunk_MessageDeltaWithUsage(t *testing.T) {
	p := NewAnthropicProvider(config.ProviderConfig{Name: "anthropic", Type: "anthropic"})

	chunk, done, err := p.TransformStreamChunk([]byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":8}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if done {
		t.Errorf("expected done=false for message delta")
	}
	if chunk == nil {
		t.Fatal("expected non-nil chunk")
	}
	if chunk.Usage == nil {
		t.Fatal("expected usage to be set")
	}
	if chunk.Usage.CompletionTokens != 8 {
		t.Errorf("expected completion tokens 8, got %d", chunk.Usage.CompletionTokens)
	}
	if chunk.Usage.TotalTokens != 8 {
		t.Errorf("expected total tokens 8, got %d", chunk.Usage.TotalTokens)
	}
}

func TestAnthropicProvider_TransformStreamChunk_MessageStop(t *testing.T) {
	p := NewAnthropicProvider(config.ProviderConfig{Name: "anthropic", Type: "anthropic"})

	chunk, done, err := p.TransformStreamChunk([]byte(`{"type":"message_stop"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !done {
		t.Errorf("expected done=true for message_stop")
	}
	if chunk != nil {
		t.Errorf("expected nil chunk for message_stop, got %v", chunk)
	}
}

func TestAnthropicProvider_TransformStreamChunk_MessageStart(t *testing.T) {
	p := NewAnthropicProvider(config.ProviderConfig{Name: "anthropic", Type: "anthropic"})

	chunk, done, err := p.TransformStreamChunk([]byte(`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"claude-sonnet-4-20250514","stop_reason":null,"usage":{"input_tokens":20,"output_tokens":0}}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if done {
		t.Errorf("expected done=false for message_start")
	}
	if chunk == nil {
		t.Fatal("expected non-nil chunk for message_start")
	}
	if len(chunk.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(chunk.Choices))
	}
	if chunk.Choices[0].Delta.Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", chunk.Choices[0].Delta.Role)
	}
	if chunk.Usage == nil {
		t.Fatal("expected usage from input_tokens")
	}
	if chunk.Usage.PromptTokens != 20 {
		t.Errorf("expected 20 prompt tokens, got %d", chunk.Usage.PromptTokens)
	}
}

func TestAnthropicProvider_TransformStreamChunk_ContentBlockStart(t *testing.T) {
	p := NewAnthropicProvider(config.ProviderConfig{Name: "anthropic", Type: "anthropic"})

	chunk, done, err := p.TransformStreamChunk([]byte(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if done {
		t.Errorf("expected done=false for content_block_start")
	}
	if chunk != nil {
		t.Errorf("expected nil chunk for content_block_start, got %v", chunk)
	}
}

func TestAnthropicProvider_TransformStreamChunk_ContentBlockStop(t *testing.T) {
	p := NewAnthropicProvider(config.ProviderConfig{Name: "anthropic", Type: "anthropic"})

	chunk, done, err := p.TransformStreamChunk([]byte(`{"type":"content_block_stop","index":0}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if done {
		t.Errorf("expected done=false for content_block_stop")
	}
	if chunk != nil {
		t.Errorf("expected nil chunk for content_block_stop, got %v", chunk)
	}
}

func TestAnthropicProvider_TransformStreamChunk_Ping(t *testing.T) {
	p := NewAnthropicProvider(config.ProviderConfig{Name: "anthropic", Type: "anthropic"})

	chunk, done, err := p.TransformStreamChunk([]byte(`{"type":"ping"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if done {
		t.Errorf("expected done=false for ping")
	}
	if chunk != nil {
		t.Errorf("expected nil chunk for ping, got %v", chunk)
	}
}

func TestAnthropicProvider_TransformStreamChunk_InvalidJSON(t *testing.T) {
	p := NewAnthropicProvider(config.ProviderConfig{Name: "anthropic", Type: "anthropic"})

	chunk, done, err := p.TransformStreamChunk([]byte(`{invalid}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if done {
		t.Errorf("expected done=false")
	}
	if chunk != nil {
		t.Errorf("expected nil chunk for invalid json")
	}
}

func TestAnthropicProvider_TransformStreamChunk_InputJSONDelta(t *testing.T) {
	p := NewAnthropicProvider(config.ProviderConfig{Name: "anthropic", Type: "anthropic"})

	chunk, done, err := p.TransformStreamChunk([]byte(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"location\": \"Istanbul\"}"}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if done {
		t.Errorf("expected done=false for input_json_delta")
	}
	if chunk == nil {
		t.Fatal("expected non-nil chunk for input_json_delta")
	}
	if len(chunk.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(chunk.Choices))
	}
	if len(chunk.Choices[0].Delta.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(chunk.Choices[0].Delta.ToolCalls))
	}
	if chunk.Choices[0].Delta.ToolCalls[0].Index != 1 {
		t.Errorf("expected index 1, got %d", chunk.Choices[0].Delta.ToolCalls[0].Index)
	}
	if chunk.Choices[0].Delta.ToolCalls[0].Function.Arguments != `{"location": "Istanbul"}` {
		t.Errorf("expected arguments '{\"location\": \"Istanbul\"}', got %q", chunk.Choices[0].Delta.ToolCalls[0].Function.Arguments)
	}
}

func TestAnthropicProvider_TransformStreamChunk_Error(t *testing.T) {
	p := NewAnthropicProvider(config.ProviderConfig{Name: "anthropic", Type: "anthropic"})

	chunk, done, err := p.TransformStreamChunk([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`))
	if err == nil {
		t.Fatal("expected error for error type")
	}
	if done {
		t.Errorf("expected done=false for error")
	}
	if chunk != nil {
		t.Errorf("expected nil chunk for error, got %v", chunk)
	}
}

func TestAnthropicProvider_TransformStreamChunk_MessageStartNoUsage(t *testing.T) {
	p := NewAnthropicProvider(config.ProviderConfig{Name: "anthropic", Type: "anthropic"})

	chunk, done, err := p.TransformStreamChunk([]byte(`{"type":"message_start","message":{"id":"msg_nousage","model":"claude-sonnet-4-20250514","content":[],"usage":{"input_tokens":0,"output_tokens":0}}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if done {
		t.Errorf("expected done=false")
	}
	if chunk == nil {
		t.Fatal("expected non-nil chunk")
	}
	if chunk.Usage != nil {
		t.Errorf("expected nil usage when input_tokens is 0, got %+v", chunk.Usage)
	}
	if chunk.Choices[0].Delta.Role != "assistant" {
		t.Errorf("expected role assistant, got %q", chunk.Choices[0].Delta.Role)
	}
}

func TestAnthropicProvider_TransformStreamChunk_ContentBlockStartToolUse(t *testing.T) {
	p := NewAnthropicProvider(config.ProviderConfig{Name: "anthropic", Type: "anthropic"})

	chunk, done, err := p.TransformStreamChunk([]byte(`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"location":"Istanbul"}}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if done {
		t.Errorf("expected done=false for tool_use content_block_start")
	}
	if chunk == nil {
		t.Fatal("expected non-nil chunk for tool_use content_block_start")
	}
	if len(chunk.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(chunk.Choices))
	}
	if len(chunk.Choices[0].Delta.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(chunk.Choices[0].Delta.ToolCalls))
	}
	if chunk.Choices[0].Delta.ToolCalls[0].ID != "toolu_1" {
		t.Errorf("expected tool call ID toolu_1, got %q", chunk.Choices[0].Delta.ToolCalls[0].ID)
	}
	if chunk.Choices[0].Delta.ToolCalls[0].Function.Name != "get_weather" {
		t.Errorf("expected function name get_weather, got %q", chunk.Choices[0].Delta.ToolCalls[0].Function.Name)
	}
}

func TestAnthropicProvider_TransformStreamChunk_MessageDeltaMaxTokens(t *testing.T) {
	p := NewAnthropicProvider(config.ProviderConfig{Name: "anthropic", Type: "anthropic"})

	chunk, done, err := p.TransformStreamChunk([]byte(`{"type":"message_delta","delta":{"stop_reason":"max_tokens","stop_sequence":null},"usage":{"output_tokens":4096}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if done {
		t.Errorf("expected done=false for message_delta")
	}
	if chunk == nil {
		t.Fatal("expected non-nil chunk for message_delta")
	}
	if chunk.Choices[0].FinishReason == nil {
		t.Fatal("expected finish_reason to be set")
	}
	if *chunk.Choices[0].FinishReason != "length" {
		t.Errorf("expected finish_reason 'length' for max_tokens, got %q", *chunk.Choices[0].FinishReason)
	}
	if chunk.Usage == nil {
		t.Fatal("expected usage to be set")
	}
	if chunk.Usage.CompletionTokens != 4096 {
		t.Errorf("expected 4096 completion tokens, got %d", chunk.Usage.CompletionTokens)
	}
}
