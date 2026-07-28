package reqmeta

import (
	"context"
	"strings"
	"sync"
)

type contextKey string

const (
	// KeyIDContextKey stores the authenticated key ID (string, e.g. "abc123").
	// Empty string means admin/no key.
	KeyIDContextKey                  contextKey = "key_id"
	APIKeyIDContextKey               contextKey = "api_key_id"
	APIKeyBudgetContextKey           contextKey = "api_key_budget"
	APIKeyDailyLimitContextKey       contextKey = "api_key_daily_limit"
	APIKeyRateLimitContextKey        contextKey = "api_key_rate_limit"
	RequestMetadataContextKey        contextKey = "request_metadata"
	APIKeyAllowedModelsContextKey    contextKey = "api_key_allowed_models"
	APIKeyAllowedProvidersContextKey contextKey = "api_key_allowed_providers"
	APIKeyAuthDoneContextKey         contextKey = "api_key_auth_done"
	UserIDContextKey                 contextKey = "user_id"
	UserBudgetContextKey             contextKey = "user_budget"
	GroupIDsContextKey               contextKey = "group_ids"
	GroupBudgetsContextKey           contextKey = "group_budgets"
)

type RequestPIIEvent struct {
	PIIType             string
	ActionTaken         string
	MaskedPromptPreview string
	PIIValue            string
	ClientIP            string
}

// RequestLoggingMetadata stores diagnostic details about the gateway execution
// path to print a high-fidelity single-line log at the end of the request.
type RequestLoggingMetadata struct {
	mu               sync.Mutex
	CacheHit         *bool
	SmartRouter      *bool
	SmartRoutedTo    string
	PIIMasked        *bool
	PIIBlocked       *bool
	LoopWarning      *bool
	LoopBlocked      *bool
	RateLimited      *bool
	BudgetExceeded   *bool
	KeyID            string
	PromptTokens     int
	CompletionTokens int
	Cost             float64
	ComplexityScore  float64
	PIIEvents        []RequestPIIEvent
}

func (m *RequestLoggingMetadata) AddPIIEvent(piiType, action, preview, value, clientIP string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PIIEvents = append(m.PIIEvents, RequestPIIEvent{
		PIIType:             piiType,
		ActionTaken:         action,
		MaskedPromptPreview: preview,
		PIIValue:            value,
		ClientIP:            clientIP,
	})
}

// InitRequestMetadata creates metadata in context and returns it.
func InitRequestMetadata(ctx context.Context) (context.Context, *RequestLoggingMetadata) {
	meta := &RequestLoggingMetadata{}
	return context.WithValue(ctx, RequestMetadataContextKey, meta), meta
}

func GetRequestMetadata(ctx context.Context) *RequestLoggingMetadata {
	if val := ctx.Value(RequestMetadataContextKey); val != nil {
		if meta, ok := val.(*RequestLoggingMetadata); ok {
			return meta
		}
	}
	return nil
}

// WithLock holds the metadata mutex for atomic multi-field reads.
func (m *RequestLoggingMetadata) WithLock(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn()
}

func (m *RequestLoggingMetadata) SetCacheHit(hit bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CacheHit = &hit
}

func (m *RequestLoggingMetadata) SetSmartRouted(routed bool, model string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SmartRouter = &routed
	m.SmartRoutedTo = model
}

func (m *RequestLoggingMetadata) SetPIIMasked(masked bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PIIMasked = &masked
}

func (m *RequestLoggingMetadata) SetPIIBlocked(blocked bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PIIBlocked = &blocked
}

func (m *RequestLoggingMetadata) SetLoopWarning(warn bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LoopWarning = &warn
}

func (m *RequestLoggingMetadata) SetLoopBlocked(blocked bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LoopBlocked = &blocked
}

func (m *RequestLoggingMetadata) SetRateLimited(limited bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.RateLimited = &limited
}

func (m *RequestLoggingMetadata) SetBudgetExceeded(exceeded bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.BudgetExceeded = &exceeded
}

