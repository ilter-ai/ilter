package guardrails

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// ModerationCategory represents a single moderation category result.
type ModerationCategory struct {
	Name    string  `json:"name"`
	Score   float64 `json:"score"`
	Flagged bool    `json:"flagged"`
}

// ModerationResult holds the outcome of a moderation API call.
type ModerationResult struct {
	Flagged    bool                 `json:"flagged"`
	Categories []ModerationCategory `json:"categories"`
}

// ModerationClient sends content to an external moderation API.
type ModerationClient interface {
	Moderate(ctx context.Context, content string) (*ModerationResult, error)
}

type moderationClient struct {
	client *http.Client
	url    string
	apiKey string
	logger *slog.Logger
}

// NewModerationClient creates a moderation API client from the given config.
// single http.Client with timeout; no retry (fail-open on error),
// extend with retry/backoff when the API proves unreliable.
func NewModerationClient(cfg ModerationAPI, logger *slog.Logger) ModerationClient {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &moderationClient{
		client: &http.Client{Timeout: timeout},
		url:    cfg.URL,
		apiKey: cfg.APIKey,
		logger: logger,
	}
}

// Moderate sends content to the external moderation API and returns the result.
// On any error (network, timeout, non-2xx, parse failure) it logs a warning and
// returns (nil, nil) — fail-open — so the request is not blocked by a flaky API.
func (mc *moderationClient) Moderate(ctx context.Context, content string) (*ModerationResult, error) {
	body, err := json.Marshal(map[string]string{"input": content})
	if err != nil {
		mc.logger.Warn("moderation: marshal request", "error", err)
		return nil, nil // fail-open
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mc.url, bytes.NewReader(body))
	if err != nil {
		mc.logger.Warn("moderation: create request", "error", err)
		return nil, nil
	}
	req.Header.Set("Content-Type", "application/json")
	if mc.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+mc.apiKey)
	}

	resp, err := mc.client.Do(req)
	if err != nil {
		mc.logger.Warn("moderation: request failed, failing open", "url", mc.url, "error", err)
		return nil, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		mc.logger.Warn("moderation: non-2xx response, failing open", "status", resp.StatusCode)
		return nil, nil
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		mc.logger.Warn("moderation: read response body, failing open", "error", err)
		return nil, nil
	}

	// Parse the OpenAI Moderation API response format (the de-facto standard).
	// Other providers can expose a compatible shape.
	type moderationResponse struct {
		Results []struct {
			Flagged        bool               `json:"flagged"`
			Categories     map[string]bool    `json:"categories"`
			CategoryScores map[string]float64 `json:"category_scores"`
		} `json:"results"`
	}

	var modResp moderationResponse
	if err := json.Unmarshal(respBody, &modResp); err != nil {
		mc.logger.Warn("moderation: parse response, failing open", "error", err)
		return nil, nil
	}

	if len(modResp.Results) == 0 {
		return &ModerationResult{}, nil
	}

	r := modResp.Results[0]
	mr := &ModerationResult{Flagged: r.Flagged}
	for name, flagged := range r.Categories {
		mr.Categories = append(mr.Categories, ModerationCategory{
			Name:    name,
			Score:   r.CategoryScores[name],
			Flagged: flagged,
		})
	}
	return mr, nil
}

// ─────────────────────────────────────────────────────────────────────
// Mock client for tests
// ─────────────────────────────────────────────────────────────────────

// MockModerationClient is a test double that returns a canned result.
type MockModerationClient struct {
	Result *ModerationResult
	Err    error
}

func (m *MockModerationClient) Moderate(_ context.Context, _ string) (*ModerationResult, error) {
	return m.Result, m.Err
}

// AlwaysPass returns a mock that never flags anything.
func AlwaysPass() *MockModerationClient {
	return &MockModerationClient{Result: &ModerationResult{}}
}

// AlwaysFlag returns a mock that flags all content in the given categories.
func AlwaysFlag(categories ...string) *MockModerationClient {
	cs := make([]ModerationCategory, len(categories))
	for i, name := range categories {
		cs[i] = ModerationCategory{Name: name, Score: 0.99, Flagged: true}
	}
	return &MockModerationClient{Result: &ModerationResult{Flagged: true, Categories: cs}}
}
