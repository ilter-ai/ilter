package dashutil

import (
	"database/sql"
	"net/http"
)

// NullToEmpty returns s.String if s.Valid, otherwise "".
func NullToEmpty(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}

// IfVal returns *p if p is non-nil, otherwise fallback.
func IfVal[T any](p *T, fallback T) T {
	if p != nil {
		return *p
	}
	return fallback
}

// IfStr returns s if s is non-empty, otherwise fallback.
func IfStr(s string, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}

// IsSecure reports whether the request uses TLS or a trusted proxy header.
func IsSecure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}
