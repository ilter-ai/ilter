package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
)

var (
	regMu    sync.Mutex
	registry []*regEntry
)

// regEntry is the non-generic registry entry shared by the typed EnvVar[T].
type regEntry struct {
	Name        string
	Description string
	Default     any
	set         bool  // true if the env var was actually present
	resolved    bool  // true once the env has been checked
	value       any   // resolved or default value
	resolveErr  error // non-nil if parsing failed
}

// EnvSnapshot is a non-generic view of an EnvVar[T] for inspection and
// debugging (for example, All() returns snapshots).
type EnvSnapshot struct {
	Name        string
	Description string
	Default     any
	Value       any
	Set         bool
}

// EnvVar is a typed environment variable binding.  Register one with
// RegisterEnv, then call Resolve() to read the value from the environment.
type EnvVar[T any] struct {
	entry  *regEntry
	parser func(string) (T, error)
}

// RegisterEnv creates a new typed environment variable binding, registers it
// in the global registry, and returns the binding.  The parser must return an
// error (not panic) for invalid values; the caller (Resolve / WasSet) will
// panic on parse failure to catch config bugs early.
func RegisterEnv[T any](name, desc string, defaultVal T, parser func(string) (T, error)) *EnvVar[T] {
	regMu.Lock()
	defer regMu.Unlock()

	entry := &regEntry{
		Name:        name,
		Description: desc,
		Default:     defaultVal,
	}
	registry = append(registry, entry)

	return &EnvVar[T]{
		entry:  entry,
		parser: parser,
	}
}

// ensureResolved reads the environment and caches the result (value + set
// flag).  Called lazily by both Resolve and WasSet so that the env is
// examined at most once.
func (e *EnvVar[T]) ensureResolved() {
	if e.entry.resolved {
		return
	}
	e.entry.resolved = true

	raw, ok := os.LookupEnv(e.entry.Name)
	if !ok {
		e.entry.value = e.entry.Default
		return
	}

	parsed, err := e.parser(raw)
	if err != nil {
		e.entry.resolveErr = fmt.Errorf("env %s: invalid value %q: %w", e.entry.Name, raw, err)
		return
	}
	e.entry.set = true
	e.entry.value = parsed
}

// Resolve reads the environment variable.  If set it parses the value and
// returns it; if absent it returns the default.  On parse failure it logs an
// error and returns the default value.  Use ResolveE for explicit error
// handling.
func (e *EnvVar[T]) Resolve() T {
	e.ensureResolved()
	if e.entry.resolveErr != nil {
		slog.Error(
			"env var parse failure, using default",
			"name", e.entry.Name,
			"error", e.entry.resolveErr,
		)
		return e.entry.Default.(T)
	}
	return e.entry.value.(T)
}

// ResolveE reads the environment variable and returns a typed value or a
// parse error.  Unlike Resolve it does not silently fall back to the default
// on failure — callers that need to distinguish "env not set" from "env set
// but invalid" should prefer this method.
func (e *EnvVar[T]) ResolveE() (T, error) {
	e.ensureResolved()
	if e.entry.resolveErr != nil {
		var zero T
		return zero, e.entry.resolveErr
	}
	if !e.entry.set {
		return e.entry.Default.(T), nil
	}
	return e.entry.value.(T), nil
}

// WasSet reports whether the environment variable was actually present
// (regardless of whether Resolve was called).
func (e *EnvVar[T]) WasSet() bool {
	e.ensureResolved()
	return e.entry.set
}

// All returns a snapshot of every registered environment variable.
// For entries that have not been resolved yet, the value field holds the
// default (without touching the environment).
func All() []EnvSnapshot {
	regMu.Lock()
	defer regMu.Unlock()

	out := make([]EnvSnapshot, 0, len(registry))
	for _, entry := range registry {
		v := entry.Default
		if entry.resolved {
			v = entry.value
		}
		out = append(out, EnvSnapshot{
			Name:        entry.Name,
			Description: entry.Description,
			Default:     entry.Default,
			Value:       v,
			Set:         entry.set,
		})
	}
	return out
}

// RegisteredEnvVars returns the names of all registered environment variables.
// This is useful for test verification and debugging.
func RegisteredEnvVars() []string {
	regMu.Lock()
	defer regMu.Unlock()

	names := make([]string, 0, len(registry))
	for _, entry := range registry {
		names = append(names, entry.Name)
	}
	return names
}

