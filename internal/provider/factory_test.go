package provider

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/config"
)

func TestNewProviderFromConfig_OpenAI(t *testing.T) {
	p, err := NewProviderFromConfig(config.ProviderConfig{
		Name:   "test-openai",
		Type:   "openai",
		APIKey: "sk-test",
	})
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "test-openai", p.Name())
	assert.Equal(t, "openai", p.Type())
}

func TestNewProviderFromConfig_Anthropic(t *testing.T) {
	p, err := NewProviderFromConfig(config.ProviderConfig{
		Name:   "test-anthropic",
		Type:   "anthropic",
		APIKey: "sk-ant-test",
	})
	require.NoError(t, err)
	assert.Equal(t, "test-anthropic", p.Name())
	assert.Equal(t, "anthropic", p.Type())
}

func TestNewProviderFromConfig_DeepSeek(t *testing.T) {
	p, err := NewProviderFromConfig(config.ProviderConfig{
		Name:   "test-deepseek",
		Type:   "deepseek",
		APIKey: "sk-ds-test",
	})
	require.NoError(t, err)
	assert.Equal(t, "deepseek", p.Type())
}

func TestNewProviderFromConfig_Gemini(t *testing.T) {
	p, err := NewProviderFromConfig(config.ProviderConfig{
		Name:   "test-gemini",
		Type:   "gemini",
		APIKey: "sk-gem-test",
	})
	require.NoError(t, err)
	assert.Equal(t, "gemini", p.Type())
}

func TestNewProviderFromConfig_OpenRouter(t *testing.T) {
	p, err := NewProviderFromConfig(config.ProviderConfig{
		Name:   "test-openrouter",
		Type:   "openrouter",
		APIKey: "sk-or-test",
	})
	require.NoError(t, err)
	assert.Equal(t, "openrouter", p.Type())
	// OpenRouter wraps OpenAIProvider; verify the underlying type.
	or, ok := p.(*OpenRouterProvider)
	require.True(t, ok)
	assert.Equal(t, "https://openrouter.ai/api/v1", or.OpenAIProvider.config.BaseURL)
}

func TestNewProviderFromConfig_Ollama(t *testing.T) {
	p, err := NewProviderFromConfig(config.ProviderConfig{
		Name:    "test-ollama",
		Type:    "ollama",
		BaseURL: "http://localhost:11434",
	})
	require.NoError(t, err)
	assert.Equal(t, "ollama", p.Type())
}

func TestNewProviderFromConfig_Qwen(t *testing.T) {
	p, err := NewProviderFromConfig(config.ProviderConfig{
		Name:   "test-qwen",
		Type:   "qwen",
		APIKey: "sk-qw-test",
	})
	require.NoError(t, err)
	assert.Equal(t, "qwen", p.Type())
}

func TestNewProviderFromConfig_OpenCodeGo(t *testing.T) {
	p, err := NewProviderFromConfig(config.ProviderConfig{
		Name:   "test-oc-go",
		Type:   "opencode_go",
		APIKey: "sk-oc-test",
	})
	require.NoError(t, err)
	assert.Equal(t, "opencode_go", p.Type())
}

func TestNewProviderFromConfig_OpenCodeZen(t *testing.T) {
	p, err := NewProviderFromConfig(config.ProviderConfig{
		Name:   "test-oc-zen",
		Type:   "opencode_zen",
		APIKey: "sk-oc-test",
	})
	require.NoError(t, err)
	assert.Equal(t, "opencode_zen", p.Type())
}

func TestNewProviderFromConfig_Mock(t *testing.T) {
	p, err := NewProviderFromConfig(config.ProviderConfig{
		Name: "test-mock",
		Type: "mock",
	})
	require.NoError(t, err)
	assert.Equal(t, "mock", p.Type())
	_, ok := p.(*MockProvider)
	require.True(t, ok)
}

func TestNewProviderFromConfig_UnsupportedType(t *testing.T) {
	_, err := NewProviderFromConfig(config.ProviderConfig{
		Name: "bad",
		Type: "nonexistent",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported provider type")
}

func TestNewProviderFromConfig_DecryptedAPIKey(t *testing.T) {
	// Simulate a provider config where the API key is the decrypted secret
	// from the config cache (as returned by providerRegistrationToConfig).
	p, err := NewProviderFromConfig(config.ProviderConfig{
		Name:   "test-with-key",
		Type:   "openai",
		APIKey: "sk-decrypted-secret-12345",
	})
	require.NoError(t, err)
	oai, ok := p.(*OpenAIProvider)
	require.True(t, ok)
	assert.Equal(t, "sk-decrypted-secret-12345", oai.config.APIKey,
		"decrypted API key from cache must be passed through to provider config")
}

func TestNewProviderFromDB_MissingProvider(t *testing.T) {
	// Create a cache with no providers.
	boot := config.DefaultBootConfig()
	cache := config.NewConfigCache(&boot)

	ctx := context.Background()
	_, err := NewProviderFromDB(ctx, "nonexistent", cache)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found in config cache")
}

func TestInitFromCache_MergeWithExistingProviders(t *testing.T) {
	reg := NewRegistry()

	// Register a YAML-sourced provider first.
	yamlCfg := &config.Config{
		Providers: []config.ProviderConfig{
			{Name: "yaml-provider", Type: "openai", APIKey: "sk-yaml"},
		},
	}
	require.NoError(t, reg.InitFromConfig(yamlCfg))

	// Create a snapshot with a DB-sourced provider.
	snap := &config.Snapshot{
		RuntimeConfigSnapshot: &config.RuntimeConfigSnapshot{
			Providers: []config.ProviderConfig{
				{Name: "db-provider", Type: "anthropic", APIKey: "sk-ant-db"},
			},
		},
	}

	err := reg.InitFromCache(snap)
	require.NoError(t, err)

	// Both YAML and DB providers should exist.
	p1, err := reg.Get("yaml-provider")
	require.NoError(t, err)
	assert.Equal(t, "openai", p1.Type())

	p2, err := reg.Get("db-provider")
	require.NoError(t, err)
	assert.Equal(t, "anthropic", p2.Type())
}
