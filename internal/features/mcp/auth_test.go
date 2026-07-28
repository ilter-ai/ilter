package mcp

import (
	"testing"

	"github.com/ilter-ai/ilter/internal/config"
)

func intPtr(i int) *int {
	return &i
}

func TestMatchToolPatternWildcard(t *testing.T) {
	if !toolMatches("*/*", "anything") {
		t.Error("*/* should match anything")
	}
	if !toolMatches("*/*", "read_file") {
		t.Error("*/* should match any tool name")
	}
	if !toolMatches("*/*", "") {
		t.Error("*/* should match empty string")
	}
}

func TestMatchToolPatternExact(t *testing.T) {
	if !toolMatches("read_file", "read_file") {
		t.Error("exact match should succeed")
	}
	if toolMatches("read_file", "write_file") {
		t.Error("different names should not match")
	}
}

func TestMatchToolPatternGlob(t *testing.T) {
	if !toolMatches("read_*", "read_file") {
		t.Error("read_* should match read_file")
	}
	if !toolMatches("read_*", "read_data") {
		t.Error("read_* should match read_data")
	}
	if toolMatches("read_*", "write_file") {
		t.Error("read_* should not match write_file")
	}
}

func TestMatchToolPatternInvalid(t *testing.T) {
	if toolMatches("[invalid", "anything") {
		t.Error("invalid pattern should return false")
	}
}

func TestAuthorizerCheckAccessNoRules(t *testing.T) {
	a := NewAuthorizer(nil, nil, "deny")
	result := a.CheckAccess("", nil, "", "", "any_tool")
	if result.Allowed {
		t.Error("expected access denied with no rules")
	}
	if result.Tool != "any_tool" {
		t.Errorf("expected tool 'any_tool', got %q", result.Tool)
	}
}

func TestAuthorizerCheckAccessEmptyRules(t *testing.T) {
	a := NewAuthorizer(nil, []config.MCPAccessRule{}, "deny")
	result := a.CheckAccess("", nil, "", "", "any_tool")
	if result.Allowed {
		t.Error("expected access denied with empty rules")
	}
}

func TestAuthorizerCheckAccessWildcardRule(t *testing.T) {
	a := NewAuthorizer(nil, []config.MCPAccessRule{
		{Tools: []string{"*/*"}},
	}, "deny")
	result := a.CheckAccess("", nil, "", "", "any_tool")
	if !result.Allowed {
		t.Error("expected allowed with wildcard rule")
	}
	if result.Tool != "any_tool" {
		t.Errorf("expected tool 'any_tool', got %q", result.Tool)
	}
	if result.MatchedRule != "*/*" {
		t.Errorf("expected matched rule '*/*', got %q", result.MatchedRule)
	}
}

func TestAuthorizerCheckAccessExactRule(t *testing.T) {
	a := NewAuthorizer(nil, []config.MCPAccessRule{
		{Tools: []string{"read_file", "write_file"}},
	}, "deny")
	if r := a.CheckAccess("", nil, "", "", "read_file"); !r.Allowed {
		t.Error("expected allowed for read_file")
	}
	if r := a.CheckAccess("", nil, "", "", "write_file"); !r.Allowed {
		t.Error("expected allowed for write_file")
	}
	if r := a.CheckAccess("", nil, "", "", "delete_file"); r.Allowed {
		t.Error("expected denied for delete_file")
	}
}

func TestAuthorizerCheckAccessGlobRule(t *testing.T) {
	a := NewAuthorizer(nil, []config.MCPAccessRule{
		{Tools: []string{"read_*"}},
	}, "deny")
	if r := a.CheckAccess("", nil, "", "", "read_file"); !r.Allowed {
		t.Error("expected allowed for read_file with read_*")
	}
	if r := a.CheckAccess("", nil, "", "", "read_data"); !r.Allowed {
		t.Error("expected allowed for read_data with read_*")
	}
	if r := a.CheckAccess("", nil, "", "", "write_file"); r.Allowed {
		t.Error("expected denied for write_file with read_*")
	}
}

