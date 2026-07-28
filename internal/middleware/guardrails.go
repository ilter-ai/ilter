package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/features/guardrails"
	"github.com/ilter-ai/ilter/internal/model"

	"github.com/ilter-ai/ilter/internal/platform/reqmeta"
)

// GuardrailsMiddleware checks requests against guardrails rules.
type GuardrailsMiddleware struct {
	checker atomic.Pointer[guardrails.Checker]
	logger  *slog.Logger
	enabled atomic.Bool

	store atomic.Pointer[db.SQLiteStore]
}

// SetStore wires the DB store used to persist guardrail_events rows for
// dashboard/API consumption (violation log, blocked-request counts). Safe to
// call after construction; event recording is a no-op until this is set.
func (m *GuardrailsMiddleware) SetStore(store *db.SQLiteStore) {
	m.store.Store(store)
}

// recordEvent persists a guardrail decision to guardrail_events, resolving
// the requested model's provider from provider_models so the violation log
// doesn't show blank Model/Provider columns. Best-effort: failures are
// logged, never surfaced to the caller.
func (m *GuardrailsMiddleware) recordEvent(r *http.Request, modelName, guardrailType, actionTaken, severity, matched string) {
	store := m.store.Load()
	if store == nil {
		return
	}
	keyID := reqmeta.GetKeyID(r.Context())
	details := matched
	if severity != "" {
		details = fmt.Sprintf("[%s] %s", severity, matched)
	}

	var provider string
	if modelName != "" {
		provider, _ = store.GetProviderForModel(modelName)
	}

	if err := store.InsertGuardrailEvent(keyID, guardrailType, actionTaken, modelName, provider, details); err != nil {
		m.logger.Warn("guardrails: failed to record event", "error", err)
	}
}

// NewGuardrailsMiddleware creates a new GuardrailsMiddleware that reads
// guardrail rules from the given ConfigCache. Built-in rule sets and
// custom rules are sourced from cache.GuardRules() and cache.CustomRules()
// respectively. The checker is rebuilt automatically whenever the cache
// fires its OnChange callback.
func NewGuardrailsMiddleware(cache *config.Cache, logger *slog.Logger) (*GuardrailsMiddleware, error) {
	if logger == nil {
		logger = slog.Default()
	}

	mw := &GuardrailsMiddleware{
		logger: logger,
	}

	snap := cache.Get()
	if err := mw.rebuildFromSnapshot(snap); err != nil {
		return nil, err
	}

	// Wire cache refresh → guardrail middleware reload.
	cache.OnChange(func(snap *config.Snapshot) {
		if err := mw.rebuildFromSnapshot(snap); err != nil {
			logger.Warn("guardrails: failed to rebuild checker on config refresh", "error", err)
		}
	})

	return mw, nil
}

// rebuildFromSnapshot builds a fresh guardrails.Checker from the given
// configuration snapshot and atomically swaps it into the middleware.
func (m *GuardrailsMiddleware) rebuildFromSnapshot(snap *config.Snapshot) error {
	if snap == nil {
		return nil
	}

	customRules := make([]guardrails.CustomRule, 0, len(snap.CustomRules()))
	for _, cr := range snap.CustomRules() {
		customRules = append(customRules, guardrails.CustomRule{
			ID:       cr.ID,
			Patterns: cr.Patterns,
			Mode:     cr.Mode,
			Severity: cr.Severity,
		})
	}

	modCfg := snap.GuardrailsModerationAPI
	modTimeout := 3 * time.Second
	if modCfg.Timeout != "" {
		if d, err := time.ParseDuration(modCfg.Timeout); err == nil && d > 0 {
			modTimeout = d
		}
	}

	gcfg := guardrails.Config{
		Enabled:     snap.GuardrailsEnabled,
		Mode:        snap.GuardrailsMode,
		RuleSets:    snap.GuardRules(),
		CustomRules: customRules,
		ModerationAPI: guardrails.ModerationAPI{
			Enabled: modCfg.Enabled,
			URL:     modCfg.URL,
			APIKey:  modCfg.APIKey,
			Timeout: modTimeout,
		},
	}

	checker, err := guardrails.NewChecker(gcfg, m.logger)
	if err != nil {
		return fmt.Errorf("guardrails: rebuild from snapshot: %w", err)
	}

	m.checker.Store(checker)
	m.enabled.Store(snap.GuardrailsEnabled)
	return nil
}

// Checker returns the underlying guardrails.Checker for use by admin handlers.
func (m *GuardrailsMiddleware) Checker() *guardrails.Checker {
	return m.checker.Load()
}

