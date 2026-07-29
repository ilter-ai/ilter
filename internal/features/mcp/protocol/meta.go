package protocol

import "encoding/json"

// Meta keys defined by the 2026-07-28 stateless protocol core. Every request
// carries its protocol version and client identity here instead of via the
// (now-removed) initialize handshake; every result may carry the server's
// identity and, for change-notification streams, a subscription id.
const (
	MetaKeyProtocolVersion    = "io.modelcontextprotocol/protocolVersion"
	MetaKeyClientCapabilities = "io.modelcontextprotocol/clientCapabilities"
	MetaKeyClientInfo         = "io.modelcontextprotocol/clientInfo"
	MetaKeyServerInfo         = "io.modelcontextprotocol/serverInfo"
	MetaKeyLogLevel           = "io.modelcontextprotocol/logLevel"
	MetaKeySubscriptionID     = "io.modelcontextprotocol/subscriptionId"
)

// RequestMeta is the parsed shape of a 2026-07-28 request's `_meta` object.
// Unknown keys are preserved via json.RawMessage passthrough at the call
// site — this struct only surfaces the fields ilter's negotiation and
// dispatch logic needs to read.
type RequestMeta struct {
	ProtocolVersion    ID                  `json:"io.modelcontextprotocol/protocolVersion,omitempty"`
	ClientCapabilities json.RawMessage     `json:"io.modelcontextprotocol/clientCapabilities,omitempty"`
	ClientInfo         *ImplementationInfo `json:"io.modelcontextprotocol/clientInfo,omitempty"`
	LogLevel           string              `json:"io.modelcontextprotocol/logLevel,omitempty"`
}

// ParseRequestMeta decodes a request's raw `_meta` JSON object into a
// RequestMeta. An empty/nil metaJSON yields a zero-value RequestMeta and a
// nil error — absence of `_meta` is meaningful (pre-2026-07-28 clients never
// send it) and is not itself malformed input.
func ParseRequestMeta(metaJSON json.RawMessage) (RequestMeta, error) {
	var m RequestMeta
	if len(metaJSON) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(metaJSON, &m); err != nil {
		return RequestMeta{}, err
	}
	return m, nil
}

// ResultMeta is the shape of a 2026-07-28 result's `_meta` object: servers
// SHOULD identify themselves in every result.
type ResultMeta struct {
	ServerInfo     *ImplementationInfo `json:"io.modelcontextprotocol/serverInfo,omitempty"`
	SubscriptionID string              `json:"io.modelcontextprotocol/subscriptionId,omitempty"`
}

// BuildResultMeta marshals a ResultMeta to raw JSON for embedding as a
// result's `_meta` field.
func BuildResultMeta(m ResultMeta) (json.RawMessage, error) {
	return json.Marshal(m)
}
