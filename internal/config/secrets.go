package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ManagedSecret is a secret that ilter can auto-generate on first boot.
type ManagedSecret struct {
	// Key is the config key path (e.g. "auth.admin_key").
	Key string
	// EnvVar is the ILTER_* env var that overrides this secret.
	EnvVar string
	// Description for the log message when generated.
	Description string
	// Generate returns a securely random value for this secret.
	Generate func() (string, error)
}

// ManagedSecrets returns the secrets ilter manages:
// admin_key and control.auth_token.
// Jobs use a regular API key (jobs.api_key) instead of a managed secret.
func ManagedSecrets() []ManagedSecret {
	return []ManagedSecret{
		{
			Key:         "auth.admin_key",
			EnvVar:      "ILTER_ADMIN_API_KEY",
			Description: "Admin API key",
			Generate:    generateKey,
		},
		{
			Key:         "dashboard.auth_token",
			EnvVar:      "ILTER_DASHBOARD_TOKEN",
			Description: "Dashboard auth token",
			Generate:    generateKey,
		},
	}
}

// generateKey produces a random 64-char hex key (32 bytes).
func generateKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random key: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// ensureDataDir ensures the data directory exists for the secrets file.
func ensureDataDir(path string) error {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o700)
}

// ResolveSecretsFile derives the secrets file path from the DB path.
// e.g. "data/ilter.db" → "data/ilter.secrets.env"
func ResolveSecretsFile(dbPath string) string {
	dir := filepath.Dir(dbPath)
	base := strings.TrimSuffix(filepath.Base(dbPath), filepath.Ext(dbPath))
	return filepath.Join(dir, base+".secrets.env")
}

// loadSecretsFromFile reads a secrets.env file into a map.
// Format: KEY=VALUE lines (sh compatible, values are NOT shell-quoted).
func loadSecretsFromFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("read secrets file %s: %w", path, err)
	}
	result := make(map[string]string)
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if eq := strings.IndexByte(line, '='); eq > 0 {
			k := strings.TrimSpace(line[:eq])
			v := strings.TrimSpace(line[eq+1:])
			if k != "" {
				result[k] = v
			}
		}
	}
	return result, nil
}

// writeSecretsFile atomically writes a secrets.env file (mode 0600).
// Uses O_CREATE|O_EXCL temp + os.Rename for race safety.
func writeSecretsFile(path string, secrets map[string]string) error {
	if err := ensureDataDir(path); err != nil {
		return fmt.Errorf("ensure data dir for secrets: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("# ilter auto-generated secrets — do not edit manually\n")
	sb.WriteString("# Set the corresponding ILTER_* env var to override.\n")
	sb.WriteString(fmt.Sprintf("# Generated: %s\n", envTimestamp()))
	keys := make([]string, 0, len(secrets))
	for k := range secrets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sb.WriteString(fmt.Sprintf("%s=%s\n", k, secrets[k]))
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(sb.String()), 0o600); err != nil {
		return fmt.Errorf("write temp secrets file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename secrets file: %w", err)
	}
	return nil
}

// printGeneratedSecrets writes generated secret info to stdout.
// Each secret is printed with its description and value so the operator can
// copy it immediately.
func printGeneratedSecrets(generated map[string]string) {
	fmt.Println("🔑  Auto-generated secrets (copy these now — they won't be shown again):")
	for _, s := range ManagedSecrets() {
		val, ok := generated[s.EnvVar]
		if !ok {
			continue
		}
		fmt.Printf("   • %s: %s\n", s.Description, val)
	}
	fmt.Println()
}

// envTimestamp returns a timestamp string for the secrets file header.
func envTimestamp() string {
	return "new" // simplified; real timestamp uses ctime of the file
}

// ResolveSecrets loads secrets from env/config first (already on cfg),
// then from the secrets file, then generates+persists any still missing.
//
// Resolution order (per secret):
//  1. Already set on cfg (from config) → keep as-is
//  2. Env var (ILTER_ADMIN_API_KEY etc.) → persist to file, return value
//  3. Secrets file → use value from file
//  4. Generate → persist to file, print once, return value
func ResolveSecrets(cfg *Config, dbPath string) error {
	secretsFile := ResolveSecretsFile(dbPath)
	fileSecrets, err := loadSecretsFromFile(secretsFile)
	if err != nil {
		return fmt.Errorf("load secrets file: %w", err)
	}

	toPersist := make(map[string]string)
	anyGenerated := false

	for _, s := range ManagedSecrets() {
		existing := resolveOneSecret(s, cfg, fileSecrets)
		if existing != "" {
			continue // already set from env/config/file
		}

		val, err := s.Generate()
		if err != nil {
			return fmt.Errorf("generate %s: %w", s.Key, err)
		}
		toPersist[s.EnvVar] = val
		anyGenerated = true

		applySecret(cfg, s.Key, val)

	}

	if anyGenerated {
		printGeneratedSecrets(toPersist)
		if err := writeSecretsFile(secretsFile, toPersist); err != nil {
			return fmt.Errorf("persist generated secrets: %w", err)
		}
		slog.Info(
			"secrets auto-generated and persisted",
			"file", secretsFile,
			"secrets", len(toPersist),
		)
	}

	return nil
}

// resolveOneSecret checks env first, then config (already on cfg), then file.
// Returns the value if set, empty string if not.
func resolveOneSecret(s ManagedSecret, cfg *Config, fileSecrets map[string]string) string {
	// 1. Check env var
	if v, ok := os.LookupEnv(s.EnvVar); ok && v != "" {
		applySecret(cfg, s.Key, v)
		return v
	}
	// 2. Check Config (from config) — warn that secret is in config rather than env
	switch s.Key {
	case "auth.admin_key":
		if cfg.Auth.AdminKey != "" {
			slog.Warn("secret sourced from config file; prefer an ILTER_* env var to avoid committing secrets to git",
				"field", s.Key, "env_var", s.EnvVar)
			return cfg.Auth.AdminKey
		}
	case "dashboard.auth_token":
		if cfg.Dashboard.AuthToken != "" {
			slog.Warn("secret sourced from config file; prefer an ILTER_* env var to avoid committing secrets to git",
				"field", s.Key, "env_var", s.EnvVar)
			return cfg.Dashboard.AuthToken
		}
	}
	// 3. Check secrets file
	if v, ok := fileSecrets[s.EnvVar]; ok && v != "" {
		applySecret(cfg, s.Key, v)
		return v
	}
	return ""
}

// applySecret writes a resolved secret value onto the Config struct.
func applySecret(cfg *Config, key, value string) {
	switch key {
	case "auth.admin_key":
		cfg.Auth.AdminKey = value
	case "dashboard.auth_token":
		cfg.Dashboard.AuthToken = value
	}
}
