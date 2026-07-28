package triggers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/ilter-ai/ilter/internal/config"
)

// testTB is a minimal interface satisfied by both *testing.T and *testing.B
// for use in shared test helpers.
type testTB interface {
	Helper()
	Fatalf(format string, args ...any)
}

// setupWebhookTestDB creates an in-memory SQLite database with the triggers
// table and inserts test webhook trigger rows. Returns the Store and a cleanup
// function.
func setupWebhookTestDB(t testTB) (*Store, func()) {
	t.Helper()

	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS triggers (
			id TEXT PRIMARY KEY,
			job_id TEXT NOT NULL,
			kind TEXT NOT NULL CHECK(kind IN ('cron', 'webhook')),
			enabled INTEGER NOT NULL DEFAULT 1,
			config TEXT NOT NULL DEFAULT '{}',
			token TEXT,
			secret_hash TEXT,
			last_used_at DATETIME,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		db.Close()
		t.Fatalf("create triggers table: %v", err)
	}

	store := NewStore(db)
	cleanup := func() { db.Close() }
	return store, cleanup
}

// insertWebhookTrigger inserts a webhook trigger into the store for testing.
func insertWebhookTrigger(t testTB, store *Store, id, jobID, token, secretHash, provider string, enabled bool) {
	t.Helper()

	cfg := TriggerConfig{Provider: provider}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	enabledInt := 0
	if enabled {
		enabledInt = 1
	}

	_, err = store.DB().Exec(
		`INSERT INTO triggers (id, job_id, kind, enabled, config, token, secret_hash, created_at, updated_at)
		 VALUES (?, ?, 'webhook', ?, ?, ?, ?, datetime('now'), datetime('now'))`,
		id, jobID, enabledInt, string(cfgJSON), strPtrOrNil(token), strPtrOrNil(secretHash),
	)
	if err != nil {
		t.Fatalf("insert trigger %s: %v", id, err)
	}
}

// hmacSHA256Hex computes HMAC-SHA256 of data using the given secret and returns
// the hex-encoded digest.
func hmacSHA256Hex(secret string, data []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyAndActivate_GitHub_Success(t *testing.T) {
	store, cleanup := setupWebhookTestDB(t)
	defer cleanup()

	secret := "super-secret-key"
	insertWebhookTrigger(t, store, "trig-1", "job-1", "wht_test_token_github", secret, "github", true)

	wt := NewWebhookTrigger(store, config.JobsConfig{}, slog.Default())
	ctx := context.Background()

	rawBody := []byte(`{"action":"push","ref":"refs/heads/main"}`)
	sig := "sha256=" + hmacSHA256Hex(secret, rawBody)
	deliveryID := "delivery-uuid-123"

	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/api/webhooks/wht_test_token_github", strings.NewReader(string(rawBody)))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("X-GitHub-Delivery", deliveryID)

	activation, status, err := wt.VerifyAndActivate(ctx, "wht_test_token_github", req, rawBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusAccepted {
		t.Errorf("expected status %d, got %d", http.StatusAccepted, status)
	}
	if activation.TriggerID != "trig-1" {
		t.Errorf("expected TriggerID trig-1, got %s", activation.TriggerID)
	}
	if activation.JobID != "job-1" {
		t.Errorf("expected JobID job-1, got %s", activation.JobID)
	}
	if string(activation.Payload) != string(rawBody) {
		t.Errorf("payload mismatch")
	}
	expectedIdemKey := fmt.Sprintf("trig-1:%s", deliveryID)
	if activation.IdempotencyKey != expectedIdemKey {
		t.Errorf("expected idempotency key %q, got %q", expectedIdemKey, activation.IdempotencyKey)
	}
	if activation.ReceivedAt.IsZero() {
		t.Error("expected ReceivedAt to be set")
	}
}

func TestVerifyAndActivate_GitHub_InvalidSignature(t *testing.T) {
	store, cleanup := setupWebhookTestDB(t)
	defer cleanup()

	insertWebhookTrigger(t, store, "trig-2", "job-2", "wht_test_token_github_bad", "secret", "github", true)

	wt := NewWebhookTrigger(store, config.JobsConfig{}, slog.Default())
	ctx := context.Background()

	rawBody := []byte(`{"action":"push"}`)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/api/webhooks/wht_test_token_github_bad", strings.NewReader(string(rawBody)))
	req.Header.Set("X-Hub-Signature-256", "sha256=0000000000000000000000000000000000000000000000000000000000000000")

	_, status, err := wt.VerifyAndActivate(ctx, "wht_test_token_github_bad", req, rawBody)
	if err == nil {
		t.Fatal("expected error for invalid signature")
	}
	if status != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, status)
	}
}

