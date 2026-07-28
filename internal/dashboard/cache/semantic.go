package cache

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"

	"github.com/ilter-ai/ilter/internal/model"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/middleware"
)

type topCachedQuery struct {
	QueryPreview string  `json:"query_preview"`
	Model        string  `json:"model"`
	HitCount     int     `json:"hit_count"`
	LastAccessed string  `json:"last_accessed"`
	AvgLatency   float64 `json:"avg_latency"`
}

type cacheHourlyPoint struct {
	Time   string `json:"time"`
	Hits   int    `json:"hits"`
	Misses int    `json:"misses"`
}

type semanticCacheSummaryResponse struct {
	CacheHits24h        int                `json:"cache_hits_24h"`
	CacheMisses24h      int                `json:"cache_misses_24h"`
	HitRatePct          float64            `json:"hit_rate_pct"`
	CacheSizeEntries    int                `json:"cache_size_entries"`
	CacheSizeMB         float64            `json:"cache_size_mb"`
	AvgLatencySavedMs   float64            `json:"avg_latency_saved_ms"`
	RedisConnected      bool               `json:"redis_connected"`
	RedisError          string             `json:"redis_error,omitempty"`
	Mode                string             `json:"mode"`
	SimilarityThreshold float64            `json:"similarity_threshold"`
	TTLSeconds          int                `json:"ttl_seconds"`
	TopQueries          []topCachedQuery   `json:"top_queries"`
	HourlyData          []cacheHourlyPoint `json:"hourly_data"`
}

