package server

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/threadmgr"
)

// TestBroadcastToSession_ErrorStatusPhrasedAsFailure reproduces the live bug
// where a watchdog/reaper timeout ("StatusError" completion) rendered as
// "**Reggie** completed delegated work: delegation timed out — thread never
// started" — an accomplishment phrasing for what is actually a failure. The
// persisted (and broadcast) content for an error-status thread_done must read
// as a failure notice instead.
func TestBroadcastToSession_ErrorStatusPhrasedAsFailure(t *testing.T) {
	srv, _ := newTestServer(t)
	sess := srv.store.New("lifecycle-fail-persist", "/workspace", "test-model")
	if err := srv.store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	tm := threadmgr.New()
	srv.SetThreadManager(tm)
	thread, err := tm.Create(threadmgr.CreateParams{
		SessionID:       sess.ID,
		AgentID:         "Reggie",
		Task:            "Echo ping",
		ParentMessageID: "parent-1",
	})
	if err != nil {
		t.Fatalf("Create thread: %v", err)
	}

	srv.BroadcastToSession(sess.ID, "thread_done", map[string]any{
		"thread_id": thread.ID,
		"status":    "error",
		"summary":   "delegation timed out — thread never started",
	})

	msgs, err := srv.store.TailMessages(sess.ID, 20)
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 persisted lifecycle message, got %d", len(msgs))
	}
	content := msgs[0].Content
	if strings.Contains(content, "completed delegated work") {
		t.Errorf("error-status thread_done must not read as accomplishment, got %q", content)
	}
	want := "**Reggie**'s delegated task failed: delegation timed out — thread never started"
	if content != want {
		t.Errorf("content = %q, want %q", content, want)
	}
}

// TestBroadcastToSession_NonErrorStatusStaysCompletedPhrasing guards against
// over-correcting: a normal (non-error) completion must keep the existing
// "completed delegated work" phrasing.
func TestBroadcastToSession_NonErrorStatusStaysCompletedPhrasing(t *testing.T) {
	srv, _ := newTestServer(t)
	sess := srv.store.New("lifecycle-ok-persist", "/workspace", "test-model")
	if err := srv.store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	tm := threadmgr.New()
	srv.SetThreadManager(tm)
	thread, err := tm.Create(threadmgr.CreateParams{
		SessionID:       sess.ID,
		AgentID:         "Researcher",
		Task:            "Investigate bug",
		ParentMessageID: "parent-1",
	})
	if err != nil {
		t.Fatalf("Create thread: %v", err)
	}

	srv.BroadcastToSession(sess.ID, "thread_done", map[string]any{
		"thread_id": thread.ID,
		"summary":   "Added regression tests.",
	})

	msgs, err := srv.store.TailMessages(sess.ID, 20)
	if err != nil {
		t.Fatalf("TailMessages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 persisted lifecycle message, got %d", len(msgs))
	}
	want := "**Researcher** completed delegated work: Added regression tests."
	if msgs[0].Content != want {
		t.Errorf("content = %q, want %q", msgs[0].Content, want)
	}
}

// TestBroadcastToSession_BackfillsAgentIDOnPayload reproduces the "Delegate"
// literal-fallback bug: none of the Go-side thread_done broadcasts include
// "agent_id" in the payload map, so the frontend's WS handler (which reads
// payload.agent_id directly for the live completion bubble) always fell back
// to a placeholder label. persistThreadLifecycleEvent resolves the real
// agent from the ThreadManager — that resolved value must be written back
// into the payload map so it reaches the WS broadcast too, not just the
// persisted hallway string.
func TestBroadcastToSession_BackfillsAgentIDOnPayload(t *testing.T) {
	srv, _ := newTestServer(t)
	sess := srv.store.New("lifecycle-agentid-backfill", "/workspace", "test-model")
	if err := srv.store.SaveManifest(sess); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	tm := threadmgr.New()
	srv.SetThreadManager(tm)
	thread, err := tm.Create(threadmgr.CreateParams{
		SessionID:       sess.ID,
		AgentID:         "Reggie",
		Task:            "Echo ping",
		ParentMessageID: "parent-1",
	})
	if err != nil {
		t.Fatalf("Create thread: %v", err)
	}

	payload := map[string]any{
		"thread_id": thread.ID,
		"status":    "error",
		"summary":   "delegation timed out — thread never started",
	}
	srv.BroadcastToSession(sess.ID, "thread_done", payload)

	got, _ := payload["agent_id"].(string)
	if got != "Reggie" {
		t.Errorf("payload[agent_id] = %q, want %q — WS clients cannot resolve the real agent name", got, "Reggie")
	}
}
