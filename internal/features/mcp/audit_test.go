package mcp

import (
	"testing"
)

func TestBoolToInt(t *testing.T) {
	if boolToInt(true) != 1 {
		t.Error("expected 1 for true")
	}
	if boolToInt(false) != 0 {
		t.Error("expected 0 for false")
	}
}

func TestNullIfEmpty(t *testing.T) {
	if nullIfEmpty("") != nil {
		t.Error("expected nil for empty string")
	}
	if v := nullIfEmpty("hello"); v == nil {
		t.Fatal("expected non-nil for non-empty string")
	} else if s, ok := v.(string); !ok {
		t.Errorf("expected string, got %T", v)
	} else if s != "hello" {
		t.Errorf("expected 'hello', got %q", s)
	}
}
