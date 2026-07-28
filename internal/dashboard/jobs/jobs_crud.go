package dashjobs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ilter-ai/ilter/internal/jobs"
	"github.com/ilter-ai/ilter/internal/jobs/triggers"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

// requestError marks an error that should surface as a 4xx client error
// rather than a 500.
type requestError struct {
	status int
	msg    string
}

func (e *requestError) Error() string { return e.msg }

// ---------------------------------------------------------------------------
// Job CRUD
// ---------------------------------------------------------------------------

// ListJobs handles GET /api/jobs.
func (h *JobsHandler) ListJobs(w http.ResponseWriter, _ *http.Request) {
	allJobs, err := h.store.ListJobs()
	if err != nil {
		h.logger.Error("failed to list jobs", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list jobs")
		return
	}
	resp := make([]JobResponse, 0, len(allJobs))
	for _, job := range allJobs {
		trigs, _ := h.trigStore.ListByJobID(job.ID)
		jr := h.jobToResponse(job, trigs, nil)
		if runs, err := h.store.ListRuns(job.ID, 1, 1); err == nil && len(runs) > 0 {
			latest := runs[0]
			jr.LastExecStatus = latest.Status
			if latest.StartedAt.Valid {
				jr.LastExecStartedAt = latest.StartedAt.Time.Format(time.RFC3339)
			}
		}
		resp = append(resp, jr)
	}
	model.WriteJSON(w, http.StatusOK, resp)
}

// CreateJob handles POST /api/jobs.
func (h *JobsHandler) CreateJob(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	var req createJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}
	if req.Name == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Name is required")
		return
	}

	if req.Steps != "" {
		if err := jobs.ValidateSteps(req.Steps, jobs.VariablesConfig(req.VariablesConfig)); err != nil {
			model.WriteJSONError(w, http.StatusBadRequest, "invalid_steps", "Invalid steps: "+err.Error())
			return
		}
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	now := time.Now()
	job := jobs.Job{
		ID:              fmt.Sprintf("%d", now.UnixNano()),
		Name:            req.Name,
		Description:     req.Description,
		StepsJSON:       req.Steps,
		VariablesConfig: req.VariablesConfig,
		DeliveryConfig:  req.DeliveryConfig,
		TimeoutMs:       req.TimeoutMs,
		Enabled:         enabled,
		APIKeyID:        req.APIKeyID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := h.store.CreateJob(r.Context(), &job); err != nil {
		h.logger.Error("failed to create job", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to create job: "+err.Error())
		return
	}

	createdTriggers := make([]triggers.TriggerRow, 0, len(req.Triggers))
	for _, ti := range req.Triggers {
		tc, err := validateTriggerConfig(ti)
		if err != nil {
			model.WriteJSONError(w, http.StatusBadRequest, "invalid_trigger", err.Error())
			return
		}
		tr := triggers.TriggerRow{
			ID:      fmt.Sprintf("trg_%d", time.Now().UnixNano()),
			JobID:   job.ID,
			Kind:    triggers.TriggerKind(ti.Kind),
			Enabled: true,
			Config:  tc,
		}
		if ti.Kind == "webhook" {
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
		createdTriggers = append(createdTriggers, tr)
	}
	if len(createdTriggers) > 0 {
		h.refreshCron()
	}

	if h.auditor != nil {
		vals := map[string]any{
			"name":    job.Name,
			"enabled": job.Enabled,
		}
		_ = h.auditor.LogCreate("job", job.ID, vals, reqmeta.GetKeyID(r.Context()))
	}

	revealTokenIDs := make(map[string]bool, len(createdTriggers))
	for _, t := range createdTriggers {
		revealTokenIDs[t.ID] = true
	}
	model.WriteJSON(w, http.StatusCreated, h.jobToResponse(job, createdTriggers, revealTokenIDs))
}

// GetJob handles GET /api/jobs/{id}.
func (h *JobsHandler) GetJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Job ID is required")
		return
	}

	job, err := h.store.GetJob(id)
	if err != nil {
		h.logger.Error("failed to get job", "id", id, "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to get job")
		return
	}
	if job == nil {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Job not found")
		return
	}

	trigs, _ := h.trigStore.ListByJobID(job.ID)
	jr := h.jobToResponse(*job, trigs, nil)
	if runs, err := h.store.ListRuns(job.ID, 1, 1); err == nil && len(runs) > 0 {
		latest := runs[0]
		jr.LastExecStatus = latest.Status
		if latest.StartedAt.Valid {
			jr.LastExecStartedAt = latest.StartedAt.Time.Format(time.RFC3339)
		}
	}
	model.WriteJSON(w, http.StatusOK, jr)
}

// UpdateJob handles PUT /api/jobs/{id}.
func (h *JobsHandler) UpdateJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Job ID is required")
		return
	}

	defer r.Body.Close()
	var req updateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Invalid request body")
		return
	}

	existing, err := h.store.GetJob(id)
	if err != nil {
		h.logger.Error("failed to get job for update", "id", id, "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to get job")
		return
	}
	if existing == nil {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Job not found")
		return
	}

	oldVals := map[string]any{
		"name":    existing.Name,
		"enabled": existing.Enabled,
	}

	if req.Name != "" {
		existing.Name = req.Name
	}
	if req.Description != "" {
		existing.Description = req.Description
	}
	if req.VariablesConfig != nil {
		existing.VariablesConfig = req.VariablesConfig
	}
	if req.DeliveryConfig != "" {
		existing.DeliveryConfig = req.DeliveryConfig
	}
	if req.Steps != "" {
		err = jobs.ValidateSteps(req.Steps, existing.VariablesConfig)
		if err != nil {
			model.WriteJSONError(w, http.StatusBadRequest, "invalid_steps", "Invalid steps: "+err.Error())
			return
		}
		existing.StepsJSON = req.Steps
	}
	if req.TimeoutMs != nil {
		existing.TimeoutMs = *req.TimeoutMs
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	if req.APIKeyID != "" {
		existing.APIKeyID = req.APIKeyID
	}

	err = h.store.UpdateJob(r.Context(), existing)
	if err != nil {
		h.logger.Error("failed to update job", "id", id, "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to update job: "+err.Error())
		return
	}
	if req.Enabled != nil {
		h.refreshCron()
	}

	if h.auditor != nil {
		newVals := map[string]any{
			"name":    existing.Name,
			"enabled": existing.Enabled,
		}
		_ = h.auditor.LogUpdate("job", id, oldVals, newVals, reqmeta.GetKeyID(r.Context()))
	}

	var newlyCreatedIDs map[string]bool
	if req.Triggers != nil {
		newlyCreatedIDs, err = h.reconcileTriggers(r.Context(), existing.ID, req.Triggers)
		if err != nil {
			if reqErr, ok := err.(*requestError); ok {
				model.WriteJSONError(w, reqErr.status, "invalid_trigger", reqErr.Error())
				return
			}
			h.logger.Error("failed to reconcile triggers", "id", id, "error", err)
			model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to update triggers: "+err.Error())
			return
		}
	}

	trigs, _ := h.trigStore.ListByJobID(existing.ID)
	model.WriteJSON(w, http.StatusOK, h.jobToResponse(*existing, trigs, newlyCreatedIDs))
}