func (m *RequestLoggingMetadata) SetKeyID(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.KeyID = id
}

func (m *RequestLoggingMetadata) SetTokensAndCost(prompt, completion int, cost float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PromptTokens = prompt
	m.CompletionTokens = completion
	m.Cost = cost
}

func (m *RequestLoggingMetadata) SetComplexityScore(score float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ComplexityScore = score
}

// SlogAttrs returns all metadata fields as flat key-value pairs
// suitable for slog.InfoContext(ctx, msg, meta.SlogAttrs()...).
// Zero-value fields and nil intervention pointers are skipped.
func (m *RequestLoggingMetadata) SlogAttrs() []any {
	m.mu.Lock()
	defer m.mu.Unlock()

	var attrs []any

	if m.KeyID != "" {
		attrs = append(attrs, "key_id", m.KeyID)
	}
	if m.PromptTokens > 0 || m.CompletionTokens > 0 {
		attrs = append(attrs, "prompt_tokens", m.PromptTokens)
		attrs = append(attrs, "completion_tokens", m.CompletionTokens)
		attrs = append(attrs, "total_tokens", m.PromptTokens+m.CompletionTokens)
	}
	if m.Cost > 0 {
		attrs = append(attrs, "cost", m.Cost)
	}

	var interventions []string
	if m.CacheHit != nil {
		if *m.CacheHit {
			interventions = append(interventions, "cache_hit")
		} else {
			interventions = append(interventions, "cache_miss")
		}
	}
	if m.SmartRouter != nil && *m.SmartRouter && m.SmartRoutedTo != "" {
		interventions = append(interventions, m.SmartRoutedTo)
	}
	if m.PIIMasked != nil && *m.PIIMasked {
		interventions = append(interventions, "pii_masked")
	}
	if m.PIIBlocked != nil && *m.PIIBlocked {
		interventions = append(interventions, "pii_blocked")
	}
	if m.LoopWarning != nil && *m.LoopWarning {
		interventions = append(interventions, "loop_warning")
	}
	if m.LoopBlocked != nil && *m.LoopBlocked {
		interventions = append(interventions, "loop_blocked")
	}
	if m.RateLimited != nil && *m.RateLimited {
		interventions = append(interventions, "rate_limited")
	}
	if m.BudgetExceeded != nil && *m.BudgetExceeded {
		interventions = append(interventions, "budget_exceeded")
	}
	if len(interventions) > 0 {
		attrs = append(attrs, "interventions", strings.Join(interventions, ","))
	}

	return attrs
}

func GetKeyID(ctx context.Context) string {
	if idVal := ctx.Value(KeyIDContextKey); idVal != nil {
		if id, ok := idVal.(string); ok {
			return id
		}
	}
	// Fallback: try the deprecated key ID context key.
	if idVal := ctx.Value(APIKeyIDContextKey); idVal != nil {
		if id, ok := idVal.(string); ok {
			return id
		}
	}
	return ""
}

func GetAPIKeyBudget(ctx context.Context) (float64, bool) {
	if val := ctx.Value(APIKeyBudgetContextKey); val != nil {
		if b, ok := val.(float64); ok {
			return b, true
		}
	}
	return 0.0, false
}

func GetAPIKeyDailyLimit(ctx context.Context) (float64, bool) {
	if val := ctx.Value(APIKeyDailyLimitContextKey); val != nil {
		if b, ok := val.(float64); ok {
			return b, true
		}
	}
	return 0.0, false
}

func IsAPIKeyAuthDone(ctx context.Context) bool {
	if val := ctx.Value(APIKeyAuthDoneContextKey); val != nil {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return false
}

func GetUserID(ctx context.Context) *int {
	if val := ctx.Value(UserIDContextKey); val != nil {
		if id, ok := val.(int); ok {
			return &id
		}
	}
	return nil
}

func GetGroupIDs(ctx context.Context) []int {
	if val := ctx.Value(GroupIDsContextKey); val != nil {
		if ids, ok := val.([]int); ok {
			return ids
		}
	}
	return nil
}
