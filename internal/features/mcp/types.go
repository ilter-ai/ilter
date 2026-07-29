package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/ilter-ai/ilter/internal/features/mcp/protocol"
)

// maxToolNameLen is the maximum length of a tool name sent to external APIs
// (OpenAI/Anthropic both enforce ^[a-zA-Z0-9_-]{1,64}$).
const maxToolNameLen = 64

// maxMCPServerIDLen is the maximum length of an MCP server ID.
const maxMCPServerIDLen = 48

var sha256New = sha256.New

var mcpLog = slog.With("component", "mcp")

// JSON-RPC 2.0 protocol constants.
const (
	JSONRPCVersion = "2.0"

	MethodInitialize               = "initialize"
	MethodNotificationsInitialized = "notifications/initialized"
	MethodToolsList                = "tools/list"
	MethodToolsCall                = "tools/call"
	MethodPing                     = "ping"

	// JSON-RPC error codes (standard).
	ErrorCodeParse          = -32700
	ErrorCodeInvalidRequest = -32600
	ErrorCodeMethodNotFound = -32601
	ErrorCodeInvalidParams  = -32602
	ErrorCodeInternal       = -32603

	// MCP-specific error codes.
	ErrorCodeToolNotFound   = -32000
	ErrorCodeToolExecution  = -32001
	ErrorCodeNotInitialized = -32002
)

// JSONRPCRequest represents a JSON-RPC 2.0 request object.
type JSONRPCRequest struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method"`
	Params  json.RawMessage  `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response object.
type JSONRPCResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *RPCError        `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// InitializeParams is sent by the client during the initialize handshake.
type InitializeParams struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      ImplementationInfo `json:"clientInfo"`
}

// ClientCapabilities advertises optional client features.
type ClientCapabilities struct {
	Roots    *struct{} `json:"roots,omitempty"`
	Sampling *struct{} `json:"sampling,omitempty"`
}

// ImplementationInfo identifies the client or server implementation.
type ImplementationInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is returned by the server on a successful initialize.
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ImplementationInfo `json:"serverInfo"`
	Instructions    string             `json:"instructions,omitempty"`
}

// ServerCapabilities advertises which MCP features the server supports.
type ServerCapabilities struct {
	Tools     *ServerToolsCap `json:"tools,omitempty"`
	Resources *struct{}       `json:"resources,omitempty"`
	Prompts   *struct{}       `json:"prompts,omitempty"`
	Logging   *struct{}       `json:"logging,omitempty"`
}

// ServerToolsCap indicates whether the server supports tools/listChanged.
type ServerToolsCap struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ToolDefinition describes a single MCP tool exposed by a server.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// ListToolsResult is the response to a tools/list request.
type ListToolsResult struct {
	Tools      []ToolDefinition `json:"tools"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

// CallToolParams are the arguments for a tools/call request.
type CallToolParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// CallToolResult is the response to a tools/call request.
type CallToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ToolConfig holds per-tool security configuration persisted in mcp_tool_config.
type ToolConfig struct {
	Destructive          bool `json:"destructive"`
	RequiresConfirmation bool `json:"requires_confirmation"`
	RateLimitRPM         int  `json:"rate_limit_rpm"` // 0 = unlimited
	TimeoutMs            int  `json:"timeout_ms"`     // 0 = server default
}

// ToolContent represents a single piece of content in a tool result.
type ToolContent struct {
	Type     string `json:"type"`               // "text", "image", or "resource"
	Text     string `json:"text,omitempty"`     // used for text content
	Data     string `json:"data,omitempty"`     // used for image content (base64)
	MIMEType string `json:"mimeType,omitempty"` // used for image or resource content
	URI      string `json:"uri,omitempty"`      // used for resource content
}

// NewSuccessResponse builds a JSON-RPC success response.
func NewSuccessResponse(id *json.RawMessage, result any) *JSONRPCResponse {
	raw, _ := json.Marshal(result)
	return &JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Result:  raw,
	}
}

// RequestContext carries per-request authentication info for Streamable HTTP.
type RequestContext struct {
	KeyID     string
	KeyPrefix string
	ClientIP  string

	// ProtocolVersion is the negotiated MCP protocol version for this
	// request. For a stateful (2024-11-05/2025-03-26) session tracked by
	// GatewayHandler's session map, the transport layer populates this
	// from the pinned value before calling Dispatch. For a stateless
	// (2026-07-28) request carrying its own per-request `_meta`, or the
	// very first request on a not-yet-pinned session, it starts empty and
	// Gateway.Dispatch resolves/negotiates it itself. On a successful
	// `initialize` call, Dispatch writes the negotiated result back into
	// this field so the transport layer can persist it for the session.
	ProtocolVersion protocol.ID
}

// NewErrorResponse builds a JSON-RPC error response.
func NewErrorResponse(id *json.RawMessage, code int, message string) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
		},
	}
}

// ValidateMCPServerID checks a server ID for MCP tool name safety.
// Rejects empty, too long, invalid chars, and substrings that would
// break the __ delimiter (__ and /).
func ValidateMCPServerID(id string) error {
	if len(id) == 0 {
		return fmt.Errorf("server ID must not be empty")
	}
	if len(id) > maxMCPServerIDLen {
		return fmt.Errorf("server ID %q exceeds %d character limit", id, maxMCPServerIDLen)
	}
	if strings.Contains(id, "__") {
		return fmt.Errorf("server ID %q contains invalid substring \"__\" (reserved delimiter)", id)
	}
	if strings.Contains(id, "/") {
		return fmt.Errorf("server ID %q contains invalid character '/' (reserved separator)", id)
	}
	for _, r := range id {
		if !isAllowedToolChar(r) {
			return fmt.Errorf("server ID %q contains invalid character %q (allowed: a-z, A-Z, 0-9, -, _)", id, r)
		}
	}
	return nil
}

// SanitizeToolName creates a safe MCP tool name from server ID and tool name.
// The server ID is sanitized (invalid chars → _); the tool name is NOT modified
// so round-trips through ResolveTool work. If the result exceeds maxToolNameLen,
// the tool name is truncated with a short hash suffix.
func SanitizeToolName(serverID, toolName string) string {
	pre := sanitizeToken(serverID) + "__"
	if len(pre)+len(toolName) <= maxToolNameLen {
		return pre + toolName
	}
	h := sha256New()
	h.Write([]byte(serverID))
	h.Write([]byte("__"))
	h.Write([]byte(toolName))
	hash := hex.EncodeToString(h.Sum(nil)[:3]) // 6 chars
	keep := maxToolNameLen - len(pre) - len(hash)
	if keep <= 0 {
		return pre[:maxToolNameLen-len(hash)] + hash
	}
	return pre + toolName[:keep] + hash
}

func isAllowedToolChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
}

func sanitizeToken(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isAllowedToolChar(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}
