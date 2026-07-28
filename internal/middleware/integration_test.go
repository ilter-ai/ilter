package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/db/dbtest"
	"github.com/ilter-ai/ilter/internal/model"

	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

// setupChainTestStore creates a temporary SQLiteStore for integration tests.
func setupChainTestStore(t *testing.T) *db.SQLiteStore {
	t.Helper()
	return dbtest.NewFile(t)
}

// seedAPIKey creates a virtual API key for non-admin auth tests.
// Returns the raw token that can be used in the Authorization header.
func seedAPIKey(t *testing.T, store *db.SQLiteStore, name string) string {
	t.Helper()

	_, rawToken, err := store.CreateAPIKey(name, nil, nil, 100.0, 0, 50, 0, nil, nil, nil)
	require.NoError(t, err)

	return rawToken
}

// errorResponse is a minimal JSON structure matching model.ErrorResponse.
type errorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

func TestMiddlewareChain_Auth(t *testing.T) {
	store := setupChainTestStore(t)

	adminToken := "admin-token-123"
	authCfg := config.AuthConfig{AdminKey: adminToken}
	auth := NewAuthMiddleware(authCfg, store)

	// Build a minimal chi router with only auth middleware
	r := chi.NewRouter()
	r.Use(auth.Handler)
	r.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		keyID := reqmeta.GetKeyID(r.Context())
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"key_id": keyID,
		})
	})

	t.Run("no auth header returns 401", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Code)

		var errResp errorResponse
		err := json.Unmarshal(rr.Body.Bytes(), &errResp)
		require.NoError(t, err)
		assert.Equal(t, "authentication_error", errResp.Error.Type)
		assert.Contains(t, errResp.Error.Message, "Missing or invalid")
		assert.Equal(t, "401", errResp.Error.Code)
	})

	t.Run("invalid bearer token returns 401", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer some-random-token")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Code)

		var errResp errorResponse
		err := json.Unmarshal(rr.Body.Bytes(), &errResp)
		require.NoError(t, err)
		assert.Equal(t, "authentication_error", errResp.Error.Type)
	})

	t.Run("invalid auth header format (no Bearer) returns 401", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Basic somecreds")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Code)

		var errResp errorResponse
		err := json.Unmarshal(rr.Body.Bytes(), &errResp)
		require.NoError(t, err)
		assert.Equal(t, "authentication_error", errResp.Error.Type)
	})

	t.Run("valid admin token returns 200 with zero key_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(rr.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "ok", resp["status"])
		assert.Equal(t, "admin", resp["key_id"]) // admin key → "admin"
	})

	t.Run("valid API key returns 200 with key_id", func(t *testing.T) {
		rawToken := seedAPIKey(t, store, "auth-test-key")

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(rr.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "ok", resp["status"])
		keyID := resp["key_id"].(string)
		assert.NotEmpty(t, keyID)
	})

	t.Run("x-api-key header is accepted (Anthropic compat)", func(t *testing.T) {
		rawToken := seedAPIKey(t, store, "anthropic-key")

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("x-api-key", rawToken)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(rr.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "ok", resp["status"])
	})
}

func TestMiddlewareChain_AdminKey(t *testing.T) {
	store := setupChainTestStore(t)

	adminToken := "admin-integration-test-key"
	auth := NewAuthMiddleware(config.AuthConfig{AdminKey: adminToken}, store)

	r := chi.NewRouter()
	r.Use(auth.Handler)
	r.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		keyID := reqmeta.GetKeyID(r.Context())
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"key_id": keyID,
		})
	})

	t.Run("admin key accepted", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(rr.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "ok", resp["status"])
		assert.Equal(t, "admin", resp["key_id"])
	})

	t.Run("unknown token rejected", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer some-unknown-token")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Code)
	})
}

func TestMiddlewareChain_ContextPropagation(t *testing.T) {
	store := setupChainTestStore(t)

	adminToken := "admin-ctx-test"
	authCfg := config.AuthConfig{AdminKey: adminToken}
	auth := NewAuthMiddleware(authCfg, store)

	r := chi.NewRouter()
	r.Use(auth.Handler)
	r.Get("/test", func(w http.ResponseWriter, r *http.Request) {
		keyID := reqmeta.GetKeyID(r.Context())
		budget, budgetOk := reqmeta.GetAPIKeyBudget(r.Context())
		dailyLimit, dailyLimitOk := reqmeta.GetAPIKeyDailyLimit(r.Context())

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"key_id":         keyID,
			"budget":         budget,
			"budget_ok":      budgetOk,
			"daily_limit":    dailyLimit,
			"daily_limit_ok": dailyLimitOk,
		})
	})

	t.Run("admin key sets key_id=\"admin\", no budget/rate-limit", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(rr.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "admin", resp["key_id"])
		assert.Equal(t, false, resp["budget_ok"])
		assert.Equal(t, false, resp["daily_limit_ok"])
	})

	t.Run("API key sets context with budget and rate limit", func(t *testing.T) {
		rawToken := seedAPIKey(t, store, "ctx-prop-key")

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+rawToken)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(rr.Body.Bytes(), &resp)
		require.NoError(t, err)
		keyID := resp["key_id"].(string)
		assert.NotEmpty(t, keyID)
		assert.Equal(t, true, resp["budget_ok"])
		assert.Equal(t, 100.0, resp["budget"])
	})
}

