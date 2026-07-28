package smartrouter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model"
)

// LLMScorer calls an LLM to rate prompt complexity from 0-100.
type LLMScorer struct {
	cfg    *config.LLMScorerConfig
	client *http.Client
	mu     sync.RWMutex
	cache  map[[32]byte]cachedScore // SHA256 → score
}

type cachedScore struct {
	score     float64
	expiresAt time.Time
}

// NewLLMScorer creates an LLM-based complexity scorer.
func NewLLMScorer(cfg *config.LLMScorerConfig) (*LLMScorer, error) {
	if cfg == nil {
		return nil, fmt.Errorf("llm scorer config is nil")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &LLMScorer{
		cfg:    cfg,
		client: &http.Client{Timeout: timeout},
		cache:  make(map[[32]byte]cachedScore),
	}, nil
}

// Score calls an LLM to rate complexity. Results are cached by message hash.
func (s *LLMScorer) Score(ctx context.Context, messages []model.Message, tools []model.Tool) float64 {
	prompt := extractUserContent(messages)

	// Check cache first
	hash := sha256Of(prompt)
	s.mu.RLock()
	if c, ok := s.cache[hash]; ok && time.Now().Before(c.expiresAt) {
		s.mu.RUnlock()
		return c.score
	}
	s.mu.RUnlock()

	score, err := s.callLLM(ctx, prompt, len(tools))
	if err != nil {
		slog.Warn("LLM scorer failed, falling back", "error", err)
		return fallbackScore(prompt, len(tools))
	}

	cacheTTL := s.cfg.CacheTTL
	if cacheTTL <= 0 {
		cacheTTL = 5 * time.Minute
	}
	s.mu.Lock()
	if len(s.cache) >= s.cfg.CacheMaxEntries && s.cfg.CacheMaxEntries > 0 {
		// evict one random entry (lazy: just clear all)
		s.cache = make(map[[32]byte]cachedScore)
	}
	s.cache[hash] = cachedScore{score: score, expiresAt: time.Now().Add(cacheTTL)}
	s.mu.Unlock()

	return score
}

// callLLM sends the prompt to the configured LLM and parses the score.
func (s *LLMScorer) callLLM(ctx context.Context, prompt string, toolCount int) (float64, error) {
	systemMsg := `You are a complexity scorer for an AI gateway. Rate the complexity of the user's prompt on a scale of 0-100.

- 0-15: Simple (greetings, small talk, simple Q&A)
- 16-50: Moderate (explanations, comparisons, multi-step instructions)
- 51-100: Complex (analysis, code generation, structured output, deep reasoning)

Respond with ONLY a number between 0 and 100. No explanation, no formatting.`
	if toolCount > 0 {
		systemMsg += ` Note: this request includes tool/function calls, which may increase complexity.`
	}

	reqBody := map[string]any{
		"model": s.cfg.Model,
		"messages": []map[string]string{
			{"role": "system", "content": systemMsg},
			{"role": "user", "content": prompt},
		},
		"max_tokens":  10,
		"temperature": 0.0,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return 0, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.Provider, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return 0, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("read: %w", err)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return 0, fmt.Errorf("parse response: %w", err)
	}
	if len(result.Choices) == 0 {
		return 0, fmt.Errorf("no choices in LLM response")
	}

	var score float64
	if _, err := fmt.Sscanf(result.Choices[0].Message.Content, "%f", &score); err != nil {
		return 0, fmt.Errorf("parse score from %q: %w", result.Choices[0].Message.Content, err)
	}
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return score, nil
}

// sha256Of returns the SHA256 hash of a string.
func sha256Of(s string) [32]byte {
	return sha256.Sum256([]byte(s))
}
