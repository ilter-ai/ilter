package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/config"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	require.NotNil(t, r)
	// Verify it's usable by registering and getting a provider
	assert.NotPanics(t, func() {
		r.Register(NewOpenAIProvider(config.ProviderConfig{Name: "test"}))
	})
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()

	p := NewOpenAIProvider(config.ProviderConfig{
		Name: "my-provider",
		Type: "openai",
	})
	r.Register(p)

	got, err := r.Get("my-provider")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "my-provider", got.Name())
	assert.Equal(t, "openai", got.Type())
}

func TestRegistry_Get_NotFound(t *testing.T) {
	r := NewRegistry()
	_, err := r.Get("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "provider nonexistent not found")
}

func TestRegistry_Register_Overwrite(t *testing.T) {
	r := NewRegistry()

	p1 := NewOpenAIProvider(config.ProviderConfig{Name: "dup", Type: "openai", APIKey: "key1"})
	p2 := NewOpenAIProvider(config.ProviderConfig{Name: "dup", Type: "openai", APIKey: "key2"})

	r.Register(p1)
	r.Register(p2) // should overwrite

	got, err := r.Get("dup")
	require.NoError(t, err)
	// Should be the last one registered
	assert.Equal(t, "key2", got.(*OpenAIProvider).config.APIKey)
}

func TestRegistry_MultipleProviders(t *testing.T) {
	r := NewRegistry()

	p1 := NewOpenAIProvider(config.ProviderConfig{Name: "provider-a", Type: "openai"})
	p2 := NewOpenAIProvider(config.ProviderConfig{Name: "provider-b", Type: "deepseek"})
	r.Register(p1)
	r.Register(p2)

	got1, err := r.Get("provider-a")
	require.NoError(t, err)
	assert.Equal(t, "openai", got1.Type())

	got2, err := r.Get("provider-b")
	require.NoError(t, err)
	assert.Equal(t, "deepseek", got2.Type())
}

func TestInitFromConfig_OpenAI(t *testing.T) {
	r := NewRegistry()
	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{
				Name:   "my-openai",
				Type:   "openai",
				APIKey: "sk-test",
			},
		},
	}

	err := r.InitFromConfig(cfg)
	require.NoError(t, err)

	p, err := r.Get("my-openai")
	require.NoError(t, err)
	assert.Equal(t, "my-openai", p.Name())
	assert.Equal(t, "openai", p.Type())
}

func TestInitFromConfig_DeepSeek(t *testing.T) {
	r := NewRegistry()
	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{
				Name:   "my-deepseek",
				Type:   "deepseek",
				APIKey: "sk-test",
			},
		},
	}

	err := r.InitFromConfig(cfg)
	require.NoError(t, err)

	p, err := r.Get("my-deepseek")
	require.NoError(t, err)
	assert.Equal(t, "deepseek", p.Type())
}

func TestInitFromConfig_Ollama(t *testing.T) {
	r := NewRegistry()
	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{
				Name:    "my-ollama",
				Type:    "ollama",
				BaseURL: "http://localhost:11434",
			},
		},
	}

	err := r.InitFromConfig(cfg)
	require.NoError(t, err)

	p, err := r.Get("my-ollama")
	require.NoError(t, err)
	assert.Equal(t, "ollama", p.Type())
	assert.Equal(t, "ollama", p.(*OllamaProvider).Type())
}

func TestInitFromConfig_Anthropic(t *testing.T) {
	r := NewRegistry()
	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{
				Name:   "my-anthropic",
				Type:   "anthropic",
				APIKey: "sk-ant-test",
			},
		},
	}

	err := r.InitFromConfig(cfg)
	require.NoError(t, err)

	p, err := r.Get("my-anthropic")
	require.NoError(t, err)
	assert.Equal(t, "anthropic", p.Type())
}

func TestInitFromConfig_MultipleProviders(t *testing.T) {
	r := NewRegistry()
	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{Name: "openai-1", Type: "openai", APIKey: "sk-1"},
			{Name: "anthropic-1", Type: "anthropic", APIKey: "sk-ant-1"},
			{Name: "ollama-1", Type: "ollama", BaseURL: "http://localhost:11434"},
			{Name: "deepseek-1", Type: "deepseek", APIKey: "sk-ds-1"},
		},
	}

	err := r.InitFromConfig(cfg)
	require.NoError(t, err)

	// Verify each provider type
	p, err := r.Get("openai-1")
	require.NoError(t, err)
	assert.Equal(t, "openai", p.Type())

	p, err = r.Get("anthropic-1")
	require.NoError(t, err)
	assert.Equal(t, "anthropic", p.Type())

	p, err = r.Get("ollama-1")
	require.NoError(t, err)
	assert.Equal(t, "ollama", p.Type())

	p, err = r.Get("deepseek-1")
	require.NoError(t, err)
	assert.Equal(t, "deepseek", p.Type())
}

