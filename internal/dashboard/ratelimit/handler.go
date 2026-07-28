package ratelimit

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/ilter-ai/ilter/internal/model"

	"github.com/redis/go-redis/v9"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/platform/rediskeys"
)

// KeyInfo holds identifying information about an API key.
type KeyInfo struct {
	ID        string `json:"id"`
	KeyName   string `json:"key_name"`
	OwnerType string `json:"owner_type,omitempty"`
	OwnerID   int    `json:"owner_id,omitempty"`
	OwnerName string `json:"owner_name,omitempty"`
}

// Handler serves rate-limit related admin endpoints.
type Handler struct {
	store       *db.SQLiteStore
	cfg         *config.Config
	configCache *config.Cache
	redis       *redis.Client
}

func NewRateLimitHandler(store *db.SQLiteStore, cfg *config.Config, configCache *config.Cache, redis *redis.Client) *Handler {
	return &Handler{store: store, cfg: cfg, configCache: configCache, redis: redis}
}

type KeyStatus string

const (
	KeyStatusOK       KeyStatus = "ok"
	KeyStatusWarning  KeyStatus = "warning"
	KeyStatusCritical KeyStatus = "critical"
)

type KeyItem struct {
	ID         string    `json:"id"`
	KeyName    string    `json:"key_name"`
	KeyPrefix  string    `json:"key_prefix"`
	RPMLimit   int       `json:"rpm_limit"`
	RetryAfter int       `json:"retry_after"`
	CurrentRPM int       `json:"current_rpm"`
	Blocked24h int       `json:"blocked_24h"`
	Status     KeyStatus `json:"status"`
	Key        *KeyInfo  `json:"key,omitempty"`
}

type ChartPoint struct {
	Time     string `json:"time"`
	Requests int    `json:"requests"`
	Limit    int    `json:"limit"`
}

// SummaryResponse is the top-level response for GET /api/rate-limits.
type SummaryResponse struct {
	Enabled      bool         `json:"enabled"`
	DefaultRPM   int          `json:"default_rpm"`
	RedisReady   bool         `json:"redis_ready"`
	TotalReqs24h int          `json:"total_requests_24h"`
	RateLimited  int          `json:"rate_limited_24h"`
	ActiveKeys   int          `json:"active_keys"`
	AvgRPM       float64      `json:"avg_rpm"`
	LimitRPM     int          `json:"limit_rpm"`
	Keys         []KeyItem    `json:"keys"`
	Chart        []ChartPoint `json:"chart"`
}