func TestVerifyAndActivate_GitHub_MissingHeader(t *testing.T) {
	store, cleanup := setupWebhookTestDB(t)
	defer cleanup()

	insertWebhookTrigger(t, store, "trig-gh-missing", "job-2", "wht_gh_missing", "secret", "github", true)

	wt := NewWebhookTrigger(store, config.JobsConfig{}, slog.Default())
	ctx := context.Background()

	rawBody := []byte(`{"action":"push"}`)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/api/webhooks/wht_gh_missing", strings.NewReader(string(rawBody)))

	_, status, err := wt.VerifyAndActivate(ctx, "wht_gh_missing", req, rawBody)
	if err == nil {
		t.Fatal("expected error for missing header")
	}
	if status != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, status)
	}
}

func TestVerifyAndActivate_GitHub_BadPrefix(t *testing.T) {
	store, cleanup := setupWebhookTestDB(t)
	defer cleanup()

	insertWebhookTrigger(t, store, "trig-gh-prefix", "job-2", "wht_gh_prefix", "secret", "github", true)

	wt := NewWebhookTrigger(store, config.JobsConfig{}, slog.Default())
	ctx := context.Background()

	rawBody := []byte(`{"action":"push"}`)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/api/webhooks/wht_gh_prefix", strings.NewReader(string(rawBody)))
	req.Header.Set("X-Hub-Signature-256", "md5=abc123")

	_, status, err := wt.VerifyAndActivate(ctx, "wht_gh_prefix", req, rawBody)
	if err == nil {
		t.Fatal("expected error for bad prefix")
	}
	if status != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, status)
	}
}

