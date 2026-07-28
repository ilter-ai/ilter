package smartrouter

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model"
)

// ReferenceVectorStore loads reference vectors for each complexity tier
// from a persistent store (SQLite, Redis, etc.). Each tier maps to a
// []float32 embedding centroid.
type ReferenceVectorStore interface {
	LoadReferenceVectors(ctx context.Context) (map[string][]float32, error)
}

// EmbeddingScorer uses text embeddings to classify complexity by comparing
// the input embedding to pre-computed reference embeddings for each tier.
type EmbeddingScorer struct {
	cfg      *config.EmbeddingScorerConfig
	mu       sync.RWMutex
	embedder func(ctx context.Context, text string) ([]float32, error)
	store    ReferenceVectorStore
}

// NewEmbeddingScorer creates an embedding-based complexity scorer.
// embedFn is the embedding function (injected to avoid circular deps with cache package).
func NewEmbeddingScorer(cfg *config.EmbeddingScorerConfig) (*EmbeddingScorer, error) {
	if cfg == nil {
		return nil, fmt.Errorf("embedding scorer config is nil")
	}
	return &EmbeddingScorer{
		cfg:      cfg,
		embedder: nil, // set via SetEmbedder
	}, nil
}

// SetEmbedder sets the embedding function. Must be called before first use.
func (s *EmbeddingScorer) SetEmbedder(fn func(ctx context.Context, text string) ([]float32, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.embedder = fn
}

// SetStore sets the reference vector store. When set, reference vectors are
// loaded from the store instead of using hard-coded defaults.
func (s *EmbeddingScorer) SetStore(store ReferenceVectorStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store = store
}

// Score generates an embedding for the input and compares with reference embeddings.
// Falls back to heuristic if embedding is unavailable.
func (s *EmbeddingScorer) Score(ctx context.Context, messages []model.Message, tools []model.Tool) float64 {
	prompt := extractUserContent(messages)

	s.mu.RLock()
	embedder := s.embedder
	s.mu.RUnlock()

	if embedder == nil {
		slog.Warn("Embedding scorer: no embedder set, falling back to heuristic")
		return fallbackScore(prompt, len(tools))
	}

	vec, err := embedder(ctx, prompt)
	if err != nil {
		slog.Warn("Embedding scorer: embedding failed, falling back", "error", err)
		return fallbackScore(prompt, len(tools))
	}

	// Load reference vectors from store, falling back to static defaults
	refs := s.loadReferences(ctx)
	economyRef, economyOK := refs["economy"]
	standardRef, standardOK := refs["standard"]
	premiumRef, premiumOK := refs["premium"]

	// Fall back to static reference vectors if store returned incomplete data
	if !economyOK || !standardOK || !premiumOK {
		economyRef = s.makeReference("economy")
		standardRef = s.makeReference("standard")
		premiumRef = s.makeReference("premium")
	}

	econSim := cosineSimilarity(vec, economyRef)
	stdSim := cosineSimilarity(vec, standardRef)
	premSim := cosineSimilarity(vec, premiumRef)

	// Map similarity to score: closest centroid maps to middle of its range
	// Economy: 0-15 → centroid ~7.5, Standard: 16-50 → centroid ~33, Premium: 51-100 → centroid ~75
	if econSim >= stdSim && econSim >= premSim {
		// Economy range: score 0-15, scaled by how close to centroid
		return 7.5 * (1 - econSim)
	}
	if stdSim >= econSim && stdSim >= premSim {
		return 33.0 * stdSim
	}
	return 75.0 * premSim
}

// loadReferences loads reference vectors from the store if available.
// Falls back to makeReference when store is nil, empty, or returns an error.
func (s *EmbeddingScorer) loadReferences(ctx context.Context) map[string][]float32 {
	s.mu.RLock()
	store := s.store
	s.mu.RUnlock()

	if store == nil {
		return nil
	}

	refs, err := store.LoadReferenceVectors(ctx)
	if err != nil {
		slog.Warn("Embedding scorer: failed to load reference vectors, using defaults", "error", err)
		return nil
	}
	if len(refs) == 0 {
		return nil
	}
	return refs
}

// makeReference returns a reference vector of configured dimensions.
// static reference vectors; fallback when store is unavailable.
func (s *EmbeddingScorer) makeReference(tier string) []float32 {
	dims := s.cfg.Dimensions
	if dims <= 0 {
		dims = 768
	}
	vec := make([]float32, dims)
	switch tier {
	case "economy":
		vec[0] = 0.1 // simple/short prompts
	case "standard":
		vec[1] = 0.5 // moderate complexity
	case "premium":
		vec[2] = 0.9 // complex prompts
	}
	return vec
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
