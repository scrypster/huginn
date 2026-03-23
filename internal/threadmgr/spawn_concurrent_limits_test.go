package threadmgr_test

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/scrypster/huginn/internal/threadmgr"
)

// TestThreadManager_ConcurrentSpawnLimit verifies that the active thread limit
// is correctly enforced even under concurrent Create calls from multiple goroutines.
func TestThreadManager_ConcurrentSpawnLimit(t *testing.T) {
	t.Parallel()

	tm := threadmgr.New()
	tm.MaxThreadsPerSession = 5 // Keep it small for testing
	sessionID := "test-session"

	var wg sync.WaitGroup
	var successCount atomic.Int32
	var limitExceededCount atomic.Int32
	var totalErrors atomic.Int32

	const numGoroutines = 20

	// Spawn 20 goroutines, each trying to create threads
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for attempt := 0; attempt < 3; attempt++ {
				_, err := tm.Create(threadmgr.CreateParams{
					SessionID: sessionID,
					AgentID:   "agent1",
					Task:      "test task",
				})
				if err == nil {
					successCount.Add(1)
				} else if err == threadmgr.ErrThreadLimitExceeded {
					limitExceededCount.Add(1)
				} else {
					totalErrors.Add(1)
				}
			}
		}(g)
	}

	wg.Wait()

	// Verify constraints
	successful := successCount.Load()
	limited := limitExceededCount.Load()
	errored := totalErrors.Load()

	// Total attempts should equal successful + limited + errored
	expectedAttempts := int32(numGoroutines * 3)
	actualAttempts := successful + limited + errored
	if actualAttempts != expectedAttempts {
		t.Errorf("total attempts mismatch: expected %d, got %d (successful=%d, limited=%d, errors=%d)",
			expectedAttempts, actualAttempts, successful, limited, errored)
	}

	// At most MaxThreadsPerSession should have succeeded
	if successful > 5 {
		t.Errorf("limit exceeded: %d threads created when max is 5", successful)
	}

	// Verify that exactly 5 threads are in the map
	threads := tm.ListBySession(sessionID)
	activeCount := 0
	for _, th := range threads {
		if th.Status != threadmgr.StatusDone && th.Status != threadmgr.StatusCancelled && th.Status != threadmgr.StatusError {
			activeCount++
		}
	}
	if activeCount > 5 {
		t.Errorf("more than 5 active threads detected: %d", activeCount)
	}
}

// TestThreadManager_ConcurrentCreateAndComplete verifies that concurrent Create
// and Complete operations correctly maintain the thread count for limit checks.
func TestThreadManager_ConcurrentCreateAndComplete(t *testing.T) {
	t.Parallel()

	tm := threadmgr.New()
	tm.MaxThreadsPerSession = 3
	sessionID := "test-session"

	// Create 3 initial threads to hit the limit
	var initialThreads []*threadmgr.Thread
	for i := 0; i < 3; i++ {
		th, err := tm.Create(threadmgr.CreateParams{
			SessionID: sessionID,
			AgentID:   "agent1",
			Task:      "initial task",
		})
		if err != nil {
			t.Fatalf("initial create %d: %v", i, err)
		}
		initialThreads = append(initialThreads, th)
	}

	var wg sync.WaitGroup
	var createdCount atomic.Int32

	// Try to create more while completing the initial ones
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Try to create a new thread
			_, err := tm.Create(threadmgr.CreateParams{
				SessionID: sessionID,
				AgentID:   "agent2",
				Task:      "concurrent task",
			})
			if err == nil {
				createdCount.Add(1)
			}
		}(g)
	}

	// In parallel, complete the initial threads
	for g := 0; g < 3; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if id < len(initialThreads) {
				tm.Complete(initialThreads[id].ID, threadmgr.FinishSummary{
					Status: "ok",
				})
			}
		}(g)
	}

	wg.Wait()

	// Verify: some creates should have succeeded after completions
	created := createdCount.Load()
	if created < 3 {
		t.Logf("Warning: only %d threads created during concurrent completion, expected ~3", created)
	}

	// Final state check: should have original 3 + some of the concurrent creates
	threads := tm.ListBySession(sessionID)
	if len(threads) < 3 {
		t.Errorf("expected at least 3 threads, got %d", len(threads))
	}
}

