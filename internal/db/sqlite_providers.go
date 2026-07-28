package db

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/ilter-ai/ilter/internal/db/sqlc"
	"github.com/ilter-ai/ilter/internal/model/catalog"
)

// ProviderModel represents a model discovered from a provider API.
type ProviderModel struct {
	ID               int       `json:"id"`
	Provider         string    `json:"provider"`
	Model            string    `json:"model"`
	Active           bool      `json:"active"`
	Tier             string    `json:"tier"`
	CostIn           float64   `json:"cost_in"`
	CostOut          float64   `json:"cost_out"`
	DisplayName      string    `json:"display_name"`
	MaxContextTokens int       `json:"max_context_tokens"`
	MaxOutputTokens  int       `json:"max_output_tokens"`
	Capabilities     string    `json:"capabilities"`
	DefaultBaseURL   string    `json:"default_base_url"`
	DiscoveredAt     time.Time `json:"discovered_at"`
}

func sqlcProviderModelToDB(m sqlc.ProviderModel) ProviderModel {
	pm := ProviderModel{
		ID:               int(m.ID),
		Provider:         m.Provider,
		Model:            m.Model,
		Active:           m.Active != 0,
		Tier:             m.Tier,
		CostIn:           m.CostIn,
		CostOut:          m.CostOut,
		DisplayName:      strDeref(m.DisplayName),
		MaxContextTokens: int(int64Deref(m.MaxContextTokens)),
		MaxOutputTokens:  int(int64Deref(m.MaxOutputTokens)),
		Capabilities:     strDeref(m.Capabilities),
		DefaultBaseURL:   strDeref(m.DefaultBaseUrl),
	}
	if m.DiscoveredAt != nil {
		pm.DiscoveredAt = *m.DiscoveredAt
	}
	return pm
}

func sqlcProviderModelsToDB(models []sqlc.ProviderModel) []ProviderModel {
	result := make([]ProviderModel, 0, len(models))
	for _, m := range models {
		result = append(result, sqlcProviderModelToDB(m))
	}
	return result
}

// SaveDiscoveredModels merges discovered models into provider_models.
// Existing models are updated (active stays as-is), new models are inserted.
// Models discovered by upstream but not in the DB are added; models already
// in the DB that upstream no longer returns are preserved (not deleted) so
// a transient upstream blip doesn't wipe known models.
func (s *SQLiteStore) SaveDiscoveredModels(provider string, models []catalog.ModelInfo) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare(`INSERT INTO provider_models
		(provider, model, active, tier, cost_in, cost_out, display_name, max_context_tokens, max_output_tokens, capabilities, default_base_url, discovered_at)
		VALUES (?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(provider, model) DO UPDATE SET
			tier=COALESCE(NULLIF(excluded.tier, ''), tier),
			cost_in=excluded.cost_in,
			cost_out=excluded.cost_out,
			display_name=excluded.display_name,
			max_context_tokens=excluded.max_context_tokens,
			max_output_tokens=excluded.max_output_tokens,
			capabilities=excluded.capabilities,
			default_base_url=excluded.default_base_url,
			discovered_at=datetime('now')`)
	if err != nil {
		return fmt.Errorf("prepare upsert: %w", err)
	}
	defer stmt.Close()

	for _, m := range models {
		capsJSON := "[]"
		if len(m.Capabilities) > 0 {
			if b, mErr := json.Marshal(m.Capabilities); mErr == nil {
				capsJSON = string(b)
			}
		}
		if _, execErr := stmt.Exec(provider, m.ID, m.Tier, m.CostPerInputToken, m.CostPerOutputToken,
			m.DisplayName, m.MaxContextTokens, m.MaxOutputTokens, capsJSON, m.DefaultBaseURL); execErr != nil {
			return fmt.Errorf("upsert model %s: %w", m.ID, execErr)
		}
	}

	// Mark models that upstream no longer returns as inactive so they don't
	// vanish from the UI — they stay available for manual re-enable.
	existing, err := s.queries.GetProviderModels(context.Background(), provider)
	if err == nil {
		discovered := make(map[string]bool, len(models))
		for _, m := range models {
			discovered[m.ID] = true
		}
		for _, em := range existing {
			if !discovered[em.Model] && em.Active != 0 {
				if _, uErr := tx.Exec("UPDATE provider_models SET active=0 WHERE provider=? AND model=?", provider, em.Model); uErr != nil {
					slog.Warn("failed to mark model inactive", "provider", provider, "model", em.Model, "error", uErr)
				}
			}
		}
	}

	return tx.Commit()
}

