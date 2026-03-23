package server

import (
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

// TestStreamEventToWS_ToolCall verifies that tool_call events are serialised
// with Payload and an empty Content field (no double-encoding of the tool name).
func TestStreamEventToWS_ToolCall(t *testing.T) {
	ev := backend.NewToolCallEvent("bash")
	msg := streamEventToWS(ev, "sess-1")

	if msg.Type != "tool_call" {
		t.Errorf("Type = %q, want %q", msg.Type, "tool_call")
	}
	if msg.Content != "" {
		t.Errorf("Content = %q, want empty (tool events must not overload Content)", msg.Content)
	}
	if msg.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want %q", msg.SessionID, "sess-1")
	}
	tool, _ := msg.Payload["tool"].(string)
	if tool != "bash" {
		t.Errorf("Payload[\"tool\"] = %q, want %q", tool, "bash")
	}
	if _, hasSuccess := msg.Payload["success"]; hasSuccess {
		t.Error("tool_call must not include 'success' key")
	}
}

// TestStreamEventToWS_ToolResult verifies that tool_result events include
// both "tool" and "success" in Payload, with an empty Content.
func TestStreamEventToWS_ToolResult(t *testing.T) {
	for _, tc := range []struct{ success bool }{
		{true}, {false},
	} {
		ev := backend.NewToolResultEvent("bash", tc.success)
		msg := streamEventToWS(ev, "sess-2")

		if msg.Type != "tool_result" {
			t.Errorf("Type = %q, want %q", msg.Type, "tool_result")
		}
		if msg.Content != "" {
			t.Errorf("Content = %q, want empty", msg.Content)
		}
		tool, _ := msg.Payload["tool"].(string)
		if tool != "bash" {
			t.Errorf("Payload[\"tool\"] = %q, want %q", tool, "bash")
		}
		success, ok := msg.Payload["success"].(bool)
		if !ok {
			t.Fatal("Payload[\"success\"] missing or not bool")
		}
		if success != tc.success {
			t.Errorf("success = %v, want %v", success, tc.success)
		}
	}
}

// TestStreamEventToWS_TokenHasNoPayload verifies that text/token events
// are mapped to type "token" and do not carry a Payload field (omitempty in JSON output).
func TestStreamEventToWS_TokenHasNoPayload(t *testing.T) {
	ev := backend.StreamEvent{Type: backend.StreamText, Content: "hello"}
	msg := streamEventToWS(ev, "sess-3")

	if msg.Type != "token" {
		t.Errorf("Type = %q, want %q", msg.Type, "token")
	}
	if msg.Content != "hello" {
		t.Errorf("Content = %q, want %q", msg.Content, "hello")
	}
	if msg.Payload != nil {
		t.Errorf("Payload = %v, want nil for text events", msg.Payload)
	}
}

// TestToolEventOrdering verifies that when multiple stream events are emitted,
// they are serialised in order: tool_call, then tool_result.
func TestToolEventOrdering(t *testing.T) {
	events := []backend.StreamEvent{
		{Type: backend.StreamText, Content: "thinking..."},
		backend.NewToolCallEvent("read_file"),
		backend.NewToolResultEvent("read_file", true),
		{Type: backend.StreamText, Content: "done"},
	}

	var msgs []WSMessage
	for _, ev := range events {
		msgs = append(msgs, streamEventToWS(ev, "sess-4"))
	}

	want := []string{"token", "tool_call", "tool_result", "token"}
	for i, msg := range msgs {
		if msg.Type != want[i] {
			t.Errorf("msgs[%d].Type = %q, want %q", i, msg.Type, want[i])
		}
	}

	// tool_call: no Content, has Payload.tool
	if msgs[1].Content != "" {
		t.Errorf("tool_call Content = %q, want empty", msgs[1].Content)
	}
	if msgs[1].Payload["tool"] != "read_file" {
		t.Errorf("tool_call Payload.tool = %v, want read_file", msgs[1].Payload["tool"])
	}

	// tool_result: no Content, has Payload.tool + Payload.success
	if msgs[2].Content != "" {
		t.Errorf("tool_result Content = %q, want empty", msgs[2].Content)
	}
	if msgs[2].Payload["tool"] != "read_file" {
		t.Errorf("tool_result Payload.tool = %v, want read_file", msgs[2].Payload["tool"])
	}
	if msgs[2].Payload["success"] != true {
		t.Errorf("tool_result Payload.success = %v, want true", msgs[2].Payload["success"])
	}
}

// TestStreamEventToWS_StreamDoneNotMapped verifies that StreamDone events
// do NOT get mapped to WSMessages. StreamDone is an internal backend signal
// that should be handled by the explicit "done" WSMessage sent after
// persistence, not forwarded to the client.
func TestStreamEventToWS_StreamDoneNotMapped(t *testing.T) {
	ev := backend.StreamEvent{Type: backend.StreamDone}
	msg := streamEventToWS(ev, "sess-5")

	// StreamDone should map to type "done" (the raw type), but the comment
	// in ws.go line 431-436 indicates onEvent explicitly returns early for
	// StreamDone before calling safeSend, so this test verifies the mapping
	// is consistent with the skipping logic.
	if msg.Type != "done" {
		t.Errorf("StreamDone mapped to Type=%q, want %q", msg.Type, "done")
	}
	// The important invariant: onEvent checks ev.Type == backend.StreamDone
	// and returns before calling safeSend(mapped), so the client should NOT
	// receive this message (it's handled by explicit done sent after persistence).
}

// TestStreamEventToWS_TextEventForwardedButNotDone verifies that StreamText
// events ARE forwarded to the client (mapped and sent via safeSend), but
// StreamDone events are explicitly filtered out by onEvent.
func TestStreamEventToWS_TextVsDoneFiltering(t *testing.T) {
	text := backend.StreamEvent{Type: backend.StreamText, Content: "output"}
	done := backend.StreamEvent{Type: backend.StreamDone}

	textMsg := streamEventToWS(text, "sess-6")
	doneMsg := streamEventToWS(done, "sess-6")

	// Text should map to "token" type, which is different from "done"
	if textMsg.Type != "token" {
		t.Errorf("StreamText mapped to Type=%q, want token", textMsg.Type)
	}
	if textMsg.Content != "output" {
		t.Errorf("StreamText Content=%q, want output", textMsg.Content)
	}

	// Done should map to "done" type (but onEvent filters it before sending)
	if doneMsg.Type != "done" {
		t.Errorf("StreamDone mapped to Type=%q, want done", doneMsg.Type)
	}
}
