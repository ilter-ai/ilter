package demo

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"log/slog"
	"math/rand"
	"time"

	dbpkg "github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/db/seed"
)

// sqliteTS formats a backdated timestamp the same way SQLite's own
// CURRENT_TIMESTAMP/datetime('now') do ("YYYY-MM-DD HH:MM:SS", UTC).
func sqliteTS(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05")
}

// RunDemoSeed populates the database with deterministic mock data for
// dashboard development. Called by `ilter init --demo`.
func RunDemoSeed(db *sql.DB) error {
	if err := dbpkg.ApplyMigrations(db); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	//nolint:gosec // G404: fixed seed (42) for reproducible demo/mock data, not security-relevant
	rng := rand.New(rand.NewSource(42))
	today := time.Now().UTC().Truncate(24 * time.Hour)

	if err := seedCoreEntities(db); err != nil {
		return err
	}
	if err := seedConfigAndCatalog(db); err != nil {
		return err
	}
	if err := seedDashboardDemoData(db, rng, today); err != nil {
		return err
	}

	// ── Smart router strategies ──
	if err := seedSmartRouterStrategies(db); err != nil {
		return fmt.Errorf("smart router strategies: %w", err)
	}
	fmt.Println("  ✓ smart router strategies")

	fmt.Println("\nDemo seed inserted successfully!")
	fmt.Println("Use `Authorization: Bearer test` for all proxy API calls.")
	return nil
}

// seedCoreEntities seeds the "test" API key, groups, users, and their
// memberships — the base entities the rest of the demo data references.
func seedCoreEntities(db *sql.DB) error {
	// ── Single API key: "test" (plaintext) ──
	testHash := fmt.Sprintf("%x", sha256.Sum256([]byte("test")))
	_, err := db.Exec(
		`INSERT INTO api_keys (id, name, hashed_key, salt, key_prefix,
		 monthly_budget_usd, monthly_budget_tokens,
		 rate_limit_rpm, rate_limit_tpm,
		 allowed_models, allowed_providers, enabled)
		 VALUES (?, ?, ?, ?, ?,
		 ?, 0,
		 ?, 100000,
		 '["*"]', '["*"]', 1)
		 ON CONFLICT(id) DO NOTHING`,
		"test", "test", testHash, "sha256", "test",
		100.0, 200,
	)
	if err != nil {
		return fmt.Errorf("test api_key: %w", err)
	}
	fmt.Println("  ✓ api_key: test")

	// ── Groups ──
	groupIDs, err := seedGroups(db)
	if err != nil {
		return fmt.Errorf("groups: %w", err)
	}
	fmt.Println("  ✓ groups")

	// ── Users ──
	userIDs, err := seedUsers(db)
	if err != nil {
		return fmt.Errorf("users: %w", err)
	}
	fmt.Println("  ✓ users")

	// ── Memberships ──
	if err := seedUserGroupMemberships(db, userIDs, groupIDs); err != nil {
		return fmt.Errorf("user_group_memberships: %w", err)
	}
	fmt.Println("  ✓ user_group_memberships")

	return nil
}

// seedConfigAndCatalog seeds prompt templates, jobs, model/provider catalog
// data, MCP servers/grants, guardrail rules, feature flags, and PII patterns.
func seedConfigAndCatalog(db *sql.DB) error {
	// ── Prompt templates ──
	if err := seedPromptTemplates(db); err != nil {
		return fmt.Errorf("prompt_templates: %w", err)
	}
	fmt.Println("  ✓ prompt_templates")

	// ── Jobs ──
	if err := seedJobs(db); err != nil {
		return fmt.Errorf("jobs: %w", err)
	}
	fmt.Println("  ✓ jobs")

	// ── Model enable/disable configs ──
	if err := seedDefaultModelConfigs(db); err != nil {
		return err
	}
	fmt.Println("  ✓ model_configs")

	// ── MCP servers + grants ──
	if err := seedMCPServers(db); err != nil {
		return fmt.Errorf("mcp_servers: %w", err)
	}
	fmt.Println("  ✓ mcp_servers")

	// ── Built-in Guardrail Rules ──
	if err := seed.BuiltinGuardrailRules(db); err != nil {
		return fmt.Errorf("builtin guardrail rules: %w", err)
	}
	fmt.Println("  ✓ guardrail_rules (builtin)")

	if err := clearAndSeed(db, "mcp_grant", func() error {
		return seedMCPGrants(db)
	}); err != nil {
		return fmt.Errorf("mcp_grant: %w", err)
	}
	fmt.Println("  ✓ mcp_grant")

	// ── Opencode Zen models for provider_models table ──
	if err := seedProviderModels(db); err != nil {
		return fmt.Errorf("provider_models: %w", err)
	}
	fmt.Println("  ✓ provider_models (opencode_zen)")

	// ── OpenAPI specs ──
	if err := seedOpenAPISpecs(db); err != nil {
		return fmt.Errorf("openapi_specs: %w", err)
	}
	fmt.Println("  ✓ openapi_specs")

	// ── Feature flags: all enabled for demo/test exploration ──
	if err := seedFeatureFlags(db); err != nil {
		return fmt.Errorf("feature_flags: %w", err)
	}
	fmt.Println("  ✓ feature_flags (all enabled)")

	// ── PII patterns ──
	if err := seedPIIPatterns(db); err != nil {
		return fmt.Errorf("pii_patterns: %w", err)
	}
	fmt.Println("  ✓ pii_patterns")

	return nil
}

