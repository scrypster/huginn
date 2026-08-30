package server

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/config"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/session"
)

// ---------------------------------------------------------------------------
// replayRing
// ---------------------------------------------------------------------------

func TestReplayRing_SinceReturnsOrderedTail(t *testing.T) {
	r := newReplayRing(8)
	for i := uint64(1); i <= 5; i++ {
		r.add(WSMessage{Type: "token", Seq: i})
	}
	got := r.since(2)
	if len(got) != 3 {
		t.Fatalf("since(2) returned %d messages, want 3", len(got))
	}
	for i, m := range got {
		if want := uint64(3 + i); m.Seq != want {
			t.Errorf("got[%d].Seq = %d, want %d", i, m.Seq, want)
		}
	}
	if r.oldestSeq() != 1 {
		t.Errorf("oldestSeq = %d, want 1", r.oldestSeq())
	}
}

func TestReplayRing_WrapsAndEvictsOldest(t *testing.T) {
	r := newReplayRing(4)
	for i := uint64(1); i <= 10; i++ {
		r.add(WSMessage{Type: "token", Seq: i})
	}
	if r.oldestSeq() != 7 {
		t.Errorf("oldestSeq = %d, want 7 (capacity 4, last seq 10)", r.oldestSeq())
	}
	got := r.since(0)
	if len(got) != 4 {
		t.Fatalf("since(0) returned %d messages, want 4", len(got))
	}
	for i, m := range got {
		if want := uint64(7 + i); m.Seq != want {
			t.Errorf("got[%d].Seq = %d, want %d (must be oldest-first)", i, m.Seq, want)
		}
	}
}

func TestReplayRing_EmptyOldestSeqIsZero(t *testing.T) {
	r := newReplayRing(4)
	if r.oldestSeq() != 0 {
		t.Errorf("empty ring oldestSeq = %d, want 0", r.oldestSeq())
	}
	if got := r.since(0); len(got) != 0 {
		t.Errorf("empty ring since(0) returned %d messages, want 0", len(got))
	}
}

// ---------------------------------------------------------------------------
// WSHub replay buffer + replaySince
// ---------------------------------------------------------------------------

// TestWSHub_ReplaySince_RecoversDroppedEvents simulates the core reliability
// scenario: events are broadcast to a session while no client is connected
// (i.e. all deliveries are dropped), then a client resumes from its last seen
// seq and recovers everything it missed.
func TestWSHub_ReplaySince_RecoversDroppedEvents(t *testing.T) {
	hub := newWSHub()
	const sessionID = "sess-replay"

	// No clients registered — every broadcast is effectively dropped, but each
	// must still be recorded in the replay buffer.
	for i := 1; i <= 5; i++ {
		hub.broadcastToSession(sessionID, WSMessage{Type: "token", Content: fmt.Sprintf("t%d", i)})
	}

	msgs, currentSeq, gap := hub.replaySince(sessionID, 2)
	if currentSeq != 5 {
		t.Errorf("currentSeq = %d, want 5", currentSeq)
	}
	if gap {
		t.Error("gap = true, want false: buffer covers everything after seq 2")
	}
	if len(msgs) != 3 {
		t.Fatalf("replayed %d messages, want 3", len(msgs))
	}
	for i, m := range msgs {
		if want := uint64(3 + i); m.Seq != want {
			t.Errorf("msgs[%d].Seq = %d, want %d", i, m.Seq, want)
		}
		if m.SessionID != sessionID {
			t.Errorf("msgs[%d].SessionID = %q, want %q", i, m.SessionID, sessionID)
		}
		if m.Epoch != serverEpoch {
			t.Errorf("msgs[%d].Epoch = %d, want %d", i, m.Epoch, serverEpoch)
		}
	}
}