func TestAuthorizerCheckAccessKeyPrefixFilter(t *testing.T) {
	a := NewAuthorizer(nil, []config.MCPAccessRule{
		{KeyPrefix: "a1b2c3d4e5f6", Tools: []string{"read_file"}},
	}, "deny")
	if r := a.CheckAccess("a1b2c3d4e5f6", nil, "", "", "read_file"); !r.Allowed {
		t.Error("expected allowed when key prefix matches")
	}
	if r := a.CheckAccess("f6e5d4c3b2a1", nil, "", "", "read_file"); r.Allowed {
		t.Error("expected denied when key prefix does not match")
	}
}

func TestAuthorizerCheckAccessKeyPrefixPartialMatch(t *testing.T) {
	a := NewAuthorizer(nil, []config.MCPAccessRule{
		{KeyPrefix: "abc", Tools: []string{"read_file"}},
	}, "deny")
	if r := a.CheckAccess("abc123def456", nil, "", "", "read_file"); !r.Allowed {
		t.Error("expected allowed when key prefix has partial match")
	}
}

func TestAuthorizerCheckAccessGroupIDFilter(t *testing.T) {
	engGroup := intPtr(1)
	a := NewAuthorizer(nil, []config.MCPAccessRule{
		{GroupID: engGroup, Tools: []string{"deploy"}},
	}, "deny")
	if r := a.CheckAccess("", []int{1}, "", "", "deploy"); !r.Allowed {
		t.Error("expected allowed when group ID matches")
	}
	if r := a.CheckAccess("", []int{2}, "", "", "deploy"); r.Allowed {
		t.Error("expected denied when group ID does not match")
	}
	if r := a.CheckAccess("", nil, "", "", "deploy"); r.Allowed {
		t.Error("expected denied when no group IDs but rule requires group")
	}
	if r := a.CheckAccess("", []int{}, "", "", "deploy"); r.Allowed {
		t.Error("expected denied when empty group IDs but rule requires group")
	}
}

func TestAuthorizerCheckAccessMultipleGroupIDs(t *testing.T) {
	devGroup := intPtr(2)
	a := NewAuthorizer(nil, []config.MCPAccessRule{
		{GroupID: devGroup, Tools: []string{"deploy"}},
	}, "deny")
	// User in multiple groups, one matches
	if r := a.CheckAccess("", []int{1, 2, 3}, "", "", "deploy"); !r.Allowed {
		t.Error("expected allowed when one of multiple group IDs matches")
	}
	// User in multiple groups, none match
	if r := a.CheckAccess("", []int{3, 4, 5}, "", "", "deploy"); r.Allowed {
		t.Error("expected denied when none of multiple group IDs match")
	}
}

func TestAuthorizerCheckAccessKeyIDFilter(t *testing.T) {
	a := NewAuthorizer(nil, []config.MCPAccessRule{
		{KeyID: "42", Tools: []string{"admin_tool"}},
	}, "deny")
	if r := a.CheckAccess("", nil, "42", "", "admin_tool"); !r.Allowed {
		t.Error("expected allowed when key ID matches")
	}
	if r := a.CheckAccess("", nil, "99", "", "admin_tool"); r.Allowed {
		t.Error("expected denied when key ID does not match")
	}
	if r := a.CheckAccess("", nil, "", "", "admin_tool"); r.Allowed {
		t.Error("expected denied when key ID is empty but rule requires key ID")
	}
}

