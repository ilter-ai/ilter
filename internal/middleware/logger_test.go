package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/ilter-ai/ilter/internal/platform/logging"

	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

func TestRequestLoggerMiddleware(t *testing.T) {
	// Setup custom pretty logger to buffer
	var buf bytes.Buffer
	handler := logging.NewPrettyHandler(&buf, slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(handler))

	// Create test handler wrapped in our middleware
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		meta := reqmeta.GetRequestMetadata(r.Context())
		if meta == nil {
			t.Error("Expected reqmeta.RequestLoggingMetadata to be present in context")
			return
		}
		meta.SetKeyID("42")
		meta.SetCacheHit(true)
		meta.SetTokensAndCost(10, 20, 0.000150)
		meta.SetSmartRouted(true, "gpt-4o")

		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("ok"))
	})

	loggerMiddleware := RequestLogger(nextHandler)

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()

	loggerMiddleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("Expected status code 202, got %d", rec.Code)
	}

	logOutput := stripANSI(buf.String())

	// Message is clean short string with method+path (status is a structured attr)
	if !strings.Contains(logOutput, "POST /v1/chat/completions") {
		t.Errorf("Expected log to contain clean request summary, got: %s", logOutput)
	}

	// Structured attributes via SlogAttrs (key-value pairs)
	if !strings.Contains(logOutput, "key_id=42") {
		t.Errorf("Expected log to contain key_id=42, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "prompt_tokens=10") {
		t.Errorf("Expected log to contain prompt_tokens=10, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "completion_tokens=20") {
		t.Errorf("Expected log to contain completion_tokens=20, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "total_tokens=30") {
		t.Errorf("Expected log to contain total_tokens=30, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "cost=0.00015") {
		t.Errorf("Expected log to contain cost=0.00015, got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "interventions=cache_hit,gpt-4o") {
		t.Errorf("Expected log to contain interventions=cache_hit,gpt-4o, got: %s", logOutput)
	}

	// Verify no old-style embedded data in the message
	if strings.Contains(logOutput, "tokens=10+20") {
		t.Errorf("Expected log NOT to contain old-style tokens=10+20 in message, got: %s", logOutput)
	}
	if strings.Contains(logOutput, "Cache:HIT") {
		t.Errorf("Expected log NOT to contain Cache:HIT in message, got: %s", logOutput)
	}
}
