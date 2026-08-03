package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/middleware"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
	"github.com/ilter-ai/ilter/internal/platform/cooldown"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
	"github.com/ilter-ai/ilter/internal/provider"
)

// resolveSingleCandidateProvider picks the first available candidate for
// modelName and resolves its Provider. Unlike ChatCompletions, embeddings
// and rerank requests don't go through the fallback executor — a single
// upstream attempt is enough for these lower-traffic, non-conversational
// endpoints. The returned context carries the candidate's selected API key
// (mirroring ChatCompletions), and candidate.Model is the actual resolved
// model name to send upstream, which can differ from the requested one.
func (h *Handler) resolveSingleCandidateProvider(w http.ResponseWriter, r *http.Request, modelName string) (provider.Provider, cooldown.Candidate, context.Context, bool) {
	candidates, err := h.lb.SelectCandidates(r.Context(), modelName, "", h.cooldownStore)
	if err != nil {
		model.WriteJSONError(w, http.StatusNotFound, model.ErrTypeModelNotFound, err.Error())
		return nil, cooldown.Candidate{}, nil, false
	}
	if len(candidates) == 0 {
		model.WriteJSONError(w, http.StatusNotFound, model.ErrTypeModelNotFound, "no candidates available")
		return nil, cooldown.Candidate{}, nil, false
	}

	cand := candidates[0]
	pvd, err := h.lb.GetProvider(cand.Provider)
	if err != nil {
		model.WriteJSONError(w, http.StatusNotFound, model.ErrTypeModelNotFound, err.Error())
		return nil, cooldown.Candidate{}, nil, false
	}

	ctx := r.Context()
	if cand.APIKey != "" {
		ctx = provider.WithSelectedAPIKey(ctx, cand.APIKey)
	}
	return pvd, cand, ctx, true
}

// recordDataEndpointOutcome mirrors the cost accounting and audit logging
// ChatCompletions does (CalculateCost + budgetEnforcer.RecordUsage +
// auditLogger.LogAsync) for the embeddings/rerank endpoints, which otherwise
// sit behind BudgetMiddleware's pre-flight check without ever depleting the
// tracked budget or showing up in the audit/usage dashboards.
func (h *Handler) recordDataEndpointOutcome(ctx context.Context, r *http.Request, providerName, modelName string, usage *model.Usage, statusCode int, start time.Time) {
	var modelCfg config.ModelConfig
	if routes, err := h.lb.GetRoutes(modelName); err == nil && len(routes) > 0 {
		modelCfg = routes[0].Model
	}
	promptTokens, completionTokens := 0, 0
	if usage != nil {
		promptTokens = usage.PromptTokens
		completionTokens = usage.CompletionTokens
	}
	cost := CalculateCost(modelCfg, promptTokens, completionTokens)
	keyID := reqmeta.GetKeyID(ctx)

	if h.budgetEnforcer != nil {
		if err := h.budgetEnforcer.RecordUsage(ctx, keyID, cost); err != nil {
			slog.Error("failed to record budget usage", "endpoint", "data", "model", modelName, "error", err)
		}
	}
	if h.auditLogger != nil {
		h.auditLogger.LogAsync(middleware.AuditLogEntry{
			IPAddress:        extractClientIP(r),
			KeyID:            keyID,
			Model:            modelName,
			Provider:         providerName,
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalCost:        cost,
			LatencyMs:        int(time.Since(start) / time.Millisecond),
			StatusCode:       statusCode,
		})
	}
}

// Embeddings handles POST /v1/embeddings.
func (h *Handler) Embeddings(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	start := time.Now()

	var req model.EmbeddingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, model.ErrTypeInvalidRequest, fmt.Sprintf("invalid json body: %v", err))
		return
	}
	if req.Model == "" {
		model.WriteJSONError(w, http.StatusBadRequest, model.ErrTypeInvalidRequest, "model is required")
		return
	}
	req.Model = catalog.CanonicalModelID(req.Model)

	pvd, cand, ctx, ok := h.resolveSingleCandidateProvider(w, r, req.Model)
	if !ok {
		return
	}
	req.Model = cand.Model

	embedder, ok := pvd.(provider.EmbeddingProvider)
	if !ok {
		model.WriteJSONError(w, http.StatusBadRequest, model.ErrTypeInvalidRequest, fmt.Sprintf("provider %q does not support embeddings", pvd.Name()))
		return
	}

	resp, err := embedder.Embed(ctx, &req)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadGateway, model.ErrTypeProviderError, sanitizeProviderErrorMessage(err.Error()))
		return
	}
	if resp.Object == "" {
		resp.Object = "list"
	}
	if resp.Model == "" {
		resp.Model = req.Model
	}
	h.recordDataEndpointOutcome(ctx, r, pvd.Name(), req.Model, resp.Usage, http.StatusOK, start)

	model.WriteJSON(w, http.StatusOK, resp)
}

// Rerank handles POST /v1/rerank.
func (h *Handler) Rerank(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	start := time.Now()

	var req model.RerankRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		model.WriteJSONError(w, http.StatusBadRequest, model.ErrTypeInvalidRequest, fmt.Sprintf("invalid json body: %v", err))
		return
	}
	if req.Model == "" {
		model.WriteJSONError(w, http.StatusBadRequest, model.ErrTypeInvalidRequest, "model is required")
		return
	}
	if req.Query == "" || len(req.Documents) == 0 {
		model.WriteJSONError(w, http.StatusBadRequest, model.ErrTypeInvalidRequest, "query and documents are required")
		return
	}
	req.Model = catalog.CanonicalModelID(req.Model)

	pvd, cand, ctx, ok := h.resolveSingleCandidateProvider(w, r, req.Model)
	if !ok {
		return
	}
	req.Model = cand.Model

	reranker, ok := pvd.(provider.RerankProvider)
	if !ok {
		model.WriteJSONError(w, http.StatusBadRequest, model.ErrTypeInvalidRequest, fmt.Sprintf("provider %q does not support reranking", pvd.Name()))
		return
	}

	resp, err := reranker.Rerank(ctx, &req)
	if err != nil {
		model.WriteJSONError(w, http.StatusBadGateway, model.ErrTypeProviderError, sanitizeProviderErrorMessage(err.Error()))
		return
	}
	if resp.Model == "" {
		resp.Model = req.Model
	}
	h.recordDataEndpointOutcome(ctx, r, pvd.Name(), req.Model, resp.Usage, http.StatusOK, start)

	model.WriteJSON(w, http.StatusOK, resp)
}
