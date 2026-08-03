package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"

	"github.com/ilter-ai/ilter/internal/app/cli"
	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/db/seed"
	"github.com/ilter-ai/ilter/internal/db/seed/demo"
	"github.com/ilter-ai/ilter/internal/features/pii"
)

// newInitCmd builds the `ilter init` subcommand.
//
// Without flags it runs the interactive huh wizard and calls seed.ApplySeedData to
// populate the runtime_config table. With --demo it populates deterministic mock
// data for dashboard development (used by `make dev`).
func newInitCmd() *cobra.Command {
	var demoFlag bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize runtime configuration for ILTER",
		Long: `Seeds the runtime_config table with providers, routing strategy,
and feature flags.

Without flags: interactive wizard for production setup.
With --demo: populate deterministic mock data for dashboard development.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			defer config.WarnUnknownEnv()()
			if demoFlag {
				return runDemoInit()
			}
			return runInitProd()
		},
	}

	cmd.Flags().BoolVarP(&demoFlag, "demo", "d", false, "Populate deterministic mock data for dashboard development")

	return cmd
}

func openDB() (*sql.DB, string, func(), error) {
	base := config.DefaultConfig()
	config.ApplyEnvOverrides(&base)
	sqlitePath := base.Storage.SqlitePath
	if !filepath.IsAbs(sqlitePath) {
		wd, _ := os.Getwd()
		sqlitePath = filepath.Join(wd, sqlitePath)
	}
	if err := os.MkdirAll(filepath.Dir(sqlitePath), 0o755); err != nil {
		return nil, "", nil, fmt.Errorf("create DB directory: %w", err)
	}
	// _time_format=datetime&_timezone=UTC: see internal/db/sqlite.go for why this
	// is required (without it, bound time.Time params write in local wall-clock
	// digits with no zone info, and SQLite's date functions silently misread them
	// as UTC — WHERE clauses comparing such columns never match correctly).
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)&_time_format=datetime&_timezone=UTC",
		sqlitePath,
	)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, "", nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, "", nil, fmt.Errorf("ping database: %w", err)
	}
	return db, sqlitePath, func() { db.Close() }, nil
}

func runInitProd() error {
	rawDB, sqlitePath, cleanup, err := openDB()
	if err != nil {
		return err
	}
	defer cleanup()

	fmt.Println("ILTER initialization wizard")
	fmt.Println()

	dashboardPort, metricsPort := 9191, 9192
	_ = rawDB.QueryRow("SELECT value FROM runtime_config WHERE section='dashboard' AND key='port'").Scan(&dashboardPort)
	_ = rawDB.QueryRow("SELECT value FROM runtime_config WHERE section='metrics' AND key='port'").Scan(&metricsPort)

	seedData, err := cli.RunInitWizard(dashboardPort, metricsPort)
	if err != nil {
		return fmt.Errorf("wizard: %w", err)
	}
	if err = seed.ApplySeedData(rawDB, seedData); err != nil {
		return fmt.Errorf("seed apply: %w", err)
	}
	slog.Info("seed applied successfully")
	fmt.Println()
	fmt.Println("Seed applied successfully!")
	fmt.Println()

	// ── Admin account (idempotent: skipped if the admin group already exists) ──
	store := db.NewSQLiteStoreFromDB(rawDB)
	adminEmail, adminPassword, adminAPIKey, adminCreated, err := seed.EnsureAdminAccount(store)
	if err != nil {
		return fmt.Errorf("seed admin account: %w", err)
	}
	if adminCreated {
		credPath := filepath.Join(filepath.Dir(sqlitePath), "admin_credentials.txt")
		credContents := fmt.Sprintf(
			"ILTER admin credentials — generated %s\nStore this somewhere safe and delete this file.\n\n"+
				"Dashboard login at /login — either works:\n"+
				"  \"Email & Password\" tab:\n    email:    %s\n    password: %s\n"+
				"  \"API Key\" tab:\n    api_key:  %s\n\n"+
				"The api_key also works for LLM proxy calls: Authorization: Bearer <key> against /v1/*\n",
			time.Now().UTC().Format(time.RFC3339), adminEmail, adminPassword, adminAPIKey,
		)
		if err := os.WriteFile(credPath, []byte(credContents), 0o600); err != nil {
			return fmt.Errorf("write admin credentials file: %w", err)
		}
		fmt.Println("Admin account created:")
		fmt.Println()
		fmt.Println("  Dashboard login at /login — either works:")
		fmt.Println("    \"Email & Password\" tab:")
		fmt.Println("      email:    " + adminEmail)
		fmt.Println("      password: " + adminPassword)
		fmt.Println("    \"API Key\" tab:")
		fmt.Println("      api_key:  " + adminAPIKey)
		fmt.Println()
		fmt.Println("  The api_key also works for LLM proxy calls: Authorization: Bearer <key> against /v1/*")
		fmt.Println()
		fmt.Println("Credentials written to:", credPath)
		fmt.Println()
	} else {
		fmt.Println("Admin account already exists, skipping.")
		fmt.Println()
	}

	// ── Optional demo data ──
	demoData := false
	if err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Add Demo Data?").
				Description("Populate with mock data for development: test API key (\"test\"), sample jobs, audit logs, groups, users, MCP servers, and dashboard charts").
				Value(&demoData),
		),
	).Run(); err != nil {
		return fmt.Errorf("demo data prompt: %w", err)
	}

	if demoData {
		fmt.Println("\nSeeding demo data...")
		if err := demo.RunDemoSeed(rawDB); err != nil {
			return fmt.Errorf("demo seed: %w", err)
		}
	}

	fmt.Println("Run 'ilter serve' to start the proxy.")

	// Create the table if it doesn't exist (init opens a raw DB, no schema migrations).
	if _, err := rawDB.Exec(`CREATE TABLE IF NOT EXISTS pii_patterns (
		name       TEXT PRIMARY KEY,
		regex      TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT (datetime('now')),
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return fmt.Errorf("create pii_patterns table: %w", err)
	}
	for _, p := range pii.DefaultPIIPatterns {
		if _, err := rawDB.Exec(
			`INSERT OR IGNORE INTO pii_patterns (name, regex) VALUES (?, ?)`,
			p.Name, p.Regex,
		); err != nil {
			return fmt.Errorf("seed pii_pattern %q: %w", p.Name, err)
		}
	}
	slog.Info("seeded default PII patterns", "count", len(pii.DefaultPIIPatterns))

	return nil
}

// runDemoInit opens the database (running all schema migrations), applies the
// default seed (opencode_zen/opencode_go providers, routing, features, guardrails),
// then populates deterministic mock data for dashboard development.
// Called by `ilter init --demo`.
func runDemoInit() error {
	base := config.DefaultConfig()
	config.ApplyEnvOverrides(&base)

	store, err := db.NewSQLiteStore(base.Storage)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer store.Close()

	// Apply default seed first (providers, routing, features, guardrails)
	if err := seed.ApplySeedData(store.DB, seed.DefaultSeedFile()); err != nil {
		return fmt.Errorf("default seed: %w", err)
	}
	fmt.Println("✓ default seed applied (opencode_zen, opencode_go, routing, features, guardrails)")

	// Then demo test data on top
	return demo.RunDemoSeed(store.DB)
}
