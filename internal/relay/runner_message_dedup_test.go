package relay_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/scrypster/huginn/internal/relay"
)

// TestActiveSessions_GenerationTokenBasics verifies the generation token
// mechanism works correctly for session replacement tracking.
func TestActiveSessions_GenerationTokenBasics(t *testing.T) {
	active := relay.NewActiveSessions()

	sessionID := "session-1"
	gen1, replaced1 := active.Start(sessionID, func() {})
	if replaced1 {
		t.Error("expected no replacement on first Start")
	}

	// Replace with session 2
	gen2, replaced2 := active.Start(sessionID, func() {})
	if !replaced2 {
		t.Error("expected replacement=true on second Start")
	}
	if gen2 <= gen1 {
		t.Errorf("gen2 (%d) should be > gen1 (%d)", gen2, gen1)
	}

	// Remove with old generation should not affect current session
	active.Remove(sessionID, gen1)

	// Verify session still exists (with gen2) by starting another session
	gen3, replaced3 := active.Start(sessionID, func() {})
	if !replaced3 {
		t.Error("expected replacement=true when replacing gen2")
	}
	if gen3 <= gen2 {
		t.Errorf("gen3 (%d) should be > gen2 (%d)", gen3, gen2)
	}

	// Now remove with correct gen
	active.Remove(sessionID, gen3)

	// Verify truly removed
	_, replaced4 := active.Start(sessionID, func() {})
	if replaced4 {
		t.Error("expected no replacement after Remove with correct gen")
	}
}

// TestActiveSessions_CancelAll verifies that CancelAll properly cancels
// all active sessions.
func TestActiveSessions_CancelAll(t *testing.T) {
	active := relay.NewActiveSessions()

	// Create multiple sessions with tracking
	var cancelledCount int32
	makeSession := func(id string) {
		cancel := func() {
			atomic.AddInt32(&cancelledCount, 1)
		}
		active.Start(id, cancel)
	}

	// Start 5 sessions
	for i := 0; i < 5; i++ {
		makeSession(string(rune('a' + i)))
	}

	// Cancel all
	active.CancelAll()

	// Verify count
	cancelled := atomic.LoadInt32(&cancelledCount)
	if cancelled != 5 {
		t.Errorf("expected 5 sessions cancelled, got %d", cancelled)
	}
}

// TestActiveSessions_CancelReturnsStatus verifies that Cancel returns true
// only for sessions that exist.
func TestActiveSessions_CancelReturnsStatus(t *testing.T) {
	active := relay.NewActiveSessions()

	_, cancel := active.Start("session-1", func() {})

	// Cancel existing session
	found := active.Cancel("session-1")
	if !found {
		t.Error("Cancel should return true for existing session")
	}

	// Cancel non-existent session
	found = active.Cancel("non-existent")
	if found {
		t.Error("Cancel should return false for non-existent session")
	}

	// Cancel same session again
	found = active.Cancel("session-1")
	if found {
		t.Error("Cancel should return false for already-cancelled session")
	}

	// Clean up
	_ = cancel
}

// TestActiveSessions_ConcurrentOperations tests concurrent Start/Remove/Cancel.
func TestActiveSessions_ConcurrentOperations(t *testing.T) {
	active := relay.NewActiveSessions()

	// Concurrent Start operations
	var wg sync.WaitGroup
	gens := make([]uint64, 100)
	gens_mu := sync.Mutex{}

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			gen, _ := active.Start("session-1", func() {})
			gens_mu.Lock()
			gens[idx] = gen
			gens_mu.Unlock()
		}(i)
	}

	wg.Wait()

	// Verify all generations are unique and increasing
	gens_mu.Lock()
	seen := make(map[uint64]bool)
	for _, g := range gens {
		if seen[g] {
			t.Errorf("duplicate generation: %d", g)
		}
		seen[g] = true
	}
	gens_mu.Unlock()

	if len(seen) != 100 {
		t.Errorf("expected 100 unique generations, got %d", len(seen))
	}
}
