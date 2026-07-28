package mcptransport

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// CallbackResult holds the captured OAuth callback.
type CallbackResult struct {
	Code  string
	State string
	Error error
}

// OAuthCallbackServer is a lightweight HTTP server that captures OAuth
// authorization callbacks on a dynamically allocated port. MCP servers or
// provider flows that need OAuth can start one, expose the callback URL to
// the authorization provider, and wait for the redirect.
//
// Usage:
//
//	srv := mcp.NewOAuthCallbackServer("/oauth/callback")
//	url, _ := srv.Start()
//	// present url to the user or configure it as the redirect_uri
//	result, _ := srv.WaitForResult(ctx)
//	srv.Stop()
type OAuthCallbackServer struct {
	path     string
	server   *http.Server
	port     int
	resultCh chan CallbackResult
	mu       sync.Mutex
	started  bool
}

// NewOAuthCallbackServer creates a new server with the given callback path.
// If path is empty, "/oauth/callback" is used.
func NewOAuthCallbackServer(path string) *OAuthCallbackServer {
	if path == "" {
		path = "/oauth/callback"
	}
	return &OAuthCallbackServer{
		path:     path,
		resultCh: make(chan CallbackResult, 1),
	}
}

// Start begins listening on a random available port. Returns the full
// callback URL (e.g., "http://localhost:18223/oauth/callback").
func (s *OAuthCallbackServer) Start() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return "", errors.New("oauth callback server already started")
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("oauth callback listen: %w", err)
	}
	s.port = listener.(*net.TCPListener).Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	mux.HandleFunc(s.path, s.handleCallback)

	s.server = &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	go func() {
		if err := s.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("OAuth callback server error", "error", err)
		}
	}()

	s.started = true
	url := fmt.Sprintf("http://localhost:%d%s", s.port, s.path)
	slog.Debug("OAuth callback server started", "url", url)
	return url, nil
}

// Port returns the allocated port, or 0 if not yet started.
func (s *OAuthCallbackServer) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

func (s *OAuthCallbackServer) handleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	slog.Info(
		"OAuth callback received",
		"code_present", code != "",
		"state_present", state != "",
		"remote", r.RemoteAddr,
	)

	if code == "" {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "<html><body><h1>OAuth Error</h1><p>Missing authorization code.</p></body></html>")
		s.resultCh <- CallbackResult{Error: errors.New("missing authorization code")}
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<html><body><h1>Authorization Successful</h1>
<p>You have been authenticated. You can close this window and return to ilter.</p>
<script>window.close()</script>
</body></html>`)

	select {
	case s.resultCh <- CallbackResult{Code: code, State: state}:
	default:
	}
}

// WaitForResult blocks until a callback is received or the context is
// canceled. Returns the captured authorization code and state.
func (s *OAuthCallbackServer) WaitForResult(ctx context.Context) (*CallbackResult, error) {
	select {
	case res := <-s.resultCh:
		return &res, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Stop gracefully shuts down the server.
func (s *OAuthCallbackServer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.server.Shutdown(ctx); err != nil {
			slog.Warn("OAuth callback server shutdown error", "port", s.port, "error", err)
		}
		s.server = nil
		s.started = false
		slog.Debug("OAuth callback server stopped", "port", s.port)
	}
}
