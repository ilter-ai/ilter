package config

import (
	"strings"
)

// EnrichConfig populates missing fields in the configuration from the built-in model catalog.
func EnrichConfig(cfg *Config) {
	if cfg.Telemetry.Enabled {
		if cfg.Telemetry.MetricsPath == "" {
			cfg.Telemetry.MetricsPath = DefaultMetricsPath
		}
		cfg.Metrics.Enabled = cfg.Telemetry.Enabled
		cfg.Metrics.Path = cfg.Telemetry.MetricsPath
	} else if cfg.Metrics.Enabled {
		cfg.Telemetry.Enabled = cfg.Metrics.Enabled
		cfg.Telemetry.MetricsPath = cfg.Metrics.Path
	}

	if cfg.Metrics.Path == "" {
		cfg.Metrics.Path = DefaultMetricsPath
	}

	if cfg.Metrics.ListenAddr == "" {
		cfg.Metrics.ListenAddr = DefaultMetricsListenAddr
	}

	defaultBaseURLs := DefaultBaseURLs
	for i := range cfg.Providers {
		pCfg := &cfg.Providers[i]

		pCfg.Type = strings.ReplaceAll(pCfg.Type, "-", "_")

		if pCfg.BaseURL == "" {
			if url, ok := defaultBaseURLs[pCfg.Type]; ok {
				pCfg.BaseURL = url
			}
		}
	}
}

func Load() *Config {
	cfg := DefaultConfig()
	ApplyEnvOverrides(&cfg)
	EnrichConfig(&cfg)
	return &cfg
}
