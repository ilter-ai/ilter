package demo

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"

	dbpkg "github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/db/seed"
	"github.com/ilter-ai/ilter/internal/features/pii"
)

// ── Static data seed functions ──

// seedFeatureFlags turns every feature toggle on via runtime_config.
func seedFeatureFlags(db *sql.DB) error {
	features := []string{
		"rate_limit", "budget", "pii", "semantic_cache",
		"loop_detection", "guardrails", "smart_router", "mcp", "openapi",
	}
	for _, key := range features {
		_, err := db.Exec(
			`INSERT INTO runtime_config (section, key, value, updated_at, version)
			 VALUES ('feature', ?, 'true', datetime('now'), 1)
			 ON CONFLICT(section, key) DO UPDATE SET value = 'true', version = version + 1, updated_at = datetime('now')`,
			key,
		)
		if err != nil {
			return fmt.Errorf("insert feature flag %q: %w", key, err)
		}
	}
	return nil
}

func seedPIIPatterns(db *sql.DB) error {
	for _, p := range pii.DefaultPIIPatterns {
		_, err := db.Exec(
			`INSERT INTO pii_patterns (name, regex)
			 VALUES (?, ?)
			 ON CONFLICT(name) DO NOTHING`,
			p.Name, p.Regex,
		)
		if err != nil {
			return fmt.Errorf("insert pii_pattern %q: %w", p.Name, err)
		}
	}
	return nil
}

func seedJobs(db *sql.DB) error {
	type jobSeed struct {
		id       string
		name     string
		desc     string
		steps    string
		cronExpr string
		varCfg   string
	}

	jobs := []jobSeed{
		{
			id:       "content_analysis",
			name:     "Content Analysis",
			desc:     "Analyzes content daily using DeepSeek v4 Flash Free via OpenCode Zen",
			steps:    `[{"type":"llm","prompt_id":1,"model":"deepseek-v4-flash"}]`,
			cronExpr: "0 9 * * *",
			varCfg:   `{"Input":"Today's market analysis: The S&P 500 reached new highs driven by technology sector growth. Key developments include AI product launches, Fed interest rate adjustments, and strong corporate earnings."}`,
		},
		{
			id:       "db_healthcheck",
			name:     "DB Health Check",
			desc:     "Periodic database health check via SQLite MCP — lists tables every 6 hours",
			steps:    `[{"type":"mcp","tool":"sqlite__list_tables","arguments":{}},{"type":"llm","prompt_id":1,"model":"opencode_zen/deepseek-v4-flash-free"}]`,
			cronExpr: "0 */6 * * *",
			varCfg:   `{"Input":"{{.prev}}"}`,
		},
	}

	for _, j := range jobs {
		varCfg := j.varCfg
		if varCfg == "" {
			varCfg = "{}"
		}
		_, err := db.Exec(
			`INSERT OR IGNORE INTO jobs
			 (id, name, description, steps, variables_config, delivery_config, timeout_ms, enabled, api_key_id, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, '{}', 120000, 1, 'test', datetime('now'), datetime('now'))`,
			j.id, j.name, j.desc, j.steps, varCfg,
		)
		if err != nil {
			return fmt.Errorf("insert job %q: %w", j.id, err)
		}

		triggerID := j.id + "_cron"
		configJSON := fmt.Sprintf(`{"expr":"%s"}`, j.cronExpr)
		_, err = db.Exec(
			`INSERT OR IGNORE INTO triggers
			 (id, job_id, kind, enabled, config, created_at, updated_at)
			 VALUES (?, ?, 'cron', 1, ?, datetime('now'), datetime('now'))`,
			triggerID, j.id, configJSON,
		)
		if err != nil {
			return fmt.Errorf("insert trigger for job %q: %w", j.id, err)
		}
	}
	return nil
}