func TestWSHub_ReplaySince_UpToDateClient(t *testing.T) {
	hub := newWSHub()
	hub.broadcastToSession("s1", WSMessage{Type: "token"})
	hub.broadcastToSession("s1", WSMessage{Type: "done"})

	msgs, currentSeq, gap := hub.replaySince("s1", 2)
	if len(msgs) != 0 || gap || currentSeq != 2 {
		t.Errorf("up-to-date client: msgs=%d gap=%v seq=%d, want 0/false/2", len(msgs), gap, currentSeq)
	}
}

func TestWSHub_ReplaySince_GapWhenBufferEvicted(t *testing.T) {
	hub := newWSHub()
	const sessionID = "sess-evict"
	// Overflow the 512-entry ring so the oldest entries are evicted.
	for i := 0; i < wsReplayBufferSize+10; i++ {
		hub.broadcastToSession(sessionID, WSMessage{Type: "token"})
	}
	msgs, currentSeq, gap := hub.replaySince(sessionID, 1)
	if !gap {
		t.Error("gap = false, want true: seq 2..10 were evicted from the ring")
	}
	if len(msgs) != wsReplayBufferSize {
		t.Errorf("replayed %d messages, want %d (full ring)", len(msgs), wsReplayBufferSize)
	}
	if currentSeq != uint64(wsReplayBufferSize+10) {
		t.Errorf("currentSeq = %d, want %d", currentSeq, wsReplayBufferSize+10)
	}
}

func TestWSHub_ReplaySince_ClientAheadMeansServerRestart(t *testing.T) {
	hub := newWSHub()
	hub.broadcastToSession("s1", WSMessage{Type: "token"})
	msgs, currentSeq, gap := hub.replaySince("s1", 99)
	if !gap {
		t.Error("gap = false, want true when client's last_seq is ahead of the server (restart)")
	}
	if len(msgs) != 0 || currentSeq != 1 {
		t.Errorf("msgs=%d seq=%d, want 0/1", len(msgs), currentSeq)
	}
}

func TestWSHub_DeleteSessionSeq_EvictsReplayBuffer(t *testing.T) {
	hub := newWSHub()
	hub.broadcastToSession("s1", WSMessage{Type: "token"})
	hub.DeleteSessionSeq("s1")

	hub.seqMu.Lock()
	_, seqExists := hub.sessionSeq["s1"]
	_, replayExists := hub.sessionReplay["s1"]
	hub.seqMu.Unlock()
	if seqExists || replayExists {
		t.Errorf("DeleteSessionSeq left state behind: seq=%v replay=%v", seqExists, replayExists)
	}
	// A resume after deletion behaves like an unknown session.
	msgs, currentSeq, gap := hub.replaySince("s1", 0)
	if len(msgs) != 0 || currentSeq != 0 || gap {
		t.Errorf("post-delete replaySince: msgs=%d seq=%d gap=%v, want 0/0/false", len(msgs), currentSeq, gap)
	}
}

// TestWSHub_Broadcast_DropStillRecordedForReplay verifies that when the global
// broadcast channel is full (the default: drop branch), session-scoped
// messages are still stamped and recorded so a resume can recover them.
func TestWSHub_Broadcast_DropStillRecordedForReplay(t *testing.T) {
	hub := newWSHub() // run() not started, so broadcastC fills up
	// Fill the 256-deep channel.
	for i := 0; i < 256; i++ {
		hub.broadcast(WSMessage{Type: "filler"})
	}
	// This one is dropped from the live channel but must land in the replay buffer.
	hub.broadcast(WSMessage{Type: "thread_done", SessionID: "sess-drop"})

	msgs, currentSeq, gap := hub.replaySince("sess-drop", 0)
	if len(msgs) != 1 {
		t.Fatalf("replayed %d messages, want 1 (the dropped broadcast)", len(msgs))
	}
	if msgs[0].Type != "thread_done" || msgs[0].Seq != 1 {
		t.Errorf("recovered message = %+v, want type=thread_done seq=1", msgs[0])
	}
	if gap || currentSeq != 1 {
		t.Errorf("gap=%v seq=%d, want false/1", gap, currentSeq)
	}
}

