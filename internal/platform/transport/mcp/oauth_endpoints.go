package mcptransport

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/db"
	mcp "github.com/ilter-ai/ilter/internal/features/mcp"
	"github.com/ilter-ai/ilter/internal/features/mcp/protocol"
)

// OAuthEndpoints implements the MCP OAuth 2.0 PKCE endpoints required by the
// MCP 2025-03-26 specification for HTTP transport.
//
// VSCode and other MCP HTTP clients follow this flow:
//  1. Client hits /mcp → 401 with WWW-Authenticate header
//  2. Client fetches /.well-known/oauth-protected-resource
//  3. Client fetches /.well-known/oauth-authorization-server
//  4. Client opens /authorize in browser → redirects to dashboard UI
//  5. User approves → proxy creates opaque auth code
//  6. Client POSTs to /token with code + PKCE verifier → gets access_token
//  7. Client uses access_token (API key) as Bearer on /mcp
//
// The access_token is a real ilter API key. The OAuth code itself is an
// opaque, single-use, time-limited reference stored in-memory.
type OAuthEndpoints struct {
	baseURL      string              // public-facing base URL, e.g. "http://localhost:8181"
	dashboardURL string              // dashboard base URL, e.g. "http://localhost:9191"
	store        *mcp.OAuthStore     // fail-closed in-memory code store
	db           *db.SQLiteStore     // for API key validation and creation
	oauthCfg     *config.OAuthConfig // optional; nil = use defaults
}

func NewOAuthEndpoints(baseURL, dashboardURL string, store *mcp.OAuthStore, database *db.SQLiteStore, oauthCfg *config.OAuthConfig) *OAuthEndpoints {
	return &OAuthEndpoints{
		baseURL:      strings.TrimRight(baseURL, "/"),
		dashboardURL: strings.TrimRight(dashboardURL, "/"),
		store:        store,
		db:           database,
		oauthCfg:     oauthCfg,
	}
}

