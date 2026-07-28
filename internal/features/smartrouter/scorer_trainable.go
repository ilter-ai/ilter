package smartrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model"
)

// TrainableScorer loads a lightweight feature-weight model from a JSON file
// and computes complexity as a weighted sum of extracted features.
type TrainableScorer struct {
	cfg   *config.TrainableScorerConfig
	mu    sync.RWMutex
	model *trainableModel

	reasoningPat  *regexp.Regexp
	constraintPat *regexp.Regexp
	formatPat     *regexp.Regexp
}

type trainableModel struct {
	Version  int                `json:"version"`
	Features map[string]float64 `json:"features"`
	Weights  map[string]float64 `json:"weights"`
	Scale    trainableScale     `json:"scale"`
}

type trainableScale struct {
	InputMax  float64 `json:"input_max"`
	OutputMax float64 `json:"output_max"` // default 100
}

var defaultTrainableFeatures = map[string]float64{
	"word_count":       0,
	"reasoning_kw":     0,
	"constraint_kw":    0,
	"format_kw":        0,
	"code_blocks":      0,
	"multi_turn_depth": 0,
	"tool_count":       0,
}

var defaultTrainableWeights = map[string]float64{
	"word_count":       0.15,
	"reasoning_kw":     5.0,
	"constraint_kw":    4.0,
	"format_kw":        3.0,
	"code_blocks":      8.0,
	"multi_turn_depth": 3.0,
	"tool_count":       5.0,
}

// NewTrainableScorer loads a model from JSON path or uses built-in defaults.
func NewTrainableScorer(cfg *config.TrainableScorerConfig) (*TrainableScorer, error) {
	if cfg == nil {
		return nil, fmt.Errorf("trainable scorer config is nil")
	}

	ts := &TrainableScorer{
		cfg:           cfg,
		reasoningPat:  regexp.MustCompile(`(?i)\b(step-by-step|reason|think|analyze|compare|evaluate|critique|synthesize|assess)\b`),
		constraintPat: regexp.MustCompile(`(?i)\b(must|must not|required|prohibited|always|never|strictly|ensure|mandatory)\b`),
		formatPat:     regexp.MustCompile(`(?i)\b(json|xml|csv|markdown|table|yaml|structured|schema)\b`),
	}

	// Try loading from file
	if cfg.ModelPath != "" {
		model, err := loadModelFromFile(cfg.ModelPath)
		if err != nil {
			if cfg.FallbackOnError {
				slog.Warn("Trainable scorer: failed to load model, using defaults", "path", cfg.ModelPath, "error", err)
				ts.model = defaultModel()
				return ts, nil
			}
			return nil, fmt.Errorf("load model from %s: %w", cfg.ModelPath, err)
		}
		ts.model = model
	} else {
		ts.model = defaultModel()
	}

	return ts, nil
}

func defaultModel() *trainableModel {
	return &trainableModel{
		Version:  1,
		Features: defaultTrainableFeatures,
		Weights:  defaultTrainableWeights,
		Scale: trainableScale{
			InputMax:  100,
			OutputMax: 100,
		},
	}
}

func loadModelFromFile(path string) (*trainableModel, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m trainableModel
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	// Fill missing features/weights with defaults
	for k, v := range defaultTrainableFeatures {
		if _, exists := m.Features[k]; !exists {
			m.Features[k] = v
		}
	}
	for k, v := range defaultTrainableWeights {
		if _, exists := m.Weights[k]; !exists {
			m.Weights[k] = v
		}
	}
	if m.Scale.OutputMax <= 0 {
		m.Scale.OutputMax = 100
	}
	return &m, nil
}

// Score extracts features and computes weighted sum.
func (s *TrainableScorer) Score(_ context.Context, messages []model.Message, tools []model.Tool) float64 {
	s.mu.RLock()
	model := s.model
	s.mu.RUnlock()

	if model == nil {
		return fallbackScore(extractUserContent(messages), len(tools))
	}

	prompt := extractUserContent(messages)
	features := s.extractFeatures(prompt, messages, tools)

	var score float64
	for name, value := range features {
		weight, ok := model.Weights[name]
		if !ok {
			weight = 1.0
		}
		if model.Scale.InputMax > 0 {
			value = math.Min(value, model.Scale.InputMax)
		}
		score += value * weight
	}

	if model.Scale.OutputMax > 0 && score > model.Scale.OutputMax {
		score = model.Scale.OutputMax
	}
	if score < 0 {
		score = 0
	}
	return score
}

func (s *TrainableScorer) extractFeatures(prompt string, messages []model.Message, tools []model.Tool) map[string]float64 {
	wordCount := len(strings.Fields(prompt))
	codeBlocks := float64(strings.Count(prompt, "```"))

	multiTurn := 0
	if len(messages) > 4 {
		multiTurn = len(messages) - 4
	}

	return map[string]float64{
		"word_count":       float64(wordCount),
		"reasoning_kw":     float64(len(s.reasoningPat.FindAllString(prompt, -1))),
		"constraint_kw":    float64(len(s.constraintPat.FindAllString(prompt, -1))),
		"format_kw":        float64(len(s.formatPat.FindAllString(prompt, -1))),
		"code_blocks":      codeBlocks,
		"multi_turn_depth": float64(multiTurn),
		"tool_count":       float64(len(tools)),
	}
}
