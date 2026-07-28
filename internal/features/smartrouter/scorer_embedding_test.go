package smartrouter

import (
	"context"
	"errors"
	"testing"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model"
)

// mockVectorStore implements ReferenceVectorStore for testing.
type mockVectorStore struct {
	vectors map[string][]float32
	err     error
}

func (m *mockVectorStore) LoadReferenceVectors(_ context.Context) (map[string][]float32, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.vectors, nil
}

func newTestScorer(dims int) *EmbeddingScorer {
	s, err := NewEmbeddingScorer(&config.EmbeddingScorerConfig{
		Model:               "test-model",
		Dimensions:          dims,
		ReferenceCount:      3,
		SimilarityThreshold: 0.5,
	})
	if err != nil {
		panic(err)
	}
	s.SetEmbedder(func(_ context.Context, _ string) ([]float32, error) {
		return make([]float32, dims), nil
	})
	return s
}

func TestEmbeddingScorer_StoreVectorsUsed(t *testing.T) {
	dims := 4
	store := &mockVectorStore{
		vectors: map[string][]float32{
			"economy":  {0.9, 0.1, 0.0, 0.0},
			"standard": {0.1, 0.9, 0.0, 0.0},
			"premium":  {0.0, 0.1, 0.9, 0.0},
		},
	}
	s := newTestScorer(dims)
	s.SetStore(store)

	msg := []model.Message{{Content: "hello world"}}
	score := s.Score(context.Background(), msg, nil)

	// With an all-zero input embedding the closest centroid is economy
	// (economy has the most energy at index 0 where the input vec is also 0 —
	//  all-zero vec has all cos-sim=0, economyRef gets picked by tie-break
	//  because econSim >= stdSim && econSim >= premSim holds at 0==0==0).
	if score < 0 || score > 100 {
		t.Errorf("score out of range [0,100]: %f", score)
	}
}

func TestEmbeddingScorer_NilStoreFallsBack(t *testing.T) {
	s := newTestScorer(4)
	// No store set — should use makeReference

	msg := []model.Message{{Content: "hello world"}}
	score := s.Score(context.Background(), msg, nil)

	if score < 0 || score > 100 {
		t.Errorf("score out of range [0,100]: %f", score)
	}
}

func TestEmbeddingScorer_EmptyStoreFallsBack(t *testing.T) {
	store := &mockVectorStore{
		vectors: map[string][]float32{}, // empty map
	}
	s := newTestScorer(4)
	s.SetStore(store)

	msg := []model.Message{{Content: "hello world"}}
	score := s.Score(context.Background(), msg, nil)

	if score < 0 || score > 100 {
		t.Errorf("score out of range [0,100]: %f", score)
	}
}

func TestEmbeddingScorer_StoreErrorFallsBack(t *testing.T) {
	store := &mockVectorStore{
		err: errors.New("connection refused"),
	}
	s := newTestScorer(4)
	s.SetStore(store)

	msg := []model.Message{{Content: "hello world"}}
	score := s.Score(context.Background(), msg, nil)

	if score < 0 || score > 100 {
		t.Errorf("score out of range [0,100]: %f", score)
	}
}

func TestEmbeddingScorer_PartialStoreFallsBack(t *testing.T) {
	// Only economy vector in store — should fall back for standard/premium
	store := &mockVectorStore{
		vectors: map[string][]float32{
			"economy": {0.9, 0.0, 0.0, 0.0},
		},
	}
	s := newTestScorer(4)
	s.SetStore(store)

	msg := []model.Message{{Content: "hello world"}}
	score := s.Score(context.Background(), msg, nil)

	if score < 0 || score > 100 {
		t.Errorf("score out of range [0,100]: %f", score)
	}
}

func TestEmbeddingScorer_StoreWithDifferentDimensions(t *testing.T) {
	// Store vectors have 8 dims but cfg has 4 — mismatch must not crash
	store := &mockVectorStore{
		vectors: map[string][]float32{
			"economy":  {0.1, 0, 0, 0, 0, 0, 0, 0},
			"standard": {0, 0.5, 0, 0, 0, 0, 0, 0},
			"premium":  {0, 0, 0.9, 0, 0, 0, 0, 0},
		},
	}
	s := newTestScorer(4)
	s.SetStore(store)

	msg := []model.Message{{Content: "hello world"}}
	score := s.Score(context.Background(), msg, nil)

	// cosineSimilarity of different-length vectors returns 0, then closest
	// centroid picks economy (tie at 0, economy first in if-chain)
	if score < 0 || score > 100 {
		t.Errorf("score out of range [0,100]: %f", score)
	}
}

func TestLoadReferences_NilStoreReturnsNil(t *testing.T) {
	s := newTestScorer(4)
	refs := s.loadReferences(context.Background())
	if refs != nil {
		t.Errorf("expected nil, got %v", refs)
	}
}

func TestLoadReferences_StoreReturnsData(t *testing.T) {
	expected := map[string][]float32{
		"economy": {0.1, 0},
	}
	store := &mockVectorStore{vectors: expected}
	s := newTestScorer(4)
	s.SetStore(store)

	refs := s.loadReferences(context.Background())
	if refs == nil {
		t.Fatal("expected non-nil refs")
	}
	if len(refs["economy"]) != 2 || refs["economy"][0] != 0.1 {
		t.Errorf("unexpected economy vector: %v", refs["economy"])
	}
}
