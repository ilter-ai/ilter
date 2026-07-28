package cooldown

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ilter-ai/ilter/internal/features/circuitbreaker"
)

// Candidate identifies a unique (provider, model, keyID) tuple for cooldown tracking.
type Candidate struct {
	Provider    string
	Model       string
	KeyID       string
	APIKey      string
	IsDowngrade bool // true when this candidate is from a ModelDowngrade fallback
}

// Key returns a consistent string key for the candidate using | as delimiter.
func (c Candidate) Key() string {
	return c.Provider + "|" + c.Model + "|" + c.KeyID
}

// RedisKey returns the Redis key format for multi-pod deployment.
func (c Candidate) RedisKey() string {
	return fmt.Sprintf("ilter:cooldown:%s:%s:%s", c.Provider, c.Model, c.KeyID)
}

// Store defines the interface for cooldown state management across pods.
type Store interface {
	// InCooldown returns true if the candidate is currently in cooldown.
	InCooldown(ctx context.Context, c Candidate) bool

	// SetCooldown sets a cooldown for the candidate with the given duration.
	SetCooldown(ctx context.Context, c Candidate, d time.Duration)

	// ClearCooldown removes the cooldown for a candidate manually.
	ClearCooldown(ctx context.Context, c Candidate)

	// GetCooldowns returns a map of candidate key -> expiry time for active cooldowns.
	GetCooldowns(ctx context.Context) map[string]time.Time
}

// RedisStore is a multi-pod, Redis-backed implementation of Store using Redis key TTLs.
type RedisStore struct {
	client   *redis.Client
	guard    *circuitbreaker.RedisBreaker
	fallback *InMemoryStore
}

// NewRedisStore creates a new multi-pod RedisStore.
func NewRedisStore(client *redis.Client, guard *circuitbreaker.RedisBreaker) *RedisStore {
	return &RedisStore{
		client:   client,
		guard:    guard,
		fallback: NewInMemoryStore(),
	}
}

// InCooldown checks if the candidate is in cooldown in Redis or local fallback.
func (s *RedisStore) InCooldown(ctx context.Context, c Candidate) bool {
	if s.client == nil {
		return s.fallback.InCooldown(ctx, c)
	}

	key := c.RedisKey()
	var inCooldown bool

	if s.guard != nil {
		degraded := s.guard.Do(ctx, func(c context.Context, rdb *redis.Client) error {
			exists, e := rdb.Exists(c, key).Result()
			if e == nil && exists > 0 {
				inCooldown = true
			}
			return e
		})
		if degraded {
			return s.fallback.InCooldown(ctx, c)
		}
	} else {
		exists, e := s.client.Exists(ctx, key).Result()
		if e != nil {
			return s.fallback.InCooldown(ctx, c)
		}
		inCooldown = exists > 0
	}

	return inCooldown
}

// SetCooldown sets the cooldown expiry in Redis and local fallback.
func (s *RedisStore) SetCooldown(ctx context.Context, c Candidate, d time.Duration) {
	s.fallback.SetCooldown(ctx, c, d)
	if s.client == nil {
		return
	}

	key := c.RedisKey()
	if s.guard != nil {
		if degraded := s.guard.Do(ctx, func(c context.Context, rdb *redis.Client) error {
			return rdb.Set(c, key, "cooldown", d).Err()
		}); degraded {
			slog.Warn("cooldown set degraded in Redis (guard)", "key", key)
		}
	} else {
		if err := s.client.Set(ctx, key, "cooldown", d).Err(); err != nil {
			slog.Error("failed to set cooldown in Redis", "error", err)
		}
	}
}

// ClearCooldown removes the cooldown for a candidate from Redis and local fallback.
func (s *RedisStore) ClearCooldown(ctx context.Context, c Candidate) {
	s.fallback.ClearCooldown(ctx, c)
	if s.client == nil {
		return
	}

	key := c.RedisKey()
	if s.guard != nil {
		if degraded := s.guard.Do(ctx, func(c context.Context, rdb *redis.Client) error {
			return rdb.Del(c, key).Err()
		}); degraded {
			slog.Warn("cooldown clear degraded in Redis (guard)", "key", key)
		}
	} else {
		if err := s.client.Del(ctx, key).Err(); err != nil {
			slog.Error("failed to clear cooldown in Redis", "error", err)
		}
	}
}

// GetCooldowns returns all active cooldown entries from local fallback.
func (s *RedisStore) GetCooldowns(ctx context.Context) map[string]time.Time {
	return s.fallback.GetCooldowns(ctx)
}

// InMemoryStore is a thread-safe in-memory implementation of Store for fallback/testing.
type InMemoryStore struct {
	mu   sync.RWMutex
	data map[string]time.Time // key -> expiry time
}

// NewInMemoryStore creates a new InMemoryStore.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		data: make(map[string]time.Time),
	}
}

// InCooldown checks if the candidate is in cooldown.
func (s *InMemoryStore) InCooldown(_ context.Context, c Candidate) bool {
	key := c.Key()

	s.mu.RLock()
	expiry, ok := s.data[key]
	s.mu.RUnlock()

	if !ok {
		return false
	}

	if time.Now().Before(expiry) {
		return true
	}

	s.mu.Lock()
	if exp, stillOk := s.data[key]; stillOk && time.Now().Before(exp) {
		s.mu.Unlock()
		return true
	}
	delete(s.data, key)
	s.mu.Unlock()

	return false
}

// SetCooldown sets the cooldown expiry for the candidate.
func (s *InMemoryStore) SetCooldown(_ context.Context, c Candidate, d time.Duration) {
	key := c.Key()
	expiry := time.Now().Add(d)

	s.mu.Lock()
	s.data[key] = expiry
	s.mu.Unlock()
}

// ClearCooldown removes the cooldown for a candidate.
func (s *InMemoryStore) ClearCooldown(_ context.Context, c Candidate) {
	key := c.Key()
	s.mu.Lock()
	delete(s.data, key)
	s.mu.Unlock()
}

// GetCooldowns returns all active non-expired cooldown entries.
func (s *InMemoryStore) GetCooldowns(_ context.Context) map[string]time.Time {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make(map[string]time.Time)
	for k, exp := range s.data {
		if now.Before(exp) {
			result[k] = exp
		} else {
			delete(s.data, k)
		}
	}
	return result
}
