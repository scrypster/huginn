package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/session"
)

func TestWatcherIngestsNewTranscript(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "-tmp-proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ing, sink, _ := newTestIngester(t)
	w := NewWatcher(root, ing)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- w.Run(ctx) }()

	// Give the watcher a moment to register its directory watches.
	time.Sleep(200 * time.Millisecond)

	fixture, err := os.ReadFile(filepath.Join("testdata", "basic.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dst := filepath.Join(proj, "11111111-2222-3333-4444-555555555555.jsonl")
	if err := os.WriteFile(dst, fixture, 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	waitForSession(t, sink, "11111111-2222-3333-4444-555555555555", 5*time.Second)
}

func TestWatcherStopsOnContextCancel(t *testing.T) {
	ing, _, _ := newTestIngester(t)
	w := NewWatcher(t.TempDir(), ing)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil && err != context.Canceled {
			t.Errorf("Run returned %v, want nil or context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return within 3s of cancellation")
	}
}

func TestWatcherMissingRootIsNotFatal(t *testing.T) {
	ing, _, _ := newTestIngester(t)
	w := NewWatcher(filepath.Join(t.TempDir(), "does-not-exist"), ing)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := w.Run(ctx); err != nil && err != context.Canceled {
		t.Errorf("Run on a missing root returned %v, want nil or context.Canceled", err)
	}
}

func TestWatcherIngestsLinesAppendedAfterCreate(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "-tmp-proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ing, sink, _ := newTestIngester(t)
	w := NewWatcher(root, ing)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()
	time.Sleep(200 * time.Millisecond)

	const extID = "33333333-3333-3333-4444-555555555555"
	dst := filepath.Join(proj, extID+".jsonl")

	u1 := `{"type":"user","uuid":"y1","sessionId":"` + extID + `","cwd":"/tmp/p3","timestamp":"2026-08-25T10:00:00.000Z","message":{"role":"user","content":[{"type":"text","text":"first"}]}}` + "\n"
	a1 := `{"type":"assistant","uuid":"y2","parentUuid":"y1","sessionId":"` + extID + `","timestamp":"2026-08-25T10:00:02.000Z","message":{"role":"assistant","model":"claude-opus-5","content":[{"type":"text","text":"second"}]}}` + "\n"

	if err := os.WriteFile(dst, []byte(u1), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	sess := waitForSession(t, sink, extID, 5*time.Second)
	waitForCount(t, sink, sess.ID, 1, 5*time.Second)

	// The session continues: Claude Code APPENDS to the same file.
	appendFile(t, dst, a1)
	waitForCount(t, sink, sess.ID, 2, 5*time.Second)
}

func waitForSession(t *testing.T, sink *fakeSink, extID string, d time.Duration) *session.Session {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if s := sink.session(extID); s != nil {
			return s
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("session %s never appeared within %s", extID, d)
	return nil
}

func waitForCount(t *testing.T, sink *fakeSink, sessionID string, want int, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	var got int
	for time.Now().Before(deadline) {
		got = sink.messageCount(sessionID)
		if got >= want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("message count reached %d, want %d, within %s", got, want, d)
}
