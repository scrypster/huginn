package spaces_test

import (
	"encoding/json"
	"testing"

	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/spaces"
)

// TestToolCallImageRoundTrip verifies that Image (the bounded inline
// screenshot data URI) survives the same JSON round-trip as Diff and
// ChecksStatus: session.PersistedToolCall -> tool_calls_json ->
// spaces.SpaceMessageToolCall. Without an Image field on
// SpaceMessageToolCall, screenshots captured in a space/hallway session are
// silently dropped on reload even though the single-session lane keeps them.
func TestToolCallImageRoundTrip(t *testing.T) {
	persisted := []session.PersistedToolCall{
		{
			ID:     "call_shot",
			Name:   "browser_take_screenshot",
			Result: "screenshot captured",
			Image:  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=",
		},
	}

	data, err := json.Marshal(persisted)
	if err != nil {
		t.Fatalf("Marshal PersistedToolCall: %v", err)
	}

	var space []spaces.SpaceMessageToolCall
	if err := json.Unmarshal(data, &space); err != nil {
		t.Fatalf("Unmarshal into SpaceMessageToolCall: %v", err)
	}
	if len(space) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(space))
	}
	if space[0].Image != persisted[0].Image {
		t.Errorf("Image lost in round-trip: got %q, want %q", space[0].Image, persisted[0].Image)
	}

	// The wire-facing SpaceMessage.MarshalJSON must also carry Image through.
	msg := spaces.SpaceMessage{
		ID:        "msg1",
		ToolCalls: space,
	}
	out, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal SpaceMessage: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("Unmarshal SpaceMessage wire JSON: %v", err)
	}
	toolCalls, ok := decoded["toolCalls"].([]any)
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("expected 1 toolCalls entry in wire JSON, got %v", decoded["toolCalls"])
	}
	tc, ok := toolCalls[0].(map[string]any)
	if !ok {
		t.Fatalf("toolCalls[0] is %T, want map[string]any", toolCalls[0])
	}
	if tc["image"] != persisted[0].Image {
		t.Errorf("wire JSON image: got %v, want %q", tc["image"], persisted[0].Image)
	}
}
