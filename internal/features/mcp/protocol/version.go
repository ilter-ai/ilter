// Package protocol implements ilter's MCP (Model Context Protocol)
// version-negotiation layer. ilter supports three protocol revisions at
// once — 2024-11-05, 2025-03-26, and 2026-07-28 — each with genuinely
// different wire behavior (handshake model, transport rules, method
// surface, error codes, capability shape). Every version-specific behavior
// lives in its own subpackage (protocol/v20241105, protocol/v20250326,
// protocol/v20260728) implementing the Version interface declared here.
//
// This package intentionally has zero dependency on package mcp (which owns
// the JSON-RPC envelope types, e.g. mcp.JSONRPCRequest): Version methods
// speak json.RawMessage at the boundary so each version subpackage can
// define its own faithful wire-shape structs internally. Concrete version
// implementations register themselves via Register (called from an init()
// in each subpackage) rather than being imported directly here, avoiding an
// import cycle — the same blank-import-for-side-effect pattern used by
// database/sql drivers and the image/* format packages in the standard
// library. Callers that need negotiation to work (package mcp) blank-import
// the version subpackages once, e.g.:
//
//	import (
//		_ "github.com/ilter-ai/ilter/internal/features/mcp/protocol/v20241105"
//		_ "github.com/ilter-ai/ilter/internal/features/mcp/protocol/v20250326"
//		_ "github.com/ilter-ai/ilter/internal/features/mcp/protocol/v20260728"
//	)
package protocol

import (
	"encoding/json"
	"fmt"
	"slices"
	"sync"
)

// ID identifies an MCP protocol revision by its spec date string, exactly as
// it appears on the wire (InitializeParams.ProtocolVersion, etc.).
type ID string

const (
	V20241105 ID = "2024-11-05"
	V20250326 ID = "2025-03-26"
	V20260728 ID = "2026-07-28"
)

// Supported lists every MCP protocol version ilter implements, ordered
// newest first. Used both for inbound advertisement (server/discover) and
// outbound negotiation (try newest, fall back to the next-older version).
var Supported = []ID{V20260728, V20250326, V20241105}

// Newest returns the newest protocol version ilter supports.
func Newest() ID { return Supported[0] }

// IsSupported reports whether id is one of the versions ilter implements.
func IsSupported(id ID) bool {
	return slices.Contains(Supported, id)
}

// ImplementationInfo identifies an MCP client or server (name + version).
// Mirrored here rather than imported from package mcp to keep this package
// dependency-free of package mcp; the two structs are wire-compatible
// (same JSON field names) by construction.
type ImplementationInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// TransportRequirements describes the HTTP-transport-level rules a version
// imposes. Consulted by the transport layer
// (internal/platform/transport/mcp) to decide header requirements and which
// change-notification mechanism to serve.
type TransportRequirements struct {
	// StatefulSessions is true for versions with a session/handshake model
	// (2024-11-05, 2025-03-26): a session is created at `initialize` and
	// reused for the connection's lifetime. False for 2026-07-28, which is
	// stateless — every request carries its own version/capabilities.
	StatefulSessions bool

	// RequiresMcpMethodHeader / RequiresMcpNameHeader: 2026-07-28 requires
	// these standard headers on every Streamable HTTP POST so
	// load-balancers/gateways can route on the operation without
	// inspecting the body.
	RequiresMcpMethodHeader bool
	RequiresMcpNameHeader   bool

	// SupportsLegacySSEGet is true when a GET request opens a legacy SSE
	// stream for server-to-client notifications (2024-11-05, 2025-03-26).
	SupportsLegacySSEGet bool

	// SupportsSubscriptionsListen is true when `subscriptions/listen` is
	// the change-notification mechanism (2026-07-28 only), replacing the
	// legacy SSE GET stream.
	SupportsSubscriptionsListen bool
}

// OAuthPolicy describes the version-specific OAuth client-registration and
// authorization behavior (Phase 6). Concrete policies live in each
// version's oauth.go and are consumed directly by
// internal/platform/transport/mcp/oauth_endpoints.go.
type OAuthPolicy interface {
	// SupportsCIMD reports whether this version resolves a URL-shaped
	// client_id as a Client ID Metadata Document (2026-07-28+).
	SupportsCIMD() bool
	// RequiresIssuerValidation reports whether `iss` must be included in
	// authorization responses and validated per RFC 9207 (2026-07-28+).
	RequiresIssuerValidation() bool
	// RequiresApplicationType reports whether Dynamic Client Registration
	// must include/validate `application_type` (2026-07-28+).
	RequiresApplicationType() bool
}