func TestAuthorizerCheckAccessMultipleRules(t *testing.T) {
	engGroup := intPtr(1)
	a := NewAuthorizer(nil, []config.MCPAccessRule{
		{GroupID: engGroup, Tools: []string{"deploy", "rollback"}},
		{Tools: []string{"read_*"}},
	}, "deny")
	if r := a.CheckAccess("", []int{1}, "", "", "deploy"); !r.Allowed {
		t.Error("expected allowed via engineering group rule")
	}
	if r := a.CheckAccess("", nil, "", "", "read_file"); !r.Allowed {
		t.Error("expected allowed via catch-all glob rule")
	}
	if r := a.CheckAccess("", []int{2}, "", "", "deploy"); r.Allowed {
		t.Error("expected denied for deploy when other group doesn't match")
	}
	if r := a.CheckAccess("", nil, "", "", "write_file"); r.Allowed {
		t.Error("expected denied for write_file when no rule matches")
	}
}

func TestGetAuthorizedTools(t *testing.T) {
	a := NewAuthorizer(nil, []config.MCPAccessRule{
		{Tools: []string{"read_*", "write_file"}},
	}, "deny")
	all := []string{"read_file", "read_data", "write_file", "delete_file"}
	authorized := a.GetAuthorizedTools("", nil, "", all)

	expected := 3
	if len(authorized) != expected {
		t.Errorf("expected %d authorized tools, got %d: %v", expected, len(authorized), authorized)
	}

	forbidden := map[string]bool{"delete_file": true}
	for _, tName := range authorized {
		if forbidden[tName] {
			t.Errorf("delete_file should not be authorized")
		}
	}
}

func TestGetAuthorizedToolsNoRules(t *testing.T) {
	a := NewAuthorizer(nil, nil, "deny")
	authorized := a.GetAuthorizedTools("", nil, "", []string{"tool1", "tool2"})
	if len(authorized) != 0 {
		t.Errorf("expected 0 authorized tools with no rules, got %d", len(authorized))
	}
}

func TestGetAuthorizedToolsGroupFilter(t *testing.T) {
	engGroup := intPtr(1)
	a := NewAuthorizer(nil, []config.MCPAccessRule{
		{GroupID: engGroup, Tools: []string{"deploy", "rollback"}},
		{Tools: []string{"read_*"}},
	}, "deny")
	all := []string{"deploy", "rollback", "read_file", "write_file"}

	// With matching group: deploy, rollback (via group rule), read_file (via catch-all glob)
	authorized := a.GetAuthorizedTools("", []int{1}, "", all)
	if len(authorized) != 3 {
		t.Errorf("expected 3 authorized tools with group match, got %d: %v", len(authorized), authorized)
	}

	// Without matching group: only the catch-all rules
	authorized = a.GetAuthorizedTools("", nil, "", all)
	if len(authorized) != 1 {
		t.Errorf("expected 1 authorized tool without group, got %d: %v", len(authorized), authorized)
	}
	if len(authorized) > 0 && authorized[0] != "read_file" {
		t.Errorf("expected only read_file, got %v", authorized)
	}
}

func TestGetAuthorizedToolsKeyPrefixAndGroup(t *testing.T) {
	devGroup := intPtr(2)
	a := NewAuthorizer(nil, []config.MCPAccessRule{
		{KeyPrefix: "a1b2c3d4e5f6", Tools: []string{"production_tool"}},
		{GroupID: devGroup, Tools: []string{"dev_tool"}},
	}, "deny")
	all := []string{"production_tool", "dev_tool"}

	// Key prefix match, no group
	authorized := a.GetAuthorizedTools("a1b2c3d4e5f6", nil, "", all)
	if len(authorized) != 1 || authorized[0] != "production_tool" {
		t.Errorf("expected only production_tool via prefix, got %v", authorized)
	}

	// Group match, no prefix
	authorized = a.GetAuthorizedTools("", []int{2}, "", all)
	if len(authorized) != 1 || authorized[0] != "dev_tool" {
		t.Errorf("expected only dev_tool via group, got %v", authorized)
	}

	// Both match
	authorized = a.GetAuthorizedTools("a1b2c3d4e5f6", []int{2}, "", all)
	if len(authorized) != 2 {
		t.Errorf("expected 2 tools with both prefix and group match, got %d: %v", len(authorized), authorized)
	}
}
