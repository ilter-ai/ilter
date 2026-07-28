package smartrouter

import (
	"strconv"
	"strings"

	"github.com/ilter-ai/ilter/internal/model"
)

// FindMatchingRule returns the first enabled rule whose condition matches the request.
// Evaluated in order (caller should sort by priority). Returns nil when no rule matches.
func FindMatchingRule(rules []model.RoutingRule, req *model.ChatCompletionRequest, complexityScore float64) *model.RoutingRule {
	for i := range rules {
		if !rules[i].Enabled {
			continue
		}
		if matchesCondition(rules[i].Condition, req, complexityScore) {
			return &rules[i]
		}
	}
	return nil
}

func matchesCondition(cond string, req *model.ChatCompletionRequest, score float64) bool {
	cond = strings.TrimSpace(cond)
	if cond == "" {
		return true
	}

	// complexity < N  complexity > N  complexity <= N  complexity >= N  complexity == N
	if strings.HasPrefix(cond, "complexity ") {
		return evalComplexity(strings.TrimSpace(cond[11:]), score)
	}

	// model == "name"
	if strings.HasPrefix(cond, "model == ") {
		target := extractQuoted(strings.TrimSpace(cond[9:]))
		return req.Model == target
	}

	// prompt contains "text"
	if strings.HasPrefix(cond, "prompt contains ") {
		target := extractQuoted(strings.TrimSpace(cond[16:]))
		if target == "" {
			return false
		}
		for _, msg := range req.Messages {
			if msg.Content != nil {
				if s, ok := msg.Content.(string); ok && strings.Contains(s, target) {
					return true
				}
			}
		}
		return false
	}

	return false
}

// extractQuoted strips surrounding double or single quotes from s.
// Returns "" if s is not properly quoted.
func extractQuoted(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return ""
}

func evalComplexity(expr string, score float64) bool {
	parts := strings.SplitN(expr, " ", 2)
	if len(parts) != 2 {
		return false
	}
	op := strings.TrimSpace(parts[0])
	rest := strings.TrimSpace(parts[1])

	switch op {
	case "between":
		// complexity between 25 50
		rangeParts := strings.Fields(rest)
		if len(rangeParts) != 2 {
			return false
		}
		low, err1 := strconv.ParseFloat(rangeParts[0], 64)
		high, err2 := strconv.ParseFloat(rangeParts[1], 64)
		if err1 != nil || err2 != nil {
			return false
		}
		return score >= low && score < high
	default:
		val, err := strconv.ParseFloat(rest, 64)
		if err != nil {
			return false
		}
		switch op {
		case "<":
			return score < val
		case ">":
			return score > val
		case "<=":
			return score <= val
		case ">=":
			return score >= val
		case "==":
			return score == val
		}
		return false
	}
}
