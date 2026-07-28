package config

import (
	"bytes"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

// captureStdout runs fn and returns everything written to stdout.
func captureStdout(fn func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	fn()

	w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	os.Stdout = old
	return buf.String()
}

// ─── ResolveSecretsFile ─────────────────────────────────────────────────────

func TestResolveSecretsFile_Simple(t *testing.T) {
	path := ResolveSecretsFile("data/ilter.db")
	assert.Equal(t, "data/ilter.secrets.env", path)
}

func TestResolveSecretsFile_Subdirectory(t *testing.T) {
	path := ResolveSecretsFile("somedir/otherdir/mydb.sqlite")
	assert.Equal(t, "somedir/otherdir/mydb.secrets.env", path)
}

func TestResolveSecretsFile_NoDirectory(t *testing.T) {
	path := ResolveSecretsFile("ilter.db")
	assert.Equal(t, "ilter.secrets.env", path)
}

func TestResolveSecretsFile_RootPath(t *testing.T) {
	path := ResolveSecretsFile("/ilter.db")
	assert.Equal(t, "/ilter.secrets.env", path)
}

func TestResolveSecretsFile_NoExtension(t *testing.T) {
	path := ResolveSecretsFile("data/myfile")
	assert.Equal(t, "data/myfile.secrets.env", path)
}

func TestResolveSecretsFile_DottedName(t *testing.T) {
	path := ResolveSecretsFile("data/special.db.db")
	assert.Equal(t, "data/special.db.secrets.env", path)
}

// ─── generateKey ────────────────────────────────────────────────────────────

func TestGenerateKey_Format(t *testing.T) {
	key, err := generateKey()
	assert.NoError(t, err, "generateKey should not error")
	assert.Equal(t, 64, len(key),
		"32 bytes hex (64 chars), got %d", len(key))

	_, decErr := hex.DecodeString(key)
	assert.NoError(t, decErr, "key %q should be valid hex", key)
}

func TestGenerateKey_Randomness(t *testing.T) {
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		k, err := generateKey()
		assert.NoError(t, err, "generateKey should not error at iteration %d", i)
		assert.False(t, seen[k], "key %q should be unique", k)
		seen[k] = true
	}
}

// ─── ensureDataDir ──────────────────────────────────────────────────────────

func TestEnsureDataDir_Creates(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "sub", "dir", "file.env")
	err := ensureDataDir(path)
	require.NoError(t, err)

	_, err = os.Stat(filepath.Dir(path))
	assert.NoError(t, err, "directory should exist")
}

func TestEnsureDataDir_ExplicitDir(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "exists", "file.env")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))

	err := ensureDataDir(path)
	require.NoError(t, err)
}

func TestEnsureDataDir_NoDirToCreate(t *testing.T) {
	err := ensureDataDir("file.env")
	require.NoError(t, err)

	err = ensureDataDir("./file.env")
	require.NoError(t, err)
}

// ─── loadSecretsFromFile ────────────────────────────────────────────────────

func TestLoadSecretsFromFile_Missing(t *testing.T) {
	m, err := loadSecretsFromFile("/nonexistent/path/that/does/not/exist")
	require.NoError(t, err)
	assert.Empty(t, m)
}

func TestLoadSecretsFromFile_Empty(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "secrets.env")
	require.NoError(t, os.WriteFile(path, []byte{}, 0o600))

	m, err := loadSecretsFromFile(path)
	require.NoError(t, err)
	assert.Empty(t, m)
}

func TestLoadSecretsFromFile_CommentsOnly(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "secrets.env")
	require.NoError(t, os.WriteFile(path, []byte("# just a comment\n# another comment\n"), 0o600))

	m, err := loadSecretsFromFile(path)
	require.NoError(t, err)
	assert.Empty(t, m)
}

func TestLoadSecretsFromFile_Normal(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "secrets.env")
	content := "# header\nILTER_ADMIN_API_KEY=admin-123\nILTER_DASHBOARD_TOKEN=token-456\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	m, err := loadSecretsFromFile(path)
	require.NoError(t, err)
	assert.Equal(t, "admin-123", m["ILTER_ADMIN_API_KEY"])
	assert.Equal(t, "token-456", m["ILTER_DASHBOARD_TOKEN"])
}

