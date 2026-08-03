package provider

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ilter-ai/ilter/internal/config"
)

func testCircuitBreakerCfg() config.CircuitBreakerConfig {
	// MaxFailures high enough that the breaker never trips mid-test — these
	// tests are about the retry layer, not the breaker.
	return config.CircuitBreakerConfig{MaxFailures: 100, Timeout: time.Second, HalfOpenMaxRequests: 1}
}

// TestNewResilientClient_RetriesOn500ThenSucceeds verifies the retry
// RoundTripper (hashicorp/go-retryablehttp) retries a 500 response and
// returns the eventual 200, and that the POST body survives every retry
// attempt (critical: LLM chat completion requests are POST with a JSON body).
func TestNewResilientClient_RetriesOn500ThenSucceeds(t *testing.T) {
	var attempts atomic.Int32
	var gotBodies []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		body, _ := io.ReadAll(r.Body)
		gotBodies = append(gotBodies, string(body))
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	cli := NewResilientClient(config.ProviderConfig{
		Name:           "test-provider",
		MaxRetries:     3,
		CircuitBreaker: testCircuitBreakerCfg(),
	})

	req, err := http.NewRequest(http.MethodPost, srv.URL, bytes.NewReader([]byte(`{"model":"x"}`)))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := cli.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after retries, got %d", resp.StatusCode)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("expected exactly 3 attempts (2 failures + 1 success), got %d", got)
	}
	for i, b := range gotBodies {
		if b != `{"model":"x"}` {
			t.Errorf("attempt %d: body not replayed correctly, got %q", i+1, b)
		}
	}
}

// TestNewResilientClient_GivesUpAfterMaxRetries verifies the client retries
// exactly MaxRetries times then surfaces the failure. The circuit breaker
// (HTTPBreaker) turns any >=500 response into an error itself — see
// circuitbreaker.HTTPBreaker.RoundTrip — so a real 503 is never observed as a
// plain *http.Response here; asserting on that conversion is the point: it
// proves the retry layer handed the breaker the *real*, final status rather
// than a synthesized "giving up after N attempts" error (which would have
// hidden the actual status/Retry-After from the fallback classifier).
func TestNewResilientClient_GivesUpAfterMaxRetries(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cli := NewResilientClient(config.ProviderConfig{
		Name:           "test-provider-giveup",
		MaxRetries:     2,
		CircuitBreaker: testCircuitBreakerCfg(),
	})

	_, err := cli.Get(srv.URL)
	if err == nil {
		t.Fatal("expected an error once retries are exhausted on a persistent 503")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("expected error to reference the real status 503, got: %v", err)
	}
	// 1 initial attempt + 2 retries = 3.
	if got := attempts.Load(); got != 3 {
		t.Fatalf("expected exactly 3 attempts (1 + MaxRetries=2), got %d", got)
	}
}

// TestNewResilientClient_NoRetryWhenMaxRetriesZero verifies that a
// MaxRetries=0 config makes exactly one attempt (retry layer elided).
func TestNewResilientClient_NoRetryWhenMaxRetriesZero(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cli := NewResilientClient(config.ProviderConfig{
		Name:           "test-provider-noretry",
		MaxRetries:     0,
		CircuitBreaker: testCircuitBreakerCfg(),
	})

	_, err := cli.Get(srv.URL)
	if err == nil {
		t.Fatal("expected an error on a 500 response")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("expected exactly 1 attempt with MaxRetries=0, got %d", got)
	}
}
