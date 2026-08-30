package server

import (
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

// TestWSToolEventHandler_PrefetchTools_OnlySetStatus verifies that automatic
// prefetch tool calls (muninn_recall, muninn_where_left_off, muninn_guide)
// only surface a "recalling memory" status line — never a tool_call/
// tool_result chip on the wire — closing the gap where ChatWithAgent's
// onToolEvent parameter was always nil, silently dropping these events.
func TestWSToolEventHandler_PrefetchTools_OnlySetStatus(t *testing.T) {
	for _, tool := range []string{"muninn_recall", "muninn_where_left_off", "muninn_guide"} {
		t.Run(tool, func(t *testing.T) {
			var statuses []string
			var events []backend.StreamEvent
			h := newWSToolEventHandler(
				func(s string) { statuses = append(statuses, s) },
				func(ev backend.StreamEvent) { events = append(events, ev) },
			)

			h("tool_call", map[string]any{"tool": tool, "args": map[string]any{"context": "x"}, "phase": "prefetch"})
			h("tool_result", map[string]any{"tool": tool, "result": "some memory", "phase": "prefetch"})

			if len(events) != 0 {
				t.Errorf("expected no tool_call/tool_result events for prefetch tool %q, got %d: %+v", tool, len(events), events)
			}
			if len(statuses) != 1 || statuses[0] != "recalling memory" {
				t.Errorf("statuses = %v, want exactly [\"recalling memory\"]", statuses)
			}
		})
	}
}

// TestWSToolEventHandler_RegularTool_ForwardsAsStreamEventsWithMatchingID
// verifies that a non-prefetch tool call/result pair is re-expressed as
// StreamToolCall/StreamToolResult events (the same shape onEvent already
// handles) and that the tool_call and tool_result share an id, so the
// client can still correlate the chip pair despite onToolEvent's payload
// carrying no callID.
func TestWSToolEventHandler_RegularTool_ForwardsAsStreamEventsWithMatchingID(t *testing.T) {
	var events []backend.StreamEvent
	h := newWSToolEventHandler(
		func(string) {},
		func(ev backend.StreamEvent) { events = append(events, ev) },
	)

	h("tool_call", map[string]any{"id": "call_1", "tool": "bash", "args": map[string]any{"cmd": "ls"}})
	h("tool_result", map[string]any{"id": "call_1", "tool": "bash", "result": "file1.txt", "args": map[string]any{"cmd": "ls"}, "success": true})

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != backend.StreamToolCall {
		t.Errorf("events[0].Type = %v, want StreamToolCall", events[0].Type)
	}
	if events[1].Type != backend.StreamToolResult {
		t.Errorf("events[1].Type = %v, want StreamToolResult", events[1].Type)
	}
	callID, _ := events[0].Payload["id"].(string)
	resultID, _ := events[1].Payload["id"].(string)
	if callID == "" {
		t.Error("tool_call event has empty id")
	}
	if callID != resultID {
		t.Errorf("tool_call id %q != tool_result id %q", callID, resultID)
	}
	if events[1].Payload["success"] != true {
		t.Errorf("expected success=true, got %v", events[1].Payload["success"])
	}
	if events[1].Payload["result"] != "file1.txt" {
		t.Errorf("expected result=file1.txt, got %v", events[1].Payload["result"])
	}
}

// TestWSToolEventHandler_ConcurrentSameToolCalls_PairFIFO verifies that when
// the same tool name is called more than once before either resolves (e.g.
// two parallel "bash" calls), tool_call/tool_result pairs are matched in
// first-in-first-out order per tool name. This is only the FALLBACK path,
// used when a producer sends no "id"; the engine does send one (see the
// real-callID test below), which is what makes parallel calls correct.
func TestWSToolEventHandler_ConcurrentSameToolCalls_PairFIFO(t *testing.T) {
	var events []backend.StreamEvent
	h := newWSToolEventHandler(
		func(string) {},
		func(ev backend.StreamEvent) { events = append(events, ev) },
	)

	h("tool_call", map[string]any{"tool": "bash", "args": map[string]any{"cmd": "first"}})
	h("tool_call", map[string]any{"tool": "bash", "args": map[string]any{"cmd": "second"}})
	h("tool_result", map[string]any{"tool": "bash", "result": "first-done"})
	h("tool_result", map[string]any{"tool": "bash", "result": "second-done"})

	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}
	firstCallID := events[0].Payload["id"]
	secondCallID := events[1].Payload["id"]
	if firstCallID == secondCallID {
		t.Fatalf("expected distinct ids for the two concurrent calls, got same id %v", firstCallID)
	}
	if events[2].Payload["id"] != firstCallID {
		t.Errorf("first tool_result id = %v, want %v (FIFO match)", events[2].Payload["id"], firstCallID)
	}
	if events[3].Payload["id"] != secondCallID {
		t.Errorf("second tool_result id = %v, want %v (FIFO match)", events[3].Payload["id"], secondCallID)
	}
}

