package smartrouter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model"
)

func jevTestServer(t *testing.T, status int, body string, calls *atomic.Int32, gotAuth *string, gotReq *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if gotAuth != nil {
			*gotAuth = r.Header.Get("Authorization")
		}
		if gotReq != nil {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, gotReq)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestJevScorer_ScoreFromProbabilities(t *testing.T) {
	var calls atomic.Int32
	var auth string
	var req map[string]any
	srv := jevTestServer(t, http.StatusOK,
		`{"model":"jev-1.13-free","answers":{"tier":{"type":"choice","choice":"premium","confidence":0.9,"probabilities":{"economy":0.0,"standard":0.2,"premium":0.8}}}}`,
		&calls, &auth, &req)

	s := newJevScorer(&config.JevScorerConfig{}, srv.URL, "secret", "jev-1.13")
	msgs := []model.Message{{Role: "user", Content: "design a distributed consensus protocol"}}

	got := s.Score(context.Background(), msgs, nil)
	want := 0.2*32 + 0.8*75
	if got < want-0.001 || got > want+0.001 {
		t.Fatalf("score = %v, want %v", got, want)
	}
	if auth != "Bearer secret" {
		t.Errorf("Authorization = %q", auth)
	}
	if req["model"] != "jev-1.13" || req["state"] == "" || req["questions"] == nil {
		t.Errorf("unexpected request body: %v", req)
	}

	// Second identical call is served from cache.
	s.Score(context.Background(), msgs, nil)
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1 (cached)", calls.Load())
	}
}

func TestJevScorer_FallsBackToHeuristicOnError(t *testing.T) {
	var calls atomic.Int32
	srv := jevTestServer(t, http.StatusInternalServerError, `{"error":"boom"}`, &calls, nil, nil)
	s := newJevScorer(&config.JevScorerConfig{}, srv.URL, "", "jev-1.13-free")

	msgs := []model.Message{{Role: "user", Content: "hi"}}
	got := s.Score(context.Background(), msgs, nil)
	want := NewHeuristicScorer().Score(context.Background(), msgs, nil)
	if got != want {
		t.Fatalf("fallback score = %v, want heuristic %v", got, want)
	}
}

func TestNewJevScorer_ResolvesProvider(t *testing.T) {
	providers := []config.ProviderConfig{
		{Name: "typesafe", Type: "jev", BaseURL: "https://api.typesafe.ai/v1/", APIKey: "k"},
		{Name: "oa", Type: "openai", BaseURL: "https://api.openai.com/v1"},
	}

	s, err := NewJevScorer(&config.JevScorerConfig{Provider: "typesafe"}, providers)
	if err != nil {
		t.Fatal(err)
	}
	if s.endpoint != "https://api.typesafe.ai/v1/systemone" || s.apiKey != "k" || s.model != "jev-latest" {
		t.Errorf("got endpoint=%q key=%q model=%q", s.endpoint, s.apiKey, s.model)
	}

	if _, err := NewJevScorer(&config.JevScorerConfig{Provider: "oa"}, providers); err == nil {
		t.Error("expected error for non-jev provider type")
	}
	if _, err := NewJevScorer(&config.JevScorerConfig{Provider: "missing"}, providers); err == nil {
		t.Error("expected error for unknown provider")
	}

	def, err := NewJevScorer(&config.JevScorerConfig{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if def.endpoint != JevDefaultEndpoint || def.model != JevDefaultModel || def.apiKey != "" {
		t.Errorf("defaults wrong: %+v", def)
	}
}

func TestNewScorerFromConfig_JevFallsBackWhenProviderMissing(t *testing.T) {
	sc := config.ScorerConfig{Type: "jev", Jev: &config.JevScorerConfig{Provider: "missing"}}
	if _, ok := NewScorerFromConfig(sc, nil).(*HeuristicScorer); !ok {
		t.Error("expected heuristic fallback")
	}
	if _, ok := NewScorerFromConfig(config.ScorerConfig{Type: "jev"}, nil).(*JevScorer); !ok {
		t.Error("expected JevScorer with defaults")
	}
}
