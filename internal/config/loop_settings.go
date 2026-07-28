package config

import (
	"strconv"
	"time"
)

// LoopSettingsWithDefaults fills zero-valued fields with the same defaults
// the loop detector applies internally. Boot config / runtime_config only
// need to carry an override for what an operator actually changed — this
// keeps the dashboard's displayed values in sync with what's really enforced
// instead of showing misleading zeros for anything left at its default.
func LoopSettingsWithDefaults(s LoopSettingsConfig) LoopSettingsConfig {
	if s.RateThreshold <= 0 {
		s.RateThreshold = 30
	}
	if s.FingerprintWindow <= 0 {
		s.FingerprintWindow = 20
	}
	if s.FingerprintDuplicates <= 0 {
		s.FingerprintDuplicates = 5
	}
	if s.CostWindow <= 0 {
		s.CostWindow = 5 * time.Minute
	}
	if s.CostThreshold <= 0 {
		s.CostThreshold = 5.0
	}
	if s.SessionMaxRequests <= 0 {
		s.SessionMaxRequests = 100
	}
	if s.OutputLoopMode == "" {
		s.OutputLoopMode = "observe"
	}
	if s.OutputLoopThreshold <= 0 {
		s.OutputLoopThreshold = 6
	}
	if s.OutputMinSentence <= 0 {
		s.OutputMinSentence = 20
	}
	return s
}

// ApplyLoopSettingsOverrides merges persisted runtime_config values (section
// "loop_settings") on top of base. Unknown/invalid/zero values are ignored so
// a partial or corrupt override map can't zero out a field.
func ApplyLoopSettingsOverrides(base LoopSettingsConfig, overrides map[string]string) LoopSettingsConfig {
	out := base
	if v, ok := overrides["rate_threshold"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			out.RateThreshold = n
		}
	}
	if v, ok := overrides["fingerprint_window"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			out.FingerprintWindow = n
		}
	}
	if v, ok := overrides["fingerprint_duplicates"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			out.FingerprintDuplicates = n
		}
	}
	if v, ok := overrides["cost_window"]; ok {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			out.CostWindow = d
		}
	}
	if v, ok := overrides["cost_threshold"]; ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			out.CostThreshold = f
		}
	}
	if v, ok := overrides["session_max_requests"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			out.SessionMaxRequests = n
		}
	}
	if v, ok := overrides["output_loop_mode"]; ok && v != "" {
		out.OutputLoopMode = v
	}
	if v, ok := overrides["output_loop_threshold"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 2 {
			out.OutputLoopThreshold = n
		}
	}
	if v, ok := overrides["output_min_sentence_len"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			out.OutputMinSentence = n
		}
	}
	return out
}

// LoopSettingsToRuntimeConfigValues flattens a LoopSettingsConfig into the
// section="loop_settings" key/value rows persisted to runtime_config.
func LoopSettingsToRuntimeConfigValues(s LoopSettingsConfig) map[string]string {
	return map[string]string{
		"rate_threshold":          strconv.Itoa(s.RateThreshold),
		"fingerprint_window":      strconv.Itoa(s.FingerprintWindow),
		"fingerprint_duplicates":  strconv.Itoa(s.FingerprintDuplicates),
		"cost_window":             s.CostWindow.String(),
		"cost_threshold":          strconv.FormatFloat(s.CostThreshold, 'f', -1, 64),
		"session_max_requests":    strconv.Itoa(s.SessionMaxRequests),
		"output_loop_mode":        s.OutputLoopMode,
		"output_loop_threshold":   strconv.Itoa(s.OutputLoopThreshold),
		"output_min_sentence_len": strconv.Itoa(s.OutputMinSentence),
	}
}
