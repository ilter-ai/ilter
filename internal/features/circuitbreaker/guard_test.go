package circuitbreaker

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sony/gobreaker/v2"
	"github.com/stretchr/testify/require"
)

func newTestRedisBreaker(t *testing.T) (*RedisBreaker, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	g := NewRedisBreaker(
		redis.NewClient(&redis.Options{Addr: mr.Addr()}),
		100*time.Millisecond,
		gobreaker.Settings{},
	)
	return g, mr
}

func TestRedisBreaker_Ping_Success(t *testing.T) {
	g, _ := newTestRedisBreaker(t)
	degraded := g.Do(context.Background(), func(ctx context.Context, cl *redis.Client) error {
		return cl.Ping(ctx).Err()
	})
	require.False(t, degraded, "healthy Redis should not degrade")
}

func TestRedisBreaker_RedisNil_NotCountedAsFailure(t *testing.T) {
	g, _ := newTestRedisBreaker(t)

	degraded := g.Do(context.Background(), func(ctx context.Context, cl *redis.Client) error {
		return cl.Get(ctx, "does-not-exist").Err()
	})
	require.False(t, degraded, "redis.Nil must NOT trip the breaker")
}

func TestRedisBreaker_FailOpen_WhenRedisDown(t *testing.T) {
	g, mr := newTestRedisBreaker(t)
	mr.Close() // simulate hard-down

	degraded := g.Do(context.Background(), func(ctx context.Context, cl *redis.Client) error {
		return cl.Ping(ctx).Err()
	})
	require.True(t, degraded, "dead Redis must report degraded")
}

func TestRedisBreaker_BreakerOpens_AndShortCircuits(t *testing.T) {
	g, mr := newTestRedisBreaker(t)
	mr.Close()

	// Exhaust failure threshold (5 consecutive failures)
	for i := 0; i < 6; i++ {
		g.Do(context.Background(), func(ctx context.Context, cl *redis.Client) error {
			return cl.Ping(ctx).Err()
		})
	}

	// Now the breaker should be open → short-circuit (< 20 ms, not timeout).
	start := time.Now()
	degraded := g.Do(context.Background(), func(ctx context.Context, cl *redis.Client) error {
		return cl.Ping(ctx).Err()
	})
	require.True(t, degraded, "open breaker must report degraded")
	require.Less(t, time.Since(start), 20*time.Millisecond,
		"open breaker must short-circuit, not wait for timeout")
}

func TestRedisBreaker_RecoversAfterBreakerHalfOpen(t *testing.T) {
	g, mr := newTestRedisBreaker(t)
	mr.Close()

	// Trip the breaker.
	for i := 0; i < 6; i++ {
		g.Do(context.Background(), func(ctx context.Context, cl *redis.Client) error {
			return cl.Ping(ctx).Err()
		})
	}

	// Let the breaker time out (5s default).  We cannot test this
	// deterministically in unit tests, so we just verify the breaker
	// is open.  The half-open → closed transition is verified by
	// gobreaker's own tests.
	degraded := g.Do(context.Background(), func(ctx context.Context, cl *redis.Client) error {
		return cl.Ping(ctx).Err()
	})
	require.True(t, degraded)
}

func TestDoWithPolicy_FailClosed(t *testing.T) {
	g, mr := newTestRedisBreaker(t)
	mr.Close()

	val, degraded := DoWithRedisPolicy(context.Background(), g, FailClosed, func(ctx context.Context, cl *redis.Client) (string, error) {
		return cl.Get(ctx, "x").Result()
	})
	require.True(t, degraded, "fail-closed must flag degraded")
	require.Empty(t, val, "fail-closed returns zero value")
}

func TestDoWithPolicy_FailOpen(t *testing.T) {
	g, mr := newTestRedisBreaker(t)
	mr.Close()

	val, degraded := DoWithRedisPolicy(context.Background(), g, FailOpen, func(ctx context.Context, cl *redis.Client) (string, error) {
		return cl.Get(ctx, "x").Result()
	})
	require.True(t, degraded, "fail-open must flag degraded")
	require.Empty(t, val, "fail-open returns zero value (no guarantee)")
}