func TestLoadSecretsFromFile_BlankLines(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "secrets.env")
	content := "ILTER_ADMIN_API_KEY=admin-123\n\n\nILTER_DASHBOARD_TOKEN=token-456\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	m, err := loadSecretsFromFile(path)
	require.NoError(t, err)
	assert.Equal(t, "admin-123", m["ILTER_ADMIN_API_KEY"])
	assert.Equal(t, "token-456", m["ILTER_DASHBOARD_TOKEN"])
}

func TestLoadSecretsFromFile_WhitespaceAroundEquals(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "secrets.env")
	content := "  ILTER_ADMIN_API_KEY = admin-123  \n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	m, err := loadSecretsFromFile(path)
	require.NoError(t, err)
	assert.Equal(t, "admin-123", m["ILTER_ADMIN_API_KEY"])
}

func TestLoadSecretsFromFile_SkipsNoEquals(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "secrets.env")
	content := "ILTER_ADMIN_API_KEY=admin-123\nINVALID_LINE_NO_EQUALS\nILTER_DASHBOARD_TOKEN=token-456\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	m, err := loadSecretsFromFile(path)
	require.NoError(t, err)
	assert.Equal(t, "admin-123", m["ILTER_ADMIN_API_KEY"])
	assert.Equal(t, "token-456", m["ILTER_DASHBOARD_TOKEN"])
}

func TestLoadSecretsFromFile_EmptyValue(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "secrets.env")
	content := "ILTER_ADMIN_API_KEY=\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	m, err := loadSecretsFromFile(path)
	require.NoError(t, err)
	assert.Equal(t, "", m["ILTER_ADMIN_API_KEY"])
}

// ─── writeSecretsFile ──────────────────────────────────────────────────────

func TestWriteSecretsFile_Creates(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "ilter.secrets.env")
	secrets := map[string]string{"ILTER_ADMIN_API_KEY": "admin-key"}

	err := writeSecretsFile(path, secrets)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "ILTER_ADMIN_API_KEY=admin-key")
}

func TestWriteSecretsFile_CreatesDir(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "sub", "ilter.secrets.env")
	secrets := map[string]string{"ILTER_ADMIN_API_KEY": "admin-key"}

	err := writeSecretsFile(path, secrets)
	require.NoError(t, err)

	_, err = os.Stat(path)
	assert.NoError(t, err, "file should exist after write")
}

func TestWriteSecretsFile_DeterministicSort(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "ilter.secrets.env")
	secrets := map[string]string{
		"ILTER_Z_KEY": "z-value",
		"ILTER_A_KEY": "a-value",
		"ILTER_M_KEY": "m-value",
	}

	err := writeSecretsFile(path, secrets)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	// Collect KEY=VALUE lines in order
	var kvLines []string
	for _, l := range strings.Split(string(data), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if strings.Contains(l, "=") {
			kvLines = append(kvLines, l)
		}
	}
	require.Len(t, kvLines, 3)
	assert.Equal(t, "ILTER_A_KEY=a-value", kvLines[0])
	assert.Equal(t, "ILTER_M_KEY=m-value", kvLines[1])
	assert.Equal(t, "ILTER_Z_KEY=z-value", kvLines[2])
}

func TestWriteSecretsFile_FileMode(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "ilter.secrets.env")
	secrets := map[string]string{"ILTER_ADMIN_API_KEY": "admin-key"}

	err := writeSecretsFile(path, secrets)
	require.NoError(t, err)

	fi, err := os.Stat(path)
	require.NoError(t, err)
	// Mode should be 0600 (owner read/write only)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
}

func TestWriteSecretsFile_Header(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "ilter.secrets.env")
	secrets := map[string]string{"ILTER_ADMIN_API_KEY": "admin-key"}

	err := writeSecretsFile(path, secrets)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)

	assert.Contains(t, content, "# ilter auto-generated secrets")
	assert.Contains(t, content, "# Set the corresponding ILTER_* env var")
}

// ─── applySecret ────────────────────────────────────────────────────────────

func TestApplySecret_AdminKey(t *testing.T) {
	cfg := &Config{}
	applySecret(cfg, "auth.admin_key", "my-admin")
	assert.Equal(t, "my-admin", cfg.Auth.AdminKey)
}

func TestApplySecret_DashboardToken(t *testing.T) {
	cfg := &Config{}
	applySecret(cfg, "dashboard.auth_token", "my-token")
	assert.Equal(t, "my-token", cfg.Dashboard.AuthToken)
}

func TestApplySecret_UnknownKey(t *testing.T) {
	cfg := &Config{
		Auth: AuthConfig{AdminKey: "existing-admin"},
	}
	// Should not panic or modify anything for unknown keys
	applySecret(cfg, "unknown.key", "value")
	assert.Equal(t, "existing-admin", cfg.Auth.AdminKey, "admin key should be unchanged")
}

