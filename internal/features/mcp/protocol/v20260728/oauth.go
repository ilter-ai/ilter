package v20260728

import "github.com/ilter-ai/ilter/internal/features/mcp/protocol"

// oauthPolicy implements the 2026-07-28 OAuth additions: Dynamic Client
// Registration (RFC 7591) is kept for backward compatibility per the
// spec's own text ("It remains available for backwards compatibility with
// authorization servers that do not support Client ID Metadata
// Documents"), alongside new support for Client ID Metadata Documents
// (CIMD) — resolving a URL-shaped client_id by fetching and validating its
// metadata document instead of requiring a prior /register call. `iss` is
// included in authorization responses and validated per RFC 9207
// (anti-mix-up protection). `application_type` is required and validated
// during Dynamic Client Registration per PR#837 (avoids OpenID Connect
// redirect URI conflicts).
//
// The actual CIMD-resolution HTTP call, iss validation logic, and
// application_type checks are wired into
// internal/platform/transport/mcp/oauth_endpoints.go in Phase 6
// (ilter-yyil.7.5) — this policy is the version-selection surface Phase 6
// dispatches through, per the user's explicit decision to force OAuth
// apart per version rather than unify it.
type oauthPolicy struct{}

func (oauthPolicy) SupportsCIMD() bool             { return true }
func (oauthPolicy) RequiresIssuerValidation() bool { return true }
func (oauthPolicy) RequiresApplicationType() bool  { return true }

var _ protocol.OAuthPolicy = oauthPolicy{}
