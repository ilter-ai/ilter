// Package v20250326 implements the MCP 2025-03-26 protocol revision. This is
// a PURE EXTRACTION of ilter's actual current behavior — before this
// refactor, ilter/internal/features/mcp/gateway.go and hub.go hardcoded
// `mcpProtocolVersion = "2025-03-26"` unconditionally, and this package
// reproduces that exact wire behavior byte-for-byte so plugging it in via
// protocol.Negotiate changes zero observable behavior for today's default
// path. Do not "improve" or "correct" anything here relative to what
// gateway.go/hub.go did before this package existed — that's the whole
// point of doing this extraction first, before building the genuinely new
// 2026-07-28 behavior in the v20260728 package.
package v20250326

import (
	"encoding/json"
	"fmt"

	"github.com/ilter-ai/ilter/internal/features/mcp/protocol"
)

func init() {
	protocol.Register(protocol.V20250326, New)
}

// New constructs the 2025-03-26 Version implementation.
func New() protocol.Version { return version{} }

type version struct{}

func (version) ID() protocol.ID { return protocol.V20250326 }

// --- wire-shape types, mirroring mcp.InitializeParams/InitializeResult/etc.
// field-for-field so JSON round-trips identically to today's behavior. ---

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

// HandleInitialize mirrors gateway.go's handleInitialize exactly: an
// unrecognized/mismatched client protocolVersion is not an error — ilter
// just proceeds and responds with its own version (today's behavior warns
// via log only, which is a call-site concern for package mcp to keep doing
// if it wants; this method's OBSERVABLE behavior, the returned bytes, is
// what must stay byte-identical).
func (version) HandleInitialize(paramsJSON json.RawMessage, serverInfo protocol.ImplementationInfo) (json.RawMessage, error) {
	var params initializeParams
	if len(paramsJSON) > 0 {
		if err := json.Unmarshal(paramsJSON, &params); err != nil {
			return nil, fmt.Errorf("invalid initialize params: %w", err)
		}
	}

	result := initializeResult{
		ProtocolVersion: string(protocol.V20250326),
		Capabilities: serverCapabilities{
			Tools: &serverToolsCap{ListChanged: false},
		},
		ServerInfo: serverInfo,
	}
	return json.Marshal(result)
}

// ValidateRequestMeta is a no-op: 2025-03-26 has no stateless per-request
// `_meta` model — it uses the initialize/notifications-initialized
// handshake instead.
func (version) ValidateRequestMeta(json.RawMessage) error { return nil }

func (version) IsMethodSupported(method string) bool {
	switch method {
	case "initialize", "notifications/initialized", "tools/list", "tools/call", "ping":
		return true
	default:
		return false
	}
}

// WrapToolsListResult reproduces today's plain {tools, nextCursor} shape —
// no CacheableResult fields (ttlMs/cacheScope), those are 2026-07-28-only.
func (version) WrapToolsListResult(toolsJSON json.RawMessage, nextCursor string) (json.RawMessage, error) {
	return json.Marshal(listToolsResult{Tools: toolsJSON, NextCursor: nextCursor})
}

// WrapCallToolResult passes content through unchanged — no `resultType`
// MRTR discriminator exists in this version.
func (version) WrapCallToolResult(contentJSON json.RawMessage, isError bool) (json.RawMessage, error) {
	return json.Marshal(struct {
		Content json.RawMessage `json:"content"`
		IsError bool            `json:"isError,omitempty"`
	}{Content: contentJSON, IsError: isError})
}

// ErrorCode reproduces today's exact codes from mcp/types.go: legacy
// -32000..-32002 custom range, ErrUnsupportedProtocolVersion mapped to
// standard InvalidParams (-32602) since that's the code hub.go's
// handleInitialize/gateway.go's handleInitialize actually used for a
// version mismatch today (there was never a dedicated code for it).
func (version) ErrorCode(kind protocol.ErrorKind) int {
	switch kind {
	case protocol.ErrToolNotFound:
		return -32000
	case protocol.ErrToolExecution:
		return -32001
	case protocol.ErrNotInitialized:
		return -32002
	case protocol.ErrUnsupportedProtocolVersion, protocol.ErrMissingRequiredClientCapability:
		return -32602 // InvalidParams
	case protocol.ErrHeaderMismatch:
		return -32602 // InvalidParams — no dedicated code existed pre-2026-07-28
	case protocol.ErrResourceNotFound:
		return -32002 // ilter never modeled a distinct resources capability pre-2026-07-28
	case protocol.ErrMethodNotFound:
		return -32601 // standard JSON-RPC MethodNotFound
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

// BuildClientHandshake is not used by today's outbound clients for
// 2025-03-26 (client_stdio.go/client_inline.go hardcode 2024-11-05 today —
// see v20241105's version of this method); it exists here so an outbound
// negotiation attempt at 2025-03-26 (e.g. a downstream server that only
// accepts this version) is fully faithful too.
func (version) BuildClientHandshake(clientInfo protocol.ImplementationInfo) (string, json.RawMessage, bool) {
	params, _ := json.Marshal(initializeParams{
		ProtocolVersion: string(protocol.V20250326),
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
	if result.ProtocolVersion != "" && result.ProtocolVersion != string(protocol.V20250326) {
		return protocol.ErrHandshakeRejected
	}
	return nil
}

func (version) OAuthPolicy() protocol.OAuthPolicy { return oauthPolicy{} }
