package model

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// OpenAPITool defines a single OpenAPI tool spec stored in runtime_config.
// It is a flattened, DB-friendly representation of
// OpenAPISpecConfig with additional fields for runtime management.
type OpenAPITool struct {
	// Name is a unique tool identifier (used as the runtime_config key).
	Name string `json:"name"`

	// Description is a human-readable summary of what this tool does.
	Description string `json:"description,omitempty"`

	// SpecURL is the URL or file path to the OpenAPI/Swagger spec.
	SpecURL string `json:"spec_url"`

	// Operations is an allowlist of operation IDs to expose as MCP tools.
	// At least one operation is required.
	Operations []string `json:"operations"`

	// AuthType specifies the authentication mechanism:
	// "bearer" | "api_key" | "basic" | "none"
	AuthType string `json:"auth_type,omitempty"`

	// AuthValue is the credential value (supports "${ENV_VAR}" substitution).
	AuthValue string `json:"auth_value,omitempty"`

	// AuthKey is the header name for api_key auth type (default "X-API-Key").
	AuthKey string `json:"auth_key,omitempty"`

	// Timeout is the per-request timeout for tool execution.
	Timeout time.Duration `json:"timeout,omitempty"`

	// IsEnabled controls whether this tool is active at runtime.
	IsEnabled bool `json:"is_enabled"`
}

// Validate checks required fields and format constraints for the OpenAPI tool.
func (t *OpenAPITool) Validate() error {
	if strings.TrimSpace(t.Name) == "" {
		return fmt.Errorf("openapi_tool: name is required")
	}
	if strings.TrimSpace(t.SpecURL) == "" {
		return fmt.Errorf("openapi_tool %q: spec_url is required", t.Name)
	}
	// Accept both absolute URLs and local file paths.
	if !isFileLikePath(t.SpecURL) {
		u, err := url.ParseRequestURI(t.SpecURL)
		if err != nil {
			return fmt.Errorf("openapi_tool %q: invalid spec_url %q: %w", t.Name, t.SpecURL, err)
		}
		if u.Scheme == "" {
			return fmt.Errorf("openapi_tool %q: spec_url %q must have a scheme or be a file path", t.Name, t.SpecURL)
		}
	}
	if len(t.Operations) == 0 {
		return fmt.Errorf("openapi_tool %q: at least one operation is required", t.Name)
	}
	return nil
}

// isFileLikePath returns true if specURL looks like a local file path.
func isFileLikePath(specURL string) bool {
	return strings.HasPrefix(specURL, "/") ||
		strings.HasPrefix(specURL, "./") ||
		strings.HasPrefix(specURL, "../") ||
		strings.HasPrefix(specURL, "file://") ||
		(len(specURL) > 1 && specURL[1] == ':')
}
