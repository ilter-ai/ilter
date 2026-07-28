package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ilter-ai/ilter/internal/db/sqlc"
)

// LoadReferenceVectors loads reference embedding vectors from the
// runtime_config table (section="reference_vector"). Each key is a
// tier name ("economy", "standard", "premium") and the value is a
// JSON-encoded []float32.
//
// Returns sql.ErrNoRows-wrapped errors from the underlying query so
// callers can distinguish "no data" from real errors.
func (s *SQLiteStore) LoadReferenceVectors(ctx context.Context) (map[string][]float32, error) {
	rows, err := s.queries.GetConfigSection(ctx, "reference_vector")
	if err != nil {
		return nil, fmt.Errorf("reference_vector: query section: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	result := make(map[string][]float32, len(rows))
	for _, row := range rows {
		var vec []float32
		if err := json.Unmarshal([]byte(row.Value), &vec); err != nil {
			return nil, fmt.Errorf("reference_vector: decode %q: %w", row.Key, err)
		}
		result[row.Key] = vec
	}
	return result, nil
}

// UpsertReferenceVector stores a single reference vector for a tier.
// The value is JSON-encoded automatically.
func (s *SQLiteStore) UpsertReferenceVector(ctx context.Context, tier string, vec []float32, updatedBy string) error {
	data, err := json.Marshal(vec)
	if err != nil {
		return fmt.Errorf("reference_vector: encode %q: %w", tier, err)
	}

	err = s.queries.UpsertConfig(ctx, sqlc.UpsertConfigParams{
		Section:   "reference_vector",
		Key:       tier,
		Value:     string(data),
		UpdatedBy: &updatedBy,
	})
	if err != nil {
		return fmt.Errorf("reference_vector: upsert %q: %w", tier, err)
	}
	return nil
}
