package budget

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/sony/gobreaker/v2"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db/dbtest"
	"github.com/ilter-ai/ilter/internal/features/circuitbreaker"

	"github.com/ilter-ai/ilter/internal/platform/rediskeys"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

// newTestRedis spins up an in-process miniredis and returns a real go-redis
// client wired to it. Cleanup is registered on t.
func newTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	return redis.NewClient(&redis.Options{Addr: mr.Addr()}), mr
}

// newTestGuard wraps a real redis.Client in a Guard for testing.
func newTestGuard(t *testing.T, cl *redis.Client) *circuitbreaker.RedisBreaker {
	t.Helper()
	return circuitbreaker.NewRedisBreaker(cl, time.Second, gobreaker.Settings{})
}

func TestEnforcer_RecordUsage_NoRedis(t *testing.T) {
	be := NewEnforcer(config.BudgetConfig{Enabled: true}, nil, nil, nil)

	err := be.RecordUsage(context.Background(), "1", 5.0)
	if err != nil {
		t.Errorf("RecordUsage should be no-op when Redis is nil, got error: %v", err)
	}
}

func TestEnforcer_RecordUsage_Disabled(t *testing.T) {
	be := NewEnforcer(config.BudgetConfig{Enabled: false}, nil, nil, nil)

	err := be.RecordUsage(context.Background(), "1", 5.0)
	if err != nil {
		t.Errorf("RecordUsage should be no-op when disabled, got error: %v", err)
	}
}

func TestEnforcer_RecordUsage_ZeroKeyID(t *testing.T) {
	be := NewEnforcer(config.BudgetConfig{Enabled: true}, nil, nil, nil)

	err := be.RecordUsage(context.Background(), "", 5.0)
	if err != nil {
		t.Errorf("RecordUsage should be no-op when keyID is 0, got error: %v", err)
	}
}

func TestEnforcer_RedisIntegration_RecordUsage(t *testing.T) {
	rdb, _ := newTestRedis(t)
	ctx := context.Background()

	t.Run("RecordUsage stores cost correctly", func(t *testing.T) {
		store := dbtest.New(t)

		be := NewEnforcer(config.BudgetConfig{Enabled: true, DefaultMonthlyLimit: 1000, DefaultDailyLimit: 500}, newTestGuard(t, rdb), store, nil)

		recordCtx := context.WithValue(ctx, reqmeta.APIKeyBudgetContextKey, 1000.0)
		recordCtx = context.WithValue(recordCtx, reqmeta.APIKeyDailyLimitContextKey, 500.0)

		require.NoError(t, be.RecordUsage(recordCtx, "1", 10.5))
		require.NoError(t, be.RecordUsage(recordCtx, "1", 5.5))

		now := time.Now()
		monthKey := rediskeys.BudgetKey("1", now)
		val, err := rdb.Get(ctx, monthKey).Result()
		require.NoError(t, err)
		require.Equal(t, "16000000", val, "expected monthly budget in micro-dollars (16.0 USD)")
	})
}
