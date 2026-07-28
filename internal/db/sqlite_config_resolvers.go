package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db/sqlc"
)

// InitConfigResolvers wires up the config package's scope-chain resolvers to SQLite.
func InitConfigResolvers(store *SQLiteStore) {
	config.WireResolvers(
		func(keyID string) (teamID, orgID string) {
			if keyID == "" {
				return "", ""
			}
			row, err := store.queries.GetAPIKeyTeamOrg(context.Background(), keyID)
			if err != nil {
				if !errors.Is(err, sql.ErrNoRows) {
					slog.Error("failed to query api_key team/org ownership", "keyID", keyID, "error", err)
				}
				return "", ""
			}
			if row.TeamID != nil {
				teamID = *row.TeamID
			}
			if row.OrgID != nil {
				orgID = *row.OrgID
			}
			return
		},
		func(scope, scopeID, field string) (any, bool) {
			raw, err := store.queries.GetConfigSetting(context.Background(), sqlc.GetConfigSettingParams{
				Scope: scope, ScopeID: scopeID, Field: field,
			})
			if err != nil {
				if !errors.Is(err, sql.ErrNoRows) {
					slog.Error("failed to query config setting", "scope", scope, "scopeID", scopeID, "field", field, "error", err)
				}
				return nil, false
			}

			var parsed any
			if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
				slog.Error("failed to unmarshal config setting JSON", "scope", scope, "scopeID", scopeID, "field", field, "error", err)
				return nil, false
			}

			return parsed, true
		},
	)
}
