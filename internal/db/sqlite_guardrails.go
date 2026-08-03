package db

import (
	"context"

	"github.com/ilter-ai/ilter/internal/db/sqlc"
)

// GuardrailRuleSummary is a guardrail_rules row for admin list display (no patterns).
type GuardrailRuleSummary struct {
	ID         string
	Name       string
	Type       string
	Mode       string
	Severity   string
	Enabled    bool
	TargetType string
	TargetID   *int
}

// ListGuardrailRules returns all guardrail rules ordered by name, for admin display.
func (s *SQLiteStore) ListGuardrailRules() ([]GuardrailRuleSummary, error) {
	rows, err := s.queries.ListGuardrailRules(context.Background())
	if err != nil {
		return nil, err
	}
	result := make([]GuardrailRuleSummary, 0, len(rows))
	for _, r := range rows {
		result = append(result, GuardrailRuleSummary{
			ID:         r.ID,
			Name:       r.Name,
			Type:       r.Type,
			Mode:       r.Mode,
			Severity:   r.Severity,
			Enabled:    r.Enabled != 0,
			TargetType: r.TargetType,
			TargetID:   int64PtrToIntPtr(r.TargetID),
		})
	}
	return result, nil
}

// GuardrailDBRule is an enabled guardrail_rules row, loaded into the runtime checker.
type GuardrailDBRule struct {
	ID         string
	Type       string
	Patterns   string
	Mode       string
	Severity   string
	TargetType string
	TargetID   *int
}

// GetEnabledGuardrailRules returns all enabled guardrail rules for loading into the checker.
func (s *SQLiteStore) GetEnabledGuardrailRules() ([]GuardrailDBRule, error) {
	rows, err := s.queries.GetEnabledGuardrailRules(context.Background())
	if err != nil {
		return nil, err
	}
	result := make([]GuardrailDBRule, 0, len(rows))
	for _, r := range rows {
		result = append(result, GuardrailDBRule{
			ID:         r.ID,
			Type:       r.Type,
			Patterns:   r.Patterns,
			Mode:       r.Mode,
			Severity:   r.Severity,
			TargetType: r.TargetType,
			TargetID:   int64PtrToIntPtr(r.TargetID),
		})
	}
	return result, nil
}

// ToggleGuardrailRule sets enabled for a rule. Returns false if no rule matched id.
func (s *SQLiteStore) ToggleGuardrailRule(id string, enabled bool) (bool, error) {
	n, err := s.queries.ToggleGuardrailRule(context.Background(), sqlc.ToggleGuardrailRuleParams{
		Enabled: boolToInt64(enabled),
		ID:      id,
	})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// CreateGuardrailRuleParams holds the fields needed to insert a new guardrail_rules row.
type CreateGuardrailRuleParams struct {
	ID          string
	Name        string
	Description string
	Patterns    string // JSON-encoded []string
	Mode        string
	Severity    string
	TargetType  string
	TargetID    *int
	Type        string
}

// CreateGuardrailRule inserts a new guardrail rule. Returns an error if id already exists.
func (s *SQLiteStore) CreateGuardrailRule(p CreateGuardrailRuleParams) error {
	return s.queries.CreateGuardrailRule(context.Background(), sqlc.CreateGuardrailRuleParams{
		ID:          p.ID,
		Name:        p.Name,
		Description: new(p.Description),
		Patterns:    p.Patterns,
		Mode:        p.Mode,
		Severity:    p.Severity,
		TargetType:  new(p.TargetType),
		TargetID:    intToInt64Ptr(p.TargetID),
		Type:        p.Type,
	})
}

// UpdateGuardrailRuleParams holds the fields for a partial guardrail_rules update.
// Name/Type/Description/Mode/Severity: empty string means "leave unchanged".
// Patterns: "[]" or "" means "leave unchanged".
// Enabled/TargetType/TargetID: nil means "leave unchanged".
type UpdateGuardrailRuleParams struct {
	ID          string
	Name        string
	Type        string
	Description string
	Patterns    string
	Mode        string
	Severity    string
	Enabled     *bool
	TargetType  *string
	TargetID    *int
}

// UpdateGuardrailRule applies a partial update. Returns false if no rule matched id.
func (s *SQLiteStore) UpdateGuardrailRule(p UpdateGuardrailRuleParams) (bool, error) {
	var enabled *int64
	if p.Enabled != nil {
		v := boolToInt64(*p.Enabled)
		enabled = &v
	}
	n, err := s.queries.UpdateGuardrailRule(context.Background(), sqlc.UpdateGuardrailRuleParams{
		Name:        p.Name,
		Type:        p.Type,
		Description: p.Description,
		Patterns:    p.Patterns,
		Mode:        p.Mode,
		Severity:    p.Severity,
		Enabled:     enabled,
		TargetType:  p.TargetType,
		TargetID:    intToInt64Ptr(p.TargetID),
		ID:          p.ID,
	})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// DeleteGuardrailRule removes a guardrail rule. Returns false if no rule matched id.
func (s *SQLiteStore) DeleteGuardrailRule(id string) (bool, error) {
	n, err := s.queries.DeleteGuardrailRule(context.Background(), id)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// InsertGuardrailEvent records a guardrail decision (block/warn) for the violation log.
func (s *SQLiteStore) InsertGuardrailEvent(keyID, guardrailType, actionTaken, modelName, provider, details string) error {
	return s.queries.InsertGuardrailEvent(context.Background(), sqlc.InsertGuardrailEventParams{
		KeyID:         new(keyID),
		GuardrailType: guardrailType,
		ActionTaken:   actionTaken,
		Model:         new(modelName),
		Provider:      new(provider),
		Details:       new(details),
	})
}

// GetProviderForModel returns the provider that owns modelName, per provider_models.
func (s *SQLiteStore) GetProviderForModel(modelName string) (string, error) {
	return s.queries.GetProviderForModel(context.Background(), modelName)
}
