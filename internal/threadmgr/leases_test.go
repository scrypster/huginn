package threadmgr

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

func TestAcquireLeases_NoConflict(t *testing.T) {
	tm := New()
	conflicts, err := tm.AcquireLeases("t-1", []string{"a.go", "b.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) != 0 {
		t.Errorf("expected no conflicts, got: %v", conflicts)
	}
}

func TestAcquireLeases_Conflict(t *testing.T) {
	tm := New()
	_, _ = tm.AcquireLeases("t-1", []string{"shared.go"})
	conflicts, err := tm.AcquireLeases("t-2", []string{"shared.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(conflicts) == 0 {
		t.Error("expected conflict for shared.go, got none")
	}
	if conflicts[0] != "shared.go" {
		t.Errorf("expected 'shared.go' in conflicts, got: %v", conflicts)
	}
}

func TestReleaseLeases_FreesFile(t *testing.T) {
	tm := New()
	_, _ = tm.AcquireLeases("t-1", []string{"file.go"})
	tm.ReleaseLeases("t-1")
	conflicts, _ := tm.AcquireLeases("t-2", []string{"file.go"})
	if len(conflicts) != 0 {
		t.Errorf("after release, expected no conflict, got: %v", conflicts)
	}
}

func TestAcquireLeases_PartialConflict(t *testing.T) {
	tm := New()
	_, _ = tm.AcquireLeases("t-1", []string{"a.go"})
	conflicts, _ := tm.AcquireLeases("t-2", []string{"a.go", "b.go"})
	// Only a.go conflicts; b.go should not be acquired on conflict
	if len(conflicts) != 1 || conflicts[0] != "a.go" {
		t.Errorf("expected [a.go] conflict, got: %v", conflicts)
	}
	// b.go was NOT acquired (conflict aborts partial acquisition)
	conflicts2, _ := tm.AcquireLeases("t-3", []string{"b.go"})
	if len(conflicts2) != 0 {
		t.Errorf("b.go should be free after aborted partial acquisition, got: %v", conflicts2)
	}
}

func TestReleaseLeases_NoOpForUnknownThread(t *testing.T) {
	tm := New()
	// Should not panic
	tm.ReleaseLeases("nonexistent-thread")
}

func TestAcquireLeases_ConcurrentStress(t *testing.T) {
	tm := New()
	const numGoroutines = 20
	const sharedFile = "shared.go"

	var wg sync.WaitGroup
	var acquireCount int64 // how many goroutines successfully held the lease

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			tid := fmt.Sprintf("t-%d", id)
			conflicts, err := tm.AcquireLeases(tid, []string{sharedFile})
			if err != nil {
				return // empty threadID, shouldn't happen here
			}
			if len(conflicts) == 0 {
				// We hold the lease
				atomic.AddInt64(&acquireCount, 1)
				tm.ReleaseLeases(tid)
			}
		}(i)
	}

	wg.Wait()
	// At least one goroutine should have acquired the lease
	if acquireCount == 0 {
		t.Error("expected at least one goroutine to acquire the lease")
	}
}
