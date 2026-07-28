package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/budget"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/middleware"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
	"github.com/ilter-ai/ilter/internal/provider"
)

type benchProvider struct{ name, typ string }

func (s *benchProvider) Name() string { return s.name }
func (s *benchProvider) Type() string { return s.typ }
func (s *benchProvider) TransformRequest(_ context.Context, _ *model.ChatCompletionRequest) (*http.Request, error) {
	return nil, nil
}

func (s *benchProvider) TransformResponse(_ context.Context, _ *http.Response) (*model.ChatCompletionResponse, error) {
	return nil, nil
}
func (s *benchProvider) Client() *http.Client                { return &http.Client{} }
func (s *benchProvider) HealthCheck(_ context.Context) error { return nil }
func (s *benchProvider) DiscoverModels(_ context.Context) ([]catalog.ModelInfo, error) {
	return nil, nil
}

func benchLoadBalancer(b *testing.B, models []config.ModelConfig) *smartrouter.LoadBalancer {
	b.Helper()
	cfg := &config.Config{Providers: []config.ProviderConfig{
		{Name: "bench-openai", Type: "openai", Models: models},
	}}
	reg := provider.NewRegistry()
	reg.Register(&benchProvider{name: "bench-openai", typ: "openai"})
	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	if err != nil {
		b.Fatalf("NewLoadBalancer: %v", err)
	}
	return lb
}

func BenchmarkCalculateCost(b *testing.B) {
	mc := config.ModelConfig{Name: "gpt-4o", CostPerInputToken: 0.0025, CostPerOutputToken: 0.01}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CalculateCost(mc, 1000, 500)
	}
}

func BenchmarkCalculateCost_ZeroTokens(b *testing.B) {
	mc := config.ModelConfig{Name: "gpt-4o", CostPerInputToken: 0.0025, CostPerOutputToken: 0.01}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CalculateCost(mc, 0, 0)
	}
}

func BenchmarkJSONDecode_ChatRequest(b *testing.B) {
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}],"temperature":0.7,"max_tokens":100,"stream":false}`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var req model.ChatCompletionRequest
		_ = json.Unmarshal([]byte(body), &req)
	}
}

func BenchmarkJSONDecode_ComplexRequest(b *testing.B) {
	body := `{"model":"","messages":[{"role":"system","content":"You are a helpful assistant."},{"role":"user","content":"Analyze the performance of this database schema."}],"tools":[{"type":"function","function":{"name":"analyze","description":"Analyze the schema","parameters":{"type":"object","properties":{"table":{"type":"string"}}}}}],"temperature":0.3,"max_tokens":2000,"stream":false}`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var req model.ChatCompletionRequest
		_ = json.Unmarshal([]byte(body), &req)
	}
}

func BenchmarkJSONEncode_ErrorResponse(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp := model.ErrorResponse{
			Error: model.ErrorDetail{Message: "Invalid API key", Type: "authentication_error", Code: "invalid_api_key"},
		}
		_, _ = json.Marshal(resp)
	}
}

func BenchmarkJSONEncode_SuccessResponse(b *testing.B) {
	resp := map[string]interface{}{
		"id": "chatcmpl-abc", "object": "chat.completion", "created": 1700000000, "model": "gpt-4o",
		"choices": []map[string]interface{}{{
			"index": 0, "finish_reason": "stop",
			"message": map[string]interface{}{"role": "assistant", "content": "Hello!"},
		}},
		"usage": map[string]interface{}{"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = json.Marshal(resp)
	}
}

func BenchmarkNewHandler(b *testing.B) {
	lb := benchLoadBalancer(b, []config.ModelConfig{{Name: "gpt-4o", Weight: 100}})
	al := middleware.NewAuditLoggerMiddleware(nil)
	be := budget.NewEnforcer(config.BudgetConfig{}, nil, nil, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewHandler(lb, al, be, nil)
	}
}

func BenchmarkGetRoutes(b *testing.B) {
	lb := benchLoadBalancer(b, []config.ModelConfig{{Name: "gpt-4o", Weight: 100}})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = lb.GetRoutes("gpt-4o")
	}
}

func BenchmarkNextRoute(b *testing.B) {
	lb := benchLoadBalancer(b, []config.ModelConfig{{Name: "gpt-4o", Weight: 100}})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = lb.NextRoute("gpt-4o", "cheapest")
	}
}

func BenchmarkGetAvailableModels(b *testing.B) {
	lb := benchLoadBalancer(b, []config.ModelConfig{
		{Name: "gpt-4o", Weight: 100}, {Name: "gpt-4o-mini", Weight: 100}, {Name: "deepseek-chat", Weight: 100},
	})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = lb.GetAvailableModels()
	}
}

func BenchmarkStreamResponse_JSON(b *testing.B) {
	chunk := `{"id":"chatcmpl-abc","object":"chat.completion.chunk","choices":[{"delta":{"content":"Hello"},"index":0}]}`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var parsed map[string]interface{}
		_ = json.Unmarshal([]byte(chunk), &parsed)
	}
}

func BenchmarkSSEFormatting(b *testing.B) {
	data := `{"id":"chatcmpl-abc","object":"chat.completion.chunk","choices":[{"delta":{"content":"Hello"},"index":0}]}`
	prefix := []byte("data: ")
	suffix := []byte("\n\n")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var buf bytes.Buffer
		buf.Write(prefix)
		buf.WriteString(data)
		buf.Write(suffix)
		_ = buf.Bytes()
	}
}

func BenchmarkRegistryLookup(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = catalog.Models["gpt-4o"]
	}
}

func BenchmarkLoadBalancerConcurrent(b *testing.B) {
	lb := benchLoadBalancer(b, []config.ModelConfig{
		{Name: "gpt-4o", Weight: 100},
		{Name: "deepseek-chat", Weight: 100},
	})
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = lb.NextRoute("gpt-4o", "cheapest")
		}
	})
}

func BenchmarkJSONDecode_Streaming(b *testing.B) {
	chunks := []string{
		`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
		`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}`,
		`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"!"},"finish_reason":null}]}`,
		`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, chunk := range chunks {
			var parsed map[string]interface{}
			_ = json.Unmarshal([]byte(chunk), &parsed)
		}
	}
}

func BenchmarkModelCostCalculation(b *testing.B) {
	models := []config.ModelConfig{
		{Name: "gpt-4o", CostPerInputToken: 0.0025, CostPerOutputToken: 0.01},
		{Name: "gpt-4o-mini", CostPerInputToken: 0.00015, CostPerOutputToken: 0.0006},
		{Name: "deepseek-chat", CostPerInputToken: 0.00027, CostPerOutputToken: 0.0011},
	}
	prompt := 1500
	completion := 800
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, m := range models {
			_ = CalculateCost(m, prompt, completion)
		}
	}
}
