package features

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/redis/go-redis/v9"

	"github.com/ilter-ai/ilter/internal/model"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/db/audit"
	"github.com/ilter-ai/ilter/internal/features/loopdetect"
	"github.com/ilter-ai/ilter/internal/platform/cooldown"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

// Handler serves feature-flag related admin endpoints.
type Handler struct {
	store         *db.SQLiteStore
	cfg           *config.Config
	configCache   *config.Cache
	redisClient   *redis.Client
	cacheClient   *redis.Client
	cooldownStore cooldown.Store
	loopDetector  *loopdetect.Detector
	auditor       *audit.SQLiteConfigAuditor
}

func NewFeaturesHandler(store *db.SQLiteStore, cfg *config.Config, configCache *config.Cache, auditor *audit.SQLiteConfigAuditor, redisClient *redis.Client, cacheClient *redis.Client) *Handler {
	return &Handler{store: store, cfg: cfg, configCache: configCache, auditor: auditor, redisClient: redisClient, cacheClient: cacheClient}
}

type toggleFeatureRequest struct {
	FeatureKey string `json:"feature_key"`
	Enabled    bool   `json:"enabled"`
}

type FeatureItem struct {
	FeatureKey string `json:"feature_key"`
	Enabled    bool   `json:"enabled"`
	Warning    string `json:"warning,omitempty"`
}

func (h *Handler) HandleFeatures(w http.ResponseWriter, _ *http.Request) {
	snap := h.bootFeatures()
	model.WriteJSON(w, http.StatusOK, snap)
}

// bootFeatures resolves the feature toggle state from the config snapshot.
// It reads the resolved typed fields (which include runtime_config overrides)
// and converts them to the same flat FeatureItem list the frontend expects.
func (h *Handler) bootFeatures() []FeatureItem {
	if h.configCache != nil && h.configCache.Get() != nil {
		snap := h.configCache.Get()
		// Force semantic_cache disabled when Redis is unavailable:
		// the feature cannot function without Redis, so showing it as enabled
		// in the Feature Flags page is misleading.
		semEnabled := snap.CacheEnabled
		var semWarning string
		if h.cfg == nil || h.cfg.Cache.RedisURL == "" {
			semEnabled = false
			semWarning = "Redis not available. Set ILTER_REDIS_URL and restart."
		} else if h.cacheClient == nil {
			semEnabled = false
			semWarning = "Redis connection failed. Semantic cache cannot be enabled."
		}
		return []FeatureItem{
			{FeatureKey: "rate_limit", Enabled: snap.RateLimit.Enabled},
			{FeatureKey: "budget", Enabled: snap.Budget.Enabled},
			{FeatureKey: "pii", Enabled: snap.PII.Enabled},
			{FeatureKey: "semantic_cache", Enabled: semEnabled, Warning: semWarning},
			{FeatureKey: "loop_detection", Enabled: snap.CostGuard.LoopDetection},
			{FeatureKey: "guardrails", Enabled: snap.GuardrailsEnabled},
			{FeatureKey: "smart_router", Enabled: snap.Routing.Enabled},
			{FeatureKey: "mcp", Enabled: snap.MCPEnabled},
			{FeatureKey: "openapi", Enabled: snap.OpenAPIEnabled},
		}
	}
	// Fallback: boot config
	cfg := h.cfg
	if cfg == nil {
		return nil
	}
	return []FeatureItem{
		{FeatureKey: "rate_limit", Enabled: cfg.RateLimit.Enabled},
		{FeatureKey: "budget", Enabled: cfg.Budget.Enabled},
		{FeatureKey: "pii", Enabled: cfg.PII.Enabled},
		{FeatureKey: "loop_detection", Enabled: cfg.CostGuard.LoopDetection},
		{FeatureKey: "guardrails", Enabled: cfg.Guardrails.Enabled},
		{FeatureKey: "smart_router", Enabled: cfg.Routing.Enabled},
		{FeatureKey: "mcp", Enabled: cfg.MCP.Enabled},
		{FeatureKey: "openapi", Enabled: true},
	}
}

func (h *Handler) HandleToggleFeature(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req toggleFeatureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}
	if req.FeatureKey == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Feature key is required")
		return
	}

	// Reject enabling semantic_cache when Redis is unavailable.
	if req.FeatureKey == "semantic_cache" && req.Enabled {
		redisUnavailable := h.cfg == nil || h.cfg.Cache.RedisURL == "" || h.cacheClient == nil
		if redisUnavailable {
			msg := "Redis connection failed. Semantic cache cannot be enabled."
			model.WriteJSONError(w, http.StatusFailedDependency, "redis_unavailable", msg)
			return
		}
	}

	// Read old value for audit log before upserting.
	var oldEnabled string
	_ = h.store.DB.QueryRow(
		`SELECT value FROM runtime_config WHERE section = 'feature' AND key = ?`,
		req.FeatureKey,
	).Scan(&oldEnabled)

	// Persist to runtime_config as feature:<key> so mergeRuntimeConfigValues
	// picks it up and sets the typed snapshot field directly.
	value := "false"
	if req.Enabled {
		value = "true"
	}
	_, err := h.store.DB.Exec(
		`INSERT INTO runtime_config (section, key, value, updated_at, version)
		 VALUES ('feature', ?, ?, datetime('now'), 1)
		 ON CONFLICT(section, key) DO UPDATE SET value = excluded.value, version = version + 1, updated_at = datetime('now')`,
		req.FeatureKey, value,
	)
	if err != nil {
		slog.Error("Failed to persist feature flag", "feature", req.FeatureKey, "enabled", req.Enabled, "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save feature flag")
		return
	}

	if h.auditor != nil {
		oldVals := map[string]any{"enabled": oldEnabled == "true"}
		newVals := map[string]any{"enabled": req.Enabled}
		if err := h.auditor.LogUpdate("feature", req.FeatureKey, oldVals, newVals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Warn("audit log failed for feature toggle", "feature", req.FeatureKey, "error", err)
		}
	}

	// Hot-apply: config cache refresh so middleware picks up the change.
	if h.configCache != nil {
		stores := &config.RuntimeStores{RuntimeConfig: h.store}
		if err := h.configCache.Refresh(context.Background(), stores); err != nil {
			slog.Warn("config cache refresh after feature toggle failed", "error", err)
		}
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}
