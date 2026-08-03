package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
	"github.com/sony/gobreaker/v2"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/circuitbreaker"
	"github.com/ilter-ai/ilter/internal/middleware"
	"github.com/ilter-ai/ilter/internal/model"

	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

// setupRateLimitE2E creates a router with RateLimiter middleware and a simple OK handler.
func setupRateLimitE2E(t *testing.T, rdb *redis.Client, defaultRPM int) *chi.Mux {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping e2e rate limit test in short mode")
	}

	rl, err := middleware.NewRateLimitMiddleware(&config.RateLimitConfig{
		Enabled:     true,
		AdminBypass: true,
		DefaultRPM:  defaultRPM,
	}, circuitbreaker.NewRedisBreaker(rdb, 200*time.Millisecond, gobreaker.Settings{}), nil)
	if err != nil {
		t.Fatalf("failed to create rate limiter: %v", err)
	}

	r := chi.NewRouter()
	r.Use(rl.Handler)
	r.Post("/v1/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	return r
}

// setRateLimitCtx injects API key ID and rate limit config into a request context.
func setRateLimitCtx(keyID string, rpm int) context.Context {
	ctx := context.WithValue(context.Background(), reqmeta.APIKeyIDContextKey, keyID)
	ctx = context.WithValue(ctx, reqmeta.APIKeyRateLimitContextKey, rpm)
	return ctx
}

// TestRateLimitE2E_ExceedsRPM verifies that a non-admin key exceeding the RPM limit gets a 429.
func TestRateLimitE2E_ExceedsRPM(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skip("Redis not available, skipping")
	}

	// Clean rate limit keys before and after
	cleanRateLimitKeys := func() {
		keys, _ := rdb.Keys(ctx, "ilter:ratelimit:*").Result()
		for _, k := range keys {
			rdb.Del(ctx, k)
		}
	}
	cleanRateLimitKeys()
	defer cleanRateLimitKeys()

	// RPM = 2: 3rd request should be blocked
	r := setupRateLimitE2E(t, rdb, 2)

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-mock",
		Messages: []model.Message{{Role: "user", Content: "Rate limit test"}},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	// Request 1 & 2: should pass (within limit)
	for i := range 2 {
		req, _ := http.NewRequestWithContext(
			setRateLimitCtx("500", 2),
			"POST", "/v1/chat/completions", bytes.NewReader(bodyBytes),
		)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d: %s", i+1, rr.Code, rr.Body.String())
		}
	}

	// Request 3: should exceed limit → 429
	req, _ := http.NewRequestWithContext(
		setRateLimitCtx("500", 2),
		"POST", "/v1/chat/completions", bytes.NewReader(bodyBytes),
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 for rate limit exceeded, got %d: %s", rr.Code, rr.Body.String())
	}

	var errResp map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &errResp)
	if e, _ := errResp["error"].(map[string]any); e["type"] != "rate_limit_exceeded" {
		t.Errorf("expected rate_limit_exceeded type, got %v", e["type"])
	}
}

// TestRateLimitE2E_AdminBypass verifies that admin key (key_id=0) bypasses rate limiting.
func TestRateLimitE2E_AdminBypass(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skip("Redis not available, skipping")
	}
	defer func() {
		keys, _ := rdb.Keys(ctx, "ilter:ratelimit:*").Result()
		for _, k := range keys {
			rdb.Del(ctx, k)
		}
	}()

	// RPM = 1, but admin should bypass even with 3 requests
	r := setupRateLimitE2E(t, rdb, 1)

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-mock",
		Messages: []model.Message{{Role: "user", Content: "Admin bypass test"}},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	for i := range 3 {
		// Admin key bypasses rate limiting
		ctx := context.WithValue(context.Background(), reqmeta.KeyIDContextKey, "admin")
		req, _ := http.NewRequestWithContext(
			ctx,
			"POST", "/v1/chat/completions", bytes.NewReader(bodyBytes),
		)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("admin request %d: expected 200, got %d: %s", i+1, rr.Code, rr.Body.String())
		}
	}
}

