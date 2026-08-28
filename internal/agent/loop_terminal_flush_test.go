package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

// Defect A: hallway CoS chat turns delivered no streamed content — the UI
// saw "status thinking" and then nothing until the bubble popped ~50s
// later with the whole answer at once (or never). The token gate
// (backend.ContentToolCallTokenGate) holds back content that looks like a
// leading tool-call JSON/XML prefix, and its terminal Finish() was never
// called by RunLoop, so a turn whose content looked like a tool-call
// candidate emitted zero tokens forever even though it became the
// authoritative answer (result.FinalContent). These tests assert that for
// every terminal path, whatever RunLoop ultimately hands back as
// FinalContent was actually delivered through cfg.OnToken.

// 1) Speech-only turn where decoding is cut because the model opened with
// '{' (speechTurnCutter.Cut()==true). The old code skipped the terminal
// emit entirely whenever the cutter had fired, even though the cutter never
// streams anything live — the sayable remainder (prose the model wrote
// after the leading tool JSON) was lost.
func TestRunLoop_SpeechOnlyCutStillFlushesFinalContent(t *testing.T) {
	var mu sync.Mutex
	var log []string
	delegate := &orderTool{name: "delegate_to_agent", result: "Delegated to @Reggie (thread th_1)", mu: &mu, log: &log}
	wait := &orderTool{name: "wait_for_threads", result: "## Finished threads (1)\nTask complete.", mu: &mu, log: &log}

	var streamed strings.Builder
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("delegate_to_agent", "call_1"),
			toolCallResponse("wait_for_threads", "call_2"),
			// Speech-only recap turn: opens with a re-typed tool call (cuts
			// decode) followed by the actual sayable answer.
			stopResponse("{\"name\": \"delegate_to_agent\", \"arguments\": {\"agent\": \"Reggie\"}}\n\n" +
				"Reggie already finished the task and confirmed the numbers match."),
		},
	}
	res, err := RunLoop(context.Background(), RunLoopConfig{
		Backend:     mb,
		Tools:       newRegistryWith(delegate, wait),
		ToolSchemas: a2aSchemas(),
		Messages:    []backend.Message{{Role: "user", Content: "ping Reggie, then 6*7"}},
		OnToken:     func(s string) { streamed.WriteString(s) },
	})
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}
	if res.StopReason != "stop" {
		t.Fatalf("stop reason = %q", res.StopReason)
	}
	if res.FinalContent == "" {
		t.Fatalf("expected non-empty final content")
	}
	if streamed.String() != res.FinalContent {
		t.Errorf("streamed content = %q, want it to equal FinalContent %q", streamed.String(), res.FinalContent)
	}
}

// 2) Main-loop terminal exit after a tool already ran this session
// (VisibleAssistantContentAfterTools applies). The model's final turn opens
// with what looks like a tool-call JSON prefix but never completes it (the
// backend stopped mid-object) — PromoteContentToolCalls correctly declines
// to promote it (incomplete JSON can't become a real tool call), so the
// turn is terminal, but the token gate also never resolves out of "holding"
// for the same incomplete-JSON reason, so it never streams anything.
func TestRunLoop_ToolsRanTerminalExitFlushesUnstreamedContent(t *testing.T) {
	var mu sync.Mutex
	var log []string
	search := &orderTool{name: "search_docs", result: "3 hits", mu: &mu, log: &log}

	const held = `{"name": "delegate_to_agent", "arguments": {"agent": "Reggie", "task": "loop forever"`

	var streamed strings.Builder
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("search_docs", "call_1"),
			{Content: held, DoneReason: "stop"},
		},
	}
	res, err := RunLoop(context.Background(), RunLoopConfig{
		Backend:     mb,
		Tools:       newRegistryWith(search),
		ToolSchemas: []backend.Tool{{Function: backend.ToolFunction{Name: "search_docs"}}},
		Messages:    []backend.Message{{Role: "user", Content: "find it"}},
		OnToken:     func(s string) { streamed.WriteString(s) },
	})
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}
	if res.StopReason != "stop" {
		t.Fatalf("stop reason = %q", res.StopReason)
	}
	if res.FinalContent == "" {
		t.Fatalf("expected non-empty final content")
	}
	if streamed.String() != res.FinalContent {
		t.Errorf("streamed content = %q, want it to equal FinalContent %q", streamed.String(), res.FinalContent)
	}
}

// 3) The plain exit: no tool ever ran this run, so the after-tools filter
// never applies (loop.go only applies it when denied||toolsRan) and the raw
// model content becomes FinalContent verbatim. The model's answer opens
// with an incomplete tool-call-shaped JSON prefix that never resolves, so
// the token gate holds it back for the entire turn.
func TestRunLoop_PlainExitFlushesUnstreamedContent(t *testing.T) {
	const held = `{"name": "wait_for_threads", "arguments": {`

	var streamed strings.Builder
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{Content: held, DoneReason: "stop"},
		},
	}
	res, err := RunLoop(context.Background(), RunLoopConfig{
		Backend:     mb,
		ToolSchemas: a2aSchemas(),
		Messages:    []backend.Message{{Role: "user", Content: "hi"}},
		OnToken:     func(s string) { streamed.WriteString(s) },
	})
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}
	if res.StopReason != "stop" {
		t.Fatalf("stop reason = %q", res.StopReason)
	}
	if res.FinalContent == "" {
		t.Fatalf("expected non-empty final content")
	}
	if streamed.String() != res.FinalContent {
		t.Errorf("streamed content = %q, want it to equal FinalContent %q", streamed.String(), res.FinalContent)
	}
}
