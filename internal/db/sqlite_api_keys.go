package db

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/ilter-ai/ilter/internal/auth"
	"github.com/ilter-ai/ilter/internal/db/sqlc"
	"github.com/ilter-ai/ilter/internal/platform/crypto"
)

func generateAPIKeyToken() (string, string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("failed to generate API key token: %w", err)
	}
	token := hex.EncodeToString(b)
	prefix := token[:12]
	return token, prefix, nil
}

func hashAPIKey(rawToken string) string {
	hash := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(hash[:])
}

// ---------------------------------------------------------------------------
// apiKeyFromSQLC — converts a sqlc API key row to auth.APIKey
// ---------------------------------------------------------------------------

func apiKeyFromSQLC(
	id string,
	name string,
	groupID *int64,
	userID *int64,
	tags *string,
	monthlyBudgetUSD *float64,
	monthlyBudgetTokens *int64,
	rateLimitRPM *int64,
	rateLimitTPM *int64,
	allowedModels *string,
	allowedProviders *string,
	enabled int64,
	createdAt time.Time,
	updatedAt time.Time,
) *auth.APIKey {
	var gid, uid *int
	if groupID != nil {
		v := int(*groupID)
		gid = &v
	}
	if userID != nil {
		v := int(*userID)
		uid = &v
	}

	var models, providers []string
	var t map[string]string
	if allowedModels != nil && *allowedModels != "" {
		if err := json.Unmarshal([]byte(*allowedModels), &models); err != nil {
			slog.Error("failed to unmarshal allowed_models", "error", err)
		}
	}
	if allowedProviders != nil && *allowedProviders != "" {
		if err := json.Unmarshal([]byte(*allowedProviders), &providers); err != nil {
			slog.Error("failed to unmarshal allowed_providers", "error", err)
		}
	}
	if tags != nil && *tags != "" {
		if err := json.Unmarshal([]byte(*tags), &t); err != nil {
			slog.Error("failed to unmarshal tags", "error", err)
		}
	}

	if models == nil {
		models = []string{}
	}
	if providers == nil {
		providers = []string{}
	}
	if t == nil {
		t = map[string]string{}
	}

	rpm := 0
	if rateLimitRPM != nil {
		rpm = int(*rateLimitRPM)
	}
	tpm := int64(0)
	if rateLimitTPM != nil {
		tpm = *rateLimitTPM
	}
	mbTokens := int64(0)
	if monthlyBudgetTokens != nil {
		mbTokens = *monthlyBudgetTokens
	}
	mbUSD := 0.0
	if monthlyBudgetUSD != nil {
		mbUSD = *monthlyBudgetUSD
	}

	return &auth.APIKey{
		ID:                  id,
		Name:                name,
		GroupID:             gid,
		UserID:              uid,
		Tags:                t,
		MonthlyBudgetUSD:    mbUSD,
		MonthlyBudgetTokens: mbTokens,
		RateLimitRPM:        rpm,
		RateLimitTPM:        tpm,
		AllowedModels:       models,
		AllowedProviders:    providers,
		Enabled:             enabled == 1,
		CreatedAt:           createdAt,
		UpdatedAt:           updatedAt,
	}
}

// ---------------------------------------------------------------------------
// CreateAPIKey
// ---------------------------------------------------------------------------