// TestWSToolEventHandler_PermissionDenied_ForwardsReasonAndSuccessFalse
// verifies the permission-denied path still carries reason/reason_code and
// success=false, matching what onEvent's own permission-denied handling
// expected when tools routed through it directly.
func TestWSToolEventHandler_PermissionDenied_ForwardsReasonAndSuccessFalse(t *testing.T) {
	var events []backend.StreamEvent
	h := newWSToolEventHandler(
		func(string) {},
		func(ev backend.StreamEvent) { events = append(events, ev) },
	)

	h("tool_call", map[string]any{"id": "call_9", "tool": "bash", "args": map[string]any{"cmd": "rm -rf /"}})
	h("tool_result", map[string]any{
		"id":                "call_9",
		"tool":              "bash",
		"result":            "",
		"permission_denied": true,
		"reason_code":       "denied_by_policy",
		"reason":            "destructive command blocked",
	})

	result := events[1].Payload
	if result["success"] != false {
		t.Errorf("expected success=false for permission-denied result, got %v", result["success"])
	}
	if result["permission_denied"] != true {
		t.Errorf("expected permission_denied=true, got %v", result["permission_denied"])
	}
	if result["reason_code"] != "denied_by_policy" {
		t.Errorf("expected reason_code passthrough, got %v", result["reason_code"])
	}
	if result["reason"] != "destructive command blocked" {
		t.Errorf("expected reason passthrough, got %v", result["reason"])
	}
}

// TestWSToolEventHandler_InterleavedSameToolCalls_PairByRealCallID is the
// regression test for the mis-attribution the per-tool-name FIFO fallback
// causes. Tools DO run in parallel (see agent loop dispatchTools: read_file,
// grep, list_dir, search_files, web_search and fetch_url are always eligible
// for concurrent execution), so two same-name calls can complete out of
// order. FIFO would hand call A's id to call B's result, pairing the chip
// for a.go with the contents of b.go. Pairing on the engine's real callID
// is order-independent.
func TestWSToolEventHandler_InterleavedSameToolCalls_PairByRealCallID(t *testing.T) {
	var events []backend.StreamEvent
	h := newWSToolEventHandler(
		func(string) {},
		func(ev backend.StreamEvent) { events = append(events, ev) },
	)

	h("tool_call", map[string]any{"id": "call_a", "tool": "read_file", "args": map[string]any{"file_path": "a.go"}})
	h("tool_call", map[string]any{"id": "call_b", "tool": "read_file", "args": map[string]any{"file_path": "b.go"}})
	// B completes FIRST — the interleaving FIFO pairing gets wrong.
	h("tool_result", map[string]any{"id": "call_b", "tool": "read_file", "result": "contents-of-b", "success": true})
	h("tool_result", map[string]any{"id": "call_a", "tool": "read_file", "result": "contents-of-a", "success": true})

	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}
	byID := map[string]string{}
	for _, ev := range events {
		if ev.Type != backend.StreamToolResult {
			continue
		}
		id, _ := ev.Payload["id"].(string)
		res, _ := ev.Payload["result"].(string)
		byID[id] = res
	}
	if byID["call_a"] != "contents-of-a" {
		t.Errorf("call_a result = %q, want %q (result mis-attributed)", byID["call_a"], "contents-of-a")
	}
	if byID["call_b"] != "contents-of-b" {
		t.Errorf("call_b result = %q, want %q (result mis-attributed)", byID["call_b"], "contents-of-b")
	}
}