// LoadDBRules queries all enabled guardrail rules from the database
// and loads them into the checker, replacing any previously loaded rules.
func (m *GuardrailsMiddleware) LoadDBRules(store *db.SQLiteStore) {
	dbRows, err := store.GetEnabledGuardrailRules()
	if err != nil {
		m.logger.Warn("guardrails: failed to query DB rules", "error", err)
		return
	}

	dbRules := make([]guardrails.DBRule, 0, len(dbRows))
	for _, row := range dbRows {
		var patterns []string
		if err := json.Unmarshal([]byte(row.Patterns), &patterns); err != nil {
			m.logger.Warn("guardrails: skip rule with invalid patterns JSON", "id", row.ID, "error", err)
			continue
		}
		dbRules = append(dbRules, guardrails.DBRule{
			ID:         row.ID,
			RuleSet:    row.Type,
			Patterns:   patterns,
			Mode:       row.Mode,
			Severity:   row.Severity,
			TargetType: row.TargetType,
			TargetID:   row.TargetID,
		})
	}

	if checker := m.checker.Load(); checker != nil {
		checker.LoadDBRules(dbRules)
	}
}

// Handler returns the HTTP middleware handler.
func (m *GuardrailsMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.enabled.Load() {
			next.ServeHTTP(w, r)
			return
		}

		if r.Method != "POST" || r.URL.Path != "/v1/chat/completions" {
			next.ServeHTTP(w, r)
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			m.logger.Error("guardrails: failed to read request body", "error", err)
			next.ServeHTTP(w, r)
			return
		}
		r.Body.Close()

		var req model.ChatCompletionRequest
		if err := json.Unmarshal(bodyBytes, &req); err != nil {
			// Pass through unparseable bodies to let proxy fail them
			r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			next.ServeHTTP(w, r)
			return
		}

		messages := make([]guardrails.Message, 0, len(req.Messages))
		for i, msg := range req.Messages {
			if contentStr, ok := msg.Content.(string); ok && contentStr != "" {
				messages = append(messages, guardrails.Message{
					Index:   i,
					Role:    msg.Role,
					Content: contentStr,
				})
			}
		}

		checkStart := time.Now()
		checker := m.checker.Load()
		result := checker.Check(r.Context(), messages)
		if !result.Blocked && !result.Warned {
			userID := reqmeta.GetUserID(r.Context())
			groupIDs := reqmeta.GetGroupIDs(r.Context())
			result = checker.CheckDB(r.Context(), messages, userID, groupIDs)
		}
		checkDuration := time.Since(checkStart).Milliseconds()

		if result.Blocked {
			m.logger.Warn(
				"guardrails: request blocked",
				"rule_id", result.RuleID,
				"rule_set", result.RuleSet,
				"severity", result.Severity,
				"matched", result.MatchedText,
			)
			attrs := metric.WithAttributes(
				attribute.String("rule_set", result.RuleSet),
				attribute.String("severity", string(result.Severity)),
			)
			if guardrailsBlockedTotal != nil {
				guardrailsBlockedTotal.Add(r.Context(), 1, attrs)
			}
			if guardrailsCheckDuration != nil {
				guardrailsCheckDuration.Record(r.Context(), float64(checkDuration), attrs)
			}
			m.recordEvent(r, req.Model, result.RuleSet, "blocked", string(result.Severity), result.MatchedText)
			writeGuardrailsBlockedError(w, result)
			return
		}

		if result.Warned {
			m.logger.Info(
				"guardrails: request warned",
				"rule_id", result.RuleID,
				"rule_set", result.RuleSet,
				"severity", result.Severity,
				"matched", result.MatchedText,
			)
			attrs := metric.WithAttributes(
				attribute.String("rule_set", result.RuleSet),
				attribute.String("severity", string(result.Severity)),
			)
			if guardrailsWarnedTotal != nil {
				guardrailsWarnedTotal.Add(r.Context(), 1, attrs)
			}
			if guardrailsCheckDuration != nil {
				guardrailsCheckDuration.Record(r.Context(), float64(checkDuration), attrs)
			}
			m.recordEvent(r, req.Model, result.RuleSet, "warned", string(result.Severity), result.MatchedText)
			w.Header().Set("X-Guardrails-Warning", result.RuleID)
		}

		if guardrailsCheckedTotal != nil {
			guardrailsCheckedTotal.Add(r.Context(), 1)
		}

		r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		next.ServeHTTP(w, r)
	})
}

func writeGuardrailsBlockedError(w http.ResponseWriter, result guardrails.Result) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	errResp := map[string]any{
		"error": map[string]any{
			"message":  "guardrails_blocked",
			"type":     "guardrails_blocked",
			"code":     "guardrails_blocked",
			"rule_id":  result.RuleID,
			"rule_set": result.RuleSet,
			"severity": string(result.Severity),
			"matched":  result.MatchedText,
		},
	}
	if err := json.NewEncoder(w).Encode(errResp); err != nil {
		slog.Error("failed to encode guardrails blocked response", "error", err)
	}
}
