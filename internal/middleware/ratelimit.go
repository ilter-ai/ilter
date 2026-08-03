package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/circuitbreaker"
	"github.com/ilter-ai/ilter/internal/features/ratelimit"
	"github.com/ilter-ai/ilter/internal/model"

	"github.com/ilter-ai/ilter/internal/platform/rediskeys"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

// RateLimitMiddleware is the thin HTTP middleware adapter wrapping ratelimit.RateLimiter.
type RateLimitMiddleware struct {
	limiter *ratelimit.RateLimiter
}

// NewRateLimitMiddleware creates a new RateLimitMiddleware adapter.
func NewRateLimitMiddleware(cfg *config.RateLimitConfig, g *circuitbreaker.RedisBreaker, cfgCache *config.Cache) (*RateLimitMiddleware, error) {
	limiter, err := ratelimit.NewRateLimiter(cfg, g, cfgCache)
	if err != nil {
		return nil, err
	}
	return &RateLimitMiddleware{limiter: limiter}, nil
}

// Limiter returns the underlying ratelimit.RateLimiter core.
func (m *RateLimitMiddleware) Limiter() *ratelimit.RateLimiter {
	return m.limiter
}

const defaultRetryAfter = 60

// Handler returns the Chi-compatible HTTP middleware handler.
func (m *RateLimitMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		enabled := m.limiter.Cfg.Enabled
		if m.limiter.CfgCache != nil {
			enabled = config.IsEnabled(m.limiter.CfgCache, "rate_limit")
		}
		if !enabled {
			next.ServeHTTP(w, r)
			return
		}

		keyID := reqmeta.GetKeyID(r.Context())
		if keyID == "admin" && m.limiter.Cfg.AdminBypass {
			next.ServeHTTP(w, r)
			return
		}
		if keyID == "" {
			keyID = "anonymous"
		}

		if m.limiter.Guard == nil {
			next.ServeHTTP(w, r)
			return
		}

		now := time.Now()
		meta := reqmeta.GetRequestMetadata(r.Context())

		keyLimit := int64(m.limiter.Cfg.DefaultRPM)
		if rateLimitVal := r.Context().Value(reqmeta.APIKeyRateLimitContextKey); rateLimitVal != nil {
			if l, ok := rateLimitVal.(int); ok && l > 0 {
				keyLimit = int64(l)
			}
		}

		var limited bool
		var activeLimit int64
		retryAfter := 0

		ctx := r.Context()
		minuteKey := rediskeys.RateLimitKey(keyID, now)
		keyCount, err := m.limiter.IncrementAndGetCount(ctx, minuteKey)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		activeLimit = keyLimit
		if keyCount > keyLimit {
			limited = true
		}

		if userID := reqmeta.GetUserID(ctx); userID != nil {
			userLimit := m.limiter.GetUserRateLimit(ctx, *userID)
			if userLimit > 0 {
				userMinuteKey := rediskeys.UserRateLimitCounterKey(*userID, now)
				userCount, err := m.limiter.IncrementAndGetCount(ctx, userMinuteKey)
				if err == nil {
					if int64(userLimit) < activeLimit {
						activeLimit = int64(userLimit)
					}
					if userCount > int64(userLimit) {
						limited = true
						if ra := m.limiter.GetUserRetryAfter(ctx, *userID); ra > 0 && ra > retryAfter {
							retryAfter = ra
						}
					}
				}
			}
		}

		for _, groupID := range reqmeta.GetGroupIDs(ctx) {
			groupLimit := m.limiter.GetGroupRateLimit(ctx, groupID)
			if groupLimit > 0 {
				groupMinuteKey := rediskeys.GroupRateLimitCounterKey(groupID, now)
				groupCount, err := m.limiter.IncrementAndGetCount(ctx, groupMinuteKey)
				if err == nil {
					if int64(groupLimit) < activeLimit {
						activeLimit = int64(groupLimit)
					}
					if groupCount > int64(groupLimit) {
						limited = true
						if ra := m.limiter.GetGroupRetryAfter(ctx, groupID); ra > 0 && ra > retryAfter {
							retryAfter = ra
						}
					}
				}
			}
		}

		w.Header().Set("X-RateLimit-Limit", strconv.FormatInt(activeLimit, 10))
		remaining := max(activeLimit-keyCount, 0)
		w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(remaining, 10))

		if retryAfter <= 0 {
			retryAfter = defaultRetryAfter
		}

		if limited {
			if meta != nil {
				meta.SetRateLimited(true)
			}
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			model.WriteJSONError(w, http.StatusTooManyRequests, "rate_limit_exceeded", "Rate limit exceeded")
			return
		}

		next.ServeHTTP(w, r)
	})
}
