package session

import (
	"testing"
	"time"
)

// TestValidateRepaired_EmptyRole verifies that messages with empty Role are discarded.
func TestValidateRepaired_EmptyRole(t *testing.T) {
	msgs := []SessionMessage{
		{ID: "1", Role: "user", Content: "hello", Seq: 1},
		{ID: "2", Role: "", Content: "invisible", Seq: 2}, // bad: empty role
		{ID: "3", Role: "assistant", Content: "world", Seq: 3},
	}
	got := validateRepaired(msgs)
	if len(got) != 2 {
		t.Errorf("expected 2 messages, got %d: %v", len(got), got)
	}
	for _, m := range got {
		if m.Role == "" {
			t.Errorf("message with empty role slipped through: %v", m)
		}
	}
}

// TestValidateRepaired_NoContentOrTool verifies that messages with neither
// content, tool_name, nor tool_call_id are discarded (unless Type=="cost").
func TestValidateRepaired_NoContentOrTool(t *testing.T) {
	msgs := []SessionMessage{
		{ID: "1", Role: "user", Content: "hello", Seq: 1},
		{ID: "2", Role: "assistant", Content: "", ToolName: "", ToolCallID: "", Seq: 2},   // bad: no content
		{ID: "3", Role: "assistant", Content: "", ToolName: "bash", Seq: 3},               // ok: has tool_name
		{ID: "4", Role: "tool", Content: "", ToolCallID: "call-123", Seq: 4},              // ok: has tool_call_id
		{ID: "5", Role: "cost", Content: "", Type: "cost", PromptTok: 10, Seq: 5},        // ok: cost record
	}
	got := validateRepaired(msgs)
	if len(got) != 4 {
		t.Errorf("expected 4 messages, got %d", len(got))
	}
	for _, m := range got {
		if m.ID == "2" {
			t.Error("empty-content non-cost message should have been discarded")
		}
	}
}

// TestValidateRepaired_OutOfOrderSeq verifies that out-of-order sequence
// numbers are preserved (seq ordering is NOT enforced by validateRepaired
// because concurrent Append calls legitimately produce out-of-order seq values
// in the JSONL file).
func TestValidateRepaired_OutOfOrderSeq(t *testing.T) {
	msgs := []SessionMessage{
		{ID: "1", Role: "user", Content: "first", Seq: 1},
		{ID: "2", Role: "assistant", Content: "second", Seq: 5},
		{ID: "3", Role: "user", Content: "concurrent write", Seq: 3},
		{ID: "4", Role: "assistant", Content: "fourth", Seq: 6},
	}
	got := validateRepaired(msgs)
	// All 4 messages must survive — out-of-order seq is not a filter criterion.
	if len(got) != 4 {
		t.Errorf("expected 4 messages (seq order not filtered), got %d", len(got))
	}
}

// TestValidateRepaired_ZeroSeqPreserved verifies that messages with Seq==0 pass
// through validateRepaired unchanged.
func TestValidateRepaired_ZeroSeqPreserved(t *testing.T) {
	msgs := []SessionMessage{
		{ID: "1", Role: "user", Content: "a", Seq: 0},
		{ID: "2", Role: "assistant", Content: "b", Seq: 0},
		{ID: "3", Role: "user", Content: "c", Seq: 0},
	}
	got := validateRepaired(msgs)
	if len(got) != 3 {
		t.Errorf("zero-seq messages should all pass, got %d", len(got))
	}
}

// TestValidateRepaired_AllValid verifies that a clean message slice passes through unchanged.
func TestValidateRepaired_AllValid(t *testing.T) {
	msgs := []SessionMessage{
		{ID: "1", Role: "user", Content: "question", Seq: 1, Ts: time.Now()},
		{ID: "2", Role: "assistant", Content: "answer", Seq: 2, Ts: time.Now()},
		{ID: "3", Role: "tool", Content: "result", ToolCallID: "tc-1", Seq: 3},
	}
	got := validateRepaired(msgs)
	if len(got) != 3 {
		t.Errorf("expected all 3 messages to pass, got %d", len(got))
	}
}

// TestValidateRepaired_Empty verifies that an empty slice returns an empty (not nil) result.
func TestValidateRepaired_Empty(t *testing.T) {
	got := validateRepaired(nil)
	if got == nil {
		t.Error("expected non-nil result for nil input")
	}
	if len(got) != 0 {
		t.Errorf("expected 0 results for nil input, got %d", len(got))
	}
}

// TestValidateRepaired_CostRecordNoContent verifies cost records with no
// content pass validation.
func TestValidateRepaired_CostRecordNoContent(t *testing.T) {
	msgs := []SessionMessage{
		{ID: "1", Role: "cost", Type: "cost", Content: "", PromptTok: 100, CompTok: 50, Seq: 1},
	}
	got := validateRepaired(msgs)
	if len(got) != 1 {
		t.Errorf("expected cost record to pass validation, got %d", len(got))
	}
}
