package v20241105

import "github.com/ilter-ai/ilter/internal/features/mcp/protocol"

// oauthPolicy: the 2024-11-05 MCP spec predates the OAuth authorization
// framework entirely (it was added in 2025-03-26) — no real-world
// 2024-11-05 client will ever discover or hit ilter's OAuth endpoints,
// since a client that old has no concept of `.well-known/oauth-*`
// discovery to find them in the first place.
//
// ilter still needs a working policy here rather than an error, though:
// ilter's own auth model requires a token regardless of which MCP version
// a client speaks (a stdio-registered downstream server, or a defensive
// direct hit on the OAuth endpoints with this version hinted, must not
// crash). This policy is therefore functionally identical to v20250326's
// (DCR only, no CIMD, no iss, no application_type) — it exists to satisfy
// "implement all 3 versions for real" rather than because 2024-11-05
// itself mandates anything here.
type oauthPolicy struct{}

func (oauthPolicy) SupportsCIMD() bool             { return false }
func (oauthPolicy) RequiresIssuerValidation() bool { return false }
func (oauthPolicy) RequiresApplicationType() bool  { return false }

var _ protocol.OAuthPolicy = oauthPolicy{}