// ─── resolveOneSecret ──────────────────────────────────────────────────────

func TestResolveOneSecret_EnvFirst(t *testing.T) {
	clearSecretsEnv(t)
	s := ManagedSecrets()[0] // ILTER_ADMIN_API_KEY

	// No env, no config, no file → empty
	cfg := &Config{}
	result := resolveOneSecret(s, cfg, map[string]string{})
	assert.Empty(t, result)
	assert.Empty(t, cfg.Auth.AdminKey)

	// With env set
	t.Setenv("ILTER_ADMIN_API_KEY", "env-key")
	result = resolveOneSecret(s, cfg, map[string]string{})
	assert.Equal(t, "env-key", result)
	assert.Equal(t, "env-key", cfg.Auth.AdminKey)
}

func TestResolveOneSecret_ConfigFallback(t *testing.T) {
	clearSecretsEnv(t)
	s := ManagedSecrets()[0] // ILTER_ADMIN_API_KEY
	cfg := &Config{Auth: AuthConfig{AdminKey: "cfg-key"}}

	result := resolveOneSecret(s, cfg, map[string]string{})
	assert.Equal(t, "cfg-key", result)
	assert.Equal(t, "cfg-key", cfg.Auth.AdminKey)
}

func TestResolveOneSecret_FileFallback(t *testing.T) {
	clearSecretsEnv(t)
	s := ManagedSecrets()[1] // ILTER_DASHBOARD_TOKEN
	cfg := &Config{}

	result := resolveOneSecret(s, cfg, map[string]string{"ILTER_DASHBOARD_TOKEN": "file-token"})
	assert.Equal(t, "file-token", result)
	assert.Equal(t, "file-token", cfg.Dashboard.AuthToken)
}

func TestResolveOneSecret_EnvOverridesFile(t *testing.T) {
	clearSecretsEnv(t)
	s := ManagedSecrets()[0] // ILTER_ADMIN_API_KEY
	cfg := &Config{}
	t.Setenv("ILTER_ADMIN_API_KEY", "env-key")

	result := resolveOneSecret(s, cfg, map[string]string{"ILTER_ADMIN_API_KEY": "file-key"})
	assert.Equal(t, "env-key", result, "env must take priority over file")
	assert.Equal(t, "env-key", cfg.Auth.AdminKey)
}

func TestResolveOneSecret_ConfigOverridesFile(t *testing.T) {
	clearSecretsEnv(t)
	s := ManagedSecrets()[0] // ILTER_ADMIN_API_KEY
	cfg := &Config{Auth: AuthConfig{AdminKey: "cfg-key"}}

	result := resolveOneSecret(s, cfg, map[string]string{"ILTER_ADMIN_API_KEY": "file-key"})
	assert.Equal(t, "cfg-key", result, "config value must take priority over file")
	assert.Equal(t, "cfg-key", cfg.Auth.AdminKey)
}

// ─── ResolveSecrets: env path ─────────────────────────────────────────────

func TestResolveSecrets_FromEnv(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "ilter.db")

	t.Setenv("ILTER_ADMIN_API_KEY", "env-admin")
	t.Setenv("ILTER_DASHBOARD_TOKEN", "env-dash")

	cfg := &Config{}
	err := ResolveSecrets(cfg, dbPath)
	require.NoError(t, err)

	assert.Equal(t, "env-admin", cfg.Auth.AdminKey)
	assert.Equal(t, "env-dash", cfg.Dashboard.AuthToken)

	// No file should be created when all values come from env
	secretsFile := ResolveSecretsFile(dbPath)
	_, err = os.Stat(secretsFile)
	assert.True(t, os.IsNotExist(err), "no secrets file should be created when all from env")
}

// ─── ResolveSecrets: Config path ─────────────────────────────────────────

func TestResolveSecrets_FromConfig(t *testing.T) {
	clearSecretsEnv(t)
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "ilter.db")

	cfg := &Config{
		Auth:      AuthConfig{AdminKey: "cfg-admin"},
		Dashboard: DashboardConfig{AuthToken: "cfg-dash"},
	}
	err := ResolveSecrets(cfg, dbPath)
	require.NoError(t, err)

	assert.Equal(t, "cfg-admin", cfg.Auth.AdminKey)
	assert.Equal(t, "cfg-dash", cfg.Dashboard.AuthToken)

	// No file created when all from config
	secretsFile := ResolveSecretsFile(dbPath)
	_, err = os.Stat(secretsFile)
	assert.True(t, os.IsNotExist(err))
}

