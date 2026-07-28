package semanticcache

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ilter-ai/ilter/internal/config"
)

type CacheMode string

const (
	CacheModeSemantic CacheMode = "semantic" // VSS with embedder
	CacheModeExact    CacheMode = "exact"    // SHA256 exact match
	CacheModeDisabled CacheMode = "disabled" // cache not available
)

const exactKeyPrefix = "ilter:cache:exact:"

type SemanticCache struct {
	cfg      config.CacheConfig
	client   *redis.Client
	embedder *OllamaEmbedder // nil = exact-only mode
}

func New(cfg config.CacheConfig, client *redis.Client, ollamaURL string) *SemanticCache {
	sc := &SemanticCache{
		cfg:    cfg,
		client: client,
	}

	if ollamaURL != "" {
		sc.embedder = NewOllamaEmbedder(ollamaURL)
	}

	if cfg.Enabled && client != nil && sc.embedder != nil {
		sc.initIndex(sc.embedder.Dim())
	}

	return sc
}

func (c *SemanticCache) Mode() CacheMode {
	if !c.cfg.Enabled || c.client == nil {
		return CacheModeDisabled
	}
	if c.embedder != nil {
		return CacheModeSemantic
	}
	return CacheModeExact
}

func (c *SemanticCache) GetFull(ctx context.Context, embedText string, exactKey string) (response string, score float64, found bool) {
	if c.client == nil {
		return "", 0, false
	}

	if c.embedder == nil {
		return c.exactGet(ctx, exactKey)
	}

	emb, err := c.embedder.Embed(ctx, embedText)
	if err != nil {
		slog.Warn("Embedding failed, falling back to exact match", "error", err)
		return c.exactGet(ctx, exactKey)
	}

	threshold := c.cfg.SimilarityThreshold
	if threshold == 0 {
		threshold = 0.70
	}

	resp, score, found := c.searchNearest(ctx, emb, threshold)
	if found {
		slog.Debug("semantic cache hit", "score", score, "threshold", threshold)
		return resp, score, found
	}
	return c.exactGet(ctx, exactKey)
}

func (c *SemanticCache) SetFull(ctx context.Context, embedText string, exactKey string, response string) error {
	if c.client == nil {
		return nil
	}

	if err := c.exactSet(ctx, exactKey, response); err != nil {
		slog.Warn("Failed to store exact cache entry", "error", err)
	}

	if c.embedder != nil {
		if c.cfg.MaxEntries > 0 {
			count, err := c.entryCount(ctx)
			if err == nil && count >= c.cfg.MaxEntries {
				slog.Warn("semantic cache at capacity, skipping VSS store",
					"count", count, "max", c.cfg.MaxEntries)
				return nil // exactSet already stored the exact-match entry
			}
		}

		emb, err := c.embedder.Embed(ctx, embedText)
		if err != nil {
			slog.Warn("Semantic cache skip: embedding failed", "error", err)
			return nil // exactSet already stored the exact-match entry
		}

		ttl := c.cfg.TTL
		if ttl <= 0 {
			ttl = 1 * time.Hour
		}

		return c.store(ctx, emb, response, ttl)
	}

	return nil
}

func cacheKey(prompt string) string {
	return exactKeyPrefix + fmt.Sprintf("%x", sha256.Sum256([]byte(prompt)))
}

// countEntriesScript atomically counts ilter:cache:* keys in Redis via non-blocking SCAN.
// Returns the exact count, not an estimate.
const countEntriesScript = `
local cursor = '0'
local count = 0
repeat
    local result = redis.call('SCAN', cursor, 'MATCH', 'ilter:cache:*', 'COUNT', '5000')
    cursor = result[1]
    count = count + #result[2]
until cursor == '0'
return count
`

var countEntriesCmd = redis.NewScript(countEntriesScript)

// entryCount returns the current number of cached entries matching ilter:cache:*.
// Returns -1 if Redis is unavailable.
func (c *SemanticCache) entryCount(ctx context.Context) (int, error) {
	if c.client == nil {
		return -1, fmt.Errorf("redis client is nil")
	}
	n, err := countEntriesCmd.Run(ctx, c.client, nil).Int()
	if err != nil {
		return -1, fmt.Errorf("entry count: %w", err)
	}
	return n, nil
}

func (c *SemanticCache) exactGet(ctx context.Context, prompt string) (string, float64, bool) {
	resp, err := c.client.Get(ctx, cacheKey(prompt)).Result()
	if err != nil {
		return "", 0, false
	}
	return resp, 0, true
}

func (c *SemanticCache) exactSet(ctx context.Context, prompt, response string) error {
	ttl := c.cfg.TTL
	if ttl <= 0 {
		ttl = 1 * time.Hour
	}
	return c.client.Set(ctx, cacheKey(prompt), response, ttl).Err()
}
