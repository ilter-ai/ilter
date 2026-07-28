package mcp

import (
	"encoding/json"
	"testing"
)

func TestNewSuccessResponse(t *testing.T) {
	id := json.RawMessage(`"test-id"`)
	result := map[string]string{"status": "ok"}

	resp := NewSuccessResponse(&id, result)
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.JSONRPC != JSONRPCVersion {
		t.Errorf("expected jsonrpc %q, got %q", JSONRPCVersion, resp.JSONRPC)
	}
	if string(*resp.ID) != `"test-id"` {
		t.Errorf("expected id %q, got %q", `"test-id"`, string(*resp.ID))
	}
	if resp.Error != nil {
		t.Errorf("expected nil error, got %+v", resp.Error)
	}
	var decoded map[string]string
	if err := json.Unmarshal(resp.Result, &decoded); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if decoded["status"] != "ok" {
		t.Errorf("expected status 'ok', got %q", decoded["status"])
	}
}

func TestNewSuccessResponseNilID(t *testing.T) {
	resp := NewSuccessResponse(nil, "result")
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.ID != nil {
		t.Errorf("expected nil ID, got %v", resp.ID)
	}
}

func TestNewErrorResponse(t *testing.T) {
	id := json.RawMessage(`42`)
	resp := NewErrorResponse(&id, ErrorCodeMethodNotFound, "method not found")

	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.JSONRPC != JSONRPCVersion {
		t.Errorf("expected jsonrpc %q, got %q", JSONRPCVersion, resp.JSONRPC)
	}
	if len(resp.Result) != 0 {
		t.Error("expected empty result for error response")
	}
	if resp.Error == nil {
		t.Fatal("expected non-nil error")
	}
	if resp.Error.Code != ErrorCodeMethodNotFound {
		t.Errorf("expected code %d, got %d", ErrorCodeMethodNotFound, resp.Error.Code)
	}
	if resp.Error.Message != "method not found" {
		t.Errorf("expected message 'method not found', got %q", resp.Error.Message)
	}
}

func TestNewErrorResponseNilID(t *testing.T) {
	resp := NewErrorResponse(nil, ErrorCodeInternal, "internal error")
	if resp.ID != nil {
		t.Errorf("expected nil ID, got %v", resp.ID)
	}
}

func TestNewErrorResponseData(t *testing.T) {
	data := map[string]string{"detail": "something broke"}
	resp := &JSONRPCResponse{
		Error: &RPCError{
			Code:    ErrorCodeInternal,
			Message: "internal error",
			Data:    data,
		},
	}

	if resp.Error.Data == nil {
		t.Fatal("expected non-nil data")
	}
	d, ok := resp.Error.Data.(map[string]string)
	if !ok {
		t.Fatalf("expected map[string]string, got %T", resp.Error.Data)
	}
	if d["detail"] != "something broke" {
		t.Errorf("expected detail 'something broke', got %q", d["detail"])
	}
}

func TestJSONRPCResponseRoundTrip(t *testing.T) {
	id := json.RawMessage(`"req-1"`)
	original := NewSuccessResponse(&id, InitializeResult{
		ProtocolVersion: "2025-03-26",
		Capabilities: ServerCapabilities{
			Tools: &ServerToolsCap{ListChanged: true},
		},
	})

	b, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded JSONRPCResponse
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.JSONRPC != JSONRPCVersion {
		t.Errorf("expected jsonrpc %q, got %q", JSONRPCVersion, decoded.JSONRPC)
	}
	if string(*decoded.ID) != `"req-1"` {
		t.Errorf("expected id %q, got %q", `"req-1"`, string(*decoded.ID))
	}
	if decoded.Error != nil {
		t.Errorf("expected nil error, got %+v", decoded.Error)
	}
}

func TestErrorCodeConstants(t *testing.T) {
	tests := []struct {
		code int
		name string
	}{
		{ErrorCodeParse, "Parse"},
		{ErrorCodeInvalidRequest, "InvalidRequest"},
		{ErrorCodeMethodNotFound, "MethodNotFound"},
		{ErrorCodeInvalidParams, "InvalidParams"},
		{ErrorCodeInternal, "Internal"},
		{ErrorCodeToolNotFound, "ToolNotFound"},
		{ErrorCodeToolExecution, "ToolExecution"},
		{ErrorCodeNotInitialized, "NotInitialized"},
	}
	for _, tc := range tests {
		if tc.code >= 0 {
			t.Errorf("%s: expected negative error code, got %d", tc.name, tc.code)
		}
	}
}
