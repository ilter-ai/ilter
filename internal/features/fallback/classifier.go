package fallback

import (
	"errors"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sony/gobreaker/v2"
)

// MaxRetryAfterDuration is the absolute ceiling for retry-after cooldowns.
// Provider headers can return absurd values (e.g. 1.3M seconds ≈ 367h).
// This cap prevents a single 429 from excluding a key for days.
const MaxRetryAfterDuration = time.Hour

// Verdict defines the classification outcome for a failed provider request.
type Verdict int

const (
	// VerdictFatal means a non-retryable error on this candidate (e.g. 400 Bad
	// Request except model-unavailable). The error string is too opaque to
	// prove it's request-wide rather than candidate-specific, so the executor
	// excludes just this candidate and still tries the rest of the fallback chain.
	VerdictFatal Verdict = iota

	// VerdictExcludeKey means rate limit, auth, or quota/billing error on specific key (e.g. 429, 401, 403, 402).
	VerdictExcludeKey

	// VerdictExcludeCandidate means candidate model/provider unavailable (e.g. 404 Not Found, 503 Service Unavailable, model not found).
	VerdictExcludeCandidate

	// VerdictRetrySame means transient network/timeout error on same candidate.
	VerdictRetrySame
)

func (v Verdict) String() string {
	switch v {
	case VerdictFatal:
		return "Fatal"
	case VerdictExcludeKey:
		return "ExcludeKey"
	case VerdictExcludeCandidate:
		return "ExcludeCandidate"
	case VerdictRetrySame:
		return "RetrySame"
	default:
		return "Unknown"
	}
}

// Classify maps HTTP status code and provider error into a Verdict.
func Classify(statusCode int, err error) Verdict {
	v, _ := ClassifyWithHeaders(statusCode, err, nil)
	return v
}

// ClassifyWithHeaders maps HTTP status code, provider error, and response headers into a Verdict and cooldown duration.
func ClassifyWithHeaders(statusCode int, err error, headers http.Header) (Verdict, time.Duration) {
	if statusCode == 0 && err != nil {
		// The breaker is shared across every model routed through the same
		// provider (see provider.NewResilientClient). An open-state rejection
		// here means this candidate was never actually contacted — another
		// model on the same provider tripped it — so it must not be excluded
		// or cooled down as if it had failed itself. Just move to the next
		// candidate; the breaker's own timeout governs recovery.
		if errors.Is(err, gobreaker.ErrOpenState) {
			return VerdictRetrySame, 0
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return VerdictRetrySame, 0
		}
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "connection refused") || strings.Contains(errStr, "reset by peer") {
			return VerdictRetrySame, 0
		}
		return VerdictExcludeCandidate, 5 * time.Minute
	}

	switch statusCode {
	case http.StatusTooManyRequests: // 429 Rate Limit
		return VerdictExcludeKey, ParseRetryAfter(headers)

	case http.StatusPaymentRequired: // 402 Billing/Quota
		return VerdictExcludeKey, 15 * time.Minute

	case http.StatusUnauthorized, http.StatusForbidden: // 401, 403 Invalid / Unauthorized API Key
		// Immediately exclude key for 24h so failover proceeds to the next key without retrying invalid keys
		return VerdictExcludeKey, 24 * time.Hour

	case http.StatusNotFound: // 404
		return VerdictExcludeCandidate, 5 * time.Minute

	case http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusGatewayTimeout: // 503, 502, 504
		return VerdictExcludeCandidate, 5 * time.Minute

	case http.StatusBadRequest: // 400
		if err != nil {
			errStr := strings.ToLower(err.Error())
			if strings.Contains(errStr, "model") || strings.Contains(errStr, "not found") || strings.Contains(errStr, "unavailable") {
				return VerdictExcludeCandidate, 5 * time.Minute
			}
		}
		return VerdictFatal, 0

	default:
		if statusCode >= 500 {
			return VerdictExcludeCandidate, 5 * time.Minute
		}
		return VerdictFatal, 0
	}
}

// ParseRetryAfter parses rate limit reset durations from standard HTTP response headers.
func ParseRetryAfter(h http.Header) time.Duration {
	if h == nil {
		return cappedJitter(20 * time.Second)
	}

	// 1. Standard Retry-After header (RFC 7231 / RFC 9110)
	if v := h.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return cappedJitter(time.Duration(secs) * time.Second)
		}
		if t, err := http.ParseTime(v); err == nil {
			if d := time.Until(t); d > 0 {
				return cappedJitter(d)
			}
		}
	}

	// 2. OpenAI rate limit headers (x-ratelimit-reset-requests / x-ratelimit-reset-tokens)
	for _, key := range []string{"x-ratelimit-reset-requests", "x-ratelimit-reset-tokens", "X-RateLimit-Reset"} {
		if v := h.Get(key); v != "" {
			if dur, err := time.ParseDuration(v); err == nil && dur > 0 {
				return cappedJitter(dur)
			}
			if secs, err := strconv.ParseFloat(v, 64); err == nil && secs > 0 {
				return cappedJitter(time.Duration(secs * float64(time.Second)))
			}
		}
	}

	// 3. Anthropic rate limit headers (anthropic-ratelimit-requests-reset / anthropic-ratelimit-tokens-reset)
	for _, key := range []string{"anthropic-ratelimit-requests-reset", "anthropic-ratelimit-tokens-reset"} {
		if v := h.Get(key); v != "" {
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				if d := time.Until(t); d > 0 {
					return cappedJitter(d)
				}
			}
		}
	}

	return cappedJitter(20 * time.Second)
}

func cappedJitter(d time.Duration) time.Duration {
	parsed := d
	j := applyJitter(d)
	if j > MaxRetryAfterDuration || j < 0 {
		if j > MaxRetryAfterDuration {
			incRetryAfterCapped()
		}
		if j < 0 {
			j = 0
		}
		// Capped keys retry at 80-100% of MaxRetryAfterDuration, not all at exactly 1h
		spread := 0.8 + rand.Float64()*0.2
		capped := time.Duration(float64(MaxRetryAfterDuration) * spread)
		slog.Warn("retry-after capped", "parsed_duration", parsed, "jittered", j, "capped", capped)
		return capped
	}
	return j
}

func applyJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 20 * time.Second
	}
	// Add pseudo-jitter ±10% to prevent thundering herd
	factor := 0.9 + rand.Float64()*0.2
	jitter := time.Duration(float64(d) * factor)
	if jitter <= 0 {
		return d
	}
	return jitter
}
