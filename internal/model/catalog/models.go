package catalog

import (
	"log/slog"
	"slices"
	"strings"
	"sync"
)

// ModelInfo holds structural parameters, costs, and limits for a model.
type ModelInfo struct {
	ID                 string   `json:"id"`
	Provider           string   `json:"provider"`
	DisplayName        string   `json:"display_name"`
	MaxContextTokens   int      `json:"max_context_tokens"`
	MaxOutputTokens    int      `json:"max_output_tokens"`
	CostPerInputToken  float64  `json:"cost_per_input_token"`
	CostPerOutputToken float64  `json:"cost_per_output_token"`
	Tier               string   `json:"tier"`
	Capabilities       []string `json:"capabilities"`
	DefaultBaseURL     string   `json:"default_base_url"`
}

// Models is the in-memory model cache, populated at startup from the
// provider_models DB table. Multiple entries per model ID are allowed when
// multiple providers serve the same model (multi-provider routing).
var (
	Models   = make(map[string][]ModelInfo)
	ModelsMu sync.RWMutex
)

// LoadFromDB replaces the in-memory Models cache with entries loaded from
// the provider_models DB table. This should be called once at startup after
// model discovery has completed.
func LoadFromDB(getAll func() ([]ModelInfo, error)) error {
	entries, err := getAll()
	if err != nil {
		return err
	}

	ModelsMu.Lock()
	Models = make(map[string][]ModelInfo, len(entries))
	for _, e := range entries {
		Models[e.ID] = append(Models[e.ID], e)
	}
	ModelsMu.Unlock()

	slog.Debug("loaded models from DB", "count", len(entries))
	return nil
}

func (m ModelInfo) SupportsTools() bool {
	return slices.Contains(m.Capabilities, "tools") || slices.Contains(m.Capabilities, "function_calling")
}

// CanonicalModelID strips the provider prefix (everything before the first '/')
// from a model ID. The frontend sends "provider/model" but registry keys and
// route tables are keyed by bare model name. Model names never contain '/',
// so any prefix is always a provider label.
func CanonicalModelID(modelID string) string {
	if idx := strings.IndexByte(modelID, '/'); idx >= 0 {
		return modelID[idx+1:]
	}
	return modelID
}

// ModelSupportsTools reports whether the model identified by modelID has
// tool-calling capability. Returns false when the model is not found.
// When multiple entries exist for the same model ID (different providers),
// any provider with tool support is sufficient.
// The frontend may send "provider/model" — CanonicalModelID handles that.
func ModelSupportsTools(modelID string) bool {
	canonical := CanonicalModelID(modelID)

	ModelsMu.RLock()
	models, ok := Models[canonical]
	ModelsMu.RUnlock()
	if !ok || len(models) == 0 {
		return false
	}
	for _, m := range models {
		if m.SupportsTools() {
			return true
		}
	}
	return false
}

func GetModel(modelID string) (ModelInfo, bool) {
	ModelsMu.RLock()
	models, ok := Models[modelID]
	ModelsMu.RUnlock()
	if !ok || len(models) == 0 {
		return ModelInfo{}, false
	}
	return models[0], true
}
