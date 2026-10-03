package smartrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model"
)

const (
	// JevDefaultEndpoint is the public OpenCode Zen Jev endpoint; jev-1.13-free
	// needs no API key.
	JevDefaultEndpoint = "https://opencode.ai/zen/v1/systemone"
	// JevDefaultModel is the keyless free Jev variant served by OpenCode Zen.
	JevDefaultModel = "jev-1.13-free"

	// jevMaxStateChars bounds the text sent to Jev (input limit is 32k tokens).
	jevMaxStateChars = 24000
	jevMaxBodyBytes  = 1 << 20
)

// jevTierScores maps each Jev tier answer to a representative point on the
// 0-100 complexity scale (inside the default economy/standard/premium bands).
var jevTierScores = map[string]float64{
	"economy":  5,
	"standard": 32,
	"premium":  75,
}

// jevQuestions is the typed decision asked of Jev for every request.
var jevQuestions = map[string]any{
	"tier": map[string]any{
		"type":         "choice",
		"instructions": "How demanding is the task in this request for an AI model?",
		"criteria": map[string]string{
			"economy":  "Greetings, small talk, simple factual Q&A, trivial rewrites or lookups",
			"standard": "Explanations, summaries, comparisons, moderate coding, multi-step instructions",
			"premium":  "Deep reasoning, complex code generation or debugging, architecture, long structured analysis",
		},
	},
}

// JevScorer rates prompt complexity with the TypeSafe Jev decision model. Jev
// returns a probability per tier instead of generated text; the score is the
// probability-weighted position on the 0-100 scale.
type JevScorer struct {
	model    string
	endpoint string
	apiKey   string
	client   *http.Client
	fallback *HeuristicScorer

	cacheTTL        time.Duration
	cacheMaxEntries int
	mu              sync.RWMutex
	cache           map[[32]byte]cachedScore
}

// NewJevScorer builds a Jev scorer, resolving endpoint and API key from the
// named provider in providers (type "jev" or "opencode_zen"). With no provider
// configured it targets the public Zen free endpoint without credentials.
func NewJevScorer(cfg *config.JevScorerConfig, providers []config.ProviderConfig) (*JevScorer, error) {
	if cfg == nil {
		return nil, fmt.Errorf("jev scorer config is nil")
	}
	endpoint, apiKey := JevDefaultEndpoint, ""
	jevModel := cfg.Model
	if cfg.Provider != "" {
		var found *config.ProviderConfig
		for i := range providers {
			if providers[i].Name == cfg.Provider {
				found = &providers[i]
				break
			}
		}
		if found == nil {
			return nil, fmt.Errorf("jev scorer: provider %q not found", cfg.Provider)
		}
		switch found.Type {
		case "jev", "opencode_zen":
		default:
			return nil, fmt.Errorf("jev scorer: provider %q has type %q, want jev or opencode_zen", found.Name, found.Type)
		}
		endpoint = strings.TrimRight(found.BaseURL, "/") + "/systemone"
		if keys := found.GetAPIKeys(); len(keys) > 0 {
			apiKey = keys[0]
		}
		if jevModel == "" && found.Type == "jev" {
			jevModel = "jev-latest"
		}
	}
	if jevModel == "" {
		jevModel = JevDefaultModel
	}
	return newJevScorer(cfg, endpoint, apiKey, jevModel), nil
}

func newJevScorer(cfg *config.JevScorerConfig, endpoint, apiKey, jevModel string) *JevScorer {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ttl := cfg.CacheTTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &JevScorer{
		model:           jevModel,
		endpoint:        endpoint,
		apiKey:          apiKey,
		client:          &http.Client{Timeout: timeout},
		fallback:        NewHeuristicScorer(),
		cacheTTL:        ttl,
		cacheMaxEntries: cfg.CacheMaxEntries,
		cache:           make(map[[32]byte]cachedScore),
	}
}

// Score asks Jev for the request's tier distribution. Results are cached by
// content hash; on any failure the heuristic scorer answers instead.
func (s *JevScorer) Score(ctx context.Context, messages []model.Message, tools []model.Tool) float64 {
	state := extractUserContent(messages)
	if len(tools) > 0 {
		state += fmt.Sprintf("\n[The request also exposes %d tool/function definitions.]", len(tools))
	}
	if len(state) > jevMaxStateChars {
		state = state[len(state)-jevMaxStateChars:]
	}

	hash := sha256Of(state)
	s.mu.RLock()
	c, ok := s.cache[hash]
	s.mu.RUnlock()
	if ok && time.Now().Before(c.expiresAt) {
		return c.score
	}

	score, err := s.callJev(ctx, state)
	if err != nil {
		slog.Warn("Jev scorer failed, falling back to heuristic", "error", err)
		return s.fallback.Score(ctx, messages, tools)
	}

	s.mu.Lock()
	if s.cacheMaxEntries > 0 && len(s.cache) >= s.cacheMaxEntries {
		s.cache = make(map[[32]byte]cachedScore)
	}
	s.cache[hash] = cachedScore{score: score, expiresAt: time.Now().Add(s.cacheTTL)}
	s.mu.Unlock()
	return score
}

type jevResponse struct {
	Answers map[string]struct {
		Probabilities map[string]float64 `json:"probabilities"`
	} `json:"answers"`
}

func (s *JevScorer) callJev(ctx context.Context, state string) (float64, error) {
	body, err := json.Marshal(map[string]any{
		"state":     state,
		"model":     s.model,
		"questions": jevQuestions,
	})
	if err != nil {
		return 0, fmt.Errorf("marshal jev request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("build jev request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("jev request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, jevMaxBodyBytes))
	if err != nil {
		return 0, fmt.Errorf("read jev response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("jev returned status %d", resp.StatusCode)
	}

	var parsed jevResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return 0, fmt.Errorf("parse jev response: %w", err)
	}
	return jevScoreFromProbabilities(parsed.Answers["tier"].Probabilities)
}

// jevScoreFromProbabilities converts a tier probability distribution into the
// probability-weighted 0-100 complexity score, renormalising over known tiers.
func jevScoreFromProbabilities(probs map[string]float64) (float64, error) {
	var total, weighted float64
	for tier, point := range jevTierScores {
		p := probs[tier]
		if p < 0 {
			continue
		}
		total += p
		weighted += p * point
	}
	if total <= 0 {
		return 0, fmt.Errorf("jev response has no tier probabilities")
	}
	return weighted / total, nil
}
