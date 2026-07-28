package provider

import (
	"net/http"
	"time"

	"github.com/hashicorp/go-retryablehttp"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/circuitbreaker"
)

// NewResilientClient creates an *http.Client whose transport chain is:
//
//	circuitbreaker.HTTPBreaker( retryablehttp.RoundTripper( baseTransport ) )
//
// Retry sits inside the breaker so the breaker records a single failure only
// when all retries are exhausted. MaxRetries is read from cfg; if zero the
// retry RoundTripper is elided.
//
// Retries are handled by hashicorp/go-retryablehttp: exponential backoff with
// jitter, Retry-After-aware (429/503), retries on 429 and 5xx (except 501).
// Its own logging is disabled (Logger: nil) — this codebase logs via slog at
// the circuit breaker/fallback layers, not per retry attempt.
func NewResilientClient(cfg config.ProviderConfig) *http.Client {
	// A stalled connection (provider accepts the TCP connection but never
	// sends a response) hangs forever without this — http.Client.Timeout
	// would also bound it, but that covers the *entire* body read too, which
	// would truncate legitimately long streaming completions. Bounding only
	// "time to first byte" catches true hangs without that risk.
	baseTransport := http.DefaultTransport.(*http.Transport).Clone()
	baseTransport.ResponseHeaderTimeout = 60 * time.Second
	var base http.RoundTripper = baseTransport

	if cfg.MaxRetries > 0 {
		rc := retryablehttp.NewClient()
		rc.HTTPClient.Transport = baseTransport
		rc.RetryMax = cfg.MaxRetries
		rc.RetryWaitMin = 200 * time.Millisecond
		rc.RetryWaitMax = 5 * time.Second
		rc.Logger = nil
		// Once retries are exhausted, return the last real response/error as-is
		// instead of a synthesized "giving up after N attempts" error — the
		// circuit breaker and fallback classifier need the actual status code
		// (and headers, for Retry-After) to make correct decisions.
		rc.ErrorHandler = retryablehttp.PassthroughErrorHandler
		base = &retryablehttp.RoundTripper{Client: rc}
	}

	transport := circuitbreaker.NewHTTPBreaker(base, cfg.Name, cfg.CircuitBreaker)
	cli := &http.Client{Transport: transport}
	// Generous backstop for the full request (including streaming body reads)
	// so a connection that goes dead mid-stream doesn't hang forever either —
	// ResponseHeaderTimeout above already handles the common "never responds
	// at all" case without risking this cutting off long completions.
	cli.Timeout = 10 * time.Minute
	if cfg.Timeout > 0 {
		cli.Timeout = cfg.Timeout
	}
	return cli
}
