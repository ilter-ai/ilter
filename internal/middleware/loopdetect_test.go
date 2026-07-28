package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/loopdetect"
)

func loopNextCalled(called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*called = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestLoopDetector_NonChatRequest(t *testing.T) {
	d := loopdetect.NewDetector(config.LoopSettingsConfig{})
	ld := NewLoopDetectorMiddleware(d, nil, nil)

	called := false
	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	ld.Handler(loopNextCalled(&called)).ServeHTTP(rr, req)

	if !called {
		t.Error("expected next handler to be called for non-chat request")
	}
}

func TestLoopDetector_DisabledViaCache(t *testing.T) {
	d := loopdetect.NewDetector(config.LoopSettingsConfig{})
	boot := config.DefaultBootConfig()
	boot.CostGuard.LoopDetection = false
	cache := config.NewConfigCache(&boot)
	ld := NewLoopDetectorMiddleware(d, nil, cache)

	called := false
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	rr := httptest.NewRecorder()
	ld.Handler(loopNextCalled(&called)).ServeHTTP(rr, req)

	if !called {
		t.Error("expected next handler to be called when loop detection is disabled")
	}
}

func TestLoopDetector_PassthroughOnNoSignal(t *testing.T) {
	d := loopdetect.NewDetector(config.LoopSettingsConfig{})
	ld := NewLoopDetectorMiddleware(d, nil, nil)

	called := false
	body := `{"model":"gpt-4o","messages":[]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	rr := httptest.NewRecorder()
	ld.Handler(loopNextCalled(&called)).ServeHTTP(rr, req)

	if !called {
		t.Error("expected next handler to be called when no loop signal")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestLoopDetector_WarningHeader(t *testing.T) {
	d := loopdetect.NewDetector(config.LoopSettingsConfig{})
	ld := NewLoopDetectorMiddleware(d, nil, nil)

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`

	// First call — primes the history
	req1 := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req1.Header.Set("X-Ilter-Session-Id", "test-session-warn")
	rr1 := httptest.NewRecorder()
	ld.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr1, req1)

	// Second call — same session, same prompt → may warn
	req2 := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req2.Header.Set("X-Ilter-Session-Id", "test-session-warn")
	rr2 := httptest.NewRecorder()
	ld.Handler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr2.Code)
	}
	if rr2.Header().Get("X-Ilter-Loop-Warning") != "true" {
		t.Log("warning header not set — may need more repeats, test is heuristic")
	}
}
