package features

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ilter-ai/ilter/internal/model"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/platform/cooldown"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

type ActiveCooldownItem struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	KeyID     string `json:"key_id"`
	ExpiresAt string `json:"expires_at"`
}

type FallbackSummaryResponse struct {
	Enabled          bool                 `json:"enabled"`
	CooldownDuration string               `json:"cooldown_duration"`
	ModelDowngrade   string               `json:"model_downgrade"`
	AllowedModels    []string             `json:"allowed_models"`
	MaxAttempts      int                  `json:"max_attempts"`
	ActiveCooldowns  []ActiveCooldownItem `json:"active_cooldowns"`
}

type updateFallbackRequest struct {
	Enabled          *bool    `json:"enabled,omitempty"`
	CooldownDuration *string  `json:"cooldown_duration,omitempty"`
	ModelDowngrade   *string  `json:"model_downgrade,omitempty"`
	AllowedModels    []string `json:"allowed_models,omitempty"`
	MaxAttempts      *int     `json:"max_attempts,omitempty"`
}

type clearCooldownRequest struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	KeyID    string `json:"key_id"`
}

func (h *Handler) SetCooldownStore(s cooldown.Store) {
	h.cooldownStore = s
}

func (h *Handler) HandleGetFallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := h.getFallbackConfig()

	cooldownsMap := make(map[string]time.Time)
	if h.cooldownStore != nil {
		cooldownsMap = h.cooldownStore.GetCooldowns(ctx)
	}

	items := make([]ActiveCooldownItem, 0, len(cooldownsMap))
	for key, exp := range cooldownsMap {
		parts := strings.Split(key, "|")
		providerName, modelName, keyID := "", "", ""
		if len(parts) >= 1 {
			providerName = parts[0]
		}
		if len(parts) >= 2 {
			modelName = parts[1]
		}
		if len(parts) >= 3 {
			keyID = parts[2]
		}
		items = append(items, ActiveCooldownItem{
			Provider:  providerName,
			Model:     modelName,
			KeyID:     keyID,
			ExpiresAt: exp.Format(time.RFC3339),
		})
	}

	resp := FallbackSummaryResponse{
		Enabled:          cfg.Enabled,
		CooldownDuration: cfg.CooldownDuration.String(),
		ModelDowngrade:   cfg.ModelDowngrade,
		AllowedModels:    cfg.AllowedModels,
		MaxAttempts:      cfg.MaxAttempts,
		ActiveCooldowns:  items,
	}

	model.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) HandleUpdateFallback(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req updateFallbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}

	if h.store != nil && h.store.DB != nil {
		if req.Enabled != nil {
			val := "false"
			if *req.Enabled {
				val = "true"
			}
			h.saveRuntimeConfig("fallback", "enabled", val)
		}
		if req.CooldownDuration != nil {
			if _, err := time.ParseDuration(*req.CooldownDuration); err != nil {
				model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid cooldown_duration format (e.g. 5m, 1m)")
				return
			}
			h.saveRuntimeConfig("fallback", "cooldown_duration", *req.CooldownDuration)
		}
		if req.ModelDowngrade != nil {
			h.saveRuntimeConfig("fallback", "model_downgrade", *req.ModelDowngrade)
		}
		if req.MaxAttempts != nil {
			h.saveRuntimeConfig("fallback", "max_attempts", fmt.Sprintf("%d", *req.MaxAttempts))
		}
		if req.AllowedModels != nil {
			h.saveRuntimeConfig("fallback", "allowed_models", strings.Join(req.AllowedModels, ","))
		}
	}

	if h.configCache != nil {
		stores := &config.RuntimeStores{RuntimeConfig: h.store}
		if err := h.configCache.Refresh(r.Context(), stores); err != nil {
			slog.Warn("Failed to refresh configCache after fallback update", "error", err)
		}
	}

	h.HandleGetFallback(w, r)
}

type toggleFallbackRequest struct {
	Enabled bool `json:"enabled"`
}

func (h *Handler) HandleToggleFallback(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req toggleFallbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}

	// Read old value for audit log before upserting.
	var oldEnabled string
	if h.store != nil && h.store.DB != nil {
		_ = h.store.DB.QueryRow(
			`SELECT value FROM runtime_config WHERE section = 'fallback' AND key = 'enabled'`,
		).Scan(&oldEnabled)
	}

	val := "false"
	if req.Enabled {
		val = "true"
	}

	if h.store != nil && h.store.DB != nil {
		h.saveRuntimeConfig("fallback", "enabled", val)
	}

	if h.auditor != nil {
		oldVals := map[string]any{"enabled": oldEnabled == "true"}
		newVals := map[string]any{"enabled": req.Enabled}
		if err := h.auditor.LogUpdate("fallback", "enabled", oldVals, newVals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Warn("audit log failed for fallback toggle", "error", err)
		}
	}

	if h.configCache != nil {
		stores := &config.RuntimeStores{RuntimeConfig: h.store}
		if err := h.configCache.Refresh(r.Context(), stores); err != nil {
			slog.Warn("Failed to refresh configCache after fallback toggle", "error", err)
		}
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (h *Handler) HandleClearCooldown(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req clearCooldownRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}

	if h.cooldownStore != nil {
		cand := cooldown.Candidate{
			Provider: req.Provider,
			Model:    req.Model,
			KeyID:    req.KeyID,
		}
		h.cooldownStore.ClearCooldown(r.Context(), cand)
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (h *Handler) getFallbackConfig() config.FallbackConfig {
	if h.configCache != nil && h.configCache.Get() != nil {
		return h.configCache.Get().Fallback()
	}
	if h.cfg != nil {
		return h.cfg.Fallback
	}
	return config.FallbackConfig{
		Enabled:          true,
		CooldownDuration: 5 * time.Minute,
		ModelDowngrade:   "none",
		MaxAttempts:      0,
	}
}

func (h *Handler) saveRuntimeConfig(section, key, value string) {
	_, err := h.store.DB.Exec(
		`INSERT INTO runtime_config (section, key, value, updated_at, version)
		 VALUES (?, ?, ?, datetime('now'), 1)
		 ON CONFLICT(section, key) DO UPDATE SET value = excluded.value, version = version + 1, updated_at = datetime('now')`,
		section, key, value,
	)
	if err != nil {
		slog.Error("Failed to save runtime_config", "section", section, "key", key, "error", err)
	}
}
