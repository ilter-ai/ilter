package guardrails

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
)

type Action string

const (
	ActionBlock Action = "block"
	ActionWarn  Action = "warn"
	ActionMask  Action = "mask"
)

type Severity string

const (
	SevLow      Severity = "low"
	SevMedium   Severity = "medium"
	SevHigh     Severity = "high"
	SevCritical Severity = "critical"
)

type Result struct {
	Blocked     bool
	Warned      bool
	Masked      bool
	RuleID      string
	RuleSet     string
	Severity    Severity
	MatchedText string // truncated to 100 runes
	Action      Action
	Mask        string // replacement text when Action == ActionMask
}

type Message struct {
	Index   int
	Role    string
	Content string
}

type compiledRule struct {
	ID          string
	RuleSet     string
	Mode        Action
	Severity    Severity
	Description string
	Regexes     []*regexp.Regexp
}

type Checker struct {
	cfg           Config
	rules         []compiledRule
	mu            sync.RWMutex // guards rules slice for dynamic LoadDBRules
	topics        []string     // simple slice over Aho-Corasick trie (small pattern sets)
	logger        *slog.Logger
	maxContent    int
	maxContentLen int
	modClient     ModerationClient
}

// NewChecker compiles all built-in rules, all custom rules, and the topic
// trie. Invalid custom regexes are logged and skipped (fail-open at config
// time) to keep operator iteration fast.
func NewChecker(cfg Config, logger *slog.Logger) (*Checker, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Mode == "" {
		cfg.Mode = string(ActionBlock)
	}
	c := &Checker{
		cfg:           cfg,
		logger:        logger,
		maxContent:    100,
		maxContentLen: cfg.MaxContentLength,
	}
	if err := c.compileBuiltinRules(); err != nil {
		return nil, fmt.Errorf("guardrails: compile built-in rules: %w", err)
	}
	c.compileCustomRules()
	if cfg.TopicBlock.Enabled && len(cfg.TopicBlock.Topics) > 0 {
		c.topics = cfg.TopicBlock.Topics
	}
	if cfg.ModerationAPI.Enabled {
		c.modClient = NewModerationClient(cfg.ModerationAPI, logger)
	}
	return c, nil
}

func (c *Checker) RuleCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.rules)
}

func (c *Checker) RuleSets() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	seen := make(map[string]struct{}, 8)
	for _, r := range c.rules {
		seen[r.RuleSet] = struct{}{}
	}
	sets := make([]string, 0, len(seen))
	for s := range seen {
		sets = append(sets, s)
	}
	return sets
}

// LoadDBRules replaces all compiled rules with rules loaded from the database.
// This allows the admin UI to control which rules are active.
func (c *Checker) LoadDBRules(rules []DBRule) {
	dbRulesMu.Lock()
	defer dbRulesMu.Unlock()

	dbRules = make([]compiledDBRule, 0, len(rules))
	for _, r := range rules {
		regexes, err := compilePatterns(r.Patterns)
		if err != nil {
			c.logger.Warn("guardrails: invalid pattern in DB rule", "id", r.ID, "error", err)
			continue
		}
		if len(regexes) == 0 {
			continue
		}
		mode := Action(r.Mode)
		if mode == "" {
			mode = Action(c.cfg.Mode)
		}
		sev := Severity(r.Severity)
		if sev == "" {
			sev = SevMedium
		}
		cr := compiledRule{
			ID:          r.ID,
			RuleSet:     r.RuleSet,
			Mode:        mode,
			Severity:    sev,
			Description: r.Description,
			Regexes:     regexes,
		}
		dbRules = append(dbRules, compiledDBRule{
			compiledRule: cr,
			TargetType:   r.TargetType,
			TargetID:     r.TargetID,
		})
	}
	c.logger.Info("guardrails: loaded rules from DB", "count", len(dbRules))
}

// CheckDB evaluates messages against DB rules, filtering by target context.
func (c *Checker) CheckDB(_ context.Context, messages []Message, userID *int, groupIDs []int) Result {
	dbRulesMu.RLock()
	defer dbRulesMu.RUnlock()

	for _, rule := range dbRules {
		if !matchesTarget(rule, userID, groupIDs) {
			continue
		}
		for _, m := range messages {
			content := m.Content
			if content == "" {
				continue
			}
			for _, re := range rule.Regexes {
				if loc := re.FindStringIndex(content); loc != nil {
					return c.buildResult(rule.compiledRule, content[loc[0]:loc[1]], rule.Mode)
				}
			}
		}
	}
	return Result{}
}

