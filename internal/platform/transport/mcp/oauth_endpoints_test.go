package mcptransport

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ilter-ai/ilter/internal/auth"
	"github.com/ilter-ai/ilter/internal/db"
	"github.com/ilter-ai/ilter/internal/db/dbtest"
	mcp "github.com/ilter-ai/ilter/internal/features/mcp"
)

func TestOAuthEndpoints_Metadata(t *testing.T) {
	o := NewOAuthEndpoints("http://localhost:8181", "http://localhost:9191", mcp.NewOAuthStore(nil), nil, nil)

	// Protected Resource Metadata
	req := httptest.NewRequest("GET", "/.well-known/oauth-protected-resource", nil)
	w := httptest.NewRecorder()
	o.ProtectedResourceMetadata(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))

	var protectedMeta map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &protectedMeta)
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:8181", protectedMeta["resource"])

	// Authorization Server Metadata
	req = httptest.NewRequest("GET", "/.well-known/oauth-authorization-server", nil)
	w = httptest.NewRecorder()
	o.AuthorizationServerMetadata(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var authMeta map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &authMeta)
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:8181", authMeta["issuer"])
	assert.Equal(t, "http://localhost:8181/authorize", authMeta["authorization_endpoint"])
}

func TestOAuthEndpoints_AuthorizeAndToken(t *testing.T) {
	database := dbtest.New(t)
	database.DB.SetMaxOpenConns(1)

	_, rawKey, err := database.CreateAPIKey("Test Key", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	store := mcp.NewOAuthStore(database)
	o := NewOAuthEndpoints("http://localhost:8181", "http://localhost:9191", store, database, nil)

	// 1. GET /authorize
	req := httptest.NewRequest("GET", "/authorize?response_type=code&client_id=mcp-client&redirect_uri=http://localhost:9999/callback&code_challenge_method=S256&code_challenge=E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM&state=mystate", nil)
	w := httptest.NewRecorder()
	o.Authorize(w, req)

	assert.Equal(t, http.StatusFound, w.Code)
	loc := w.Header().Get("Location")
	assert.Contains(t, loc, "http://localhost:9191/authorize?")
	u, err := url.Parse(loc)
	require.NoError(t, err)
	reqID := u.Query().Get("request_id")
	require.NotEmpty(t, reqID)

	// 2. POST /authorize (consent approval)
	authReq := store.GetRequest(reqID)
	require.NotNil(t, authReq)

	consentBody := map[string]any{
		"request_id": reqID,
		"action":     "use_existing",
		"api_key":    rawKey,
	}
	bodyBytes, _ := json.Marshal(consentBody)
	req = httptest.NewRequest("POST", "/authorize", bytes.NewReader(bodyBytes))
	w = httptest.NewRecorder()
	o.Authorize(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var consentResp map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &consentResp)
	require.NoError(t, err)
	redirectURL := consentResp["redirect_uri"].(string)
	assert.Contains(t, redirectURL, "http://localhost:9999/callback?code=")
	uCallback, err := url.Parse(redirectURL)
	require.NoError(t, err)
	code := uCallback.Query().Get("code")
	require.NotEmpty(t, code)

	// 3. POST /token (exchange code for token)
	form := url.Values{}
	form.Set("code", code)
	form.Set("grant_type", "authorization_code")
	form.Set("code_verifier", "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk") // verifier for challenge E9Mel...

	req = httptest.NewRequest("POST", "/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	o.Token(w, req)

	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var tokenResp map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &tokenResp)
	require.NoError(t, err)
	assert.Equal(t, rawKey, tokenResp["access_token"])
	assert.Equal(t, "bearer", tokenResp["token_type"])
}

func TestOAuthEndpoints_Register(t *testing.T) {
	o := NewOAuthEndpoints("http://localhost:8181", "http://localhost:9191", mcp.NewOAuthStore(nil), nil, nil)

	req := httptest.NewRequest("POST", "/register", nil)
	w := httptest.NewRecorder()
	o.Register(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var regResp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &regResp)
	require.NoError(t, err)
	assert.Equal(t, "ilter-mcp-client", regResp["client_id"])
}

// ---------------------------------------------------------------------------
// Error & edge-case tests
// ---------------------------------------------------------------------------

// PKCE standard test vectors from RFC 7636 Appendix B.
const (
	testVerifier  = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	testChallenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
)

func pkceURLParams(t *testing.T) string {
	t.Helper()
	return "response_type=code&client_id=mcp-client&redirect_uri=http://127.0.0.1:9999/callback&code_challenge_method=S256&code_challenge=" + testChallenge + "&state=mystate"
}

// performAuthorizeGet is a test helper that does a GET /authorize and returns
// the request_id from the redirect Location.
func performAuthorizeGet(t *testing.T, o *OAuthEndpoints, extraParams string) (reqID string) {
	t.Helper()
	u := "/authorize?" + pkceURLParams(t)
	if extraParams != "" {
		u = "/authorize?" + extraParams
	}
	req := httptest.NewRequest("GET", u, nil)
	w := httptest.NewRecorder()
	o.Authorize(w, req)
	require.Equal(t, http.StatusFound, w.Code)
	loc, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	reqID = loc.Query().Get("request_id")
	require.NotEmpty(t, reqID)
	return reqID
}

// performConsentApproval completes POST /authorize (consent approval) and
// returns the auth code.  When keyID is non-empty it sends key_id instead of
// api_key so the server clones the referenced key rather than using it directly.
func performConsentApproval(t *testing.T, o *OAuthEndpoints, reqID, action, apiKey, keyID string) string {
	t.Helper()
	body := map[string]any{
		"request_id": reqID,
		"action":     action,
	}
	if keyID != "" {
		body["key_id"] = keyID
	} else if apiKey != "" {
		body["api_key"] = apiKey
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/authorize", bytes.NewReader(b))
	w := httptest.NewRecorder()
	o.Authorize(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	redirectURL, ok := resp["redirect_uri"].(string)
	require.True(t, ok)
	u, err := url.Parse(redirectURL)
	require.NoError(t, err)
	code := u.Query().Get("code")
	require.NotEmpty(t, code)
	return code
}

// performTokenExchange is a test helper that does a form-encoded POST /token.
func performTokenExchange(t *testing.T, o *OAuthEndpoints, code, verifier string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{}
	form.Set("code", code)
	form.Set("grant_type", "authorization_code")
	if verifier != "" {
		form.Set("code_verifier", verifier)
	}
	req := httptest.NewRequest("POST", "/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	o.Token(w, req)
	return w
}

// Setup helpers
func newTestEndpoints() (*OAuthEndpoints, *mcp.OAuthStore) {
	return newTestEndpointsWithDB(nil)
}

func newTestEndpointsWithDB(database *db.SQLiteStore) (*OAuthEndpoints, *mcp.OAuthStore) {
	store := mcp.NewOAuthStore(database)
	o := NewOAuthEndpoints("http://localhost:8181", "http://localhost:9191", store, database, nil)
	return o, store
}

// ---------------------------------------------------------------------------
// GET /authorize validation
// ---------------------------------------------------------------------------

func TestOAuthEndpoints_AuthorizeGet_Validation(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		wantCode int
		wantErr  string // expected "error" field value
	}{
		{
			name:     "missing response_type",
			query:    "client_id=mcp-client&redirect_uri=http://127.0.0.1:9999/callback&code_challenge_method=S256&code_challenge=" + testChallenge,
			wantCode: http.StatusBadRequest,
			wantErr:  "unsupported_response_type",
		},
		{
			name:     "unsupported response_type (token)",
			query:    "response_type=token&client_id=mcp-client&redirect_uri=http://127.0.0.1:9999/callback&code_challenge_method=S256&code_challenge=" + testChallenge,
			wantCode: http.StatusBadRequest,
			wantErr:  "unsupported_response_type",
		},
		{
			name:     "missing client_id",
			query:    "response_type=code&redirect_uri=http://127.0.0.1:9999/callback&code_challenge_method=S256&code_challenge=" + testChallenge,
			wantCode: http.StatusBadRequest,
			wantErr:  "invalid_request",
		},
		{
			name:     "missing redirect_uri",
			query:    "response_type=code&client_id=mcp-client&code_challenge_method=S256&code_challenge=" + testChallenge,
			wantCode: http.StatusBadRequest,
			wantErr:  "invalid_redirect_uri",
		},
		{
			name:     "non-loopback redirect_uri",
			query:    "response_type=code&client_id=mcp-client&redirect_uri=http://evil.com/callback&code_challenge_method=S256&code_challenge=" + testChallenge,
			wantCode: http.StatusBadRequest,
			wantErr:  "invalid_redirect_uri",
		},
		{
			name:     "missing code_challenge_method",
			query:    "response_type=code&client_id=mcp-client&redirect_uri=http://127.0.0.1:9999/callback&code_challenge=" + testChallenge,
			wantCode: http.StatusBadRequest,
			wantErr:  "invalid_request",
		},
		{
			name:     "unsupported code_challenge_method (plain)",
			query:    "response_type=code&client_id=mcp-client&redirect_uri=http://127.0.0.1:9999/callback&code_challenge_method=plain&code_challenge=" + testChallenge,
			wantCode: http.StatusBadRequest,
			wantErr:  "invalid_request",
		},
		{
			name:     "missing code_challenge",
			query:    "response_type=code&client_id=mcp-client&redirect_uri=http://127.0.0.1:9999/callback&code_challenge_method=S256",
			wantCode: http.StatusBadRequest,
			wantErr:  "invalid_request",
		},
		{
			name:     "empty string redirect_uri",
			query:    "response_type=code&client_id=mcp-client&redirect_uri=&code_challenge_method=S256&code_challenge=" + testChallenge,
			wantCode: http.StatusBadRequest,
			wantErr:  "invalid_redirect_uri",
		},
		{
			name:     "vscode scheme redirect_uri (valid)",
			query:    "response_type=code&client_id=mcp-client&redirect_uri=vscode://vscode.github-authentication/auth&code_challenge_method=S256&code_challenge=" + testChallenge,
			wantCode: http.StatusFound,
			wantErr:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, _ := newTestEndpoints()
			req := httptest.NewRequest("GET", "/authorize?"+tt.query, nil)
			w := httptest.NewRecorder()
			o.Authorize(w, req)

			assert.Equal(t, tt.wantCode, w.Code)
			if tt.wantCode == http.StatusBadRequest {
				var errResp map[string]any
				err := json.Unmarshal(w.Body.Bytes(), &errResp)
				require.NoError(t, err)
				assert.Equal(t, tt.wantErr, errResp["error"])
			}
		})
	}
}

// ---------------------------------------------------------------------------
// PKCE: mismatched code_verifier
// ---------------------------------------------------------------------------

func TestOAuthEndpoints_PKCEMismatch(t *testing.T) {
	database := dbtest.New(t)
	_, rawKey, err := database.CreateAPIKey("Test Key", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	o, store := newTestEndpointsWithDB(database)

	reqID := performAuthorizeGet(t, o, "")
	code := performConsentApproval(t, o, reqID, "use_existing", rawKey, "")

	// Exchange with WRONG code_verifier
	w := performTokenExchange(t, o, code, "this-is-the-wrong-verifier")

	assert.Equal(t, http.StatusBadRequest, w.Code, "expected PKCE verification to fail")
	var errResp map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &errResp)
	require.NoError(t, err)
	assert.Equal(t, "invalid_grant", errResp["error"])

	// Also ensure the code was consumed (deleted) by the failed exchange.
	found := store.GetCode(code)
	assert.Nil(t, found, "code must be deleted even on failed PKCE verification")
}

// ---------------------------------------------------------------------------
// Replay: exchanging the same code twice
// ---------------------------------------------------------------------------

func TestOAuthEndpoints_CodeReplay(t *testing.T) {
	database := dbtest.New(t)
	_, rawKey, err := database.CreateAPIKey("Test Key", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	o, _ := newTestEndpointsWithDB(database)

	reqID := performAuthorizeGet(t, o, "")
	code := performConsentApproval(t, o, reqID, "use_existing", rawKey, "")

	// First exchange should succeed.
	w1 := performTokenExchange(t, o, code, testVerifier)
	assert.Equal(t, http.StatusOK, w1.Code, "first exchange should succeed")

	// Second exchange should fail.
	w2 := performTokenExchange(t, o, code, testVerifier)
	assert.Equal(t, http.StatusBadRequest, w2.Code, "second exchange (replay) should fail")
	var errResp map[string]any
	err = json.Unmarshal(w2.Body.Bytes(), &errResp)
	require.NoError(t, err)
	assert.Equal(t, "invalid_grant", errResp["error"])
}

// ---------------------------------------------------------------------------
// Missing code on Token POST
// ---------------------------------------------------------------------------

func TestOAuthEndpoints_Token_MissingCode(t *testing.T) {
	o, _ := newTestEndpoints()

	// POST with no code at all
	req := httptest.NewRequest("POST", "/token", nil)
	w := httptest.NewRecorder()
	o.Token(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	var errResp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &errResp)
	require.NoError(t, err)
	assert.Equal(t, "invalid_grant", errResp["error"])
}

// ---------------------------------------------------------------------------
// Expired code
// ---------------------------------------------------------------------------

func TestOAuthEndpoints_ExpiredCode(t *testing.T) {
	o, store := newTestEndpoints()

	store.SetCode("expired-test", &mcp.AuthCode{
		APIKey:        "sk-test",
		RedirectURI:   "http://127.0.0.1:9999/callback",
		CodeChallenge: testChallenge,
		State:         "mystate",
		ExpiresAt:     time.Now().Add(-1 * time.Hour),
		Used:          false,
	})

	// Try to exchange the expired code.
	w := performTokenExchange(t, o, "expired-test", testVerifier)
	assert.Equal(t, http.StatusBadRequest, w.Code, "expired code should be rejected")
	var errResp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &errResp)
	require.NoError(t, err)
	assert.Equal(t, "invalid_grant", errResp["error"])
}

// ---------------------------------------------------------------------------
// Invalid / expired request_id on POST /authorize
// ---------------------------------------------------------------------------

func TestOAuthEndpoints_InvalidRequestID(t *testing.T) {
	t.Run("non-existent request_id", func(t *testing.T) {
		o, _ := newTestEndpoints()
		body := map[string]any{"request_id": "i-do-not-exist", "action": "create_new"}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/authorize", bytes.NewReader(b))
		w := httptest.NewRecorder()
		o.Authorize(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		var errResp map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &errResp)
		require.NoError(t, err)
		assert.Equal(t, "invalid_grant", errResp["error"])
	})
}

// ---------------------------------------------------------------------------
// Database nil with create_new action
// ---------------------------------------------------------------------------

func TestOAuthEndpoints_DBNilCreateNew(t *testing.T) {
	o, store := newTestEndpoints() // db is nil

	// Create a request through the public API.
	reqID := store.CreateRequest("mcp-client", "http://127.0.0.1:9999/callback", testChallenge, "mystate")

	// Try create_new with db=nil
	body := map[string]any{"request_id": reqID, "action": "create_new"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/authorize", bytes.NewReader(b))
	w := httptest.NewRecorder()
	o.Authorize(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	var errResp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &errResp)
	require.NoError(t, err)
	assert.Equal(t, "server_error", errResp["error"])
}

// ---------------------------------------------------------------------------
// Invalid action on POST /authorize
// ---------------------------------------------------------------------------

func TestOAuthEndpoints_InvalidAction(t *testing.T) {
	o, store := newTestEndpoints()

	reqID := store.CreateRequest("mcp-client", "http://127.0.0.1:9999/callback", testChallenge, "mystate")

	body := map[string]any{"request_id": reqID, "action": "delete_everything"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/authorize", bytes.NewReader(b))
	w := httptest.NewRecorder()
	o.Authorize(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	var errResp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &errResp)
	require.NoError(t, err)
	assert.Equal(t, "invalid_request", errResp["error"])
}

// ---------------------------------------------------------------------------
// Missing request_id or action in POST /authorize body
// ---------------------------------------------------------------------------

func TestOAuthEndpoints_MissingPostFields(t *testing.T) {
	t.Run("missing request_id", func(t *testing.T) {
		o, _ := newTestEndpoints()
		body := map[string]any{"action": "create_new"}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/authorize", bytes.NewReader(b))
		w := httptest.NewRecorder()
		o.Authorize(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing action", func(t *testing.T) {
		o, _ := newTestEndpoints()
		body := map[string]any{"request_id": "some-id"}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest("POST", "/authorize", bytes.NewReader(b))
		w := httptest.NewRecorder()
		o.Authorize(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		o, _ := newTestEndpoints()
		req := httptest.NewRequest("POST", "/authorize", bytes.NewReader([]byte("{bad json")))
		w := httptest.NewRecorder()
		o.Authorize(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// ---------------------------------------------------------------------------
// Token with JSON body
// ---------------------------------------------------------------------------

func TestOAuthEndpoints_TokenJSONBody(t *testing.T) {
	database := dbtest.New(t)
	_, rawKey, err := database.CreateAPIKey("Test Key", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	o, _ := newTestEndpointsWithDB(database)

	reqID := performAuthorizeGet(t, o, "")
	code := performConsentApproval(t, o, reqID, "use_existing", rawKey, "")

	// Send JSON body instead of form-encoded.
	jsonBody := map[string]string{
		"code":          code,
		"grant_type":    "authorization_code",
		"code_verifier": testVerifier,
	}
	b, _ := json.Marshal(jsonBody)
	req := httptest.NewRequest("POST", "/token", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	o.Token(w, req)

	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var tokenResp map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &tokenResp)
	require.NoError(t, err)
	assert.Equal(t, rawKey, tokenResp["access_token"])
	assert.Equal(t, "bearer", tokenResp["token_type"])
}

// ---------------------------------------------------------------------------
// Method not allowed
// ---------------------------------------------------------------------------

func TestOAuthEndpoints_MethodNotAllowed(t *testing.T) {
	o, _ := newTestEndpoints()

	t.Run("Token endpoint: GET", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/token", nil)
		w := httptest.NewRecorder()
		o.Token(w, req)
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
		var errResp map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &errResp)
		require.NoError(t, err)
		assert.Equal(t, "method_not_allowed", errResp["error"])
	})

	t.Run("Authorize endpoint: PUT", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/authorize", nil)
		w := httptest.NewRecorder()
		o.Authorize(w, req)
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("Authorize endpoint: DELETE", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/authorize", nil)
		w := httptest.NewRecorder()
		o.Authorize(w, req)
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

// ---------------------------------------------------------------------------
// Token redirect_uri mismatch
// ---------------------------------------------------------------------------

func TestOAuthEndpoints_TokenRedirectURIMismatch(t *testing.T) {
	database := dbtest.New(t)
	_, rawKey, err := database.CreateAPIKey("Test Key", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	o, _ := newTestEndpointsWithDB(database)

	reqID := performAuthorizeGet(t, o, "")
	code := performConsentApproval(t, o, reqID, "use_existing", rawKey, "")

	// Exchange with mismatched redirect_uri.
	form := url.Values{}
	form.Set("code", code)
	form.Set("grant_type", "authorization_code")
	form.Set("code_verifier", testVerifier)
	form.Set("redirect_uri", "http://evil.com/callback")
	req := httptest.NewRequest("POST", "/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	o.Token(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var errResp map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &errResp)
	require.NoError(t, err)
	assert.Equal(t, "invalid_grant", errResp["error"])
}

// ---------------------------------------------------------------------------
// use_existing with missing api_key
// ---------------------------------------------------------------------------

func TestOAuthEndpoints_UseExistingMissingAPIKey(t *testing.T) {
	o, store := newTestEndpoints()
	reqID := store.CreateRequest("mcp-client", "http://127.0.0.1:9999/callback", testChallenge, "mystate")

	// Send use_existing without api_key.
	body := map[string]any{"request_id": reqID, "action": "use_existing"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/authorize", bytes.NewReader(b))
	w := httptest.NewRecorder()
	o.Authorize(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	var errResp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &errResp)
	require.NoError(t, err)
	assert.Equal(t, "invalid_request", errResp["error"])
}

// ---------------------------------------------------------------------------
// GET /authorize with state (optional param preserved)
// ---------------------------------------------------------------------------

func TestOAuthEndpoints_StatePreservation(t *testing.T) {
	o, _ := newTestEndpoints()

	// GET with state
	query := pkceURLParams(t)
	req := httptest.NewRequest("GET", "/authorize?"+query, nil)
	w := httptest.NewRecorder()
	o.Authorize(w, req)
	assert.Equal(t, http.StatusFound, w.Code)
	loc := w.Header().Get("Location")
	assert.Contains(t, loc, "state=mystate")
}

// ---------------------------------------------------------------------------
// Register OPTIONS preflight
// ---------------------------------------------------------------------------

func TestOAuthEndpoints_RegisterPreflight(t *testing.T) {
	o := NewOAuthEndpoints("http://localhost:8181", "http://localhost:9191", mcp.NewOAuthStore(nil), nil, nil)

	req := httptest.NewRequest("OPTIONS", "/register", nil)
	w := httptest.NewRecorder()
	o.Register(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
}

// ---------------------------------------------------------------------------
// Authorize OPTIONS preflight
// ---------------------------------------------------------------------------

func TestOAuthEndpoints_AuthorizePreflight(t *testing.T) {
	o, _ := newTestEndpoints()

	req := httptest.NewRequest("OPTIONS", "/authorize", nil)
	w := httptest.NewRecorder()
	o.Authorize(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
}

// ---------------------------------------------------------------------------
// PKCE bypass: form-encoded token POST without code_verifier
//
// Per RFC 7636 §4.6, if the code_challenge_method is S256 the server MUST
// verify the code_verifier. This test verifies the server rejects requests
// without a code_verifier.
// ---------------------------------------------------------------------------

func TestOAuthEndpoints_PKCEBypassViaEmptyVerifier(t *testing.T) {
	database := dbtest.New(t)
	_, rawKey, err := database.CreateAPIKey("Test Key", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	o, _ := newTestEndpointsWithDB(database)

	reqID := performAuthorizeGet(t, o, "")
	code := performConsentApproval(t, o, reqID, "use_existing", rawKey, "")

	// Call /token with code but WITHOUT code_verifier (form-encoded).
	w := performTokenExchange(t, o, code, "")
	assert.Equal(t, http.StatusBadRequest, w.Code,
		"must reject token exchange with empty code_verifier when PKCE was S256")
	var errResp map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &errResp)
	require.NoError(t, err)
	assert.Equal(t, "invalid_grant", errResp["error"])
}

// ---------------------------------------------------------------------------
// KeyID clone: full end-to-end flow
// ---------------------------------------------------------------------------

func TestOAuthEndpoints_KeyID_Clone(t *testing.T) {
	database := dbtest.New(t)
	database.DB.SetMaxOpenConns(1)

	// Create a source key with specific limits that the clone should inherit.
	originalKey, rawKey, err := database.CreateAPIKey(
		"Source Key", nil, nil, 50, 0, 30, 0,
		[]string{"gpt-4"}, nil, nil,
	)
	require.NoError(t, err)

	o, _ := newTestEndpointsWithDB(database)

	// 1. GET /authorize
	reqID := performAuthorizeGet(t, o, "")

	// 2. POST /authorize with key_id (clone path)
	code := performConsentApproval(t, o, reqID, "use_existing", "", originalKey.ID)

	// 3. POST /token
	w := performTokenExchange(t, o, code, testVerifier)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var tokenResp map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &tokenResp)
	require.NoError(t, err)

	// The access_token must be a NEW raw key, not the original.
	accessToken, ok := tokenResp["access_token"].(string)
	require.True(t, ok)
	assert.NotEqual(t, rawKey, accessToken, "cloned key must be different from the source")
	assert.Contains(t, accessToken, "ilter_")
	assert.Equal(t, "bearer", tokenResp["token_type"])

	// Verify the cloned key inherited the source limits.
	clonedKey, err := database.GetAPIKeyByHash(accessToken)
	require.NoError(t, err)
	assert.Equal(t, 50.0, clonedKey.MonthlyBudgetUSD)
	assert.Equal(t, 30, clonedKey.RateLimitRPM)
	assert.Equal(t, []string{"gpt-4"}, clonedKey.AllowedModels)
}

// ---------------------------------------------------------------------------
// KeyID: nonexistent key_id → 400 invalid_grant
// ---------------------------------------------------------------------------

func TestOAuthEndpoints_KeyID_Nonexistent(t *testing.T) {
	database := dbtest.New(t)

	o, _ := newTestEndpointsWithDB(database)

	reqID := performAuthorizeGet(t, o, "")

	body := map[string]any{
		"request_id": reqID,
		"action":     "use_existing",
		"key_id":     "nonexistent-id",
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/authorize", bytes.NewReader(b))
	w := httptest.NewRecorder()
	o.Authorize(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var errResp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &errResp)
	require.NoError(t, err)
	assert.Equal(t, "invalid_grant", errResp["error"])
}

// ---------------------------------------------------------------------------
// KeyID: disabled key_id → 400 invalid_grant
// ---------------------------------------------------------------------------

func TestOAuthEndpoints_KeyID_Disabled(t *testing.T) {
	database := dbtest.New(t)

	// Create a key, then disable it.
	key, _, err := database.CreateAPIKey("To Disable", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	err = database.UpdateAPIKey(key.ID, auth.APIKey{Enabled: false}, false, false)
	require.NoError(t, err)

	o, _ := newTestEndpointsWithDB(database)

	reqID := performAuthorizeGet(t, o, "")

	body := map[string]any{
		"request_id": reqID,
		"action":     "use_existing",
		"key_id":     key.ID,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/authorize", bytes.NewReader(b))
	w := httptest.NewRecorder()
	o.Authorize(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var errResp map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &errResp)
	require.NoError(t, err)
	assert.Equal(t, "invalid_grant", errResp["error"])
}

// ---------------------------------------------------------------------------
// KeyID backward compat: api_key (no key_id) still works
// ---------------------------------------------------------------------------

func TestOAuthEndpoints_KeyID_BackwardCompat(t *testing.T) {
	database := dbtest.New(t)
	_, rawKey, err := database.CreateAPIKey("Test Key", nil, nil, 0, 0, 0, 0, nil, nil, nil)
	require.NoError(t, err)

	o, _ := newTestEndpointsWithDB(database)

	reqID := performAuthorizeGet(t, o, "")
	code := performConsentApproval(t, o, reqID, "use_existing", rawKey, "")

	w := performTokenExchange(t, o, code, testVerifier)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var tokenResp map[string]any
	err = json.Unmarshal(w.Body.Bytes(), &tokenResp)
	require.NoError(t, err)
	assert.Equal(t, rawKey, tokenResp["access_token"])
	assert.Equal(t, "bearer", tokenResp["token_type"])
}

// ---------------------------------------------------------------------------
// Create new: full happy path — create_new on POST /authorize
// ---------------------------------------------------------------------------

func TestOAuthEndpoints_CreateNew_HappyPath(t *testing.T) {
	database := dbtest.New(t)
	database.DB.SetMaxOpenConns(1)

	o, _ := newTestEndpointsWithDB(database)

	// 1. GET /authorize — initiate PKCE flow
	reqID := performAuthorizeGet(t, o, "")

	// 2. POST /authorize with action=create_new — consent approval
	code := performConsentApproval(t, o, reqID, "create_new", "", "")

	// 3. POST /token — exchange code for access_token
	w := performTokenExchange(t, o, code, testVerifier)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var tokenResp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &tokenResp)
	require.NoError(t, err)

	accessToken, ok := tokenResp["access_token"].(string)
	require.True(t, ok)
	assert.NotEmpty(t, accessToken)
	assert.Contains(t, accessToken, "ilter_")
	assert.Equal(t, "bearer", tokenResp["token_type"])

	// 4. Verify the key was actually persisted in DB.
	key, err := database.GetAPIKeyByHash(accessToken)
	require.NoError(t, err)
	assert.True(t, key.Enabled)
	assert.Contains(t, key.Name, "MCP OAuth -")
}