// TestRateLimitE2E_Headers verifies that X-RateLimit-Limit and X-RateLimit-Remaining headers
// are set correctly on rate-limited requests.
func TestRateLimitE2E_Headers(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skip("Redis not available, skipping")
	}
	defer func() {
		keys, _ := rdb.Keys(ctx, "ilter:ratelimit:*").Result()
		for _, k := range keys {
			rdb.Del(ctx, k)
		}
	}()

	r := setupRateLimitE2E(t, rdb, 5)

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-mock",
		Messages: []model.Message{{Role: "user", Content: "Headers test"}},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequestWithContext(
		setRateLimitCtx("600", 5),
		"POST", "/v1/chat/completions", bytes.NewReader(bodyBytes),
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	limitHeader := rr.Header().Get("X-RateLimit-Limit")
	if limitHeader == "" {
		t.Error("expected X-RateLimit-Limit header")
	} else if limitHeader != "5" {
		t.Errorf("expected X-RateLimit-Limit 5, got %s", limitHeader)
	}

	remainingHeader := rr.Header().Get("X-RateLimit-Remaining")
	if remainingHeader == "" {
		t.Error("expected X-RateLimit-Remaining header")
	} else {
		remaining, err := strconv.Atoi(remainingHeader)
		if err != nil {
			t.Errorf("invalid X-RateLimit-Remaining value %q: %v", remainingHeader, err)
		}
		if remaining < 0 || remaining > 5 {
			t.Errorf("X-RateLimit-Remaining %d outside expected range [0,5]", remaining)
		}
	}
}

// TestRateLimitE2E_RetryAfterHeader verifies that Retry-After header is set when rate limited.
func TestRateLimitE2E_RetryAfterHeader(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skip("Redis not available, skipping")
	}
	defer func() {
		keys, _ := rdb.Keys(ctx, "ilter:ratelimit:*").Result()
		for _, k := range keys {
			rdb.Del(ctx, k)
		}
	}()

	r := setupRateLimitE2E(t, rdb, 0) // RPM=0: first request is already exceeding

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-mock",
		Messages: []model.Message{{Role: "user", Content: "Retry-After test"}},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	// First request with RPM=0 on the context, exceeded immediately
	req, _ := http.NewRequestWithContext(
		setRateLimitCtx("700", 0),
		"POST", "/v1/chat/completions", bytes.NewReader(bodyBytes),
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", rr.Code, rr.Body.String())
	}

	if rr.Header().Get("Retry-After") != "60" {
		t.Errorf("expected Retry-After: 60, got %s", rr.Header().Get("Retry-After"))
	}
}

// TestRateLimitE2E_KeySpecificRateLimit verifies that when a key-specific rate limit is set
// in the request context, it overrides the default RPM.
func TestRateLimitE2E_KeySpecificRateLimit(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skip("Redis not available, skipping")
	}
	defer func() {
		keys, _ := rdb.Keys(ctx, "ilter:ratelimit:*").Result()
		for _, k := range keys {
			rdb.Del(ctx, k)
		}
	}()

	// Default RPM=100 but key-specific RPM=2
	r := setupRateLimitE2E(t, rdb, 100)

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-mock",
		Messages: []model.Message{{Role: "user", Content: "Key-specific rate limit test"}},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	// Key 800 with RPM=2
	for i := range 2 {
		req, _ := http.NewRequestWithContext(
			setRateLimitCtx("800", 2),
			"POST", "/v1/chat/completions", bytes.NewReader(bodyBytes),
		)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d: %s", i+1, rr.Code, rr.Body.String())
		}
	}

	// 3rd request overrides the default RPM=100 with key-specific RPM=2 → should exceed
	req, _ := http.NewRequestWithContext(
		setRateLimitCtx("800", 2),
		"POST", "/v1/chat/completions", bytes.NewReader(bodyBytes),
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 for key-specific rate limit, got %d: %s", rr.Code, rr.Body.String())
	}
}
