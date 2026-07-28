package guardrails

import (
	"fmt"
	"regexp"
)

const (
	RuleSetPromptInjection = "prompt_injection"
	RuleSetInputGuardrails = "input_guardrails"
	RuleSetToxicContent    = "toxic_content"
	RuleSetTopicBlock      = "topic_block"
	RuleSetCustom          = "custom"
)

func (c *Checker) compileBuiltinRules() error {
	for _, def := range builtInInjectionPatterns {
		regexes, err := compilePatterns(def.Patterns)
		if err != nil {
			return fmt.Errorf("compile %s: %w", def.ID, err)
		}
		c.rules = append(c.rules, compiledRule{
			ID:          def.ID,
			RuleSet:     RuleSetPromptInjection,
			Mode:        Action(c.cfg.Mode),
			Severity:    def.Severity,
			Description: def.Description,
			Regexes:     regexes,
		})
	}
	if err := c.compileToxicRules(); err != nil {
		return err
	}
	if err := c.compileInputGuardrails(); err != nil {
		return err
	}
	return nil
}

func (c *Checker) compileInputGuardrails() error {
	for _, g := range allInputGuardrails() {
		regexes, err := compilePatterns(g.Patterns)
		if err != nil {
			return fmt.Errorf("compile input %s: %w", g.ID, err)
		}
		c.rules = append(c.rules, compiledRule{
			ID:          g.ID,
			RuleSet:     RuleSetInputGuardrails,
			Mode:        Action(c.cfg.Mode),
			Severity:    g.Severity,
			Description: g.Description,
			Regexes:     regexes,
		})
	}
	return nil
}

func (c *Checker) compileToxicRules() error {
	for _, t := range builtInToxicPatterns {
		regexes, err := compilePatterns(t.Patterns)
		if err != nil {
			return fmt.Errorf("compile %s: %w", t.ID, err)
		}
		c.rules = append(c.rules, compiledRule{
			ID:       t.ID,
			RuleSet:  RuleSetToxicContent,
			Mode:     t.Mode,
			Severity: t.Severity,
			Regexes:  regexes,
		})
	}
	return nil
}

func (c *Checker) compileCustomRules() {
	for _, cr := range c.cfg.CustomRules {
		mode := Action(cr.Mode)
		if mode == "" {
			mode = Action(c.cfg.Mode)
		}
		sev := Severity(cr.Severity)
		if sev == "" {
			sev = SevMedium
		}
		regexes, err := compilePatterns(cr.Patterns)
		if err != nil {
			c.logger.Warn("guardrails: invalid custom regex, skipping rule",
				"rule_id", cr.ID, "error", err)
			continue
		}
		if len(regexes) == 0 {
			continue
		}
		c.rules = append(c.rules, compiledRule{
			ID:       cr.ID,
			RuleSet:  RuleSetCustom,
			Mode:     mode,
			Severity: sev,
			Regexes:  regexes,
		})
	}
}

func compilePatterns(patterns []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, err
		}
		out = append(out, re)
	}
	return out, nil
}
