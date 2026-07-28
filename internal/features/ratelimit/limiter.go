package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/circuitbreaker"
	"github.com/ilter-ai/ilter/internal/platform/rediskeys"
)

type RateLimiter struct {
	Cfg      *config.RateLimitConfig
	Guard    *circuitbreaker.RedisBreaker
	CfgCache *config.Cache
}

func NewRateLimiter(cfg *config.RateLimitConfig, g *circuitbreaker.RedisBreaker, cfgCache *config.Cache) (*RateLimiter, error) {
	return &RateLimiter{
		Cfg:      cfg,
		Guard:    g,
		CfgCache: cfgCache,
	}, nil
}

func (rl *RateLimiter) IncrementAndGetCount(ctx context.Context, key string) (int64, error) {
	var val int64
	degraded := rl.Guard.Do(ctx, func(c context.Context, rdb *redis.Client) error {
		pipe := rdb.TxPipeline()
		incr := pipe.Incr(c, key)
		pipe.Expire(c, key, 2*time.Minute)
		_, err := pipe.Exec(c)
		if err != nil {
			return err
		}
		val = incr.Val()
		return nil
	})
	if degraded {
		return 0, fmt.Errorf("redis unavailable")
	}
	return val, nil
}

// Returns 0 if no config is set.
func (rl *RateLimiter) GetUserRateLimit(ctx context.Context, userID int) int {
	cfgKey := rediskeys.UserRateLimitConfigKey(userID)
	var val int
	degraded := rl.Guard.Do(ctx, func(c context.Context, rdb *redis.Client) error {
		v, err := rdb.Get(c, cfgKey).Int()
		if err != nil {
			return err
		}
		val = v
		return nil
	})
	if degraded {
		return 0
	}
	return val
}

// Returns 0 if no config is set.
func (rl *RateLimiter) GetGroupRateLimit(ctx context.Context, groupID int) int {
	cfgKey := rediskeys.GroupRateLimitConfigKey(groupID)
	var val int
	degraded := rl.Guard.Do(ctx, func(c context.Context, rdb *redis.Client) error {
		v, err := rdb.Get(c, cfgKey).Int()
		if err != nil {
			return err
		}
		val = v
		return nil
	})
	if degraded {
		return 0
	}
	return val
}

// GetUserRetryAfter returns the retry-after seconds for the given user ID.
// Returns 0 if no config is set (caller uses defaultRetryAfter).
func (rl *RateLimiter) GetUserRetryAfter(ctx context.Context, userID int) int {
	key := rediskeys.UserRateLimitRetryAfterKey(userID)
	var val int
	degraded := rl.Guard.Do(ctx, func(c context.Context, rdb *redis.Client) error {
		v, err := rdb.Get(c, key).Int()
		if err != nil {
			return err
		}
		val = v
		return nil
	})
	if degraded {
		return 0
	}
	return val
}

// Returns 0 if no config is set (caller uses defaultRetryAfter).
func (rl *RateLimiter) GetGroupRetryAfter(ctx context.Context, groupID int) int {
	key := rediskeys.GroupRateLimitRetryAfterKey(groupID)
	var val int
	degraded := rl.Guard.Do(ctx, func(c context.Context, rdb *redis.Client) error {
		v, err := rdb.Get(c, key).Int()
		if err != nil {
			return err
		}
		val = v
		return nil
	})
	if degraded {
		return 0
	}
	return val
}
