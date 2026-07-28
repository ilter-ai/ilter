package pii

import (
	"database/sql"
	"fmt"
	"log/slog"
	"regexp"
)

// LoadPatternsFromDB reads PII patterns from the pii_patterns table and populates
// the global LoadedPatterns map. If the table doesn't exist or is empty, no
// patterns are loaded (the masker will mask nothing until patterns are seeded).
func LoadPatternsFromDB(db *sql.DB) error {
	rows, err := db.Query("SELECT name, regex, enabled, COALESCE(action, 'mask') FROM pii_patterns")
	if err != nil {
		return fmt.Errorf("query pii_patterns: %w", err)
	}
	defer rows.Close()

	var patterns []Pattern
	for rows.Next() {
		var p Pattern
		if err := rows.Scan(&p.Name, &p.Regex, &p.Enabled, &p.Action); err != nil {
			return fmt.Errorf("scan pii_patterns row: %w", err)
		}
		re, err := regexp.Compile(p.Regex)
		if err != nil {
			slog.Error("failed to compile PII regex from DB", "name", p.Name, "regex", p.Regex, "error", err)
			continue
		}
		p.Re = re
		patterns = append(patterns, p)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate pii_patterns rows: %w", err)
	}

	LoadPatterns(patterns)
	slog.Info("loaded PII patterns from database", "count", len(patterns))
	return nil
}

// LoadPatterns replaces the global LoadedPatterns map with the given patterns.
// Each pattern's regex is expected to already be compiled (Re field populated).
// If Re is nil, the regex is compiled here; if compilation fails, the pattern
// is skipped with a warning.
func LoadPatterns(patterns []Pattern) {
	patternMu.Lock()
	defer patternMu.Unlock()

	// Clear existing patterns.
	for k := range LoadedPatterns {
		delete(LoadedPatterns, k)
	}

	for _, p := range patterns {
		if p.Re == nil {
			re, err := regexp.Compile(p.Regex)
			if err != nil {
				slog.Warn("failed to compile PII regex", "name", p.Name, "regex", p.Regex, "error", err)
				continue
			}
			p.Re = re
		}
		LoadedPatterns[p.Name] = p
	}
}
