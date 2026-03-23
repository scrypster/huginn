package threadmgr

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/session"
)

// hangingHelpResolver never returns from Resolve — it blocks until its context
// is cancelled. This simulates a network partition or a dead LLM backend.
type hangingHelpResolver struct {
	resolveCalled chan struct{}
}

func (h *hangingHelpResolver) Resolve(ctx context.Context, _, _, _, _ string) (string, error) {
	// Signal that Resolve was entered.
	select {
	case h.resolveCalled <- struct{}{}:
	default:
	}
	// Block until the context supplied by threadmgr fires.
	<-ctx.Done()
	return "", ctx.Err()
}

// TestHelpResolver_TimeoutCancelsThread verifies that when a HelpResolver hangs
// past HelpResolveTimeout, the blocked thread is cancelled and does not leak.
//
// Because HelpResolveTimeout is 30 minutes (unsuitable for a fast unit test),
// we substitute a tiny timeout by temporarily patching the constant via a
// custom resolver that we wrap with a very short context.WithTimeout. The
// real guarantee we exercise here is: after the resolver's context expires,
// tm.Cancel(threadID) is called, moving the thread to StatusCancelled.
func TestHelpResolver_TimeoutCancelsThread(t *testing.T) {
	t.Parallel()

	// We drive the timeout through a very short-lived context passed into
	// SpawnThread, not through the full HelpResolveTimeout, so the test
	// completes well within 30 seconds.

	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("s1", "/tmp", "claude-haiku-4")

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:    "HelpBot",
		ModelID: "claude-haiku-4",
	})

	// Backend returns request_help() on the first call, triggering ErrHelp.
	helpFb := &fakeBackend{
		response: &backend.ChatResponse{
			ToolCalls: []backend.ToolCall{
				{
					ID: "tc-help",
					Function: backend.ToolCallFunction{
						Name:      "request_help",
						Arguments: map[string]any{"message": "need help"},
					},
				},
			},
			DoneReason: "tool_calls",
		},
	}

	thread, err := tm.Create(CreateParams{
		SessionID: sess.ID,
		AgentID:   "HelpBot",
		Task:      "test help timeout",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	resolveCalled := make(chan struct{}, 1)
	resolver := &hangingHelpResolver{resolveCalled: resolveCalled}
	tm.SetHelpResolver(resolver)

	broadcastFn := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	// Use a parent context with a short timeout so that the help resolver's
	// context (derived from it) also expires quickly — exercising the
	// "resolver timed out → cancel thread" path without waiting 30 minutes.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	tm.SpawnThread(ctx, thread.ID, store, sess, reg, helpFb, broadcastFn, ca, nil)

	// Wait for the resolver to be entered (confirms the ErrHelp path fired).
	select {
	case <-resolveCalled:
		// Good — resolver was invoked.
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: resolver was never called; ErrHelp path not reached")
	}

	// After the parent context times out (200ms), the resolver's context also
	// cancels, which causes the hangingHelpResolver to return an error. The
	// threadmgr goroutine should then call tm.Cancel(threadID).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, ok := tm.Get(thread.ID)
		if ok && got.Status == StatusCancelled {
			return // success
		}
		time.Sleep(20 * time.Millisecond)
	}

	got, _ := tm.Get(thread.ID)
	status := "<not found>"
	if got != nil {
		status = string(got.Status)
	}
	t.Errorf("expected thread StatusCancelled after resolver timeout, got %s", status)
}
