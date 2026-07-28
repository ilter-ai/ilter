package dashjobs

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ilter-ai/ilter/internal/jobs"
	"github.com/ilter-ai/ilter/internal/jobs/triggers"
	"github.com/ilter-ai/ilter/internal/model"
)

// ---------------------------------------------------------------------------
// Job Execution
// ---------------------------------------------------------------------------

// TriggerJob handles POST /api/jobs/{id}/trigger.
func (h *JobsHandler) TriggerJob(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Job ID is required")
		return
	}

	job, err := h.store.GetJob(id)
	if err != nil {
		h.logger.Error("failed to get job for trigger", "id", id, "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to get job")
		return
	}
	if job == nil {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Job not found")
		return
	}

	activation := triggers.Activation{
		TriggerID:      "manual",
		JobID:          job.ID,
		IdempotencyKey: fmt.Sprintf("manual_%d", time.Now().UnixNano()),
		Payload:        nil,
		ReceivedAt:     time.Now(),
	}
	runID, err := h.dispatcher.HandleActivation(r.Context(), activation)
	if err != nil {
		if errors.Is(err, triggers.ErrJobDisabled) {
			model.WriteJSONError(w, http.StatusConflict, "job_disabled", "Job is disabled — enable it before triggering a run")
			return
		}
		h.logger.Error("failed to dispatch job trigger", "id", id, "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to trigger job: "+err.Error())
		return
	}

	model.WriteJSON(w, http.StatusAccepted, map[string]string{
		"run_id": runID,
	})
}

// ListRuns handles GET /api/jobs/{id}/runs.
func (h *JobsHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Job ID is required")
		return
	}

	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if perPage < 1 {
		perPage = 20
	}

	runs, err := h.store.ListRuns(id, page, perPage)
	if err != nil {
		h.logger.Error("failed to list runs", "job_id", id, "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list runs")
		return
	}

	total, err := h.store.CountRuns(id)
	if err != nil {
		h.logger.Error("failed to count runs", "job_id", id, "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to count runs")
		return
	}

	data := make([]map[string]any, 0, len(runs))
	for _, run := range runs {
		data = append(data, runToResponse(run))
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"data":     data,
		"total":    total,
		"page":     page,
		"per_page": perPage,
	})
}

// GetRun handles GET /api/jobs/{id}/runs/{runId}.
func (h *JobsHandler) GetRun(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runId")
	if runID == "" {
		model.WriteJSONError(w, http.StatusBadRequest, "invalid_request_error", "Run ID is required")
		return
	}

	run, err := h.store.GetRun(runID)
	if err != nil {
		h.logger.Error("failed to get run", "run_id", runID, "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to get run")
		return
	}
	if run == nil {
		model.WriteJSONError(w, http.StatusNotFound, "not_found", "Run not found")
		return
	}

	model.WriteJSON(w, http.StatusOK, runToResponse(*run))
}

// GetStats handles GET /api/jobs/stats.
func (h *JobsHandler) GetStats(w http.ResponseWriter, _ *http.Request) {
	allJobs, err := h.store.ListJobs()
	if err != nil {
		h.logger.Error("failed to list jobs for stats", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to get stats")
		return
	}

	enabledCount := 0
	for _, j := range allJobs {
		if j.Enabled {
			enabledCount++
		}
	}

	var totalRuns, errorCount int
	_ = h.store.DB().QueryRow("SELECT COUNT(*) FROM job_runs").Scan(&totalRuns)
	_ = h.store.DB().QueryRow("SELECT COUNT(*) FROM job_runs WHERE status NOT IN ('success', 'running', 'pending')").Scan(&errorCount)

	recent := make([]map[string]any, 0)
	rows, err := h.store.DB().Query(
		`SELECT id, job_id, trigger_id, status, llm_result, llm_error,
		 delivery_result, delivery_error, prompt_tokens, completion_tokens, cost_usd,
		 started_at, completed_at, duration_ms, request_body, execution_key, steps
		 FROM job_runs ORDER BY started_at DESC LIMIT 10`,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var r jobs.JobRun
			if err := rows.Scan(&r.ID, &r.JobID, &r.TriggerID, &r.Status,
				&r.LLMResult, &r.LLMError, &r.DeliveryResult, &r.DeliveryError,
				&r.PromptTokens, &r.CompletionTokens, &r.Cost,
				&r.StartedAt, &r.FinishedAt, &r.DurationMs,
				&r.RequestBody, &r.ExecutionKey, &r.Steps); err == nil {
				recent = append(recent, runToResponse(r))
			}
		}
	}

	model.WriteJSON(w, http.StatusOK, map[string]any{
		"jobs": map[string]any{
			"total":   len(allJobs),
			"enabled": enabledCount,
		},
		"executions": map[string]any{
			"total":  totalRuns,
			"errors": errorCount,
		},
		"recent": recent,
	})
}

// ---------------------------------------------------------------------------
// Dead Letter Queue
// ---------------------------------------------------------------------------

// ListDeadLetterRuns handles GET /admin/jobs/dead-letter.
func (h *JobsHandler) ListDeadLetterRuns(w http.ResponseWriter, r *http.Request) {
	runs, err := h.store.ListDeadLetterRuns(r.Context())
	if err != nil {
		h.logger.Error("failed to list dead letter runs", "error", err)
		model.WriteJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list dead letter runs")
		return
	}
	data := make([]map[string]any, 0, len(runs))
	for _, run := range runs {
		data = append(data, runToResponse(run))
	}
	model.WriteJSON(w, http.StatusOK, data)
}
