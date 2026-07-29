// Package v20260728 implements the MCP 2026-07-28 protocol revision — a
// major breaking release vs. 2025-03-26. This is genuinely new code, not an
// extraction: ilter never spoke this version before. Per
// https://modelcontextprotocol.io/specification/2026-07-28/changelog:
//
//   - No initialize/notifications-initialized handshake. The protocol is
//     stateless: every request carries its own protocol version and client
//     identity in `_meta` (see protocol.RequestMeta), and every result MAY
//     carry the server's identity in `_meta` (protocol.ResultMeta).
//   - `server/discover` replaces version negotiation via initialize.
//   - `ping`, `logging/setLevel`, and `notifications/roots/list_changed`
//     are removed entirely.
//   - Every result carries a `resultType` discriminator ("complete" or
//     "input_required", for the new MRTR pattern — see mrtr.go).
//   - `tools/list` (and other list/read results) carry `ttlMs`/`cacheScope`
//     (CacheableResult) — see cacheable.go.
//   - Custom error codes are renumbered into the -32020..-32099 range
//     reserved for the MCP spec; ilter's own implementation-defined codes
//     (ToolNotFound/ToolExecution) stay in the grandfathered -32000..-32019
//     range per the spec's own error-code allocation policy.
//   - Legacy HTTP+SSE (session header + GET stream) is replaced by
//     Streamable HTTP with mandatory Mcp-Method/Mcp-Name headers and a
//     single `subscriptions/listen` stream for change notifications (see
//     subscriptions.go) instead of a GET-based SSE stream.
//   - OAuth gains CIMD (alongside DCR, kept for back-compat) and `iss`
//     validation (see oauth.go).
//   - Tasks move to the `io.modelcontextprotocol/tasks` extension (see the
//     tasks/ subpackage, wired in Phase 5).
package v20260728

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/ilter-ai/ilter/internal/features/mcp/protocol"
)

func init() {
	protocol.Register(protocol.V20260728, New)
}

// New constructs the 2026-07-28 Version implementation.
func New() protocol.Version { return version{} }

type version struct{}

func (version) ID() protocol.ID { return protocol.V20260728 }

// HandleInitialize: 2026-07-28 has no initialize handshake at all — the
// stateless core removed it (SEP-2575). Callers must route these clients
// through ValidateRequestMeta (per-request) or server/discover instead.
func (version) HandleInitialize(json.RawMessage, protocol.ImplementationInfo) (json.RawMessage, error) {
	return nil, protocol.ErrNoInitializeHandshake
}

// ValidateRequestMeta validates a request's `_meta` block: protocolVersion,
// if present, must be exactly 2026-07-28 (a request that omits it entirely
// is accepted — per-request version pinning is the model, but callers that
// already resolved this Version some other way, e.g. an explicit query
// param, may not need every single request to repeat it).
func (version) ValidateRequestMeta(metaJSON json.RawMessage) error {
	if len(metaJSON) == 0 {
		return nil
	}
	meta, err := protocol.ParseRequestMeta(metaJSON)
	if err != nil {
		return fmt.Errorf("invalid _meta: %w", err)
	}
	if meta.ProtocolVersion != "" && meta.ProtocolVersion != protocol.V20260728 {
		return protocol.ErrHandshakeRejected
	}
	return nil
}

// IsMethodSupported reflects the 2026-07-28 method surface: server/discover
// and the Tasks extension methods are new; ping, logging/setLevel,
// notifications/roots/list_changed, initialize, and
// notifications/initialized are all removed. ilter does not implement an
// MCP resources capability in any version, so resources/* stay unsupported
// here too (not a 2026-07-28-specific gap).
func (version) IsMethodSupported(method string) bool {
	switch method {
	case "tools/list", "tools/call",
		protocol.MethodServerDiscover,
		MethodTasksGet, MethodTasksUpdate,
		MethodSubscriptionsListen:
		return true
	default:
		return false
	}
}

// defaultToolsListTTLMs is the freshness hint (CacheableResult.ttlMs) ilter
// advertises for tools/list results: tool registrations change rarely
// (admin-driven), so a conservative 60s hint lets clients avoid re-polling
// on every turn without risking meaningfully stale data.
const defaultToolsListTTLMs = 60_000

type cacheableToolsListResult struct {
	Tools      json.RawMessage `json:"tools"`
	NextCursor string          `json:"nextCursor,omitempty"`
	TTLMs      int             `json:"ttlMs"`
	CacheScope string          `json:"cacheScope"`
}

