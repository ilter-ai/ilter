package catalog

import (
	"errors"
	"testing"
)

var errTestLoader = errors.New("loader error")

func TestLoadFromDB(t *testing.T) {
	// Reset state from any previous test.
	ModelsMu.Lock()
	Models = make(map[string][]ModelInfo)
	ModelsMu.Unlock()

	mockLoader := func() ([]ModelInfo, error) {
		return []ModelInfo{
			{
				ID:                 "gpt-4o",
				Provider:           "openai",
				DisplayName:        "GPT-4o",
				MaxContextTokens:   128000,
				CostPerInputToken:  0.0000025,
				CostPerOutputToken: 0.00001,
				Tier:               "standard",
			},
			{
				ID:                 "claude-sonnet-4-20250514",
				Provider:           "anthropic",
				DisplayName:        "Claude Sonnet 4",
				MaxContextTokens:   200000,
				CostPerInputToken:  0.000003,
				CostPerOutputToken: 0.000015,
				Tier:               "standard",
			},
		}, nil
	}

	if err := LoadFromDB(mockLoader); err != nil {
		t.Fatalf("LoadFromDB failed: %v", err)
	}

	if len(Models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(Models))
	}

	gpt4o, ok := Models["gpt-4o"]
	if !ok {
		t.Fatal("gpt-4o should be in the cache")
	}
	if gpt4o[0].Provider != "openai" {
		t.Errorf("expected provider openai, got %s", gpt4o[0].Provider)
	}
	if gpt4o[0].CostPerInputToken != 0.0000025 {
		t.Errorf("unexpected cost per input token: %f", gpt4o[0].CostPerInputToken)
	}
	if gpt4o[0].MaxContextTokens != 128000 {
		t.Errorf("unexpected max context tokens: %d", gpt4o[0].MaxContextTokens)
	}

	claude, ok := Models["claude-sonnet-4-20250514"]
	if !ok {
		t.Fatal("claude-sonnet-4-20250514 should be in the cache")
	}
	if claude[0].Provider != "anthropic" {
		t.Errorf("expected provider anthropic, got %s", claude[0].Provider)
	}
}

func TestLoadFromDB_Empty(t *testing.T) {
	ModelsMu.Lock()
	Models = make(map[string][]ModelInfo)
	ModelsMu.Unlock()

	mockLoader := func() ([]ModelInfo, error) {
		return []ModelInfo{}, nil
	}

	if err := LoadFromDB(mockLoader); err != nil {
		t.Fatalf("LoadFromDB failed: %v", err)
	}

	if len(Models) != 0 {
		t.Fatalf("expected 0 models, got %d", len(Models))
	}
}

func TestLoadFromDB_Error(t *testing.T) {
	mockLoader := func() ([]ModelInfo, error) {
		return nil, errTestLoader
	}

	if err := LoadFromDB(mockLoader); err == nil {
		t.Fatal("expected error, got nil")
	}
}
