package model

import (
	"fmt"
	"net/url"
)

// MCPServer represents an MCP server configuration stored in runtime_config.
// Fields mirror the runtime_config DB model for MCP server db.
type MCPServer struct {
	Name        string            `json:"name"`
	EndpointURL string            `json:"endpoint_url,omitempty"`
	AuthToken   string            `json:"auth_token,omitempty"`
	IsEnabled   bool              `json:"is_enabled"`
	Timeout     string            `json:"timeout,omitempty"`
	MaxRetries  int               `json:"max_retries"`
	Transport   string            `json:"transport,omitempty"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Handler     string            `json:"handler,omitempty"`
}

// Validate checks that the MCPServer has valid required fields.
func (s *MCPServer) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("name is required")
	}
	if s.EndpointURL != "" {
		if _, err := url.ParseRequestURI(s.EndpointURL); err != nil {
			return fmt.Errorf("invalid endpoint_url: %w", err)
		}
	}
	return nil
}

// MCPAccessRule defines a per-key or per-group tool access rule.
type MCPAccessRule struct {
	ServerName string   `json:"server_name"`
	KeyID      string   `json:"key_id"`
	GroupID    *int     `json:"group_id,omitempty"`
	Tools      []string `json:"tools"`
	Effect     string   `json:"effect"` // "allow" or "deny"
}

// Validate checks that the MCPAccessRule has valid required fields.
func (r *MCPAccessRule) Validate() error {
	if r.ServerName == "" {
		return fmt.Errorf("server_name is required")
	}
	if r.KeyID == "" {
		return fmt.Errorf("key_id is required")
	}
	if r.Effect != "allow" && r.Effect != "deny" {
		return fmt.Errorf("effect must be 'allow' or 'deny', got %q", r.Effect)
	}
	return nil
}
