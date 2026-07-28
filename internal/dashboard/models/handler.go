package models

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/ilter-ai/ilter/internal/model"

	"github.com/go-chi/chi/v5"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/model/catalog"
)

// Handler serves model-related admin endpoints.
type Handler struct {
	store *db.SQLiteStore
	cfg   *config.Config
	lb    *smartrouter.LoadBalancer
}

func NewModelsHandler(store *db.SQLiteStore, cfg *config.Config, lb *smartrouter.LoadBalancer) *Handler {
	return &Handler{store: store, cfg: cfg, lb: lb}
}

type ModelResponseItem struct {
	Name               string  `json:"name"`
	Provider           string  `json:"provider"`
	Type               string  `json:"type"`
	OwnedBy            string  `json:"owned_by"`
	Active             bool    `json:"active"`
	Configured         bool    `json:"configured"`
	DisplayName        string  `json:"display_name,omitempty"`
	Tier               string  `json:"tier,omitempty"`
	CostPerInputToken  float64 `json:"cost_per_input_token,omitempty"`
	CostPerOutputToken float64 `json:"cost_per_output_token,omitempty"`
}

func (h *Handler) HandleModels(w http.ResponseWriter, _ *http.Request) {
	lbInfos := h.lb.GetAvailableModelInfos()
	lbSeen := make(map[string]bool, len(lbInfos))

	configuredProviders := make(map[string]bool, len(h.cfg.Providers))
	for _, p := range h.cfg.Providers {
		configuredProviders[p.Name] = true
	}

	dbModels, err := h.store.GetAllProviderModels()
	if err != nil {
		slog.Error("Failed to query provider_models", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	dbMap := make(map[string]db.ProviderModel, len(dbModels))
	for _, pm := range dbModels {
		key := pm.Provider + ":" + pm.Model
		dbMap[key] = pm
	}

	catalog.ModelsMu.RLock()
	defer catalog.ModelsMu.RUnlock()

	resp := make([]ModelResponseItem, 0, len(lbInfos)+len(dbModels))

	for _, info := range lbInfos {
		lbSeen[info.Provider+":"+info.Name] = true
		item := ModelResponseItem{
			Name:       info.Name,
			Provider:   info.Provider,
			Type:       info.Type,
			OwnedBy:    info.OwnedBy,
			Active:     info.Active,
			Configured: true,
		}
		if regInfo, ok := catalog.GetModel(info.Name); ok {
			item.DisplayName = regInfo.DisplayName
			item.Tier = regInfo.Tier
			item.CostPerInputToken = regInfo.CostPerInputToken
			item.CostPerOutputToken = regInfo.CostPerOutputToken
		} else if dbEntry, ok := dbMap[info.Provider+":"+info.Name]; ok {
			item.Tier = dbEntry.Tier
			item.CostPerInputToken = dbEntry.CostIn
			item.CostPerOutputToken = dbEntry.CostOut
		}
		resp = append(resp, item)
	}

	for _, pm := range dbModels {
		if !configuredProviders[pm.Provider] {
			continue
		}
		if lbSeen[pm.Provider+":"+pm.Model] {
			continue
		}
		lbSeen[pm.Provider+":"+pm.Model] = true
		item := ModelResponseItem{
			Name:       pm.Model,
			Provider:   pm.Provider,
			Type:       pm.Provider,
			OwnedBy:    pm.Provider,
			Active:     pm.Active,
			Configured: false,
			Tier:       pm.Tier,
		}
		item.CostPerInputToken = pm.CostIn
		item.CostPerOutputToken = pm.CostOut
		if regInfo, ok := catalog.GetModel(pm.Model); ok {
			item.DisplayName = regInfo.DisplayName
			if regInfo.Tier != "" && regInfo.Tier != "standard" {
				item.Tier = regInfo.Tier
			}
			if regInfo.CostPerInputToken > 0 {
				item.CostPerInputToken = regInfo.CostPerInputToken
			}
			if regInfo.CostPerOutputToken > 0 {
				item.CostPerOutputToken = regInfo.CostPerOutputToken
			}
		}
		resp = append(resp, item)
	}

	model.WriteJSON(w, http.StatusOK, resp)
}

type ToggleModelRequest struct {
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

func (h *Handler) HandleToggleModel(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req ToggleModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}
	if req.Name == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Model name is required")
		return
	}

	if err := h.store.SaveModelStatus(req.Name, req.Active); err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save model status to DB")
		return
	}

	inactiveList, err := h.store.GetInactiveModels()
	if err == nil {
		h.lb.SetInactiveModels(inactiveList)
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (h *Handler) HandleUpdateModelByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Model ID is required")
		return
	}

	var req ToggleModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}
	if req.Name == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Model name is required")
		return
	}

	if err := h.store.SaveModelStatus(req.Name, req.Active); err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save model status to DB")
		return
	}

	inactiveList, err := h.store.GetInactiveModels()
	if err == nil {
		h.lb.SetInactiveModels(inactiveList)
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"model_id": id,
		"name":     req.Name,
		"active":   req.Active,
	})
}

type UpdateModelTierRequest struct {
	Name string `json:"name"`
	Tier string `json:"tier"`
}

func (h *Handler) HandleUpdateModelTier(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req UpdateModelTierRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}
	if req.Name == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Model name is required")
		return
	}
	validTiers := map[string]bool{"free": true, "economy": true, "standard": true, "premium": true}
	if req.Tier == "" || !validTiers[req.Tier] {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid tier: must be free, economy, standard, or premium")
		return
	}

	catalog.ModelsMu.Lock()
	if infos, ok := catalog.Models[req.Name]; ok {
		for i := range infos {
			infos[i].Tier = req.Tier
		}
		catalog.Models[req.Name] = infos
	}
	catalog.ModelsMu.Unlock()

	if err := h.store.SaveModelTier(req.Name, req.Tier); err != nil {
		slog.Error("Failed to persist model tier to DB", "model", req.Name, "tier", req.Tier, "error", err)
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}