// ---------------------------------------------------------------------------
// resume WS message (drop + resume end-to-end at the handler level)
// ---------------------------------------------------------------------------

func TestHandleWSMessage_Resume_ReplaysMissedEventsInOrder(t *testing.T) {
	hub := newWSHub()
	srv := &Server{wsHub: hub}
	const sessionID = "sess-resume"

	// Simulate a streaming run that happened while the client was disconnected.
	for i := 1; i <= 4; i++ {
		hub.broadcastToSession(sessionID, WSMessage{Type: "token", Content: fmt.Sprintf("t%d", i)})
	}
	hub.broadcastToSession(sessionID, WSMessage{Type: "done"})

	// Reconnected client resumes from seq 2.
	c := &wsClient{send: make(chan WSMessage, 16), ctx: context.Background()}
	srv.handleWSMessage(c, WSMessage{
		Type:      "resume",
		SessionID: sessionID,
		Payload:   map[string]any{"last_seq": float64(2), "epoch": float64(serverEpoch)},
	})

	var got []WSMessage
	for {
		select {
		case m := <-c.send:
			got = append(got, m)
			if m.Type == "resume_ok" {
				goto drained
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for resume_ok")
		}
	}
drained:
	if len(got) != 4 { // seq 3,4,5 + resume_ok
		t.Fatalf("received %d messages, want 4 (3 replayed + resume_ok): %+v", len(got), got)
	}
	for i, want := range []uint64{3, 4, 5} {
		if got[i].Seq != want {
			t.Errorf("replayed[%d].Seq = %d, want %d", i, got[i].Seq, want)
		}
	}
	ok := got[3]
	if ok.SessionID != sessionID {
		t.Errorf("resume_ok.SessionID = %q, want %q", ok.SessionID, sessionID)
	}
	if gap := parseBoolPayload(ok.Payload["gap"]); gap {
		t.Error("resume_ok.gap = true, want false")
	}
	if seq, _ := ok.Payload["seq"].(uint64); seq != 5 {
		t.Errorf("resume_ok.seq = %v, want 5", ok.Payload["seq"])
	}
}

func TestHandleWSMessage_Resume_GapSignalsHistoryRefetch(t *testing.T) {
	hub := newWSHub()
	srv := &Server{wsHub: hub}

	// Unknown session (or evicted buffer) with activity the client claims to
	// have partially seen → gap so the client re-fetches via REST.
	hub.broadcastToSession("sess-gap", WSMessage{Type: "done"})
	hub.DeleteSessionSeq("sess-gap")
	hub.broadcastToSession("sess-gap", WSMessage{Type: "done"})
	hub.broadcastToSession("sess-gap", WSMessage{Type: "done"})

	c := &wsClient{send: make(chan WSMessage, 16), ctx: context.Background()}
	// Client epoch differs from the server's → restart → always a gap.
	srv.handleWSMessage(c, WSMessage{
		Type:    "resume",
		Payload: map[string]any{"session_id": "sess-gap", "last_seq": float64(1), "epoch": float64(serverEpoch + 1)},
	})

	select {
	case m := <-c.send:
		if m.Type != "resume_ok" {
			t.Fatalf("expected resume_ok (no replay on epoch mismatch), got %q", m.Type)
		}
		if !parseBoolPayload(m.Payload["gap"]) {
			t.Error("resume_ok.gap = false, want true on epoch mismatch")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for resume_ok")
	}
}

func TestHandleWSMessage_Resume_NoSessionIDIsIgnored(t *testing.T) {
	srv := &Server{wsHub: newWSHub()}
	c := &wsClient{send: make(chan WSMessage, 4), ctx: context.Background()}
	srv.handleWSMessage(c, WSMessage{Type: "resume"})
	select {
	case m := <-c.send:
		t.Fatalf("expected no reply for resume without session_id, got %q", m.Type)
	case <-time.After(100 * time.Millisecond):
	}
}

// ---------------------------------------------------------------------------
// connection-independent chat runs
// ---------------------------------------------------------------------------

// gateBackend streams one token, then blocks until released (or its context is
// cancelled), then streams a final token. Lets tests unregister clients while
// the run is provably in flight.
type gateBackend struct {
	startedOnce sync.Once
	started     chan struct{}
	release     chan struct{}
}

func (b *gateBackend) ChatCompletion(ctx context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	if req.OnToken != nil {
		req.OnToken("hello ")
	}
	b.startedOnce.Do(func() { close(b.started) })
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.release:
	}
	if req.OnToken != nil {
		req.OnToken("world")
	}
	return &backend.ChatResponse{Content: "hello world", DoneReason: "stop"}, nil
}
func (b *gateBackend) Health(_ context.Context) error   { return nil }
func (b *gateBackend) Shutdown(_ context.Context) error { return nil }
func (b *gateBackend) ContextWindow() int               { return 4096 }

// TestWSChat_RunSurvivesClientUnregister verifies the headline reliability
// fix: the chat run keeps streaming and persists its result even when the
// originating client is unregistered (tab close / reconnect) mid-run, and a
// client that connects mid-run still receives the streamed events because
// they are broadcast to the session rather than sent only to the originator.
func TestWSChat_RunSurvivesClientUnregister(t *testing.T) {
	gb := &gateBackend{started: make(chan struct{}), release: make(chan struct{})}
	orch, err := agent.NewOrchestrator(gb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	store := session.NewStore(t.TempDir())
	srv := New(*config.Default(), orch, store, testToken, t.TempDir(), nil, nil, nil)
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return agents.DefaultAgentsConfig(), nil
	}

	sess := store.New("survive-test", "/workspace", "test-model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	// Originating client, registered with the hub like a real connection.
	ctx1, cancel1 := context.WithCancel(context.Background())
	c1 := &wsClient{send: make(chan WSMessage, 64), ctx: ctx1, cancel: cancel1}
	srv.wsHub.registerWithSession(c1, "")

	srv.handleWSMessage(c1, WSMessage{
		Type:      "chat",
		SessionID: sess.ID,
		Content:   "hello from survivor test",
		RunID:     "run-survive-1",
	})

	select {
	case <-gb.started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for backend to start streaming")
	}

	// Simulate the originating tab closing mid-run.
	srv.wsHub.unregisterClient(c1)

	// A different client reconnects mid-run and subscribes to broadcasts.
	c2 := &wsClient{send: make(chan WSMessage, 64), ctx: context.Background()}
	srv.wsHub.registerWithSession(c2, "")

	// Let the run finish.
	close(gb.release)

	// The reconnected client must receive the done event for the run.
	deadline := time.After(5 * time.Second)
	var doneMsg WSMessage
	for doneMsg.Type == "" {
		select {
		case m := <-c2.send:
			if m.Type == "done" {
				doneMsg = m
			}
			if m.Type == "error" {
				t.Fatalf("unexpected error event: %q", m.Content)
			}
		case <-deadline:
			t.Fatal("reconnected client never received the done event — run did not survive unregister")
		}
	}
	if doneMsg.RunID != "run-survive-1" {
		t.Errorf("done.RunID = %q, want run-survive-1", doneMsg.RunID)
	}
	if doneMsg.SessionID != sess.ID {
		t.Errorf("done.SessionID = %q, want %q", doneMsg.SessionID, sess.ID)
	}
	if parseBoolPayload(doneMsg.Payload["cancelled"]) {
		t.Error("done.cancelled = true, want false: run completed normally")
	}

	// The full response (including tokens streamed after the disconnect) must
	// be persisted.
	waitFor := time.After(3 * time.Second)
	for {
		msgs, _ := store.TailMessages(sess.ID, 10)
		if len(msgs) >= 2 {
			if msgs[0].Role != "user" || msgs[0].Content != "hello from survivor test" {
				t.Errorf("persisted user message = %+v", msgs[0])
			}
			if msgs[1].Role != "assistant" || !strings.Contains(msgs[1].Content, "world") {
				t.Errorf("persisted assistant content = %q, want it to contain tokens streamed after disconnect", msgs[1].Content)
			}
			return
		}
		select {
		case <-waitFor:
			t.Fatalf("timed out waiting for persistence; have %d messages", len(msgs))
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// TestHandleWSMessage_ChatCancel_StopsActiveRun verifies the explicit cancel
// pathway: chat_cancel cancels the session's in-flight run, the client gets a
// chat_cancel_result and a done event with cancelled=true, and accumulated
// content is persisted.
func TestHandleWSMessage_ChatCancel_StopsActiveRun(t *testing.T) {
	gb := &gateBackend{started: make(chan struct{}), release: make(chan struct{})}
	orch, err := agent.NewOrchestrator(gb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	store := session.NewStore(t.TempDir())
	srv := New(*config.Default(), orch, store, testToken, t.TempDir(), nil, nil, nil)
	srv.agentLoader = func() (*agents.AgentsConfig, error) {
		return agents.DefaultAgentsConfig(), nil
	}

	sess := store.New("cancel-test", "/workspace", "test-model")
	if err := store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	c := &wsClient{send: make(chan WSMessage, 64), ctx: context.Background()}
	srv.wsHub.registerWithSession(c, "")

	srv.handleWSMessage(c, WSMessage{
		Type:      "chat",
		SessionID: sess.ID,
		Content:   "please cancel me",
		RunID:     "run-cancel-1",
	})
	select {
	case <-gb.started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for backend to start")
	}

	srv.handleWSMessage(c, WSMessage{Type: "chat_cancel", SessionID: sess.ID})

	sawCancelResult, sawCancelledDone := false, false
	deadline := time.After(5 * time.Second)
	for !sawCancelResult || !sawCancelledDone {
		select {
		case m := <-c.send:
			switch m.Type {
			case "chat_cancel_result":
				sawCancelResult = true
				if !parseBoolPayload(m.Payload["cancelled"]) {
					t.Error("chat_cancel_result.cancelled = false, want true")
				}
			case "done":
				sawCancelledDone = true
				if !parseBoolPayload(m.Payload["cancelled"]) {
					t.Error("done.cancelled = false, want true for a cancelled run")
				}
				if m.RunID != "run-cancel-1" {
					t.Errorf("done.RunID = %q, want run-cancel-1", m.RunID)
				}
			case "error":
				t.Fatalf("unexpected error event: %q", m.Content)
			}
		case <-deadline:
			t.Fatalf("timed out: cancel_result=%v cancelled_done=%v", sawCancelResult, sawCancelledDone)
		}
	}

	// Partial content ("hello ") must be persisted.
	waitFor := time.After(3 * time.Second)
	for {
		msgs, _ := store.TailMessages(sess.ID, 10)
		if len(msgs) >= 2 {
			if !strings.Contains(msgs[1].Content, "hello") {
				t.Errorf("persisted partial content = %q, want it to contain 'hello'", msgs[1].Content)
			}
			return
		}
		select {
		case <-waitFor:
			t.Fatalf("timed out waiting for partial persistence; have %d messages", len(msgs))
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// TestHandleWSMessage_ChatCancel_NoActiveRun verifies chat_cancel for an idle
// session reports cancelled=false and does not panic on a bare Server.
func TestHandleWSMessage_ChatCancel_NoActiveRun(t *testing.T) {
	srv := &Server{wsHub: newWSHub()}
	c := &wsClient{send: make(chan WSMessage, 4), ctx: context.Background()}
	srv.handleWSMessage(c, WSMessage{Type: "chat_cancel", SessionID: "idle-session"})
	select {
	case m := <-c.send:
		if m.Type != "chat_cancel_result" {
			t.Fatalf("expected chat_cancel_result, got %q", m.Type)
		}
		if parseBoolPayload(m.Payload["cancelled"]) {
			t.Error("cancelled = true, want false when no run is active")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for chat_cancel_result")
	}
}

// TestBeginChatRun_IndependentOfClientContext is the focused unit test for the
// context derivation: the run context must not be a child of any client
// connection context, and must be cancellable via cancelChatRun.
func TestBeginChatRun_IndependentOfClientContext(t *testing.T) {
	srv := &Server{}

	clientCtx, clientCancel := context.WithCancel(context.Background())
	runCtx, run := srv.beginChatRun("s1", "")
	defer srv.endChatRun("s1", run)

	clientCancel() // simulated disconnect
	select {
	case <-runCtx.Done():
		t.Fatal("run context was cancelled by an unrelated client context")
	default:
	}
	_ = clientCtx

	if !srv.cancelChatRun("s1") {
		t.Fatal("cancelChatRun should find the active run")
	}
	select {
	case <-runCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("cancelChatRun did not cancel the run context")
	}
	// Second cancel is a no-op.
	if srv.cancelChatRun("s1") {
		t.Error("second cancelChatRun should return false")
	}
}

// TestEndChatRun_DoesNotDeregisterSuccessor verifies the pointer-identity
// guard: an older run finishing must not remove a newer run's cancel handle.
// Runs now queue strictly FIFO (a fast-follow message never supersedes an
// in-flight run — see beginChatRun), so run2's admission is reserved
// immediately but it must wait for run1 to end before beginChatRun returns;
// once it does, run2 is the session's registered handle and run1 ending
// late (its own endChatRun, called after run2 already started) must not
// deregister it.
func TestEndChatRun_DoesNotDeregisterSuccessor(t *testing.T) {
	srv := &Server{}
	_, run1 := srv.beginChatRun("s1", "")

	started := make(chan struct{})
	var run2 *chatRunHandle
	var ctx2 context.Context
	go func() {
		ctx2, run2 = srv.beginChatRun("s1", "")
		close(started)
	}()
	select {
	case <-started:
		t.Fatal("run2 must queue behind run1, not replace it immediately")
	case <-time.After(80 * time.Millisecond):
	}

	srv.endChatRun("s1", run1) // run1 finishes, unblocking queued run2

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("run2 did not start after run1 ended")
	}

	// run1 finishing already ran its own endChatRun above (simulating a late
	// finish relative to run2 having started) — it must not have deregistered
	// run2, which is now the session's active handle.
	if !srv.cancelChatRun("s1") {
		t.Fatal("newer run's handle was removed by the older run's endChatRun")
	}
	select {
	case <-ctx2.Done():
	case <-time.After(time.Second):
		t.Fatal("newer run context was not cancelled")
	}
	_ = run2
}

func TestBeginChatRun_TrivialPingQueues(t *testing.T) {
	srv := &Server{}
	ctx1, run1 := srv.beginChatRun("s1", "@Winston ping one")
	started := make(chan struct{})
	var run2 *chatRunHandle
	var ctx2 context.Context
	go func() {
		ctx2, run2 = srv.beginChatRun("s1", "@Winston ping two")
		close(started)
	}()
	select {
	case <-started:
		t.Fatal("ping two must queue behind ping one")
	case <-time.After(80 * time.Millisecond):
	}
	select {
	case <-ctx1.Done():
		t.Fatal("queued ping must not cancel the in-flight ping")
	default:
	}
	srv.endChatRun("s1", run1)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("ping two did not start after ping one ended")
	}
	if run2 == nil {
		t.Fatal("ping two handle missing")
	}
	select {
	case <-ctx2.Done():
		t.Fatal("ping two context cancelled at start")
	default:
	}
	srv.endChatRun("s1", run2)
}
