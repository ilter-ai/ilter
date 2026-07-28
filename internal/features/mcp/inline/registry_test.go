package inline

import (
	"context"
	"testing"
)

func TestRegisterAndLookup(t *testing.T) {
	handler := func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return "ok", nil
	}
	tools := []ToolDef{
		{Name: "test_tool", Description: "A test tool"},
	}

	err := RegisterTools("test-server", handler, tools)
	if err != nil {
		t.Fatalf("RegisterTools failed: %v", err)
	}

	h, ok := Lookup("test-server")
	if !ok {
		t.Fatal("Lookup should return true for registered server")
	}
	if h == nil {
		t.Fatal("handler should not be nil")
	}
}

func TestRegisterDuplicate(t *testing.T) {
	handler := func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return "ok", nil
	}

	err1 := RegisterTools("dup-server", handler, nil)
	if err1 != nil {
		t.Fatalf("first registration failed: %v", err1)
	}

	err2 := RegisterTools("dup-server", handler, nil)
	if err2 == nil {
		t.Fatal("expected error for duplicate registration")
	}
}

func TestLookupNonexistent(t *testing.T) {
	_, ok := Lookup("nonexistent")
	if ok {
		t.Fatal("Lookup should return false for unregistered server")
	}
}

func TestListTools(t *testing.T) {
	handler := func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return "ok", nil
	}
	tools := []ToolDef{
		{Name: "tool1", Description: "First tool"},
		{Name: "tool2", Description: "Second tool"},
	}

	err := RegisterTools("list-test", handler, tools)
	if err != nil {
		t.Fatalf("RegisterTools failed: %v", err)
	}

	listed := ListTools("list-test")
	if len(listed) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(listed))
	}
	if listed[0].Name != "tool1" || listed[1].Name != "tool2" {
		t.Errorf("unexpected tool order: %+v", listed)
	}

	nilTools := ListTools("nonexistent")
	if nilTools != nil {
		t.Errorf("expected nil for nonexistent server, got %+v", nilTools)
	}
}

func TestMultipleServers(t *testing.T) {
	handlerA := func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return "a", nil
	}
	handlerB := func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return "b", nil
	}

	_ = RegisterTools("server-a", handlerA, []ToolDef{{Name: "a1"}})
	_ = RegisterTools("server-b", handlerB, []ToolDef{{Name: "b1"}})

	hA, okA := Lookup("server-a")
	hB, okB := Lookup("server-b")

	if !okA || hA == nil {
		t.Error("failed to lookup server-a")
	}
	if !okB || hB == nil {
		t.Error("failed to lookup server-b")
	}

	resultA, _ := hA(context.Background(), nil)
	resultB, _ := hB(context.Background(), nil)

	if resultA != "a" {
		t.Errorf("expected handler-a to return 'a', got %v", resultA)
	}
	if resultB != "b" {
		t.Errorf("expected handler-b to return 'b', got %v", resultB)
	}
}

func TestListToolsNoDefs(t *testing.T) {
	handler := func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return "ok", nil
	}

	_ = RegisterTools("no-tools", handler, nil)

	tools := ListTools("no-tools")
	if tools == nil {
		t.Fatal("expected non-nil (possibly empty) slice, got nil")
	}
	if len(tools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(tools))
	}
}