// CreateAPIKey inserts a new API key and returns the generated raw token.
// The token is returned only once — it is not stored in plaintext.
func (s *SQLiteStore) CreateAPIKey(name string, groupID *int, userID *int, monthlyBudgetUSD float64, monthlyBudgetTokens int64, rateLimitRPM int, rateLimitTPM int64, allowedModels, allowedProviders []string, tags map[string]string) (*auth.APIKey, string, error) {
	token, prefix, err := generateAPIKeyToken()
	if err != nil {
		return nil, "", err
	}

	hash := hashAPIKey(token)
	id := prefix

	modelsJSON, _ := json.Marshal(allowedModels)
	providersJSON, _ := json.Marshal(allowedProviders)
	tagsJSON, _ := json.Marshal(tags)

	modelsStr := string(modelsJSON)
	providersStr := string(providersJSON)
	tagsStr := string(tagsJSON)

	now := time.Now().UTC()
	rpm := int64(rateLimitRPM)

	err = s.queries.CreateAPIKey(context.Background(), sqlc.CreateAPIKeyParams{
		ID:                  id,
		Name:                name,
		HashedKey:           hash,
		Salt:                "sha256",
		KeyPrefix:           &prefix,
		GroupID:             intToInt64Ptr(groupID),
		UserID:              intToInt64Ptr(userID),
		Tags:                &tagsStr,
		MonthlyBudgetUsd:    &monthlyBudgetUSD,
		MonthlyBudgetTokens: &monthlyBudgetTokens,
		RateLimitRpm:        &rpm,
		RateLimitTpm:        &rateLimitTPM,
		RateLimitRetryAfter: nil,
		AllowedModels:       &modelsStr,
		AllowedProviders:    &providersStr,
		Enabled:             1,
		CreatedAt:           now,
		UpdatedAt:           now,
	})
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return nil, "", fmt.Errorf("API key %q already exists", name)
		}
		return nil, "", fmt.Errorf("create API key: %w", err)
	}

	vk := &auth.APIKey{
		ID:                  id,
		Name:                name,
		Key:                 token,
		GroupID:             groupID,
		UserID:              userID,
		Tags:                tags,
		MonthlyBudgetUSD:    monthlyBudgetUSD,
		MonthlyBudgetTokens: monthlyBudgetTokens,
		RateLimitRPM:        rateLimitRPM,
		RateLimitTPM:        rateLimitTPM,
		AllowedModels:       allowedModels,
		AllowedProviders:    allowedProviders,
		Enabled:             true,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	return vk, token, nil
}

// ---------------------------------------------------------------------------
// GetAPIKey
// ---------------------------------------------------------------------------

// GetAPIKey retrieves an API key by its ID.
func (s *SQLiteStore) GetAPIKey(id string) (*auth.APIKey, error) {
	dbKey, err := s.queries.GetAPIKey(context.Background(), id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("API key %q not found", id)
		}
		return nil, err
	}
	return apiKeyFromSQLC(
		dbKey.ID, dbKey.Name, dbKey.GroupID, dbKey.UserID,
		dbKey.Tags, dbKey.MonthlyBudgetUsd, dbKey.MonthlyBudgetTokens,
		dbKey.RateLimitRpm, dbKey.RateLimitTpm,
		dbKey.AllowedModels, dbKey.AllowedProviders,
		dbKey.Enabled, dbKey.CreatedAt, dbKey.UpdatedAt,
	), nil
}

// ---------------------------------------------------------------------------
// getAPIKeyByHashLookup
// ---------------------------------------------------------------------------

func (s *SQLiteStore) getAPIKeyByHashLookup(hash string) (*auth.APIKey, error) {
	dbKey, err := s.queries.GetAPIKeyByHash(context.Background(), hash)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("API key not found")
		}
		return nil, err
	}
	return apiKeyFromSQLC(
		dbKey.ID, dbKey.Name, dbKey.GroupID, dbKey.UserID,
		dbKey.Tags, dbKey.MonthlyBudgetUsd, dbKey.MonthlyBudgetTokens,
		dbKey.RateLimitRpm, dbKey.RateLimitTpm,
		dbKey.AllowedModels, dbKey.AllowedProviders,
		dbKey.Enabled, dbKey.CreatedAt, dbKey.UpdatedAt,
	), nil
}

// ---------------------------------------------------------------------------
// GetAPIKeyByHash
// ---------------------------------------------------------------------------

// GetAPIKeyByHash retrieves an API key by its hashed token (SHA-256).
func (s *SQLiteStore) GetAPIKeyByHash(rawToken string) (*auth.APIKey, error) {
	hash := hashAPIKey(rawToken)
	return s.getAPIKeyByHashLookup(hash)
}

// ---------------------------------------------------------------------------
// GetActiveKeyByHash
// ---------------------------------------------------------------------------

