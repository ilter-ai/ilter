package circuitbreaker

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sony/gobreaker/v2"

	"github.com/ilter-ai/ilter/internal/config"
)

func State(rt http.RoundTripper) string {
	t, ok := rt.(*HTTPBreaker)
	if !ok {
		return "unknown"
	}
	if t.forceOpen.Load() {
		return "open"
	}
	switch t.cb.State() {
	case gobreaker.StateClosed:
		return "closed"
	case gobreaker.StateOpen:
		return "open"
	case gobreaker.StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

func Counts(rt http.RoundTripper) *gobreaker.Counts {
	t, ok := rt.(*HTTPBreaker)
	if !ok {
		return nil
	}
	c := t.cb.Counts()
	return &c
}

func Metrics(rt http.RoundTripper) (totalRequests, totalErrors int64, lastErrorTime, lastSuccessTime *time.Time) {
	t, ok := rt.(*HTTPBreaker)
	if !ok {
		return 0, 0, nil, nil
	}
	return t.Metrics()
}

type HTTPBreaker struct {
	cb        *gobreaker.CircuitBreaker[*http.Response]
	transport http.RoundTripper
	name      string
	cfg       config.CircuitBreakerConfig

	enabled   atomic.Bool
	forceOpen atomic.Bool

	lastErrorTime   time.Time
	lastSuccessTime time.Time
	totalRequests   int64
	totalErrors     int64
	mu              sync.Mutex
}

func NewHTTPBreaker(transport http.RoundTripper, name string, cfg config.CircuitBreakerConfig) *HTTPBreaker {
	// An unset (zero) MaxFailures makes ReadyToTrip's ConsecutiveFailures>=0
	// always true, tripping the breaker fully open after a single failed
	// request — taking every model on that provider down with it. No config
	// path currently sets this per-provider, so default it here.
	maxFailures := cfg.MaxFailures
	if maxFailures <= 0 {
		maxFailures = 5
	}

	st := gobreaker.Settings{
		Name:        name,
		MaxRequests: uint32(cfg.HalfOpenMaxRequests),
		Interval:    0, // zero means clear counts on open/close
		Timeout:     cfg.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= uint32(maxFailures)
		},
	}

	cb := gobreaker.NewCircuitBreaker[*http.Response](st)

	t := &HTTPBreaker{
		cb:        cb,
		transport: transport,
		name:      name,
		cfg:       cfg,
	}
	t.enabled.Store(true)
	return t
}

func (t *HTTPBreaker) RoundTrip(req *http.Request) (*http.Response, error) {
	if !t.enabled.Load() {
		return t.transport.RoundTrip(req)
	}
	if t.forceOpen.Load() {
		return nil, gobreaker.ErrOpenState
	}

	t.mu.Lock()
	t.totalRequests++
	t.mu.Unlock()

	resp, err := t.cb.Execute(func() (*http.Response, error) {
		r, e := t.transport.RoundTrip(req)
		if e != nil {
			t.mu.Lock()
			t.totalErrors++
			t.lastErrorTime = time.Now()
			t.mu.Unlock()
			return nil, e
		}
		if r.StatusCode >= 500 {
			r.Body.Close()
			t.mu.Lock()
			t.totalErrors++
			t.lastErrorTime = time.Now()
			t.mu.Unlock()
			return nil, fmt.Errorf("provider returned status %d", r.StatusCode)
		}
		t.mu.Lock()
		t.lastSuccessTime = time.Now()
		t.mu.Unlock()
		return r, nil
	})

	return resp, err
}

func (t *HTTPBreaker) SetEnabled(v bool) { t.enabled.Store(v) }

func (t *HTTPBreaker) SetForceOpen(v bool) { t.forceOpen.Store(v) }

func (t *HTTPBreaker) Enabled() bool { return t.enabled.Load() }

func (t *HTTPBreaker) Reset() {
	st := gobreaker.Settings{
		Name:        t.name,
		MaxRequests: uint32(t.cfg.HalfOpenMaxRequests),
		Interval:    0,
		Timeout:     t.cfg.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= uint32(t.cfg.MaxFailures)
		},
	}
	t.mu.Lock()
	t.cb = gobreaker.NewCircuitBreaker[*http.Response](st)
	t.totalRequests = 0
	t.totalErrors = 0
	t.lastErrorTime = time.Time{}
	t.lastSuccessTime = time.Time{}
	t.mu.Unlock()
	t.forceOpen.Store(false)
	t.enabled.Store(true)
}

func (t *HTTPBreaker) Metrics() (totalRequests, totalErrors int64, lastErrorTime, lastSuccessTime *time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	var lastErr *time.Time
	if !t.lastErrorTime.IsZero() {
		lastErr = &t.lastErrorTime
	}
	var lastSuc *time.Time
	if !t.lastSuccessTime.IsZero() {
		lastSuc = &t.lastSuccessTime
	}

	return t.totalRequests, t.totalErrors, lastErr, lastSuc
}
