package threadmgr

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/backend"
)

// twoTurnBackend scripts a realistic delegate run: turn 1 calls a work tool,
// turn 2 calls finish with a structured summary.
type twoTurnBackend struct {
	mu    sync.Mutex
	calls int
}

func (b *twoTurnBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	b.mu.Lock()
	b.calls++
	turn := b.calls
	b.mu.Unlock()

	if req.OnToken != nil {
		req.OnToken("working…")
	}
	if turn == 1 {
		return &backend.ChatResponse{
			ToolCalls: []backend.ToolCall{{
				ID: "tc-work",
				Function: backend.ToolCallFunction{
					Name:      "read_file",
					Arguments: map[string]any{"path": "main.go"},
				},
			}},
			DoneReason: "tool_calls",
		}, nil
	}
	return &backend.ChatResponse{
		ToolCalls: []backend.ToolCall{{
			ID: "tc-finish",
			Function: backend.ToolCallFunction{
				Name: "finish",
				Arguments: map[string]any{
					"summary":        "audited main.go and fixed the wiring",
					"status":         "completed",
					"files_modified": []any{"main.go"},
				},
			},
		}},
		DoneReason: "tool_calls",
	}, nil
}
func (b *twoTurnBackend) Health(_ context.Context) error   { return nil }
func (b *twoTurnBackend) Shutdown(_ context.Context) error { return nil }
func (b *twoTurnBackend) ContextWindow() int               { return 8192 }

// TestHandoffLoop_DelegateHeartbeatWaitResult drives the full handoff path the
// lead agent experiences: spawn a delegate, observe its heartbeat while it
// runs, block on WaitForThreads, and render the result through the
// wait_for_threads tool — the delegate → heartbeat → wait → result seam.
func TestHandoffLoop_DelegateHeartbeatWaitResult(t *testing.T) {
	tm, store, sess, reg := makeSpawnFixture(t)

	// Gate-free tool executor so the scripted read_file call resolves.
	tm.SetToolExecutor(func(ctx context.Context, name string, args map[string]any) (string, error) {
		return "file contents", nil
	})

	// Capture broadcasts so we can assert heartbeat enrichment on the wire.
	var bmu sync.Mutex
	var broadcasts []capturedBroadcast
	broadcast := func(sid, msgType string, payload map[string]any) {
		bmu.Lock()
		broadcasts = append(broadcasts, capturedBroadcast{sid, msgType, payload})
		bmu.Unlock()
	}

	th, err := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "coder", Task: "audit main.go"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	tm.SpawnThread(context.Background(), th.ID, store, sess, reg, &twoTurnBackend{}, broadcast, NewCostAccumulator(0), nil)

	// The lead agent's barrier: block until the delegate finishes.
	report := tm.WaitForThreads(context.Background(), sess.ID, []string{th.ID}, 10*time.Second)
	if report.TimedOut {
		t.Fatal("delegate did not finish within the wait window")
	}
	if len(report.Completed) != 1 {
		t.Fatalf("expected 1 completed thread, got %d", len(report.Completed))
	}
	done := report.Completed[0]
	if done.Summary == nil || done.Summary.Summary != "audited main.go and fixed the wiring" {
		t.Fatalf("wait report missing full summary: %+v", done.Summary)
	}

	// Heartbeat progressed during the run and survives into the final state.
	if done.Turn < 2 {
		t.Errorf("expected at least 2 recorded turns, got %d", done.Turn)
	}
	if done.LastActivityAt.IsZero() {
		t.Error("LastActivityAt never stamped during the run")
	}
	if done.CurrentActivity == "" {
		t.Error("CurrentActivity never recorded during the run")
	}

	// The wire saw enriched thread_status events and the tool-call heartbeat.
	bmu.Lock()
	var sawActivity, sawTurn bool
	for _, b := range broadcasts {
		if b.msgType == "thread_status" {
			if a, ok := b.payload["activity"].(string); ok && strings.HasPrefix(a, "thinking (turn") {
				sawActivity = true
			}
			if _, ok := b.payload["turn"]; ok {
				sawTurn = true
			}
		}
	}
	bmu.Unlock()
	if !sawActivity || !sawTurn {
		t.Errorf("thread_status broadcasts missing heartbeat enrichment (activity=%v turn=%v)", sawActivity, sawTurn)
	}

	// Finally, the lead-facing tool renders the full structured result.
	tool := &WaitForThreadsTool{
		Fn: func(ctx context.Context, ids []string, timeout time.Duration) (WaitReport, error) {
			return tm.WaitForThreads(ctx, sess.ID, ids, timeout), nil
		},
	}
	res := tool.Execute(context.Background(), map[string]any{"thread_ids": []any{th.ID}})
	if res.IsError {
		t.Fatalf("wait_for_threads tool error: %s", res.Error)
	}
	if !strings.Contains(res.Output, "audited main.go and fixed the wiring") {
		t.Errorf("tool output missing the delegate's summary: %q", res.Output)
	}
	if !strings.Contains(res.Output, "Finished threads (1)") {
		t.Errorf("tool output missing finished section: %q", res.Output)
	}
}
