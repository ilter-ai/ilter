package smartrouter

import (
	"context"
	"regexp"
	"strings"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model"
)

var (
	reasoningPatterns  = regexp.MustCompile(`(?i)\b(step-by-step|reason|think|analyze|compare|evaluate|critique|synthesize|assess)\b`)
	constraintPatterns = regexp.MustCompile(`(?i)\b(must|must not|required|prohibited|always|never|strictly|ensure|mandatory)\b`)
	formatPatterns     = regexp.MustCompile(`(?i)\b(json|xml|csv|markdown|table|yaml|structured|schema)\b`)
)

type Scorer interface {
	Score(ctx context.Context, messages []model.Message, tools []model.Tool) float64
}

type HeuristicScorer struct{}

func NewHeuristicScorer() *HeuristicScorer {
	return &HeuristicScorer{}
}

// Score computes complexity using regex heuristics: word count, reasoning keywords,
// constraint density, format requests, code blocks, and multi-turn depth.
func (s *HeuristicScorer) Score(_ context.Context, messages []model.Message, tools []model.Tool) float64 {
	prompt := extractUserContent(messages)
	score := 0.0

	wordCount := len(strings.Fields(prompt))
	if wordCount < 100 {
		score += 10
	} else if wordCount < 500 {
		score += 25
	} else {
		score += 40
	}

	score += float64(len(reasoningPatterns.FindAllString(prompt, -1))) * 5.0
	score += float64(len(constraintPatterns.FindAllString(prompt, -1))) * 4.0
	score += float64(len(formatPatterns.FindAllString(prompt, -1))) * 3.0

	codeBlocks := strings.Count(prompt, "```")
	score += float64(codeBlocks) * 8.0

	if len(messages) > 4 {
		score += float64(len(messages)-4) * 3.0
	}

	for _, tool := range tools {
		score += 5.0
		if tool.Function.Description != "" {
			score += 3.0
		}
	}

	if score > 100 {
		score = 100
	}
	return score
}

func ScoreComplexity(messages []model.Message) float64 {
	return NewHeuristicScorer().Score(context.Background(), messages, nil)
}

// NewScorerFromConfig creates a Scorer based on the config's scorer type field.
// Falls back to heuristic when the requested type's dependencies aren't available or on error.
func NewScorerFromConfig(sc config.ScorerConfig) Scorer {
	if sc.Type == "" || sc.Type == "heuristic" {
		return NewHeuristicScorer()
	}

	switch sc.Type {
	case "llm":
		if sc.LLM != nil {
			s, err := NewLLMScorer(sc.LLM)
			if err == nil {
				return s
			}
		}
	case "embedding":
		if sc.Embedding != nil {
			s, err := NewEmbeddingScorer(sc.Embedding)
			if err == nil {
				return s
			}
		}
	case "trainable":
		if sc.Trainable != nil {
			s, err := NewTrainableScorer(sc.Trainable)
			if err == nil {
				return s
			}
		}
	}
	return NewHeuristicScorer()
}

// simple word-count based fallback; sufficient for rare error paths.
func fallbackScore(prompt string, toolCount int) float64 {
	score := float64(len(strings.Fields(prompt))) * 0.1
	score += float64(toolCount) * 5.0
	if score > 100 {
		score = 100
	}
	return score
}

func extractUserContent(messages []model.Message) string {
	var builder strings.Builder
	for _, m := range messages {
		if m.Content == nil {
			continue
		}
		switch val := m.Content.(type) {
		case string:
			builder.WriteString(val)
			builder.WriteString(" ")
		case []any:
			for _, item := range val {
				if s, ok := item.(string); ok {
					builder.WriteString(s)
					builder.WriteString(" ")
				} else if itemMap, ok := item.(map[string]any); ok {
					if textVal, ok := itemMap["text"].(string); ok {
						builder.WriteString(textVal)
						builder.WriteString(" ")
					}
				}
			}
		}
	}
	return builder.String()
}
