package db

import "time"

// sqliteTimestampLayout is the format datetime('now')/CURRENT_TIMESTAMP
// produce, and the format our SQLite DSN's _time_format=datetime writes for
// bound time.Time values: "YYYY-MM-DD HH:MM:SS", always UTC (SQLite's own
// date functions are hardcoded UTC, and the DSN's _timezone=UTC normalizes
// bound values to UTC before writing — see sqlite.go).
const sqliteTimestampLayout = "2006-01-02 15:04:05"

// FormatSQLiteTimestamp converts a raw SQLite timestamp string (as scanned
// from a TEXT column populated by datetime('now')/CURRENT_TIMESTAMP or a
// normalized bound time.Time) into RFC3339 with an explicit "Z" suffix.
//
// Without this, a JSON API response carrying the bare string
// "2026-07-27 07:00:00" gets parsed by JS `new Date(...)` as LOCAL time in
// the viewer's browser, not UTC — silently skewing every displayed timestamp
// by the viewer's UTC offset. If the string doesn't match the expected
// layout, it's returned unchanged rather than dropped.
func FormatSQLiteTimestamp(raw string) string {
	if raw == "" {
		return raw
	}
	t, err := time.Parse(sqliteTimestampLayout, raw)
	if err != nil {
		return raw
	}
	return t.UTC().Format(time.RFC3339)
}