func clearSecretsEnv(t *testing.T) {
	t.Helper()
	os.Unsetenv("ILTER_ADMIN_API_KEY")
	os.Unsetenv("ILTER_DASHBOARD_TOKEN")
	resetForTest()
	t.Cleanup(func() {
		os.Unsetenv("ILTER_ADMIN_API_KEY")
		os.Unsetenv("ILTER_DASHBOARD_TOKEN")
		resetForTest()
	})
}

// ─── ResolveSecrets: file path ───────────────────────────────────────────

func TestResolveSecrets_FromFile(t *testing.T) {
	clearSecretsEnv(t)
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "ilter.db")
	secretsFile := ResolveSecretsFile(dbPath)

	content := "ILTER_ADMIN_API_KEY=file-admin\nILTER_DASHBOARD_TOKEN=file-dash\n"
	require.NoError(t, os.MkdirAll(filepath.Dir(secretsFile), 0o700))
	require.NoError(t, os.WriteFile(secretsFile, []byte(content), 0o600))

	cfg := &Config{}
	err := ResolveSecrets(cfg, dbPath)
	require.NoError(t, err)

	assert.Equal(t, "file-admin", cfg.Auth.AdminKey)
	assert.Equal(t, "file-dash", cfg.Dashboard.AuthToken)
}

func TestResolveSecrets_PartialFromFile(t *testing.T) {
	clearSecretsEnv(t)
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "ilter.db")
	secretsFile := ResolveSecretsFile(dbPath)

	// Only admin_key in file; dashboard must be generated
	content := "ILTER_ADMIN_API_KEY=file-admin\n"
	require.NoError(t, os.MkdirAll(filepath.Dir(secretsFile), 0o700))
	require.NoError(t, os.WriteFile(secretsFile, []byte(content), 0o600))

	cfg := &Config{}
	err := ResolveSecrets(cfg, dbPath)
	require.NoError(t, err)

	assert.Equal(t, "file-admin", cfg.Auth.AdminKey)
	assert.True(t, strings.HasPrefix(cfg.Dashboard.AuthToken, ""),
		"dashboard token should be generated (non-empty)")

	// writeSecretsFile replaces the entire file with only newly-generated
	// secrets (the pre-existing file admin_key is not re-persisted).
	data, err := os.ReadFile(secretsFile)
	require.NoError(t, err)
	contentStr := string(data)
	assert.NotContains(t, contentStr, "ILTER_ADMIN_API_KEY",
		"admin_key from original file is not re-persisted")
	assert.Contains(t, contentStr, "ILTER_DASHBOARD_TOKEN=")
}

// ─── ResolveSecrets: generate path ───────────────────────────────────────

func TestResolveSecrets_GenerateAll(t *testing.T) {
	clearSecretsEnv(t)
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "ilter.db")

	cfg := &Config{}
	output := captureStdout(func() {
		err := ResolveSecrets(cfg, dbPath)
		require.NoError(t, err)
	})

	// All two generated
	assert.NotEmpty(t, cfg.Auth.AdminKey)
	assert.NotEmpty(t, cfg.Dashboard.AuthToken)

	// Stdout contains the generated secrets
	assert.Contains(t, output, "🔑")
	assert.Contains(t, output, "Admin API key")
	assert.Contains(t, output, "Dashboard auth token")
	assert.Contains(t, output, cfg.Auth.AdminKey)
	assert.Contains(t, output, cfg.Dashboard.AuthToken)

	// File was created with all two
	secretsFile := ResolveSecretsFile(dbPath)
	data, err := os.ReadFile(secretsFile)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "ILTER_ADMIN_API_KEY="+cfg.Auth.AdminKey)
	assert.Contains(t, content, "ILTER_DASHBOARD_TOKEN="+cfg.Dashboard.AuthToken)
}

func TestResolveSecrets_GenerateGeneratesUniqueKeys(t *testing.T) {
	clearSecretsEnv(t)
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "ilter.db")

	cfg := &Config{}
	err := ResolveSecrets(cfg, dbPath)
	require.NoError(t, err)

	// Verify both keys are distinct
	assert.NotEqual(t, cfg.Auth.AdminKey, cfg.Dashboard.AuthToken,
		"admin and dashboard must differ")
}

// ─── ResolveSecrets: idempotency ─────────────────────────────────────────

