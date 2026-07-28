package config

import (
	"log/slog"
	"os"
	"sort"
	"strings"
)

// ProviderKeyEnv returns the environment variable name for a provider's single API key.
func ProviderKeyEnv(name string) string {
	return "ILTER_PROVIDER_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_API_KEY"
}

// ProviderKeysEnv returns the environment variable name for a provider's multiple API keys.
// Convention: ILTER_PROVIDER_<UPPER_NAME>_API_KEYS (comma or newline separated).
func ProviderKeysEnv(name string) string {
	return "ILTER_PROVIDER_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_API_KEYS"
}

// providerKeysFromEnv reads ILTER_PROVIDER_<NAME>_API_KEY(S) for name and
// returns the parsed keys, which env var supplied them, and whether either
// was set. The *_KEYS (plural) var wins when both are set.
func providerKeysFromEnv(name string) (keys []string, source string, ok bool) {
	envMulti := ProviderKeysEnv(name)
	if v, isSet := os.LookupEnv(envMulti); isSet && strings.TrimSpace(v) != "" {
		rawKeys := strings.FieldsFunc(v, func(r rune) bool {
			return r == ',' || r == '\n' || r == '\r'
		})
		for _, k := range rawKeys {
			if trimmed := strings.TrimSpace(k); trimmed != "" {
				keys = append(keys, trimmed)
			}
		}
		if len(keys) > 0 {
			return keys, envMulti, true
		}
	}

	envSingle := ProviderKeyEnv(name)
	if v, isSet := os.LookupEnv(envSingle); isSet && strings.TrimSpace(v) != "" {
		trimmed := strings.TrimSpace(v)
		return []string{trimmed}, envSingle, true
	}

	return nil, "", false
}

// AnyProviderKeyEnvSet reports whether at least one known provider's
// ILTER_PROVIDER_<NAME>_API_KEY(S) env var is set, regardless of whether
// that provider is already registered in cfg.Providers.
func AnyProviderKeyEnvSet() bool {
	for t := range DefaultBaseURLs {
		if _, _, ok := providerKeysFromEnv(t); ok {
			return true
		}
	}
	return false
}

// ResolveProviderKeys overrides each provider's APIKey / APIKeys from its
// environment variables when set (DB value is the fallback), then registers
// any known provider type that has an env key set but no DB/config entry —
// setting ILTER_PROVIDER_<NAME>_API_KEY is enough to enable that provider,
// no `ilter init` required.
func ResolveProviderKeys(cfg *Config, _ *slog.Logger) {
	configured := make(map[string]bool, len(cfg.Providers))

	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		configured[p.Type] = true

		if keys, source, ok := providerKeysFromEnv(p.Name); ok {
			p.APIKeys = keys
			p.APIKey = keys[0]
			p.APIKeySource = source
		} else if len(p.GetAPIKeys()) > 0 {
			p.APIKeySource = "db"
		}
	}

	types := make([]string, 0, len(DefaultBaseURLs))
	for t := range DefaultBaseURLs {
		types = append(types, t)
	}
	sort.Strings(types)

	for _, t := range types {
		if configured[t] {
			continue
		}
		keys, source, ok := providerKeysFromEnv(t)
		if !ok {
			continue
		}
		cfg.Providers = append(cfg.Providers, ProviderConfig{
			Name:         t,
			Type:         t,
			BaseURL:      DefaultBaseURLs[t],
			APIKey:       keys[0],
			APIKeys:      keys,
			APIKeySource: source,
		})
	}
}
