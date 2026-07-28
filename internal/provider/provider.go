package provider

import (
	"context"
	"net/http"

	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
)

// Provider represents an LLM provider.
type Provider interface {
	// Name returns the provider's unique name (from config).
	Name() string

	// Type returns the provider type ("openai", "anthropic", "deepseek", etc.).
	Type() string

	TransformRequest(ctx context.Context, req *model.ChatCompletionRequest) (*http.Request, error)

	TransformResponse(ctx context.Context, resp *http.Response) (*model.ChatCompletionResponse, error)

	// Client returns the HTTP client configured for this provider, including circuit breaker transport.
	Client() *http.Client

	HealthCheck(ctx context.Context) error

	// DiscoverModels returns a list of model definitions available from the provider.
	DiscoverModels(ctx context.Context) ([]catalog.ModelInfo, error)
}

// StreamEvent represents a streaming event.
type StreamEvent struct {
	Chunk *model.ChatCompletionChunk // nil on error or done
	Err   error                      // non-nil on error
	Done  bool                       // true when stream is done
}

// StreamingProvider supports streaming responses.
type StreamingProvider interface {
	Provider

	TransformStreamChunk(data []byte) (*model.ChatCompletionChunk, bool, error)
}

type selectedAPIKeyContextKey struct{}

// WithSelectedAPIKey returns a new context with the selected provider API key.
func WithSelectedAPIKey(ctx context.Context, apiKey string) context.Context {
	return context.WithValue(ctx, selectedAPIKeyContextKey{}, apiKey)
}

// SelectedAPIKeyFromContext returns the selected API key from context, or empty string.
func SelectedAPIKeyFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(selectedAPIKeyContextKey{}).(string); ok {
		return v
	}
	return ""
}

// ConfigurableProvider supports runtime base URL and API key updates.
type ConfigurableProvider interface {
	UpdateConfig(baseURL string, apiKey string)
	UpdateKeys(baseURL string, apiKey string, apiKeys []string)
}