func TestMiddlewareChain_PIIMasking(t *testing.T) {
	store := setupChainTestStore(t)

	// Auth + PII in the chain
	adminToken := "admin-pii-test"
	authCfg := config.AuthConfig{AdminKey: adminToken}
	auth := NewAuthMiddleware(authCfg, store)

	piiCfg := config.PIIConfig{
		Enabled: true,
		Mode:    "mask",
	}
	pii := NewPIIMaskerMiddleware(store, piiCfg, nil, nil)

	r := chi.NewRouter()
	r.Use(auth.Handler)
	r.Use(pii.Handler)
	r.Post("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		// Read the body as received by the final handler
		bodyBytes, _ := io.ReadAll(r.Body)
		r.Body.Close()

		var req model.ChatCompletionRequest
		_ = json.Unmarshal(bodyBytes, &req)

		content := ""
		if len(req.Messages) > 0 {
			if s, ok := req.Messages[0].Content.(string); ok {
				content = s
			}
		}

		keyID := reqmeta.GetKeyID(r.Context())
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":          "ok",
			"key_id":          keyID,
			"masked_content":  content,
			"original_length": len(bodyBytes),
		})
	})

	t.Run("PII is masked in request body through chain", func(t *testing.T) {
		reqBody := model.ChatCompletionRequest{
			Model: "gpt-4o",
			Messages: []model.Message{
				{Role: "user", Content: "My email is user@example.com and card is 4321-0987-6543-2107"},
			},
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(rr.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "ok", resp["status"])
		assert.Equal(t, "admin", resp["key_id"])

		maskedContent, ok := resp["masked_content"].(string)
		require.True(t, ok, "masked_content should be a string")
		assert.NotContains(t, maskedContent, "user@example.com")
		assert.NotContains(t, maskedContent, "4321-0987-6543-2107")
		assert.Contains(t, maskedContent, "<MASKED_PII>")
	})

	t.Run("non-POST path skips PII middleware", func(t *testing.T) {
		// PII only runs on POST /v1/chat/completions
		r2 := chi.NewRouter()
		r2.Use(auth.Handler)
		r2.Use(pii.Handler)
		r2.Get("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
			bodyBytes, _ := io.ReadAll(r.Body)
			r.Body.Close()

			keyID := reqmeta.GetKeyID(r.Context())
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "ok",
				"key_id": keyID,
				"body":   string(bodyBytes),
			})
		})

		reqBody := model.ChatCompletionRequest{
			Model: "gpt-4o",
			Messages: []model.Message{
				{Role: "user", Content: "My email is user@example.com"},
			},
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("GET", "/v1/chat/completions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rr := httptest.NewRecorder()
		r2.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(rr.Body.Bytes(), &resp)
		require.NoError(t, err)

		// Body should still contain the email (PII not masked on GET)
		body := resp["body"].(string)
		assert.Contains(t, body, "user@example.com")
	})

	t.Run("PII block mode rejects via chain", func(t *testing.T) {
		piiBlock := NewPIIMaskerMiddleware(store, config.PIIConfig{
			Enabled: true,
			Mode:    "block",
		}, nil, nil)

		rBlock := chi.NewRouter()
		rBlock.Use(auth.Handler)
		rBlock.Use(piiBlock.Handler)
		rBlock.Post("/v1/chat/completions", func(_ http.ResponseWriter, _ *http.Request) {
			t.Fatal("next handler should not be called in block mode")
		})

		reqBody := model.ChatCompletionRequest{
			Model: "gpt-4o",
			Messages: []model.Message{
				{Role: "user", Content: "My email is user@example.com"},
			},
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rr := httptest.NewRecorder()
		rBlock.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnprocessableEntity, rr.Code)

		var errResp errorResponse
		err := json.Unmarshal(rr.Body.Bytes(), &errResp)
		require.NoError(t, err)
		assert.Equal(t, "pii_blocked", errResp.Error.Code)
		assert.Equal(t, "pii_blocked", errResp.Error.Type)
	})

	t.Run("PII reversible mode masks in request and unmask in response", func(t *testing.T) {
		piiRev := NewPIIMaskerMiddleware(store, config.PIIConfig{
			Enabled: true,
			Mode:    "reversible",
		}, nil, nil)

		rRev := chi.NewRouter()
		rRev.Use(auth.Handler)
		rRev.Use(piiRev.Handler)
		rRev.Post("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
			bodyBytes, _ := io.ReadAll(r.Body)
			r.Body.Close()

			var req model.ChatCompletionRequest
			_ = json.Unmarshal(bodyBytes, &req)
			content := req.Messages[0].Content.(string)

			// Find placeholder and echo it back in the response
			var placeholder string
			for _, word := range strings.Fields(content) {
				if strings.HasPrefix(word, "PII:") {
					placeholder = strings.TrimRight(word, ".,")
					break
				}
			}
			require.NotEmpty(t, placeholder, "expected a placeholder in masked body")

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Contact: ` + placeholder + `"}}],"usage":{"prompt_tokens":5,"completion_tokens":5}}`))
		})

		reqBody := model.ChatCompletionRequest{
			Model: "gpt-4o",
			Messages: []model.Message{
				{Role: "user", Content: "My email is user@example.com"},
			},
		}
		bodyBytes, _ := json.Marshal(reqBody)

		req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rr := httptest.NewRecorder()
		rRev.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		respBody := rr.Body.String()
		assert.Contains(t, respBody, "user@example.com")
		assert.NotContains(t, respBody, "PII:")
	})
}

