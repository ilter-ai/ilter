package fallback

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/sony/gobreaker/v2"
	"github.com/stretchr/testify/assert"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
	"github.com/ilter-ai/ilter/internal/platform/cooldown"
	"github.com/ilter-ai/ilter/internal/provider"
)

func TestClassifier_Classify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		err        error
		want       Verdict
	}{
		{
			name:       "429 Too Many Requests -> ExcludeKey",
			statusCode: http.StatusTooManyRequests,
			err:        nil,
			want:       VerdictExcludeKey,
		},
		{
			name:       "404 Not Found -> ExcludeCandidate",
			statusCode: http.StatusNotFound,
			err:        nil,
			want:       VerdictExcludeCandidate,
		},
		{
			name:       "503 Service Unavailable -> ExcludeCandidate",
			statusCode: http.StatusServiceUnavailable,
			err:        nil,
			want:       VerdictExcludeCandidate,
		},
		{
			name:       "401 Unauthorized -> ExcludeKey",
			statusCode: http.StatusUnauthorized,
			err:        nil,
			want:       VerdictExcludeKey,
		},
		{
			name:       "400 Bad Request with model not found -> ExcludeCandidate",
			statusCode: http.StatusBadRequest,
			err:        fmt.Errorf("model gpt-4 not found"),
			want:       VerdictExcludeCandidate,
		},
		{
			name:       "400 Bad Request general -> Fatal",
			statusCode: http.StatusBadRequest,
			err:        fmt.Errorf("invalid json payload"),
			want:       VerdictFatal,
		},
		{
			// A provider's breaker is shared across every model routed through it
			// (see provider.NewResilientClient). When it's open, the candidate was
			// never actually contacted, so it must not be excluded/cooled down as
			// if it had failed itself.
			name:       "shared breaker open (unrelated model tripped it) -> RetrySame, not ExcludeCandidate",
			statusCode: 0,
			err:        &url.Error{Op: "Post", URL: "https://opencode.ai/zen/v1/chat/completions", Err: gobreaker.ErrOpenState},
			want:       VerdictRetrySame,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.statusCode, tt.err)
			if got != tt.want {
				t.Errorf("Classify(%d, %v) = %v, want %v", tt.statusCode, tt.err, got, tt.want)
			}
		})
	}
}

type mockProvider struct {
	name string
}

func (m *mockProvider) Name() string { return m.name }

func (m *mockProvider) Type() string { return m.name }

func (m *mockProvider) TransformRequest(_ context.Context, _ *model.ChatCompletionRequest) (*http.Request, error) {
	return nil, nil
}

func (m *mockProvider) TransformResponse(_ context.Context, _ *http.Response) (*model.ChatCompletionResponse, error) {
	return nil, nil
}

func (m *mockProvider) Client() *http.Client { return http.DefaultClient }

func (m *mockProvider) HealthCheck(_ context.Context) error { return nil }

func (m *mockProvider) DiscoverModels(_ context.Context) ([]catalog.ModelInfo, error) {
	return nil, nil
}

func TestFallbackExecutor_Execute(t *testing.T) {
	t.Parallel()

	reg := provider.NewRegistry()
	reg.Register(&mockProvider{name: "p1"})
	reg.Register(&mockProvider{name: "p2"})

	store := cooldown.NewInMemoryStore()
	cfg := config.FallbackConfig{
		Enabled:          true,
		CooldownDuration: 5 * time.Minute,
	}

	fe := NewFallbackExecutor(cfg, store, reg)
	ctx := context.Background()

	candidates := []cooldown.Candidate{
		{Provider: "p1", Model: "m1", KeyID: "k1"},
		{Provider: "p2", Model: "m2", KeyID: "k2"},
	}

	t.Run("first candidate succeeds", func(t *testing.T) {
		res, err := fe.Execute(ctx, candidates, func(_ context.Context, cand cooldown.Candidate, _ provider.Provider) (int, http.Header, error) {
			if cand.Provider == "p1" {
				return 200, nil, nil
			}
			return 500, nil, fmt.Errorf("error")
		})
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if res.Candidate.Provider != "p1" {
			t.Errorf("expected candidate p1, got %s", res.Candidate.Provider)
		}
	})

	t.Run("first candidate fails with 429, falls back to second", func(t *testing.T) {
		res, err := fe.Execute(ctx, candidates, func(_ context.Context, cand cooldown.Candidate, _ provider.Provider) (int, http.Header, error) {
			if cand.Provider == "p1" {
				return 429, nil, fmt.Errorf("rate limited")
			}
			return 200, nil, nil
		})
		if err != nil {
			t.Fatalf("expected success on fallback, got %v", err)
		}
		if res.Candidate.Provider != "p2" {
			t.Errorf("expected candidate p2, got %s", res.Candidate.Provider)
		}
		if !store.InCooldown(ctx, candidates[0]) {
			t.Errorf("expected p1 to be in cooldown after 429")
		}
	})

	t.Run("candidate rejected by shared breaker is not cooled down", func(t *testing.T) {
		freshStore := cooldown.NewInMemoryStore()
		fe := NewFallbackExecutor(cfg, freshStore, reg)
		breakerErr := &url.Error{Op: "Post", URL: "https://opencode.ai/zen/v1/chat/completions", Err: gobreaker.ErrOpenState}
		res, err := fe.Execute(ctx, candidates, func(_ context.Context, cand cooldown.Candidate, _ provider.Provider) (int, http.Header, error) {
			if cand.Provider == "p1" {
				return 0, nil, breakerErr
			}
			return 200, nil, nil
		})
		if err != nil {
			t.Fatalf("expected success on fallback, got %v", err)
		}
		if res.Candidate.Provider != "p2" {
			t.Errorf("expected candidate p2, got %s", res.Candidate.Provider)
		}
		if freshStore.InCooldown(ctx, candidates[0]) {
			t.Errorf("p1 was never actually contacted (shared breaker was open) - it must not be put in cooldown")
		}
	})
}

func TestParseRetryAfter_Headers(t *testing.T) {
	t.Parallel()

	// 1. Standard integer Retry-After
	h1 := http.Header{}
	h1.Set("Retry-After", "30")
	dur1 := ParseRetryAfter(h1)
	assert.True(t, dur1 >= 27*time.Second && dur1 <= 33*time.Second)

	// 2. OpenAI duration format
	h2 := http.Header{}
	h2.Set("x-ratelimit-reset-requests", "15s")
	dur2 := ParseRetryAfter(h2)
	assert.True(t, dur2 >= 13*time.Second && dur2 <= 17*time.Second)

	// 3. Anthropic RFC3339 timestamp
	h3 := http.Header{}
	futureTime := time.Now().Add(45 * time.Second).Format(time.RFC3339)
	h3.Set("anthropic-ratelimit-requests-reset", futureTime)
	dur3 := ParseRetryAfter(h3)
	assert.True(t, dur3 >= 35*time.Second && dur3 <= 55*time.Second)

	// 4. Default fallback when no headers
	dur4 := ParseRetryAfter(nil)
	assert.True(t, dur4 >= 17*time.Second && dur4 <= 23*time.Second)
}