func TestInitFromConfig_UnsupportedType(t *testing.T) {
	r := NewRegistry()
	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{
				Name: "unknown",
				Type: "nonexistent-provider",
			},
		},
	}

	err := r.InitFromConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported provider type")
}

func TestInitFromConfig_DefaultBaseURLs(t *testing.T) {
	r := NewRegistry()

	// Test providers that have default base URLs
	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{Name: "my-openrouter", Type: "openrouter", APIKey: "sk-test"},
			{Name: "my-gemini", Type: "gemini", APIKey: "sk-test"},
			{Name: "my-qwen", Type: "qwen", APIKey: "sk-test"},
			{Name: "my-opencode-go", Type: "opencode_go", APIKey: "sk-test"},
			{Name: "my-opencode-zen", Type: "opencode_zen", APIKey: "sk-test"},
		},
	}

	// EnrichConfig sets BaseURL defaults; InitFromConfig no longer duplicates that.
	config.EnrichConfig(cfg)

	err := r.InitFromConfig(cfg)
	require.NoError(t, err)

	// Verify default base URLs were assigned
	tests := []struct {
		name        string
		expectedURL string
		assertType  func(t *testing.T, p Provider)
	}{
		{
			"my-openrouter", "https://openrouter.ai/api/v1",
			func(t *testing.T, p Provider) {
				openRouterProvider, ok := p.(*OpenRouterProvider)
				require.True(t, ok, "expected OpenRouterProvider")
				assert.Equal(t, "https://openrouter.ai/api/v1", openRouterProvider.OpenAIProvider.config.BaseURL)
			},
		},
		{
			"my-gemini", "https://generativelanguage.googleapis.com/v1beta/openai",
			func(t *testing.T, p Provider) {
				openAIProvider, ok := p.(*OpenAIProvider)
				require.True(t, ok, "expected OpenAIProvider")
				assert.Equal(t, "https://generativelanguage.googleapis.com/v1beta/openai", openAIProvider.config.BaseURL)
			},
		},
		{
			"my-qwen", "https://dashscope-intl.aliyuncs.com/compatible-mode/v1",
			func(t *testing.T, p Provider) {
				openAIProvider, ok := p.(*OpenAIProvider)
				require.True(t, ok, "expected OpenAIProvider")
				assert.Equal(t, "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", openAIProvider.config.BaseURL)
			},
		},
		{
			"my-opencode-go", "https://opencode.ai/zen/go/v1",
			func(t *testing.T, p Provider) {
				openAIProvider, ok := p.(*OpenAIProvider)
				require.True(t, ok, "expected OpenAIProvider")
				assert.Equal(t, "https://opencode.ai/zen/go/v1", openAIProvider.config.BaseURL)
			},
		},
		{
			"my-opencode-zen", "https://opencode.ai/zen/v1",
			func(t *testing.T, p Provider) {
				openAIProvider, ok := p.(*OpenAIProvider)
				require.True(t, ok, "expected OpenAIProvider")
				assert.Equal(t, "https://opencode.ai/zen/v1", openAIProvider.config.BaseURL)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := r.Get(tt.name)
			require.NoError(t, err)
			tt.assertType(t, p)
		})
	}
}

func TestInitFromConfig_OpenAI_DefaultBaseURLFromEnrich(t *testing.T) {
	r := NewRegistry()

	// EnrichConfig now provides the default BaseURL for openai
	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{Name: "my-openai", Type: "openai", APIKey: "sk-test"},
		},
	}
	config.EnrichConfig(cfg)

	err := r.InitFromConfig(cfg)
	require.NoError(t, err)

	p, err := r.Get("my-openai")
	require.NoError(t, err)
	openAIProvider, ok := p.(*OpenAIProvider)
	require.True(t, ok)
	// EnrichConfig should set the default BaseURL for openai
	assert.Equal(t, "https://api.openai.com/v1", openAIProvider.config.BaseURL)
}

// TestInitFromConfig_CustomBaseURL tests that a custom BaseURL is not overridden by defaults.
func TestInitFromConfig_CustomBaseURL(t *testing.T) {
	r := NewRegistry()

	cfg := &config.Config{
		Providers: []config.ProviderConfig{
			{
				Name:    "my-custom-openrouter",
				Type:    "openrouter",
				APIKey:  "sk-test",
				BaseURL: "https://custom.openrouter.ai/v1",
			},
		},
	}

	err := r.InitFromConfig(cfg)
	require.NoError(t, err)

	p, err := r.Get("my-custom-openrouter")
	require.NoError(t, err)
	openRouterProvider, ok := p.(*OpenRouterProvider)
	require.True(t, ok)
	// Should retain the custom URL, not the default
	assert.Equal(t, "https://custom.openrouter.ai/v1", openRouterProvider.OpenAIProvider.config.BaseURL)
}
