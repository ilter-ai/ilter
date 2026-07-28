package db

import (
	"context"
	"fmt"

	"github.com/ilter-ai/ilter/internal/db/sqlc"
)

// ─────────────────────────────────────────────────────────────────────
// RuntimeConfigEntry — a single row in the runtime_config table.
// ─────────────────────────────────────────────────────────────────────

// RuntimeConfigEntry represents one row in the runtime_config table.
type RuntimeConfigEntry struct {
	Section   string `json:"section"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	Version   int    `json:"version"`
	UpdatedBy string `json:"updated_by,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────
// SQLiteStore — generic CRUD over the runtime_config table.
//
// These methods live on SQLiteStore rather than a separate store type so
// callers reuse the single *sqlc.Queries already held by the store instead
// of constructing a redundant wrapper around the same *sql.DB.
// ─────────────────────────────────────────────────────────────────────

// GetAll returns every row in the runtime_config table as a map of
// "section:key" → value.  This includes entries managed by specialised
// stores; callers can filter by section prefix as needed.
func (s *SQLiteStore) GetAll() (map[string]string, error) {
	ctx := context.Background()
	rows, err := s.queries.GetAllConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("runtime_config: query all: %w", err)
	}
	result := make(map[string]string)
	for _, row := range rows {
		result[row.Section+":"+row.Key] = row.Value
	}
	return result, nil
}

// GetBySection returns all entries for a given section as a map of
// key → value.
func (s *SQLiteStore) GetBySection(section string) (map[string]string, error) {
	ctx := context.Background()
	rows, err := s.queries.GetConfigSection(ctx, section)
	if err != nil {
		return nil, fmt.Errorf("runtime_config: query section %q: %w", section, err)
	}
	result := make(map[string]string)
	for _, row := range rows {
		result[row.Key] = row.Value
	}
	return result, nil
}

// GetRuntimeConfigEntry returns the value of a single runtime_config entry.
// Returns sql.ErrNoRows when the key does not exist.
func (s *SQLiteStore) GetRuntimeConfigEntry(section, key string) (*RuntimeConfigEntry, error) {
	ctx := context.Background()
	dbEntry, err := s.queries.GetConfig(ctx, sqlc.GetConfigParams{Section: section, Key: key})
	if err != nil {
		return nil, err
	}
	return &RuntimeConfigEntry{
		Section:   dbEntry.Section,
		Key:       dbEntry.Key,
		Value:     dbEntry.Value,
		Version:   int(dbEntry.Version),
		UpdatedBy: dbEntry.UpdatedBy,
	}, nil
}

// UpsertRuntimeConfig inserts or updates a runtime_config entry.  When the
// entry already exists the version is bumped and updated_at refreshed.
func (s *SQLiteStore) UpsertRuntimeConfig(section, key, value, updatedBy string) error {
	ctx := context.Background()
	err := s.queries.UpsertConfig(ctx, sqlc.UpsertConfigParams{
		Section:   section,
		Key:       key,
		Value:     value,
		UpdatedBy: &updatedBy,
	})
	if err != nil {
		return fmt.Errorf("runtime_config: upsert %s/%s: %w", section, key, err)
	}
	return nil
}

// DeleteRuntimeConfig removes a runtime_config entry.  It is not an error if
// the entry does not exist.
func (s *SQLiteStore) DeleteRuntimeConfig(section, key string) error {
	ctx := context.Background()
	err := s.queries.DeleteConfig(ctx, sqlc.DeleteConfigParams{Section: section, Key: key})
	if err != nil {
		return fmt.Errorf("runtime_config: delete %s/%s: %w", section, key, err)
	}
	return nil
}