func (s *SQLiteStore) GetActiveKeyByHash(rawToken string) (*auth.APIKey, error) {
	hash := hashAPIKey(rawToken)
	dbKey, err := s.queries.GetAPIKeyByHash(context.Background(), hash)
	if err == nil {
		return apiKeyFromSQLC(
			dbKey.ID, dbKey.Name, dbKey.GroupID, dbKey.UserID,
			dbKey.Tags, dbKey.MonthlyBudgetUsd, dbKey.MonthlyBudgetTokens,
			dbKey.RateLimitRpm, dbKey.RateLimitTpm,
			dbKey.AllowedModels, dbKey.AllowedProviders,
			dbKey.Enabled, dbKey.CreatedAt, dbKey.UpdatedAt,
		), nil
	}

	prefix := crypto.ExtractKeyPrefix(rawToken)
	if prefix != "" {
		vk, err := s.lookupAPIKeyArgon2id(rawToken, &prefix)
		if err == nil {
			return vk, nil
		}

		vk, err = s.lookupAPIKeyArgon2idNoPrefix(rawToken)
		if err == nil {
			return vk, nil
		}
	}

	return nil, sql.ErrNoRows
}

// ---------------------------------------------------------------------------
// IsAdminAPIKey
// ---------------------------------------------------------------------------

// IsAdminAPIKey reports whether rawToken is a valid, enabled API key that
// belongs to the "admin" group. This lets the API key ilter init prints
// (meant for calling the LLM proxy) also work as a dashboard login
// credential, so dashboard access isn't solely gated on ILTER_ADMIN_API_KEY
// being set.
func (s *SQLiteStore) IsAdminAPIKey(rawToken string) bool {
	if rawToken == "" {
		return false
	}
	key, err := s.GetActiveKeyByHash(rawToken)
	if err != nil || key == nil || !key.Enabled || key.GroupID == nil {
		return false
	}
	group, err := s.GetGroupByName("admin")
	if err != nil || group == nil {
		return false
	}
	return *key.GroupID == group.ID
}

// lookupAPIKeyArgon2id queries a single key row by prefix and verifies with Argon2id.
func (s *SQLiteStore) lookupAPIKeyArgon2id(rawToken string, keyPrefix *string) (*auth.APIKey, error) {
	row, err := s.queries.GetAPIKeyWithHash(context.Background(), keyPrefix)
	if err != nil {
		return nil, err
	}
	return s.argon2idVerifyAndConvert(rawToken, row.HashedKey, row.Salt, row.ID, row.Name, row.GroupID, row.UserID, row.Tags, row.MonthlyBudgetUsd, row.MonthlyBudgetTokens, row.RateLimitRpm, row.RateLimitTpm, row.AllowedModels, row.AllowedProviders, row.Enabled, row.CreatedAt, row.UpdatedAt)
}

// lookupAPIKeyArgon2idNoPrefix queries keys without a key_prefix and verifies with Argon2id.
func (s *SQLiteStore) lookupAPIKeyArgon2idNoPrefix(rawToken string) (*auth.APIKey, error) {
	row, err := s.queries.GetAPIKeyWithHashNoPrefix(context.Background())
	if err != nil {
		return nil, err
	}
	return s.argon2idVerifyAndConvert(rawToken, row.HashedKey, row.Salt, row.ID, row.Name, row.GroupID, row.UserID, row.Tags, row.MonthlyBudgetUsd, row.MonthlyBudgetTokens, row.RateLimitRpm, row.RateLimitTpm, row.AllowedModels, row.AllowedProviders, row.Enabled, row.CreatedAt, row.UpdatedAt)
}

// argon2idVerifyAndConvert checks rawToken against storedHash using Argon2id and
// converts the sqlc row fields to an auth.APIKey on match.
func (s *SQLiteStore) argon2idVerifyAndConvert(rawToken, storedHash, salt string, id string, name string, groupID *int64, userID *int64, tags *string, monthlyBudgetUSD *float64, monthlyBudgetTokens *int64, rateLimitRPM *int64, rateLimitTPM *int64, allowedModels *string, allowedProviders *string, enabled int64, createdAt, updatedAt time.Time) (*auth.APIKey, error) {
	computedHash, err := crypto.HashTokenWithSalt(rawToken, salt, "argon2")
	if err != nil {
		slog.Warn("Failed to compute Argon2id hash", "error", err)
		return nil, err
	}

	if subtle.ConstantTimeCompare([]byte(computedHash), []byte(storedHash)) == 1 {
		return apiKeyFromSQLC(
			id, name, groupID, userID,
			tags, monthlyBudgetUSD, monthlyBudgetTokens,
			rateLimitRPM, rateLimitTPM,
			allowedModels, allowedProviders,
			enabled, createdAt, updatedAt,
		), nil
	}

	return nil, sql.ErrNoRows
}

