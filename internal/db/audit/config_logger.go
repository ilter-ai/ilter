// Package audit provides audit logging for configuration mutations.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ilter-ai/ilter/internal/db/sqlc"
)

// SQLiteConfigAuditor logs configuration mutations (create/update/delete) to the
// config_audit_log table. Sensitive fields (api_key, secret, token, etc.) are
// masked before db.
type SQLiteConfigAuditor struct {
	queries *sqlc.Queries
}

// NewSQLiteConfigAuditor creates a new SQLiteConfigAuditor.
func NewSQLiteConfigAuditor(db *sql.DB) *SQLiteConfigAuditor {
	return &SQLiteConfigAuditor{queries: sqlc.New(db)}
}

func (a *SQLiteConfigAuditor) LogCreate(entityType, entityID string, newValues map[string]any, performedBy string) error {
	newJSON, err := maskAndMarshal(newValues)
	if err != nil {
		return fmt.Errorf("marshal new_values: %w", err)
	}
	err = a.queries.LogConfigCreate(context.Background(), sqlc.LogConfigCreateParams{
		EntityType:  entityType,
		EntityID:    entityID,
		NewValues:   &newJSON,
		PerformedBy: nullIfEmpty(performedBy),
	})
	if err != nil {
		return fmt.Errorf("insert config audit log: %w", err)
	}
	return nil
}

func (a *SQLiteConfigAuditor) LogUpdate(entityType, entityID string, oldValues, newValues map[string]any, performedBy string) error {
	oldJSON, err := maskAndMarshal(oldValues)
	if err != nil {
		return fmt.Errorf("marshal old_values: %w", err)
	}
	newJSON, err := maskAndMarshal(newValues)
	if err != nil {
		return fmt.Errorf("marshal new_values: %w", err)
	}
	err = a.queries.LogConfigUpdate(context.Background(), sqlc.LogConfigUpdateParams{
		EntityType:  entityType,
		EntityID:    entityID,
		OldValues:   &oldJSON,
		NewValues:   &newJSON,
		PerformedBy: nullIfEmpty(performedBy),
	})
	if err != nil {
		return fmt.Errorf("insert config audit log: %w", err)
	}
	return nil
}

func (a *SQLiteConfigAuditor) LogDelete(entityType, entityID string, oldValues map[string]any, performedBy string) error {
	oldJSON, err := maskAndMarshal(oldValues)
	if err != nil {
		return fmt.Errorf("marshal old_values: %w", err)
	}
	err = a.queries.LogConfigDelete(context.Background(), sqlc.LogConfigDeleteParams{
		EntityType:  entityType,
		EntityID:    entityID,
		OldValues:   &oldJSON,
		PerformedBy: nullIfEmpty(performedBy),
	})
	if err != nil {
		return fmt.Errorf("insert config audit log: %w", err)
	}
	return nil
}

// secretFieldPrefixes are case-insensitive key substrings that indicate a field
// holds sensitive data. Matching fields are replaced with "***" before db.
var secretFieldPrefixes = []string{
	"api_key",
	"secret",
	"token",
	"password",
	"auth_key",
}

// maskAndMarshal masks secret fields in the map, then marshals to JSON.
func maskAndMarshal(v map[string]any) (string, error) {
	if v == nil {
		return "null", nil
	}
	masked := make(map[string]any, len(v))
	for k, val := range v {
		if isSecretField(k) {
			masked[k] = "***"
		} else {
			masked[k] = val
		}
	}
	raw, err := json.Marshal(masked)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// isSecretField checks if a key matches known secret field patterns.
func isSecretField(key string) bool {
	lower := strings.ToLower(key)
	for _, p := range secretFieldPrefixes {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