// ProviderModelCount returns the number of models registered for a provider.
func (s *SQLiteStore) ProviderModelCount(provider string) (int, error) {
	count, err := s.queries.ProviderModelCount(context.Background(), provider)
	return int(count), err
}

// GetAllProviderModels returns all rows from provider_models ordered by provider, model.
func (s *SQLiteStore) GetAllProviderModels() ([]ProviderModel, error) {
	models, err := s.queries.GetAllProviderModels(context.Background())
	if err != nil {
		return nil, err
	}
	return sqlcProviderModelsToDB(models), nil
}

// GetAllModelInfo returns all provider_models rows converted to catalog.ModelInfo.
func (s *SQLiteStore) GetAllModelInfo() ([]catalog.ModelInfo, error) {
	models, err := s.queries.GetAllProviderModels(context.Background())
	if err != nil {
		return nil, err
	}
	return providerModelsToModelInfo(sqlcProviderModelsToDB(models)), nil
}

// GetModelInfoByProvider returns all provider_models rows for a given provider,
// converted to catalog.ModelInfo.
func (s *SQLiteStore) GetModelInfoByProvider(provider string) ([]catalog.ModelInfo, error) {
	models, err := s.queries.GetProviderModels(context.Background(), provider)
	if err != nil {
		return nil, err
	}
	return providerModelsToModelInfo(sqlcProviderModelsToDB(models)), nil
}

// GetActiveProviderModels returns rows from provider_models where active = 1.
func (s *SQLiteStore) GetActiveProviderModels() ([]ProviderModel, error) {
	models, err := s.queries.GetActiveProviderModels(context.Background())
	if err != nil {
		return nil, err
	}
	return sqlcProviderModelsToDB(models), nil
}

// GetLatestDiscovery returns the most recent discovery timestamp for a provider.
// Returns zero time if no models exist yet.
func (s *SQLiteStore) GetLatestDiscovery(provider string) (time.Time, error) {
	val, err := s.queries.GetLatestDiscovery(context.Background(), provider)
	if err != nil {
		return time.Time{}, err
	}
	str, ok := val.(string)
	if !ok || str == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse("2006-01-02 15:04:05", str)
	if err != nil {
		return time.Time{}, err
	}
	return t, nil
}

// GetProviderModels returns all rows for a given provider.
func (s *SQLiteStore) GetProviderModels(provider string) ([]ProviderModel, error) {
	models, err := s.queries.GetProviderModels(context.Background(), provider)
	if err != nil {
		return nil, err
	}
	return sqlcProviderModelsToDB(models), nil
}

func providerModelsToModelInfo(pms []ProviderModel) []catalog.ModelInfo {
	models := make([]catalog.ModelInfo, 0, len(pms))
	for _, pm := range pms {
		caps := []string{}
		if pm.Capabilities != "" {
			if err := json.Unmarshal([]byte(pm.Capabilities), &caps); err != nil {
				slog.Warn("failed to unmarshal provider capabilities", "provider", pm.Provider, "model", pm.Model, "error", err)
			}
		}
		models = append(models, catalog.ModelInfo{
			ID:                 pm.Model,
			Provider:           pm.Provider,
			DisplayName:        pm.DisplayName,
			MaxContextTokens:   pm.MaxContextTokens,
			MaxOutputTokens:    pm.MaxOutputTokens,
			CostPerInputToken:  pm.CostIn,
			CostPerOutputToken: pm.CostOut,
			Tier:               pm.Tier,
			Capabilities:       caps,
			DefaultBaseURL:     pm.DefaultBaseURL,
		})
	}
	return models
}

func (s *SQLiteStore) GetInactiveModels() ([]string, error) {
	return s.queries.GetInactiveModels(context.Background())
}

func (s *SQLiteStore) GetModelStatuses() (map[string]bool, error) {
	rows, err := s.queries.GetModelStatuses(context.Background())
	if err != nil {
		return nil, err
	}
	res := make(map[string]bool, len(rows))
	for _, r := range rows {
		res[r.Model] = r.Active != 0
	}
	return res, nil
}

func (s *SQLiteStore) SaveModelStatus(name string, active bool) error {
	actVal := int64(0)
	if active {
		actVal = 1
	}
	return s.queries.SaveModelStatus(context.Background(), sqlc.SaveModelStatusParams{
		Active: actVal,
		Model:  name,
	})
}

func (s *SQLiteStore) SaveModelTier(modelName string, tier string) error {
	return s.queries.SaveModelTier(context.Background(), sqlc.SaveModelTierParams{
		Tier:  tier,
		Model: modelName,
	})
}
