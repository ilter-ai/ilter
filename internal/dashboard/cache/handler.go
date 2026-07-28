package cache

import (
	"net/http"

	"github.com/ilter-ai/ilter/internal/middleware"
	"github.com/ilter-ai/ilter/internal/model"

	"github.com/redis/go-redis/v9"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
)

type Handler struct {
	store           *db.SQLiteStore
	cfg             *config.Config
	configCache     *config.Cache
	redis           *redis.Client
	cacheClient     *redis.Client
	semanticCacheMw *middleware.SemanticCacheMiddleware
}

func NewCacheHandler(store *db.SQLiteStore, cfg *config.Config, configCache *config.Cache, redis *redis.Client, cacheClient *redis.Client) *Handler {
	return &Handler{store: store, cfg: cfg, configCache: configCache, redis: redis, cacheClient: cacheClient}
}

// SetSemanticCacheMiddleware wires the live semantic-cache middleware so the
// dashboard can report the real cache mode (semantic/exact/disabled) instead
// of just the feature-flag enabled/disabled state.
func (h *Handler) SetSemanticCacheMiddleware(m *middleware.SemanticCacheMiddleware) {
	h.semanticCacheMw = m
}

func (h *Handler) CacheFlush(w http.ResponseWriter, r *http.Request) {
	if h.redis != nil {
		keys, err := h.redis.Keys(r.Context(), "ilter:cache:*").Result()
		if err != nil {
			model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to flush cache")
			return
		}
		if len(keys) > 0 {
			if err := h.redis.Del(r.Context(), keys...).Err(); err != nil {
				model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to flush cache")
				return
			}
		}
	}
	model.WriteJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"message": "Cache flushed successfully",
	})
}