func matchesTarget(rule compiledDBRule, userID *int, groupIDs []int) bool {
	switch rule.TargetType {
	case "", "global":
		return true
	case "user":
		if userID == nil || rule.TargetID == nil {
			return false
		}
		return *userID == *rule.TargetID
	case "group":
		if rule.TargetID == nil {
			return false
		}
		return slices.Contains(groupIDs, *rule.TargetID)
	default:
		return true
	}
}

// DBRule holds a single rule loaded from the database.
type DBRule struct {
	ID          string
	RuleSet     string
	Patterns    []string
	Mode        string
	Severity    string
	Description string
	TargetType  string // "global", "user", "group"
	TargetID    *int
}

type compiledDBRule struct {
	compiledRule
	TargetType string
	TargetID   *int
}

var (
	dbRules   []compiledDBRule
	dbRulesMu sync.RWMutex
)

// Check evaluates every message against every enabled rule. It returns the
// first non-pass result. When no rule matches it returns a zero Result.
// If a ModerationAPI client is configured, the combined message content is sent
// to the external moderation endpoint after local rules pass.
func (c *Checker) Check(ctx context.Context, messages []Message) Result {
	for _, m := range messages {
		content := m.Content
		if content == "" {
			continue
		}
		if c.maxContentLen > 0 && len([]rune(content)) > c.maxContentLen {
			return Result{
				Blocked:  true,
				RuleID:   "input_token_boundary",
				RuleSet:  RuleSetInputGuardrails,
				Severity: SevMedium,
				Action:   ActionBlock,
			}
		}
		c.mu.RLock()
		for _, r := range c.rules {
			for _, re := range r.Regexes {
				if loc := re.FindStringIndex(content); loc != nil {
					res := c.buildResult(r, content[loc[0]:loc[1]], r.Mode)
					c.mu.RUnlock()
					return res
				}
			}
		}
		c.mu.RUnlock()
		if len(c.topics) > 0 {
			lower := strings.ToLower(content)
			for _, topic := range c.topics {
				if idx := strings.Index(lower, topic); idx >= 0 {
					return c.buildResult(c.topicRule(), content[idx:idx+len(topic)], Action(c.cfg.TopicBlock.Mode))
				}
			}
		}
	}
	if c.modClient != nil {
		return c.checkModeration(ctx, messages)
	}
	return Result{}
}

// checkModeration sends the concatenated message content to the external
// moderation API. If the API flags any content, a block result is returned.
// The API is called fail-open: errors (timeout, network, non-2xx) pass through.
func (c *Checker) checkModeration(ctx context.Context, messages []Message) Result {
	var sb strings.Builder
	for _, m := range messages {
		if m.Content != "" {
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(m.Content)
		}
	}
	if sb.Len() == 0 {
		return Result{}
	}

	mr, err := c.modClient.Moderate(ctx, sb.String())
	if err != nil || mr == nil || !mr.Flagged {
		return Result{}
	}

	catNames := make([]string, 0, len(mr.Categories))
	for _, cat := range mr.Categories {
		if cat.Flagged {
			catNames = append(catNames, cat.Name)
		}
	}
	matched := "moderation_flagged"
	if len(catNames) > 0 {
		matched = catNames[0]
	}

	return Result{
		Blocked:     true,
		RuleID:      "moderation_api",
		RuleSet:     "moderation_api",
		Severity:    SevHigh,
		MatchedText: matched,
		Action:      ActionBlock,
	}
}

func (c *Checker) buildResult(r compiledRule, matched string, action Action) Result {
	truncated := matched
	if r := []rune(truncated); len(r) > c.maxContent {
		truncated = string(r[:c.maxContent]) + "..."
	}
	mask := ""
	if action == ActionMask {
		mask = "[FILTERED]"
	}
	return Result{
		Blocked:     action == ActionBlock,
		Warned:      action == ActionWarn,
		Masked:      action == ActionMask,
		RuleID:      r.ID,
		RuleSet:     r.RuleSet,
		Severity:    r.Severity,
		MatchedText: truncated,
		Action:      action,
		Mask:        mask,
	}
}

func (c *Checker) topicRule() compiledRule {
	return compiledRule{
		ID:       "topic_block",
		RuleSet:  "topic_block",
		Mode:     Action(c.cfg.TopicBlock.Mode),
		Severity: SevHigh,
	}
}
