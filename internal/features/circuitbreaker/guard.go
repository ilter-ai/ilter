// Package circuitbreaker provides a circuit-breaking wrapper around redis.Client.
package circuitbreaker

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sony/gobreaker/v2"
)

type Policy int

const (
	FailOpen Policy = iota
	FailClosed
)

type RedisBreaker struct {
	client  *redis.Client
	cb      *gobreaker.CircuitBreaker[*result]
	timeout time.Duration
}

type result struct{}

// NewRedisBreaker returns a RedisBreaker wrapping cl with a per-call timeout and circuit-breaker settings.
func NewRedisBreaker(cl *redis.Client, perCallTimeout time.Duration, breakerSettings gobreaker.Settings) *RedisBreaker {
	bs := breakerSettings
	if bs.Name == "" {
		bs.Name = "redis"
	}
	if bs.MaxRequests == 0 {
		bs.MaxRequests = 3
	}
	if bs.Interval == 0 {
		bs.Interval = 30 * time.Second
	}
	if bs.Timeout == 0 {
		bs.Timeout = 5 * time.Second
	}
	if bs.ReadyToTrip == nil {
		bs.ReadyToTrip = func(c gobreaker.Counts) bool {
			return c.ConsecutiveFailures >= 5
		}
	}
	return &RedisBreaker{
		client:  cl,
		cb:      gobreaker.NewCircuitBreaker[*result](bs),
		timeout: perCallTimeout,
	}
}

func (g *RedisBreaker) Client() *redis.Client { return g.client }

// Do runs fn under a timeout + circuit breaker. degraded=true when Redis was unavailable.
func (g *RedisBreaker) Do(ctx context.Context, fn func(context.Context, *redis.Client) error) (degraded bool) {
	cctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	_, err := g.cb.Execute(func() (*result, error) {
		err := fn(cctx, g.client)
		if err != nil && errors.Is(err, redis.Nil) {
			return &result{}, nil
		}
		if err != nil {
			return nil, err
		}
		return &result{}, nil
	})
	return err != nil
}

func DoWithRedisPolicy[T any](ctx context.Context, g *RedisBreaker, policy Policy, fn func(context.Context, *redis.Client) (T, error)) (val T, degraded bool) {
	degraded = g.Do(ctx, func(cctx context.Context, cl *redis.Client) error {
		v, err := fn(cctx, cl)
		if err != nil {
			return err
		}
		val = v
		return nil
	})
	if degraded && policy == FailClosed {
		return val, true
	}
	return val, degraded
}
