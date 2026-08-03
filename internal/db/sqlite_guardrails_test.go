package db

import (
	"testing"
)

func TestGuardrailRules_CreateListToggleUpdateDelete(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()
	s := ts.store

	err := s.CreateGuardrailRule(CreateGuardrailRuleParams{
		ID:          "rule-1",
		Name:        "Block SSNs",
		Description: "blocks US social security numbers",
		Patterns:    `["\\d{3}-\\d{2}-\\d{4}"]`,
		Mode:        "block",
		Severity:    "high",
		TargetType:  "global",
		Type:        "pii",
	})
	if err != nil {
		t.Fatalf("CreateGuardrailRule: %v", err)
	}

	// Duplicate ID must fail (primary key).
	err = s.CreateGuardrailRule(CreateGuardrailRuleParams{
		ID: "rule-1", Name: "dup", Patterns: "[]", Mode: "block", Severity: "low", TargetType: "global", Type: "custom",
	})
	if err == nil {
		t.Fatal("expected error creating duplicate rule ID")
	}

	rules, err := s.ListGuardrailRules()
	if err != nil {
		t.Fatalf("ListGuardrailRules: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].ID != "rule-1" || rules[0].Name != "Block SSNs" || !rules[0].Enabled {
		t.Errorf("unexpected rule: %+v", rules[0])
	}

	enabled, err := s.GetEnabledGuardrailRules()
	if err != nil {
		t.Fatalf("GetEnabledGuardrailRules: %v", err)
	}
	if len(enabled) != 1 || enabled[0].ID != "rule-1" {
		t.Fatalf("expected rule-1 in enabled rules, got %+v", enabled)
	}

	found, err := s.ToggleGuardrailRule("rule-1", false)
	if err != nil {
		t.Fatalf("ToggleGuardrailRule: %v", err)
	}
	if !found {
		t.Fatal("expected ToggleGuardrailRule to find rule-1")
	}
	enabled, err = s.GetEnabledGuardrailRules()
	if err != nil {
		t.Fatalf("GetEnabledGuardrailRules after disable: %v", err)
	}
	if len(enabled) != 0 {
		t.Fatalf("expected 0 enabled rules after disable, got %d", len(enabled))
	}

	found, err = s.ToggleGuardrailRule("no-such-rule", true)
	if err != nil {
		t.Fatalf("ToggleGuardrailRule nonexistent: %v", err)
	}
	if found {
		t.Fatal("expected ToggleGuardrailRule to report not-found for missing id")
	}

	newTargetID := 42
	found, err = s.UpdateGuardrailRule(UpdateGuardrailRuleParams{
		ID:         "rule-1",
		Name:       "Block SSNs v2",
		TargetType: new("user"),
		TargetID:   &newTargetID,
	})
	if err != nil {
		t.Fatalf("UpdateGuardrailRule: %v", err)
	}
	if !found {
		t.Fatal("expected UpdateGuardrailRule to find rule-1")
	}

	rules, err = s.ListGuardrailRules()
	if err != nil {
		t.Fatalf("ListGuardrailRules after update: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	got := rules[0]
	if got.Name != "Block SSNs v2" {
		t.Errorf("expected updated name, got %q", got.Name)
	}
	if got.Severity != "high" {
		t.Errorf("expected severity unchanged ('high'), got %q", got.Severity)
	}
	if got.TargetType != "user" || got.TargetID == nil || *got.TargetID != 42 {
		t.Errorf("expected target_type=user target_id=42, got type=%q id=%v", got.TargetType, got.TargetID)
	}
	if got.Enabled {
		t.Errorf("expected enabled to remain false (untouched by update), got true")
	}

	deleted, err := s.DeleteGuardrailRule("rule-1")
	if err != nil {
		t.Fatalf("DeleteGuardrailRule: %v", err)
	}
	if !deleted {
		t.Fatal("expected DeleteGuardrailRule to find rule-1")
	}

	deleted, err = s.DeleteGuardrailRule("rule-1")
	if err != nil {
		t.Fatalf("DeleteGuardrailRule second time: %v", err)
	}
	if deleted {
		t.Fatal("expected DeleteGuardrailRule to report not-found the second time")
	}

	rules, err = s.ListGuardrailRules()
	if err != nil {
		t.Fatalf("ListGuardrailRules after delete: %v", err)
	}
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules after delete, got %d", len(rules))
	}
}

func TestGuardrailEvents_InsertAndProviderLookup(t *testing.T) {
	ts := setupTestStore(t)
	defer ts.close()
	s := ts.store

	if err := s.InsertGuardrailEvent("key-1", "pii", "blocked", "gpt-4o", "openai", "matched SSN"); err != nil {
		t.Fatalf("InsertGuardrailEvent: %v", err)
	}

	var count int
	if err := ts.store.DB.QueryRow("SELECT COUNT(*) FROM guardrail_events WHERE key_id = ?", "key-1").Scan(&count); err != nil {
		t.Fatalf("query guardrail_events: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 guardrail_events row, got %d", count)
	}

	if err := s.SaveDiscoveredModels("openai", nil); err != nil {
		t.Fatalf("SaveDiscoveredModels: %v", err)
	}
	if _, err := ts.store.DB.Exec(
		`INSERT INTO provider_models (provider, model, active, tier, cost_in, cost_out) VALUES (?, ?, 1, 'standard', 0, 0)`,
		"openai", "gpt-4o",
	); err != nil {
		t.Fatalf("seed provider_models: %v", err)
	}

	provider, err := s.GetProviderForModel("gpt-4o")
	if err != nil {
		t.Fatalf("GetProviderForModel: %v", err)
	}
	if provider != "openai" {
		t.Errorf("expected provider 'openai', got %q", provider)
	}

	if _, err := s.GetProviderForModel("no-such-model"); err == nil {
		t.Fatal("expected error (sql.ErrNoRows) for unknown model")
	}
}
