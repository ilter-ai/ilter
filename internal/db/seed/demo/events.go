package demo

import (
	"database/sql"
	"fmt"
	"math/rand"
	"time"
)

// ── Package-level variables used by event seed functions ──

var (
	models   = []string{"big-pickle", "deepseek-v4-flash-free", "hy3-free", "mimo-v2.5-free", "nemotron-3-ultra-free", "north-mini-code-free"}
	statuses = []int{200, 200, 200, 200, 201, 400, 401, 429, 500}
)

// openapiSearchIntents/openapiOperationIDs seed a handful of realistic
// OpenAPI-bridge calls against the Petstore spec loaded by seedOpenAPISpecs.
var (
	openapiSearchIntents = []string{"find pets by status", "place an order", "check store inventory", "look up a pet by id"}
	openapiOperationIDs  = []string{"Petstore_findPetsByStatus", "Petstore_getPetById", "Petstore_placeOrder", "Petstore_getInventory"}
)

type seedGuardrailEvent struct {
	guardrailType string
	actionTaken   string
	detailFmt     string
}

// ── Randomized event seed functions ──

// seedTimestampOffset returns a random duration to subtract from "now" when
// generating seed timestamps. Seed data intentionally stops 6 hours before
// "now" and reaches back 30 days.
func seedTimestampOffset(rng *rand.Rand) time.Duration {
	const minAge = 6 * time.Hour
	const maxAge = 30 * 24 * time.Hour
	return minAge + time.Duration(rng.Int63n(int64(maxAge-minAge)))
}

