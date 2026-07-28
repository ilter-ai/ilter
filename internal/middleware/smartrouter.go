package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync/atomic"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/model"

	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

type strategyContextKey struct{}

var StrategyKey strategyContextKey

type preferenceContextKey struct{}

var PreferenceKey preferenceContextKey

type SmartRouterMiddleware struct {
	configCache *config.Cache
	smartRouter atomic.Pointer[smartrouter.SmartRouter]
}

func NewSmartRouterMiddleware(configCache *config.Cache, smartRouter *smartrouter.SmartRouter) *SmartRouterMiddleware {
	m := &SmartRouterMiddleware{configCache: configCache}
	m.smartRouter.Store(smartRouter)
	return m
}

// UpdateSmartRouter atomically swaps the SmartRouter instance on config change.
func (m *SmartRouterMiddleware) UpdateSmartRouter(sr *smartrouter.SmartRouter) {
	m.smartRouter.Store(sr)
}

func (m *SmartRouterMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		snap := m.configCache.Get()
		if snap == nil {
			next.ServeHTTP(w, r)
			return
		}

		rc := snap.RoutingConfig()
		if !rc.Enabled {
			next.ServeHTTP(w, r)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))

		var req model.ChatCompletionRequest
		if err = json.Unmarshal(body, &req); err != nil {
			next.ServeHTTP(w, r)
			return
		}

		if req.Model != "" {
			ctx := context.WithValue(r.Context(), StrategyKey, req.Model)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		sr := m.smartRouter.Load()
		selectedModel, score, err := sr.RouteRequest(r.Context(), &req)
		if err != nil || selectedModel == "" {
			ctx := context.WithValue(r.Context(), StrategyKey, "")
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		preference := rc.ProviderPreference
		if preference == "" {
			preference = "cheapest"
		}
		ctx := context.WithValue(r.Context(), StrategyKey, selectedModel)
		ctx = context.WithValue(ctx, PreferenceKey, preference)
		if meta := reqmeta.GetRequestMetadata(r.Context()); meta != nil {
			meta.SetSmartRouted(true, selectedModel)
			meta.SetComplexityScore(score)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
