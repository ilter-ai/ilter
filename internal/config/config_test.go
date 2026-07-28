package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnrichConfig_MetricsPathNeverEmpty(t *testing.T) {
	cfg := &Config{}
	cfg.Telemetry.MetricsPath = "" // hostile input — no path set
	EnrichConfig(cfg)
	assert.Equal(t, "/metrics", cfg.Metrics.Path, "Metrics.Path must never be empty after EnrichConfig")
}
