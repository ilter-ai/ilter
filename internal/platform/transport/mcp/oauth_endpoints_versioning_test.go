package mcptransport

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/db/dbtest"
	mcp "github.com/ilter-ai/ilter/internal/features/mcp"
)

// runFullAuthorizeFlow drives /authorize (GET+POST) and /token for a given
// mcp_protocol_version query-string suffix (e.g. "" for no hint, or
// "&mcp_protocol_version=2026-07-28"), returning the final /token response
// body and the /authorize POST's redirect_uri so tests can inspect
// version-specific behavior (iss presence) at both steps.
func runFullAuthorizeFlow(t *testing.T, o *OAuthEndpoints, store *mcp.OAuthStore, rawKey, clientID, versionSuffix string) (tokenResp map[string]any, authorizeRedirectURI string) {
	t.Helper()

	req := httptest.NewRequest("GET", "/authorize?response_type=code&client_id="+url.QueryEscape(clientID)+
		"&redirect_uri=http://localhost:9999/callback&code_challenge_method=S256&code_challenge=E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM&state=mystate"+versionSuffix, nil)
	w := httptest.NewRecorder()
	o.Authorize(w, req)
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())

	loc := w.Header().Get("Location")
	u, err := url.Parse(loc)
	require.NoError(t, err)
	reqID := u.Query().Get("request_id")
	require.NotEmpty(t, reqID)

	consentBody := map[string]any{"request_id": reqID, "action": "use_existing", "api_key": rawKey}
	bodyBytes, _ := json.Marshal(consentBody)
	req = httptest.NewRequest("POST", "/authorize", bytes.NewReader(bodyBytes))
	w = httptest.NewRecorder()
	o.Authorize(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var consentResp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &consentResp))
	redirectURL := consentResp["redirect_uri"].(string)
	uCallback, err := url.Parse(redirectURL)
	require.NoError(t, err)
	code := uCallback.Query().Get("code")
	require.NotEmpty(t, code)

	form := url.Values{}
	form.Set("code", code)
	form.Set("grant_type", "authorization_code")
	form.Set("code_verifier", "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
	req = httptest.NewRequest("POST", "/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	o.Token(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var tr map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tr))
	return tr, redirectURL
}

func TestOAuthEndpoints_NoHint_DefaultsTo2025_NoIss(t *testing.T) {
	database := dbtest.New(t)
	database.DB.SetMaxOpenConns(1)
	_, rawKey, err := database.CreateAPIKey("Test Key", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	store := mcp.NewOAuthStore(database)
	o := NewOAuthEndpoints("http://localhost:8181", "http://localhost:9191", store, database, nil)

	tokenResp, redirectURI := runFullAuthorizeFlow(t, o, store, rawKey, "mcp-client", "")
	assert.NotContains(t, redirectURI, "iss=", "an unhinted (pre-2026-07-28) flow must not add iss — today's exact behavior")
	_, hasIss := tokenResp["iss"]
	assert.False(t, hasIss, "token response must not carry iss for the default (2025-03-26) version")
}

func TestOAuthEndpoints_2026Hint_IncludesIss(t *testing.T) {
	database := dbtest.New(t)
	database.DB.SetMaxOpenConns(1)
	_, rawKey, err := database.CreateAPIKey("Test Key", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	store := mcp.NewOAuthStore(database)
	o := NewOAuthEndpoints("http://localhost:8181", "http://localhost:9191", store, database, nil)

	tokenResp, redirectURI := runFullAuthorizeFlow(t, o, store, rawKey, "mcp-client", "&mcp_protocol_version=2026-07-28")
	assert.Contains(t, redirectURI, "iss="+url.QueryEscape("http://localhost:8181"), "2026-07-28 authorization response must include iss (RFC 9207)")
	assert.Equal(t, "http://localhost:8181", tokenResp["iss"], "2026-07-28 token response must include iss")
}

func TestOAuthEndpoints_Register_NoHint_NoApplicationTypeRequired(t *testing.T) {
	o := NewOAuthEndpoints("http://localhost:8181", "http://localhost:9191", mcp.NewOAuthStore(nil), nil, nil)

	req := httptest.NewRequest("POST", "/register", nil)
	w := httptest.NewRecorder()
	o.Register(w, req)

	assert.Equal(t, http.StatusCreated, w.Code, w.Body.String())
}

func TestOAuthEndpoints_Register_2026Hint_RequiresApplicationType(t *testing.T) {
	o := NewOAuthEndpoints("http://localhost:8181", "http://localhost:9191", mcp.NewOAuthStore(nil), nil, nil)

	t.Run("missing application_type is rejected", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/register?mcp_protocol_version=2026-07-28", bytes.NewReader([]byte(`{}`)))
		w := httptest.NewRecorder()
		o.Register(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("valid application_type is accepted", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"application_type": "native"})
		req := httptest.NewRequest("POST", "/register?mcp_protocol_version=2026-07-28", bytes.NewReader(body))
		w := httptest.NewRecorder()
		o.Register(w, req)
		assert.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	})
}

func TestOAuthEndpoints_CIMD_2026Hint_ResolvesValidMetadataDocument(t *testing.T) {
	cimd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"client_name":"Test Client","redirect_uris":["http://localhost:9999/callback"]}`))
	}))
	defer cimd.Close()

	database := dbtest.New(t)
	database.DB.SetMaxOpenConns(1)
	store := mcp.NewOAuthStore(database)
	o := NewOAuthEndpoints("http://localhost:8181", "http://localhost:9191", store, database, nil)

	req := httptest.NewRequest("GET", "/authorize?response_type=code&client_id="+url.QueryEscape(cimd.URL)+
		"&redirect_uri=http://localhost:9999/callback&code_challenge_method=S256&code_challenge=E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM&state=mystate&mcp_protocol_version=2026-07-28", nil)
	w := httptest.NewRecorder()
	o.Authorize(w, req)
	assert.Equal(t, http.StatusFound, w.Code, w.Body.String())
}

func TestOAuthEndpoints_CIMD_2026Hint_RejectsUnresolvableClientID(t *testing.T) {
	database := dbtest.New(t)
	database.DB.SetMaxOpenConns(1)
	store := mcp.NewOAuthStore(database)
	o := NewOAuthEndpoints("http://localhost:8181", "http://localhost:9191", store, database, nil)

	req := httptest.NewRequest("GET", "/authorize?response_type=code&client_id="+url.QueryEscape("http://127.0.0.1:1/nonexistent")+
		"&redirect_uri=http://localhost:9999/callback&code_challenge_method=S256&code_challenge=E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM&state=mystate&mcp_protocol_version=2026-07-28", nil)
	w := httptest.NewRecorder()
	o.Authorize(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, "a URL-shaped client_id that can't be resolved as a CIMD must be rejected for a 2026-07-28 flow")
}

func TestOAuthEndpoints_CIMD_OlderVersion_URLClientIDNotResolved(t *testing.T) {
	// Same unreachable URL client_id, but WITHOUT the 2026-07-28 hint —
	// today's exact behavior treats it as an opaque string, no fetch
	// attempted, so the flow proceeds normally (unlike the 2026-07-28 case
	// above, which rejects it).
	database := dbtest.New(t)
	database.DB.SetMaxOpenConns(1)
	store := mcp.NewOAuthStore(database)
	o := NewOAuthEndpoints("http://localhost:8181", "http://localhost:9191", store, database, nil)

	req := httptest.NewRequest("GET", "/authorize?response_type=code&client_id="+url.QueryEscape("http://127.0.0.1:1/nonexistent")+
		"&redirect_uri=http://localhost:9999/callback&code_challenge_method=S256&code_challenge=E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM&state=mystate", nil)
	w := httptest.NewRecorder()
	o.Authorize(w, req)
	assert.Equal(t, http.StatusFound, w.Code, w.Body.String())
}
