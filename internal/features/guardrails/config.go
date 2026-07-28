package guardrails

import "time"

// Config is the runtime view of config.GuardrailsConfig passed to the checker.
// Decoupled from the config package so the guardrails package has no upward
// import — the middleware is the only thing that knows about both types.
type Config struct {
	Enabled          bool
	Mode             string
	RuleSets         []string
	CustomRules      []CustomRule
	TopicBlock       TopicBlock
	ModerationAPI    ModerationAPI
	MaxContentLength int // max rune count per message (0 = no limit)
}

// CustomRule is the runtime shape of a user-defined regex rule.
type CustomRule struct {
	ID       string
	Patterns []string
	Mode     string
	Severity string
}

// TopicBlock configures keyword-based topic blocking.
type TopicBlock struct {
	Enabled bool
	Topics  []string
	Mode    string
}

// ModerationAPI configures the optional external moderation endpoint.
type ModerationAPI struct {
	Enabled bool
	URL     string
	APIKey  string
	Timeout time.Duration
}
