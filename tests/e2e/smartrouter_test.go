// allow: SIZE_OK — 6 curl-style e2e test functions with per-test config
// blocks. Each test defines its own config and model map; the repeated
// provider config is inherent to independent httptest-style tests and
// cannot be deduplicated without obscuring per-test setup.
package e2e

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/model"
	testutil "github.com/ilter-ai/ilter/tests/utils"
)

// curlModels maps the curl-style model IDs to their smart-router tiers.
var curlModels = map[string]string{
	"economy-model":  "economy",
	"standard-model": "standard",
	"premium-model":  "premium",
}

// TestCurlE2E_SimplePrompt verifies that a simple "Hi" prompt with empty model
// routes to economy-model (economy) and sets the X-Ilter-Complexity-Score header.
func TestCurlE2E_SimplePrompt(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{
			{
				Name: "mock",
				Type: "mock",
				Models: []config.ModelConfig{
					{Name: "economy-model", Weight: 1},
					{Name: "standard-model", Weight: 1},
					{Name: "premium-model", Weight: 1},
				},
			},
		},
		Routing: config.RoutingConfig{
			Enabled: true,
			ComplexityThresholds: config.ComplexityThresholdsConfig{
				Economy:  15.0,
				Standard: 50.0,
			},
		},
	}

	fixt := testutil.NewSmartRouterFixture(t, cfg, curlModels)
	rr := testutil.Serve(t, fixt, testutil.NewTestRequest(t, model.ChatCompletionRequest{
		Model:    "",
		Messages: []model.Message{{Role: "user", Content: "Hi"}},
	}))

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "economy-model", rr.Header().Get("X-Ilter-Model-Selected"))
	assert.NotEmpty(t, rr.Header().Get("X-Ilter-Complexity-Score"))
}

// TestCurlE2E_ComplexPrompt verifies that a complex prompt with reasoning,
// constraints, code blocks, and JSON output requirement routes to premium-model (premium).
func TestCurlE2E_ComplexPrompt(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{
			{
				Name: "mock",
				Type: "mock",
				Models: []config.ModelConfig{
					{Name: "economy-model", Weight: 1},
					{Name: "standard-model", Weight: 1},
					{Name: "premium-model", Weight: 1},
				},
			},
		},
		Routing: config.RoutingConfig{
			Enabled: true,
			ComplexityThresholds: config.ComplexityThresholdsConfig{
				Economy:  15.0,
				Standard: 50.0,
			},
		},
	}

	fixt := testutil.NewSmartRouterFixture(t, cfg, curlModels)
	rr := testutil.Serve(t, fixt, testutil.NewTestRequest(t, model.ChatCompletionRequest{
		Model:    "",
		Messages: []model.Message{{Role: "user", Content: "Analyze the performance of this database schema and provide step-by-step reasoning. You must strictly ensure that there are no table scans. Output must be in json. ```sql SELECT * FROM users ```"}},
	}))

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "premium-model", rr.Header().Get("X-Ilter-Model-Selected"))
	assert.NotEmpty(t, rr.Header().Get("X-Ilter-Complexity-Score"))
}

// TestCurlE2E_ExplicitModel verifies that when an explicit model "standard-model" is
// provided in the request body, the model stays "standard-model" (smart routing is bypassed).
func TestCurlE2E_ExplicitModel(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{
			{
				Name: "mock",
				Type: "mock",
				Models: []config.ModelConfig{
					{Name: "economy-model", Weight: 1},
					{Name: "standard-model", Weight: 1},
					{Name: "premium-model", Weight: 1},
				},
			},
		},
		Routing: config.RoutingConfig{
			Enabled: true,
			ComplexityThresholds: config.ComplexityThresholdsConfig{
				Economy:  15.0,
				Standard: 50.0,
			},
		},
	}

	fixt := testutil.NewSmartRouterFixture(t, cfg, curlModels)
	rr := testutil.Serve(t, fixt, testutil.NewTestRequest(t, model.ChatCompletionRequest{
		Model:    "standard-model",
		Messages: []model.Message{{Role: "user", Content: "Hi"}},
	}))

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "standard-model", rr.Header().Get("X-Ilter-Model-Selected"))
}

