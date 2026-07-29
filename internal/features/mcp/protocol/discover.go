package protocol

import "encoding/json"

// MethodServerDiscover is the JSON-RPC method name added by the 2026-07-28
// spec: servers MUST implement it to advertise their supported protocol
// versions, capabilities, and identity. Clients MAY call it before any
// other request for up-front version selection, or as a
// backward-compatibility probe on stdio. It is version-agnostic by
// definition — a client that hasn't picked a version yet must be able to
// call it — so it is handled once, here, rather than per-Version.
const MethodServerDiscover = "server/discover"

// DiscoverResult is the response to server/discover: every version ilter
// supports (newest first) plus ilter's own identity, so a client can pick
// the newest version it also understands before sending anything
// version-pinned.
type DiscoverResult struct {
	ProtocolVersions []ID               `json:"protocolVersions"`
	ServerInfo       ImplementationInfo `json:"serverInfo"`
}

// Discover builds the server/discover result. Called by Gateway/Hub
// dispatch before any version has been negotiated for the connection.
func Discover(serverInfo ImplementationInfo) DiscoverResult {
	return DiscoverResult{
		ProtocolVersions: Supported,
		ServerInfo:       serverInfo,
	}
}

// MarshalDiscoverResult is a convenience wrapper for callers that need the
// raw JSON-RPC result bytes directly.
func MarshalDiscoverResult(serverInfo ImplementationInfo) (json.RawMessage, error) {
	return json.Marshal(Discover(serverInfo))
}