// ---------------------------------------------------------------------------
// ListAPIKeys
// ---------------------------------------------------------------------------

// ListAPIKeys returns all API keys, optionally filtered by group_id.
func (s *SQLiteStore) ListAPIKeys(groupID ...int) ([]auth.APIKey, error) {
	var rows []auth.APIKey
	var err error

	if len(groupID) > 0 {
		gid := int64(groupID[0])
		dbKeys, qErr := s.queries.ListAPIKeysByGroup(context.Background(), &gid)
		if qErr != nil {
			return nil, fmt.Errorf("list API keys: %w", qErr)
		}
		rows = make([]auth.APIKey, 0, len(dbKeys))
		for _, k := range dbKeys {
			rows = append(rows, *apiKeyFromSQLC(
				k.ID, k.Name, k.GroupID, k.UserID,
				k.Tags, k.MonthlyBudgetUsd, k.MonthlyBudgetTokens,
				k.RateLimitRpm, k.RateLimitTpm,
				k.AllowedModels, k.AllowedProviders,
				k.Enabled, k.CreatedAt, k.UpdatedAt,
			))
		}
		return rows, nil
	}

	dbKeys, err := s.queries.ListAPIKeys(context.Background())
	if err != nil {
		return nil, fmt.Errorf("list API keys: %w", err)
	}

	rows = make([]auth.APIKey, 0, len(dbKeys))
	for _, k := range dbKeys {
		rows = append(rows, *apiKeyFromSQLC(
			k.ID, k.Name, k.GroupID, k.UserID,
			k.Tags, k.MonthlyBudgetUsd, k.MonthlyBudgetTokens,
			k.RateLimitRpm, k.RateLimitTpm,
			k.AllowedModels, k.AllowedProviders,
			k.Enabled, k.CreatedAt, k.UpdatedAt,
		))
	}
	return rows, nil
}

// ---------------------------------------------------------------------------
// UpdateAPIKey
// ---------------------------------------------------------------------------

