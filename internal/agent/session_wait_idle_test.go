package agent

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestSession_WaitForIdle_SignalsOnEndRun verifies that WaitForIdle returns true
// once endRun is called from another goroutine.
func TestSession_WaitForIdle_SignalsOnEndRun(t *testing.T) {
	sess := newSession("test-signal")
	if !sess.tryBeginRun() {
		t.Fatal("tryBeginRun: expected true for fresh session")
	}

	done := make(chan bool, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		done <- sess.WaitForIdle(ctx)
	}()

	// Briefly sleep to ensure WaitForIdle is blocked before we release.
	time.Sleep(20 * time.Millisecond)
	sess.endRun()

	select {
	case result := <-done:
		if !result {
			t.Error("WaitForIdle: expected true after endRun, got false")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForIdle: timed out waiting for signal")
	}
}

// TestSession_WaitForIdle_ContextTimeout verifies that WaitForIdle returns false
// when the context expires before endRun is called.
func TestSession_WaitForIdle_ContextTimeout(t *testing.T) {
	sess := newSession("test-timeout")
	if !sess.tryBeginRun() {
		t.Fatal("tryBeginRun: expected true for fresh session")
	}
	defer sess.endRun()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	result := sess.WaitForIdle(ctx)
	elapsed := time.Since(start)

	if result {
		t.Error("WaitForIdle: expected false (context timeout), got true")
	}
	if elapsed < 40*time.Millisecond {
		t.Errorf("WaitForIdle returned too fast: %v", elapsed)
	}
}

// TestSession_WaitForIdle_AlreadyIdle verifies that WaitForIdle returns true
// immediately when the session is not running.
func TestSession_WaitForIdle_AlreadyIdle(t *testing.T) {
	sess := newSession("test-already-idle")
	ctx := context.Background()
	if !sess.WaitForIdle(ctx) {
		t.Error("WaitForIdle on idle session: expected true immediately")
	}
}

// TestSession_ConcurrentRunDone spawns 50 goroutines that each tryBeginRun +
// WaitForIdle to verify no data races and correct serialization under load.
func TestSession_ConcurrentRunDone(t *testing.T) {
	sess := newSession("test-concurrent")
	const n = 50

	var wg sync.WaitGroup
	errors := make(chan string, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if sess.tryBeginRun() {
				// We got the slot; briefly hold it then release.
				time.Sleep(1 * time.Millisecond)
				sess.endRun()
			} else {
				// We didn't get the slot — wait for idle (should succeed quickly).
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				if !sess.WaitForIdle(ctx) {
					errors <- "WaitForIdle timed out"
				}
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}
}

// TestAppendToThread_UpdatesReplyCount verifies that Append with a ParentMessageID
// increments thread_reply_count on the parent message in the same transaction.
