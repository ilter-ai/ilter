package triggers

import (
	"context"
	"net/http"
	"time"
)

// TriggerKind defines the type of trigger.
type TriggerKind string

const (
	TriggerKindCron    TriggerKind = "cron"
	TriggerKindHTTP    TriggerKind = "http"
	TriggerKindWebhook TriggerKind = "webhook"
	// TriggerKindEvent  TriggerKind = "event"  // future
)

// TriggerConfig holds kind-specific configuration as JSON.
// Cron: {"expr":"0 */6 * * *","timezone":"UTC"}
// HTTP: {"auth":"api_key"}  // goes through AuthMiddleware
// Webhook: {"provider":"generic"}  // HMAC-SHA256
type TriggerConfig struct {
	Expr     string `json:"expr,omitempty"`     // cron expression
	Timezone string `json:"timezone,omitempty"` // cron timezone
	Auth     string `json:"auth,omitempty"`     // http auth type
	Provider string `json:"provider,omitempty"` // webhook provider
}

// Trigger is the base interface for all triggers.
type Trigger interface {
	ID() string
	JobID() string
	Kind() TriggerKind
	Enabled() bool
	Config() TriggerConfig
}

// ActiveTrigger represents triggers that self-activate (cron, event subscription).
// Run starts the trigger's goroutine; it should block until ctx is canceled.
type ActiveTrigger interface {
	Trigger
	Run(ctx context.Context, fireFunc FireFunc) error
}

// FireFunc is called when a trigger fires to create an activation.
type FireFunc func(ctx context.Context, activation Activation) error

// PassiveTrigger represents triggers activated by external requests (HTTP, webhook).
type PassiveTrigger interface {
	Trigger
	// Verify checks the request's authenticity (HMAC, token, etc.).
	Verify(r *http.Request, rawBody []byte) error
	// Activation creates an Activation from the verified request.
	Activation(r *http.Request, rawBody []byte) (Activation, error)
}

// Activation represents a single trigger firing.
type Activation struct {
	TriggerID      string
	JobID          string
	IdempotencyKey string // UNIQUE(trigger_id, idem_key) for dedup
	Payload        []byte
	ReceivedAt     time.Time
}

// TriggerRow is the DB representation matching the triggers table.
type TriggerRow struct {
	ID      string        `json:"id"`
	JobID   string        `json:"job_id"`
	Kind    TriggerKind   `json:"kind"`
	Enabled bool          `json:"enabled"`
	Config  TriggerConfig `json:"config"`
	Token   string        `json:"-"` // plaintext wht_ token (only shown on create)
	// Secret is the plaintext HMAC-SHA256 signing key for webhook triggers
	// (generic/github/gitlab), persisted in the secret_hash column. Despite
	// the column's name it holds live key material, not a one-way hash —
	// HMAC verification requires the raw secret server-side. Only shown on
	// create, same as Token.
	Secret     string     `json:"-"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}
