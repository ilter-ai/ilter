package triggers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// Store provides SQLite CRUD operations on the triggers table.
// The table is created by migration V11 (000011_add_triggers_table.up.sql).
type Store struct {
	db *sql.DB
}

// NewStore creates a new Store backed by the given *sql.DB.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

// DB returns the underlying *sql.DB. Used when the store needs to be used
// within a larger transaction or passed to other store constructors.
func (s *Store) DB() *sql.DB {
	return s.db
}

// Create inserts a new trigger row.
func (s *Store) Create(ctx context.Context, tr TriggerRow) error {
	configJSON, err := json.Marshal(tr.Config)
	if err != nil {
		return fmt.Errorf("marshal trigger config: %w", err)
	}
	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO triggers (id, job_id, kind, enabled, config, token, secret_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))`,
		tr.ID, tr.JobID, tr.Kind, boolToInt(tr.Enabled), string(configJSON),
		strPtrOrNil(tr.Token), strPtrOrNil(tr.Secret),
	)
	if err != nil {
		return fmt.Errorf("create trigger %s: %w", tr.ID, err)
	}
	return nil
}

// Get retrieves a trigger by its ID. Returns (nil, nil) if not found.
func (s *Store) Get(id string) (*TriggerRow, error) {
	var t TriggerRow
	var enabled int
	var token, tokenHash sql.NullString
	var lastUsedAt sql.NullTime
	var configStr string
	err := s.db.QueryRow(
		`SELECT id, job_id, kind, enabled, config, token, secret_hash, last_used_at, created_at, updated_at
		 FROM triggers WHERE id = ?`, id,
	).Scan(&t.ID, &t.JobID, &t.Kind, &enabled, &configStr, &token, &tokenHash, &lastUsedAt, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get trigger %s: %w", id, err)
	}
	t.Enabled = enabled != 0
	if token.Valid {
		t.Token = token.String
	}
	if tokenHash.Valid {
		t.Secret = tokenHash.String
	}
	if lastUsedAt.Valid {
		t.LastUsedAt = &lastUsedAt.Time
	}
	if err := json.Unmarshal([]byte(configStr), &t.Config); err != nil {
		return nil, fmt.Errorf("unmarshal trigger config: %w", err)
	}
	return &t, nil
}

// ListByJobID returns all triggers associated with the given job ID.
func (s *Store) ListByJobID(jobID string) ([]TriggerRow, error) {
	rows, err := s.db.Query(
		`SELECT id, job_id, kind, enabled, config, token, secret_hash, last_used_at, created_at, updated_at
		 FROM triggers WHERE job_id = ?`, jobID,
	)
	if err != nil {
		return nil, fmt.Errorf("list triggers by job %s: %w", jobID, err)
	}
	defer rows.Close()
	return scanTriggers(rows)
}

// ListEnabled returns all triggers where enabled = 1.
func (s *Store) ListEnabled() ([]TriggerRow, error) {
	// Join against jobs.enabled: a trigger's own enabled flag never implies its
	// parent job is enabled, and a disabled job's triggers must not be
	// scheduled — Dispatch() also enforces this per-activation, but filtering
	// here avoids registering (and repeatedly failing to fire) dead cron
	// entries for jobs the user has turned off.
	rows, err := s.db.Query(
		`SELECT t.id, t.job_id, t.kind, t.enabled, t.config, t.token, t.secret_hash, t.last_used_at, t.created_at, t.updated_at
		 FROM triggers t JOIN jobs j ON j.id = t.job_id
		 WHERE t.enabled = 1 AND j.enabled = 1`,
	)
	if err != nil {
		return nil, fmt.Errorf("list enabled triggers: %w", err)
	}
	defer rows.Close()
	return scanTriggers(rows)
}

// Update modifies an existing trigger row. Returns an error if the trigger
// is not found.
func (s *Store) Update(ctx context.Context, tr TriggerRow) error {
	configJSON, err := json.Marshal(tr.Config)
	if err != nil {
		return fmt.Errorf("marshal trigger config: %w", err)
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE triggers SET job_id=?, kind=?, enabled=?, config=?, token=?, secret_hash=?,
		 updated_at=datetime('now') WHERE id=?`,
		tr.JobID, tr.Kind, boolToInt(tr.Enabled), string(configJSON),
		strPtrOrNil(tr.Token), strPtrOrNil(tr.Secret), tr.ID)
	if err != nil {
		return fmt.Errorf("update trigger %s: %w", tr.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("trigger %s not found", tr.ID)
	}
	return nil
}

// Delete removes a trigger by ID. Returns an error if the trigger is not found.
func (s *Store) Delete(id string) error {
	res, err := s.db.Exec("DELETE FROM triggers WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete trigger %s: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("trigger %s not found", id)
	}
	return nil
}

// GetByTokenHash looks up an enabled trigger by its webhook token.
// This is used for webhook endpoint routing: POST /api/webhooks/<token>.
func (s *Store) GetByTokenHash(hash string) (*TriggerRow, error) {
	var t TriggerRow
	var enabled int
	var token, tokenHash sql.NullString
	var lastUsedAt sql.NullTime
	var configStr string
	err := s.db.QueryRow(
		`SELECT id, job_id, kind, enabled, config, token, secret_hash, last_used_at, created_at, updated_at
		 FROM triggers WHERE token = ? AND enabled = 1`, hash,
	).Scan(&t.ID, &t.JobID, &t.Kind, &enabled, &configStr, &token, &tokenHash, &lastUsedAt, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get trigger by token: %w", err)
	}
	t.Enabled = enabled != 0
	if token.Valid {
		t.Token = token.String
	}
	if tokenHash.Valid {
		t.Secret = tokenHash.String
	}
	if lastUsedAt.Valid {
		t.LastUsedAt = &lastUsedAt.Time
	}
	if err := json.Unmarshal([]byte(configStr), &t.Config); err != nil {
		return nil, fmt.Errorf("unmarshal trigger config: %w", err)
	}
	return &t, nil
}

// UpdateLastUsed sets last_used_at to the current timestamp for the given trigger.
func (s *Store) UpdateLastUsed(id string) error {
	_, err := s.db.Exec("UPDATE triggers SET last_used_at = datetime('now') WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("update last_used_at for trigger %s: %w", id, err)
	}
	return nil
}

// scanTriggers scans all rows from a triggers query into a slice of TriggerRow.
func scanTriggers(rows *sql.Rows) ([]TriggerRow, error) {
	triggers := make([]TriggerRow, 0)
	for rows.Next() {
		var t TriggerRow
		var enabled int
		var token, tokenHash sql.NullString
		var lastUsedAt sql.NullTime
		var configStr string
		if err := rows.Scan(
			&t.ID, &t.JobID, &t.Kind, &enabled, &configStr,
			&token, &tokenHash, &lastUsedAt, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan trigger row: %w", err)
		}
		t.Enabled = enabled != 0
		if token.Valid {
			t.Token = token.String
		}
		if tokenHash.Valid {
			t.Secret = tokenHash.String
		}
		if lastUsedAt.Valid {
			t.LastUsedAt = &lastUsedAt.Time
		}
		if err := json.Unmarshal([]byte(configStr), &t.Config); err != nil {
			return nil, fmt.Errorf("unmarshal trigger config: %w", err)
		}
		triggers = append(triggers, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration: %w", err)
	}
	return triggers, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// strPtrOrNil returns nil for empty strings, the string value otherwise.
// Used to store NULL in SQLite for empty token/secret_hash columns.
func strPtrOrNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}
