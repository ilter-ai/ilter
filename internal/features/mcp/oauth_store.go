package mcp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"log/slog"
	"sync"
	"time"

	"github.com/ilter-ai/ilter/internal/db"
)

// OAuthStore manages OAuth PKCE authorization requests and codes.
//
// Primary storage is SQLite (via *db.SQLiteStore). When db is nil (e.g. in
// unit tests that don't need persistence), a concurrency-safe in-memory map
// is used as a fallback.
//
// Fail-closed: if the store was never created with a valid db, all lookups
// return false/nil and no request or code is ever issued.
type OAuthStore struct {
	db         *db.SQLiteStore // nil → in-memory fallback
	requestTTL time.Duration
	codeTTL    time.Duration

	// In-memory fallback (used only when db is nil).
	mu       sync.RWMutex
	requests map[string]*AuthRequest
	codes    map[string]*AuthCode
}

// AuthRequest holds a pending OAuth authorization request (pre-user-consent).
type AuthRequest struct {
	ClientID      string
	RedirectURI   string
	CodeChallenge string
	State         string
	// ProtocolVersion is the connecting client's hinted MCP protocol
	// version (mcp_protocol_version query param on /authorize, "auto" if
	// absent) — carried through to the issued AuthCode so /token resolves
	// the same per-version OAuthPolicy the whole flow started with.
	ProtocolVersion string
	CreatedAt       time.Time
}

// AuthCode holds an issued OAuth authorization code (post-user-consent).
type AuthCode struct {
	APIKey          string // raw API key plaintext — returned at /token exchange
	RedirectURI     string
	CodeChallenge   string
	State           string
	ProtocolVersion string
	ExpiresAt       time.Time
	Used            bool
}

const (
	defaultRequestTTL = 5 * time.Minute
	defaultCodeTTL    = 120 * time.Second
)

// NewOAuthStore creates an OAuthStore. When db is non-nil the store uses
// SQLite for persistence; when nil it uses an in-memory fallback suitable
// for tests that don't exercise the persistence layer.
func NewOAuthStore(database *db.SQLiteStore) *OAuthStore {
	return &OAuthStore{
		db:         database,
		requestTTL: defaultRequestTTL,
		codeTTL:    defaultCodeTTL,
		requests:   make(map[string]*AuthRequest),
		codes:      make(map[string]*AuthCode),
	}
}

// CreateRequest stores a pending authorization request and returns a unique
// request ID. The request expires after requestTTL. protocolVersion is the
// connecting client's hinted MCP protocol version ("auto" if unspecified).
func (s *OAuthStore) CreateRequest(clientID, redirectURI, codeChallenge, state, protocolVersion string) string {
	id := generateID()
	if protocolVersion == "" {
		protocolVersion = "auto"
	}

	if s.db != nil {
		_, err := s.db.DB.Exec(
			`INSERT INTO oauth_requests (id, client_id, redirect_uri, code_challenge, state, protocol_version, created_at) VALUES (?, ?, ?, ?, ?, ?, datetime('now'))`,
			id, clientID, redirectURI, codeChallenge, state, protocolVersion,
		)
		if err != nil {
			return ""
		}
		return id
	}

	// In-memory fallback.
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests["req:"+id] = &AuthRequest{
		ClientID:        clientID,
		RedirectURI:     redirectURI,
		CodeChallenge:   codeChallenge,
		State:           state,
		ProtocolVersion: protocolVersion,
		CreatedAt:       time.Now(),
	}
	return id
}

// GetRequest retrieves a pending request. Returns nil if the request is
// unknown or expired.
func (s *OAuthStore) GetRequest(id string) *AuthRequest {
	if s.db != nil {
		var clientID, redirectURI, codeChallenge, state, protocolVersion string
		var createdAt time.Time
		err := s.db.DB.QueryRow(
			`SELECT client_id, redirect_uri, code_challenge, state, protocol_version, created_at FROM oauth_requests WHERE id = ?`, id,
		).Scan(&clientID, &redirectURI, &codeChallenge, &state, &protocolVersion, &createdAt)
		if err != nil {
			return nil
		}
		if time.Since(createdAt) > s.requestTTL {
			return nil
		}
		return &AuthRequest{
			ClientID:        clientID,
			RedirectURI:     redirectURI,
			CodeChallenge:   codeChallenge,
			State:           state,
			ProtocolVersion: protocolVersion,
			CreatedAt:       createdAt,
		}
	}

	// In-memory fallback.
	s.mu.RLock()
	defer s.mu.RUnlock()
	req, ok := s.requests["req:"+id]
	if !ok {
		return nil
	}
	if time.Since(req.CreatedAt) > s.requestTTL {
		return nil
	}
	return req
}

