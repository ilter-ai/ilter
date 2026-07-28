package provider

import (
	"fmt"
	"sync"

	"github.com/ilter-ai/ilter/internal/config"
)

type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
	}
}

func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
}

func (r *Registry) Get(name string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %s not found", name)
	}
	return p, nil
}

func (r *Registry) List() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Provider, 0, len(r.providers))
	for _, p := range r.providers {
		result = append(result, p)
	}
	return result
}

// InitFromCache re-initializes all DB-backed providers from a config cache
// snapshot. Builds the entire DB provider set off to the side, then atomically
// swaps it into the registry while keeping any existing (YAML-sourced) providers
// that are not present in the cache. This ensures a failed provider construction
// does not corrupt the live routing state (all-or-nothing semantics).
//
// On error, the registry is left unchanged (previous working set preserved).
func (r *Registry) InitFromCache(snap *config.Snapshot) error {
	providers := snap.Providers()
	if len(providers) == 0 {
		return nil // nothing to add from DB; YAML-only providers remain
	}

	// Build the new provider set off to the side.
	next := make(map[string]Provider, len(r.providers)+len(providers))

	// Copy existing (YAML-sourced) providers first.
	r.mu.RLock()
	for k, v := range r.providers {
		next[k] = v
	}
	r.mu.RUnlock()

	// Add or override with DB providers from the cache.
	for _, pCfg := range providers {
		p, err := NewProviderFromConfig(pCfg)
		if err != nil {
			return fmt.Errorf("init provider %q from cache: %w", pCfg.Name, err)
		}
		next[pCfg.Name] = p
	}

	// Atomic swap — readers see the full new set or the full old set.
	r.mu.Lock()
	r.providers = next
	r.mu.Unlock()
	return nil
}

// BaseURL defaults are handled by config.EnrichConfig before this is called.
func (r *Registry) InitFromConfig(cfg *config.Config) error {
	for _, pCfg := range cfg.Providers {
		switch pCfg.Type {
		case "openai", "deepseek", "gemini", "opencode_go", "opencode_zen":
			r.Register(NewOpenAIProvider(pCfg))
		case "qwen":
			r.Register(NewOpenAIProvider(pCfg))
		case "openrouter":
			r.Register(NewOpenRouterProvider(pCfg))
		case "anthropic":
			r.Register(NewAnthropicProvider(pCfg))
		case "ollama":
			r.Register(NewOllamaProvider(pCfg))
		case "mock":
			r.Register(NewMockProvider(pCfg.Name))
		default:
			return fmt.Errorf("unsupported provider type: %s", pCfg.Type)
		}
	}
	return nil
}
