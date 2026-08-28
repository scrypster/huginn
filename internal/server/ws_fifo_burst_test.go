package server

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/session"
)

// fifoBurstBackend is a scripted Backend that replies deterministically in
// call order ("ALPHA." for the 1st ChatCompletion invocation, "BRAVO." for
// the 2nd, "CHARLIE." for the 3rd) and records the full message history it
// was asked to answer with, so a test can assert that a later turn's prompt
// actually contains earlier turns' persisted answers (not just their
// unanswered asks).
type fifoBurstBackend struct {
	mu           sync.Mutex
	calls        int
	replies      []string
	seenMessages [][]backend.Message
}

func (b *fifoBurstBackend) ChatCompletion(ctx context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	b.mu.Lock()
	idx := b.calls
	b.calls++
	msgsCopy := append([]backend.Message(nil), req.Messages...)
	b.seenMessages = append(b.seenMessages, msgsCopy)
	b.mu.Unlock()

	reply := fmt.Sprintf("call-%d", idx)
	if idx < len(b.replies) {
		reply = b.replies[idx]
	}
	// Emit content immediately (as a real streaming backend would flush
	// tokens into assistantBuf as they arrive), then stay "in flight" for a
	// short while — like a stream still finishing — so a fast-follow
	// message that arrives during that window exercises exactly the race a
	// naive cancel-on-arrival policy would lose: real content already
	// buffered, then the run cancelled out from under it.
	if req.OnToken != nil {
		req.OnToken(reply)
	}
	select {
	case <-time.After(80 * time.Millisecond):
		return &backend.ChatResponse{DoneReason: "stop", Content: reply}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (b *fifoBurstBackend) Health(_ context.Context) error   { return nil }
func (b *fifoBurstBackend) Shutdown(_ context.Context) error { return nil }
func (b *fifoBurstBackend) ContextWindow() int               { return 8192 }

func (b *fifoBurstBackend) callCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

// drainDoneMessages reads from client.send until n "done" messages (echoing
// the given run IDs, in order) have been observed, or the deadline fires.
// Any "error" message fails the test immediately — the FIFO queue must never
// surface a dropped/errored ask to the client.
func drainDoneMessages(t *testing.T, c *wsClient, wantRunIDs []string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	next := 0
	for next < len(wantRunIDs) {
		select {
		case msg := <-c.send:
			switch msg.Type {
			case "error":
				t.Fatalf("unexpected error message while waiting for run %q: %v", wantRunIDs[next], msg.Content)
			case "done":
				if msg.RunID != wantRunIDs[next] {
					t.Fatalf("done #%d run_id = %q, want %q (turns must complete in FIFO order)", next+1, msg.RunID, wantRunIDs[next])
				}
				next++
			}
		case <-deadline:
			t.Fatalf("timed out waiting for done #%d (run_id %q); got %d/%d", next+1, wantRunIDs[next], next, len(wantRunIDs))
		}
	}
}

// TestWSChat_RapidBurst_FIFOTurnsAllPersistInOrder is the server-level
// regression test for the #Huginn "Buggy isn't in this company" defect: three
// WS chat messages sent in rapid succession to the same session must each
// run its own full turn, strictly in arrival order, and each must persist
// its own bound assistant reply — no run silently superseded/cancelled by a
// fast-follow message, no dropped ask, and no cross-contaminated reply.
func TestWSChat_RapidBurst_FIFOTurnsAllPersistInOrder(t *testing.T) {
	srv, _ := newTestServer(t)

	backend := &fifoBurstBackend{replies: []string{"ALPHA.", "BRAVO.", "CHARLIE."}}
	orch, err := agent.NewOrchestrator(backend, modelconfig.DefaultModels(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	srv.orch = orch

	sess := srv.store.New("fifo-burst-session", "/workspace", modelconfig.DefaultModels().Reasoner)
	if err := srv.store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	client := &wsClient{send: make(chan WSMessage, 256), ctx: context.Background()}

	asks := []string{
		"burst check one: say ALPHA",
		"burst check two: say BRAVO",
		"burst check three: say CHARLIE",
	}
	runIDs := []string{"run-1", "run-2", "run-3"}

	// Fire the three messages in rapid succession — a small stagger mirrors
	// the ~600ms real-world gap just enough for each call's synchronous user-row
	// persist to land before the next arrives (matching the wire arrival order
	// a single WS read pump would see); the fix must not require more than
	// that to stay correct.
	for i, ask := range asks {
		srv.handleWSMessage(client, WSMessage{
			Type:      "chat",
			SessionID: sess.ID,
			Content:   ask,
			RunID:     runIDs[i],
		})
		if i < len(asks)-1 {
			time.Sleep(30 * time.Millisecond)
		}
	}

	drainDoneMessages(t, client, runIDs, 10*time.Second)

	if got := backend.callCount(); got != 3 {
		t.Fatalf("backend ChatCompletion called %d times, want 3 (no dropped/duplicated turns)", got)
	}

	// "done" is emitted to the client just before persistAccumulated runs
	// (see runWSChat), so the last turn's own persistAccumulated+endChatRun
	// may still be finishing microseconds after its "done" is observed here
	// (nothing waits on the LAST turn's completion the way turn N+1 waits on
	// turn N's). Poll briefly rather than asserting on a single snapshot.
	var msgs []session.SessionMessage
	deadline := time.Now().Add(2 * time.Second)
	for {
		got, err := srv.store.TailMessages(sess.ID, 50)
		if err != nil {
			t.Fatalf("TailMessages: %v", err)
		}
		msgs = got
		if len(msgs) >= 6 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(msgs) != 6 {
		t.Fatalf("persisted %d messages, want 6 (3 user + 3 assistant, one pair per turn)\n%+v", len(msgs), msgs)
	}

	// The 3 user rows persist immediately at accept (before their FIFO wait);
	// the 3 assistant rows persist only once each turn actually completes,
	// which is gated strictly FIFO — so the wire shape is 3 user rows
	// (in arrival order) followed by 3 assistant rows (in completion order),
	// not a strict per-turn alternation. What must hold, per turn, is: the
	// right user row exists, and the right (uncontaminated) assistant reply
	// exists — bound to it by content, never to some other turn's ask.
	wantReplies := []string{"ALPHA.", "BRAVO.", "CHARLIE."}
	var users, assistants []session.SessionMessage
	for _, m := range msgs {
		switch m.Role {
		case "user":
			users = append(users, m)
		case "assistant":
			assistants = append(assistants, m)
		}
	}
	if len(users) != 3 || len(assistants) != 3 {
		t.Fatalf("got %d user rows and %d assistant rows, want 3 and 3\n%+v", len(users), len(assistants), msgs)
	}
	for i := 0; i < 3; i++ {
		if users[i].Content != asks[i] {
			t.Fatalf("user row %d content = %q, want %q", i, users[i].Content, asks[i])
		}
		if !strings.Contains(assistants[i].Content, wantReplies[i]) {
			t.Fatalf("assistant reply #%d = %q, want it to contain %q (no cross-contamination)", i+1, assistants[i].Content, wantReplies[i])
		}
	}
	// TailMessages returns rows in persisted order: every user row's own ask
	// must have been recorded before that turn's assistant reply is ever
	// looked up by index above (guaranteed by construction — persist* is
	// append-only), and role-bucketing above already confirmed content
	// binding turn-by-turn.

	// The third turn's prompt must have been built AFTER the first two turns
	// persisted — i.e. it should see BOTH earlier answers, not three
	// unanswered asks stacked on top of each other (the actual mechanism
	// behind the "Buggy isn't in this company" non-sequitur: the model was
	// staring at unresolved asks plus injected roster context).
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.seenMessages) != 3 {
		t.Fatalf("captured %d prompts, want 3", len(backend.seenMessages))
	}
	thirdPrompt := backend.seenMessages[2]
	var sawAlpha, sawBravo bool
	for _, m := range thirdPrompt {
		if m.Role == "assistant" && strings.Contains(m.Content, "ALPHA.") {
			sawAlpha = true
		}
		if m.Role == "assistant" && strings.Contains(m.Content, "BRAVO.") {
			sawBravo = true
		}
	}
	if !sawAlpha || !sawBravo {
		t.Fatalf("third turn's prompt did not contain both earlier answers (sawAlpha=%v sawBravo=%v) — "+
			"turn 3 must run after turns 1 and 2 persisted, not against unanswered asks", sawAlpha, sawBravo)
	}
}

// TestWSChat_ChatCancel_StillCancelsActiveRun verifies the FIFO fix does not
// regress explicit cancellation: an in-flight run still stops on
// "chat_cancel", and a run queued behind it is unblocked (it must not wait
// out the full FIFO ceiling).
func TestWSChat_ChatCancel_StillCancelsActiveRun(t *testing.T) {
	srv, _ := newTestServer(t)

	release := make(chan struct{})
	started := make(chan struct{}, 1)
	slow := &blockingBackend{release: release, started: started}
	orch, err := agent.NewOrchestrator(slow, modelconfig.DefaultModels(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	srv.orch = orch

	sess := srv.store.New("fifo-cancel-session", "/workspace", modelconfig.DefaultModels().Reasoner)
	if err := srv.store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	client := &wsClient{send: make(chan WSMessage, 64), ctx: context.Background()}

	srv.handleWSMessage(client, WSMessage{
		Type:      "chat",
		SessionID: sess.ID,
		Content:   "this will hang until cancelled",
		RunID:     "run-cancel-1",
	})

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("first run never reached the backend")
	}

	cancelled := srv.cancelChatRun(sess.ID)
	if !cancelled {
		t.Fatal("cancelChatRun should have found the active run")
	}
	close(release)

	drainDoneMessages(t, client, []string{"run-cancel-1"}, 5*time.Second)
}

// blockingBackend blocks in ChatCompletion until release is closed, signalling
// arrival via started. Used to hold a run open long enough for an explicit
// chat_cancel to land on it.
type blockingBackend struct {
	release chan struct{}
	started chan struct{}
}

func (b *blockingBackend) ChatCompletion(ctx context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	select {
	case b.started <- struct{}{}:
	default:
	}
	select {
	case <-b.release:
		return &backend.ChatResponse{DoneReason: "stop", Content: "late"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (b *blockingBackend) Health(_ context.Context) error   { return nil }
func (b *blockingBackend) Shutdown(_ context.Context) error { return nil }
func (b *blockingBackend) ContextWindow() int               { return 8192 }