// ProtectedResourceMetadata handles GET /.well-known/oauth-protected-resource
// RFC 8707 / MCP spec §Authentication.
func (o *OAuthEndpoints) ProtectedResourceMetadata(w http.ResponseWriter, _ *http.Request) {
	meta := map[string]any{
		"resource":                 o.baseURL,
		"authorization_servers":    []string{o.baseURL},
		"bearer_methods_supported": []string{"header"},
		"resource_documentation":   o.baseURL + "/docs",
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	writeJSON(w, http.StatusOK, meta)
}

// AuthorizationServerMetadata handles GET /.well-known/oauth-authorization-server
// RFC 8414 / MCP spec §Authentication.
func (o *OAuthEndpoints) AuthorizationServerMetadata(w http.ResponseWriter, _ *http.Request) {
	meta := map[string]any{
		"issuer":                                o.baseURL,
		"authorization_endpoint":                o.baseURL + "/authorize",
		"token_endpoint":                        o.baseURL + "/token",
		"registration_endpoint":                 o.baseURL + "/register",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      []string{"mcp"},
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	writeJSON(w, http.StatusOK, meta)
}

// Authorize handles GET, POST, and OPTIONS /authorize.
//
// GET: Validates the OAuth PKCE params, stores a pending request, and
// redirects the browser to the dashboard authorize page.
//
// POST: Called by the dashboard JS after user consent. Creates or validates
// an API key, then issues an opaque auth code. Returns JSON with the
// redirect_uri so the dashboard JS can redirect the browser to VSCode.
//
// OPTIONS: CORS preflight — returns 204 with permissive CORS headers.
func (o *OAuthEndpoints) Authorize(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		o.authorizeGet(w, r)
	case http.MethodPost:
		o.authorizePost(w, r)
	case http.MethodOptions:
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (o *OAuthEndpoints) authorizeGet(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	// Validate required OAuth params.
	responseType := q.Get("response_type")
	if responseType != "code" {
		writeOAuthError(w, http.StatusBadRequest, "unsupported_response_type", "Only authorization_code grant is supported")
		return
	}

	clientID := q.Get("client_id")
	if clientID == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "client_id is required")
		return
	}

	// mcp_protocol_version is the connecting client's hint of which MCP
	// version it intends to speak once it opens the actual /mcp
	// connection — resolves which per-version OAuthPolicy this flow uses
	// (explicit product decision: OAuth is forced apart per version, not
	// unified). A request with NO hint at all is exactly today's
	// pre-refactor status quo (no client sends this brand-new param yet),
	// so it defaults to 2025-03-26 — today's actual behavior — rather
	// than the newest version: defaulting an unhinted request to
	// 2026-07-28 would silently start requiring application_type/CIMD
	// for callers that have done nothing wrong, they simply predate this
	// param existing (the same lesson learned the hard way in
	// gateway.go's resolveVersion).
	hintedVersion := protocol.ID(q.Get("mcp_protocol_version"))
	if hintedVersion == "" {
		hintedVersion = protocol.V20250326
	}
	policyVersion := protocol.Negotiate(hintedVersion)
	policy := policyVersion.OAuthPolicy()

	// CIMD (Client ID Metadata Documents, 2026-07-28+): a URL-shaped
	// client_id is resolved by fetching it and validating it's a real
	// metadata document, instead of requiring a prior /register call.
	// Older-version policies don't support this — a URL-shaped client_id
	// under DCR-only is just treated as an opaque string like any other,
	// unchanged from today's behavior.
	if policy.SupportsCIMD() && looksLikeURL(clientID) {
		if err := resolveCIMDClientID(clientID); err != nil {
			writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "failed to resolve Client ID Metadata Document: "+err.Error())
			return
		}
	}

	redirectURI := q.Get("redirect_uri")
	if !isValidRedirectURI(redirectURI) {
		writeOAuthError(w, http.StatusBadRequest, "invalid_redirect_uri", "redirect_uri must be a loopback address (127.0.0.1, [::1], or vscode://)")
		return
	}

	codeChallengeMethod := q.Get("code_challenge_method")
	if codeChallengeMethod != "S256" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "Only S256 code_challenge_method is supported")
		return
	}

	codeChallenge := q.Get("code_challenge")
	if codeChallenge == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "code_challenge is required")
		return
	}

	state := q.Get("state") // optional

	// Store the pending request.
	reqID := o.store.CreateRequest(clientID, redirectURI, codeChallenge, state, string(hintedVersion))

	// Redirect to the dashboard authorize page, passing details as query params.
	dashURL := fmt.Sprintf(
		"%s/authorize?request_id=%s&client_id=%s&redirect_uri=%s&state=%s",
		o.dashboardURL,
		url.QueryEscape(reqID),
		url.QueryEscape(clientID),
		url.QueryEscape(redirectURI),
		url.QueryEscape(state),
	)
	http.Redirect(w, r, dashURL, http.StatusFound)
}

type authorizePostRequest struct {
	RequestID string `json:"request_id"`
	Action    string `json:"action"` // "create_new" or "use_existing"
	APIKey    string `json:"api_key,omitempty"`
	KeyID     string `json:"key_id,omitempty"`
}

