package circuitbreaker

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/sony/gobreaker/v2"
	"github.com/stretchr/testify/assert"

	"github.com/ilter-ai/ilter/internal/config"
)

type mockRoundTripper struct {
	roundTripFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTripFunc(req)
}

func TestCircuitBreaker_Flow(t *testing.T) {
	cfg := config.CircuitBreakerConfig{
		MaxFailures:         3,
		Timeout:             50 * time.Millisecond,
		HalfOpenMaxRequests: 1,
	}

	callCount := 0
	shouldFail := true

	mockTrans := &mockRoundTripper{
		roundTripFunc: func(_ *http.Request) (*http.Response, error) {
			callCount++
			if shouldFail {
				return nil, errors.New("transport error")
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       http.NoBody,
			}, nil
		},
	}

	transport := NewHTTPBreaker(mockTrans, "test-breaker", cfg)

	req, _ := http.NewRequestWithContext(context.Background(), "POST", "http://localhost", nil)

	// 1. Successive failures to trigger Open state
	for i := 0; i < 3; i++ {
		resp, err := transport.RoundTrip(req)
		assert.Error(t, err)
		assert.Nil(t, resp)
	}

	// The 4th call should immediately return ErrOpenState without calling mockTrans
	currentCalls := callCount
	resp, err := transport.RoundTrip(req)
	assert.Error(t, err)
	assert.Equal(t, gobreaker.ErrOpenState, err)
	assert.Nil(t, resp)
	assert.Equal(t, currentCalls, callCount) // mockTrans was NOT called

	// 2. Wait for Timeout to transition to Half-Open state
	time.Sleep(60 * time.Millisecond)

	// 3. Make a call in Half-Open state. It should call the mockTrans.
	// Let's test success in Half-Open to transition to Closed.
	shouldFail = false
	resp, err = transport.RoundTrip(req)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, currentCalls+1, callCount)

	// Circuit should be Closed now. Failures shouldn't immediately open it until threshold is reached.
	resp, err = transport.RoundTrip(req)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestCircuitBreaker_5xxStatus(t *testing.T) {
	cfg := config.CircuitBreakerConfig{
		MaxFailures:         2,
		Timeout:             10 * time.Millisecond,
		HalfOpenMaxRequests: 1,
	}

	mockTrans := &mockRoundTripper{
		roundTripFunc: func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       http.NoBody,
			}, nil
		},
	}

	transport := NewHTTPBreaker(mockTrans, "test-breaker-5xx", cfg)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "http://localhost", nil)

	// 500 status should trip the breaker
	for i := 0; i < 2; i++ {
		resp, err := transport.RoundTrip(req)
		assert.Error(t, err)
		assert.Nil(t, resp)
	}

	resp, err := transport.RoundTrip(req)
	assert.Equal(t, gobreaker.ErrOpenState, err)
	assert.Nil(t, resp)
}
