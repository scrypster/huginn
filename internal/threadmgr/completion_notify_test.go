package threadmgr

import (
	"context"
	"testing"
)

// notifyBroadcastRecorder records all broadcasts for assertion in CompletionNotifier tests.
type notifyBroadcastRecorder struct {
	events []notifyBroadcastEvent
}

type notifyBroadcastEvent struct {
	sessionID string
	msgType   string
	payload   map[string]any
}

func (c *notifyBroadcastRecorder) fn() BroadcastFn {
	return func(sessionID, msgType string, payload map[string]any) {
		c.events = append(c.events, notifyBroadcastEvent{sessionID, msgType, payload})
	}
}

func (c *notifyBroadcastRecorder) types() []string {
	var out []string
	for _, e := range c.events {
		out = append(out, e.msgType)
	}
	return out
}

func TestCompletionNotifier(t *testing.T) {
	bc := &notifyBroadcastRecorder{}

	n := &CompletionNotifier{
		Broadcast: bc.fn(),
	}

	summary := &FinishSummary{
		Summary: "Found today's date",
		Status:  "completed",
	}

	n.Notify(context.Background(), "sess-1", "t-1", "Steve", summary)

	types := bc.types()
	if len(types) != 1 {
		t.Fatalf("expected exactly 1 broadcast (thread_result), got %d: %v", len(types), types)
	}
	if types[0] != "thread_result" {
		t.Errorf("expected thread_result broadcast, got %q", types[0])
	}

	ev := bc.events[0]
	if ev.payload["agent_id"] != "Steve" {
		t.Errorf("agent_id = %q, want \"Steve\"", ev.payload["agent_id"])
	}
	if ev.payload["status"] != "completed" {
		t.Errorf("status = %q, want \"completed\"", ev.payload["status"])
	}
	if ev.payload["summary"] != "Found today's date" {
		t.Errorf("summary = %q, want \"Found today's date\"", ev.payload["summary"])
	}
}

func TestCompletionNotifier_NilSummary(t *testing.T) {
	bc := &notifyBroadcastRecorder{}
	n := &CompletionNotifier{Broadcast: bc.fn()}

	// nil summary → no broadcast
	n.Notify(context.Background(), "sess-1", "t-1", "Steve", nil)
	if len(bc.events) != 0 {
		t.Errorf("expected no broadcasts for nil summary, got %d", len(bc.events))
	}
}

func TestCompletionNotifier_DefaultStatus(t *testing.T) {
	bc := &notifyBroadcastRecorder{}
	n := &CompletionNotifier{Broadcast: bc.fn()}

	// Empty status → defaults to "completed"
	summary := &FinishSummary{Summary: "done", Status: ""}
	n.Notify(context.Background(), "sess-1", "t-1", "Steve", summary)

	if len(bc.events) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(bc.events))
	}
	if bc.events[0].payload["status"] != "completed" {
		t.Errorf("status = %q, want \"completed\"", bc.events[0].payload["status"])
	}
}

// TestCompletionNotifier_SummaryClipped guards against broadcasting full multi-paragraph
// agent output over WS. The broadcast payload summary must be ≤200 chars regardless of
// how large the FinishSummary is.
func TestCompletionNotifier_SummaryClipped(t *testing.T) {
	bc := &notifyBroadcastRecorder{}
	n := &CompletionNotifier{Broadcast: bc.fn()}

	longSummary := string(make([]byte, 500)) // 500 zero-bytes → definitely > 200
	for i := range longSummary {
		longSummary = longSummary[:i] + "x" + longSummary[i+1:]
	}
	summary := &FinishSummary{Summary: longSummary, Status: "completed"}
	n.Notify(context.Background(), "sess-1", "t-1", "Sam", summary)

	if len(bc.events) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(bc.events))
	}
	got, _ := bc.events[0].payload["summary"].(string)
	// "…" is U+2026 = 3 UTF-8 bytes; max = 200 ASCII bytes + 3 byte ellipsis = 203 bytes.
	if len(got) > 203 {
		t.Errorf("broadcast summary byte length %d exceeds 203 — full output is leaking over WS", len(got))
	}
}

// TestCompletionNotifier_ShortSummaryUnclipped verifies short summaries are not truncated.
func TestCompletionNotifier_ShortSummaryUnclipped(t *testing.T) {
	bc := &notifyBroadcastRecorder{}
	n := &CompletionNotifier{Broadcast: bc.fn()}

	summary := &FinishSummary{Summary: "Short result.", Status: "completed"}
	n.Notify(context.Background(), "sess-1", "t-1", "Sam", summary)

	if len(bc.events) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(bc.events))
	}
	got, _ := bc.events[0].payload["summary"].(string)
	if got != "Short result." {
		t.Errorf("short summary was modified: got %q, want \"Short result.\"", got)
	}
}
