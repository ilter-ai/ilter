package access

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ilter-ai/ilter/internal/model"

	"github.com/go-chi/chi/v5"

	"github.com/ilter-ai/ilter/internal/auth"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

type CreateAPIKeyRequest struct {
	Name             string            `json:"name"`
	GroupID          *int              `json:"group_id,omitempty"`
	UserID           *int              `json:"user_id,omitempty"`
	Tags             map[string]string `json:"tags,omitempty"`
	RateLimitRPM     int               `json:"rate_limit_rpm"`
	RateLimitTPM     int64             `json:"rate_limit_tpm"`
	AllowedModels    []string          `json:"allowed_models,omitempty"`
	AllowedProviders []string          `json:"allowed_providers,omitempty"`
}

type UpdateAPIKeyRequest struct {
	Name             *string            `json:"name,omitempty"`
	GroupID          *int               `json:"group_id,omitempty"`
	UserID           *int               `json:"user_id,omitempty"`
	Tags             *map[string]string `json:"tags,omitempty"`
	RateLimitRPM     *int               `json:"rate_limit_rpm,omitempty"`
	RateLimitTPM     *int64             `json:"rate_limit_tpm,omitempty"`
	AllowedModels    *[]string          `json:"allowed_models,omitempty"`
	AllowedProviders *[]string          `json:"allowed_providers,omitempty"`
	Enabled          *bool              `json:"enabled,omitempty"`
}

func (h *Handler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	var keys []auth.APIKey
	var err error

	if groupIDStr := r.URL.Query().Get("group_id"); groupIDStr != "" {
		gid, parseErr := strconv.Atoi(groupIDStr)
		if parseErr != nil {
			model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid group_id")
			return
		}
		keys, err = h.store.ListAPIKeys(gid)
	} else {
		keys, err = h.store.ListAPIKeys()
	}
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list virtual keys")
		return
	}

	type akResponse struct {
		ID               string            `json:"id"`
		Name             string            `json:"name"`
		GroupID          *int              `json:"group_id,omitempty"`
		UserID           *int              `json:"user_id,omitempty"`
		Tags             map[string]string `json:"tags"`
		RateLimitRPM     int               `json:"rate_limit_rpm"`
		RateLimitTPM     int64             `json:"rate_limit_tpm"`
		AllowedModels    []string          `json:"allowed_models"`
		AllowedProviders []string          `json:"allowed_providers"`
		Enabled          bool              `json:"enabled"`
		CreatedAt        string            `json:"created_at"`
		UpdatedAt        string            `json:"updated_at"`
	}

	items := make([]akResponse, 0, len(keys))
	for _, k := range keys {
		items = append(items, akResponse{
			ID:               k.ID,
			Name:             k.Name,
			GroupID:          k.GroupID,
			UserID:           k.UserID,
			Tags:             k.Tags,
			RateLimitRPM:     k.RateLimitRPM,
			RateLimitTPM:     k.RateLimitTPM,
			AllowedModels:    k.AllowedModels,
			AllowedProviders: k.AllowedProviders,
			Enabled:          k.Enabled,
			CreatedAt:        k.CreatedAt.Format(time.RFC3339),
			UpdatedAt:        k.UpdatedAt.Format(time.RFC3339),
		})
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"api_keys": items,
	})
}