// WrapToolsListResult adds the CacheableResult fields (ttlMs/cacheScope)
// required by 2026-07-28. cacheScope is "private" — ilter's tools/list
// result is authorization-filtered per API key (Authorizer.GetAuthorizedTools),
// so it must not be treated as safely cacheable by a shared intermediary.
func (version) WrapToolsListResult(toolsJSON json.RawMessage, nextCursor string) (json.RawMessage, error) {
	return json.Marshal(cacheableToolsListResult{
		Tools:      toolsJSON,
		NextCursor: nextCursor,
		TTLMs:      defaultToolsListTTLMs,
		CacheScope: "private",
	})
}

// WrapCallToolResult applies the MRTR `resultType` discriminator (see
// mrtr.go for the "input_required" variant used by Phase 4/5 features);
// a normal, fully-completed tool call is always "complete".
func (version) WrapCallToolResult(contentJSON json.RawMessage, isError bool) (json.RawMessage, error) {
	return json.Marshal(struct {
		Content    json.RawMessage `json:"content"`
		IsError    bool            `json:"isError,omitempty"`
		ResultType string          `json:"resultType"`
	}{Content: contentJSON, IsError: isError, ResultType: resultTypeComplete})
}

// ErrorCode implements the 2026-07-28 error-code allocation policy: -32000
// to -32019 remains implementation-defined (ilter's own ToolNotFound/
// ToolExecution are grandfathered here, unchanged), -32020 to -32099 is
// reserved for spec-defined conditions, renumbered from their draft values
// per the changelog (HeaderMismatch -32001->-32020, MissingRequiredClientCapability
// -32003->-32021, UnsupportedProtocolVersion -32004->-32022). Resource-not-found
// moves to standard InvalidParams (-32602) per the spec's explicit change.
func (version) ErrorCode(kind protocol.ErrorKind) int {
	switch kind {
	case protocol.ErrToolNotFound:
		return -32000 // implementation-defined, grandfathered
	case protocol.ErrToolExecution:
		return -32001 // implementation-defined, grandfathered
	case protocol.ErrNotInitialized:
		// The stateless core has no handshake to "not have done" — this
		// value is defensive/unused by Dispatch for this version, kept
		// for interface completeness.
		return -32002
	case protocol.ErrHeaderMismatch:
		return -32020
	case protocol.ErrMissingRequiredClientCapability:
		return -32021
	case protocol.ErrUnsupportedProtocolVersion:
		return -32022
	case protocol.ErrResourceNotFound:
		return -32602 // InvalidParams, per spec's explicit renumbering
	case protocol.ErrMethodNotFound:
		return -32601 // standard JSON-RPC MethodNotFound
	default:
		return -32603 // Internal
	}
}

func (version) Transport() protocol.TransportRequirements {
	return protocol.TransportRequirements{
		StatefulSessions:            false,
		RequiresMcpMethodHeader:     true,
		RequiresMcpNameHeader:       true,
		SupportsLegacySSEGet:        false,
		SupportsSubscriptionsListen: true,
	}
}

// discoverParams/discoverResult mirror protocol.DiscoverResult's wire shape
// for the client role (ilter calling OUT to a downstream server that may
// itself understand server/discover).
type discoverResult struct {
	ProtocolVersions []protocol.ID               `json:"protocolVersions"`
	ServerInfo       protocol.ImplementationInfo `json:"serverInfo"`
}

// BuildClientHandshake sends server/discover as ilter's first message when
// attempting to negotiate 2026-07-28 with a downstream server. needsInitialize
// is false: this version has no handshake to wait on before doing real work
// in the stateless sense, but outbound negotiation (Phase 3) still inspects
// the server/discover response via ParseServerHandshake to confirm the
// downstream server actually understands this version before committing to
// it — that confirmation step is a negotiation concern, not a per-request
// protocol requirement, hence needsInitialize=false here.
func (version) BuildClientHandshake(clientInfo protocol.ImplementationInfo) (string, json.RawMessage, bool) {
	params, _ := json.Marshal(struct {
		ClientInfo protocol.ImplementationInfo `json:"clientInfo"`
	}{ClientInfo: clientInfo})
	return protocol.MethodServerDiscover, params, false
}

// ParseServerHandshake accepts a server/discover-shaped response only if it
// explicitly lists 2026-07-28 among its supported protocolVersions. A
// downstream server that doesn't understand server/discover at all will
// return a JSON-RPC MethodNotFound error (surfaced as a transport/call
// error before this function is ever reached) or an unrelated shape here,
// both of which the outbound negotiator (Phase 3) treats as rejection and
// falls back to the next-older version.
func (version) ParseServerHandshake(resultJSON json.RawMessage) error {
	var result discoverResult
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		return fmt.Errorf("parse server/discover result: %w", err)
	}
	if slices.Contains(result.ProtocolVersions, protocol.V20260728) {
		return nil
	}
	return protocol.ErrHandshakeRejected
}

func (version) OAuthPolicy() protocol.OAuthPolicy { return oauthPolicy{} }
