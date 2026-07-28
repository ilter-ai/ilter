package stats

import (
	"encoding/json"
	"net/http"

	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"

	"github.com/ilter-ai/ilter/internal/features/circuitbreaker"
)

type circuitBreakerStats struct {
	TotalCircuits int  `json:"total_circuits"`
	ClosedCount   int  `json:"closed_count"`
	OpenCount     int  `json:"open_count"`
	HalfOpenCount int  `json:"half_open_count"`
	TotalFailures int  `json:"total_failures_24h"`
	Enabled       bool `json:"enabled"`
}

type circuitStatus struct {
	Provider             string `json:"provider"`
	State                string `json:"state"`
	Requests             int    `json:"requests"`
	Successes            int    `json:"successes"`
	Failures             int    `json:"failures"`
	ConsecutiveSuccesses int    `json:"consecutive_successes"`
	ConsecutiveFailures  int    `json:"consecutive_failures"`
}

type CircuitBreakerSummaryResponse struct {
	Summary  circuitBreakerStats `json:"summary"`
	Circuits []circuitStatus     `json:"circuits"`
}

func (h *Handler) HandleCircuitBreakerSummary(w http.ResponseWriter, _ *http.Request) {
	providers := h.reg.List()

	var resp CircuitBreakerSummaryResponse
	resp.Circuits = make([]circuitStatus, 0, len(providers))

	for _, prov := range providers {
		client := prov.Client()
		if client == nil || client.Transport == nil {
			continue
		}

		state := circuitbreaker.State(client.Transport)
		cs := circuitStatus{
			Provider: prov.Name(),
			State:    state,
		}

		if counts := circuitbreaker.Counts(client.Transport); counts != nil {
			cs.Requests = int(counts.Requests)
			cs.Successes = int(counts.TotalSuccesses)
			cs.Failures = int(counts.TotalFailures)
			cs.ConsecutiveSuccesses = int(counts.ConsecutiveSuccesses)
			cs.ConsecutiveFailures = int(counts.ConsecutiveFailures)
		}

		// Providers with ≤1 request have only seen a health-check ping, not real traffic.
		if cs.Requests <= 1 {
			cs.State = "idle"
			cs.Requests = 0
			cs.Successes = 0
			cs.ConsecutiveSuccesses = 0
		}

		resp.Circuits = append(resp.Circuits, cs)
		resp.Summary.TotalFailures += cs.Failures

		switch cs.State {
		case "closed":
			resp.Summary.ClosedCount++
		case "open":
			resp.Summary.OpenCount++
		case "half-open":
			resp.Summary.HalfOpenCount++
		}
	}

	resp.Summary.TotalCircuits = len(resp.Circuits)

	resp.Summary.Enabled = true
	if len(providers) > 0 {
		if client := providers[0].Client(); client != nil {
			if t, ok := client.Transport.(*circuitbreaker.HTTPBreaker); ok {
				resp.Summary.Enabled = t.Enabled()
			}
		}
	}

	model.WriteJSON(w, http.StatusOK, resp)
}

func (h *Handler) forEachTransport(fn func(*circuitbreaker.HTTPBreaker)) {
	for _, prov := range h.reg.List() {
		client := prov.Client()
		if client == nil || client.Transport == nil {
			continue
		}
		if t, ok := client.Transport.(*circuitbreaker.HTTPBreaker); ok {
			fn(t)
		}
	}
}

// circuitBreakerRequest is sent from the toggle endpoint.
type circuitBreakerRequest struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason"`
}

func (h *Handler) HandleCircuitBreakerToggle(w http.ResponseWriter, r *http.Request) {
	actor := reqmeta.GetKeyID(r.Context())

	var req circuitBreakerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	h.forEachTransport(func(t *circuitbreaker.HTTPBreaker) {
		t.SetEnabled(req.Enabled)
	})

	if h.configAuditor != nil {
		vals := map[string]any{"enabled": req.Enabled}
		if req.Reason != "" {
			vals["reason"] = req.Reason
		}
		_ = h.configAuditor.LogCreate("circuit_breaker", "global", vals, actor)
	}

	model.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) HandleCircuitBreakerReset(w http.ResponseWriter, r *http.Request) {
	actor := reqmeta.GetKeyID(r.Context())

	var req struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	h.forEachTransport(func(t *circuitbreaker.HTTPBreaker) {
		t.Reset()
	})

	if h.configAuditor != nil {
		vals := map[string]any{"action": "reset"}
		if req.Reason != "" {
			vals["reason"] = req.Reason
		}
		_ = h.configAuditor.LogCreate("circuit_breaker", "global", vals, actor)
	}

	model.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) HandleCircuitBreakerForceOpen(w http.ResponseWriter, r *http.Request) {
	actor := reqmeta.GetKeyID(r.Context())

	var req struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	h.forEachTransport(func(t *circuitbreaker.HTTPBreaker) {
		t.SetForceOpen(true)
	})

	if h.configAuditor != nil {
		vals := map[string]any{"action": "force_open"}
		if req.Reason != "" {
			vals["reason"] = req.Reason
		}
		_ = h.configAuditor.LogCreate("circuit_breaker", "global", vals, actor)
	}

	model.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