func (h *Handler) HandleSemanticCacheSummary(w http.ResponseWriter, r *http.Request) {
	db := h.store.DB
	resp := &semanticCacheSummaryResponse{
		TopQueries: []topCachedQuery{},
		HourlyData: []cacheHourlyPoint{},
	}

	var cacheHits, cacheMisses int
	var avgHitLatency, avgMissLatency float64

	err := db.QueryRow(`
		SELECT COUNT(*) FROM audit_log
		WHERE cache_hit = 1 AND timestamp >= datetime('now', '-1 day')
	`).Scan(&cacheHits)
	if err != nil {
		slog.Error("Failed to query cache hits", "error", err)
	}

	err = db.QueryRow(`
		SELECT COUNT(*) FROM audit_log
		WHERE cache_hit = 0 AND timestamp >= datetime('now', '-1 day')
	`).Scan(&cacheMisses)
	if err != nil {
		slog.Error("Failed to query cache misses", "error", err)
	}

	if err = db.QueryRow(`
		SELECT COALESCE(AVG(latency_ms), 0) FROM audit_log
		WHERE cache_hit = 1 AND timestamp >= datetime('now', '-1 day') AND latency_ms > 0
	`).Scan(&avgHitLatency); err != nil {
		slog.Warn("Failed to query average cache hit latency", "error", err)
	}

	if err = db.QueryRow(`
		SELECT COALESCE(AVG(latency_ms), 0) FROM audit_log
		WHERE cache_hit = 0 AND timestamp >= datetime('now', '-1 day') AND latency_ms > 0
	`).Scan(&avgMissLatency); err != nil {
		slog.Warn("Failed to query average cache miss latency", "error", err)
	}

	var hitRate float64
	total := cacheHits + cacheMisses
	if total > 0 {
		hitRate = math.Round(float64(cacheHits)/float64(total)*1000) / 10
	}

	avgLatencySaved := avgMissLatency - avgHitLatency
	if avgLatencySaved < 0 {
		avgLatencySaved = 0
	}

	var cacheSizeEntries int
	var keys []string
	if h.cacheClient != nil {
		keys, err = h.cacheClient.Keys(r.Context(), "ilter:cache:*").Result()
		if err == nil {
			cacheSizeEntries = len(keys)
		} else {
			slog.Warn("Failed to get cache keys from Redis", "error", err)
		}
	}

	cacheSizeMB := math.Round(float64(cacheSizeEntries)*15.0/1024.0*100) / 100

	redisConnected := h.cacheClient != nil

	// Compute mode: "enabled" only when the feature flag is on AND Redis is
	// available. Without Redis the semantic cache cannot function.
	redisError := ""
	cacheMode := "disabled"
	if h.configCache != nil {
		if middleware.IsEnabled(h.configCache, "semantic_cache") {
			if redisConnected {
				// Read the real cache engine mode (semantic/exact) from the
				// live middleware to show the user what kind of caching is active.
				if h.semanticCacheMw != nil {
					engMode := h.semanticCacheMw.Mode()
					if engMode != "disabled" {
						cacheMode = engMode
					} else {
						cacheMode = "enabled"
					}
				} else {
					cacheMode = "enabled"
				}
			} else {
				if h.cfg != nil && h.cfg.Cache.RedisURL == "" {
					redisError = "Redis not available. Set ILTER_REDIS_URL and restart."
				} else {
					redisError = "Redis connection failed. Semantic cache cannot be enabled."
				}
			}
		}
	}

	topRows, err := db.Query(`
		SELECT
			COALESCE(a.prompt_preview, '') as query_preview,
			a.model,
			COUNT(*) as hit_count,
			MAX(a.timestamp) as last_accessed,
			COALESCE(AVG(a.latency_ms), 0) as avg_latency
		FROM audit_log a
		WHERE a.cache_hit = 1
		  AND a.timestamp >= datetime('now', '-1 day')
		  AND a.prompt_preview != ''
		GROUP BY a.prompt_preview, a.model
		ORDER BY hit_count DESC
		LIMIT 10
		`)
	if err == nil {
		defer topRows.Close()
		topQueries := make([]topCachedQuery, 0, 10)
		for topRows.Next() {
			var q topCachedQuery
			if err = topRows.Scan(&q.QueryPreview, &q.Model, &q.HitCount, &q.LastAccessed, &q.AvgLatency); err == nil {
				q.AvgLatency = math.Round(q.AvgLatency*100) / 100
				topQueries = append(topQueries, q)
			}
		}
		resp.TopQueries = topQueries
	}

	hourRows, err := db.Query(`
		SELECT
			strftime('%Y-%m-%dT%H:00', timestamp) as bucket,
			SUM(CASE WHEN cache_hit = 1 THEN 1 ELSE 0 END) as hits,
			SUM(CASE WHEN cache_hit = 0 THEN 1 ELSE 0 END) as misses
		FROM audit_log
		WHERE timestamp >= datetime('now', '-1 day')
		GROUP BY bucket
		ORDER BY bucket ASC
		`)
	if err == nil {
		defer hourRows.Close()
		hourly := make([]cacheHourlyPoint, 0, 24)
		for hourRows.Next() {
			var p cacheHourlyPoint
			if err := hourRows.Scan(&p.Time, &p.Hits, &p.Misses); err == nil {
				hourly = append(hourly, p)
			}
		}
		resp.HourlyData = hourly
	}

	resp.CacheHits24h = cacheHits
	resp.CacheMisses24h = cacheMisses
	resp.HitRatePct = hitRate
	resp.CacheSizeEntries = cacheSizeEntries
	resp.CacheSizeMB = cacheSizeMB
	resp.AvgLatencySavedMs = math.Round(avgLatencySaved*100) / 100
	resp.RedisConnected = redisConnected
	resp.RedisError = redisError
	resp.Mode = cacheMode

	resp.SimilarityThreshold = h.cfg.Cache.SimilarityThreshold
	if h.configCache != nil {
		if snap := h.configCache.Get(); snap != nil {
			resp.SimilarityThreshold = snap.CacheSimilarityThreshold
		}
	}
	// Mirror the zero-value fallback semanticcache.Cache actually applies at
	// match time (features/semanticcache/cache.go) — config plumbing doesn't
	// carry the real default through, so show the value that's truly in effect.
	if resp.SimilarityThreshold <= 0 {
		resp.SimilarityThreshold = 0.70
	}
	resp.TTLSeconds = int(h.cfg.Cache.TTL.Seconds())

	model.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleCacheModeToggle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode string `json:"mode"` // "enabled" or "disabled"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.Mode != "enabled" && req.Mode != "disabled" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request", "mode must be 'enabled' or 'disabled'")
		return
	}

	enabled := req.Mode == "enabled"

	// Don't allow enabling when Redis is not available — the cache
	// cannot function without it.
	if enabled && h.cacheClient == nil {
		var msg string
		if h.cfg != nil && h.cfg.Cache.RedisURL == "" {
			msg = "Redis not available. Set ILTER_REDIS_URL and restart."
		} else {
			msg = "Redis connection failed. Check Redis server and restart."
		}
		model.WriteJSONError(w, http.StatusBadRequest, "redis_unavailable", msg)
		return
	}
	// picks it up into snap.CacheEnabled (the same key middleware.IsEnabled and the
	// Feature Flags page read), and trigger config cache refresh.
	value := "false"
	if enabled {
		value = "true"
	}
	_, err := h.store.DB.Exec(
		`INSERT INTO runtime_config (section, key, value, updated_at, version)
		 VALUES ('feature', 'semantic_cache', ?, datetime('now'), 1)
		 ON CONFLICT(section, key) DO UPDATE SET value = excluded.value, version = version + 1, updated_at = datetime('now')`,
		value,
	)
	if err != nil {
		slog.Error("Failed to persist cache feature flag", "enabled", enabled, "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save cache feature flag")
		return
	}

	if h.configCache != nil {
		stores := &config.RuntimeStores{RuntimeConfig: h.store}
		if err := h.configCache.Refresh(context.Background(), stores); err != nil {
			slog.Warn("config cache refresh after cache toggle failed", "error", err)
		}
	}

	slog.Info("Cache toggled via feature flag", "enabled", enabled)

	model.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "mode": req.Mode})
}