// Version encapsulates every version-specific MCP protocol behavior: server
// role (ilter as MCP server, handshake/dispatch), client role (ilter as MCP
// client connecting outbound to a registered downstream server), transport
// rules, error codes, and OAuth policy.
//
// Methods speak json.RawMessage at the boundary rather than this package's
// own structs so each version subpackage can define its own wire-shape
// types internally without protocol depending on package mcp.
type Version interface {
	ID() ID

	// --- Server role (ilter as MCP server) ---

	// HandleInitialize processes an `initialize` request's raw params and
	// returns the raw InitializeResult bytes to send back. v20260728 (which
	// has no initialize handshake) returns ErrNoInitializeHandshake —
	// callers should route 2026-07-28 clients through
	// ValidateRequestMeta/server-discover instead.
	HandleInitialize(paramsJSON json.RawMessage, serverInfo ImplementationInfo) (resultJSON json.RawMessage, err error)

	// ValidateRequestMeta validates a per-request `_meta` block against
	// this version's expectations. A no-op returning nil for the two
	// older, session-based versions; meaningful only for v20260728's
	// stateless model.
	ValidateRequestMeta(metaJSON json.RawMessage) error

	// IsMethodSupported reports whether method is part of this version's
	// JSON-RPC method surface (e.g. "ping" is false for v20260728, which
	// removed it; "server/discover" is false for the two older versions,
	// which never had it).
	IsMethodSupported(method string) bool

	// WrapToolsListResult wraps an already-built tools array (raw JSON)
	// in this version's ListToolsResult shape. v20260728 adds
	// ttlMs/cacheScope (CacheableResult); the two older versions pass
	// through unchanged (nextCursor-only shape).
	WrapToolsListResult(toolsJSON json.RawMessage, nextCursor string) (json.RawMessage, error)

	// WrapCallToolResult applies this version's result envelope.
	// v20260728 adds the `resultType` MRTR discriminator; older versions
	// pass through unchanged.
	WrapCallToolResult(contentJSON json.RawMessage, isError bool) (json.RawMessage, error)

	ErrorCode(kind ErrorKind) int

	Transport() TransportRequirements

	// --- Client role (ilter as MCP client to a downstream server) ---

	// BuildClientHandshake returns the JSON-RPC method + params ilter
	// should send to a downstream server as its first message when
	// attempting to negotiate this version (e.g. "initialize" for the two
	// older versions, "server/discover" for v20260728). needsInitialize
	// is false for v20260728 (stateless — the caller proceeds directly to
	// real work with per-request _meta instead of waiting on a handshake
	// response).
	BuildClientHandshake(clientInfo ImplementationInfo) (method string, paramsJSON json.RawMessage, needsInitialize bool)

	// ParseServerHandshake inspects a downstream server's handshake
	// response (initialize result, or server/discover result) and
	// confirms this version was actually accepted. An error signals the
	// outbound negotiator to fall back to the next-older version.
	ParseServerHandshake(resultJSON json.RawMessage) error

	OAuthPolicy() OAuthPolicy
}

var (
	registryMu sync.RWMutex
	registry   = map[ID]func() Version{}
)

// Register makes a Version implementation available to Negotiate. It is
// called from each version subpackage's init(), not directly by callers.
// Panics on duplicate registration of the same ID, matching the stdlib
// convention (e.g. sql.Register, image.RegisterFormat) since this always
// indicates a programming error, not a runtime condition.
func Register(id ID, ctor func() Version) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[id]; exists {
		panic(fmt.Sprintf("protocol: Register called twice for version %q", id))
	}
	registry[id] = ctor
}

// Negotiate returns the Version implementation for requested. If requested
// is empty or not one of Supported, it returns the newest supported
// version instead (spec-compliant "server picks a version it supports"
// fallback) rather than an error, since callers (Gateway/Hub dispatch,
// outbound client handshake) need a usable Version to proceed with in
// every case.
//
// Panics if no version implementations have been registered at all (a
// programming error — the caller forgot to blank-import the version
// subpackages), since returning a nil Version would defer the failure to a
// much more confusing nil-pointer panic deep inside Dispatch.
func Negotiate(requested ID) Version {
	registryMu.RLock()
	defer registryMu.RUnlock()

	if len(registry) == 0 {
		panic("protocol: Negotiate called with no registered versions — blank-import protocol/v20241105, v20250326, v20260728")
	}

	if ctor, ok := registry[requested]; ok {
		return ctor()
	}

	for _, id := range Supported {
		if ctor, ok := registry[id]; ok {
			return ctor()
		}
	}
	panic("protocol: Negotiate found a non-empty registry but none of Supported is registered — inconsistent registration")
}
