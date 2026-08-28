package threadmgr

import (
	"context"
	"testing"
	"time"
)

// TestWatchdogPersistsErrorTransition reproduces the live-bug regression: a
// stale StatusQueued thread timed out by the watchdog must have its terminal
// StatusError durably written to the store, not just held in memory. If the
// transition isn't persisted, a process restart reloads the thread from the
// store still StatusQueued (via LoadFromStore), the watchdog times it out
// again on the next scan, and the client sees a duplicate "thread timed out"
// broadcast every process lifetime.
func TestWatchdogPersistsErrorTransition(t *testing.T) {
	db, cleanup := openTestDB(t)
	defer cleanup()

	store := NewSQLiteThreadStore(db)
	tm := New()
	tm.SetStore(store)

	sess := "sess-watchdog-persist"
	th, err := tm.Create(CreateParams{
		SessionID: sess,
		AgentID:   "reggie",
		Task:      "Echo ping",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Backdate CreatedAt so the thread looks stale to the watchdog scan
	// (queuedTimeout is 10 minutes).
	tm.mu.Lock()
	tm.threads[th.ID].CreatedAt = time.Now().Add(-11 * time.Minute)
	tm.mu.Unlock()

	var broadcasts int
	tm.watchdogScan(time.Now(), func(sid, msgType string, payload map[string]any) {
		broadcasts++
	}, 10*time.Minute, 30*time.Minute)

	if broadcasts != 1 {
		t.Fatalf("expected 1 broadcast from watchdog scan, got %d", broadcasts)
	}

	// In-memory transition must have happened.
	got, ok := tm.Get(th.ID)
	if !ok || got.Status != StatusError {
		t.Fatalf("expected thread in-memory status StatusError, got %+v ok=%v", got, ok)
	}

	// Durable store write is the crux of the fix: it must reflect StatusError
	// too, without waiting on the async goroutine used elsewhere — give the
	// (expected) async persistence a brief moment to land.
	deadline := time.Now().Add(2 * time.Second)
	var persisted *Thread
	for time.Now().Before(deadline) {
		threads, lerr := store.LoadThreads(context.Background(), sess)
		if lerr != nil {
			t.Fatalf("LoadThreads: %v", lerr)
		}
		for _, pt := range threads {
			if pt.ID == th.ID {
				persisted = pt
			}
		}
		if persisted != nil && persisted.Status == StatusError {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if persisted == nil {
		t.Fatalf("thread %s not found in store after watchdog transition", th.ID)
	}
	if persisted.Status != StatusError {
		t.Fatalf("store status = %q, want %q — watchdog transition did not write through", persisted.Status, StatusError)
	}

	// --- Simulate a process restart: fresh manager, same store, LoadFromStore. ---
	tm2 := New()
	tm2.SetStore(store)
	if err := tm2.LoadFromStore(context.Background(), sess); err != nil {
		t.Fatalf("LoadFromStore: %v", err)
	}
	reloaded, ok := tm2.Get(th.ID)
	if !ok {
		t.Fatalf("thread %s missing after simulated restart reload", th.ID)
	}
	if reloaded.Status != StatusError {
		t.Fatalf("reloaded status = %q, want %q — restart resurrected a terminal thread as non-terminal", reloaded.Status, StatusError)
	}

	// A second watchdog scan on the "new process" must not re-fire — the
	// thread is already terminal, so no duplicate broadcast.
	var broadcasts2 int
	tm2.watchdogScan(time.Now(), func(sid, msgType string, payload map[string]any) {
		broadcasts2++
	}, 10*time.Minute, 30*time.Minute)
	if broadcasts2 != 0 {
		t.Fatalf("expected 0 broadcasts on restart-scan of an already-terminal thread, got %d", broadcasts2)
	}
}
