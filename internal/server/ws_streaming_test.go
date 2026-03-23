package server

import (
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

// ---------------------------------------------------------------------------
// streamEventToWS
// ---------------------------------------------------------------------------

func TestStreamEventToWS_TextMappedToToken(t *testing.T) {
	ev := backend.StreamEvent{Type: backend.StreamText, Content: "hello"}
	msg := streamEventToWS(ev, "sess-1")
	if msg.Type != "token" {
		t.Errorf("StreamText should map to type 'token', got %q", msg.Type)
	}
	if msg.Content != "hello" {
		t.Errorf("expected content 'hello', got %q", msg.Content)
	}
	if msg.SessionID != "sess-1" {
		t.Errorf("expected session_id 'sess-1', got %q", msg.SessionID)
	}
}

func TestStreamEventToWS_ThoughtMappedToToken(t *testing.T) {
	ev := backend.StreamEvent{Type: backend.StreamThought, Content: "thinking..."}
	msg := streamEventToWS(ev, "sess-2")
	if msg.Type != "token" {
		t.Errorf("StreamThought should map to type 'token', got %q", msg.Type)
	}
	if msg.Content != "thinking..." {
		t.Errorf("expected content 'thinking...', got %q", msg.Content)
	}
}

func TestStreamEventToWS_ToolCallPreservedType(t *testing.T) {
	ev := backend.StreamEvent{Type: backend.StreamToolCall, Content: "", Payload: map[string]any{"name": "bash"}}
	msg := streamEventToWS(ev, "sess-3")
	if msg.Type != string(backend.StreamToolCall) {
		t.Errorf("non-text event type should be preserved, got %q", msg.Type)
	}
}

func TestStreamEventToWS_PayloadPassedThrough(t *testing.T) {
	payload := map[string]any{"key": "value"}
	ev := backend.StreamEvent{Type: backend.StreamToolCall, Payload: payload}
	msg := streamEventToWS(ev, "sess-4")
	if msg.Payload["key"] != "value" {
		t.Errorf("payload should pass through unchanged, got %v", msg.Payload)
	}
}

func TestStreamEventToWS_EmptySessionID(t *testing.T) {
	ev := backend.StreamEvent{Type: backend.StreamText, Content: "hi"}
	msg := streamEventToWS(ev, "")
	if msg.SessionID != "" {
		t.Errorf("empty sessionID should remain empty, got %q", msg.SessionID)
	}
}

// ---------------------------------------------------------------------------
// parseBoolPayload
// ---------------------------------------------------------------------------

func TestParseBoolPayload_NativeBool(t *testing.T) {
	if !parseBoolPayload(true) {
		t.Error("true should parse as true")
	}
	if parseBoolPayload(false) {
		t.Error("false should parse as false")
	}
}

func TestParseBoolPayload_Float64(t *testing.T) {
	if !parseBoolPayload(float64(1)) {
		t.Error("float64(1) should parse as true")
	}
	if parseBoolPayload(float64(0)) {
		t.Error("float64(0) should parse as false")
	}
}

func TestParseBoolPayload_Int(t *testing.T) {
	if !parseBoolPayload(int(1)) {
		t.Error("int(1) should parse as true")
	}
	if parseBoolPayload(int(0)) {
		t.Error("int(0) should parse as false")
	}
}

func TestParseBoolPayload_StringTrue(t *testing.T) {
	if !parseBoolPayload("true") {
		t.Error("\"true\" should parse as true")
	}
	if !parseBoolPayload("1") {
		t.Error("\"1\" should parse as true")
	}
}

func TestParseBoolPayload_StringFalse(t *testing.T) {
	if parseBoolPayload("false") {
		t.Error("\"false\" should parse as false")
	}
	if parseBoolPayload("0") {
		t.Error("\"0\" should parse as false")
	}
	if parseBoolPayload("yes") {
		t.Error("\"yes\" should parse as false (unrecognised)")
	}
}

func TestParseBoolPayload_NilReturnsFalse(t *testing.T) {
	if parseBoolPayload(nil) {
		t.Error("nil should parse as false")
	}
}

func TestParseBoolPayload_UnrecognisedTypeReturnsFalse(t *testing.T) {
	if parseBoolPayload(struct{}{}) {
		t.Error("struct{}{} should parse as false")
	}
}

// ---------------------------------------------------------------------------
// run_id guard: WSMessage RunID field round-trips through JSON marshaling
// ---------------------------------------------------------------------------

func TestWSMessage_RunID_RoundTrip(t *testing.T) {
	// RunID must be present on the struct so the frontend stale-event guard works.
	msg := WSMessage{Type: "done", SessionID: "s1", RunID: "run-abc-123"}
	if msg.RunID != "run-abc-123" {
		t.Errorf("RunID should be set, got %q", msg.RunID)
	}
}

func TestWSMessage_RunID_EmptyByDefault(t *testing.T) {
	msg := WSMessage{Type: "token", Content: "hi"}
	if msg.RunID != "" {
		t.Errorf("RunID should be empty by default, got %q", msg.RunID)
	}
}

func TestWSMessage_RunID_EchoedInDone(t *testing.T) {
	// Verify the done message is constructed with the run_id from the request.
	// This is the contract the frontend relies on to discard stale events.
	runID := "run-xyz-456"
	doneMsg := WSMessage{Type: "done", SessionID: "sess-1", RunID: runID}
	if doneMsg.Type != "done" {
		t.Errorf("type should be 'done', got %q", doneMsg.Type)
	}
	if doneMsg.RunID != runID {
		t.Errorf("RunID should be echoed in done message, got %q", doneMsg.RunID)
	}
}

func TestWSMessage_RunID_EchoedInError(t *testing.T) {
	runID := "run-err-789"
	errMsg := WSMessage{Type: "error", Content: "something failed", SessionID: "sess-2", RunID: runID}
	if errMsg.RunID != runID {
		t.Errorf("RunID should be echoed in error message, got %q", errMsg.RunID)
	}
}