// HandleRateLimits returns rate-limit summary statistics and per-key breakdown.
func (h *Handler) HandleRateLimits(w http.ResponseWriter, _ *http.Request) {
	db := h.store.DB

	var totalReqs, rateLimited int
	var activeKeys int

	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE timestamp >= datetime('now', '-1 day')`).Scan(&totalReqs); err != nil {
		slog.Warn("Failed to count total requests", "error", err)
	}
	if errRateLimited := db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE status_code = 429 AND timestamp >= datetime('now', '-1 day')`).Scan(&rateLimited); errRateLimited != nil {
		slog.Warn("Failed to count rate-limited requests", "error", errRateLimited)
	}
	if errActiveKeys := db.QueryRow(`SELECT COUNT(DISTINCT key_id) FROM audit_log WHERE key_id IS NOT NULL AND key_id != '' AND timestamp >= datetime('now', '-1 day')`).Scan(&activeKeys); errActiveKeys != nil {
		slog.Warn("Failed to count active keys", "error", errActiveKeys)
	}

	// Bind minuteAgo as time.Time (not a pre-formatted string): the SQLite
	// DSN's _time_format/_timezone conversion (internal/db/sqlite.go) only
	// intercepts the time.Time Go type, so a string built via .Format(...) in
	// the server's local zone would bypass it and compare wrong once the
	// server isn't running in UTC.
	minuteAgo := time.Now().Add(-1 * time.Minute)

	keyRows, err := db.Query(`
		SELECT
			vk.id,
			vk.name,
			COALESCE(vk.key_prefix, '') AS key_prefix,
			CASE WHEN vk.user_id IS NOT NULL THEN 'user' WHEN vk.group_id IS NOT NULL THEN 'group' ELSE '' END,
			COALESCE(vk.user_id, vk.group_id, 0),
			COALESCE(u.name, g.name, '') AS owner_name,
			COALESCE(vk.rate_limit_rpm, 100) AS rpm_limit,
			COALESCE(vk.rate_limit_retry_after, 60) AS retry_after,
			COALESCE((SELECT COUNT(*) FROM audit_log a WHERE a.key_id = vk.id AND datetime(a.timestamp) >= datetime(?)), 0) AS current_rpm,
			COALESCE((SELECT COUNT(*) FROM audit_log a WHERE a.key_id = vk.id AND a.status_code = 429 AND a.timestamp >= datetime('now', '-1 day')), 0) AS blocked_24h
		FROM api_keys vk
		LEFT JOIN users u ON vk.user_id = u.id
		LEFT JOIN groups g ON vk.group_id = g.id
		ORDER BY vk.name`, minuteAgo)
	if err != nil {
		slog.Error("Failed to query rate limit keys", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	defer keyRows.Close()

	keys := make([]KeyItem, 0)
	for keyRows.Next() {
		var item KeyItem
		var ownerType, ownerName string
		var ownerID int
		if errScan := keyRows.Scan(&item.ID, &item.KeyName, &item.KeyPrefix, &ownerType, &ownerID, &ownerName, &item.RPMLimit, &item.RetryAfter, &item.CurrentRPM, &item.Blocked24h); errScan != nil {
			slog.Error("Failed to scan rate limit key row", "error", errScan)
			continue
		}
		item.Key = &KeyInfo{
			ID:        item.ID,
			KeyName:   item.KeyName,
			OwnerType: ownerType,
			OwnerID:   ownerID,
			OwnerName: ownerName,
		}
		item.Status = computeKeyStatus(item.CurrentRPM, item.RPMLimit)
		keys = append(keys, item)
	}
	if errIter := keyRows.Err(); errIter != nil {
		slog.Error("Rate limit key rows iteration error", "error", errIter)
	}

	chart, err := queryRateLimitChart(db, h.cfg.RateLimit.DefaultRPM)
	if err != nil {
		slog.Error("Failed to query rate limit chart", "error", err)
		chart = []ChartPoint{}
	}

	avgRPM := 0.0
	if totalReqs > 0 {
		avgRPM = float64(totalReqs) / 1440.0 // requests per minute over 24h
	}

	enabled := h.cfg.RateLimit.Enabled
	if h.configCache != nil {
		enabled = config.IsEnabled(h.configCache, "rate_limit")
	}

	resp := SummaryResponse{
		Enabled:      enabled,
		DefaultRPM:   h.cfg.RateLimit.DefaultRPM,
		RedisReady:   h.cfg.RateLimit.RedisURL != "",
		TotalReqs24h: totalReqs,
		RateLimited:  rateLimited,
		ActiveKeys:   activeKeys,
		AvgRPM:       avgRPM,
		LimitRPM:     h.cfg.RateLimit.DefaultRPM,
		Keys:         keys,
		Chart:        chart,
	}

	model.WriteJSON(w, http.StatusOK, resp)
}

func queryRateLimitChart(db *sql.DB, defaultLimit int) ([]ChartPoint, error) {
	rows, err := db.Query(`
		SELECT
			strftime('%H:00', timestamp) AS hour,
			COUNT(*) AS requests
		FROM audit_log
		WHERE timestamp >= datetime('now', '-1 day')
		GROUP BY strftime('%Y-%m-%d %H', timestamp)
		ORDER BY hour ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	points := make([]ChartPoint, 0)
	for rows.Next() {
		var p ChartPoint
		if err := rows.Scan(&p.Time, &p.Requests); err != nil {
			continue
		}
		p.Limit = defaultLimit
		points = append(points, p)
	}
	return points, rows.Err()
}

func computeKeyStatus(current, limit int) KeyStatus {
	if limit <= 0 {
		return KeyStatusOK
	}
	pct := (float64(current) / float64(limit)) * 100
	switch {
	case pct >= 90:
		return KeyStatusCritical
	case pct >= 70:
		return KeyStatusWarning
	default:
		return KeyStatusOK
	}
}

type ConfigResponse struct {
	TargetType string `json:"target_type"`
	TargetID   int    `json:"target_id"`
	RPM        int    `json:"rpm"`
	CurrentRPM int    `json:"current_rpm"`
}

type ConfigRequest struct {
	RPM int `json:"rpm"`
}

type KeyRateLimitResponse struct {
	ID         string    `json:"id"`
	KeyPrefix  string    `json:"key_prefix"`
	KeyName    string    `json:"key_name"`
	RPMLimit   int       `json:"rpm_limit"`
	RetryAfter int       `json:"retry_after"`
	CurrentRPM int       `json:"current_rpm"`
	Blocked24h int       `json:"blocked_24h"`
	Status     KeyStatus `json:"status"`
	Key        *KeyInfo  `json:"key,omitempty"`
}

type KeyRateLimitRequest struct {
	RPMLimit   int `json:"rpm_limit"`
	RetryAfter int `json:"retry_after,omitempty"`
}

// HandleGetUserRateLimit returns the user-level rate limit config and current usage.
func (h *Handler) HandleGetUserRateLimit(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.PathValue("id")
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_id", "Invalid user ID")
		return
	}

	resp := ConfigResponse{
		TargetType: "user",
		TargetID:   userID,
	}

	if h.redis != nil {
		cfgKey := rediskeys.UserRateLimitConfigKey(userID)
		val, err := h.redis.Get(r.Context(), cfgKey).Int()
		if err == nil {
			resp.RPM = val
		}

		counterKey := rediskeys.UserRateLimitCounterKey(userID, time.Now())
		count, err := h.redis.Get(r.Context(), counterKey).Int()
		if err == nil {
			resp.CurrentRPM = count
		}
	}

	model.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleSetUserRateLimit(w http.ResponseWriter, r *http.Request) {
	userIDStr := r.PathValue("id")
	userID, err := strconv.Atoi(userIDStr)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_id", "Invalid user ID")
		return
	}

	var req ConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_body", "Invalid request body")
		return
	}

	if req.RPM < 0 {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_rpm", "RPM must be >= 0 (0 = unlimited)")
		return
	}

	if h.redis == nil {
		model.WriteJSONError(w, http.StatusServiceUnavailable, "redis_unavailable", "Redis is not available")
		return
	}

	cfgKey := rediskeys.UserRateLimitConfigKey(userID)
	if req.RPM == 0 {
		h.redis.Del(r.Context(), cfgKey)
	} else {
		h.redis.Set(r.Context(), cfgKey, req.RPM, 0)
	}

	resp := ConfigResponse{
		TargetType: "user",
		TargetID:   userID,
		RPM:        req.RPM,
	}
	model.WriteJSON(w, http.StatusOK, resp)
}

// HandleGetGroupRateLimit returns the group-level rate limit config and current usage.
func (h *Handler) HandleGetGroupRateLimit(w http.ResponseWriter, r *http.Request) {
	groupIDStr := r.PathValue("id")
	groupID, err := strconv.Atoi(groupIDStr)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_id", "Invalid group ID")
		return
	}

	resp := ConfigResponse{
		TargetType: "group",
		TargetID:   groupID,
	}

	if h.redis != nil {
		cfgKey := rediskeys.GroupRateLimitConfigKey(groupID)
		val, err := h.redis.Get(r.Context(), cfgKey).Int()
		if err == nil {
			resp.RPM = val
		}

		counterKey := rediskeys.GroupRateLimitCounterKey(groupID, time.Now())
		count, err := h.redis.Get(r.Context(), counterKey).Int()
		if err == nil {
			resp.CurrentRPM = count
		}
	}

	model.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleSetGroupRateLimit(w http.ResponseWriter, r *http.Request) {
	groupIDStr := r.PathValue("id")
	groupID, err := strconv.Atoi(groupIDStr)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_id", "Invalid group ID")
		return
	}

	var req ConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_body", "Invalid request body")
		return
	}

	if req.RPM < 0 {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_rpm", "RPM must be >= 0 (0 = unlimited)")
		return
	}

	if h.redis == nil {
		model.WriteJSONError(w, http.StatusServiceUnavailable, "redis_unavailable", "Redis is not available")
		return
	}

	cfgKey := rediskeys.GroupRateLimitConfigKey(groupID)
	if req.RPM == 0 {
		h.redis.Del(r.Context(), cfgKey)
	} else {
		h.redis.Set(r.Context(), cfgKey, req.RPM, 0)
	}

	resp := ConfigResponse{
		TargetType: "group",
		TargetID:   groupID,
		RPM:        req.RPM,
	}
	model.WriteJSON(w, http.StatusOK, resp)
}

// HandleGetKeyRateLimit returns the per-key rate limit config and current usage.
func (h *Handler) HandleGetKeyRateLimit(w http.ResponseWriter, r *http.Request) {
	keyID := r.PathValue("id")
	if keyID == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "missing_id", "Key ID is required")
		return
	}

	var dbKeyName, dbKeyPrefix string
	var dbRPMLimit, dbRetryAfter int
	var dbEnabled int
	err := h.store.DB.QueryRow(
		`SELECT name, COALESCE(key_prefix, ''), rate_limit_rpm, COALESCE(rate_limit_retry_after, 60), enabled FROM api_keys WHERE id = ?`,
		keyID,
	).Scan(&dbKeyName, &dbKeyPrefix, &dbRPMLimit, &dbRetryAfter, &dbEnabled)
	if err != nil {
		if err == sql.ErrNoRows {
			model.WriteJSONError(w, http.StatusNotFound, "not_found", "API key not found")
			return
		}
		model.WriteJSONError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}

	// Query current RPM from audit_log (last minute). minuteAgo is bound as
	// time.Time, not a pre-formatted string — see comment at the other
	// call site in this file for why that distinction matters.
	var currentRPM int
	minuteAgo := time.Now().Add(-1 * time.Minute)
	if err := h.store.DB.QueryRow(
		`SELECT COUNT(*) FROM audit_log WHERE key_id = ? AND datetime(timestamp) >= datetime(?)`, keyID, minuteAgo,
	).Scan(&currentRPM); err != nil {
		slog.Warn("Failed to query current RPM for key", "key_id", keyID, "error", err)
	}

	// Query blocked 24h
	var blocked24h int
	if err := h.store.DB.QueryRow(
		`SELECT COUNT(*) FROM audit_log WHERE key_id = ? AND status_code = 429 AND timestamp >= datetime('now', '-1 day')`, keyID,
	).Scan(&blocked24h); err != nil {
		slog.Warn("Failed to query blocked 24h for key", "key_id", keyID, "error", err)
	}

	resp := KeyRateLimitResponse{
		ID:         keyID,
		KeyPrefix:  dbKeyPrefix,
		KeyName:    dbKeyName,
		RPMLimit:   dbRPMLimit,
		RetryAfter: dbRetryAfter,
		CurrentRPM: currentRPM,
		Blocked24h: blocked24h,
		Status:     computeKeyStatus(currentRPM, dbRPMLimit),
	}
	model.WriteJSON(w, http.StatusOK, resp)
}

// HandleSetKeyRateLimit sets the per-key rate limit config.
func (h *Handler) HandleSetKeyRateLimit(w http.ResponseWriter, r *http.Request) {
	keyID := r.PathValue("id")
	if keyID == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "missing_id", "Key ID is required")
		return
	}

	var req KeyRateLimitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_body", "Invalid request body")
		return
	}

	if req.RPMLimit < 0 {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_rpm", "RPM limit must be >= 0")
		return
	}

	retryAfter := req.RetryAfter
	if retryAfter == 0 {
		retryAfter = 60
	}

	if err := h.store.SetKeyRateLimit(keyID, req.RPMLimit, retryAfter); err != nil {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "API key not found")
		return
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}