func seedGroups(db *sql.DB) ([]int, error) {
	groups := []struct {
		name        string
		description string
		budget      float64
	}{
		{name: "test", description: "Test group", budget: 500.0},
		{name: "engineering", description: "Engineering team", budget: 500.0},
		{name: "admin", description: "Administrators", budget: 1000.0},
	}

	var ids []int
	for _, g := range groups {
		res, err := db.Exec(
			`INSERT INTO groups (name, description, budget) VALUES (?, ?, ?)
			 ON CONFLICT(name) DO NOTHING`,
			g.name, g.description, g.budget,
		)
		if err != nil {
			return nil, fmt.Errorf("insert group %q: %w", g.name, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		ids = append(ids, int(id))
	}
	return ids, nil
}

func seedUsers(db *sql.DB) ([]int, error) {
	type seedUser struct {
		name     string
		email    string
		budget   float64
		password string
	}
	users := []seedUser{
		{name: "test", email: "test@test", budget: 200.0, password: "test"},
		{name: "admin", email: "admin@test", budget: 500.0, password: "admin"},
	}

	var ids []int
	for _, u := range users {
		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(u.password)))
		storedHash := "sha256:" + hash

		res, err := db.Exec(
			`INSERT INTO users (name, email, status, password_hash, budget) VALUES (?, ?, 'active', ?, ?)
			 ON CONFLICT(email) DO NOTHING`,
			u.name, u.email, storedHash, u.budget,
		)
		if err != nil {
			return nil, fmt.Errorf("insert user %q: %w", u.email, err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		ids = append(ids, int(id))
	}
	return ids, nil
}

func seedUserGroupMemberships(db *sql.DB, userIDs, groupIDs []int) error {
	if len(userIDs) < 2 || len(groupIDs) < 2 {
		return nil
	}
	if err := addMembership(db, userIDs[0], groupIDs[0]); err != nil {
		return err
	}
	if err := addMembership(db, userIDs[1], groupIDs[1]); err != nil {
		return err
	}
	return nil
}

func addMembership(db *sql.DB, userID, groupID int) error {
	_, err := db.Exec(
		`INSERT INTO user_group_memberships (user_id, group_id, role) VALUES (?, ?, ?)
		 ON CONFLICT(user_id, group_id) DO NOTHING`,
		userID, groupID, "member",
	)
	return err
}

func seedPromptTemplates(db *sql.DB) error {
	type seedTemplate struct {
		name        string
		description string
		content     string
		version     string
		labels      []string
	}

	templates := []seedTemplate{
		{
			name:        "content-analyzer",
			description: "Analyzes the given content for key insights, sentiment, structure, and main themes",
			version:     "1.0.0",
			content:     "You are a sharp analytical assistant. Analyze the content provided below and produce a structured analysis covering:\n1. Main Topic & Purpose\n2. Key Points\n3. Sentiment & Tone\n4. Structure & Flow\n5. Notable Patterns\n6. Questions Raised\n\nBe concise, specific, and reference the actual content. Avoid generic statements.\n\n--- CONTENT ---\n{{.Input}}\n--- END ---",
			labels:      []string{"production", "internal"},
		},
		{
			name:        "config-validator",
			description: "Validates an embedded node config payload against rules and returns a deterministic verdict for end-to-end canary testing",
			version:     "1.0.0",
			content:     "You are a configuration validator. Answer the question below immediately.\n\nRules: id non-empty, port 1-65535, enabled bool.\n\nNodes:\n1. id=node-a port=8181 enabled=true\n2. id=node-b port=70000 enabled=true\n3. id=node-c port=443 enabled=false\n\nQuestion: which node violates the rules?\n\nAnswer: VALID or INVALID: <id>",
			labels:      []string{"production", "internal"},
		},
	}

	for _, t := range templates {
		labelsJSON := `["` + t.labels[0] + `","` + t.labels[1] + `"]`

		var existingID int
		err := db.QueryRow("SELECT id FROM prompts WHERE name = ?", t.name).Scan(&existingID)
		if err == nil {
			continue
		}

		res, err := db.Exec(
			`INSERT INTO prompts (name, description, version, content, is_active, labels)
			 VALUES (?, ?, ?, ?, 1, ?)`,
			t.name, t.description, t.version, t.content, labelsJSON,
		)
		if err != nil {
			return fmt.Errorf("insert prompt %q: %w", t.name, err)
		}

		id, err := res.LastInsertId()
		if err != nil {
			return err
		}

		_, err = db.Exec(
			`INSERT INTO prompt_versions (prompt_id, version, content, change_summary)
			 VALUES (?, ?, ?, ?)`,
			id, t.version, t.content, "initial seed version",
		)
		if err != nil {
			return fmt.Errorf("insert version for %q: %w", t.name, err)
		}
	}

	return nil
}

func seedMCPServers(db *sql.DB) error {
	servers := []seed.MCPServer{
		{
			ID:          "sqlite",
			Name:        "SQLite",
			Description: "Local SQLite database explorer with CRUD, schema inspection, and natural-language query",
			Transport:   "stdio",
			Command:     "npx",
			Args:        []string{"mcp-sqlite", "data/ilter.db"},
			IsEnabled:   true,
			TimeoutMs:   30000,
			MaxRetries:  2,
		},
		{
			ID:          "mcp-fetch",
			Name:        "Fetch",
			Description: "Fetch web pages and return their content in clean markdown.",
			Transport:   "stdio",
			Command:     "uvx",
			// mcp-server-fetch declares "mcp>=1.1.3" with no upper bound; mcp 2.0.0
			// renamed mcp.shared.exceptions.McpError to MCPError, which breaks
			// mcp-server-fetch's import at startup. Pin mcp below the rename until
			// mcp-server-fetch publishes a release built against mcp 2.x.
			Args:       []string{"--with", "mcp<2.0.0", "mcp-server-fetch"},
			IsEnabled:  true,
			TimeoutMs:  30000,
			MaxRetries: 2,
		},
	}

	for _, s := range servers {
		argsJSON := ""
		if len(s.Args) > 0 {
			if b, err := json.Marshal(s.Args); err == nil {
				argsJSON = string(b)
			}
		}
		_, err := db.Exec(
			`INSERT INTO mcp_servers
			 (id, name, description, transport, url, command, args, handler, enabled, timeout_ms, max_retries, auth_type, auth_key_env)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO NOTHING`,
			s.ID, s.Name, s.Description, s.Transport,
			nullIfEmpty(s.EndpointURL), nullIfEmpty(s.Command), nullIfEmpty(argsJSON),
			nil, boolToInt(s.IsEnabled), s.TimeoutMs, s.MaxRetries,
			nullIfEmpty(s.AuthType), nullIfEmpty(s.AuthKeyEnv),
		)
		if err != nil {
			return fmt.Errorf("insert mcp server %q: %w", s.ID, err)
		}
		_, _ = db.Exec(`UPDATE mcp_servers SET enabled = ? WHERE id = ?`, boolToInt(s.IsEnabled), s.ID)
	}

	return nil
}

func seedMCPGrants(db *sql.DB) error {
	grants := []struct {
		id          string
		subjectType string
		subjectID   string
		serverID    string
		tools       string
		effect      string
	}{
		{
			id:          "grant-sqlite-wildcard",
			subjectType: "key",
			subjectID:   "*",
			serverID:    "sqlite",
			tools:       "*",
			effect:      "allow",
		},
		{
			id:          "grant-fetch-wildcard",
			subjectType: "key",
			subjectID:   "*",
			serverID:    "mcp-fetch",
			tools:       "*",
			effect:      "allow",
		},
	}

	for _, g := range grants {
		_, err := db.Exec(
			`INSERT INTO mcp_grant (id, subject_type, subject_id, server_id, tools, effect)
			 VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO NOTHING`,
			g.id, g.subjectType, g.subjectID, g.serverID, g.tools, g.effect,
		)
		if err != nil {
			return fmt.Errorf("insert mcp grant %q: %w", g.id, err)
		}
	}
	return nil
}

func seedOpenAPISpecs(db *sql.DB) error {
	specs := []seed.OpenAPISpec{
		{
			ID:          "ilter-api",
			Name:        "ILTER Gateway API",
			Description: "ILTER AI Gateway & Proxy Management API for keys, models, providers, features, and system status",
			SpecURL:     "api/openapi.yaml",
			Operations:  []string{"/healthz", "/api-keys", "/models", "/api/costs", "/api/stats", "/api/usage"},
			AuthType:    "bearer",
			Enabled:     true,
		},
		{
			ID:          "petstore",
			Name:        "Petstore",
			Description: "Sample Petstore API for pets, orders, and store inventory management",
			SpecURL:     "https://petstore.swagger.io/v2/swagger.json",
			Operations:  []string{"findPetsByStatus", "getPetById", "placeOrder", "getInventory"},
			AuthType:    "none",
			Enabled:     true,
		},
	}
	for _, s := range specs {
		opsJSON, err := json.Marshal(s.Operations)
		if err != nil {
			return fmt.Errorf("seed: marshal operations for %q: %w", s.ID, err)
		}
		enabled := 0
		if s.Enabled {
			enabled = 1
		}
		_, err = db.Exec(
			`INSERT INTO openapi_specs (id, name, description, spec_url, operations, auth_type, auth_value, auth_key, timeout_ms, enabled, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 30000, ?, datetime('now'), datetime('now'))
			 ON CONFLICT(id) DO NOTHING`,
			s.ID, s.Name, s.Description, s.SpecURL, string(opsJSON), s.AuthType, s.AuthValue, s.AuthKey, enabled,
		)
		if err != nil {
			return fmt.Errorf("seed: upsert openapi spec %q: %w", s.ID, err)
		}
	}
	return nil
}

// seedProviderModels seeds Opencode Zen free models into the provider_models table.
func seedProviderModels(db *sql.DB) error {
	providerModels := []struct {
		model        string
		displayName  string
		tier         string
		costIn       float64
		costOut      float64
		ctxTokens    int
		outTokens    int
		capabilities string
	}{
		{"deepseek-v4-flash-free", "DeepSeek v4 Flash", "free", 0, 0, 65536, 4096, `["tools","code"]`},
		{"big-pickle", "Big Pickle", "free", 0, 0, 1000000, 384000, `["tools","code"]`},
		{"hy3-free", "Hy3", "free", 0, 0, 32768, 2048, `["tools"]`},
		{"mimo-v2.5-free", "Mimo v2.5", "free", 0, 0, 8192, 1024, `["tools","vision"]`},
		{"nemotron-3-ultra-free", "Nemotron 3 Ultra", "free", 0, 0, 131072, 8192, `["tools","code","reasoning"]`},
		{"north-mini-code-free", "North Mini Code", "free", 0, 0, 65536, 4096, `["tools","code"]`},
	}
	for _, m := range providerModels {
		_, err := db.Exec(
			`INSERT INTO provider_models (provider, model, active, tier, cost_in, cost_out, display_name, max_context_tokens, max_output_tokens, capabilities, default_base_url)
			 VALUES (?, ?, 1, ?, ?, ?, ?, ?, ?, ?, 'https://opencode.ai/zen/v1')
			 ON CONFLICT(provider, model) DO NOTHING`,
			"opencode_zen", m.model, m.tier, m.costIn, m.costOut, m.displayName, m.ctxTokens, m.outTokens, m.capabilities,
		)
		if err != nil {
			return fmt.Errorf("insert provider_model %q: %w", m.model, err)
		}
	}
	return nil
}

func seedSmartRouterStrategies(db *sql.DB) error {
	rcStore := dbpkg.NewSQLiteStoreFromDB(db)

	economyStrategy := map[string]any{
		"name":                   "economy",
		"description":            "Cost-first — simple→deepseek-v4-flash, moderate→hy3, complex→nemotron-3-ultra",
		"enabled":                true,
		"provider_preference":    "cheapest",
		"load_balancer_strategy": "weighted-random",
		"scorer": map[string]string{
			"type": "heuristic",
		},
		"complexity_thresholds": map[string]float64{
			"economy":  25,
			"standard": 60,
		},
		"rules": []map[string]any{
			{
				"name":         "Simple queries",
				"condition":    "complexity < 25",
				"target_model": "deepseek-v4-flash-free",
				"priority":     100,
				"enabled":      true,
			},
			{
				"name":         "Moderate queries",
				"condition":    "complexity between 25 60",
				"target_model": "hy3-free",
				"priority":     90,
				"enabled":      true,
			},
			{
				"name":         "Complex queries",
				"condition":    "complexity >= 60",
				"target_model": "nemotron-3-ultra-free",
				"priority":     80,
				"enabled":      true,
			},
		},
	}

	data, err := json.Marshal(economyStrategy)
	if err != nil {
		return fmt.Errorf("marshal economy strategy: %w", err)
	}

	if err := rcStore.UpsertRuntimeConfig("routing_strategy", "economy", string(data), "system"); err != nil {
		return fmt.Errorf("upsert economy strategy: %w", err)
	}

	activeStrategyData, _ := json.Marshal("economy")
	if err := rcStore.UpsertRuntimeConfig("active_routing_strategy", "active", string(activeStrategyData), "system"); err != nil {
		return fmt.Errorf("set active strategy: %w", err)
	}

	return nil
}