// GetCode returns the auth code for the given code string, or nil on miss.
func (s *OAuthStore) GetCode(code string) *AuthCode {
	if s.db != nil {
		var apiKey, redirectURI, cc, state, protocolVersion string
		var expiresAt time.Time
		var used int64
		err := s.db.DB.QueryRow(
			`SELECT api_key, redirect_uri, code_challenge, state, protocol_version, expires_at, used FROM oauth_codes WHERE id = ?`, code,
		).Scan(&apiKey, &redirectURI, &cc, &state, &protocolVersion, &expiresAt, &used)
		if err != nil {
			return nil
		}
		return &AuthCode{
			APIKey:          apiKey,
			RedirectURI:     redirectURI,
			CodeChallenge:   cc,
			State:           state,
			ProtocolVersion: protocolVersion,
			ExpiresAt:       expiresAt,
			Used:            used != 0,
		}
	}

	// In-memory fallback.
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.codes["code:"+code]
}

// SetCode stores a code directly by ID. Pass a nil code to delete.
func (s *OAuthStore) SetCode(id string, code *AuthCode) {
	if s.db != nil {
		if code == nil {
			_, _ = s.db.DB.Exec(`DELETE FROM oauth_codes WHERE id = ?`, id)
			return
		}
		var used int64
		if code.Used {
			used = 1
		}
		_, _ = s.db.DB.Exec(
			`INSERT OR REPLACE INTO oauth_codes (id, api_key, redirect_uri, code_challenge, state, protocol_version, expires_at, used) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			id, code.APIKey, code.RedirectURI, code.CodeChallenge, code.State, code.ProtocolVersion, code.ExpiresAt, used,
		)
		return
	}

	// In-memory fallback.
	s.mu.Lock()
	defer s.mu.Unlock()
	if code == nil {
		delete(s.codes, "code:"+id)
	} else {
		s.codes["code:"+id] = code
	}
}

// DeleteRequest removes a pending request.
func (s *OAuthStore) DeleteRequest(id string) {
	if s.db != nil {
		_, _ = s.db.DB.Exec(`DELETE FROM oauth_requests WHERE id = ?`, id)
		return
	}

	// In-memory fallback.
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.requests, "req:"+id)
}

// CreateCode issues an opaque authorization code bound to the given API key
// and PKCE challenge. Returns the opaque code string. protocolVersion is
// carried over from the originating AuthRequest so /token resolves the
// same per-version OAuthPolicy.
func (s *OAuthStore) CreateCode(apiKey, redirectURI, codeChallenge, state, protocolVersion string) string {
	code := generateID()
	if protocolVersion == "" {
		protocolVersion = "auto"
	}

	if s.db != nil {
		_, err := s.db.DB.Exec(
			`INSERT INTO oauth_codes (id, api_key, redirect_uri, code_challenge, state, protocol_version, expires_at, used) VALUES (?, ?, ?, ?, ?, ?, datetime('now', ?), 0)`,
			code, apiKey, redirectURI, codeChallenge, state, protocolVersion, "+120 seconds",
		)
		if err != nil {
			return ""
		}
		return code
	}

	// In-memory fallback.
	s.mu.Lock()
	defer s.mu.Unlock()
	s.codes["code:"+code] = &AuthCode{
		APIKey:          apiKey,
		RedirectURI:     redirectURI,
		CodeChallenge:   codeChallenge,
		State:           state,
		ProtocolVersion: protocolVersion,
		ExpiresAt:       time.Now().Add(s.codeTTL),
		Used:            false,
	}
	return code
}

// ExchangeCode validates and consumes an authorization code.
//
// Returns the API key and associated metadata on success.
// Returns ok=false if the code is invalid, expired, already used, or PKCE
// verification fails.
func (s *OAuthStore) ExchangeCode(code, codeVerifier string) (apiKey, redirectURI, state, protocolVersion string, ok bool) {
	if s.db != nil {
		return s.exchangeCodeDB(code, codeVerifier)
	}

	// In-memory fallback.
	s.mu.Lock()
	defer s.mu.Unlock()

	ac, found := s.codes["code:"+code]
	if !found {
		return "", "", "", "", false
	}

	// Always delete on use or failure — single-use enforcement.
	defer delete(s.codes, "code:"+code)

	if ac.Used {
		return "", "", "", "", false
	}
	if time.Now().After(ac.ExpiresAt) {
		return "", "", "", "", false
	}

	if ac.CodeChallenge != "" {
		if codeVerifier == "" {
			return "", "", "", "", false
		}
		hash := sha256.Sum256([]byte(codeVerifier))
		expected := base64.RawURLEncoding.EncodeToString(hash[:])
		if subtle.ConstantTimeCompare([]byte(expected), []byte(ac.CodeChallenge)) != 1 {
			return "", "", "", "", false
		}
	}

	ac.Used = true
	return ac.APIKey, ac.RedirectURI, ac.State, ac.ProtocolVersion, true
}

func (s *OAuthStore) exchangeCodeDB(code, codeVerifier string) (apiKey, redirectURI, state, protocolVersion string, ok bool) {
	tx, err := s.db.DB.Begin()
	if err != nil {
		return "", "", "", "", false
	}
	defer func() { _ = tx.Rollback() }()

	var apiKeyDB, redirectURIDB, cc, stateDB, protocolVersionDB string
	var expiresAt time.Time
	var used int64

	err = tx.QueryRow(
		`SELECT api_key, redirect_uri, code_challenge, state, protocol_version, expires_at, used FROM oauth_codes WHERE id = ?`, code,
	).Scan(&apiKeyDB, &redirectURIDB, &cc, &stateDB, &protocolVersionDB, &expiresAt, &used)
	if err != nil {
		return "", "", "", "", false
	}

	// Always delete on use or failure — single-use enforcement.
	if _, err := tx.Exec(`DELETE FROM oauth_codes WHERE id = ?`, code); err != nil {
		slog.Error("failed to delete oauth_code", "error", err)
	}

	if used != 0 {
		if err := tx.Commit(); err != nil {
			slog.Error("failed to commit tx (used)", "error", err)
		}
		return "", "", "", "", false
	}
	if time.Now().After(expiresAt) {
		if err := tx.Commit(); err != nil {
			slog.Error("failed to commit tx (expired)", "error", err)
		}
		return "", "", "", "", false
	}

	if cc != "" {
		if codeVerifier == "" {
			if err := tx.Commit(); err != nil {
				slog.Error("failed to commit tx (no verifier)", "error", err)
			}
			return "", "", "", "", false
		}
		hash := sha256.Sum256([]byte(codeVerifier))
		expected := base64.RawURLEncoding.EncodeToString(hash[:])
		if subtle.ConstantTimeCompare([]byte(expected), []byte(cc)) != 1 {
			if err := tx.Commit(); err != nil {
				slog.Error("failed to commit tx (code_challenge mismatch)", "error", err)
			}
			return "", "", "", "", false
		}
	}

	if err := tx.Commit(); err != nil {
		return "", "", "", "", false
	}

	return apiKeyDB, redirectURIDB, stateDB, protocolVersionDB, true
}

// StartCleanup launches a background goroutine that periodically removes
// expired entries from the SQLite tables (or in-memory maps). The goroutine
// exits when ctx is canceled. Call this as a goroutine.
func (s *OAuthStore) StartCleanup(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanup()
		}
	}
}

// cleanup removes all expired entries.
func (s *OAuthStore) cleanup() {
	if s.db != nil {
		_, _ = s.db.DB.Exec(`DELETE FROM oauth_requests WHERE created_at < datetime('now', '-' || ? || ' seconds')`, int(s.requestTTL.Seconds()))
		_, _ = s.db.DB.Exec(`DELETE FROM oauth_codes WHERE datetime('now') > expires_at`)
		return
	}

	// In-memory fallback.
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, req := range s.requests {
		if now.Sub(req.CreatedAt) > s.requestTTL {
			delete(s.requests, id)
		}
	}
	for id, code := range s.codes {
		if now.After(code.ExpiresAt) {
			delete(s.codes, id)
		}
	}
}

// generateID returns a random 32-byte hex string.
func generateID() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