// WarnUnknownEnv scans os.Environ() and logs a warning for every variable
// with the ILTER_ prefix that is NOT in the catalog.  Call it after all
// env overrides have been applied (typically at the end of boot).
//
// It returns a no-op function so it can be used with defer:
//
//	defer config.WarnUnknownEnv()()
func WarnUnknownEnv() func() {
	regMu.Lock()
	registered := make(map[string]bool, len(registry))
	for _, entry := range registry {
		registered[entry.Name] = true
	}
	regMu.Unlock()

	for _, pair := range os.Environ() {
		k, _, _ := strings.Cut(pair, "=")
		if !strings.HasPrefix(k, "ILTER_") {
			continue
		}
		if !registered[k] {
			slog.Warn("unknown environment variable (did you mean a different name?)", "var", k)
		}
	}

	return func() {}
}

// Fallback configuration parsers removed - fallback uses sqlite config + ilter init, not environment variables
// resetForTest clears the resolved state of all registered entries so that
// the next call to Resolve / WasSet re-reads the environment.  Only safe in
// single-threaded test contexts.
func resetForTest() {
	regMu.Lock()
	defer regMu.Unlock()
	for _, entry := range registry {
		entry.resolved = false
		entry.set = false
		entry.value = nil
		entry.resolveErr = nil
	}
}

var (
	ServerPortEnv = RegisterEnv(
		"ILTER_SERVER_PORT",
		"Proxy listen port",
		8181,
		strconv.Atoi,
	)

	StoragePathEnv = RegisterEnv(
		"ILTER_STORAGE_PATH",
		"SQLite database path",
		"./data/ilter.db",
		func(s string) (string, error) { return s, nil },
	)

	LogLevelEnv = RegisterEnv(
		"ILTER_LOG_LEVEL",
		"Log level (debug, info, warn, error)",
		"info",
		func(s string) (string, error) { return s, nil },
	)

	AdminKeyEnv = RegisterEnv(
		"ILTER_ADMIN_API_KEY",
		"Admin API key (break-glass override)",
		"",
		func(s string) (string, error) { return s, nil },
	)

	RedisURLEnv = RegisterEnv(
		"ILTER_REDIS_URL",
		"Redis URL shared by cache, rate limiting, and PII backends",
		"",
		func(s string) (string, error) { return s, nil },
	)
)

// ApplyEnvOverrides sets Config fields from environment variables that were
// actually set.  Precedence (highest wins): explicit ILTER_ prefixed env
// vars, then compiled-in DefaultConfig values.
func ApplyEnvOverrides(cfg *Config) {
	if ServerPortEnv.WasSet() {
		if val, err := ServerPortEnv.ResolveE(); err != nil {
			slog.Error("invalid env var, using default", "name", "ILTER_SERVER_PORT", "error", err)
		} else {
			cfg.Server.Port = val
		}
	}
	if StoragePathEnv.WasSet() {
		if val, err := StoragePathEnv.ResolveE(); err != nil {
			slog.Error("invalid env var, using default", "name", "ILTER_STORAGE_PATH", "error", err)
		} else {
			cfg.Storage.SqlitePath = val
		}
	}
	if LogLevelEnv.WasSet() {
		if val, err := LogLevelEnv.ResolveE(); err != nil {
			slog.Error("invalid env var, using default", "name", "ILTER_LOG_LEVEL", "error", err)
		} else {
			cfg.Logging.Level = val
		}
	}
	if AdminKeyEnv.WasSet() {
		if val, err := AdminKeyEnv.ResolveE(); err != nil {
			slog.Error("invalid env var, using default", "name", "ILTER_ADMIN_API_KEY", "error", err)
		} else {
			cfg.Auth.AdminKey = val
		}
	}

	if RedisURLEnv.WasSet() {
		if val, err := RedisURLEnv.ResolveE(); err != nil {
			slog.Error("invalid env var, using default", "name", "ILTER_REDIS_URL", "error", err)
		} else {
			cfg.Cache.RedisURL = val
			cfg.RateLimit.RedisURL = val
			cfg.PII.RedisURL = val
		}
	}
}
