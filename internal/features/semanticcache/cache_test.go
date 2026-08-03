package semanticcache

import (
	"testing"

	"github.com/ilter-ai/ilter/internal/config"
)

func TestNew(t *testing.T) {
	// With nil client and disabled cfg, New should not panic.
	sc := New(cfgDisabled(), nil, "")
	if sc == nil {
		t.Fatal("New returned nil")
	}
}

func TestNew_WithConfig(t *testing.T) {
	cfg := configWithDefaults()
	sc := New(cfg, nil, "")
	if sc == nil {
		t.Fatal("New returned nil")
	}
	if sc.cfg.SimilarityThreshold != 0.92 {
		t.Errorf("expected similarity threshold 0.92, got %f", sc.cfg.SimilarityThreshold)
	}
	if sc.client != nil {
		t.Errorf("expected nil client, got %v", sc.client)
	}
}

func TestFloat32ToByte(t *testing.T) {
	tests := []struct {
		name string
		vec  []float32
		want int // expected byte length
	}{
		{"empty slice", []float32{}, 0},
		{"single element", []float32{1.0}, 4},
		{"three elements", []float32{1.0, 2.0, 3.0}, 12},
		{"1536 dims", make([]float32, 1536), 1536 * 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := float32ToByte(tt.vec)
			if len(got) != tt.want {
				t.Errorf("float32ToByte length = %d, want %d", len(got), tt.want)
			}
		})
	}
}

func TestParseSearchResponse_Nil(t *testing.T) {
	resp, score, found := parseSearchResponse(nil, 0.5)
	if found {
		t.Error("expected found=false for nil input")
	}
	if resp != "" {
		t.Errorf("expected empty response, got %q", resp)
	}
	if score != 0 {
		t.Errorf("expected score 0, got %f", score)
	}
}

func TestParseSearchResponse_RESP2(t *testing.T) {
	// Simulate RESP2 array: [count, _, [key, val, key, val], ...]
	// FT.SEARCH returns: [1, "key:name", ["response", "hello", "score", "0.05"]]
	val := []any{
		int64(1),
		"ilter:cache:abc",
		[]any{
			"response", "hello world",
			"score", "0.05",
		},
	}

	resp, score, found := parseSearchResponse(val, 0.08)
	if !found {
		t.Fatal("expected found=true for RESP2 within threshold")
	}
	if resp != "hello world" {
		t.Errorf("expected 'hello world', got %q", resp)
	}
	if score != 0.05 {
		t.Errorf("expected score 0.05, got %f", score)
	}
}

func TestParseSearchResponse_RESP2_AboveThreshold(t *testing.T) {
	val := []any{
		int64(1),
		"ilter:cache:abc",
		[]any{
			"response", "far away",
			"score", "0.5",
		},
	}

	resp, score, found := parseSearchResponse(val, 0.08)
	if found {
		t.Error("expected found=false for distance above threshold")
	}
	if resp != "" {
		t.Errorf("expected empty response, got %q", resp)
	}
	if score != 0 {
		t.Errorf("expected score 0, got %f", score)
	}
}

func TestParseSearchResponse_RESP3_MapString(t *testing.T) {
	val := map[string]any{
		"results": []any{
			map[string]any{
				"extra_attributes": map[string]any{
					"response": "cached response",
					"score":    "0.03",
				},
			},
		},
	}

	resp, score, found := parseSearchResponse(val, 0.08)
	if !found {
		t.Fatal("expected found=true for RESP3 map[string]")
	}
	if resp != "cached response" {
		t.Errorf("expected 'cached response', got %q", resp)
	}
	if score != 0.03 {
		t.Errorf("expected score 0.03, got %f", score)
	}
}

func TestParseSearchResponse_RESP3_MapInterface(t *testing.T) {
	val := map[any]any{
		"results": []any{
			map[any]any{
				"extra_attributes": map[any]any{
					"response": "cached response",
					"score":    "0.03",
				},
			},
		},
	}

	resp, score, found := parseSearchResponse(val, 0.08)
	if !found {
		t.Fatal("expected found=true for RESP3 map[interface{}]")
	}
	if resp != "cached response" {
		t.Errorf("expected 'cached response', got %q", resp)
	}
	if score != 0.03 {
		t.Errorf("expected score 0.03, got %f", score)
	}
}

func TestParseSearchResponse_RESP3_Float64Score(t *testing.T) {
	val := map[string]any{
		"results": []any{
			map[string]any{
				"extra_attributes": map[string]any{
					"response": "float score",
					"score":    float64(0.04),
				},
			},
		},
	}

	resp, score, found := parseSearchResponse(val, 0.08)
	if !found {
		t.Fatal("expected found=true for float64 score")
	}
	if resp != "float score" {
		t.Errorf("expected 'float score', got %q", resp)
	}
	if score != 0.04 {
		t.Errorf("expected score 0.04, got %f", score)
	}
}

func TestParseSearchResponse_RESP3_Int64Score(t *testing.T) {
	val := map[string]any{
		"results": []any{
			map[string]any{
				"extra_attributes": map[string]any{
					"response": "int score",
					"score":    int64(0),
				},
			},
		},
	}

	resp, score, found := parseSearchResponse(val, 0.08)
	if !found {
		t.Fatal("expected found=true for int64 score (0 <= 0.08)")
	}
	if resp != "int score" {
		t.Errorf("expected 'int score', got %q", resp)
	}
	if score != 0 {
		t.Errorf("expected score 0, got %f", score)
	}
}

func TestParseSearchResponse_EmptyResults(t *testing.T) {
	val := map[string]any{
		"results": []any{},
	}

	resp, score, found := parseSearchResponse(val, 0.08)
	if found {
		t.Error("expected found=false for empty results")
	}
	if resp != "" {
		t.Errorf("expected empty response, got %q", resp)
	}
	if score != 0 {
		t.Errorf("expected score 0, got %f", score)
	}
}

// Helpers

func cfgDisabled() config.CacheConfig {
	return config.CacheConfig{Enabled: false}
}

func configWithDefaults() config.CacheConfig {
	return config.CacheConfig{
		Enabled:             true,
		SimilarityThreshold: 0.92,
		TTL:                 3600000000000, // 1 hour
	}
}