func (h *Handler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req CreateAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}

	if req.Name == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Name is required")
		return
	}

	if req.GroupID != nil {
		_, err := h.store.GetGroup(*req.GroupID)
		if err != nil {
			model.WriteJSONError(w, http.StatusNotFound, "not_found", "Group not found")
			return
		}
	}

	if req.UserID != nil {
		_, err := h.store.GetUser(*req.UserID)
		if err != nil {
			model.WriteJSONError(w, http.StatusNotFound, "not_found", "User not found")
			return
		}
	}

	vk, rawToken, err := h.store.CreateAPIKey(
		req.Name, req.GroupID, req.UserID,
		0, 0,
		req.RateLimitRPM, req.RateLimitTPM,
		req.AllowedModels, req.AllowedProviders,
		req.Tags,
	)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "already exists") {
			model.WriteJSONError(w, http.StatusConflict, "duplicate_key", "Virtual key with this name already exists")
			return
		}
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid virtual key parameters: "+errStr)
		return
	}

	if h.auditor != nil {
		vals := map[string]any{
			"name":              vk.Name,
			"rate_limit_rpm":    vk.RateLimitRPM,
			"rate_limit_tpm":    vk.RateLimitTPM,
			"allowed_models":    vk.AllowedModels,
			"allowed_providers": vk.AllowedProviders,
			"enabled":           vk.Enabled,
			"api_key":           rawToken,
		}
		if vk.GroupID != nil {
			vals["group_id"] = *vk.GroupID
		}
		if vk.UserID != nil {
			vals["user_id"] = *vk.UserID
		}
		if vk.Tags != nil {
			vals["tags"] = vk.Tags
		}
		if err := h.auditor.LogCreate("api_key", vk.ID, vals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Error("failed to log audit create api_key", "error", err)
		}
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"id":       vk.ID,
		"name":     vk.Name,
		"key":      rawToken,
		"group_id": vk.GroupID,
		"user_id":  vk.UserID,
	})
}

func (h *Handler) GetAPIKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	vk, err := h.store.GetAPIKey(id)
	if err != nil {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Virtual key not found")
		return
	}

	resp := map[string]any{
		"id":                vk.ID,
		"name":              vk.Name,
		"tags":              vk.Tags,
		"rate_limit_rpm":    vk.RateLimitRPM,
		"rate_limit_tpm":    vk.RateLimitTPM,
		"allowed_models":    vk.AllowedModels,
		"allowed_providers": vk.AllowedProviders,
		"enabled":           vk.Enabled,
		"created_at":        vk.CreatedAt.Format(time.RFC3339),
		"updated_at":        vk.UpdatedAt.Format(time.RFC3339),
	}
	if vk.GroupID != nil {
		resp["group_id"] = *vk.GroupID
	}
	if vk.UserID != nil {
		resp["user_id"] = *vk.UserID
	}
	model.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) UpdateAPIKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}

	var req UpdateAPIKeyRequest
	if umErr := json.Unmarshal(body, &req); umErr != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}

	// A plain *int can't distinguish "field omitted" (leave unchanged) from
	// "field explicitly null" (clear it), so check key presence separately.
	var rawFields map[string]json.RawMessage
	if uErr := json.Unmarshal(body, &rawFields); uErr != nil {
		slog.Error("failed to unmarshal raw fields for field presence detection", "error", uErr)
	}
	_, groupIDSent := rawFields["group_id"]
	_, userIDSent := rawFields["user_id"]

	existing, err := h.store.GetAPIKey(id)
	if err != nil {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Virtual key not found")
		return
	}
	existingOld := *existing

	if req.Name != nil {
		existing.Name = *req.Name
	}
	if groupIDSent {
		if req.GroupID != nil {
			_, err := h.store.GetGroup(*req.GroupID)
			if err != nil {
				model.WriteJSONError(w, http.StatusNotFound, "not_found", "Group not found")
				return
			}
			existing.GroupID = req.GroupID
		} else {
			existing.GroupID = nil
		}
	}
	if userIDSent {
		if req.UserID != nil {
			_, err := h.store.GetUser(*req.UserID)
			if err != nil {
				model.WriteJSONError(w, http.StatusNotFound, "not_found", "User not found")
				return
			}
			existing.UserID = req.UserID
		} else {
			existing.UserID = nil
		}
	}
	if req.Tags != nil {
		existing.Tags = *req.Tags
	}
	if req.RateLimitRPM != nil {
		existing.RateLimitRPM = *req.RateLimitRPM
	}
	if req.RateLimitTPM != nil {
		existing.RateLimitTPM = *req.RateLimitTPM
	}
	if req.AllowedModels != nil {
		existing.AllowedModels = *req.AllowedModels
	}
	if req.AllowedProviders != nil {
		existing.AllowedProviders = *req.AllowedProviders
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}

	oldVals := map[string]any{
		"name":              existingOld.Name,
		"rate_limit_rpm":    existingOld.RateLimitRPM,
		"rate_limit_tpm":    existingOld.RateLimitTPM,
		"allowed_models":    existingOld.AllowedModels,
		"allowed_providers": existingOld.AllowedProviders,
		"enabled":           existingOld.Enabled,
		"tags":              existingOld.Tags,
	}
	if existingOld.GroupID != nil {
		oldVals["group_id"] = *existingOld.GroupID
	}
	if existingOld.UserID != nil {
		oldVals["user_id"] = *existingOld.UserID
	}

	newVals := map[string]any{
		"name":              existing.Name,
		"rate_limit_rpm":    existing.RateLimitRPM,
		"rate_limit_tpm":    existing.RateLimitTPM,
		"allowed_models":    existing.AllowedModels,
		"allowed_providers": existing.AllowedProviders,
		"enabled":           existing.Enabled,
		"tags":              existing.Tags,
	}
	if existing.GroupID != nil {
		newVals["group_id"] = *existing.GroupID
	}
	if existing.UserID != nil {
		newVals["user_id"] = *existing.UserID
	}

	clearGroupID := groupIDSent && req.GroupID == nil
	clearUserID := userIDSent && req.UserID == nil
	if err := h.store.UpdateAPIKey(id, *existing, clearGroupID, clearUserID); err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to update virtual key")
		return
	}

	if h.auditor != nil {
		if err := h.auditor.LogUpdate("api_key", id, oldVals, newVals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Error("failed to log audit update api_key", "error", err)
		}
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"id":     id,
	})
}

