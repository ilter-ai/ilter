package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sony/gobreaker/v2"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/circuitbreaker"
	"github.com/ilter-ai/ilter/internal/model"
)

func TestSemanticCache_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping semantic cache integration test in short mode")
	}
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skip("Redis container is not available, skipping semantic cache integration test")
	}

	// Clean any leftover keys from previous runs
	keys, _ := rdb.Keys(ctx, "ilter:cache:*").Result()
	for _, k := range keys {
		rdb.Del(ctx, k)
	}

	// No embedder path → exact match mode (SHA256 keyed)
	cfg := config.CacheConfig{
		Enabled:             true,
		SimilarityThreshold: 0.85,
		TTL:                 1 * time.Hour,
	}

	sc := NewSemanticCacheMiddleware(cfg, circuitbreaker.NewRedisBreaker(rdb, 200*time.Millisecond, gobreaker.Settings{}), nil)

	callCount := 0
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		resp := model.ChatCompletionResponse{
			ID:    "resp-123",
			Model: "gpt-mock",
			Choices: []model.Choice{
				{
					Index:        0,
					Message:      model.ChoiceMessage{Role: "assistant", Content: "This is a cached output response"},
					FinishReason: "stop",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	})

	router := sc.Handler(dummyHandler)

	// --- 1st request (Cache Miss — exact mode) ---
	reqBody1 := model.ChatCompletionRequest{
		Model:    "gpt-mock",
		Messages: []model.Message{{Role: "user", Content: "What is 2+2?"}},
		Stream:   false,
	}
	bodyBytes1, _ := json.Marshal(reqBody1)
	req1 := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes1))
	rr1 := httptest.NewRecorder()

	router.ServeHTTP(rr1, req1)

	if rr1.Code != http.StatusOK {
		t.Fatalf("1st request failed with status: %d, body: %s", rr1.Code, rr1.Body.String())
	}
	if rr1.Header().Get("X-Cache-Hit") != "false" {
		t.Errorf("Expected X-Cache-Hit header to be 'false', got %s", rr1.Header().Get("X-Cache-Hit"))
	}
	if callCount != 1 {
		t.Errorf("Expected callCount to be 1, got %d", callCount)
	}

	// Wait briefly for Redis to write
	time.Sleep(100 * time.Millisecond)

	// --- 2nd request (Cache Hit — identical prompt, exact match) ---
	req2 := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes1))
	rr2 := httptest.NewRecorder()

	router.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("2nd request failed with status: %d", rr2.Code)
	}
	if rr2.Header().Get("X-Cache-Hit") != "true" {
		t.Errorf("Expected X-Cache-Hit header to be 'true', got %s", rr2.Header().Get("X-Cache-Hit"))
	}
	if callCount != 1 {
		t.Errorf("Expected callCount to remain 1 (cache hit), got %d", callCount)
	}

	var cacheResp model.ChatCompletionResponse
	if err := json.NewDecoder(rr2.Body).Decode(&cacheResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if cacheResp.Choices[0].Message.Content != "This is a cached output response" {
		t.Errorf("Unexpected cached content: %s", cacheResp.Choices[0].Message.Content)
	}

	// --- 3rd request (Cache Miss — different prompt in exact mode) ---
	reqBody3 := model.ChatCompletionRequest{
		Model:    "gpt-mock",
		Messages: []model.Message{{Role: "user", Content: "Give me something different"}},
		Stream:   false,
	}
	bodyBytes3, _ := json.Marshal(reqBody3)
	req3 := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes3))
	rr3 := httptest.NewRecorder()

	router.ServeHTTP(rr3, req3)

	if rr3.Code != http.StatusOK {
		t.Fatalf("3rd request failed with status: %d", rr3.Code)
	}
	if rr3.Header().Get("X-Cache-Hit") != "false" {
		t.Errorf("Expected X-Cache-Hit header to be 'false' for different prompt, got %s", rr3.Header().Get("X-Cache-Hit"))
	}
	if callCount != 2 {
		t.Errorf("Expected callCount to be 2, got %d", callCount)
	}
}
