package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// --------------------------------------------------------------------------
// looksLikeUnfinishedPlan unit tests
// --------------------------------------------------------------------------

func TestLooksLikeUnfinishedPlan(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "repro: future-tense narration after list_dir",
			text: "I will read the contents of mathutil.go and mathutil_test.go, then fix the bug.",
			want: true,
		},
		{
			name: "let me read narration",
			text: "Let me read the file mathutil.go first.",
			want: true,
		},
		{
			name: "next I will narration",
			text: "The directory has go.mod, mathutil.go, and mathutil_test.go. Next, I will read mathutil.go to find the bug.",
			want: true,
		},
		{
			name: "completed answer with result and passes",
			text: "I fixed Add: it now returns a+b. go test passes.",
			want: false,
		},
		{
			name: "completed answer mentioning fixed and diff block",
			text: "Fixed the bug in Add.\n```go\nfunc Add(a, b int) int { return a + b }\n```\ngo test now passes.",
			want: false,
		},
		{
			name: "completed answer says done",
			text: "Done. The test suite is green now.",
			want: false,
		},
		{
			name: "plain empty text",
			text: "",
			want: false,
		},
		{
			name: "plain factual answer, no future tense",
			text: "The directory contains go.mod, mathutil.go, and mathutil_test.go.",
			want: false,
		},
		{
			name: "resolved marker beats future tense mention",
			text: "I will keep an eye on this, but the issue is already resolved.",
			want: false,
		},
		// Polite closers carry the intent phrase but no next step to
		// execute. Each false nudge costs a full extra model turn.
		{
			name: "closer: let me know if you need anything else",
			text: "Let me know if you need anything else.",
			want: false,
		},
		{
			name: "closer: I'll be here if you need me",
			text: "I'll be here if you need me.",
			want: false,
		},
		{
			name: "closer: next I will be available for questions",
			text: "Next, I will be available for questions.",
			want: false,
		},
		{
			name: "closer: I plan to keep the vault updated",
			text: "I plan to keep the vault updated as we learn more.",
			want: false,
		},
		{
			name: "closer: let me know if you'd like me to dig deeper",
			text: "Let me know if you'd like me to dig deeper.",
			want: false,
		},
		{
			name: "closer: I will be happy to help",
			text: "I will be happy to help with the next one.",
			want: false,
		},
		{
			name: "going to check is real work",
			text: "I'm going to check the test output now.",
			want: true,
		},
		{
			name: "let me go ahead and run is real work",
			text: "Let me go ahead and run the tests.",
			want: true,
		},
		{
			name: "I'll now update is real work",
			text: "I'll now update the config file.",
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := looksLikeUnfinishedPlan(tc.text)
			if got != tc.want {
				t.Errorf("looksLikeUnfinishedPlan(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

// --------------------------------------------------------------------------
// RunLoop continuation behavior
// --------------------------------------------------------------------------

// TestRunLoop_UnfinishedPlan_ContinuesAndFinishes exercises the full
// repro shape: turn 1 calls a tool, turn 2 replies with plan-only narration
// (no tool calls), turn 3 (after the nudge) calls the edit tool and finishes.
// Asserts the loop continued, the final content is the finished answer,
// exactly one nudge was injected, and the nudge text never appears in the
// history slice persisted to the hallway (result.Messages beyond what the
// caller would slice off as internal-only).
func TestRunLoop_UnfinishedPlan_ContinuesAndFinishes(t *testing.T) {
	editTool := &mockTool{name: "edit_file", result: tools.ToolResult{Output: "edited mathutil.go"}}
	listTool := &mockTool{name: "list_dir", result: tools.ToolResult{Output: "go.mod\nmathutil.go\nmathutil_test.go"}}
	reg := newRegistryWith(editTool, listTool)

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("list_dir", "call_1"), // turn 1: calls a tool
			stopResponse("I will read the contents of mathutil.go and mathutil_test.go, then fix the bug."), // turn 2: plan-only narration, no tool calls
			toolCallResponse("edit_file", "call_2"),                                                         // turn 3 (after nudge): calls the edit tool
			stopResponse("I fixed Add: it now returns a+b. go test passes."),                                // turn 4: finished answer
		},
	}

	var statusEvents []backend.StreamEvent
	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 10,
		Backend:  mb,
		Tools:    reg,
		ToolSchemas: []backend.Tool{
			{Function: backend.ToolFunction{Name: "list_dir"}},
			{Function: backend.ToolFunction{Name: "edit_file"}},
		},
		Messages: []backend.Message{{Role: "user", Content: "fix the failing test in .tmp-t5sandbox"}},
		OnEvent: func(ev backend.StreamEvent) {
			statusEvents = append(statusEvents, ev)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Fatalf("StopReason = %q, want %q", result.StopReason, "stop")
	}
	want := "I fixed Add: it now returns a+b. go test passes."
	if result.FinalContent != want {
		t.Fatalf("FinalContent = %q, want %q", result.FinalContent, want)
	}
	if editTool.callCount != 1 {
		t.Fatalf("edit_file callCount = %d, want 1 (loop must have continued past the narration turn)", editTool.callCount)
	}
	if mb.callCount != 4 {
		t.Fatalf("backend calls = %d, want 4 (list_dir, narration, nudge->edit_file, finish)", mb.callCount)
	}

	// Exactly one nudge injected: count "[system] Continue:" user messages.
	nudgeCount := 0
	for _, m := range result.Messages {
		if m.Role == "user" && strings.Contains(m.Content, "[system] Continue:") {
			nudgeCount++
		}
	}
	if nudgeCount != 1 {
		t.Fatalf("nudge count = %d, want 1", nudgeCount)
	}

	// Exactly one "continuing" status event emitted.
	continuing := 0
	for _, ev := range statusEvents {
		if ev.Type == backend.StreamStatus && ev.Content == "continuing" {
			continuing++
		}
	}
	if continuing != 1 {
		t.Fatalf("continuing status events = %d, want 1", continuing)
	}
}

// TestRunLoop_UnfinishedPlan_CapsAtMax verifies that after
// maxUnfinishedPlanContinuations nudges the loop falls through to the normal
// terminal path instead of nudging forever, returning the last narration as
// an honest partial answer.
func TestRunLoop_UnfinishedPlan_CapsAtMax(t *testing.T) {
	origMax := maxUnfinishedPlanContinuations
	maxUnfinishedPlanContinuations = 3
	defer func() { maxUnfinishedPlanContinuations = origMax }()

	listTool := &mockTool{name: "list_dir", result: tools.ToolResult{Output: "go.mod"}}
	reg := newRegistryWith(listTool)

	narration := "I will read the file next, then fix the bug."
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("list_dir", "call_1"), // turn 1: one tool call, establishes toolsRan
			stopResponse(narration),                // turn 2: narration -> nudge 1
			stopResponse(narration),                // turn 3: narration -> nudge 2
			stopResponse(narration),                // turn 4: narration -> nudge 3
			stopResponse(narration),                // turn 5: narration again -> cap reached, terminal path
		},
	}

	var statusEvents []backend.StreamEvent
	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 10,
		Backend:  mb,
		Tools:    reg,
		ToolSchemas: []backend.Tool{
			{Function: backend.ToolFunction{Name: "list_dir"}},
		},
		Messages: []backend.Message{{Role: "user", Content: "fix the failing test"}},
		OnEvent: func(ev backend.StreamEvent) {
			statusEvents = append(statusEvents, ev)
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopReason != "stop" {
		t.Fatalf("StopReason = %q, want %q", result.StopReason, "stop")
	}
	if result.FinalContent != narration {
		t.Fatalf("FinalContent = %q, want %q (honest partial answer after cap)", result.FinalContent, narration)
	}
	if mb.callCount != 5 {
		t.Fatalf("backend calls = %d, want 5 (1 tool turn + 4 narration turns, only 3 nudged)", mb.callCount)
	}

	nudgeCount := 0
	for _, m := range result.Messages {
		if m.Role == "user" && strings.Contains(m.Content, "[system] Continue:") {
			nudgeCount++
		}
	}
	if nudgeCount != 3 {
		t.Fatalf("nudge count = %d, want 3 (capped)", nudgeCount)
	}

	continuing := 0
	for _, ev := range statusEvents {
		if ev.Type == backend.StreamStatus && ev.Content == "continuing" {
			continuing++
		}
	}
	if continuing != 3 {
		t.Fatalf("continuing status events = %d, want 3", continuing)
	}
}

// TestRunLoop_UnfinishedPlan_NoToolsAvailable verifies the nudge never fires
// when no tool schemas were offered at all (nothing to call), even if the
// text superficially looks like plan narration and a tool happened to run
// (e.g. a synthetic auto-wait).
func TestRunLoop_UnfinishedPlan_NoToolsAvailable(t *testing.T) {
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			stopResponse("I will read the file next, then fix the bug."),
		},
	}
	reg := newRegistryWith()

	result, err := RunLoop(context.Background(), RunLoopConfig{
		MaxTurns: 5,
		Backend:  mb,
		Tools:    reg,
		Messages: []backend.Message{{Role: "user", Content: "fix it"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mb.callCount != 1 {
		t.Fatalf("backend calls = %d, want 1 (no nudge without tool schemas)", mb.callCount)
	}
	if result.StopReason != "stop" {
		t.Fatalf("StopReason = %q, want %q", result.StopReason, "stop")
	}
}
