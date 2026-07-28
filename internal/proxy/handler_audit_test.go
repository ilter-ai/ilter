package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db/dbtest"
	"github.com/ilter-ai/ilter/internal/features/smartrouter"
	"github.com/ilter-ai/ilter/internal/middleware"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/model/catalog"
	"github.com/ilter-ai/ilter/internal/provider"
)

// testAuditProvider is a mock provider that can simulate various errors.
type testAuditProvider struct {
	name            string
	errTransform    error
	errClient       error
	errTransformRes error
	clientResp      *http.Response
}

func (p *testAuditProvider) Name() string                        { return p.name }
func (p *testAuditProvider) Type() string                        { return "test" }
func (p *testAuditProvider) HealthCheck(_ context.Context) error { return nil }
func (p *testAuditProvider) DiscoverModels(_ context.Context) ([]catalog.ModelInfo, error) {
	return nil, nil
}

func (p *testAuditProvider) TransformRequest(ctx context.Context, _ *model.ChatCompletionRequest) (*http.Request, error) {
	if p.errTransform != nil {
		return nil, p.errTransform
	}
	return http.NewRequestWithContext(ctx, "POST", "http://test", nil)
}

func (p *testAuditProvider) TransformResponse(_ context.Context, _ *http.Response) (*model.ChatCompletionResponse, error) {
	if p.errTransformRes != nil {
		return nil, p.errTransformRes
	}
	return &model.ChatCompletionResponse{
		Choices: []model.Choice{{Message: model.ChoiceMessage{Role: "assistant", Content: "ok"}}},
		Usage:   &model.Usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
	}, nil
}