// seedDefaultModelConfigs enables a fixed set of free-tier models by default
// so the demo dashboard has usable models out of the box.
func seedDefaultModelConfigs(db *sql.DB) error {
	for _, m := range []string{
		"big-pickle", "deepseek-v4-flash-free",
		"mimo-v2.6-flash-free", "mimo-v2.5-free",
		"nemotron-3-ultra-free", "nemotron-3.5-lightning-free",
	} {
		if _, err := db.Exec(
			`INSERT INTO model_configs (name, active) VALUES (?, 1)
			 ON CONFLICT(name) DO NOTHING`, m,
		); err != nil {
			return fmt.Errorf("model_config %s: %w", m, err)
		}
	}
	return nil
}

// seedDashboardDemoData seeds the time-series demo data (audit log, usage,
// loop/PII/guardrail events, MCP audit log) that populates the dashboard.
func seedDashboardDemoData(db *sql.DB, rng *rand.Rand, today time.Time) error {
	// ── Dashboard demo data ──
	if err := clearAndSeed(db, "audit_log", func() error {
		return seedAuditLog(db, rng, []string{"test"}, today)
	}); err != nil {
		return fmt.Errorf("audit_log: %w", err)
	}
	fmt.Println("  ✓ audit_log")

	if err := seedUsageDaily(db, rng, []string{"test"}, today); err != nil {
		return fmt.Errorf("usage_daily: %w", err)
	}
	fmt.Println("  ✓ usage_daily")

	if err := clearAndSeed(db, "loop_events", func() error {
		return seedLoopEvents(db, rng, []string{"test"}, today)
	}); err != nil {
		return fmt.Errorf("loop_events: %w", err)
	}
	fmt.Println("  ✓ loop_events")

	if err := clearAndSeed(db, "pii_events", func() error {
		return seedPIIEvents(db, rng, []string{"test"}, today)
	}); err != nil {
		return fmt.Errorf("pii_events: %w", err)
	}
	fmt.Println("  ✓ pii_events")

	if err := clearAndSeed(db, "guardrail_events", func() error {
		return seedGuardrailEvents(db, rng, []string{"test"}, today)
	}); err != nil {
		return fmt.Errorf("guardrail_events: %w", err)
	}
	fmt.Println("  ✓ guardrail_events")

	if err := clearAndSeed(db, "mcp_audit_log", func() error {
		return seedMCPAuditLog(db, rng, []string{"test"}, today)
	}); err != nil {
		return fmt.Errorf("mcp_audit_log: %w", err)
	}
	fmt.Println("  ✓ mcp_audit_log")

	return nil
}

// clearAndSeed deletes all rows from table, resets its auto-increment
// sequence (if any), then runs the seedFn to insert fresh data.
func clearAndSeed(db *sql.DB, table string, seedFn func() error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	//nolint:gosec // G202: table is always one of this file's hardcoded literals (see call sites above), never user input
	if _, err := tx.Exec("DELETE FROM " + table); err != nil {
		return fmt.Errorf("delete %s: %w", table, err)
	}
	if _, err := tx.Exec("DELETE FROM sqlite_sequence WHERE name = ?", table); err != nil {
		slog.Warn("sqlite_sequence may not exist yet", "table", table, "error", err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return seedFn()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