// TestCurlE2E_NoModelNoFallback verifies that when routing is enabled and only an
// economy model is available, the smart router selects the economy model via
// adjacent-tier search rather than returning an error.
func TestCurlE2E_NoModelNoFallback(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{
			{
				Name: "mock",
				Type: "mock",
				Models: []config.ModelConfig{
					{Name: "economy-model", Weight: 1},
				},
			},
		},
		Routing: config.RoutingConfig{
			Enabled: true,
			ComplexityThresholds: config.ComplexityThresholdsConfig{
				Economy:  15.0,
				Standard: 50.0,
			},
			// FallbackModel is intentionally left as zero-value (empty string).
		},
	}

	fixt := testutil.NewSmartRouterFixture(t, cfg, curlModels)
	rr := testutil.Serve(t, fixt, testutil.NewTestRequest(t, model.ChatCompletionRequest{
		Model:    "",
		Messages: []model.Message{{Role: "user", Content: "Analyze the performance of this database schema and provide step-by-step reasoning. You must strictly ensure that there are no table scans. Output must be in json. ```sql SELECT * FROM users ```"}},
	}))

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "economy-model", rr.Header().Get("X-Ilter-Model-Selected"))
	assert.NotEmpty(t, rr.Header().Get("X-Ilter-Complexity-Score"))
}

// TestCurlE2E_XIlterHeaders verifies that a simple prompt with empty model returns
// all three X-Ilter-* headers: X-Ilter-Model-Selected, X-Ilter-Complexity-Score,
// and X-Ilter-Cost-Estimate.
func TestCurlE2E_XIlterHeaders(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{
			{
				Name: "mock",
				Type: "mock",
				Models: []config.ModelConfig{
					{
						Name:               "economy-model",
						Weight:             1,
						CostPerInputToken:  0.00000015,
						CostPerOutputToken: 0.00000060,
					},
					{Name: "standard-model", Weight: 1},
					{Name: "premium-model", Weight: 1},
				},
			},
		},
		Routing: config.RoutingConfig{
			Enabled: true,
			ComplexityThresholds: config.ComplexityThresholdsConfig{
				Economy:  15.0,
				Standard: 50.0,
			},
		},
	}

	fixt := testutil.NewSmartRouterFixture(t, cfg, curlModels)
	rr := testutil.Serve(t, fixt, testutil.NewTestRequest(t, model.ChatCompletionRequest{
		Model:    "",
		Messages: []model.Message{{Role: "user", Content: "Hi"}},
	}))

	require.Equal(t, http.StatusOK, rr.Code)
	assert.NotEmpty(t, rr.Header().Get("X-Ilter-Model-Selected"))
	assert.NotEmpty(t, rr.Header().Get("X-Ilter-Complexity-Score"))
	assert.NotEmpty(t, rr.Header().Get("X-Ilter-Cost-Estimate"))
}

// TestCurlE2E_Unauthenticated verifies that a request without an Authorization
// header returns 401 Unauthorized.
func TestCurlE2E_Unauthenticated(t *testing.T) {
	cfg := &config.Config{
		Auth: config.AuthConfig{AdminKey: testutil.AdminKey},
		Providers: []config.ProviderConfig{
			{
				Name: "mock",
				Type: "mock",
				Models: []config.ModelConfig{
					{Name: "economy-model", Weight: 1},
					{Name: "standard-model", Weight: 1},
					{Name: "premium-model", Weight: 1},
				},
			},
		},
		Routing: config.RoutingConfig{
			Enabled: true,
			ComplexityThresholds: config.ComplexityThresholdsConfig{
				Economy:  15.0,
				Standard: 50.0,
			},
		},
	}

	fixt := testutil.NewSmartRouterFixture(t, cfg, curlModels)
	rr := testutil.Serve(t, fixt, testutil.NewTestRequestUnauthenticated(t, model.ChatCompletionRequest{
		Model:    "",
		Messages: []model.Message{{Role: "user", Content: "Hi"}},
	}))

	require.Equal(t, http.StatusUnauthorized, rr.Code)
}
