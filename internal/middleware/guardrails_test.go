package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ilter-ai/ilter/internal/config"
	dbpkg "github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/model"

	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

// setupGuardrailsTestStore creates an in-memory SQLiteStore for guardrails tests.
func setupGuardrailsTestStore(t *testing.T) *dbpkg.SQLiteStore {
	t.Helper()
	store, err := dbpkg.NewSQLiteStore(config.StorageConfig{
		Type:       "sqlite",
		SqlitePath: ":memory:",
	})
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// newTestGuardrailsCache creates a ConfigCache with a boot config suitable
// for guardrails tests (enabled, block mode, no DB-sourced rules).
func newTestGuardrailsCache(t *testing.T) *config.Cache {
	t.Helper()
	boot := config.DefaultBootConfig()
	boot.Guardrails.Enabled = true
	boot.Guardrails.Mode = "block"
	return config.NewConfigCache(&boot)
}

// guardrailsTestHandler is a simple next handler that returns 200 OK.
var guardrailsTestHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

// makeGuardrailsRequest creates a POST /v1/chat/completions request with the given
// body and context values set for user/group targeting.
func makeGuardrailsRequest(t *testing.T, body string, userID *int, groupIDs []int) *http.Request {
	t.Helper()
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")

	ctx := req.Context()
	if userID != nil {
		ctx = context.WithValue(ctx, reqmeta.UserIDContextKey, *userID)
	}
	if groupIDs != nil {
		ctx = context.WithValue(ctx, reqmeta.GroupIDsContextKey, groupIDs)
	}
	return req.WithContext(ctx)
}

// insertGuardrailRule inserts a guardrail rule with the given parameters for testing.
func insertGuardrailRule(t *testing.T, store *dbpkg.SQLiteStore, id, pattern, mode, severity, targetType string, targetID *int) {
	t.Helper()
	patternsJSON, _ := json.Marshal([]string{pattern})
	_, err := store.DB.Exec(
		`INSERT INTO guardrail_rules (id, name, description, patterns, mode, severity, target_type, target_id)
		 VALUES (?, ?, '', ?, ?, ?, ?, ?)`,
		id, "test-"+id, string(patternsJSON), mode, severity, targetType, targetID,
	)
	if err != nil {
		t.Fatalf("failed to insert guardrail rule %s: %v", id, err)
	}
}

// TestGuardrailsTarget_CreateRuleWithTarget verifies that a rule can be created with
// target_type='user' and a specific target_id, and that the middleware loads it correctly.
func TestGuardrailsTarget_CreateRuleWithTarget(t *testing.T) {
	store := setupGuardrailsTestStore(t)

	// Create a rule with user targeting.
	user3 := 3
	insertGuardrailRule(t, store, "rule-user-3", "secret-user-data", "block", "high", "user", &user3)

	// Load rules into middleware.
	mw, err := NewGuardrailsMiddleware(newTestGuardrailsCache(t), nil)
	if err != nil {
		t.Fatalf("failed to create guardrails middleware: %v", err)
	}
	mw.LoadDBRules(store)

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"secret-user-data found"}]}`
	req := makeGuardrailsRequest(t, body, &user3, nil)
	rr := httptest.NewRecorder()
	mw.Handler(guardrailsTestHandler).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 for user 3 matching rule, got %d. Body: %s", rr.Code, rr.Body.String())
	}
}

// TestGuardrailsTarget_MiddlewareUserScope verifies that a user-specific rule
// is applied only when the request context contains the matching user ID.
func TestGuardrailsTarget_MiddlewareUserScope(t *testing.T) {
	store := setupGuardrailsTestStore(t)

	// Create a user-specific rule for user 1.
	user1 := 1
	insertGuardrailRule(t, store, "rule-for-user1", "block-if-seen", "block", "high", "user", &user1)

	mw, err := NewGuardrailsMiddleware(newTestGuardrailsCache(t), nil)
	if err != nil {
		t.Fatalf("failed to create guardrails middleware: %v", err)
	}
	mw.LoadDBRules(store)

	// Test: request with user_id=1 should be blocked.
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"contains block-if-seen"}]}`
	req := makeGuardrailsRequest(t, body, &user1, nil)
	rr := httptest.NewRecorder()
	mw.Handler(guardrailsTestHandler).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 for matching user rule, got %d. Body: %s", rr.Code, rr.Body.String())
	}

	// Test: request with user_id=2 should NOT be blocked (different user).
	user2 := 2
	req2 := makeGuardrailsRequest(t, body, &user2, nil)
	rr2 := httptest.NewRecorder()
	mw.Handler(guardrailsTestHandler).ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Errorf("expected 200 for non-matching user, got %d. Body: %s", rr2.Code, rr2.Body.String())
	}

	// Test: request with NO user_id should NOT be blocked (no user context).
	req3 := makeGuardrailsRequest(t, body, nil, nil)
	rr3 := httptest.NewRecorder()
	mw.Handler(guardrailsTestHandler).ServeHTTP(rr3, req3)

	if rr3.Code != http.StatusOK {
		t.Errorf("expected 200 for no user context, got %d", rr3.Code)
	}
}

