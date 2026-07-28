// Package config handles configuration types, environment variable loading,
// and schema definitions for the ilter proxy gateway. Configuration is
// loaded from ILTER_* environment variables with compiled-in defaults.
// No YAML or config file is required.
//
// Sub-packages:
//
//	boot/       — Bootstrap loading (compiled-in defaults)
//	schema/     — Runtime config schema types and validation
//	resolve/    — Config resolution (runtime state + boot defaults)
//	env/        — ILTER_* env var helpers
//	validate/   — Config validation rules
//	secrets/    — API key and secret management
//	show/       — Config display/redaction
//	cache/      — Cache configuration types (semantic, Redis)
//	state/      — Config state tracking
//	option/     — Functional option pattern for config overrides
//
// Domain-specific config types (prompts, provider_keys) live here but should
// be moved to their respective domain packages when the type is referenced
// outside config/ to avoid import cycles.
//
// Runtime (DB-backed) configuration is handled by store/runtime_config.go.
package config
