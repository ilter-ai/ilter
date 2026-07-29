package v20250326

import "github.com/ilter-ai/ilter/internal/features/mcp/protocol"

// oauthPolicy is a pure extraction of today's actual OAuth behavior in
// internal/platform/transport/mcp/oauth_endpoints.go: RFC 7591 Dynamic
// Client Registration only (fixed client_id echo, no metadata validation),
// no Client ID Metadata Documents, no `iss` parameter/validation (RFC
// 9207), no `application_type` requirement. Zero behavior change — this
// type exists so oauth_endpoints.go can be rewired (Phase 6) to dispatch
// through protocol.Negotiate(hint).OAuthPolicy() without altering what a
// 2025-03-26-hinted flow actually does today.
type oauthPolicy struct{}

func (oauthPolicy) SupportsCIMD() bool             { return false }
func (oauthPolicy) RequiresIssuerValidation() bool { return false }
func (oauthPolicy) RequiresApplicationType() bool  { return false }

var _ protocol.OAuthPolicy = oauthPolicy{}