func TestVerifyAndActivate_GitLab_Success(t *testing.T) {
	store, cleanup := setupWebhookTestDB(t)
	defer cleanup()

	gitlabSecret := "gitlab-shared-secret"
	insertWebhookTrigger(t, store, "trig-3", "job-3", "wht_gitlab_url", gitlabSecret, "gitlab", true)

	wt := NewWebhookTrigger(store, config.JobsConfig{}, slog.Default())
	ctx := context.Background()

	rawBody := []byte(`{"object_kind":"push"}`)
	eventUUID := "gl-event-456"

	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/api/webhooks/wht_gitlab_url", strings.NewReader(string(rawBody)))
	req.Header.Set("X-Gitlab-Token", gitlabSecret)
	req.Header.Set("X-Gitlab-Event-UUID", eventUUID)

	activation, status, err := wt.VerifyAndActivate(ctx, "wht_gitlab_url", req, rawBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusAccepted {
		t.Errorf("expected status %d, got %d", http.StatusAccepted, status)
	}
	if activation.TriggerID != "trig-3" {
		t.Errorf("expected TriggerID trig-3, got %s", activation.TriggerID)
	}
	expectedIdemKey := fmt.Sprintf("trig-3:%s", eventUUID)
	if activation.IdempotencyKey != expectedIdemKey {
		t.Errorf("expected idempotency key %q, got %q", expectedIdemKey, activation.IdempotencyKey)
	}
}

func TestVerifyAndActivate_GitLab_FallbackToToken(t *testing.T) {
	store, cleanup := setupWebhookTestDB(t)
	defer cleanup()

	// Trigger has secret_hash empty but token is set. verifyGitLab should
	// fall back to comparing against the token column.
	insertWebhookTrigger(t, store, "trig-gl-fallback", "job-3", "url-token-value", "", "gitlab", true)

	wt := NewWebhookTrigger(store, config.JobsConfig{}, slog.Default())
	ctx := context.Background()

	rawBody := []byte(`{"object_kind":"push"}`)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/api/webhooks/url-token-value", strings.NewReader(string(rawBody)))
	req.Header.Set("X-Gitlab-Token", "url-token-value")
	req.Header.Set("X-Gitlab-Event-UUID", "gl-uuid-789")

	activation, status, err := wt.VerifyAndActivate(ctx, "url-token-value", req, rawBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusAccepted {
		t.Errorf("expected status %d, got %d", http.StatusAccepted, status)
	}
	if activation.TriggerID != "trig-gl-fallback" {
		t.Errorf("expected TriggerID trig-gl-fallback, got %s", activation.TriggerID)
	}
}

func TestVerifyAndActivate_GitLab_CompareSecretHash(t *testing.T) {
	store, cleanup := setupWebhookTestDB(t)
	defer cleanup()

	// Both token and secret_hash set — primary comparison uses secret_hash.
	insertWebhookTrigger(t, store, "trig-gl-secret", "job-3a", "url-token", "shared-secret-value", "gitlab", true)

	wt := NewWebhookTrigger(store, config.JobsConfig{}, slog.Default())
	ctx := context.Background()

	rawBody := []byte(`{"object_kind":"push"}`)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/api/webhooks/url-token", strings.NewReader(string(rawBody)))
	req.Header.Set("X-Gitlab-Token", "shared-secret-value")
	req.Header.Set("X-Gitlab-Event-UUID", "gl-uuid-secret")

	activation, status, err := wt.VerifyAndActivate(ctx, "url-token", req, rawBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusAccepted {
		t.Errorf("expected status %d, got %d", http.StatusAccepted, status)
	}
	if activation.TriggerID != "trig-gl-secret" {
		t.Errorf("expected TriggerID trig-gl-secret, got %s", activation.TriggerID)
	}
}

func TestVerifyAndActivate_GitLab_WrongToken(t *testing.T) {
	store, cleanup := setupWebhookTestDB(t)
	defer cleanup()

	insertWebhookTrigger(t, store, "trig-4", "job-4", "wht_gitlab_url", "correct-secret", "gitlab", true)

	wt := NewWebhookTrigger(store, config.JobsConfig{}, slog.Default())
	ctx := context.Background()

	rawBody := []byte(`{}`)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/api/webhooks/wht_gitlab_url", strings.NewReader(string(rawBody)))
	req.Header.Set("X-Gitlab-Token", "wrong-token")

	_, status, err := wt.VerifyAndActivate(ctx, "wht_gitlab_url", req, rawBody)
	if err == nil {
		t.Fatal("expected error for wrong token")
	}
	if status != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, status)
	}
}

func TestVerifyAndActivate_Generic_Success(t *testing.T) {
	store, cleanup := setupWebhookTestDB(t)
	defer cleanup()

	secret := "generic-hmac-secret"
	insertWebhookTrigger(t, store, "trig-5", "job-5", "wht_generic", secret, "generic", true)

	wt := NewWebhookTrigger(store, config.JobsConfig{}, slog.Default())
	ctx := context.Background()

	rawBody := []byte(`{"event":"deploy","status":"ok"}`)
	sig := hmacSHA256Hex(secret, rawBody)
	idemKey := "my-custom-idem-001"

	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/api/webhooks/wht_generic", strings.NewReader(string(rawBody)))
	req.Header.Set("X-Signature-256", sig)
	req.Header.Set("Idempotency-Key", idemKey)

	activation, status, err := wt.VerifyAndActivate(ctx, "wht_generic", req, rawBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusAccepted {
		t.Errorf("expected status %d, got %d", http.StatusAccepted, status)
	}
	if activation.TriggerID != "trig-5" {
		t.Errorf("expected TriggerID trig-5, got %s", activation.TriggerID)
	}
	expectedIdemKey := fmt.Sprintf("trig-5:%s", idemKey)
	if activation.IdempotencyKey != expectedIdemKey {
		t.Errorf("expected idempotency key %q, got %q", expectedIdemKey, activation.IdempotencyKey)
	}
}

func TestVerifyAndActivate_Generic_NoIdemKey(t *testing.T) {
	store, cleanup := setupWebhookTestDB(t)
	defer cleanup()

	secret := "generic-secret"
	insertWebhookTrigger(t, store, "trig-6", "job-6", "wht_generic_no_idem", secret, "generic", true)

	wt := NewWebhookTrigger(store, config.JobsConfig{}, slog.Default())
	ctx := context.Background()

	rawBody := []byte(`{"event":"test"}`)
	sig := hmacSHA256Hex(secret, rawBody)

	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/api/webhooks/wht_generic_no_idem", strings.NewReader(string(rawBody)))
	req.Header.Set("X-Signature-256", sig)

	activation, status, err := wt.VerifyAndActivate(ctx, "wht_generic_no_idem", req, rawBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusAccepted {
		t.Errorf("expected status %d, got %d", http.StatusAccepted, status)
	}
	// Without Idempotency-Key header, the key should be triggerID:timestamp
	if activation.IdempotencyKey == "" {
		t.Fatal("expected non-empty idempotency key")
	}
	if !strings.HasPrefix(activation.IdempotencyKey, "trig-6:") {
		t.Errorf("expected idempotency key to start with 'trig-6:', got %q", activation.IdempotencyKey)
	}
}

func TestVerifyAndActivate_Generic_InvalidSignature(t *testing.T) {
	store, cleanup := setupWebhookTestDB(t)
	defer cleanup()

	insertWebhookTrigger(t, store, "trig-7", "job-7", "wht_generic_bad", "secret", "generic", true)

	wt := NewWebhookTrigger(store, config.JobsConfig{}, slog.Default())
	ctx := context.Background()

	rawBody := []byte(`{}`)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/api/webhooks/wht_generic_bad", strings.NewReader(string(rawBody)))
	req.Header.Set("X-Signature-256", "deadbeef")

	_, status, err := wt.VerifyAndActivate(ctx, "wht_generic_bad", req, rawBody)
	if err == nil {
		t.Fatal("expected error for bad signature")
	}
	if status != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, status)
	}
}

func TestVerifyAndActivate_TriggerNotFound(t *testing.T) {
	store, cleanup := setupWebhookTestDB(t)
	defer cleanup()

	wt := NewWebhookTrigger(store, config.JobsConfig{}, slog.Default())
	ctx := context.Background()

	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/api/webhooks/nonexistent", nil)
	_, status, err := wt.VerifyAndActivate(ctx, "nonexistent-token", req, nil)
	if err == nil {
		t.Fatal("expected error for non-existent trigger")
	}
	if status != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, status)
	}
}

