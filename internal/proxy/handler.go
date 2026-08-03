package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/features/budget"
	"github.com/ilter-ai/ilter/internal/features/fallback"
	"github.com/ilter-ai/ilter/internal/features/loopdetect"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/middleware"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
	"github.com/ilter-ai/ilter/internal/platform/cooldown"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
	"github.com/ilter-ai/ilter/internal/provider"
)

type Handler struct {
	lb               *smartrouter.LoadBalancer
	auditLogger      *middleware.AuditLoggerMiddleware
	budgetEnforcer   *budget.Enforcer
	loopDetector     *loopdetect.Detector
	cfg              *config.Config
	configCache      *config.Cache
	store            *db.SQLiteStore
	cooldownStore    cooldown.Store
	fallbackExecutor *fallback.FallbackExecutor
	chatChain        http.Handler
}

func (h *Handler) SetFallbackExecutor(fe *fallback.FallbackExecutor, store cooldown.Store) {
	h.fallbackExecutor = fe
	h.cooldownStore = store
}

// SetChatChain wires in the chat-completions middleware chain (auth, budget,
// PII, guardrails, MCP tool injection, smart routing, semantic cache, loop
// detection) so that wire-format-translating endpoints like AnthropicMessages
// and LegacyCompletions can re-enter it and get identical behavior/headers to
// a native /v1/chat/completions request.
func (h *Handler) SetChatChain(chain http.Handler) {
	h.chatChain = chain
}

// ChatChain returns the chat-completions middleware chain, for other
// consumers (e.g. the dashboard's request-replay feature) that need the same
// reference SetChatChain was given.
func (h *Handler) ChatChain() http.Handler {
	return h.chatChain
}

func NewHandler(
	lb *smartrouter.LoadBalancer,
	auditLogger *middleware.AuditLoggerMiddleware,
	budgetEnforcer *budget.Enforcer,
	loopDetector *loopdetect.Detector,
) *Handler {
	return &Handler{
		lb:             lb,
		auditLogger:    auditLogger,
		budgetEnforcer: budgetEnforcer,
		loopDetector:   loopDetector,
	}
}

func (h *Handler) SetStore(store *db.SQLiteStore) {
	h.store = store
}

// SetConfigCache sets the runtime config cache for the handler. The cache
// provides access to decrypted provider API keys and other runtime config.
// It is safe to call concurrently with requests (the cache uses atomic snapshots).
func (h *Handler) SetConfigCache(cache *config.Cache) {
	h.configCache = cache
}

func (h *Handler) SetConfig(cfg *config.Config) { h.cfg = cfg }

func (h *Handler) emitStandard() bool {
	return h.cfg != nil && h.cfg.Headers.EmitStandard
}

// resolveRequestedModel resolves the model to use for this request.
// Priority: middleware-selected model (from SmartRouterMiddleware) > explicit model in the
// request body. Returns ok=false and writes an error response
// when no model can be resolved.
func (h *Handler) resolveRequestedModel(w http.ResponseWriter, r *http.Request, req *model.ChatCompletionRequest, meta *reqmeta.RequestLoggingMetadata) (selectedModel string, complexityScore float64, ok bool) {
	// 1. Check if SmartRouterMiddleware already selected a model
	if midModel, ok := r.Context().Value(middleware.StrategyKey).(string); ok && midModel != "" {
		selectedModel = midModel
		if meta != nil {
			meta.WithLock(func() {
				complexityScore = meta.ComplexityScore
			})
		}
	} else if req.Model != "" {
		// 2. Use explicit model from request body
		selectedModel = req.Model
		complexityScore = smartrouter.ScoreComplexity(req.Messages)
		if meta != nil {
			meta.SetComplexityScore(complexityScore)
		}
	} else {
		model.WriteJSONError(w, http.StatusBadRequest, model.ErrTypeInvalidRequest, "no model selected in request")
		return "", 0, false
	}

	if canonical := catalog.CanonicalModelID(selectedModel); canonical != selectedModel {
		slog.Debug("model: stripped provider prefix", "before", selectedModel, "after", canonical)
		selectedModel = canonical
	}

	return selectedModel, complexityScore, true
}

