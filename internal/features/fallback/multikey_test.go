package fallback_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/features/fallback"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/platform/cooldown"
	"github.com/ilter-ai/ilter/internal/provider"
)

func TestMultiAPIKey_AutomaticFailover(t *testing.T) {
	// 1. Mock server that returns 429 on key-1 and 200 on key-2
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "Bearer key-1-failing" {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error": {"message": "Rate limit reached for key 1"}}`))
			return
		}
		if authHeader == "Bearer key-2-working" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"chatcmpl-123","choices":[{"message":{"content":"Hello from key 2"}}],"usage":{"total_tokens":10}}`))
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	// 2. Setup ProviderConfig with 2 API keys
	pCfg := config.ProviderConfig{
		Name:    "mock-openai",
		Type:    "openai",
		BaseURL: server.URL,
		APIKeys: []string{"key-1-failing", "key-2-working"},
		Models:  []config.ModelConfig{{Name: "mock-model"}},
	}

	pvd := provider.NewOpenAIProvider(pCfg)
	reg := provider.NewRegistry()
	reg.Register(pvd)

	// 3. Setup LoadBalancer & generate candidates
	cfg := &config.Config{
		Providers: []config.ProviderConfig{pCfg},
	}
	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	require.NoError(t, err)

	cooldownStore := cooldown.NewInMemoryStore()
	candidates, err := lb.SelectCandidates(context.Background(), "mock-model", "", cooldownStore)
	require.NoError(t, err)

	// Verify SelectCandidates expanded both API keys into candidates
	require.Len(t, candidates, 2)
	assert.Equal(t, "key_1", candidates[0].KeyID)
	assert.Equal(t, "key-1-failing", candidates[0].APIKey)
	assert.Equal(t, "key_2", candidates[1].KeyID)
	assert.Equal(t, "key-2-working", candidates[1].APIKey)

	// 4. Execute with FallbackExecutor
	fbCfg := config.FallbackConfig{
		Enabled:          true,
		CooldownDuration: 5 * time.Minute,
		MaxAttempts:      5,
	}
	fe := fallback.NewFallbackExecutor(fbCfg, cooldownStore, reg)

	res, execErr := fe.Execute(context.Background(), candidates, func(c context.Context, cand cooldown.Candidate, p provider.Provider) (int, http.Header, error) {
		callCtx := c
		if cand.APIKey != "" {
			callCtx = provider.WithSelectedAPIKey(c, cand.APIKey)
		}
		req, errTransform := p.TransformRequest(callCtx, nil)
		if errTransform != nil {
			return http.StatusBadRequest, nil, errTransform
		}
		resp, errDo := p.Client().Do(req)
		if errDo != nil {
			return 0, nil, errDo
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return resp.StatusCode, resp.Header, fmt.Errorf("status %d", resp.StatusCode)
		}
		return resp.StatusCode, resp.Header, nil
	})

	// 5. Assertions: FallbackExecutor should succeed on Candidate 2 after Candidate 1 failed with 429
	require.NoError(t, execErr)
	require.NotNil(t, res)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "key_2", res.Candidate.KeyID)
	assert.Equal(t, "key-2-working", res.Candidate.APIKey)

	// Verify key_0 is now in cooldown so future calls skip it
	assert.True(t, cooldownStore.InCooldown(context.Background(), candidates[0]))
	assert.False(t, cooldownStore.InCooldown(context.Background(), candidates[1]))
}
