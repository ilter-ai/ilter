// Package cli provides interactive terminal wizards for ILTER configuration.
package cli

import (
	"fmt"
	"strconv"

	"github.com/charmbracelet/huh"

	"github.com/ilter-ai/ilter/internal/db/seed"
)

// providerDefaults maps provider type to its default base URL and model name.
var providerDefaults = map[string]struct {
	baseURL string
}{
	"openai":       {"https://api.openai.com/v1"},
	"anthropic":    {"https://api.anthropic.com/v1"},
	"deepseek":     {"https://api.deepseek.com"},
	"gemini":       {"https://generativelanguage.googleapis.com/v1beta"},
	"openrouter":   {"https://openrouter.ai/api/v1"},
	"ollama":       {"http://localhost:11434"},
	"qwen":         {"https://dashscope.aliyuncs.com/api/v1"},
	"opencode_go":  {"https://opencode.ai/zen/go/v1"},
	"opencode_zen": {"https://opencode.ai/zen/v1"},
}

func RunInitWizard(dashboardPortDefault, metricsPortDefault int) (*seed.File, error) {
	selectedTypes := []string{"opencode_go", "opencode_zen"}

	// ── Step 1: Select which provider types to configure ──
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Select Providers").
				Description("Choose the LLM providers you want to configure").
				Options(
					huh.NewOption("OpenAI", "openai"),
					huh.NewOption("Anthropic", "anthropic"),
					huh.NewOption("DeepSeek", "deepseek"),
					huh.NewOption("Gemini", "gemini"),
					huh.NewOption("OpenRouter", "openrouter"),
					huh.NewOption("Ollama", "ollama"),
					huh.NewOption("Qwen", "qwen"),
					huh.NewOption("OpenCode Go", "opencode_go"),
					huh.NewOption("OpenCode Zen", "opencode_zen"),
				).
				Value(&selectedTypes),
		),
	).Run()
	if err != nil {
		return nil, fmt.Errorf("provider selection: %w", err)
	}

	if len(selectedTypes) == 0 {
		return nil, fmt.Errorf("at least one provider must be selected")
	}

	// ── Step 2: Build per-provider detail groups ──
	type providerForm struct {
		name    string
		baseURL string
		apiKey  string
		active  bool
	}

	provForms := make([]providerForm, len(selectedTypes))
	var detailGroups []*huh.Group

	for i, pType := range selectedTypes {
		def := providerDefaults[pType]
		provForms[i].name = pType
		provForms[i].baseURL = def.baseURL
		provForms[i].active = true

		fields := []huh.Field{
			huh.NewInput().
				Title(fmt.Sprintf("%s — Name", pType)).
				Description("Unique name for this provider registration").
				Value(&provForms[i].name),
			huh.NewInput().
				Title(fmt.Sprintf("%s — Base URL", pType)).
				Description("API endpoint URL").
				Value(&provForms[i].baseURL),
			huh.NewInput().
				Title(fmt.Sprintf("%s — API Secret Key", pType)).
				Description("Supports ${ENV_VAR} patterns for environment variables").
				Value(&provForms[i].apiKey),
			huh.NewConfirm().
				Title(fmt.Sprintf("%s — Active?", pType)).
				Description("Enable this provider immediately").
				Value(&provForms[i].active),
		}
		detailGroups = append(detailGroups, huh.NewGroup(fields...))
	}

	// ── Step 3: Routing configuration ──
	activeStrategy := "economy"

	detailGroups = append(detailGroups, huh.NewGroup(
		huh.NewSelect[string]().
			Title("Active Routing Strategy").
			Description("Which strategy to use for model routing (all 4 are seeded)").
			Options(
				huh.NewOption("Economy — cheapest provider, cost-first", "economy"),
				huh.NewOption("Premium — quality-first, round-robin", "premium"),
				huh.NewOption("Balanced — cost/quality trade-off", "balanced"),
				huh.NewOption("Smart — capability-based routing", "smart"),
			).
			Value(&activeStrategy),
	))

	// ── Step 4: Feature flag toggles ──
	rateLimit, pii, cache, budget, loopDetect := true, true, false, false, true

	detailGroups = append(detailGroups, huh.NewGroup(
		huh.NewConfirm().
			Title("Rate Limiting").
			Description("Enforce rate limits per API key").
			Value(&rateLimit),
		huh.NewConfirm().
			Title("PII Masker").
			Description("Detect and mask personally identifiable information").
			Value(&pii),
		huh.NewConfirm().
			Title("Semantic Cache").
			Description("Cache responses using semantic similarity").
			Value(&cache),
		huh.NewConfirm().
			Title("Budget Enforcement").
			Description("Enforce monthly and daily budget limits").
			Value(&budget),
		huh.NewConfirm().
			Title("Loop Detection").
			Description("Detect and break infinite request loops").
			Value(&loopDetect),
	))

	// ── Step 5: Port configuration ──
	dashboardPortStr := fmt.Sprintf("%d", dashboardPortDefault)
	metricsPortStr := fmt.Sprintf("%d", metricsPortDefault)

	detailGroups = append(detailGroups, huh.NewGroup(
		huh.NewInput().
			Title("Dashboard Port").
			Description("Dashboard UI HTTP port (default: 9191)").
			Value(&dashboardPortStr),
		huh.NewInput().
			Title("Metrics Port").
			Description("Prometheus metrics HTTP port (default: 9192)").
			Value(&metricsPortStr),
	))

	err = huh.NewForm(detailGroups...).Run()
	if err != nil {
		return nil, fmt.Errorf("setup details: %w", err)
	}

	dashboardPort, _ := strconv.Atoi(dashboardPortStr)
	metricsPort, _ := strconv.Atoi(metricsPortStr)

	// ── Assemble SeedFile from collected values ──
	providers := make([]seed.Provider, 0, len(selectedTypes))
	for i, pType := range selectedTypes {
		// OpenCode Zen/Go serve public /models endpoints — no API key needed.
		isPublic := pType == "opencode_zen" || pType == "opencode_go"

		providers = append(providers, seed.Provider{
			Name:            provForms[i].name,
			Provider:        pType,
			BaseURL:         provForms[i].baseURL,
			APISecretKey:    provForms[i].apiKey,
			IsActive:        provForms[i].active,
			DiscoveryPublic: isPublic,
		})
	}

	routing := &seed.Routing{
		Strategies: seed.Strategies(),
		Active:     activeStrategy,
	}

	flags := map[string]bool{
		"rate_limit":     rateLimit,
		"pii":            pii,
		"cache":          cache,
		"budget":         budget,
		"loop_detection": loopDetect,
	}

	return &seed.File{
		Version:        "1.0",
		Providers:      providers,
		MCPServers:     nil,
		GuardrailRules: seed.DefaultGuardrailRules(),
		Routing:        routing,
		FeatureFlags:   flags,
		DashboardPort:  dashboardPort,
		MetricsPort:    metricsPort,
	}, nil
}
