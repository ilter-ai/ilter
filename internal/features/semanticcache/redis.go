package semanticcache

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/ilter-ai/ilter/internal/platform/rediskeys"
)

// initIndex creates the Redis VSS index (idx:cache:v3) for 768D nomic-embed-text vectors.
func (c *SemanticCache) initIndex(dim int) {
	if dim <= 0 {
		dim = 768
	}
	cmd := redis.NewStringCmd(context.Background(), "FT.CREATE", "idx:cache:v3", "ON", "HASH", "PREFIX", "1", "ilter:cache:", "SCHEMA", "vector", "VECTOR", "FLAT", "6", "TYPE", "FLOAT32", "DIM", fmt.Sprintf("%d", dim), "DISTANCE_METRIC", "COSINE", "response", "TEXT")
	if err := c.client.Process(context.Background(), cmd); err != nil && !strings.Contains(err.Error(), "Index already exists") {
		slog.Error("Failed to create Redis VSS index", "error", err)
	}
}

// searchNearest performs a KNN 1 search on the Redis VSS index and returns
// the closest cached response if it is within the similarity threshold.
func (c *SemanticCache) searchNearest(ctx context.Context, emb []float32, threshold float64) (string, float64, bool) {
	maxDistance := 1.0 - threshold
	searchCmd := redis.NewCmd(ctx, "FT.SEARCH", "idx:cache:v3", "*=>[KNN 1 @vector $vec AS score]", "PARAMS", "2", "vec", float32ToByte(emb), "RETURN", "2", "response", "score", "DIALECT", "2")
	if err := c.client.Process(ctx, searchCmd); err != nil {
		slog.Error("FT.SEARCH failed", "err", err)
		return "", 0, false
	}
	return parseSearchResponse(searchCmd.Val(), maxDistance)
}

// store persists an embedding+response in a Redis hash discoverable by VSS index.
func (c *SemanticCache) store(ctx context.Context, emb []float32, response string, ttl time.Duration) error {
	cacheKey := rediskeys.CacheKey()
	if err := c.client.HSet(ctx, cacheKey, map[string]any{
		"vector":   float32ToByte(emb),
		"response": response,
	}).Err(); err != nil {
		return err
	}
	return c.client.Expire(ctx, cacheKey, ttl).Err()
}

// parseSearchResponse extracts the closest cached response within maxDistance.
func parseSearchResponse(val any, maxDistance float64) (string, float64, bool) {
	for _, hit := range extractResults(val) {
		resp, score, ok := extractHit(hit)
		if ok && resp != "" && !math.IsNaN(score) && score <= maxDistance {
			return resp, score, true
		}
	}
	return "", 0, false
}

func extractResults(val any) []any {
	switch v := val.(type) {
	case nil:
		return nil
	case map[string]any:
		r, _ := v["results"].([]any)
		return r
	case map[any]any:
		r, _ := v["results"].([]any)
		return r
	case []any:
		if len(v) <= 2 {
			return nil
		}
		count, ok := v[0].(int64)
		if !ok || count <= 0 {
			return nil
		}
		out := make([]any, 0, count)
		for i := 2; i < len(v); i += 2 {
			if fields, ok := v[i].([]any); ok {
				out = append(out, fields)
			}
		}
		return out
	default:
		return nil
	}
}

func extractHit(item any) (string, float64, bool) {
	switch v := item.(type) {
	case map[string]any:
		extra, _ := v["extra_attributes"].(map[string]any)
		if extra == nil {
			return "", 0, false
		}
		resp, _ := extra["response"].(string)
		return resp, toFloat64(extra["score"]), resp != ""
	case map[any]any:
		extra, _ := v["extra_attributes"].(map[any]any)
		if extra == nil {
			return "", 0, false
		}
		resp, _ := extra["response"].(string)
		return resp, toFloat64(extra["score"]), resp != ""
	case []any:
		return extractRESP2Hit(v)
	default:
		return "", 0, false
	}
}

// extractRESP2Hit handles RESP2 flat field list [key1, val1, key2, val2, ...].
func extractRESP2Hit(fields []any) (string, float64, bool) {
	var resp, scoreStr string
	for i := 0; i+1 < len(fields); i += 2 {
		key, _ := fields[i].(string)
		val, _ := fields[i+1].(string)
		switch key {
		case "response":
			resp = val
		case "score":
			scoreStr = val
		}
	}
	if resp == "" || scoreStr == "" {
		return "", 0, false
	}
	score, err := strconv.ParseFloat(scoreStr, 64)
	if err != nil {
		return "", 0, false
	}
	return resp, score, true
}

func toFloat64(v any) float64 {
	switch vv := v.(type) {
	case float64:
		if math.IsNaN(vv) || math.IsInf(vv, 0) {
			return math.NaN()
		}
		return vv
	case string:
		f, err := strconv.ParseFloat(vv, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return math.NaN()
		}
		return f
	case int64:
		return float64(vv)
	default:
		return math.NaN()
	}
}

func float32ToByte(vec []float32) []byte {
	b := make([]byte, len(vec)*4)
	for i, v := range vec {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(v))
	}
	return b
}
