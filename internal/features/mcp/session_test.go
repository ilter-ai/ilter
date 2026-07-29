package mcp

import (
	"testing"

	"github.com/ilter-ai/ilter/internal/features/mcp/protocol"
)

// TestProtocolVersionsRegistered guards protocol_register.go's blank
// imports: if one of the three version subpackages is ever accidentally
// removed from that file, protocol.Negotiate would panic deep inside
// Gateway/Hub dispatch instead of failing a test. This confirms all three
// are actually registered and reachable from package mcp.
func TestProtocolVersionsRegistered(t *testing.T) {
	for _, id := range protocol.Supported {
		v := protocol.Negotiate(id)
		if v.ID() != id {
			t.Errorf("protocol.Negotiate(%q).ID() = %q, want %q — is protocol_register.go missing a blank import?", id, v.ID(), id)
		}
	}
}

func TestSession_ProtocolVersionDefaultsEmpty(t *testing.T) {
	sm := NewSessionManager()
	s := sm.Create("key1", "prefix1")
	defer sm.Delete(s.ID)

	if s.ProtocolVersion != "" {
		t.Errorf("new session ProtocolVersion = %q, want empty until initialize pins it", s.ProtocolVersion)
	}
}

func TestSession_ProtocolVersionPinning(t *testing.T) {
	sm := NewSessionManager()
	s := sm.Create("key1", "prefix1")
	defer sm.Delete(s.ID)

	s.ProtocolVersion = protocol.V20241105
	got := sm.Get(s.ID)
	if got.ProtocolVersion != protocol.V20241105 {
		t.Errorf("ProtocolVersion = %q, want %q", got.ProtocolVersion, protocol.V20241105)
	}
}
