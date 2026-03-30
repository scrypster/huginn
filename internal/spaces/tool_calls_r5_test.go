package spaces_test

import (
	"encoding/json"
	"testing"

	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/spaces"
)

// TestToolCallJSONCompatibility verifies that JSON written by session.PersistedToolCall
// can be read back as spaces.SpaceMessageToolCall. Both types share the same
// tool_calls_json column in SQLite.
func TestToolCallJSONCompatibility(t *testing.T) {
	persisted := []session.PersistedToolCall{
		{
			ID:     "call_abc",
			Name:   "muninn_recall",
			Args:   map[string]any{"vault": "default", "context": "foo"},
			Result: `{"memories":[]}`,
		},
	}

	data, err := json.Marshal(persisted)
	if err != nil {
		t.Fatalf("Marshal PersistedToolCall: %v", err)
	}

	var space []spaces.SpaceMessageToolCall
	if err := json.Unmarshal(data, &space); err != nil {
		t.Fatalf("Unmarshal into SpaceMessageToolCall: %v — JSON format incompatible", err)
	}

	if len(space) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(space))
	}
	if space[0].ID != "call_abc" {
		t.Errorf("ID: got %q, want call_abc", space[0].ID)
	}
	if space[0].Name != "muninn_recall" {
		t.Errorf("Name: got %q, want muninn_recall", space[0].Name)
	}
	if space[0].Result != `{"memories":[]}` {
		t.Errorf("Result: got %q, want {\"memories\":[]}", space[0].Result)
	}
}

// TestToolCallNestedArgsRoundTrip verifies that deeply nested Args survive a
// JSON round-trip without loss or type coercion surprises.
func TestToolCallNestedArgsRoundTrip(t *testing.T) {
	original := session.PersistedToolCall{
		ID:   "call_nested",
		Name: "complex_tool",
		Args: map[string]any{
			"vault": "default",
			"filters": map[string]any{
				"limit": 10,
				"tags":  []any{"alpha", "beta"},
			},
		},
		Result: "ok",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got session.PersistedToolCall
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.ID != "call_nested" {
		t.Errorf("ID: got %q, want call_nested", got.ID)
	}

	filters, ok := got.Args["filters"].(map[string]any)
	if !ok {
		t.Fatalf("Args[\"filters\"] is %T, want map[string]any", got.Args["filters"])
	}

	tags, ok := filters["tags"].([]any)
	if !ok {
		t.Fatalf("filters[\"tags\"] is %T, want []any", filters["tags"])
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}
	if tags[0] != "alpha" || tags[1] != "beta" {
		t.Errorf("tags: got %v, want [alpha beta]", tags)
	}
}

// TestSpaceMessageToolCall_EmptyArgs verifies that a tool call with no args
// round-trips cleanly without nil-map panics or spurious "null" in JSON.
func TestSpaceMessageToolCall_EmptyArgs(t *testing.T) {
	tc := spaces.SpaceMessageToolCall{
		ID:     "call_no_args",
		Name:   "list_files",
		Result: "file1.txt",
	}

	data, err := json.Marshal(tc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// Args are omitempty — should not appear in JSON when nil.
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("Unmarshal to map: %v", err)
	}
	if _, hasArgs := m["args"]; hasArgs {
		t.Error("args key should be omitted when nil (omitempty), but it was present")
	}

	// Round-trip back.
	var got spaces.SpaceMessageToolCall
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Args != nil {
		t.Errorf("expected nil Args, got %v", got.Args)
	}
}
