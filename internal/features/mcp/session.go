package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// Session represents a single SSE-based MCP client session.
type Session struct {
	// ID is the session identifier used in POST ?sessionId=… query parameters.
	ID string

	// KeyID is the authenticated key ID ("" for admin / no-key).
	KeyID     string
	KeyPrefix string

	// ClientIP is the IP address of the connected client, extracted from the HTTP request.
	ClientIP string

	// Client info gathered during the initialize handshake.
	ClientName    string
	ClientVersion string

	// Initialized becomes true after a successful initialize request.
	Initialized bool

	CreatedAt time.Time

	// NotifyCh receives server-initiated events (for future use with notifications).
	// The channel is created with a small buffer to avoid blocking.
	NotifyCh chan any
}

// SessionManager provides concurrency-safe CRUD for SSE sessions.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
	}
}

func (sm *SessionManager) Create(keyID string, keyPrefix string) *Session {
	session := &Session{
		ID:        generateSessionID(),
		KeyID:     keyID,
		KeyPrefix: keyPrefix,
		CreatedAt: time.Now(),
		NotifyCh:  make(chan any, 64),
	}
	sm.mu.Lock()
	sm.sessions[session.ID] = session
	sm.mu.Unlock()
	return session
}

func (sm *SessionManager) Get(id string) *Session {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.sessions[id]
}

func (sm *SessionManager) Delete(id string) {
	sm.mu.Lock()
	session, ok := sm.sessions[id]
	if ok {
		delete(sm.sessions, id)
		close(session.NotifyCh)
	}
	sm.mu.Unlock()
}

func (sm *SessionManager) Count() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.sessions)
}

func generateSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand never fails on Linux/macOS. Remove fallback if targeting constrained env.
		return hex.EncodeToString([]byte(time.Now().String()))
	}
	return hex.EncodeToString(b)
}
