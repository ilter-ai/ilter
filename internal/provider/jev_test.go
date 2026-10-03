package provider

import (
	"context"
	"testing"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model"
)

func TestJevProvider(t *testing.T) {
	p, err := NewProviderFromConfig(config.ProviderConfig{Name: "typesafe", Type: "jev"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Type() != "jev" {
		t.Errorf("Type = %q", p.Type())
	}
	if _, err := p.TransformRequest(context.Background(), &model.ChatCompletionRequest{}); err == nil {
		t.Error("chat TransformRequest must fail for jev")
	}
	models, err := p.DiscoverModels(context.Background())
	if err != nil || len(models) == 0 {
		t.Fatalf("DiscoverModels = %v, %v", models, err)
	}
	for _, m := range models {
		if !m.IsDecisionModel() || m.Provider != "jev" {
			t.Errorf("model %q not marked as jev decision model", m.ID)
		}
	}
}