// TestThreadManager_MultipleSessionsIndependentLimits verifies that the limit
// is per-session, not global.
func TestThreadManager_MultipleSessionsIndependentLimits(t *testing.T) {
	t.Parallel()

	tm := threadmgr.New()
	tm.MaxThreadsPerSession = 3

	var wg sync.WaitGroup
	var results map[string]int32 = make(map[string]int32)
	var resultsMu sync.Mutex

	// Pre-populate results map before launching goroutines to avoid a
	// concurrent-map-write race between the setup loop and the goroutines.
	for s := 0; s < 3; s++ {
		results["session-"+string(rune('A'+s))] = 0
	}

	// Create threads in 3 different sessions concurrently
	for s := 0; s < 3; s++ {
		sessionID := "session-" + string(rune('A'+s))

		wg.Add(1)
		go func(sid string) {
			defer wg.Done()
			var count int32
			for attempt := 0; attempt < 10; attempt++ {
				_, err := tm.Create(threadmgr.CreateParams{
					SessionID: sid,
					AgentID:   "agent",
					Task:      "test",
				})
				if err == nil {
					count++
				}
			}
			resultsMu.Lock()
			results[sid] = count
			resultsMu.Unlock()
		}(sessionID)
	}

	wg.Wait()

	// Each session should have exactly 3 threads (hit the limit)
	for sessionID, count := range results {
		if count != 3 {
			t.Errorf("session %s: expected 3 threads, got %d", sessionID, count)
		}
	}

	// Total threads should be 9 (3 per session)
	allThreads := 0
	for s := 0; s < 3; s++ {
		sessionID := "session-" + string(rune('A'+s))
		threads := tm.ListBySession(sessionID)
		allThreads += len(threads)
	}
	if allThreads != 9 {
		t.Errorf("expected 9 total threads, got %d", allThreads)
	}
}

// TestThreadManager_ConcurrentCancel verifies that concurrent Cancel calls
// don't cause data corruption or panics.
func TestThreadManager_ConcurrentCancel(t *testing.T) {
	t.Parallel()

	tm := threadmgr.New()
	sessionID := "test-session"

	// Create 5 threads
	var threadIDs []string
	for i := 0; i < 5; i++ {
		th, err := tm.Create(threadmgr.CreateParams{
			SessionID: sessionID,
			AgentID:   "agent1",
			Task:      "test",
		})
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		threadIDs = append(threadIDs, th.ID)
	}

	// Spawn multiple goroutines cancelling the same threads
	var wg sync.WaitGroup
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Each goroutine cancels a thread
			tm.Cancel(threadIDs[id%len(threadIDs)])
		}(g)
	}

	wg.Wait()

	// Verify all threads are in cancelled or another terminal state
	threads := tm.ListBySession(sessionID)
	for _, th := range threads {
		if th.Status != threadmgr.StatusCancelled && th.Status != threadmgr.StatusDone &&
			th.Status != threadmgr.StatusError {
			t.Errorf("thread %s in unexpected status", th.ID)
		}
	}
}

// TestThreadManager_GetUnderConcurrentCreate verifies that Get() correctly
// observes threads created concurrently.
func TestThreadManager_GetUnderConcurrentCreate(t *testing.T) {
	t.Parallel()

	tm := threadmgr.New()
	sessionID := "test-session"

	var createdThreads []*threadmgr.Thread
	var createdMu sync.Mutex
	var wg sync.WaitGroup

	// Create threads concurrently
	for g := 0; g < 5; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			th, err := tm.Create(threadmgr.CreateParams{
				SessionID: sessionID,
				AgentID:   "agent1",
				Task:      "test",
			})
			if err == nil {
				createdMu.Lock()
				createdThreads = append(createdThreads, th)
				createdMu.Unlock()
			}
		}(g)
	}

	wg.Wait()

	// Verify that Get() can retrieve each created thread
	for _, th := range createdThreads {
		retrieved, ok := tm.Get(th.ID)
		if !ok {
			t.Errorf("failed to Get thread %s", th.ID)
		}
		if retrieved.ID != th.ID {
			t.Errorf("retrieved wrong thread: expected %s, got %s", th.ID, retrieved.ID)
		}
	}

	// Verify ListBySession returns all threads
	listed := tm.ListBySession(sessionID)
	if len(listed) != len(createdThreads) {
		t.Errorf("ListBySession returned %d threads, expected %d", len(listed), len(createdThreads))
	}
}