func (h *Handler) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	vk, fetchErr := h.store.GetAPIKey(id)

	if err := h.store.DeleteAPIKey(id); err != nil {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Virtual key not found")
		return
	}

	if h.auditor != nil && fetchErr == nil && vk != nil {
		vals := map[string]any{
			"name":              vk.Name,
			"rate_limit_rpm":    vk.RateLimitRPM,
			"rate_limit_tpm":    vk.RateLimitTPM,
			"allowed_models":    vk.AllowedModels,
			"allowed_providers": vk.AllowedProviders,
			"enabled":           vk.Enabled,
			"tags":              vk.Tags,
		}
		if vk.GroupID != nil {
			vals["group_id"] = *vk.GroupID
		}
		if vk.UserID != nil {
			vals["user_id"] = *vk.UserID
		}
		if err := h.auditor.LogDelete("api_key", id, vals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Error("failed to log audit delete api_key", "error", err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) GetAPIKeyUsage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	fromDate := r.URL.Query().Get("from")
	toDate := r.URL.Query().Get("to")
	today := time.Now().UTC().Format("2006-01-02")
	if fromDate == "" {
		fromDate = today
	}
	if toDate == "" {
		toDate = today
	}

	usage, err := h.store.GetKeyUsage(id, fromDate, toDate)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to get usage")
		return
	}

	type usageItem struct {
		Date         string  `json:"date"`
		Model        string  `json:"model"`
		Provider     string  `json:"provider"`
		TokensIn     int64   `json:"tokens_in"`
		TokensOut    int64   `json:"tokens_out"`
		CostUSD      float64 `json:"cost_usd"`
		RequestCount int64   `json:"request_count"`
	}

	items := make([]usageItem, 0, len(usage))
	for _, u := range usage {
		items = append(items, usageItem{
			Date:         u.Date.Format("2006-01-02"),
			Model:        u.Model,
			Provider:     u.Provider,
			TokensIn:     u.TokensIn,
			TokensOut:    u.TokensOut,
			CostUSD:      u.CostUSD,
			RequestCount: u.RequestCount,
		})
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"key_id": id,
		"from":   fromDate,
		"to":     toDate,
		"items":  items,
	})
}

func (h *Handler) GetAPIKeysSummary(w http.ResponseWriter, _ *http.Request) {
	summary, err := h.store.GetAPIKeySummary()
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to get summary")
		return
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"total_keys":       summary.TotalKeys,
		"enabled_keys":     summary.EnabledKeys,
		"total_requests":   summary.TotalRequests,
		"total_cost_usd":   summary.TotalCostUSD,
		"total_tokens_in":  summary.TotalTokensIn,
		"total_tokens_out": summary.TotalTokensOut,
	})
}
