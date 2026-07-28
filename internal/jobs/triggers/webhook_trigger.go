package triggers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ilter-ai/ilter/internal/config"
)

// WebhookTrigger validates incoming webhook HTTP requests via HMAC signatures
// and creates Activations for verified requests.
//
// Supported providers:
//   - github:    HMAC-SHA256 of raw body, compared against X-Hub-Signature-256
//   - gitlab:    constant-time compare of X-Gitlab-Token against stored token
//   - generic:   HMAC-SHA256 of raw body, compared against X-Signature-256
type WebhookTrigger struct {
	store  *Store
	cfg    config.JobsConfig
	logger *slog.Logger
}

// NewWebhookTrigger creates a WebhookTrigger with the given store, config, and logger.
func NewWebhookTrigger(store *Store, cfg config.JobsConfig, log *slog.Logger) *WebhookTrigger {
	return &WebhookTrigger{
		store:  store,
		cfg:    cfg,
		logger: log,
	}
}

// VerifyAndActivate looks up the trigger by its webhook token, verifies the
// request authenticity (HMAC/token) according to the trigger's provider type,
// and returns an Activation on success along with an HTTP status code.
//
// The token parameter is the opaque wht_ token from the URL path, used to
// look up the trigger in the database.
func (w *WebhookTrigger) VerifyAndActivate(_ context.Context, token string, r *http.Request, rawBody []byte) (*Activation, int, error) {
	if token == "" {
		return nil, http.StatusNotFound, errors.New("trigger not found")
	}

	trigger, err := w.store.GetByTokenHash(token)
	if err != nil {
		return nil, http.StatusInternalServerError, fmt.Errorf("lookup trigger: %w", err)
	}
	if trigger == nil {
		return nil, http.StatusNotFound, errors.New("trigger not found")
	}
	if trigger.Kind != TriggerKindWebhook {
		return nil, http.StatusNotFound, errors.New("trigger not found")
	}
	if !trigger.Enabled {
		return nil, http.StatusNotFound, errors.New("trigger not found")
	}

	provider := trigger.Config.Provider
	if provider == "" {
		return nil, http.StatusBadRequest, errors.New("webhook provider not configured")
	}

	// The HMAC secret is generated independently of the token and shown to
	// the caller once at creation (see generateWebhookSecret in the jobs
	// dashboard handler). Older triggers created before that existed fall
	// back to the token itself so they don't break.
	secret := trigger.Secret
	if secret == "" {
		secret = trigger.Token
	}

	deliveryID, err := verifyProvider(provider, r, rawBody, secret, trigger.Token)
	if err != nil {
		// Map verification errors to appropriate HTTP status.
		if errors.Is(err, errSignature) {
			return nil, http.StatusUnauthorized, err
		}
		return nil, http.StatusBadRequest, err
	}

	// Build idempotency key from provider-specific delivery headers.
	idemKey := buildIdemKey(provider, trigger.ID, r, deliveryID)

	activation := &Activation{
		TriggerID:      trigger.ID,
		JobID:          trigger.JobID,
		Payload:        rawBody,
		IdempotencyKey: idemKey,
		ReceivedAt:     time.Now(),
	}

	return activation, http.StatusAccepted, nil
}

// Sentinel error for HMAC/signature failures — used to route to 401.
var errSignature = errors.New("invalid signature")

// verifyProvider dispatches to the correct verification logic based on provider type.
// It returns the delivery identifier (for idempotency key construction) and any
// verification error.
func verifyProvider(provider string, r *http.Request, rawBody []byte, secret string, token string) (string, error) {
	switch provider {
	case "github":
		return verifyGitHub(r, rawBody, secret)
	case "gitlab":
		return verifyGitLab(r, token, secret)
	case "generic":
		return verifyGeneric(r, rawBody, secret)
	default:
		return "", fmt.Errorf("unsupported webhook provider: %s", provider)
	}
}

// verifyGitHub verifies a GitHub webhook using HMAC-SHA256.
// Expects X-Hub-Signature-256 header in format "sha256=<hex>".
func verifyGitHub(r *http.Request, rawBody []byte, secret string) (string, error) {
	sig := r.Header.Get("X-Hub-Signature-256")
	if sig == "" {
		return "", fmt.Errorf("missing X-Hub-Signature-256 header: %w", errSignature)
	}
	if !strings.HasPrefix(sig, "sha256=") {
		return "", errSignature
	}
	sigBytes, err := hex.DecodeString(sig[len("sha256="):])
	if err != nil {
		return "", errSignature
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(rawBody)
	expected := mac.Sum(nil)

	if !hmac.Equal(sigBytes, expected) {
		return "", errSignature
	}
	return r.Header.Get("X-GitHub-Delivery"), nil
}

// verifyGitLab verifies a GitLab webhook using constant-time token comparison.
// Compares X-Gitlab-Token header against the secret_hash first (the shared
// secret configured in GitLab), falling back to the trigger's URL token.
func verifyGitLab(r *http.Request, token string, secret string) (string, error) {
	tokenHeader := r.Header.Get("X-Gitlab-Token")
	if tokenHeader == "" {
		return "", fmt.Errorf("missing X-Gitlab-Token header: %w", errSignature)
	}

	// Primary: compare against the secret_hash column (the shared GitLab token).
	// Fallback: compare against the trigger's token column (the URL wht_ token).
	compareToken := secret
	if compareToken == "" {
		compareToken = token
	}

	if subtle.ConstantTimeCompare([]byte(tokenHeader), []byte(compareToken)) != 1 {
		return "", errSignature
	}
	return r.Header.Get("X-Gitlab-Event-UUID"), nil
}

// verifyGeneric verifies a generic webhook using HMAC-SHA256.
// Expects X-Signature-256 header with hex-encoded HMAC-SHA256 digest.
func verifyGeneric(r *http.Request, rawBody []byte, secret string) (string, error) {
	sig := r.Header.Get("X-Signature-256")
	if sig == "" {
		return "", fmt.Errorf("missing X-Signature-256 header: %w", errSignature)
	}
	sigBytes, err := hex.DecodeString(sig)
	if err != nil {
		return "", errSignature
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(rawBody)
	expected := mac.Sum(nil)

	if !hmac.Equal(sigBytes, expected) {
		return "", errSignature
	}
	return "", nil
}

// buildIdemKey constructs an idempotency key from provider-specific headers.
//
//	github:  triggerID:deliveryID (X-GitHub-Delivery)
//	gitlab:  triggerID:deliveryID (X-Gitlab-Event-UUID)
//	generic: triggerID:idemKeyHeader (Idempotency-Key) or fallback to timestamp
func buildIdemKey(provider string, triggerID string, r *http.Request, deliveryID string) string {
	switch provider {
	case "github", "gitlab":
		if deliveryID != "" {
			return fmt.Sprintf("%s:%s", triggerID, deliveryID)
		}
	case "generic":
		if key := r.Header.Get("Idempotency-Key"); key != "" {
			return fmt.Sprintf("%s:%s", triggerID, key)
		}
	}
	return fmt.Sprintf("%s:%d", triggerID, time.Now().Unix())
}