func (o *OAuthEndpoints) authorizePost(w http.ResponseWriter, r *http.Request) {
	var req authorizePostRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	if req.RequestID == "" || req.Action == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "request_id and action are required")
		return
	}

	// Look up the pending authorization request.
	authReq := o.store.GetRequest(req.RequestID)
	if authReq == nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "request not found or expired")
		return
	}
	// Clean up the request regardless of outcome.
	defer o.store.DeleteRequest(req.RequestID)

	var apiKey string

	switch req.Action {
	case "create_new":
		if o.db == nil {
			writeOAuthError(w, http.StatusInternalServerError, "server_error", "database not available")
			return
		}
		keyName := fmt.Sprintf("MCP OAuth - %s", authReq.ClientID)

		monthlyBudget := 100.0
		rateLimitRPM := 60
		if o.oauthCfg != nil {
			if o.oauthCfg.DefaultBudget > 0 {
				monthlyBudget = o.oauthCfg.DefaultBudget
			}
			if o.oauthCfg.DefaultRateLimit > 0 {
				rateLimitRPM = o.oauthCfg.DefaultRateLimit
			}
		}
		_, rawKey, err := o.db.CreateAPIKey(keyName, nil, nil, monthlyBudget, 0, rateLimitRPM, 0, nil, nil, nil)
		if err != nil {
			slog.Error("failed to create OAuth API key", "error", err)
			writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to create API key")
			return
		}
		apiKey = rawKey

	case "use_existing":
		if req.KeyID == "" && req.APIKey == "" {
			writeOAuthError(w, http.StatusBadRequest, "invalid_request", "key_id or api_key is required for use_existing action")
			return
		}
		if o.db == nil {
			writeOAuthError(w, http.StatusInternalServerError, "server_error", "database not available")
			return
		}

		if req.KeyID != "" {
			existingKey, err := o.db.GetAPIKey(req.KeyID)
			if err != nil {
				writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "API key not found")
				return
			}
			if !existingKey.Enabled {
				writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "API key is disabled")
				return
			}
			keyName := fmt.Sprintf("MCP OAuth - %s", authReq.ClientID)
			_, rawKey, err := o.db.CreateAPIKey(keyName, existingKey.GroupID, existingKey.UserID, existingKey.MonthlyBudgetUSD, existingKey.MonthlyBudgetTokens, existingKey.RateLimitRPM, existingKey.RateLimitTPM, existingKey.AllowedModels, existingKey.AllowedProviders, existingKey.Tags)
			if err != nil {
				slog.Error("failed to clone OAuth API key", "error", err)
				writeOAuthError(w, http.StatusInternalServerError, "server_error", "failed to create API key")
				return
			}
			apiKey = rawKey
		} else {
			vk, err := o.db.GetActiveKeyByHash(req.APIKey)
			if err != nil || !vk.Enabled {
				writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "API key is invalid or disabled")
				return
			}
			apiKey = req.APIKey
		}

	default:
		writeOAuthError(w, http.StatusBadRequest, "invalid_request", "action must be 'create_new' or 'use_existing'")
		return
	}

	// Issue an opaque authorization code bound to the API key.
	if o.store == nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "store not available")
		return
	}
	code := o.store.CreateCode(apiKey, authReq.RedirectURI, authReq.CodeChallenge, authReq.State, authReq.ProtocolVersion)

	redirectURI := authReq.RedirectURI + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(authReq.State)
	// iss (RFC 9207, anti-mix-up protection): included in the authorization
	// response only for a version whose OAuthPolicy requires it
	// (2026-07-28+) — older versions' flows are byte-for-byte unchanged.
	if protocol.Negotiate(protocol.ID(authReq.ProtocolVersion)).OAuthPolicy().RequiresIssuerValidation() {
		redirectURI += "&iss=" + url.QueryEscape(o.baseURL)
	}

	resp := map[string]string{
		"redirect_uri": redirectURI,
		"code":         code,
		"state":        authReq.State,
	}
	writeJSON(w, http.StatusOK, resp)
}

// Token handles POST /token. VSCode exchanges the opaque auth code for an
// API key (access_token) using PKCE verification.
func (o *OAuthEndpoints) Token(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOAuthError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is accepted")
		return
	}

	var body struct {
		Code         string `json:"code"`
		GrantType    string `json:"grant_type"`
		CodeVerifier string `json:"code_verifier"`
	}
	bodyDecoded := false

	code := r.FormValue("code")
	if code == "" {
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			code = body.Code
			bodyDecoded = true
		}
	}

	if code == "" {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "missing code")
		return
	}

	codeVerifier := r.FormValue("code_verifier")
	if codeVerifier == "" && bodyDecoded {
		codeVerifier = body.CodeVerifier
	}

	// Exchange the code with PKCE verification.
	if o.store == nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "store not available")
		return
	}
	apiKey, redirectURI, state, protocolVersion, ok := o.store.ExchangeCode(code, codeVerifier)
	if !ok {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "code is invalid, expired, or already used")
		return
	}

	// Validate redirect_uri matches (if provided in token request).
	if redirectURI != "" && r.FormValue("redirect_uri") != "" && r.FormValue("redirect_uri") != redirectURI {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri mismatch")
		return
	}

	resp := map[string]any{
		"access_token": apiKey,
		"token_type":   "bearer",
		"scope":        "mcp",
		"redirect_uri": redirectURI,
		"state":        state,
	}
	if protocol.Negotiate(protocol.ID(protocolVersion)).OAuthPolicy().RequiresIssuerValidation() {
		resp["iss"] = o.baseURL
	}
	writeJSON(w, http.StatusOK, resp)
}

