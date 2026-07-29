package v20260728

import "encoding/json"

// Multi Round-Trip Requests (MRTR) result-type discriminator (SEP-2322).
// Every 2026-07-28 result carries one of these; "complete" is the normal
// case (see WrapCallToolResult in version.go). "input_required" is used by
// features that need to pause a request for more information from the
// client — e.g. a Tasks-extension tool call that needs client input mid-
// execution (Phase 5) — and is built out fully there, since ilter has no
// existing server-initiated roots/sampling/elicitation flow to retrofit
// (this protocol version deprecates those in favor of MRTR, but ilter never
// implemented them in the first place — see the MCP gap analysis this
// project started from).
const (
	resultTypeComplete      = "complete"
	resultTypeInputRequired = "input_required"
)

// InputRequiredResult is the MRTR interim-result shape: instead of a normal
// result, a server returns this when it needs more information before it
// can finish processing a request. The client answers by retrying the
// original request with `inputResponses` populated (SEP-2322) — there is no
// separate "answer" RPC method, unlike the old server-initiated model this
// replaces.
type InputRequiredResult struct {
	ResultType    string          `json:"resultType"`
	InputRequests json.RawMessage `json:"inputRequests"`
}

// NewInputRequiredResult builds an InputRequiredResult with the discriminator
// pre-filled, for callers (Phase 5's Tasks engine) that need to pause a
// request pending client input.
func NewInputRequiredResult(inputRequests json.RawMessage) InputRequiredResult {
	return InputRequiredResult{ResultType: resultTypeInputRequired, InputRequests: inputRequests}
}