// reconcileTriggers diffs the submitted trigger list against the job's
// existing triggers. Returns the set of newly-created trigger IDs.
func (h *JobsHandler) reconcileTriggers(ctx context.Context, jobID string, inputs []triggerInput) (map[string]bool, error) {
	existingTriggers, err := h.trigStore.ListByJobID(jobID)
	if err != nil {
		return nil, fmt.Errorf("list existing triggers: %w", err)
	}
	existingByID := make(map[string]triggers.TriggerRow, len(existingTriggers))
	for _, t := range existingTriggers {
		existingByID[t.ID] = t
	}

	keepIDs := make(map[string]bool, len(inputs))
	newlyCreatedIDs := make(map[string]bool)

	for _, ti := range inputs {
		tc, err := validateTriggerConfig(ti)
		if err != nil {
			return nil, &requestError{status: http.StatusBadRequest, msg: err.Error()}
		}

		if ti.ID != "" {
			tr, ok := existingByID[ti.ID]
			if !ok {
				return nil, &requestError{status: http.StatusBadRequest, msg: fmt.Sprintf("trigger %q does not belong to this job", ti.ID)}
			}
			keepIDs[ti.ID] = true
			tr.Config = tc
			if err := h.trigStore.Update(ctx, tr); err != nil {
				return nil, fmt.Errorf("update trigger %s: %w", ti.ID, err)
			}
			continue
		}

		tr := triggers.TriggerRow{
			ID:      fmt.Sprintf("trg_%d", time.Now().UnixNano()),
			JobID:   jobID,
			Kind:    triggers.TriggerKind(ti.Kind),
			Enabled: true,
			Config:  tc,
		}
		if ti.Kind == "webhook" {
			token, secret, err := generateWebhookCredentials()
			if err != nil {
				return nil, fmt.Errorf("generate webhook credentials: %w", err)
			}
			tr.Token = token
			tr.Secret = secret
		}
		if err := h.trigStore.Create(ctx, tr); err != nil {
			return nil, fmt.Errorf("create trigger: %w", err)
		}
		newlyCreatedIDs[tr.ID] = true
	}

	for _, t := range existingTriggers {
		if !keepIDs[t.ID] {
			if err := h.trigStore.Delete(t.ID); err != nil {
				return nil, fmt.Errorf("delete removed trigger %s: %w", t.ID, err)
			}
		}
	}

	h.refreshCron()
	return newlyCreatedIDs, nil
}

// DeleteJob handles DELETE /api/jobs/{id}.
func (h *JobsHandler) DeleteJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Job ID is required")
		return
	}

	oldJob, fetchErr := h.store.GetJob(id)

	trigs, _ := h.trigStore.ListByJobID(id)
	for _, t := range trigs {
		_ = h.trigStore.Delete(t.ID)
	}
	if len(trigs) > 0 {
		h.refreshCron()
	}

	if err := h.store.DeleteJob(id); err != nil {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Job not found")
		return
	}

	if h.auditor != nil && fetchErr == nil && oldJob != nil {
		vals := map[string]any{
			"name":    oldJob.Name,
			"enabled": oldJob.Enabled,
		}
		_ = h.auditor.LogDelete("job", id, vals, reqmeta.GetKeyID(r.Context()))
	}

	w.WriteHeader(http.StatusNoContent)
}