func TestMiddlewareChain_ErrorResponseFormat(t *testing.T) {
	store := setupChainTestStore(t)

	auth := NewAuthMiddleware(config.AuthConfig{AdminKey: "admin"}, store)

	r := chi.NewRouter()
	r.Use(auth.Handler)
	r.Get("/test", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("error response has proper JSON format", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusUnauthorized, rr.Code)
		assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

		var parsed map[string]interface{}
		err := json.Unmarshal(rr.Body.Bytes(), &parsed)
		require.NoError(t, err, "response must be valid JSON")

		errObj, ok := parsed["error"].(map[string]interface{})
		require.True(t, ok, "response must have error object")

		errType, ok := errObj["type"].(string)
		require.True(t, ok, "error.type must be a string")
		assert.Equal(t, "authentication_error", errType)

		msg, ok := errObj["message"].(string)
		require.True(t, ok, "error.message must be a string")
		assert.NotEmpty(t, msg)

		code, ok := errObj["code"].(string)
		require.True(t, ok, "error.code must be a string")
		assert.Equal(t, "401", code)
	})
}

func TestMiddlewareChain_ReversibleCrossRequest(t *testing.T) {
	t.Skip("cross-request unmask requires Redis: set pii.redis_url in config")

	store := setupChainTestStore(t)

	adminToken := "admin-cross-request"
	authCfg := config.AuthConfig{AdminKey: adminToken}
	auth := NewAuthMiddleware(authCfg, store)

	pii := NewPIIMaskerMiddleware(store, config.PIIConfig{
		Enabled: true,
		Mode:    "reversible",
	}, nil, nil)

	r := chi.NewRouter()
	r.Use(auth.Handler)
	r.Use(pii.Handler)

	// Track placeholder across requests
	var sharedPlaceholder string

	// Request 1: Mask PII and capture placeholder
	r.Post("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, _ := io.ReadAll(r.Body)
		r.Body.Close()

		var req model.ChatCompletionRequest
		_ = json.Unmarshal(bodyBytes, &req)
		content := req.Messages[0].Content.(string)

		for _, word := range strings.Fields(content) {
			if strings.HasPrefix(word, "PII:") {
				sharedPlaceholder = strings.TrimRight(word, ".,")
				break
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":5,"completion_tokens":5}}`))
	})

	email := "cross-request@example.com"
	reqBody1 := model.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []model.Message{
			{Role: "user", Content: "my email is " + email},
		},
	}
	bodyBytes1, _ := json.Marshal(reqBody1)
	req1 := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes1))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Authorization", "Bearer "+adminToken)
	rr1 := httptest.NewRecorder()
	r.ServeHTTP(rr1, req1)
	assert.Equal(t, http.StatusOK, rr1.Code)
	require.NotEmpty(t, sharedPlaceholder, "must capture placeholder")

	// Request 2: No PII in input, but downstream writes the old placeholder
	// The cache should unmask it back to the original email
	r2 := chi.NewRouter()
	r2.Use(auth.Handler)
	r2.Use(pii.Handler)
	r2.Post("/v1/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Your email is ` + sharedPlaceholder + `"}}],"usage":{"prompt_tokens":5,"completion_tokens":5}}`))
	})

	reqBody2 := model.ChatCompletionRequest{
		Model: "gpt-4o",
		Messages: []model.Message{
			{Role: "user", Content: "What was my email?"},
		},
	}
	bodyBytes2, _ := json.Marshal(reqBody2)
	req2 := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(bodyBytes2))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+adminToken)
	rr2 := httptest.NewRecorder()
	r2.ServeHTTP(rr2, req2)

	assert.Equal(t, http.StatusOK, rr2.Code)
	respBody2 := rr2.Body.String()
	assert.NotContains(t, respBody2, sharedPlaceholder)
	assert.Contains(t, respBody2, email)
}