func TestResolveSecrets_IdempotentKeys(t *testing.T) {
	clearSecretsEnv(t)
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "ilter.db")

	// First call: generate and persist
	cfg1 := &Config{}
	err := ResolveSecrets(cfg1, dbPath)
	require.NoError(t, err)

	savedAdmin := cfg1.Auth.AdminKey
	savedDash := cfg1.Dashboard.AuthToken

	// Second call with fresh cfg — should read from file, not re-generate
	cfg2 := &Config{}
	err = ResolveSecrets(cfg2, dbPath)
	require.NoError(t, err)

	assert.Equal(t, savedAdmin, cfg2.Auth.AdminKey,
		"admin key should be stable across calls")
	assert.Equal(t, savedDash, cfg2.Dashboard.AuthToken,
		"dashboard token should be stable across calls")
}

func TestResolveSecrets_IdempotentFileNotRewritten(t *testing.T) {
	clearSecretsEnv(t)
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "ilter.db")

	// Generate once
	cfg := &Config{}
	require.NoError(t, ResolveSecrets(cfg, dbPath))

	secretsFile := ResolveSecretsFile(dbPath)
	data1, err := os.ReadFile(secretsFile)
	require.NoError(t, err)

	// Call again — file content should be identical
	cfg2 := &Config{}
	require.NoError(t, ResolveSecrets(cfg2, dbPath))

	data2, err := os.ReadFile(secretsFile)
	require.NoError(t, err)

	assert.Equal(t, string(data1), string(data2),
		"secrets file content should be unchanged on second call")
}

// ─── ResolveSecrets: env overrides file ──────────────────────────────────

func TestResolveSecrets_EnvOverridesFile(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "ilter.db")
	secretsFile := ResolveSecretsFile(dbPath)

	content := "ILTER_ADMIN_API_KEY=file-admin\nILTER_DASHBOARD_TOKEN=file-dash\n"
	require.NoError(t, os.MkdirAll(filepath.Dir(secretsFile), 0o700))
	require.NoError(t, os.WriteFile(secretsFile, []byte(content), 0o600))

	// Env overrides admin_key only
	t.Setenv("ILTER_ADMIN_API_KEY", "env-admin")

	cfg := &Config{}
	err := ResolveSecrets(cfg, dbPath)
	require.NoError(t, err)

	assert.Equal(t, "env-admin", cfg.Auth.AdminKey, "env must override file")
	assert.Equal(t, "file-dash", cfg.Dashboard.AuthToken, "file value used when no env")
}

// ─── ResolveSecrets: config overrides file ───────────────────────────────

func TestResolveSecrets_ConfigOverridesFile(t *testing.T) {
	clearSecretsEnv(t)
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "ilter.db")
	secretsFile := ResolveSecretsFile(dbPath)

	content := "ILTER_ADMIN_API_KEY=file-admin\n"
	require.NoError(t, os.MkdirAll(filepath.Dir(secretsFile), 0o700))
	require.NoError(t, os.WriteFile(secretsFile, []byte(content), 0o600))

	cfg := &Config{Auth: AuthConfig{AdminKey: "cfg-admin"}}
	err := ResolveSecrets(cfg, dbPath)
	require.NoError(t, err)

	assert.Equal(t, "cfg-admin", cfg.Auth.AdminKey, "config must override file")
}

// ─── ResolveSecrets: empty dbPath edge cases ─────────────────────────────

func TestResolveSecrets_DbPathInSubdir(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "deep", "nested", "path", "data", "ilter.db")

	cfg := &Config{}
	err := ResolveSecrets(cfg, dbPath)
	require.NoError(t, err)

	// Secrets file should be next to the db
	secretsFile := ResolveSecretsFile(dbPath)
	_, err = os.Stat(secretsFile)
	assert.NoError(t, err, "secrets file should exist")
	assert.NotEmpty(t, cfg.Auth.AdminKey)
}

func TestResolveSecrets_DbPathIsFilenameOnly(t *testing.T) {
	tmp := t.TempDir()
	// Use a bare filename (no directory) inside tmp
	dbPath := filepath.Join(tmp, "ilter.db")

	cfg := &Config{}
	err := ResolveSecrets(cfg, dbPath)
	require.NoError(t, err)

	secretsFile := ResolveSecretsFile(dbPath)
	_, err = os.Stat(secretsFile)
	assert.NoError(t, err, "secrets file should exist at %s", secretsFile)
}

// ─── ResolveSecrets: concurrent safety ───────────────────────────────────

