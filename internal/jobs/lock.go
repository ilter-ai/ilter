package jobs

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// LockProvider provides distributed locking for job execution.
//
// Locks are BEST-EFFORT, not authoritative. The authoritative exactly-once
// guarantee comes from the job_activations UNIQUE(trigger_id, idem_key)
// constraint in SQLite — that works without Redis and across any number of
// replicas. The lock here is a fail-fast optimisation: it prevents a replica
// from doing expensive work (LLM calls, MCP tool executions) when another
// replica has already claimed the slot.
//
// On Redis error, implementations should fall open (returning true) so jobs
// don't stall. Lock holders MUST call Unlock to release the lock promptly.
type LockProvider interface {
	TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error)
	Unlock(ctx context.Context, key string) error
}

type RedisLock struct {
	rdb *redis.Client
}

func NewRedisLock(rdb *redis.Client) *RedisLock { return &RedisLock{rdb: rdb} }

func (l *RedisLock) TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	ok, err := l.rdb.SetNX(ctx, key, "1", ttl).Result()
	if err != nil {
		// Redis unavailable — fall open so jobs don't stall
		return true, err
	}
	return ok, nil
}

func (l *RedisLock) Unlock(ctx context.Context, key string) error {
	return l.rdb.Del(ctx, key).Err()
}

// LocalLock implements LockProvider using an in-memory keyed lock map.
// Only effective within a single process. TTL is not enforced —
// callers must always call Unlock to release.
//
// Safe for concurrent access; uses a sync.Mutex for the internal map.
type LocalLock struct {
	mu    sync.Mutex
	locks map[string]bool // key → held
}

// NewLocalLock creates a new LocalLock.
func NewLocalLock() *LocalLock {
	return &LocalLock{locks: make(map[string]bool)}
}

// TryLock attempts to acquire the lock for the given key.
func (l *LocalLock) TryLock(_ context.Context, key string, _ time.Duration) (bool, error) {
	l.mu.Lock()
	if l.locks[key] {
		l.mu.Unlock()
		return false, nil
	}
	l.locks[key] = true
	l.mu.Unlock()
	return true, nil
}

// Unlock releases the lock for the given key.
func (l *LocalLock) Unlock(_ context.Context, key string) error {
	l.mu.Lock()
	delete(l.locks, key)
	l.mu.Unlock()
	return nil
}
