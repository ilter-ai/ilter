package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveRuntime_Merge(t *testing.T) {
	boot := &BootConfig{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8181,
		},
		Auth: AuthConfig{
			AdminKey: "boot-admin-key",
		},
		Cache: BootCacheConfig{
			Type:                "redis",
			SimilarityThreshold: OptionFloat64{optionCore[float64]{val: 0.70, present: true}},
		},
		Audit: BootAuditConfig{
			Enabled:       true,
			RetentionDays: OptionInt64{optionCore[int64]{val: 90, present: true}},
		},
		Guardrails: BootGuardConfig{
			Enabled: true,
			Mode:    "block",
		},
		MCP: BootMCPConfig{
			Enabled:       true,
			DefaultPolicy: "allow",
			HubEndpoint:   DefaultMCPHubEndpoint,
		},
	}

	// State overrides some boot defaults
	threshold := 0.95
	retention := int64(180)
	mode := "warn"
	state := &StateConfig{
		Providers: []ProviderConfig{
			{Name: "my-openai", Type: "openai", APIKey: "sk-xxx"},
		},
		CacheSimilarityThreshold: &threshold,
		AuditRetentionDays:       &retention,
		GuardrailsMode:           &mode,
	}

	snap := ResolveRuntime(boot, state)
	require.NotNil(t, snap)

	// Boot pass-through
	assert.Equal(t, "0.0.0.0", snap.Server.Host)
	assert.Equal(t, 8181, snap.Server.Port)
	assert.Equal(t, "boot-admin-key", snap.Auth.AdminKey)

	// Override applied
	assert.Equal(t, 0.95, snap.CacheSimilarityThreshold)
	assert.Equal(t, int64(180), snap.AuditRetentionDays)
	assert.Equal(t, "warn", snap.GuardrailsMode)

	// Boot defaults preserved where no override
	assert.True(t, snap.GuardrailsEnabled)
	assert.True(t, snap.MCPEnabled)

	// From state
	require.Len(t, snap.Providers, 1)
	assert.Equal(t, "my-openai", snap.Providers[0].Name)
}

func TestResolveRuntime_NilState(t *testing.T) {
	boot := &BootConfig{
		Server: ServerConfig{Host: "0.0.0.0", Port: 8181},
		Auth:   AuthConfig{AdminKey: "test"},
	}

	snap := ResolveRuntime(boot, nil)
	require.NotNil(t, snap)

	// Boot values preserved
	assert.Equal(t, "0.0.0.0", snap.Server.Host)
	assert.Equal(t, 8181, snap.Server.Port)

	// Defaults resolved
	assert.Equal(t, 0.70, snap.CacheSimilarityThreshold)
	assert.Equal(t, int64(DefaultRetentionDays), snap.AuditRetentionDays)

	// State sections are nil/empty
	assert.Nil(t, snap.Providers)
}

func TestResolveRuntime_EmptyState(t *testing.T) {
	boot := DefaultBootConfig()
	state := &StateConfig{} // zero-value state, no overrides

	snap := ResolveRuntime(&boot, state)
	require.NotNil(t, snap)

	// Boot defaults preserved
	assert.True(t, snap.AuditEnabled)
	assert.True(t, snap.Telemetry.Enabled)

	// No override pointers → boot defaults win
	assert.Equal(t, 0.70, snap.CacheSimilarityThreshold)
	assert.Equal(t, int64(DefaultRetentionDays), snap.AuditRetentionDays)

	// State sections empty
	assert.Nil(t, snap.Providers)
}