func TestVerifyAndActivate_EmptyToken(t *testing.T) {
	store, cleanup := setupWebhookTestDB(t)
	defer cleanup()

	wt := NewWebhookTrigger(store, config.JobsConfig{}, slog.Default())
	ctx := context.Background()

	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/api/webhooks/", nil)
	_, status, err := wt.VerifyAndActivate(ctx, "", req, nil)
	if err == nil {
		t.Fatal("expected error for empty token")
	}
	if status != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, status)
	}
}

func TestVerifyAndActivate_DisabledTrigger(t *testing.T) {
	store, cleanup := setupWebhookTestDB(t)
	defer cleanup()

	insertWebhookTrigger(t, store, "trig-disabled", "job-8", "wht_disabled", "secret", "github", false)

	wt := NewWebhookTrigger(store, config.JobsConfig{}, slog.Default())
	ctx := context.Background()

	rawBody := []byte(`{}`)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/api/webhooks/wht_disabled", strings.NewReader(string(rawBody)))

	_, status, err := wt.VerifyAndActivate(ctx, "wht_disabled", req, rawBody)
	if err == nil {
		t.Fatal("expected error for disabled trigger")
	}
	if status != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, status)
	}
}

func TestVerifyAndActivate_UnsupportedProvider(t *testing.T) {
	store, cleanup := setupWebhookTestDB(t)
	defer cleanup()

	insertWebhookTrigger(t, store, "trig-bad-prov", "job-9", "wht_bad_prov", "secret", "bitbucket", true)

	wt := NewWebhookTrigger(store, config.JobsConfig{}, slog.Default())
	ctx := context.Background()

	rawBody := []byte(`{}`)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/api/webhooks/wht_bad_prov", strings.NewReader(string(rawBody)))

	_, status, err := wt.VerifyAndActivate(ctx, "wht_bad_prov", req, rawBody)
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
	if status != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, status)
	}
}

