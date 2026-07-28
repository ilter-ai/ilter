package provider

import (
	"context"
	"fmt"

	"github.com/ilter-ai/ilter/internal/config"
)

// NewProviderFromConfig is the single dispatch point for creating a Provider
// from a ProviderConfig. It handles all supported provider types.
func NewProviderFromConfig(cfg config.ProviderConfig) (Provider, error) {
	switch cfg.Type {
	case "openai", "deepseek", "gemini", "opencode_go", "opencode_zen", "qwen":
		return NewOpenAIProvider(cfg), nil
	case "openrouter":
		return NewOpenRouterProvider(cfg), nil
	case "anthropic":
		return NewAnthropicProvider(cfg), nil
	case "ollama":
		return NewOllamaProvider(cfg), nil
	case "mock":
		return NewMockProvider(cfg.Name), nil
	default:
		return nil, fmt.Errorf("unsupported provider type: %q for provider %q", cfg.Type, cfg.Name)
	}
}

// NewProviderFromDB creates a Provider from the config cache by looking up
// the provider by name. The config cache already holds decrypted API keys
// from the DB-backed ProviderRegistry store.
//
// Returns an error if the provider is not found in the cache or if the
// provider type is unsupported.
func NewProviderFromDB(_ context.Context, providerName string, cache *config.Cache) (Provider, error) {
	snap := cache.Get()
	if snap == nil {
		return nil, fmt.Errorf("config cache not initialized")
	}
	for _, pCfg := range snap.Providers() {
		if pCfg.Name == providerName {
			return NewProviderFromConfig(pCfg)
		}
	}
	return nil, fmt.Errorf("provider %q not found in config cache", providerName)
}
