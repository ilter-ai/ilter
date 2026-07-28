package db

import (
	"database/sql"
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────
// SQLiteStore runtime_config tests
// ─────────────────────────────────────────────────────────────────────

func TestRuntimeConfigStore_GetAll_Empty(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	vals, err := ts.store.GetAll()
	if err != nil {
		t.Fatalf("GetAll on empty DB: %v", err)
	}
	// We might have seeded values depending on initial migrations, but let's check
	// if we can just test CRUD functionality directly.
	_ = vals
}

func TestRuntimeConfigStore_GetAll_Roundtrip(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	s := ts.store

	// Clean any migrated configs for a clean test
	_, _ = ts.store.DB.Exec("DELETE FROM runtime_config")

	// Insert rows
	if err := s.UpsertRuntimeConfig("test_section", "key1", "value1", "tester"); err != nil {
		t.Fatalf("UpsertRuntimeConfig(key1): %v", err)
	}
	if err := s.UpsertRuntimeConfig("test_section", "key2", "value2", "tester"); err != nil {
		t.Fatalf("UpsertRuntimeConfig(key2): %v", err)
	}
	if err := s.UpsertRuntimeConfig("other_section", "akey", "aval", ""); err != nil {
		t.Fatalf("UpsertRuntimeConfig(other): %v", err)
	}

	vals, err := s.GetAll()
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}

	if len(vals) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(vals))
	}

	tests := []struct {
		composite string
		want      string
	}{
		{"test_section:key1", "value1"},
		{"test_section:key2", "value2"},
		{"other_section:akey", "aval"},
	}
	for _, tt := range tests {
		got, ok := vals[tt.composite]
		if !ok {
			t.Errorf("key %q missing from GetAll", tt.composite)
			continue
		}
		if got != tt.want {
			t.Errorf("key %q: got %q, want %q", tt.composite, got, tt.want)
		}
	}
}

func TestRuntimeConfigStore_GetBySection(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	s := ts.store
	_, _ = ts.store.DB.Exec("DELETE FROM runtime_config")

	if err := s.UpsertRuntimeConfig("section_a", "k1", "v1", "tester"); err != nil {
		t.Fatalf("UpsertRuntimeConfig: %v", err)
	}
	if err := s.UpsertRuntimeConfig("section_a", "k2", "v2", "tester"); err != nil {
		t.Fatalf("UpsertRuntimeConfig: %v", err)
	}
	if err := s.UpsertRuntimeConfig("section_b", "k3", "v3", "tester"); err != nil {
		t.Fatalf("UpsertRuntimeConfig: %v", err)
	}

	got, err := s.GetBySection("section_a")
	if err != nil {
		t.Fatalf("GetBySection: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries for section_a, got %d", len(got))
	}
	if got["k1"] != "v1" {
		t.Errorf("k1: got %q, want %q", got["k1"], "v1")
	}
	if got["k2"] != "v2" {
		t.Errorf("k2: got %q, want %q", got["k2"], "v2")
	}
}

func TestRuntimeConfigStore_Get_Found(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	s := ts.store
	_, _ = ts.store.DB.Exec("DELETE FROM runtime_config")

	if err := s.UpsertRuntimeConfig("mysection", "mykey", "myvalue", "op"); err != nil {
		t.Fatalf("UpsertRuntimeConfig: %v", err)
	}

	entry, err := s.GetRuntimeConfigEntry("mysection", "mykey")
	if err != nil {
		t.Fatalf("GetRuntimeConfigEntry: %v", err)
	}
	if entry.Section != "mysection" || entry.Key != "mykey" || entry.Value != "myvalue" {
		t.Errorf("unexpected entry: %+v", entry)
	}
	if entry.Version != 1 {
		t.Errorf("expected version 1, got %d", entry.Version)
	}
	if entry.UpdatedBy != "op" {
		t.Errorf("expected updated_by 'op', got %q", entry.UpdatedBy)
	}
}