func TestVerifyAndActivate_EmptyProvider(t *testing.T) {
	store, cleanup := setupWebhookTestDB(t)
	defer cleanup()

	insertWebhookTrigger(t, store, "trig-no-prov", "job-10", "wht_no_prov", "secret", "", true)

	wt := NewWebhookTrigger(store, config.JobsConfig{}, slog.Default())
	ctx := context.Background()

	rawBody := []byte(`{}`)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/api/webhooks/wht_no_prov", strings.NewReader(string(rawBody)))

	_, status, err := wt.VerifyAndActivate(ctx, "wht_no_prov", req, rawBody)
	if err == nil {
		t.Fatal("expected error for empty provider")
	}
	if status != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, status)
	}
}

func TestVerifyAndActivate_GitHub_EmptyBody(t *testing.T) {
	store, cleanup := setupWebhookTestDB(t)
	defer cleanup()

	secret := "test-secret"
	insertWebhookTrigger(t, store, "trig-empty-body", "job-11", "wht_empty_body", secret, "github", true)

	wt := NewWebhookTrigger(store, config.JobsConfig{}, slog.Default())
	ctx := context.Background()

	rawBody := []byte{}
	sig := "sha256=" + hmacSHA256Hex(secret, rawBody)

	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/api/webhooks/wht_empty_body", nil)
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("X-GitHub-Delivery", "delivery-empty")

	activation, status, err := wt.VerifyAndActivate(ctx, "wht_empty_body", req, rawBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusAccepted {
		t.Errorf("expected status %d, got %d", http.StatusAccepted, status)
	}
	if len(activation.Payload) != 0 {
		t.Errorf("expected empty payload, got %d bytes", len(activation.Payload))
	}
}

func TestNewWebhookTrigger(t *testing.T) {
	store, cleanup := setupWebhookTestDB(t)
	defer cleanup()

	wt := NewWebhookTrigger(store, config.JobsConfig{Enabled: true}, slog.Default())
	if wt == nil {
		t.Fatal("expected non-nil WebhookTrigger")
	}
	if wt.store != store {
		t.Error("store not set")
	}
	if wt.logger == nil {
		t.Error("logger not set")
	}
}

// TestVerifyAndActivate_GitLab_MissingTokenHeader ensures a missing
// X-Gitlab-Token header returns 401.
func TestVerifyAndActivate_GitLab_MissingTokenHeader(t *testing.T) {
	store, cleanup := setupWebhookTestDB(t)
	defer cleanup()

	insertWebhookTrigger(t, store, "trig-gl-missing-hdr", "job-12", "gl-token", "secret", "gitlab", true)

	wt := NewWebhookTrigger(store, config.JobsConfig{}, slog.Default())
	ctx := context.Background()

	rawBody := []byte(`{}`)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/api/webhooks/gl-token", strings.NewReader(string(rawBody)))

	_, status, err := wt.VerifyAndActivate(ctx, "gl-token", req, rawBody)
	if err == nil {
		t.Fatal("expected error for missing X-Gitlab-Token header")
	}
	if status != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, status)
	}
}

// TestVerifyAndActivate_Generic_MissingSignatureHeader ensures a missing
// X-Signature-256 header returns 401.
func TestVerifyAndActivate_Generic_MissingSignatureHeader(t *testing.T) {
	store, cleanup := setupWebhookTestDB(t)
	defer cleanup()

	insertWebhookTrigger(t, store, "trig-gen-missing-sig", "job-13", "wht_gen_missing_sig", "secret", "generic", true)

	wt := NewWebhookTrigger(store, config.JobsConfig{}, slog.Default())
	ctx := context.Background()

	rawBody := []byte(`{}`)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/api/webhooks/wht_gen_missing_sig", strings.NewReader(string(rawBody)))

	_, status, err := wt.VerifyAndActivate(ctx, "wht_gen_missing_sig", req, rawBody)
	if err == nil {
		t.Fatal("expected error for missing X-Signature-256 header")
	}
	if status != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, status)
	}
}