// Register handles POST /register (Dynamic Client Registration, RFC 7591)
// and OPTIONS for CORS preflight.
// VSCode may call this to self-register. We echo back a fixed client_id.
func (o *OAuthEndpoints) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// See authorizeGet's identical comment: an unhinted request defaults
	// to today's actual behavior (2025-03-26, DCR-only), not the newest
	// version, so an existing caller that predates this param isn't
	// suddenly required to send application_type.
	hintedVersion := protocol.ID(r.URL.Query().Get("mcp_protocol_version"))
	if hintedVersion == "" {
		hintedVersion = protocol.V20250326
	}
	policy := protocol.Negotiate(hintedVersion).OAuthPolicy()

	// application_type (PR#837, avoids OpenID Connect redirect URI
	// conflicts): required and validated only for a version whose
	// OAuthPolicy demands it (2026-07-28+). Older-version registrations
	// keep today's exact behavior — body ignored entirely, fixed
	// client_id echoed back.
	if policy.RequiresApplicationType() {
		var body struct {
			ApplicationType string `json:"application_type"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body) // best-effort; empty body -> empty ApplicationType -> rejected below
		if body.ApplicationType != "web" && body.ApplicationType != "native" {
			writeOAuthError(w, http.StatusBadRequest, "invalid_client_metadata", "application_type is required (\"web\" or \"native\") for this protocol version")
			return
		}
	}

	resp := map[string]any{
		"client_id":                  "ilter-mcp-client",
		"client_secret_expires_at":   0,
		"grant_types":                []string{"authorization_code"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	}
	writeJSON(w, http.StatusCreated, resp)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// isValidRedirectURI checks that the redirect_uri is a local loopback address
// or a privileged scheme (vscode://). This prevents open-redirect attacks.
// looksLikeURL reports whether s is plausibly a Client ID Metadata
// Document URL (http/https) rather than an opaque DCR-issued client_id
// string.
func looksLikeURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// cimdHTTPClient is used only to fetch Client ID Metadata Documents — a
// short timeout since this blocks the /authorize request path.
var cimdHTTPClient = &http.Client{Timeout: 5 * time.Second}

// resolveCIMDClientID fetches a URL-shaped client_id and validates it
// resolves to a real JSON metadata document, per the 2026-07-28 spec's
// Client ID Metadata Documents mechanism (an alternative to Dynamic
// Client Registration — the client's identity IS the URL, and that URL's
// content is its self-asserted metadata).
func resolveCIMDClientID(clientIDURL string) error {
	resp, err := cimdHTTPClient.Get(clientIDURL)
	if err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("not a valid JSON metadata document: %w", err)
	}
	return nil
}

func isValidRedirectURI(uri string) bool {
	if uri == "" {
		return false
	}
	u, err := url.Parse(uri)
	if err != nil {
		return false
	}
	// Allow vscode:// and vscodium:// schemes (native app protocol handlers).
	if u.Scheme == "vscode" || u.Scheme == "vscodium" {
		return true
	}
	// Allow http/https loopback: 127.0.0.1, [::1], localhost.
	host := strings.ToLower(u.Hostname())
	if host == "127.0.0.1" || host == "::1" || host == "localhost" || host == "0.0.0.0" {
		return u.Scheme == "http" || u.Scheme == "https"
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeOAuthError(w http.ResponseWriter, status int, errType, desc string) {
	// Determine the correct error field name based on status
	// OAuth 2.0 uses "error" and "error_description" for 4xx
	// Our generic API uses "error.message" pattern
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":             errType,
		"error_description": desc,
	})
}
