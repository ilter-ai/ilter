package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

func TestBudgetMiddleware_Disabled(t *testing.T) {
	cfg := config.BudgetConfig{Enabled: false}
	mw := NewBudgetMiddleware(cfg, nil, nil, nil)

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

func TestBudgetMiddleware_NoKeyID(t *testing.T) {
	cfg := config.BudgetConfig{Enabled: true, DefaultMonthlyLimit: 100}
	mw := NewBudgetMiddleware(cfg, nil, nil, nil)

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	rr := httptest.NewRecorder()
	mw.Handler(next).ServeHTTP(rr, req)

	if !called {
		t.Error("expected next handler to be called when no keyID")
	}
}

func TestBudgetMiddleware_NilGuard(t *testing.T) {
	cfg := config.BudgetConfig{Enabled: true, DefaultMonthlyLimit: 100}
	mw := NewBudgetMiddleware(cfg, nil, nil, nil)

	ctx := context.WithValue(context.Background(), reqmeta.KeyIDContextKey, "test-key")
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

func TestBudgetMiddleware_HeadersSet(t *testing.T) {
	t.Skip("requires non-nil Guard (Redis) — add Redis integration variant when test Redis is available")

	cfg := config.BudgetConfig{Enabled: true, DefaultMonthlyLimit: 100}
	mw := NewBudgetMiddleware(cfg, nil, nil, nil)

	ctx := context.WithValue(context.Background(), reqmeta.KeyIDContextKey, "test-key")
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	mw.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr, req)

	if rr.Header().Get("X-Budget-Limit") == "" {
		t.Error("expected X-Budget-Limit header to be set")
	}
	if rr.Header().Get("X-Budget-Remaining") == "" {
		t.Error("expected X-Budget-Remaining header to be set")
	}
}

func TestBudgetMiddleware_Enforcer(t *testing.T) {
	cfg := config.BudgetConfig{Enabled: true}
	mw := NewBudgetMiddleware(cfg, nil, nil, nil)

	enf := mw.Enforcer()
	if enf == nil {
		t.Fatal("expected non-nil Enforcer")
	}
}
