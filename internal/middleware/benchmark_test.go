package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/db/dbtest"
	"github.com/ilter-ai/ilter/internal/model"
	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

type noopHandler struct{}

func (h *noopHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func benchPIIBody() *bytes.Reader {
	return bytes.NewReader([]byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`))
}

func BenchmarkAuthMiddleware_AdminKey(b *testing.B) {
	store, cleanup := setupBenchStore(b)
	defer cleanup()
	auth := NewAuthMiddleware(config.AuthConfig{}, store)
	handler := auth.Handler(&noopHandler{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", benchPIIBody())
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkAuthMiddleware_AdminToken(b *testing.B) {
	store, cleanup := setupBenchStore(b)
	defer cleanup()
	auth := NewAuthMiddleware(config.AuthConfig{AdminKey: "test-admin-key"}, store)
	handler := auth.Handler(&noopHandler{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
		req.Header.Set("Authorization", "Bearer test-admin-key")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkAuthMiddleware_MissingToken(b *testing.B) {
	store, cleanup := setupBenchStore(b)
	defer cleanup()
	auth := NewAuthMiddleware(config.AuthConfig{AdminKey: "test-admin-key"}, store)
	handler := auth.Handler(&noopHandler{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkRateLimitMiddleware_Disabled(b *testing.B) {
	rl, err := NewRateLimitMiddleware(&config.RateLimitConfig{Enabled: false}, nil, nil)
	if err != nil {
		b.Fatal(err)
	}
	handler := rl.Handler(&noopHandler{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", benchPIIBody())
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkBudgetMiddleware_Disabled(b *testing.B) {
	store, cleanup := setupBenchStore(b)
	defer cleanup()
	be := NewBudgetMiddleware(config.BudgetConfig{Enabled: false}, nil, store, nil)
	handler := be.Handler(&noopHandler{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", benchPIIBody())
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkRequestLogger(b *testing.B) {
	handler := RequestLogger(&noopHandler{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", benchPIIBody())
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkPIIMasker_CleanText(b *testing.B) {
	store, cleanup := setupBenchStore(b)
	defer cleanup()
	mw := NewPIIMaskerMiddleware(store, config.PIIConfig{Enabled: true}, nil, nil)
	handler := mw.Handler(&noopHandler{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"Hello, how are you today?"}]}`)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkPIIMasker_PIIText(b *testing.B) {
	store, cleanup := setupBenchStore(b)
	defer cleanup()
	mw := NewPIIMaskerMiddleware(store, config.PIIConfig{Enabled: true}, nil, nil)
	handler := mw.Handler(&noopHandler{})
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"My email is john@example.com"}]}`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkPIIMasker_SkipNonPost(b *testing.B) {
	store, cleanup := setupBenchStore(b)
	defer cleanup()
	mw := NewPIIMaskerMiddleware(store, config.PIIConfig{Enabled: true}, nil, nil)
	handler := mw.Handler(&noopHandler{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/health", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkContextOperations(b *testing.B) {
	ctx := context.Background()
	_, meta := reqmeta.InitRequestMetadata(ctx)

	b.Run("reqmeta.InitRequestMetadata", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = reqmeta.InitRequestMetadata(ctx)
		}
	})

	b.Run("reqmeta.GetRequestMetadata", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = reqmeta.GetRequestMetadata(ctx)
		}
	})

	b.Run("reqmeta.GetKeyID", func(b *testing.B) {
		bCtx := context.WithValue(ctx, reqmeta.KeyIDContextKey, "42")
		for i := 0; i < b.N; i++ {
			_ = reqmeta.GetKeyID(bCtx)
		}
	})

	b.Run("SetAndGetMetadata_fields", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			meta.SetCacheHit(true)
			meta.SetPIIMasked(true)
			meta.SetSmartRouted(true, "gpt-4o")
			meta.SetComplexityScore(45.5)
			_ = meta.SlogAttrs()
		}
	})
}

func BenchmarkPromptInjection_NoHeaders(b *testing.B) {
	store, cleanup := setupBenchStore(b)
	defer cleanup()
	mw := NewPromptInjectionMiddleware(store)
	handler := mw.Handler(&noopHandler{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", benchPIIBody())
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkSemanticCache_Disabled(b *testing.B) {
	sc := NewSemanticCacheMiddleware(config.CacheConfig{Enabled: false}, nil, nil)
	handler := sc.Handler(&noopHandler{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", benchPIIBody())
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkSemanticCache_NonPost(b *testing.B) {
	sc := NewSemanticCacheMiddleware(config.CacheConfig{Enabled: true}, nil, nil)
	handler := sc.Handler(&noopHandler{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("GET", "/health", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkFullMiddlewareChain(b *testing.B) {
	store, cleanup := setupBenchStore(b)
	defer cleanup()
	r := chi.NewRouter()
	auth := NewAuthMiddleware(config.AuthConfig{}, store)
	rl, _ := NewRateLimitMiddleware(&config.RateLimitConfig{Enabled: false}, nil, nil)
	be := NewBudgetMiddleware(config.BudgetConfig{Enabled: false}, nil, store, nil)
	promptMW := NewPromptInjectionMiddleware(store)
	piiMW := NewPIIMaskerMiddleware(store, config.PIIConfig{Enabled: true}, nil, nil)
	sc := NewSemanticCacheMiddleware(config.CacheConfig{Enabled: false}, nil, nil)
	r.Use(auth.Handler)
	r.Use(rl.Handler)
	r.Use(be.Handler)
	r.Use(promptMW.Handler)
	r.Use(piiMW.Handler)
	r.Use(sc.Handler)
	r.Post("/v1/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"Hello, how are you?"}]}`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(reqBody)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-admin-key")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkFullMiddlewareChain_PIIText(b *testing.B) {
	store, cleanup := setupBenchStore(b)
	defer cleanup()
	r := chi.NewRouter()
	auth := NewAuthMiddleware(config.AuthConfig{}, store)
	rl, _ := NewRateLimitMiddleware(&config.RateLimitConfig{Enabled: false}, nil, nil)
	be := NewBudgetMiddleware(config.BudgetConfig{Enabled: false}, nil, store, nil)
	promptMW := NewPromptInjectionMiddleware(store)
	piiMW := NewPIIMaskerMiddleware(store, config.PIIConfig{Enabled: true}, nil, nil)
	sc := NewSemanticCacheMiddleware(config.CacheConfig{Enabled: false}, nil, nil)
	r.Use(auth.Handler)
	r.Use(rl.Handler)
	r.Use(be.Handler)
	r.Use(promptMW.Handler)
	r.Use(piiMW.Handler)
	r.Use(sc.Handler)
	r.Post("/v1/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"My email is john@example.com and my card is 4111-1111-1111-1111"}]}`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(reqBody)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-admin-key")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
	}
}

func BenchmarkWriteJSONError(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		model.WriteJSONError(rec, http.StatusUnauthorized, "auth_error", "Invalid API key")
	}
}

var benchStoreMu sync.Mutex

func setupBenchStore(t testing.TB) (*db.SQLiteStore, func()) {
	benchStoreMu.Lock()
	store := dbtest.NewFile(t)
	return store, benchStoreMu.Unlock
}