// TestWSToolEventHandler_DeliberateMuninnRecall_NotSwallowed guards the
// distinction between the silent prefetch phase and a real in-loop call.
// muninn_recall is both: gating on the tool NAME suppressed the chip, the
// persisted tool-call record and the permission audit entry for recalls the
// agent made deliberately. Only the producer-set "phase" marker may suppress.
func TestWSToolEventHandler_DeliberateMuninnRecall_NotSwallowed(t *testing.T) {
	var statuses []string
	var events []backend.StreamEvent
	h := newWSToolEventHandler(
		func(s string) { statuses = append(statuses, s) },
		func(ev backend.StreamEvent) { events = append(events, ev) },
	)

	// No "phase" key: this is an in-loop call the agent chose to make.
	h("tool_call", map[string]any{"id": "call_1", "tool": "muninn_recall", "args": map[string]any{"context": "deploy key"}})
	h("tool_result", map[string]any{"id": "call_1", "tool": "muninn_recall", "result": "the key is in 1Password", "success": true})

	if len(events) != 2 {
		t.Fatalf("deliberate muninn_recall was swallowed: expected 2 events, got %d (%+v)", len(events), events)
	}
	if events[0].Type != backend.StreamToolCall || events[1].Type != backend.StreamToolResult {
		t.Errorf("unexpected event types: %v, %v", events[0].Type, events[1].Type)
	}
	if len(statuses) != 0 {
		t.Errorf("expected no recalling-memory status for an in-loop call, got %v", statuses)
	}
}

// TestWSToolEventHandler_ToolResultCarriesArgs verifies args survive onto the
// tool_result event. They are read back into session.PersistedToolCall.Args
// by runWSChat's onEvent and rendered by the web ToolCallModal, so dropping
// them silently empties both.
func TestWSToolEventHandler_ToolResultCarriesArgs(t *testing.T) {
	var events []backend.StreamEvent
	h := newWSToolEventHandler(
		func(string) {},
		func(ev backend.StreamEvent) { events = append(events, ev) },
	)

	args := map[string]any{"file_path": "main.go"}
	h("tool_call", map[string]any{"id": "call_1", "tool": "read_file", "args": args})
	h("tool_result", map[string]any{"id": "call_1", "tool": "read_file", "result": "package main", "args": args, "success": true})

	got, ok := events[1].Payload["args"].(map[string]any)
	if !ok {
		t.Fatalf("tool_result carries no args: %+v", events[1].Payload)
	}
	if got["file_path"] != "main.go" {
		t.Errorf("args[file_path] = %v, want main.go", got["file_path"])
	}
}

// TestWSToolEventHandler_FailedToolReportsSuccessFalse covers a tool that
// errored WITHOUT being permission-denied. Deriving success purely from the
// permission_denied flag reported every such failure as a success.
func TestWSToolEventHandler_FailedToolReportsSuccessFalse(t *testing.T) {
	var events []backend.StreamEvent
	h := newWSToolEventHandler(
		func(string) {},
		func(ev backend.StreamEvent) { events = append(events, ev) },
	)

	h("tool_call", map[string]any{"id": "call_1", "tool": "read_file", "args": map[string]any{"file_path": "nope.go"}})
	h("tool_result", map[string]any{"id": "call_1", "tool": "read_file", "result": "no such file", "success": false})

	if events[1].Payload["success"] != false {
		t.Errorf("failed (non-denied) tool reported success=%v, want false", events[1].Payload["success"])
	}
}
