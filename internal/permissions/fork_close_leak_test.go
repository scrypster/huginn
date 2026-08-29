package permissions

import (
	"runtime"
	"testing"
	"time"
)

// TestGate_Fork_CloseStopsSweepGoroutine is a regression test for a goroutine
// leak: every Fork started its own sweep goroutine (startSweep) but nothing
// closed forked gates created per agent-dispatch turn, so each dispatch
// permanently leaked one sweeper. This repeatedly forks and closes a gate and
// asserts the goroutine count stays bounded, proving Close actually tears
// down the fork's sweeper rather than the leak just being timing-masked.
func TestGate_Fork_CloseStopsSweepGoroutine(t *testing.T) {
	const iterations = 50

	parent := NewGate(false, func(req PermissionRequest) Decision { return Deny })
	defer parent.Close()

	// Let the parent's own sweep goroutine start and settle before measuring.
	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	before := runtime.NumGoroutine()

	for i := 0; i < iterations; i++ {
		child := parent.Fork(nil, nil)
		child.Close()
	}

	// Give any leaked goroutines a chance to show up (they wouldn't exit on
	// their own, but GC + a short settle avoids flakiness from scheduler
	// noise around the measurement itself).
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	after := runtime.NumGoroutine()

	// Small slack for unrelated runtime/test-harness goroutines. Without the
	// Close() call fixed, this delta would grow by ~1 goroutine per
	// iteration (50), which dwarfs this slack.
	const slack = 5
	if after > before+slack {
		t.Errorf("possible sweep goroutine leak across %d fork+close cycles: before=%d after=%d (delta=%d > slack=%d)",
			iterations, before, after, after-before, slack)
	}
}

// TestGate_Fork_CloseIsIndependentOfParent verifies the safety property that
// closing a forked (child) gate does not stop the parent's sweep goroutine or
// otherwise disturb the parent's ability to keep serving relay/permission
// requests — Close on a fork must only affect that fork.
func TestGate_Fork_CloseIsIndependentOfParent(t *testing.T) {
	parent := NewGate(false, func(req PermissionRequest) Decision { return Deny })
	defer parent.Close()

	child := parent.Fork(nil, nil)
	child.Close()

	// Parent's sweepDone must remain open (not closed by the child's Close).
	select {
	case <-parent.sweepDone:
		t.Fatal("closing a forked child gate closed the parent's sweepDone channel")
	default:
		// expected: parent still open
	}

	// A second, independent fork from the same parent must still work
	// normally (own sweeper, own relayChans) after a sibling fork was closed.
	sibling := parent.Fork(nil, nil)
	defer sibling.Close()

	select {
	case <-sibling.sweepDone:
		t.Fatal("sibling fork's sweepDone was already closed — forks must not share sweepDone")
	default:
	}

	// Registering a relay entry on the closed child must not panic and must
	// not affect the parent's or sibling's relayChans.
	ch := make(chan bool, 1)
	child.mu.Lock()
	child.relayChans["leftover"] = relayEntry{ch: ch, createdAt: time.Now()}
	child.mu.Unlock()

	parent.mu.Lock()
	_, parentHasEntry := parent.relayChans["leftover"]
	parent.mu.Unlock()
	if parentHasEntry {
		t.Fatal("child's relayChans entry leaked into the parent's relayChans map")
	}
}

// TestGate_Close_IsIdempotent verifies Close can be called multiple times
// (e.g. a defer plus an explicit early call) without panicking, matching the
// documented idempotent contract.
func TestGate_Close_IsIdempotent(t *testing.T) {
	g := NewGate(false, nil)
	g.Close()
	g.Close()
	g.Close()
}