func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	meta := reqmeta.GetRequestMetadata(r.Context())

	defer r.Body.Close()
	var req model.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.recordErrorAudit(r, nil, "", http.StatusBadRequest, err, start, nil)
		model.WriteJSONError(w, http.StatusBadRequest, model.ErrTypeInvalidRequest, fmt.Sprintf("invalid json body: %v", err))
		return
	}

	keyID := reqmeta.GetKeyID(r.Context())
	if meta != nil {
		meta.SetKeyID(keyID)
	}

	selectedModel, complexityScore, ok := h.resolveRequestedModel(w, r, &req, meta)
	if !ok {
		h.recordErrorAudit(r, nil, "", http.StatusBadRequest, fmt.Errorf("no model selected in request"), start, req.Messages)
		return
	}
	req.Model = selectedModel

	preference, _ := r.Context().Value(middleware.PreferenceKey).(string)
	candidates, err := h.lb.SelectCandidates(r.Context(), req.Model, preference, h.cooldownStore)
	if err != nil {
		h.recordErrorAudit(r, nil, selectedModel, http.StatusNotFound, err, start, req.Messages)
		model.WriteJSONError(w, http.StatusNotFound, model.ErrTypeModelNotFound, err.Error())
		return
	}

	routes, _ := h.lb.GetRoutes(req.Model)
	var firstRoute smartrouter.Route
	if len(routes) > 0 {
		firstRoute = routes[0]
	}

	// Cost estimate response headers (computed after routes are known)
	costEstimate, altCost, savingsPotential := computeCostEstimates(req.Messages, firstRoute.Model, selectedModel, req.MaxTokens)
	setPreRequest(w, preRequest{
		ComplexityScore:  complexityScore,
		SelectedModel:    selectedModel,
		CostEstimate:     costEstimate,
		AlternativeCost:  altCost,
		SavingsPotential: savingsPotential,
	}, h.emitStandard())

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	var resp *http.Response
	var p provider.Provider
	var finalCandidate cooldown.Candidate

	if h.fallbackExecutor != nil {
		res, execErr := h.fallbackExecutor.Execute(ctx, candidates, func(c context.Context, cand cooldown.Candidate, pvd provider.Provider) (int, http.Header, error) {
			callCtx := c
			if cand.APIKey != "" {
				callCtx = provider.WithSelectedAPIKey(c, cand.APIKey)
			}
			// ModelDowngrade: cand.Model may differ from req.Model when falling
			// back to an alternative model. Use cand.Model so the upstream
			// receives the actual model the candidate serves.
			req.Model = cand.Model
			providerReq, errTransform := pvd.TransformRequest(callCtx, &req)
			if errTransform != nil {
				return http.StatusBadRequest, nil, errTransform
			}
			httpResp, errDo := pvd.Client().Do(providerReq)
			if errDo != nil {
				return 0, nil, errDo
			}
			if httpResp.StatusCode >= 400 {
				headers := httpResp.Header
				bodyBytes, _ := io.ReadAll(httpResp.Body)
				httpResp.Body.Close()
				cleanMsg := sanitizeProviderErrorMessage(string(bodyBytes))
				return httpResp.StatusCode, headers, fmt.Errorf("provider %s status %d: %s", pvd.Name(), httpResp.StatusCode, cleanMsg)
			}
			resp = httpResp
			p = pvd
			finalCandidate = cand
			return httpResp.StatusCode, httpResp.Header, nil
		})
		if execErr != nil {
			statusCode := http.StatusServiceUnavailable
			if res != nil && res.StatusCode > 0 {
				statusCode = res.StatusCode
			}
			errType := model.ErrTypeAllProvidersFail
			if statusCode == http.StatusTooManyRequests {
				errType = model.ErrTypeInsufficientQuota
			}
			cleanMsg := sanitizeProviderErrorMessage(execErr.Error())
			slog.Error("all providers failed", "model", req.Model, "error", execErr)
			h.recordErrorAudit(r, nil, req.Model, statusCode, execErr, start, req.Messages)
			model.WriteJSONError(w, statusCode, errType, cleanMsg)
			return
		}
	} else {
		if len(candidates) == 0 {
			h.recordErrorAudit(r, nil, req.Model, http.StatusNotFound, fmt.Errorf("no candidates available"), start, req.Messages)
			model.WriteJSONError(w, http.StatusNotFound, model.ErrTypeModelNotFound, "no candidates available")
			return
		}
		firstCand := candidates[0]
		pvd, errGet := h.lb.GetProvider(firstCand.Provider)
		if errGet != nil {
			h.recordErrorAudit(r, nil, req.Model, http.StatusNotFound, errGet, start, req.Messages)
			model.WriteJSONError(w, http.StatusNotFound, model.ErrTypeModelNotFound, errGet.Error())
			return
		}
		providerReq, errTransform := pvd.TransformRequest(ctx, &req)
		if errTransform != nil {
			h.recordErrorAudit(r, nil, req.Model, http.StatusBadRequest, errTransform, start, req.Messages)
			model.WriteJSONError(w, http.StatusBadRequest, model.ErrTypeInvalidRequest, errTransform.Error())
			return
		}
		httpResp, errDo := pvd.Client().Do(providerReq)
		if errDo != nil {
			h.recordErrorAudit(r, nil, req.Model, http.StatusBadGateway, errDo, start, req.Messages)
			model.WriteJSONError(w, http.StatusBadGateway, model.ErrTypeAllProvidersFail, errDo.Error())
			return
		}
		if httpResp.StatusCode >= 400 {
			bodyBytes, _ := io.ReadAll(httpResp.Body)
			httpResp.Body.Close()
			cleanMsg := sanitizeProviderErrorMessage(string(bodyBytes))
			statusCode := httpResp.StatusCode
			errType := model.ErrTypeProviderError
			if statusCode == http.StatusTooManyRequests {
				errType = model.ErrTypeInsufficientQuota
			}
			h.recordErrorAudit(r, nil, req.Model, statusCode, fmt.Errorf("%s", cleanMsg), start, req.Messages)
			model.WriteJSONError(w, statusCode, errType, cleanMsg)
			return
		}
		resp = httpResp
		p = pvd
		finalCandidate = firstCand
	}

	finalRoute := smartrouter.Route{
		Provider: p,
		Model: config.ModelConfig{
			Name: finalCandidate.Model,
		},
	}

	if req.Stream {
		h.handleStreaming(ctx, cancel, w, resp, p, &finalRoute, req.Model, start, r, &req)
		return
	}

	defer resp.Body.Close()

	chatResp, err := p.TransformResponse(ctx, resp)
	if err != nil {
		statusCode := providerErrorStatus(err)
		errType := model.ErrTypeProviderError
		if statusCode == http.StatusTooManyRequests {
			errType = model.ErrTypeInsufficientQuota
		}
		h.recordErrorAudit(r, &finalRoute, req.Model, statusCode, err, start, req.Messages)
		model.WriteJSONError(w, statusCode, errType, fmt.Sprintf("failed to parse provider response: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	actualModel := finalRoute.Model.Name
	if finalRoute.Provider != nil {
		actualModel = finalRoute.Provider.Name() + "/" + finalRoute.Model.Name
	}
	w.Header().Set("X-Ilter-Model-Actual", actualModel)
	if chatResp.Usage != nil {
		actualCost := CalculateCost(finalRoute.Model, chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens)
		setPostResponse(w, postResponse{
			Model:            finalRoute.Model.Name,
			PromptTokens:     chatResp.Usage.PromptTokens,
			CompletionTokens: chatResp.Usage.CompletionTokens,
			ActualCost:       actualCost,
		}, h.emitStandard())
		w.Header().Set("X-Ilter-Cost", strconv.FormatFloat(math.Round(actualCost*1e6)/1e6, 'f', -1, 64))
	}
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(chatResp); err != nil {
		slog.Debug("encode response error", "error", err)
	}

	h.recordAudit(r, &finalRoute, req.Model, chatResp, http.StatusOK, start, false, req.Messages)
	h.recordPostResponse(r, chatResp, &finalRoute, start)
}

func (h *Handler) recordAudit(
	r *http.Request,
	route *smartrouter.Route,
	requestedModel string,
	chatResp *model.ChatCompletionResponse,
	statusCode int,
	start time.Time,
	cacheHit bool,
	messages []model.Message,
) {
	if h.auditLogger == nil {
		return
	}

	promptTokens := 0
	completionTokens := 0
	if chatResp != nil && chatResp.Usage != nil {
		promptTokens = chatResp.Usage.PromptTokens
		completionTokens = chatResp.Usage.CompletionTokens
	}

	cost := CalculateCost(route.Model, promptTokens, completionTokens)
	latencyMs := int(time.Since(start) / time.Millisecond)

	keyID := reqmeta.GetKeyID(r.Context())

	var promptPreview string
	if h.cfg != nil && h.cfg.Audit.LogPrompts && len(messages) > 0 {
		lastMsg := messages[len(messages)-1]
		if contentStr, ok := lastMsg.Content.(string); ok {
			promptPreview = contentStr
			if len(promptPreview) > 200 {
				promptPreview = promptPreview[:200]
			}
		}
	}

	var requestBody string
	if h.cfg != nil && h.cfg.Audit.LogBodies && len(messages) > 0 {
		reqJSON := map[string]any{
			"messages": messages,
		}
		if b, err := json.Marshal(reqJSON); err == nil {
			requestBody = string(b)
		}
	}

	var responseBody string
	if h.cfg != nil && h.cfg.Audit.LogBodies && chatResp != nil {
		if b, err := json.Marshal(chatResp); err == nil {
			responseBody = string(b)
		}
	}

	var complexityScore float64
	if meta := reqmeta.GetRequestMetadata(r.Context()); meta != nil {
		meta.WithLock(func() {
			complexityScore = meta.ComplexityScore
		})
	}

	h.auditLogger.LogAsync(middleware.AuditLogEntry{
		IPAddress:        extractClientIP(r),
		KeyID:            keyID,
		Model:            requestedModel,
		Provider:         route.Provider.Name(),
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalCost:        cost,
		LatencyMs:        latencyMs,
		StatusCode:       statusCode,
		CacheHit:         cacheHit,
		PromptPreview:    promptPreview,
		RequestBody:      requestBody,
		ResponseBody:     responseBody,
		ComplexityScore:  complexityScore,
	})
}

// recordErrorAudit records an audit log entry for error paths in ChatCompletions.
// It handles partial information (nil route, nil messages) gracefully.
func (h *Handler) recordErrorAudit(
	r *http.Request,
	route *smartrouter.Route,
	requestedModel string,
	statusCode int,
	err error,
	start time.Time,
	messages []model.Message,
) {
	if h.auditLogger == nil {
		return
	}

	keyID := reqmeta.GetKeyID(r.Context())

	var providerName string
	if route != nil && route.Provider != nil {
		providerName = route.Provider.Name()
	}

	var promptPreview string
	if h.cfg != nil && h.cfg.Audit.LogPrompts && len(messages) > 0 {
		lastMsg := messages[len(messages)-1]
		if contentStr, ok := lastMsg.Content.(string); ok {
			promptPreview = contentStr
			if len(promptPreview) > 200 {
				promptPreview = promptPreview[:200]
			}
		}
	}

	var requestBody string
	if h.cfg != nil && h.cfg.Audit.LogBodies && len(messages) > 0 {
		reqJSON := map[string]any{
			"messages": messages,
		}
		if b, marshalErr := json.Marshal(reqJSON); marshalErr == nil {
			requestBody = string(b)
		}
	}

	var responseBody string
	if h.cfg != nil && h.cfg.Audit.LogBodies && err != nil {
		responseBody = err.Error()
	}

	var complexityScore float64
	if meta := reqmeta.GetRequestMetadata(r.Context()); meta != nil {
		meta.WithLock(func() {
			complexityScore = meta.ComplexityScore
		})
	}

	latencyMs := int(time.Since(start) / time.Millisecond)

	h.auditLogger.LogAsync(middleware.AuditLogEntry{
		IPAddress:        extractClientIP(r),
		KeyID:            keyID,
		Model:            requestedModel,
		Provider:         providerName,
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalCost:        0,
		LatencyMs:        latencyMs,
		StatusCode:       statusCode,
		CacheHit:         false,
		PromptPreview:    promptPreview,
		RequestBody:      requestBody,
		ResponseBody:     responseBody,
		ComplexityScore:  complexityScore,
	})
}

// extractClientIP resolves the client IP from X-Forwarded-For, X-Real-IP, or RemoteAddr.
func extractClientIP(r *http.Request) string {
	clientIP := r.RemoteAddr
	if idx := strings.LastIndex(clientIP, ":"); idx != -1 {
		clientIP = clientIP[:idx]
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	return clientIP
}

func (h *Handler) Models(w http.ResponseWriter, _ *http.Request) {
	infos := h.lb.GetAvailableModelInfos()

	data := make([]map[string]any, 0, len(infos))
	for _, info := range infos {
		data = append(data, map[string]any{
			"id":       info.Name,
			"object":   "model",
			"created":  time.Now().Unix(),
			"owned_by": info.OwnedBy,
			"provider": info.Provider,
			"type":     info.Type,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   data,
	}); err != nil {
		slog.Debug("encode models error", "error", err)
	}
}
