package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

func TestRateLimitMiddleware_Disabled(t *testing.T) {
	cfg := &config.RateLimitConfig{Enabled: false}
	mw, err := NewRateLimitMiddleware(cfg, nil, nil)
	if err != nil {
		t.Fatalf("failed to create rate limit middleware: %v", err)
	}

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	rr := httptest.NewRecorder()
	mw.Handler(next).ServeHTTP(rr, req)

	if !called {
		t.Error("expected next handler to be called when disabled")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestRateLimitMiddleware_AdminBypass(t *testing.T) {
	cfg := &config.RateLimitConfig{Enabled: true, DefaultRPM: 10, AdminBypass: true}
	mw, err := NewRateLimitMiddleware(cfg, nil, nil)
	if err != nil {
		t.Fatalf("failed to create rate limit middleware: %v", err)
	}

	ctx := context.WithValue(context.Background(), reqmeta.KeyIDContextKey, "admin")
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	mw.Handler(next).ServeHTTP(rr, req)

	if !called {
		t.Error("expected next handler to be called for admin bypass")
	}
}

func TestRateLimitMiddleware_NilGuard(t *testing.T) {
	cfg := &config.RateLimitConfig{Enabled: true, DefaultRPM: 10, AdminBypass: false}
	mw, err := NewRateLimitMiddleware(cfg, nil, nil)
	if err != nil {
		t.Fatalf("failed to create rate limit middleware: %v", err)
	}

	ctx := context.WithValue(context.Background(), reqmeta.KeyIDContextKey, "some-key")
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	mw.Handler(next).ServeHTTP(rr, req)

	if !called {
		t.Error("expected next handler to be called when guard is nil")
	}
}

func TestRateLimitMiddleware_Limiter(t *testing.T) {
	cfg := &config.RateLimitConfig{Enabled: false}
	mw, err := NewRateLimitMiddleware(cfg, nil, nil)
	if err != nil {
		t.Fatalf("failed to create rate limit middleware: %v", err)
	}

	lim := mw.Limiter()
	if lim == nil {
		t.Fatal("expected non-nil Limiter")
	}
}
