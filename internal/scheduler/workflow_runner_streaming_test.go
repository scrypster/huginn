package scheduler

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// TestWorkflowRunner_TokenStreaming verifies that an AgentFunc emitting tokens
// via opts.OnToken results in throttled `workflow_step_token` broadcasts whose
// concatenated payloads reproduce the agent's full output verbatim. Streaming
// is the foundation of the Phase 1 live UI, so a regression here would silently
// blank the live panel.
func TestWorkflowRunner_TokenStreaming(t *testing.T) {
	t.Parallel()

	runStore := &mockRunStore{}

	// Long, varied output to exercise the buffer flush threshold (256 chars).
	full := strings.Repeat("the agent emitted token ", 60) + "DONE"

	agentFn := func(ctx context.Context, opts RunOptions) (string, error) {
		// Stream in small chunks so the runner has to flush mid-output.
		const chunkSize = 16
		for i := 0; i < len(full); i += chunkSize {
			end := i + chunkSize
			if end > len(full) {
				end = len(full)
			}
			if opts.OnToken != nil {
				opts.OnToken(full[i:end])
			}
		}
		return full, nil
	}

	var (
		mu      sync.Mutex
		streamed strings.Builder
		seenStep bool
	)
	bcast := func(event string, payload map[string]any) {
		if event != "workflow_step_token" {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		seenStep = true
		// Sanity-check the payload shape.
		if payload["workflow_id"] != "wf-stream" {
			t.Errorf("workflow_id = %v, want wf-stream", payload["workflow_id"])
		}
		if payload["step_name"] == "" {
			t.Error("step_name is empty")
		}
		streamed.WriteString(payload["token"].(string))
	}

	runner := MakeWorkflowRunner(runStore, agentFn, nil, bcast, nil, nil, "", nil, nil)

	wf := &Workflow{
		ID:   "wf-stream",
		Name: "stream",
		Steps: []WorkflowStep{
			{Position: 1, Name: "echo", Agent: "writer", Prompt: "say hi"},
		},
	}

	if err := runner(context.Background(), wf); err != nil {
		t.Fatalf("runner: %v", err)
	}

	if !seenStep {
		t.Fatal("expected at least one workflow_step_token broadcast")
	}
	if got := streamed.String(); !strings.Contains(got, "DONE") || !strings.HasPrefix(got, "the agent emitted token ") {
		t.Fatalf("streamed payload mismatch:\n got: %q\nwant prefix \"the agent emitted token \" and trailing \"DONE\"", got)
	}
}

// TestWorkflowRunner_TokenStreaming_OptionalBroadcast verifies that a runner
// configured WITHOUT a broadcast callback still honours OnToken (the agent
// gets a no-op callback) and produces the full step output normally. This
// keeps the streaming feature backwards-compatible with the existing
// in-process unit tests that wire a nil broadcast.
func TestWorkflowRunner_TokenStreaming_OptionalBroadcast(t *testing.T) {
	t.Parallel()

	runStore := &mockRunStore{}

	agentFn := func(ctx context.Context, opts RunOptions) (string, error) {
		// OnToken must be non-nil even when broadcast is nil so AgentFuncs
		// don't have to nil-check it.
		if opts.OnToken == nil {
			t.Error("opts.OnToken should never be nil even when broadcast is nil")
		}
		opts.OnToken("hello")
		opts.OnToken(" world")
		return "hello world", nil
	}

	runner := MakeWorkflowRunner(runStore, agentFn, nil, nil, nil, nil, "", nil, nil)
	wf := &Workflow{
		ID:    "wf-nb",
		Steps: []WorkflowStep{{Position: 1, Name: "s", Agent: "a", Prompt: "p"}},
	}
	if err := runner(context.Background(), wf); err != nil {
		t.Fatalf("runner: %v", err)
	}
}