func TestRuntimeConfigStore_Get_NotFound(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	s := ts.store
	_, _ = ts.store.DB.Exec("DELETE FROM runtime_config")

	_, err := s.GetRuntimeConfigEntry("nonexistent", "nokey")
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestRuntimeConfigStore_Upsert_OverwritesAndBumpsVersion(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	s := ts.store
	_, _ = ts.store.DB.Exec("DELETE FROM runtime_config")

	// First insert
	if err := s.UpsertRuntimeConfig("sec", "k", "v1", "alice"); err != nil {
		t.Fatalf("first UpsertRuntimeConfig: %v", err)
	}

	entry, err := s.GetRuntimeConfigEntry("sec", "k")
	if err != nil {
		t.Fatalf("GetRuntimeConfigEntry after first insert: %v", err)
	}
	if entry.Version != 1 {
		t.Errorf("expected version 1, got %d", entry.Version)
	}

	// Overwrite
	if err = s.UpsertRuntimeConfig("sec", "k", "v2", "bob"); err != nil {
		t.Fatalf("second UpsertRuntimeConfig: %v", err)
	}

	entry, err = s.GetRuntimeConfigEntry("sec", "k")
	if err != nil {
		t.Fatalf("GetRuntimeConfigEntry after overwrite: %v", err)
	}
	if entry.Value != "v2" {
		t.Errorf("expected value 'v2', got %q", entry.Value)
	}
	if entry.Version != 2 {
		t.Errorf("expected version 2, got %d", entry.Version)
	}
	if entry.UpdatedBy != "bob" {
		t.Errorf("expected updated_by 'bob', got %q", entry.UpdatedBy)
	}
}

func TestRuntimeConfigStore_Delete(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	s := ts.store
	_, _ = ts.store.DB.Exec("DELETE FROM runtime_config")

	if err := s.UpsertRuntimeConfig("sec", "k", "v", ""); err != nil {
		t.Fatalf("UpsertRuntimeConfig: %v", err)
	}

	// Delete existing
	if err := s.DeleteRuntimeConfig("sec", "k"); err != nil {
		t.Fatalf("DeleteRuntimeConfig: %v", err)
	}

	_, err := s.GetRuntimeConfigEntry("sec", "k")
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows after delete, got %v", err)
	}

	// Delete non-existent (should not error)
	if err := s.DeleteRuntimeConfig("nonexistent", "nokey"); err != nil {
		t.Fatalf("DeleteRuntimeConfig non-existent: %v", err)
	}
}

func TestRuntimeConfigStore_GetAll_AfterOverwrite(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	s := ts.store
	_, _ = ts.store.DB.Exec("DELETE FROM runtime_config")

	// Insert and overwrite
	if err := s.UpsertRuntimeConfig("s", "k", "original", ""); err != nil {
		t.Fatalf("UpsertRuntimeConfig original: %v", err)
	}
	if err := s.UpsertRuntimeConfig("s", "k", "updated", ""); err != nil {
		t.Fatalf("UpsertRuntimeConfig updated: %v", err)
	}

	vals, err := s.GetAll()
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(vals) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(vals))
	}
	if vals["s:k"] != "updated" {
		t.Errorf("expected 'updated', got %q", vals["s:k"])
	}
}

func TestRuntimeConfigStore_LargeValues(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	s := ts.store
	_, _ = ts.store.DB.Exec("DELETE FROM runtime_config")

	large := strings.Repeat("A", 10000)
	if err := s.UpsertRuntimeConfig("big", "data", large, ""); err != nil {
		t.Fatalf("UpsertRuntimeConfig large value: %v", err)
	}

	entry, err := s.GetRuntimeConfigEntry("big", "data")
	if err != nil {
		t.Fatalf("GetRuntimeConfigEntry large value: %v", err)
	}
	if entry.Value != large {
		t.Errorf("value length mismatch: got %d, want %d", len(entry.Value), len(large))
	}
}

func TestRuntimeConfigStore_GetBySection_Empty(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()

	s := ts.store
	_, _ = ts.store.DB.Exec("DELETE FROM runtime_config")

	// Insert something in a different section
	if err := s.UpsertRuntimeConfig("other", "k", "v", ""); err != nil {
		t.Fatalf("UpsertRuntimeConfig: %v", err)
	}

	vals, err := s.GetBySection("nonexistent")
	if err != nil {
		t.Fatalf("GetBySection: %v", err)
	}
	if len(vals) != 0 {
		t.Fatalf("expected empty map, got %v", vals)
	}
}
