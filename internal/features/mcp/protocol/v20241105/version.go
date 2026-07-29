// Package v20241105 implements the MCP 2024-11-05 protocol revision — the
// very first stable MCP spec. Today, ilter only speaks this version on the
// OUTBOUND side (as a client connecting to downstream MCP servers):
// client_stdio.go and client_inline.go hardcode this exact
// protocolVersion/clientInfo handshake. This package's BuildClientHandshake
// is a PURE EXTRACTION of that hardcoded behavior, byte-for-byte.
//
// ilter never had a 2024-11-05 SERVER role before this refactor (gateway.go
// and hub.go only ever spoke 2025-03-26 inbound) — the server-role methods
// here (HandleInitialize, WrapToolsListResult, etc.) are genuinely new
// code, needed so an inbound client that explicitly requests 2024-11-05 at
// initialize gets a faithful, real response in that version's shape rather
// than being silently upgraded.
package v20241105

import (
	"encoding/json"
	"fmt"

	"github.com/ilter-ai/ilter/internal/features/mcp/protocol"
)

func init() {
	protocol.Register(protocol.V20241105, New)
}

// New constructs the 2024-11-05 Version implementation.
func New() protocol.Version { return version{} }

type version struct{}

func (version) ID() protocol.ID { return protocol.V20241105 }

type initializeParams struct {
	ProtocolVersion string                      `json:"protocolVersion"`
	Capabilities    clientCapabilities          `json:"capabilities"`
	ClientInfo      protocol.ImplementationInfo `json:"clientInfo"`
}

type clientCapabilities struct {
	Roots    *struct{} `json:"roots,omitempty"`
	Sampling *struct{} `json:"sampling,omitempty"`
}

type initializeResult struct {
	ProtocolVersion string                      `json:"protocolVersion"`
	Capabilities    serverCapabilities          `json:"capabilities"`
	ServerInfo      protocol.ImplementationInfo `json:"serverInfo"`
	Instructions    string                      `json:"instructions,omitempty"`
}

type serverCapabilities struct {
	Tools     *serverToolsCap `json:"tools,omitempty"`
	Resources *struct{}       `json:"resources,omitempty"`
	Prompts   *struct{}       `json:"prompts,omitempty"`
	Logging   *struct{}       `json:"logging,omitempty"`
}

type serverToolsCap struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

type listToolsResult struct {
	Tools      json.RawMessage `json:"tools"`
	NextCursor string          `json:"nextCursor,omitempty"`
}

func (version) HandleInitialize(paramsJSON json.RawMessage, serverInfo protocol.ImplementationInfo) (json.RawMessage, error) {
	var params initializeParams
	if len(paramsJSON) > 0 {
		if err := json.Unmarshal(paramsJSON, &params); err != nil {
			return nil, fmt.Errorf("invalid initialize params: %w", err)
		}
	}

	result := initializeResult{
		ProtocolVersion: string(protocol.V20241105),
		Capabilities: serverCapabilities{
			Tools: &serverToolsCap{ListChanged: false},
		},
		ServerInfo: serverInfo,
	}
	return json.Marshal(result)
}

// ValidateRequestMeta is a no-op: 2024-11-05 predates the stateless
// per-request `_meta` model entirely.
func (version) ValidateRequestMeta(json.RawMessage) error { return nil }

func (version) IsMethodSupported(method string) bool {
	switch method {
	case "initialize", "notifications/initialized", "tools/list", "tools/call", "ping":
		return true
	default:
		return false
	}
}

func (version) WrapToolsListResult(toolsJSON json.RawMessage, nextCursor string) (json.RawMessage, error) {
	return json.Marshal(listToolsResult{Tools: toolsJSON, NextCursor: nextCursor})
}

func (version) WrapCallToolResult(contentJSON json.RawMessage, isError bool) (json.RawMessage, error) {
	return json.Marshal(struct {
		Content json.RawMessage `json:"content"`
		IsError bool            `json:"isError,omitempty"`
	}{Content: contentJSON, IsError: isError})
}

// ErrorCode: 2024-11-05 predates any dedicated MCP error-code allocation —
// same legacy -32000..-32002 custom range ilter already uses, standard
// JSON-RPC codes for everything else.
func (version) ErrorCode(kind protocol.ErrorKind) int {
	switch kind {
	case protocol.ErrToolNotFound:
		return -32000
	case protocol.ErrToolExecution:
		return -32001
	case protocol.ErrNotInitialized:
		return -32002
	case protocol.ErrUnsupportedProtocolVersion, protocol.ErrMissingRequiredClientCapability, protocol.ErrHeaderMismatch:
		return -32602 // InvalidParams
	case protocol.ErrResourceNotFound:
		return -32002
	case protocol.ErrMethodNotFound:
		return -32601
	default:
		return -32603 // Internal
	}
}

func (version) Transport() protocol.TransportRequirements {
	return protocol.TransportRequirements{
		StatefulSessions:            true,
		RequiresMcpMethodHeader:     false,
		RequiresMcpNameHeader:       false,
		SupportsLegacySSEGet:        true,
		SupportsSubscriptionsListen: false,
	}
}

// BuildClientHandshake reproduces client_stdio.go's exact hardcoded
// initialize request byte-for-byte:
// {"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"ilter","version":"0.1.0"}}
func (version) BuildClientHandshake(clientInfo protocol.ImplementationInfo) (string, json.RawMessage, bool) {
	params, _ := json.Marshal(initializeParams{
		ProtocolVersion: string(protocol.V20241105),
		Capabilities:    clientCapabilities{},
		ClientInfo:      clientInfo,
	})
	return "initialize", params, true
}

func (version) ParseServerHandshake(resultJSON json.RawMessage) error {
	var result initializeResult
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		return fmt.Errorf("parse initialize result: %w", err)
	}
	if result.ProtocolVersion != "" && result.ProtocolVersion != string(protocol.V20241105) {
		return protocol.ErrHandshakeRejected
	}
	return nil
}

func (version) OAuthPolicy() protocol.OAuthPolicy { return oauthPolicy{} }
