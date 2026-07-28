package auth

import "time"

// APIKey represents an API key with budgets, rate limits,
// and model/provider access restrictions.
type APIKey struct {
	ID                  string            `json:"id"`
	Name                string            `json:"name"`
	Key                 string            `json:"-"` // raw token, returned only on create
	GroupID             *int              `json:"group_id,omitempty"`
	UserID              *int              `json:"user_id,omitempty"`
	Tags                map[string]string `json:"tags"`
	MonthlyBudgetUSD    float64           `json:"monthly_budget_usd"`
	MonthlyBudgetTokens int64             `json:"monthly_budget_tokens"`
	RateLimitRPM        int               `json:"rate_limit_rpm"`
	RateLimitTPM        int64             `json:"rate_limit_tpm"`
	AllowedModels       []string          `json:"allowed_models"`
	AllowedProviders    []string          `json:"allowed_providers"`
	Enabled             bool              `json:"enabled"`
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
}

// KeyUsage records per-key usage breakdown for a single date/model/provider.
type KeyUsage struct {
	KeyID        string    `json:"key_id"`
	Date         time.Time `json:"date"`
	TokensIn     int64     `json:"tokens_in"`
	TokensOut    int64     `json:"tokens_out"`
	CostUSD      float64   `json:"cost_usd"`
	RequestCount int64     `json:"request_count"`
	Model        string    `json:"model"`
	Provider     string    `json:"provider"`
}