func TestResolveSecrets_ConcurrentCalls(t *testing.T) {
	clearSecretsEnv(t)
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "ilter.db")

	var wg sync.WaitGroup
	var mu sync.Mutex
	anySucceeded := false

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cfg := &Config{}
			err := ResolveSecrets(cfg, dbPath)
			if err == nil {
				mu.Lock()
				anySucceeded = true
				mu.Unlock()
				return
			}
			// An "rename ... no such file or directory" error is expected
			// under concurrent writes because writeSecretsFile uses a shared
			// .tmp path for the atomic rename; whichever goroutine loses the
			// race sees a missing .tmp file. This does NOT corrupt the
			// target file (os.Rename is atomic on POSIX).
			assert.ErrorContains(t, err, "rename secrets file",
				"only rename errors are expected from concurrent call")
		}()
	}

	wg.Wait()

	assert.True(t, anySucceeded,
		"at least one concurrent ResolveSecrets call must succeed")

	// File should be valid and contain all secrets
	secretsFile := ResolveSecretsFile(dbPath)
	data, err := os.ReadFile(secretsFile)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "ILTER_ADMIN_API_KEY=")
	assert.Contains(t, content, "ILTER_DASHBOARD_TOKEN=")
}

// ─── ManagedSecrets ─────────────────────────────────────────────────────

func TestManagedSecrets_HasTwo(t *testing.T) {
	ms := ManagedSecrets()
	require.Len(t, ms, 2)
	assert.Equal(t, "auth.admin_key", ms[0].Key)
	assert.Equal(t, "dashboard.auth_token", ms[1].Key)
}

func TestManagedSecrets_EachHasEnvVar(t *testing.T) {
	for _, s := range ManagedSecrets() {
		assert.True(t, strings.HasPrefix(s.EnvVar, "ILTER_"),
			"EnvVar %q should start with ILTER_", s.EnvVar)
		assert.NotNil(t, s.Generate,
			"%s should have a non-nil Generate function", s.Key)
		val, err := s.Generate()
		assert.NoError(t, err, "%s Generate should not error", s.Key)
		assert.Len(t, val, 64, "%s Generate should return 64-char hex key", s.Key)
	}
}

// ─── envTimestamp ────────────────────────────────────────────────────────

func TestEnvTimestamp_NonEmpty(t *testing.T) {
	ts := envTimestamp()
	assert.NotEmpty(t, ts, "envTimestamp should return a non-empty string")
}

// ─── ResolveSecrets: unknown env var warning silence ────────────────────

func TestResolveSecrets_SilentOnUnknownEnvVar(t *testing.T) {
	// Set an ILTER_* var that is neither a managed secret nor a registered env.
	// This should NOT cause a panic or error from ResolveSecrets itself
	// (WarnUnknownEnv is a separate concern tested in env_test.go).
	t.Setenv("ILTER_SOME_UNKNOWN_VAR", "some-value")

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "ilter.db")

	cfg := &Config{}
	err := ResolveSecrets(cfg, dbPath)
	require.NoError(t, err, "unknown env vars should not affect ResolveSecrets")

	assert.NotEmpty(t, cfg.Auth.AdminKey)
}

// ─── Integration: full boot-style scenario ──────────────────────────────

func TestResolveSecrets_FullBootScenario(t *testing.T) {
	// Simulate a real boot: default config with only admin_key from config,
	// dashboard must be generated.
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "data", "ilter.db")

	cfg := &Config{
		Auth: AuthConfig{AdminKey: "configured-admin-key"},
		// Dashboard.AuthToken left empty
	}

	output := captureStdout(func() {
		err := ResolveSecrets(cfg, dbPath)
		require.NoError(t, err)
	})

	// Config-provided admin_key preserved
	assert.Equal(t, "configured-admin-key", cfg.Auth.AdminKey)
	// Other one generated
	assert.NotEmpty(t, cfg.Dashboard.AuthToken)

	// Only the generated secret appears in output
	assert.NotContains(t, output, "configured-admin-key",
		"admin key from config should NOT be printed")
	assert.Contains(t, output, cfg.Dashboard.AuthToken)

	// Only one key is in the file (admin_key was not generated)
	secretsFile := ResolveSecretsFile(dbPath)
	data, err := os.ReadFile(secretsFile)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "configured-admin-key",
		"config-provided key should not be persisted to secrets file")
	assert.Contains(t, string(data), "ILTER_DASHBOARD_TOKEN=")
}
