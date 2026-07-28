package dashjobs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ilter-ai/ilter/internal/jobs/triggers"
	"github.com/ilter-ai/ilter/internal/model"
)

// ---------------------------------------------------------------------------
// Trigger Management
// ---------------------------------------------------------------------------

// ListTriggers handles GET /api/jobs/{id}/triggers.
func (h *JobsHandler) ListTriggers(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Job ID is required")
		return
	}

	trigs, err := h.trigStore.ListByJobID(id)
	if err != nil {
		h.logger.Error("failed to list triggers", "job_id", id, "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list triggers")
		return
	}

	resp := make([]TriggerResponse, 0, len(trigs))
	for _, t := range trigs {
		resp = append(resp, h.triggerToResponse(t, false))
	}
	model.WriteJSON(w, http.StatusOK, resp)
}

// CreateTrigger handles POST /api/jobs/{id}/triggers.
func (h *JobsHandler) CreateTrigger(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	if jobID == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Job ID is required")
		return
	}

	defer r.Body.Close()
	var input triggerInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}

	tc, err := validateTriggerConfig(input)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_trigger", err.Error())
		return
	}

	tr := triggers.TriggerRow{
		ID:      fmt.Sprintf("trg_%d", time.Now().UnixNano()),
		JobID:   jobID,
		Kind:    triggers.TriggerKind(input.Kind),
		Enabled: true,
		Config:  tc,
	}
	if input.Kind == "webhook" {
		token, secret, err := generateWebhookCredentials()
		if err != nil {
			model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to generate webhook credentials")
			return
		}
		tr.Token = token
		tr.Secret = secret
	}
	if err := h.trigStore.Create(r.Context(), tr); err != nil {
		h.logger.Error("failed to create trigger", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to create trigger: "+err.Error())
		return
	}
	h.refreshCron()

	model.WriteJSON(w, http.StatusCreated, h.triggerToResponse(tr, true))
}

type triggerCredentials struct {
	Token  string `json:"token"`
	Secret string `json:"secret"`
}

// RevealTrigger handles GET /api/jobs/{id}/triggers/{triggerId}/reveal.
func (h *JobsHandler) RevealTrigger(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "id")
	triggerID := chi.URLParam(r, "triggerId")
	if jobID == "" || triggerID == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Job ID and trigger ID are required")
		return
	}

	tr, err := h.trigStore.Get(triggerID)
	if err != nil {
		h.logger.Error("failed to get trigger", "trigger_id", triggerID, "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to get trigger")
		return
	}
	if tr == nil || tr.JobID != jobID {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Trigger not found")
		return
	}
	if tr.Kind != triggers.TriggerKindWebhook {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_trigger", "Only webhook triggers have credentials to reveal")
		return
	}

	model.WriteJSON(w, http.StatusOK, triggerCredentials{Token: tr.Token, Secret: tr.Secret})
}

// DeleteTrigger handles DELETE /api/triggers/{id}.
func (h *JobsHandler) DeleteTrigger(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Trigger ID is required")
		return
	}

	if err := h.trigStore.Delete(id); err != nil {
		h.logger.Error("failed to delete trigger", "id", id, "error", err)
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Trigger not found")
		return
	}
	h.refreshCron()

	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// Webhook
// ---------------------------------------------------------------------------

// WebhookHandler handles POST /api/webhooks/{token}.
func (h *JobsHandler) WebhookHandler(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		if _, ok := err.(*http.MaxBytesError); ok {
			model.WriteJSONError(w, http.StatusRequestEntityTooLarge, "body_too_large", "Request body exceeds 1MB limit")
		} else {
			model.WriteJSONError(w, http.StatusBadRequest, "invalid_body", "Failed to read request body")
		}
		return
	}

	token := chi.URLParam(r, "token")
	if token == "" {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Trigger not found")
		return
	}

	wh := triggers.NewWebhookTrigger(h.trigStore, *h.cfg, h.logger)
	activation, statusCode, err := wh.VerifyAndActivate(r.Context(), token, r, rawBody)
	if err != nil || activation == nil {
		if statusCode == http.StatusInternalServerError {
			h.logger.Error("webhook verification error", "error", err)
		}
		model.WriteJSONError(w, statusCode, "verification_failed", "Trigger not found or verification failed")
		return
	}

	runID, err := h.dispatcher.HandleActivation(r.Context(), *activation)
	if err != nil {
		if errors.Is(err, triggers.ErrJobDisabled) {
			model.WriteJSONError(w, http.StatusConflict, "job_disabled", "Job is disabled")
			return
		}
		h.logger.Error("failed to dispatch webhook activation", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to process webhook")
		return
	}

	model.WriteJSON(w, http.StatusAccepted, map[string]string{
		"run_id": runID,
	})
}
