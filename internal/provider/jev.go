package provider

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
)

// JevDefaultBaseURL is the TypeSafe API root; Jev is served at {base}/systemone.
const JevDefaultBaseURL = "https://api.typesafe.ai/v1"

// JevProvider holds credentials and model entries for TypeSafe Jev. Jev is a
// typed-decision model, not a chat model: it is consumed by the smart router's
// "jev" scorer and never receives chat completion traffic.
type JevProvider struct {
	*OpenAIProvider
}

func NewJevProvider(cfg config.ProviderConfig) *JevProvider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = JevDefaultBaseURL
	}
	cfg.Type = "jev"
	return &JevProvider{
		OpenAIProvider: &OpenAIProvider{
			config:   cfg,
			client:   NewResilientClient(cfg),
			provType: "jev",
		},
	}
}

func (p *JevProvider) Type() string { return "jev" }

func (p *JevProvider) TransformRequest(_ context.Context, _ *model.ChatCompletionRequest) (*http.Request, error) {
	return nil, fmt.Errorf("provider %q (jev) is a decision model and does not support chat completions", p.config.Name)
}

func (p *JevProvider) TransformResponse(_ context.Context, _ *http.Response) (*model.ChatCompletionResponse, error) {
	return nil, fmt.Errorf("provider %q (jev) is a decision model and does not support chat completions", p.config.Name)
}

// HealthCheck is a no-op: the TypeSafe API has no side-effect-free probe endpoint.
func (p *JevProvider) HealthCheck(_ context.Context) error { return nil }

// DiscoverModels returns the static Jev catalog (TypeSafe exposes no /models).
func (p *JevProvider) DiscoverModels(_ context.Context) ([]catalog.ModelInfo, error) {
	return []catalog.ModelInfo{
		jevModelInfo("jev-1.13", "Jev 1.13", 0.000000042, p.config.BaseURL),
		jevModelInfo("jev-latest", "Jev (latest)", 0.000000042, p.config.BaseURL),
	}, nil
}

func jevModelInfo(id, display string, costIn float64, baseURL string) catalog.ModelInfo {
	return catalog.ModelInfo{
		ID:                id,
		Provider:          "jev",
		DisplayName:       display,
		MaxContextTokens:  32000,
		MaxOutputTokens:   0,
		CostPerInputToken: costIn,
		Tier:              "economy",
		Capabilities:      []string{catalog.CapabilityDecision},
		DefaultBaseURL:    baseURL,
	}
}
