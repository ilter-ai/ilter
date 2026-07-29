package v20260728

import "encoding/json"

// MethodSubscriptionsListen is the 2026-07-28 method replacing the legacy
// HTTP GET SSE stream: a single long-lived POST-response stream for
// opted-in server-to-client change notifications. Clients opt in to
// specific notification types; the server acknowledges and tags every
// notification it sends with a subscriptionId.
const MethodSubscriptionsListen = "subscriptions/listen"

// Notification type strings a client may opt into via ListenParams.Types.
const (
	NotifyToolsListChanged      = "toolsListChanged"
	NotifyPromptsListChanged    = "promptsListChanged"
	NotifyResourcesListChanged  = "resourcesListChanged"
	NotifyResourceSubscriptions = "resourceSubscriptions"
)

// ListenParams is the params object for a subscriptions/listen request.
type ListenParams struct {
	Types []string `json:"types"`
}

// ListenAck is the initial JSON-RPC result sent once, synchronously, when
// a subscriptions/listen stream is accepted — carries the subscriptionId
// every subsequent notification on this stream will be tagged with.
type ListenAck struct {
	SubscriptionID string `json:"subscriptionId"`
}

// Notification is a single server-to-client event delivered on an open
// subscriptions/listen stream: a bare JSON-RPC notification (no id) whose
// `_meta.io.modelcontextprotocol/subscriptionId` ties it back to the
// stream that requested it.
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	Meta    struct {
		SubscriptionID string `json:"io.modelcontextprotocol/subscriptionId"`
	} `json:"_meta"`
}

// BuildNotification constructs a Notification envelope for a given
// notification type and subscription id, with an empty/omitted payload
// (ilter's current notifications — e.g. toolsListChanged — carry no
// additional data beyond "this happened").
//
// The JSON-RPC method is "notifications/" + notifyType, mirroring the
// pre-2026-07-28 naming convention (e.g. the older
// `notifications/tools/list_changed`) — the changelog available to this
// implementation describes the opt-in type vocabulary
// (toolsListChanged/promptsListChanged/resourcesListChanged/
// resourceSubscriptions) and the subscriptionId tagging requirement, but
// not a byte-exact wire method name for delivered notifications, so this
// is a deliberate, documented design choice rather than a spec quote.
func BuildNotification(notifyType, subscriptionID string) Notification {
	n := Notification{JSONRPC: "2.0", Method: "notifications/" + notifyType}
	n.Meta.SubscriptionID = subscriptionID
	return n
}
