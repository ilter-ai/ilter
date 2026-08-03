package protocol

import (
	"encoding/json"
	"testing"
)

func TestSupported_NewestFirst(t *testing.T) {
	if len(Supported) != 3 {
		t.Fatalf("expected 3 supported versions, got %d", len(Supported))
	}
	if Supported[0] != V20260728 {
		t.Errorf("Supported[0] = %q, want newest V20260728", Supported[0])
	}
	if Supported[len(Supported)-1] != V20241105 {
		t.Errorf("Supported[last] = %q, want oldest V20241105", Supported[len(Supported)-1])
	}
	if Newest() != V20260728 {
		t.Errorf("Newest() = %q, want V20260728", Newest())
	}
}

func TestIsSupported(t *testing.T) {
	cases := []struct {
		id   ID
		want bool
	}{
		{V20241105, true},
		{V20250326, true},
		{V20260728, true},
		{ID("2025-11-25"), false},
		{ID(""), false},
	}
	for _, c := range cases {
		if got := IsSupported(c.id); got != c.want {
			t.Errorf("IsSupported(%q) = %v, want %v", c.id, got, c.want)
		}
	}
}

// stubVersion is a minimal Version implementation used only to exercise the
// Register/Negotiate registry mechanics in isolation from any real
// version's wire-protocol behavior (those get their own tests in each
// v202XXXXX subpackage).
type stubVersion struct{ id ID }

func (s stubVersion) ID() ID { return s.id }
func (s stubVersion) HandleInitialize(json.RawMessage, ImplementationInfo) (json.RawMessage, error) {
	return nil, nil
}
func (s stubVersion) ValidateRequestMeta(json.RawMessage) error { return nil }
func (s stubVersion) IsMethodSupported(string) bool             { return true }
func (s stubVersion) WrapToolsListResult(toolsJSON json.RawMessage, _ string) (json.RawMessage, error) {
	return toolsJSON, nil
}

func (s stubVersion) WrapCallToolResult(contentJSON json.RawMessage, _ bool) (json.RawMessage, error) {
	return contentJSON, nil
}
func (s stubVersion) ErrorCode(ErrorKind) int          { return -32000 }
func (s stubVersion) Transport() TransportRequirements { return TransportRequirements{} }
func (s stubVersion) BuildClientHandshake(ImplementationInfo) (string, json.RawMessage, bool) {
	return "initialize", nil, true
}
func (s stubVersion) ParseServerHandshake(json.RawMessage) error { return nil }
func (s stubVersion) OAuthPolicy() OAuthPolicy                   { return nil }

func TestRegisterAndNegotiate(t *testing.T) {
	// Use a private registry via a throwaway ID so this test doesn't
	// collide with real version registrations that happen via the real
	// subpackages' init() functions in a full build.
	const testID ID = "test-only-version"
	Register(testID, func() Version { return stubVersion{id: testID} })

	got := Negotiate(testID)
	if got.ID() != testID {
		t.Fatalf("Negotiate(%q).ID() = %q, want %q", testID, got.ID(), testID)
	}
}

func TestNegotiate_UnknownFallsBackToNewestRegistered(t *testing.T) {
	// Register a stub under every real Supported ID so the fallback path
	// (requested version not found -> newest in Supported that IS
	// registered) is exercised deterministically regardless of whether
	// the real v20241105/v20250326/v20260728 packages happened to be
	// blank-imported by this test binary.
	for _, id := range Supported {
		func(id ID) {
			defer func() { _ = recover() }() // ignore "registered twice" if already done by another test
			Register(id, func() Version { return stubVersion{id: id} })
		}(id)
	}

	got := Negotiate(ID("totally-unknown-version"))
	if got.ID() != Newest() {
		t.Errorf("Negotiate(unknown).ID() = %q, want newest %q", got.ID(), Newest())
	}
}

func TestRegister_PanicsOnDuplicate(t *testing.T) {
	const dupID ID = "dup-test-version"
	Register(dupID, func() Version { return stubVersion{id: dupID} })

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate Register, got none")
		}
	}()
	Register(dupID, func() Version { return stubVersion{id: dupID} })
}

func TestNegotiate_PanicsWithEmptyRegistry(t *testing.T) {
	// Save and clear the registry to test the "no versions registered at
	// all" guard, then restore so other tests in this package aren't
	// affected by test ordering.
	registryMu.Lock()
	saved := registry
	registry = map[ID]func() Version{}
	registryMu.Unlock()
	defer func() {
		registryMu.Lock()
		registry = saved
		registryMu.Unlock()
	}()

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when Negotiate is called with an empty registry, got none")
		}
	}()
	Negotiate(V20250326)
}