func seedAuditLog(db *sql.DB, rng *rand.Rand, keyIDs []string, _ time.Time) error {
	stmt, err := db.Prepare(
		`INSERT INTO audit_log (timestamp, key_id, model, provider,
		                         prompt_tokens, completion_tokens, total_cost,
		                         latency_ms, status_code, cache_hit,
		                         prompt_preview, client_ip)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()

	providerOf := map[string]string{
		"big-pickle":             "opencode_zen",
		"deepseek-v4-flash-free": "opencode_zen",
		"hy3-free":               "opencode_zen",
		"mimo-v2.5-free":         "opencode_zen",
		"nemotron-3-ultra-free":  "opencode_zen",
		"north-mini-code-free":   "opencode_zen",
	}

	const targetRows = 50
	inserted := 0
	for inserted < targetRows {
		ts := time.Now().Add(-seedTimestampOffset(rng))
		keyID := keyIDs[rng.Intn(len(keyIDs))]
		model := models[rng.Intn(len(models))]
		provider := providerOf[model]
		promptTokens := 50 + rng.Intn(4000)
		completionTokens := 20 + rng.Intn(2000)
		cost := float64(promptTokens)*0.0000025 + float64(completionTokens)*0.00001
		latency := 200 + rng.Intn(8000)
		status := statuses[rng.Intn(len(statuses))]
		cacheHit := rng.Intn(3) == 0
		preview := fmt.Sprintf("Seed prompt for %s — request #%d", model, inserted+1)

		ip := fmt.Sprintf("192.168.%d.%d", rng.Intn(256), 1+rng.Intn(254))

		_, err = stmt.Exec(sqliteTS(ts), keyID, model, provider,
			promptTokens, completionTokens, cost,
			latency, status, cacheHit, preview, ip)
		if err != nil {
			return err
		}
		inserted++
	}
	return nil
}

func seedUsageDaily(db *sql.DB, rng *rand.Rand, keyIDs []string, today time.Time) error {
	providerOf := map[string]string{
		"big-pickle":             "opencode_zen",
		"deepseek-v4-flash-free": "opencode_zen",
		"hy3-free":               "opencode_zen",
		"mimo-v2.5-free":         "opencode_zen",
		"nemotron-3-ultra-free":  "opencode_zen",
		"north-mini-code-free":   "opencode_zen",
	}

	stmt, err := db.Prepare(
		`INSERT INTO usage_daily (key_id, date, model, provider,
		                          tokens, cost, request_count,
		                          prompt_tokens, completion_tokens, cache_hits)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(key_id, date, model, provider) DO NOTHING`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for day := range 30 {
		date := today.Add(-time.Duration(day) * 24 * time.Hour).Format("2006-01-02")
		for _, keyID := range keyIDs {
			nModels := 1 + rng.Intn(4)
			for range nModels {
				model := models[rng.Intn(len(models))]
				provider := providerOf[model]
				reqCount := 1 + rng.Intn(80)
				promptTok := 100 + rng.Intn(8000)
				completionTok := 50 + rng.Intn(4000)
				totalTok := promptTok + completionTok
				cost := float64(promptTok)*0.0000025 + float64(completionTok)*0.00001
				cacheDiv := max(reqCount/3, 1)
				cacheHits := rng.Intn(cacheDiv)

				_, err = stmt.Exec(keyID, date, model, provider,
					totalTok, cost, reqCount,
					promptTok, completionTok, cacheHits)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func seedLoopEvents(db *sql.DB, rng *rand.Rand, keyIDs []string, _ time.Time) error {
	actions := []string{"blocked", "throttled", "logged", "alerted"}
	stmt, err := db.Prepare(
		`INSERT INTO loop_events (detected_at, key_id, client_ip,
		                          prompt_hash, repeat_count, window_seconds,
		                          action_taken, resolved_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()

	n := 20 + rng.Intn(11)
	for range n {
		detectedAt := time.Now().Add(-seedTimestampOffset(rng))
		keyID := keyIDs[rng.Intn(len(keyIDs))]
		ip := fmt.Sprintf("192.168.%d.%d", rng.Intn(256), 1+rng.Intn(254))
		hash := fmt.Sprintf("loop_hash_%x", rng.Int63())
		repeatCount := 3 + rng.Intn(48)
		windowSec := 30 + rng.Intn(300)
		action := actions[rng.Intn(len(actions))]

		var resolvedAt *string
		if rng.Intn(2) == 0 {
			r := detectedAt.Add(time.Duration(rng.Intn(3600)) * time.Second)
			s := sqliteTS(r)
			resolvedAt = &s
		}

		_, err = stmt.Exec(sqliteTS(detectedAt), keyID, ip,
			hash, repeatCount, windowSec, action, resolvedAt)
		if err != nil {
			return err
		}
	}
	return nil
}

func randomPIIValue(rng *rand.Rand, piiType string) string {
	switch piiType {
	case "EMAIL":
		user := fmt.Sprintf("user%d", 100+rng.Intn(900))
		domains := []string{"example.com", "acme.org", "testmail.com", "company.co"}
		return fmt.Sprintf("%s@%s", user, domains[rng.Intn(len(domains))])
	case "TCKN":
		d := make([]byte, 11)
		d[0] = byte(1+rng.Intn(9)) + '0'
		for i := 1; i < 10; i++ {
			d[i] = byte(rng.Intn(10)) + '0'
		}
		d[10] = byte(rng.Intn(5)*2) + '0'
		return string(d)
	case "SSN":
		return fmt.Sprintf("%03d-%02d-%04d", rng.Intn(900), rng.Intn(100), rng.Intn(10000))
	case "PHONE":
		area := []string{"532", "533", "536", "537", "505", "506", "507", "541", "542", "543", "551", "552", "553", "554", "555"}
		return fmt.Sprintf("+90 %s %03d %02d %02d",
			area[rng.Intn(len(area))],
			rng.Intn(1000), rng.Intn(100), rng.Intn(100))
	case "IP_ADDRESS":
		return fmt.Sprintf("%d.%d.%d.%d", 1+rng.Intn(223), rng.Intn(256), rng.Intn(256), 1+rng.Intn(254))
	case "CREDIT_CARD":
		return fmt.Sprintf("%04d-%04d-%04d-%04d",
			rng.Intn(10000), rng.Intn(10000), rng.Intn(10000), rng.Intn(10000))
	default:
		return "unknown"
	}
}

func seedPIIEvents(db *sql.DB, rng *rand.Rand, keyIDs []string, _ time.Time) error {
	piiTypes := []string{"EMAIL", "TCKN", "SSN", "PHONE", "IP_ADDRESS", "CREDIT_CARD"}
	actions := []string{"masked", "blocked", "logged"}

	stmt, err := db.Prepare(
		`INSERT INTO pii_events (timestamp, key_id, request_id,
		                         pii_type, action_taken, masked_prompt_preview, client_ip)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()

	n := 30 + rng.Intn(21)
	for i := range n {
		ts := time.Now().Add(-seedTimestampOffset(rng))
		keyID := keyIDs[rng.Intn(len(keyIDs))]
		requestID := 10000 + rng.Intn(90000)
		piiType := piiTypes[rng.Intn(len(piiTypes))]
		action := actions[rng.Intn(len(actions))]
		ip := fmt.Sprintf("10.0.%d.%d", rng.Intn(256), 1+rng.Intn(254))
		preview := randomPIIValue(rng, piiType)

		_, err = stmt.Exec(sqliteTS(ts), keyID, requestID,
			piiType, action, preview, ip)
		if err != nil {
			return fmt.Errorf("insert pii event %d: %w", i, err)
		}
	}
	return nil
}

func seedGuardrailEvents(db *sql.DB, rng *rand.Rand, keyIDs []string, _ time.Time) error {
	eventTypes := []seedGuardrailEvent{
		{"pii_block", "blocked", "Request blocked — PII detected: %s in user message"},
		{"pii_block", "flagged", "PII flagged: potential %s detected in prompt"},
		{"pii_block", "masked", "PII masked: %s replaced with placeholder in request"},
		{"budget_block", "blocked", "Monthly budget of $%.2f exceeded for key %q"},
		{"budget_block", "flagged", "Budget warning: key %q at %.0f%% of monthly limit"},
		{"budget_block", "throttled", "Request delayed — key %q approaching daily limit of $%.2f"},
		{"rate_limit", "blocked", "Rate limit exceeded: %d RPM for key %q"},
		{"rate_limit", "throttled", "Request queued — key %q at %d/%d RPM"},
		{"rate_limit", "flagged", "High traffic: key %q reached %d%% of rate limit"},
		{"loop_detection", "blocked", "Agentic loop blocked: %d repeats detected in %ds window"},
		{"loop_detection", "throttled", "Loop throttled: session %q showing repetitive pattern"},
		{"loop_detection", "alerted", "Loop alert: key %q triggered %d fingerprint matches"},
		{"content_policy", "blocked", "Content policy violation: %s"},
		{"content_policy", "flagged", "Content flagged: potential %s in model output"},
		{"content_policy", "allowed", "Content allowed with warning: %s"},
		{"model_access", "blocked", "Model %q not authorized for key prefix %q"},
		{"model_access", "flagged", "Unusual model access: %q requested from key %q"},
	}

	piiValues := []string{"EMAIL", "SSN", "CREDIT_CARD", "TCKN", "PHONE", "IP_ADDRESS"}
	contentFlags := []string{"prompt injection attempt", "toxic language", "jailbreak attempt", "topic restriction"}

	stmt, err := db.Prepare(
		`INSERT INTO guardrail_events
		 (timestamp, key_id, guardrail_type, action_taken, model, provider, details, request_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()

	n := 40 + rng.Intn(11)
	for i := range n {
		ts := time.Now().Add(-seedTimestampOffset(rng))
		keyID := keyIDs[rng.Intn(len(keyIDs))]
		evt := eventTypes[rng.Intn(len(eventTypes))]

		model := models[rng.Intn(len(models))]
		providerOf := map[string]string{
			"gpt-4o":                   "openai",
			"gpt-4o-mini":              "openai",
			"claude-sonnet-4-20250514": "anthropic",
			"deepseek-chat":            "deepseek",
			"deepseek-v4-flash-free":   "opencode_zen",
			"gemini-2.5-flash":         "gemini",
		}
		provider := providerOf[model]

		var detail string
		switch evt.guardrailType {
		case "pii_block":
			piiType := piiValues[rng.Intn(len(piiValues))]
			detail = fmt.Sprintf(evt.detailFmt, randomPIIValue(rng, piiType))
		case "budget_block":
			budget := 20.0 + float64(rng.Intn(480))
			pct := 70.0 + float64(rng.Intn(30))
			if evt.actionTaken == "blocked" {
				detail = fmt.Sprintf(evt.detailFmt, budget, fmt.Sprintf("key-%s", keyID))
			} else if evt.actionTaken == "flagged" {
				detail = fmt.Sprintf(evt.detailFmt, fmt.Sprintf("key-%s", keyID), pct)
			} else {
				detail = fmt.Sprintf(evt.detailFmt, fmt.Sprintf("key-%s", keyID), 5.0+float64(rng.Intn(45)))
			}
		case "rate_limit":
			rpm := 50 + rng.Intn(950)
			detail = fmt.Sprintf(evt.detailFmt, rpm, fmt.Sprintf("key-%s", keyID))
			if evt.actionTaken == "throttled" {
				detail = fmt.Sprintf(evt.detailFmt, fmt.Sprintf("key-%s", keyID), rng.Intn(rpm), rpm)
			} else if evt.actionTaken == "flagged" {
				pct := 60 + rng.Intn(39)
				detail = fmt.Sprintf(evt.detailFmt, fmt.Sprintf("key-%s", keyID), pct)
			}
		case "loop_detection":
			repeats := 5 + rng.Intn(45)
			window := 30 + rng.Intn(270)
			if evt.actionTaken == "blocked" {
				detail = fmt.Sprintf(evt.detailFmt, repeats, window)
			} else if evt.actionTaken == "alerted" {
				detail = fmt.Sprintf(evt.detailFmt, fmt.Sprintf("key-%s", keyID), repeats)
			} else {
				sessionID := fmt.Sprintf("sess_%x", rng.Int63())
				detail = fmt.Sprintf(evt.detailFmt, sessionID)
			}
		case "content_policy":
			detail = fmt.Sprintf(evt.detailFmt, contentFlags[rng.Intn(len(contentFlags))])
		case "model_access":
			unauthModel := models[rng.Intn(len(models))]
			if evt.actionTaken == "blocked" {
				detail = fmt.Sprintf(evt.detailFmt, unauthModel, "sk_test_"+fmt.Sprintf("key-%s", keyID))
			} else {
				detail = fmt.Sprintf(evt.detailFmt, unauthModel, fmt.Sprintf("key-%s", keyID))
			}
		}

		requestID := 10000 + rng.Intn(90000)

		_, err = stmt.Exec(sqliteTS(ts), keyID,
			evt.guardrailType, evt.actionTaken,
			model, provider, detail, requestID)
		if err != nil {
			return fmt.Errorf("insert guardrail event %d: %w", i, err)
		}
	}

	return nil
}

// seedOpenAPIAuditRow picks one of the three OpenAPI meta-tool call shapes
// and returns the (tool label, params JSON) pair.
func seedOpenAPIAuditRow(rng *rand.Rand) (tool, params string) {
	opID := openapiOperationIDs[rng.Intn(len(openapiOperationIDs))]
	switch rng.Intn(3) {
	case 0:
		intent := openapiSearchIntents[rng.Intn(len(openapiSearchIntents))]
		return "search: " + intent, fmt.Sprintf(`{"intent":"%s"}`, intent)
	case 1:
		return "describe: " + opID, fmt.Sprintf(`{"operation_ids":["%s"]}`, opID)
	default:
		return opID, fmt.Sprintf(`{"operation_id":"%s","params":{}}`, opID)
	}
}

func seedMCPAuditLog(db *sql.DB, rng *rand.Rand, keyIDs []string, _ time.Time) error {
	toolsByServer := map[string][]string{
		"sqlite":    {"db_info", "list_tables", "get_table_schema", "create_record", "read_records", "update_records", "delete_records", "query"},
		"mcp-fetch": {"fetch"},
	}
	serverIDs := []string{"sqlite", "mcp-fetch", "openapi"}

	methods := []string{"tools/call", "tools/call", "tools/call", "tools/list", "initialize"}
	mcpStatuses := []int{200, 200, 200, 200, 201, 400, 404, 500}

	stmt, err := db.Prepare(
		`INSERT INTO mcp_audit_log
		 (key_id, tool, server_id, method, params, duration_ms, status_code, success, error_msg, client_ip, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return err
	}
	defer stmt.Close()

	n := 50 + rng.Intn(51)
	for i := range n {
		serverID := serverIDs[rng.Intn(len(serverIDs))]

		var tool, method, params string
		if serverID == "openapi" {
			method = "tools/call"
			tool, params = seedOpenAPIAuditRow(rng)
		} else {
			tools := toolsByServer[serverID]
			tool = tools[rng.Intn(len(tools))]
			method = methods[rng.Intn(len(methods))]
			params = "{}"
			if method == "tools/call" {
				switch tool {
				case "get_table_schema":
					params = `{"table_name":"api_keys"}`
				case "read_records":
					params = `{"table":"api_keys","limit":5}`
				case "query":
					params = `{"query":"SELECT * FROM api_keys LIMIT 5"}`
				case "fetch":
					params = `{"url":"https://example.com"}`
				}
			}
		}

		keyID := keyIDs[rng.Intn(len(keyIDs))]
		durationMs := 50.0 + float64(rng.Intn(3000))
		status := mcpStatuses[rng.Intn(len(mcpStatuses))]
		success := status >= 200 && status < 400
		var errorMsg string
		if !success {
			switch status {
			case 400:
				errorMsg = fmt.Sprintf("invalid params for %s: missing required field", tool)
			case 404:
				errorMsg = fmt.Sprintf("tool %s not found on server %s", tool, serverID)
			case 500:
				errorMsg = "internal server error: connection refused"
			}
		}

		createdAt := time.Now().Add(-seedTimestampOffset(rng))
		clientIP := fmt.Sprintf("10.0.%d.%d", rng.Intn(256), 1+rng.Intn(254))

		_, err = stmt.Exec(keyID, tool, serverID, method, params,
			durationMs, status, boolToInt(success), nullIfEmpty(errorMsg),
			clientIP, sqliteTS(createdAt))
		if err != nil {
			return fmt.Errorf("insert mcp audit log %d: %w", i, err)
		}
	}

	return nil
}