func (p *testAuditProvider) Client() *http.Client {
	if p.errClient != nil {
		return &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			return nil, p.errClient
		})}
	}
	return &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		if p.clientResp != nil {
			return p.clientResp, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`))),
		}, nil
	})}
}

// TestRecordErrorAudit_JSONDecodeError tests audit logging for JSON decode error.
func TestRecordErrorAudit_JSONDecodeError(t *testing.T) {
	store := dbtest.New(t)

	auditLogger := middleware.NewAuditLoggerMiddleware(store)
	t.Cleanup(func() { auditLogger.Close() })

	cfg := &config.Config{
		Providers: []config.ProviderConfig{{Name: "test", Type: "openai", Models: []config.ModelConfig{{Name: "gpt-4o-mini", Weight: 1}}}},
	}

	reg := provider.NewRegistry()
	reg.Register(&testAuditProvider{name: "test"})

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	require.NoError(t, err)

	h := NewHandler(lb, auditLogger, nil, nil)
	h.SetConfig(cfg)

	// Invalid JSON body
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(`invalid json`)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.ChatCompletions(rr, req)

	// Wait for async audit log write
	time.Sleep(50 * time.Millisecond)

	// Verify error response
	assert.Equal(t, http.StatusBadRequest, rr.Code)

	// Verify audit log was written
	var count int
	err = store.DB.QueryRow("SELECT COUNT(*) FROM audit_log WHERE status_code = 400 AND key_id IS NULL").Scan(&count)
	require.NoError(t, err)
	assert.Greater(t, count, 0, "expected audit log entry for JSON decode error")
}

// TestRecordErrorAudit_NoModel tests audit logging when no model is selected.
func TestRecordErrorAudit_NoModel(t *testing.T) {
	store := dbtest.New(t)

	auditLogger := middleware.NewAuditLoggerMiddleware(store)
	t.Cleanup(func() { auditLogger.Close() })

	cfg := &config.Config{
		Providers: []config.ProviderConfig{{Name: "test", Type: "openai", Models: []config.ModelConfig{{Name: "gpt-4o-mini", Weight: 1}}}},
	}

	reg := provider.NewRegistry()
	reg.Register(&testAuditProvider{name: "test"})

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	require.NoError(t, err)

	h := NewHandler(lb, auditLogger, nil, nil)
	h.SetConfig(cfg)

	// Request without model
	reqBody := model.ChatCompletionRequest{
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.ChatCompletions(rr, req)

	// Wait for async audit log write
	time.Sleep(50 * time.Millisecond)

	// Verify error response
	assert.Equal(t, http.StatusBadRequest, rr.Code)

	// Verify audit log was written
	var count int
	err = store.DB.QueryRow("SELECT COUNT(*) FROM audit_log WHERE status_code = 400").Scan(&count)
	require.NoError(t, err)
	assert.Greater(t, count, 0, "expected audit log entry for no model error")
}

// TestRecordErrorAudit_NoCandidates tests audit logging when no candidates found.
func TestRecordErrorAudit_NoCandidates(t *testing.T) {
	store := dbtest.New(t)

	auditLogger := middleware.NewAuditLoggerMiddleware(store)
	t.Cleanup(func() { auditLogger.Close() })

	cfg := &config.Config{
		Providers: []config.ProviderConfig{{Name: "test", Type: "openai", Models: []config.ModelConfig{{Name: "gpt-4o-mini", Weight: 1}}}},
	}

	reg := provider.NewRegistry()
	reg.Register(&testAuditProvider{name: "test"})

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	require.NoError(t, err)

	h := NewHandler(lb, auditLogger, nil, nil)
	h.SetConfig(cfg)

	// Request for unknown model
	reqBody := model.ChatCompletionRequest{
		Model:    "unknown-model",
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.ChatCompletions(rr, req)

	// Wait for async audit log write
	time.Sleep(50 * time.Millisecond)

	// Verify error response
	assert.Equal(t, http.StatusNotFound, rr.Code)

	// Verify audit log was written
	var count int
	err = store.DB.QueryRow("SELECT COUNT(*) FROM audit_log WHERE status_code = 404").Scan(&count)
	require.NoError(t, err)
	assert.Greater(t, count, 0, "expected audit log entry for no candidates error")
}

// TestRecordErrorAudit_ProviderError tests audit logging for provider 4xx/5xx errors.
func TestRecordErrorAudit_ProviderError(t *testing.T) {
	store := dbtest.New(t)

	auditLogger := middleware.NewAuditLoggerMiddleware(store)
	t.Cleanup(func() { auditLogger.Close() })

	cfg := &config.Config{
		Providers: []config.ProviderConfig{{Name: "test", Type: "openai", Models: []config.ModelConfig{{Name: "gpt-4o-mini", Weight: 1}}}},
	}

	reg := provider.NewRegistry()
	// Provider that returns 400 error
	reg.Register(&testAuditProvider{
		name: "test",
		clientResp: &http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       io.NopCloser(bytes.NewReader([]byte(`{"error":"bad request"}`))),
		},
	})

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	require.NoError(t, err)

	h := NewHandler(lb, auditLogger, nil, nil)
	h.SetConfig(cfg)

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-4o-mini",
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.ChatCompletions(rr, req)

	// Wait for async audit log write
	time.Sleep(50 * time.Millisecond)

	// Verify error response
	assert.Equal(t, http.StatusBadRequest, rr.Code)

	// Verify audit log was written
	var count int
	err = store.DB.QueryRow("SELECT COUNT(*) FROM audit_log WHERE status_code = 400").Scan(&count)
	require.NoError(t, err)
	assert.Greater(t, count, 0, "expected audit log entry for provider error")
}

// TestRecordErrorAudit_Success tests that successful requests still audit correctly.
func TestRecordErrorAudit_Success(t *testing.T) {
	store := dbtest.New(t)

	auditLogger := middleware.NewAuditLoggerMiddleware(store)
	t.Cleanup(func() { auditLogger.Close() })

	cfg := &config.Config{
		Providers: []config.ProviderConfig{{Name: "test", Type: "openai", Models: []config.ModelConfig{{Name: "gpt-4o-mini", Weight: 1}}}},
	}

	reg := provider.NewRegistry()
	reg.Register(&testAuditProvider{name: "test"})

	lb, err := smartrouter.NewLoadBalancer(cfg, reg, nil)
	require.NoError(t, err)

	h := NewHandler(lb, auditLogger, nil, nil)
	h.SetConfig(cfg)

	reqBody := model.ChatCompletionRequest{
		Model:    "gpt-4o-mini",
		Messages: []model.Message{{Role: "user", Content: "hello"}},
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.ChatCompletions(rr, req)

	// Wait for async audit log write
	time.Sleep(50 * time.Millisecond)

	// Verify success response
	assert.Equal(t, http.StatusOK, rr.Code)

	// Verify audit log was written with status 200
	var count int
	err = store.DB.QueryRow("SELECT COUNT(*) FROM audit_log WHERE status_code = 200").Scan(&count)
	require.NoError(t, err)
	assert.Greater(t, count, 0, "expected audit log entry for successful request")
}
