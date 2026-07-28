package config

import (
	"bytes"
	"log/slog"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

// identityParser returns a parser for string env vars that accepts any value.
func identityParser(s string) (string, error) { return s, nil }

// captureWarnLog runs fn with a slog logger that captures WARN+ messages into
// the returned buffer.
func captureWarnLog(fn func()) *bytes.Buffer {
	var buf bytes.Buffer
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(orig)
	fn()
	return &buf
}

// ─── Default resolution ─────────────────────────────────────────────────────

func TestEnvDefault(t *testing.T) {
	e := RegisterEnv("ILTER_Z2_DEFAULT", "test", 8181, strconv.Atoi)

	got := e.Resolve()
	assert.Equal(t, 8181, got)
	assert.False(t, e.WasSet(), "WasSet should be false when env is not set")
}

func TestEnvDefaultString(t *testing.T) {
	e := RegisterEnv("ILTER_Z2_DEFAULT_STR", "test", "fallback", identityParser)

	got := e.Resolve()
	assert.Equal(t, "fallback", got)
	assert.False(t, e.WasSet())
}

func TestEnvDefaultZero(t *testing.T) {
	e := RegisterEnv("ILTER_Z2_DEFAULT_ZERO", "test", "", identityParser)

	got := e.Resolve()
	assert.Equal(t, "", got)
	assert.False(t, e.WasSet())
}

// ─── Env override ───────────────────────────────────────────────────────────

func TestEnvOverrideInt(t *testing.T) {
	e := RegisterEnv("ILTER_Z2_OVERRIDE_INT", "test", 8181, strconv.Atoi)
	t.Setenv("ILTER_Z2_OVERRIDE_INT", "9090")

	got := e.Resolve()
	assert.Equal(t, 9090, got)
	assert.True(t, e.WasSet(), "WasSet should be true when env is set")
}

func TestEnvOverrideString(t *testing.T) {
	e := RegisterEnv("ILTER_Z2_OVERRIDE_STR", "test", "info", identityParser)
	t.Setenv("ILTER_Z2_OVERRIDE_STR", "debug")

	got := e.Resolve()
	assert.Equal(t, "debug", got)
	assert.True(t, e.WasSet())
}

// ─── WasSet before Resolve ──────────────────────────────────────────────────

func TestWasSetBeforeResolve(t *testing.T) {
	e := RegisterEnv("ILTER_Z2_SET_BEFORE_RESOLVE", "test", "default", identityParser)
	t.Setenv("ILTER_Z2_SET_BEFORE_RESOLVE", "override")

	// WasSet should trigger env lookup even without Resolve being called first.
	assert.True(t, e.WasSet(), "WasSet should find the env var before Resolve is called")
	assert.Equal(t, "override", e.Resolve(), "Resolve should return the same value after WasSet")
}

func TestWasSetAfterResolve(t *testing.T) {
	e := RegisterEnv("ILTER_Z2_SET_AFTER_RESOLVE", "test", "default", identityParser)
	t.Setenv("ILTER_Z2_SET_AFTER_RESOLVE", "override")

	_ = e.Resolve()
	assert.True(t, e.WasSet())
}

func TestWasSetNotSet(t *testing.T) {
	e := RegisterEnv("ILTER_Z2_NOT_SET", "test", "default", identityParser)

	assert.False(t, e.WasSet(), "WasSet should be false when no env var")
	assert.Equal(t, "default", e.Resolve())
}

// ─── Parse error ────────────────────────────────────────────────────────────

func TestEnvParseError(t *testing.T) {
	e := RegisterEnv("ILTER_Z2_BAD_INT", "test", 0, strconv.Atoi)
	t.Setenv("ILTER_Z2_BAD_INT", "not-a-number")

	_, err := e.ResolveE()
	assert.Error(t, err, "ResolveE should return error on parse failure")
	// Resolve should fall back to default without panic
	assert.Equal(t, 0, e.Resolve())
}

func TestEnvParseErrorEmptyStringForInt(t *testing.T) {
	e := RegisterEnv("ILTER_Z2_EMPTY_INT", "test", 0, strconv.Atoi)
	t.Setenv("ILTER_Z2_EMPTY_INT", "")

	_, err := e.ResolveE()
	assert.Error(t, err, "ResolveE should return error on empty string for int parser")
	// Resolve should fall back to default without panic
	assert.Equal(t, 0, e.Resolve())
}

func TestEnvParseErrorInvalidPort(t *testing.T) {
	e := RegisterEnv("ILTER_Z2_NEG_PORT", "test", 8080, strconv.Atoi)
	t.Setenv("ILTER_Z2_NEG_PORT", "-1abc")

	_, err := e.ResolveE()
	assert.Error(t, err)
	// Resolve should fall back to default without panic
	assert.Equal(t, 8080, e.Resolve())
}

// ─── WarnUnknownEnv ─────────────────────────────────────────────────────────

func TestWarnUnknownEnv_WarnsOnUnknown(t *testing.T) {
	t.Setenv("ILTER_Z2_MISSPELLED_PORT", "9090")

	buf := captureWarnLog(func() {
		WarnUnknownEnv()()
	})

	assert.Contains(t, buf.String(), "ILTER_Z2_MISSPELLED_PORT",
		"should warn about unknown ILTER_ env var")
}

func TestWarnUnknownEnv_DoesNotWarnOnRegistered(t *testing.T) {
	t.Setenv("ILTER_SERVER_PORT", "9090")

	buf := captureWarnLog(func() {
		WarnUnknownEnv()()
	})

	assert.NotContains(t, buf.String(), "ILTER_SERVER_PORT",
		"should NOT warn about registered ILTER_SERVER_PORT")
}

func TestWarnUnknownEnv_SkipsNonIlterVars(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("HOME", "/root")

	buf := captureWarnLog(func() {
		WarnUnknownEnv()()
	})

	// Just verify it doesn't panic and doesn't mention PATH or HOME.
	assert.NotContains(t, buf.String(), "PATH")
	assert.NotContains(t, buf.String(), "HOME")
}

func TestWarnUnknownEnv_WarnsMultipleUnknown(t *testing.T) {
	t.Setenv("ILTER_Z2_UNKNOWN_A", "a")
	t.Setenv("ILTER_Z2_UNKNOWN_B", "b")
	t.Setenv("ILTER_SERVER_PORT", "8181") // known

	buf := captureWarnLog(func() {
		WarnUnknownEnv()()
	})

	output := buf.String()
	assert.Contains(t, output, "ILTER_Z2_UNKNOWN_A")
	assert.Contains(t, output, "ILTER_Z2_UNKNOWN_B")
	// Count "WARN" level lines — at least 2.
	assert.GreaterOrEqual(t, strings.Count(output, "WARN"), 2)
}

// ─── All() ──────────────────────────────────────────────────────────────────

func TestAll_ContainsGlobals(t *testing.T) {
	snaps := All()

	names := make(map[string]bool, len(snaps))
	for _, s := range snaps {
		names[s.Name] = true
	}

	globals := []string{
		"ILTER_SERVER_PORT",
		"ILTER_STORAGE_PATH",
		"ILTER_LOG_LEVEL",
		"ILTER_ADMIN_API_KEY",
	}
	for _, g := range globals {
		assert.True(t, names[g], "All() should include %s", g)
	}
}

func TestAll_IncludesLocallyRegistered(t *testing.T) {
	e := RegisterEnv("ILTER_Z2_ALL_LOCAL", "test-local", 42, strconv.Atoi)

	snaps := All()
	found := false
	for _, s := range snaps {
		if s.Name == "ILTER_Z2_ALL_LOCAL" {
			found = true
			assert.Equal(t, 42, s.Default)
			assert.Equal(t, 42, s.Value)
			assert.False(t, s.Set)
			break
		}
	}
	assert.True(t, found, "All() should include locally registered var")

	// Cleanup: this test registered a binding that persists. That's fine for
	// the test package scope since other tests won't reference it.
	_ = e
}

func TestAll_ReflectsResolvedState(t *testing.T) {
	e := RegisterEnv("ILTER_Z2_ALL_RESOLVED", "test", "initial", identityParser)
	t.Setenv("ILTER_Z2_ALL_RESOLVED", "overridden")

	_ = e.Resolve() // resolves and caches "overridden"

	snaps := All()
	for _, s := range snaps {
		if s.Name == "ILTER_Z2_ALL_RESOLVED" {
			assert.Equal(t, "overridden", s.Value)
			assert.True(t, s.Set)
			break
		}
	}
}

// ─── ApplyEnvOverrides ──────────────────────────────────────────────────────

func TestApplyEnvOverrides(t *testing.T) {
	resetForTest()

	t.Setenv("ILTER_SERVER_PORT", "9090")
	t.Setenv("ILTER_LOG_LEVEL", "debug")
	t.Setenv("ILTER_STORAGE_PATH", "/tmp/ilter-test.db")
	t.Setenv("ILTER_ADMIN_API_KEY", "env-admin-key")

	cfg := DefaultConfig()
	ApplyEnvOverrides(&cfg)

	assert.Equal(t, 9090, cfg.Server.Port)
	assert.Equal(t, "debug", cfg.Logging.Level)
	assert.Equal(t, "/tmp/ilter-test.db", cfg.Storage.SqlitePath)
	assert.Equal(t, "env-admin-key", cfg.Auth.AdminKey)
}

func TestApplyEnvOverrides_OnlyEnvOverrides(t *testing.T) {
	resetForTest()
	// Only set one var; others should stay at defaults.
	t.Setenv("ILTER_SERVER_PORT", "9090")

	cfg := DefaultConfig()
	ApplyEnvOverrides(&cfg)

	assert.Equal(t, 9090, cfg.Server.Port)
	assert.Equal(t, "info", cfg.Logging.Level, "unset env should not change log level")
	assert.Equal(t, "./data/ilter.db", cfg.Storage.SqlitePath, "default storage path from DefaultConfig")
}

func TestApplyEnvOverrides_EmptyNotOverride(t *testing.T) {
	resetForTest()
	// Setting an env var to empty string should still trigger WasSet.
	t.Setenv("ILTER_ADMIN_API_KEY", "")

	cfg := DefaultConfig()
	cfg.Auth.AdminKey = "default-key"

	ApplyEnvOverrides(&cfg)

	assert.Equal(t, "", cfg.Auth.AdminKey, "empty env var should override default value")
}

func TestApplyEnvOverrides_AllRegisteredVarsApply(t *testing.T) {
	// Table-driven test that proves every registered ILTER_ env var (not test
	// vars) actually mutates a Config field via ApplyEnvOverrides.
	// Each case has an envValue (what we set in the environment) and a want
	// (the expected Config field value after ApplyEnvOverrides).
	type testCase struct {
		name     string
		envName  string
		envValue string
		want     string
		getter   func(cfg *Config) string
	}

	cases := []testCase{
		{
			name:     "server_port",
			envName:  "ILTER_SERVER_PORT",
			envValue: "9999",
			want:     "9999",
			getter:   func(cfg *Config) string { return strconv.Itoa(cfg.Server.Port) },
		},
		{
			name:     "storage_path",
			envName:  "ILTER_STORAGE_PATH",
			envValue: "/tmp/env-test-store",
			want:     "/tmp/env-test-store",
			getter:   func(cfg *Config) string { return cfg.Storage.SqlitePath },
		},
		{
			name:     "log_level",
			envName:  "ILTER_LOG_LEVEL",
			envValue: "warn",
			want:     "warn",
			getter:   func(cfg *Config) string { return cfg.Logging.Level },
		},
		{
			name:     "admin_key",
			envName:  "ILTER_ADMIN_API_KEY",
			envValue: "env-admin-key",
			want:     "env-admin-key",
			getter:   func(cfg *Config) string { return cfg.Auth.AdminKey },
		},
		{
			name:     "redis_url",
			envName:  "ILTER_REDIS_URL",
			envValue: "redis://myredis:6379",
			want:     "redis://myredis:6379",
			getter:   func(cfg *Config) string { return cfg.Cache.RedisURL },
		},
	}

	// Count only production-registered vars (skip ILTER_Z2_* test registrations
	// from other tests that also live in the package-level registry).
	registered := RegisteredEnvVars()
	var prodCount int
	for _, name := range registered {
		if strings.HasPrefix(name, "ILTER_") && !strings.HasPrefix(name, "ILTER_Z2") {
			prodCount++
		}
	}
	if len(cases) != prodCount {
		t.Fatalf("test cases count (%d) does not match registered non-test env vars (%d); add or remove test cases to match",
			len(cases), prodCount)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetForTest()
			cfg := DefaultConfig()
			t.Setenv(tc.envName, tc.envValue)
			ApplyEnvOverrides(&cfg)
			if got := tc.getter(&cfg); got != tc.want {
				t.Errorf("ApplyEnvOverrides(%s): got %q, want %q", tc.envName, got, tc.want)
			}
		})
	}
}

// ─── Unique name collision ──────────────────────────────────────────────────

func TestEnvUniqueNames(t *testing.T) {
	// Register two vars with the same name.  The registry allows duplicates,
	// but each binding is independent.
	e1 := RegisterEnv("ILTER_Z2_DUP", "first", "a", identityParser)
	e2 := RegisterEnv("ILTER_Z2_DUP", "second", "b", identityParser)

	// Each should resolve independently (no shared state).
	assert.Equal(t, "a", e1.Resolve())
	assert.Equal(t, "b", e2.Resolve())
	assert.False(t, e1.WasSet())
	assert.False(t, e2.WasSet())
}