// TestVerifyAndActivate_GitHub_DeliveryHeader_Missing ensures activation still
// succeeds even when X-GitHub-Delivery is missing (idem key falls back to timestamp).
func TestVerifyAndActivate_GitHub_DeliveryHeader_Missing(t *testing.T) {
	store, cleanup := setupWebhookTestDB(t)
	defer cleanup()

	secret := "secret-key"
	insertWebhookTrigger(t, store, "trig-gh-no-delivery", "job-14", "wht_gh_no_delivery", secret, "github", true)

	wt := NewWebhookTrigger(store, config.JobsConfig{}, slog.Default())
	ctx := context.Background()

	rawBody := []byte(`{"action":"push"}`)
	sig := "sha256=" + hmacSHA256Hex(secret, rawBody)

	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/api/webhooks/wht_gh_no_delivery", strings.NewReader(string(rawBody)))
	req.Header.Set("X-Hub-Signature-256", sig)

	activation, status, err := wt.VerifyAndActivate(ctx, "wht_gh_no_delivery", req, rawBody)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != http.StatusAccepted {
		t.Errorf("expected status %d, got %d", http.StatusAccepted, status)
	}
	if !strings.HasPrefix(activation.IdempotencyKey, "trig-gh-no-delivery:") {
		t.Errorf("expected idempotency key to start with 'trig-gh-no-delivery:', got %q", activation.IdempotencyKey)
	}
}

// TestVerifyAndActivate_Generic_InvalidHexSignature ensures a non-hex signature
// returns 401.
func TestVerifyAndActivate_Generic_InvalidHexSignature(t *testing.T) {
	store, cleanup := setupWebhookTestDB(t)
	defer cleanup()

	insertWebhookTrigger(t, store, "trig-gen-bad-hex", "job-15", "wht_gen_bad_hex", "secret", "generic", true)

	wt := NewWebhookTrigger(store, config.JobsConfig{}, slog.Default())
	ctx := context.Background()

	rawBody := []byte(`{}`)
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/api/webhooks/wht_gen_bad_hex", strings.NewReader(string(rawBody)))
	req.Header.Set("X-Signature-256", "zzz-not-hex!!")

	_, status, err := wt.VerifyAndActivate(ctx, "wht_gen_bad_hex", req, rawBody)
	if err == nil {
		t.Fatal("expected error for invalid hex signature")
	}
	if status != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, status)
	}
}

func TestVerifyAndActivate_GitHub_DisallowedKind(t *testing.T) {
	store, cleanup := setupWebhookTestDB(t)
	defer cleanup()

	// Insert a cron trigger with the same token lookup path.
	cfg := TriggerConfig{Provider: "github"}
	cfgJSON, _ := json.Marshal(cfg)
	_, err := store.DB().Exec(
		`INSERT INTO triggers (id, job_id, kind, enabled, config, token, secret_hash, created_at, updated_at)
		 VALUES (?, ?, 'cron', 1, ?, ?, ?, datetime('now'), datetime('now'))`,
		"trig-cron-kind", "job-16", string(cfgJSON), "wht_cron_kind", "secret",
	)
	if err != nil {
		t.Fatalf("insert cron trigger: %v", err)
	}

	wt := NewWebhookTrigger(store, config.JobsConfig{}, slog.Default())
	ctx := context.Background()

	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/api/webhooks/wht_cron_kind", nil)
	_, status, err := wt.VerifyAndActivate(ctx, "wht_cron_kind", req, nil)
	if err == nil {
		t.Fatal("expected error for non-webhook trigger kind")
	}
	if status != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, status)
	}
}

// BenchmarkVerifyAndActivate_GitHub benchmarks the happy path for GitHub webhook verification.
func BenchmarkVerifyAndActivate_GitHub(b *testing.B) {
	store, cleanup := setupWebhookTestDB(b)
	defer cleanup()

	secret := "benchmark-secret-key"
	insertWebhookTrigger(b, store, "trig-bench", "job-bench", "wht_bench", secret, "github", true)

	wt := NewWebhookTrigger(store, config.JobsConfig{}, slog.Default())
	ctx := context.Background()

	rawBody := []byte(`{"action":"push","ref":"refs/heads/main","repository":{"full_name":"test/repo"}}`)
	sig := "sha256=" + hmacSHA256Hex(secret, rawBody)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequestWithContext(context.Background(), "POST", "/api/webhooks/wht_bench", strings.NewReader(string(rawBody)))
		req.Header.Set("X-Hub-Signature-256", sig)
		req.Header.Set("X-GitHub-Delivery", fmt.Sprintf("delivery-%d", i))

		activation, status, err := wt.VerifyAndActivate(ctx, "wht_bench", req, rawBody)
		if err != nil || status != http.StatusAccepted || activation == nil {
			b.Fatal("benchmark iteration failed")
		}
	}
}