// TestGuardrailsTarget_MiddlewareGroupScope verifies that a group-specific rule
// is applied only when the request context contains a matching group ID.
func TestGuardrailsTarget_MiddlewareGroupScope(t *testing.T) {
	store := setupGuardrailsTestStore(t)

	// Create a group-specific rule for group 10.
	group10 := 10
	insertGuardrailRule(t, store, "rule-for-group10", "group-only", "block", "medium", "group", &group10)

	mw, err := NewGuardrailsMiddleware(newTestGuardrailsCache(t), nil)
	if err != nil {
		t.Fatalf("failed to create guardrails middleware: %v", err)
	}
	mw.LoadDBRules(store)

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"contains group-only text"}]}`

	// Test: request with group_ids=[10] should be blocked.
	req := makeGuardrailsRequest(t, body, nil, []int{10})
	rr := httptest.NewRecorder()
	mw.Handler(guardrailsTestHandler).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 for matching group rule, got %d. Body: %s", rr.Code, rr.Body.String())
	}

	// Test: request with group_ids=[20] should NOT be blocked (different group).
	req2 := makeGuardrailsRequest(t, body, nil, []int{20})
	rr2 := httptest.NewRecorder()
	mw.Handler(guardrailsTestHandler).ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Errorf("expected 200 for non-matching group, got %d", rr2.Code)
	}

	// Test: request with NO group_ids should NOT be blocked.
	req3 := makeGuardrailsRequest(t, body, nil, nil)
	rr3 := httptest.NewRecorder()
	mw.Handler(guardrailsTestHandler).ServeHTTP(rr3, req3)

	if rr3.Code != http.StatusOK {
		t.Errorf("expected 200 for no group context, got %d", rr3.Code)
	}
}

// TestGuardrailsTarget_GlobalFallback verifies that global rules always apply
// regardless of user/group context, even when user/group-specific rules also exist.
func TestGuardrailsTarget_GlobalFallback(t *testing.T) {
	store := setupGuardrailsTestStore(t)

	// Create a global rule (target_type='global').
	insertGuardrailRule(t, store, "global-rule", "global-block", "block", "high", "global", nil)

	// Create a user-specific rule for user 5.
	user5 := 5
	insertGuardrailRule(t, store, "user5-rule", "user5-only", "warn", "low", "user", &user5)

	mw, err := NewGuardrailsMiddleware(newTestGuardrailsCache(t), nil)
	if err != nil {
		t.Fatalf("failed to create guardrails middleware: %v", err)
	}
	mw.LoadDBRules(store)

	// Test 1: Global rule blocks ALL requests regardless of user/group context.
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"contains global-block text"}]}`

	// Request with no user context — global rule should still block.
	req := makeGuardrailsRequest(t, body, nil, nil)
	rr := httptest.NewRecorder()
	mw.Handler(guardrailsTestHandler).ServeHTTP(rr, req)
	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 from global rule (no context), got %d. Body: %s", rr.Code, rr.Body.String())
	}

	// Request with user context — global rule should still block.
	user1 := 1
	req2 := makeGuardrailsRequest(t, body, &user1, nil)
	rr2 := httptest.NewRecorder()
	mw.Handler(guardrailsTestHandler).ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 from global rule (with user context), got %d", rr2.Code)
	}

	// Test 2: Warn-type user-specific rule is applied only for that user.
	warnBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"contains user5-only text"}]}`

	// User 5 should get a warning header.
	req3 := makeGuardrailsRequest(t, warnBody, &user5, nil)
	rr3 := httptest.NewRecorder()
	mw.Handler(guardrailsTestHandler).ServeHTTP(rr3, req3)
	warnHeader3 := rr3.Result().Header.Get("X-Guardrails-Warning")
	if warnHeader3 != "user5-rule" {
		t.Errorf("expected warning header 'user5-rule' for user 5, got %q. Code: %d", warnHeader3, rr3.Code)
	}

	// Other users should NOT get the warning.
	user2 := 2
	req4 := makeGuardrailsRequest(t, warnBody, &user2, nil)
	rr4 := httptest.NewRecorder()
	mw.Handler(guardrailsTestHandler).ServeHTTP(rr4, req4)
	warnHeader4 := rr4.Result().Header.Get("X-Guardrails-Warning")
	if warnHeader4 != "" {
		t.Errorf("expected no warning header for user 2, got %q", warnHeader4)
	}
	if rr4.Code != http.StatusOK {
		t.Errorf("expected 200 for non-matching user, got %d", rr4.Code)
	}
}

// TestGuardrailsCache_RulesFromCache verifies that guardrail rules sourced
// from the ConfigCache (via the runtime_config pathway) are applied correctly
// in middleware execution.
func TestGuardrailsCache_RulesFromCache(t *testing.T) {
	sto := setupGuardrailsTestStore(t)
	db := sto.DB

	// runtime_config table is created by the store package migrations.
	if err := dbpkg.ApplyMigrations(db); err != nil {
		t.Fatalf("failed to apply store migrations: %v", err)
	}

	// Insert a guardrail rule into the runtime_config table via SQLiteStore.
	rule := model.GuardrailRule{
		Name:     "cache-block-test",
		Type:     model.GuardrailTypePromptInjection,
		Pattern:  `block-if-seen-in-cache`,
		Action:   model.GuardrailActionBlock,
		Priority: 100,
		Enabled:  true,
		Severity: model.GuardrailSeverityHigh,
	}
	ruleData, err := json.Marshal(rule)
	if err != nil {
		t.Fatalf("failed to marshal guardrail rule: %v", err)
	}
	if err = sto.UpsertRuntimeConfig("guardrail_rule", rule.Name, string(ruleData), "test"); err != nil {
		t.Fatalf("failed to insert guardrail rule: %v", err)
	}

	// Create ConfigCache and populate it via Refresh.
	boot := config.DefaultBootConfig()
	boot.Guardrails.Enabled = true
	boot.Guardrails.Mode = "block"
	cache := config.NewConfigCache(&boot)

	runtimeStores := &config.RuntimeStores{
		RuntimeConfig: sto,
	}
	if err = cache.Refresh(context.Background(), runtimeStores); err != nil {
		t.Fatalf("failed to refresh config cache: %v", err)
	}

	// Create middleware with the populated cache.
	mw, err := NewGuardrailsMiddleware(cache, nil)
	if err != nil {
		t.Fatalf("failed to create guardrails middleware: %v", err)
	}

	// Send a request that matches the pattern - should be blocked.
	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"this contains block-if-seen-in-cache text"}]}`
	req := makeGuardrailsRequest(t, body, nil, nil)
	rr := httptest.NewRecorder()
	mw.Handler(guardrailsTestHandler).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 from cache-sourced rule, got %d. Body: %s", rr.Code, rr.Body.String())
	}

	// Send a request that does NOT match the pattern - should pass through.
	cleanBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"harmless message"}]}`
	req2 := makeGuardrailsRequest(t, cleanBody, nil, nil)
	rr2 := httptest.NewRecorder()
	mw.Handler(guardrailsTestHandler).ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Errorf("expected 200 for non-matching message, got %d", rr2.Code)
	}
}

// TestGuardrailsTarget_RequestBodyRead verifies that the request body is still
// readable after guardrails middleware processes it (body must be replaced).
func TestGuardrailsTarget_RequestBodyRead(t *testing.T) {
	store := setupGuardrailsTestStore(t)

	mw, err := NewGuardrailsMiddleware(newTestGuardrailsCache(t), nil)
	if err != nil {
		t.Fatalf("failed to create guardrails middleware: %v", err)
	}
	mw.LoadDBRules(store)

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`
	req := makeGuardrailsRequest(t, body, nil, nil)

	var bodyRead bool
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err == nil && len(b) > 0 {
			bodyRead = true
		}
		w.WriteHeader(http.StatusOK)
	})

	rr := httptest.NewRecorder()
	mw.Handler(nextHandler).ServeHTTP(rr, req)

	if !bodyRead {
		t.Error("expected downstream handler to be able to read request body")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}