// UpdateAPIKey updates an API key's metadata. Fields with zero values
// are treated as no-change, except GroupID/UserID when clearGroupID/
// clearUserID is set, which explicitly NULLs that column (a nil pointer
// alone is ambiguous between "not provided" and "clear").
func (s *SQLiteStore) UpdateAPIKey(id string, updates auth.APIKey, clearGroupID, clearUserID bool) error {
	existing, err := s.GetAPIKey(id)
	if err != nil {
		return err
	}

	name := updates.Name
	if name == "" {
		name = existing.Name
	}
	groupID := updates.GroupID
	if groupID == nil && !clearGroupID {
		groupID = existing.GroupID
	}
	userID := updates.UserID
	if userID == nil && !clearUserID {
		userID = existing.UserID
	}
	tags := updates.Tags
	if tags == nil {
		tags = existing.Tags
	}
	monthlyBudgetUSD := updates.MonthlyBudgetUSD
	if monthlyBudgetUSD == 0 {
		monthlyBudgetUSD = existing.MonthlyBudgetUSD
	}
	monthlyBudgetTokens := updates.MonthlyBudgetTokens
	if monthlyBudgetTokens == 0 {
		monthlyBudgetTokens = existing.MonthlyBudgetTokens
	}
	rateLimitRPM := updates.RateLimitRPM
	if rateLimitRPM == 0 {
		rateLimitRPM = existing.RateLimitRPM
	}
	rateLimitTPM := updates.RateLimitTPM
	if rateLimitTPM == 0 {
		rateLimitTPM = existing.RateLimitTPM
	}
	allowedModels := updates.AllowedModels
	if allowedModels == nil {
		allowedModels = existing.AllowedModels
	}
	allowedProviders := updates.AllowedProviders
	if allowedProviders == nil {
		allowedProviders = existing.AllowedProviders
	}
	modelsJSON, _ := json.Marshal(allowedModels)
	providersJSON, _ := json.Marshal(allowedProviders)
	tagsJSON, _ := json.Marshal(tags)

	modelsStr := string(modelsJSON)
	providersStr := string(providersJSON)
	tagsStr := string(tagsJSON)
	rpm := int64(rateLimitRPM)

	now := time.Now().UTC()

	err = s.queries.UpdateAPIKey(context.Background(), sqlc.UpdateAPIKeyParams{
		Name:                name,
		GroupID:             intToInt64Ptr(groupID),
		UserID:              intToInt64Ptr(userID),
		Tags:                &tagsStr,
		MonthlyBudgetUsd:    &monthlyBudgetUSD,
		MonthlyBudgetTokens: &monthlyBudgetTokens,
		RateLimitRpm:        &rpm,
		RateLimitTpm:        &rateLimitTPM,
		AllowedModels:       &modelsStr,
		AllowedProviders:    &providersStr,
		Enabled:             boolToInt64(updates.Enabled),
		UpdatedAt:           now,
		ID:                  id,
	})
	if err != nil {
		return fmt.Errorf("update API key %q: %w", id, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// SetKeyBudget — hand-written (dynamic SET builder for optional fields)
// ---------------------------------------------------------------------------

// SetKeyBudget updates monthly_budget_usd and/or monthly_budget_tokens for an API key.
// Each field pointer may be nil to leave it unchanged.
func (s *SQLiteStore) SetKeyBudget(id string, monthlyBudgetUSD *float64, monthlyBudgetTokens *int64) error {
	var sets []string
	var args []any

	if monthlyBudgetUSD != nil {
		sets = append(sets, "monthly_budget_usd = ?")
		args = append(args, *monthlyBudgetUSD)
	}
	if monthlyBudgetTokens != nil {
		sets = append(sets, "monthly_budget_tokens = ?")
		args = append(args, *monthlyBudgetTokens)
	}

	if len(sets) == 0 {
		return nil
	}

	now := time.Now().UTC()
	args = append(args, now, id)

	query := fmt.Sprintf("UPDATE api_keys SET %s, updated_at = ? WHERE id = ?", strings.Join(sets, ", "))
	res, err := s.DB.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("set key budget: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("API key %q not found", id)
	}
	return nil
}

// ---------------------------------------------------------------------------
// SetKeyRateLimit
// ---------------------------------------------------------------------------

// SetKeyRateLimit updates the rate_limit_rpm and rate_limit_retry_after for a key.
// Uses s.DB.Exec for RowsAffected checking; sqlc's :exec discards the result.
func (s *SQLiteStore) SetKeyRateLimit(id string, rpmLimit, retryAfter int) error {
	now := time.Now().UTC()
	res, err := s.DB.Exec(
		"UPDATE api_keys SET rate_limit_rpm = ?, rate_limit_retry_after = ?, updated_at = ? WHERE id = ?",
		rpmLimit, retryAfter, now, id,
	)
	if err != nil {
		return fmt.Errorf("set key rate limit %q: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("key %q not found", id)
	}
	return nil
}

// ---------------------------------------------------------------------------
// DeleteAPIKey
// ---------------------------------------------------------------------------

// DeleteAPIKey removes an API key and its usage records (via CASCADE).
// Uses s.DB.Exec for RowsAffected checking; sqlc's :exec discards the result.
func (s *SQLiteStore) DeleteAPIKey(id string) error {
	res, err := s.DB.Exec("DELETE FROM api_keys WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete API key %q: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("API key %q not found", id)
	}
	return nil
}

// ---------------------------------------------------------------------------
// RecordKeyUsage
// ---------------------------------------------------------------------------

// RecordKeyUsage inserts or updates a usage record for an API key.
// If a record for the same key/date/model/provider already exists, it is
// updated with the accumulated values.
func (s *SQLiteStore) RecordKeyUsage(keyID, date, model, provider string, tokensIn, tokensOut, requestCount int64, costUSD float64) error {
	err := s.queries.RecordKeyUsage(context.Background(), sqlc.RecordKeyUsageParams{
		KeyID:        keyID,
		Date:         date,
		TokensIn:     &tokensIn,
		TokensOut:    &tokensOut,
		CostUsd:      &costUSD,
		RequestCount: &requestCount,
		Model:        new(model),
		Provider:     new(provider),
	})
	if err != nil {
		return fmt.Errorf("record key usage: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// GetKeyUsage
// ---------------------------------------------------------------------------

// GetKeyUsage retrieves usage records for an API key within a date range.
func (s *SQLiteStore) GetKeyUsage(keyID, fromDate, toDate string) ([]auth.KeyUsage, error) {
	rows, err := s.queries.GetKeyUsage(context.Background(), sqlc.GetKeyUsageParams{
		KeyID:  keyID,
		Date:   fromDate,
		Date_2: toDate,
	})
	if err != nil {
		return nil, fmt.Errorf("get key usage: %w", err)
	}

	usage := make([]auth.KeyUsage, 0, len(rows))
	for _, r := range rows {
		date, _ := time.Parse("2006-01-02", r.Date)
		u := auth.KeyUsage{
			KeyID: r.KeyID,
			Date:  date,
			TokensIn: func() int64 {
				if r.TokensIn != nil {
					return *r.TokensIn
				}
				return 0
			}(),
			TokensOut: func() int64 {
				if r.TokensOut != nil {
					return *r.TokensOut
				}
				return 0
			}(),
			CostUSD: func() float64 {
				if r.CostUsd != nil {
					return *r.CostUsd
				}
				return 0
			}(),
			RequestCount: func() int64 {
				if r.RequestCount != nil {
					return *r.RequestCount
				}
				return 0
			}(),
			Model: func() string {
				if r.Model != nil {
					return *r.Model
				}
				return ""
			}(),
			Provider: func() string {
				if r.Provider != nil {
					return *r.Provider
				}
				return ""
			}(),
		}
		usage = append(usage, u)
	}
	return usage, nil
}

// ---------------------------------------------------------------------------
// GetCurrentMonthUsage
// ---------------------------------------------------------------------------

// GetCurrentMonthUsage returns the total cost spent by an API key this month.
func (s *SQLiteStore) GetCurrentMonthUsage(vkID string) (float64, error) {
	now := time.Now().UTC()
	firstDay := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	fromDate := firstDay.Format("2006-01-02")
	toDate := now.Format("2006-01-02")

	raw, err := s.queries.GetCurrentMonthUsage(context.Background(), sqlc.GetCurrentMonthUsageParams{
		KeyID:  vkID,
		Date:   fromDate,
		Date_2: toDate,
	})
	if err != nil {
		return 0, fmt.Errorf("get current month usage for %s: %w", vkID, err)
	}

	total, _ := toFloat64(raw)
	return total, nil
}

func toFloat64(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int64:
		return float64(val), true
	case nil:
		return 0, true
	default:
		return 0, false
	}
}

// ---------------------------------------------------------------------------
// APIKeySummary
// ---------------------------------------------------------------------------

// APIKeySummary returns aggregate usage for all API keys.
type APIKeySummary struct {
	TotalKeys      int     `json:"total_keys"`
	EnabledKeys    int     `json:"enabled_keys"`
	TotalRequests  int64   `json:"total_requests"`
	TotalCostUSD   float64 `json:"total_cost_usd"`
	TotalTokensIn  int64   `json:"total_tokens_in"`
	TotalTokensOut int64   `json:"total_tokens_out"`
}

// GetAPIKeySummary returns aggregate metrics across all API keys.
func (s *SQLiteStore) GetAPIKeySummary() (*APIKeySummary, error) {
	var summary APIKeySummary

	// Count keys and enabled keys.
	countRow, err := s.queries.CountAPIKeys(context.Background())
	if err != nil {
		return nil, fmt.Errorf("API key count: %w", err)
	}
	summary.TotalKeys = int(countRow.Count)
	enabled, _ := toFloat64(countRow.Coalesce)
	summary.EnabledKeys = int(enabled)

	// Aggregate usage.
	usageRow, err := s.queries.GetUsageSummary(context.Background())
	if err != nil {
		return nil, fmt.Errorf("API key usage summary: %w", err)
	}
	reqs, _ := toFloat64(usageRow.Coalesce)
	summary.TotalRequests = int64(reqs)
	cost, _ := toFloat64(usageRow.Coalesce_2)
	summary.TotalCostUSD = cost
	tokIn, _ := toFloat64(usageRow.Coalesce_3)
	summary.TotalTokensIn = int64(tokIn)
	tokOut, _ := toFloat64(usageRow.Coalesce_4)
	summary.TotalTokensOut = int64(tokOut)

	return &summary, nil
}
