package prompts

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/ilter-ai/ilter/internal/model"

	"github.com/go-chi/chi/v5"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

type createTemplateRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Content     string   `json:"content"`
	Version     string   `json:"version,omitempty"`
	IsActive    *bool    `json:"is_active,omitempty"`
	Labels      []string `json:"labels,omitempty"`
}

func (h *PromptHandler) ListTemplates(w http.ResponseWriter, _ *http.Request) {
	templates, err := h.store.ListPromptTemplates()
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list prompt templates")
		return
	}
	if templates == nil {
		templates = []config.PromptTemplate{}
	}
	model.WriteJSON(w, http.StatusOK, map[string]any{
		"prompts": templates,
	})
}

func (h *PromptHandler) CreateTemplate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req createTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}
	if req.Name == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Name is required")
		return
	}
	if req.Content == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Content is required")
		return
	}
	if err := config.ValidateTemplate(req.Content); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_template", "Invalid template syntax: "+err.Error())
		return
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	if req.Labels == nil {
		req.Labels = []string{}
	}

	tmpl := config.PromptTemplate{
		Name:        req.Name,
		Description: req.Description,
		Content:     req.Content,
		Version:     req.Version,
		IsActive:    isActive,
		Labels:      req.Labels,
	}

	id, err := h.store.CreatePromptTemplate(tmpl)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			model.WriteJSONError(w, http.StatusConflict, "duplicate_prompt", "Prompt template with this name already exists")
			return
		}
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to create prompt template: "+err.Error())
		return
	}

	if h.auditor != nil {
		vals := map[string]any{
			"name":        req.Name,
			"description": req.Description,
			"content":     req.Content,
			"version":     req.Version,
			"is_active":   isActive,
			"labels":      req.Labels,
		}
		if err := h.auditor.LogCreate("prompt_template", strconv.Itoa(id), vals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Error("failed to log audit create prompt_template", "error", err)
		}
	}

	model.WriteJSON(w, http.StatusCreated, map[string]any{
		"status": "ok",
		"id":     id,
	})
}

func (h *PromptHandler) GetTemplate(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid ID format")
		return
	}

	tmpl, err := h.store.GetPromptTemplate(id)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to get prompt template")
		return
	}
	if tmpl == nil {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Prompt template not found")
		return
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"prompt": tmpl,
	})
}

type updateTemplateRequest struct {
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Content     string   `json:"content,omitempty"`
	Version     string   `json:"version,omitempty"`
	IsActive    *bool    `json:"is_active,omitempty"`
	Labels      []string `json:"labels,omitempty"`
}

func (h *PromptHandler) UpdateTemplate(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid ID format")
		return
	}

	defer r.Body.Close()
	var req updateTemplateRequest
	if err = json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}

	existing, err := h.store.GetPromptTemplate(id)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to get prompt template")
		return
	}
	if existing == nil {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Prompt template not found")
		return
	}
	existingOld := *existing

	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.Description != "" {
		existing.Description = req.Description
	}
	if req.Content != "" {
		if err := config.ValidateTemplate(req.Content); err != nil {
			model.WriteJSONError(w, http.StatusBadRequest, "invalid_template", "Invalid template syntax: "+err.Error())
			return
		}
		existing.Content = req.Content
	}
	if req.Version != "" {
		existing.Version = req.Version
	}
	if req.IsActive != nil {
		existing.IsActive = *req.IsActive
	}
	if req.Labels != nil {
		existing.Labels = req.Labels
	}

	if err := h.store.UpdatePromptTemplate(*existing); err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to update prompt template: "+err.Error())
		return
	}

	if h.auditor != nil {
		oldVals := map[string]any{
			"name":        existingOld.Name,
			"description": existingOld.Description,
			"content":     existingOld.Content,
			"version":     existingOld.Version,
			"is_active":   existingOld.IsActive,
			"labels":      existingOld.Labels,
		}
		newVals := map[string]any{
			"name":        existing.Name,
			"description": existing.Description,
			"content":     existing.Content,
			"version":     existing.Version,
			"is_active":   existing.IsActive,
			"labels":      existing.Labels,
		}
		if err := h.auditor.LogUpdate("prompt_template", strconv.Itoa(id), oldVals, newVals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Error("failed to log audit update prompt_template", "error", err)
		}
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (h *PromptHandler) DeleteTemplate(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid ID format")
		return
	}

	oldTmpl, fetchErr := h.store.GetPromptTemplate(id)

	if err := h.store.DeletePromptTemplate(id); err != nil {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Prompt template not found")
		return
	}

	if h.auditor != nil && fetchErr == nil && oldTmpl != nil {
		vals := map[string]any{
			"name":        oldTmpl.Name,
			"description": oldTmpl.Description,
			"content":     oldTmpl.Content,
			"version":     oldTmpl.Version,
			"is_active":   oldTmpl.IsActive,
			"labels":      oldTmpl.Labels,
		}
		if err := h.auditor.LogDelete("prompt_template", strconv.Itoa(id), vals, reqmeta.GetKeyID(r.Context())); err != nil {
			slog.Error("failed to log audit delete prompt_template", "error", err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *PromptHandler) GetVersions(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid ID format")
		return
	}

	versions, err := h.store.GetPromptTemplateVersions(id)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to get versions")
		return
	}
	if versions == nil {
		versions = []config.PromptTemplateVersion{}
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"versions": versions,
	})
}

func (h *PromptHandler) GetTemplateByName(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Name is required")
		return
	}

	tmpl, err := h.store.GetPromptTemplateByName(name)
	if err != nil {
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to get prompt template")
		return
	}
	if tmpl == nil {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Prompt template not found")
		return
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"prompt": tmpl,
	})
}
