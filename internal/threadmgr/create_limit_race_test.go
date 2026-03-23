package threadmgr

import (
	"sync"
	"sync/atomic"
	"testing"
)

// TestThreadManager_CreateLimitRace verifies that the thread creation limit
// is properly enforced even under concurrent Create calls.
func TestThreadManager_CreateLimitRace(t *testing.T) {
	tm := New()
	sessionID := "session-1"
	limit := 5
	tm.MaxThreadsPerSession = limit

	// Attempt to create (limit + N) threads concurrently
	var wg sync.WaitGroup
	var successCount int32
	var errorCount int32

	attempts := limit * 3

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			p := CreateParams{
				SessionID: sessionID,
				AgentID:   "agent-1",
				Task:      "task",
			}
			_, err := tm.Create(p)
			if err == nil {
				atomic.AddInt32(&successCount, 1)
			} else {
				atomic.AddInt32(&errorCount, 1)
			}
		}(i)
	}

	wg.Wait()

	final := atomic.LoadInt32(&successCount)
	if final > int32(limit) {
		t.Errorf("created %d threads, limit is %d: race condition detected", final, limit)
	}

	finalErrors := atomic.LoadInt32(&errorCount)
	if finalErrors == 0 {
		t.Errorf("expected some Create() calls to fail, all succeeded")
	}

	// Verify internal state matches
	tm.mu.RLock()
	actual := tm.activeThreadCountLocked(sessionID)
	tm.mu.RUnlock()

	if actual != int(final) {
		t.Errorf("internal state mismatch: success=%d, actual count=%d", final, actual)
	}
}

// TestThreadManager_DependencyChaining verifies dependency recording works.
func TestThreadManager_DependencyChaining(t *testing.T) {
	tm := New()
	sessionID := "session-1"

	// Create thread A
	p1 := CreateParams{
		SessionID: sessionID,
		AgentID:   "agent-1",
		Task:      "task-a",
	}
	t1, err := tm.Create(p1)
	if err != nil {
		t.Fatalf("create t1: %v", err)
	}

	// Create thread B that depends on A
	p2 := CreateParams{
		SessionID: sessionID,
		AgentID:   "agent-1",
		Task:      "task-b",
		DependsOn: []string{t1.ID},
	}
	t2, err := tm.Create(p2)
	if err != nil {
		t.Fatalf("create t2: %v", err)
	}

	// Verify dependency chain is recorded correctly
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if _, ok := tm.threads[t1.ID]; !ok {
		t.Fatalf("t1 not found")
	}
	if _, ok := tm.threads[t2.ID]; !ok {
		t.Fatalf("t2 not found")
	}

	// Verify t2 depends on t1
	if len(tm.threads[t2.ID].DependsOn) != 1 {
		t.Errorf("expected t2 to have 1 dependency, got %d", len(tm.threads[t2.ID].DependsOn))
	}
	if tm.threads[t2.ID].DependsOn[0] != t1.ID {
		t.Errorf("t2 dependency incorrect: expected %s, got %s", t1.ID, tm.threads[t2.ID].DependsOn[0])
	}
}

// TestThreadManager_GetByID verifies thread lookup by ID.
func TestThreadManager_GetByID(t *testing.T) {
	tm := New()
	sessionID := "session-1"

	p := CreateParams{
		SessionID: sessionID,
		AgentID:   "agent-1",
		Task:      "task",
	}
	thread, _ := tm.Create(p)

	// Lookup by ID
	got, ok := tm.Get(thread.ID)
	if !ok {
		t.Fatalf("Get returned not found")
	}
	if got.ID != thread.ID {
		t.Errorf("ID mismatch: expected %s, got %s", thread.ID, got.ID)
	}

	// Lookup non-existent should return not found
	_, notFound := tm.Get("non-existent")
	if notFound {
		t.Error("expected not found for non-existent thread")
	}
}

// TestThreadManager_ListBySession verifies listing threads by session.
func TestThreadManager_ListBySession(t *testing.T) {
	tm := New()
	session1 := "session-1"
	session2 := "session-2"

	// Create threads in session 1
	for i := 0; i < 3; i++ {
		p := CreateParams{
			SessionID: session1,
			AgentID:   "agent-1",
			Task:      "task",
		}
		_, _ = tm.Create(p)
	}

	// Create threads in session 2
	for i := 0; i < 2; i++ {
		p := CreateParams{
			SessionID: session2,
			AgentID:   "agent-2",
			Task:      "task",
		}
		_, _ = tm.Create(p)
	}

	// List session 1
	threads1 := tm.ListBySession(session1)
	if len(threads1) != 3 {
		t.Errorf("expected 3 threads in session1, got %d", len(threads1))
	}

	// List session 2
	threads2 := tm.ListBySession(session2)
	if len(threads2) != 2 {
		t.Errorf("expected 2 threads in session2, got %d", len(threads2))
	}
}

// TestThreadManager_ConcurrentCreation tests creation under concurrent load.
func TestThreadManager_ConcurrentCreation(t *testing.T) {
	tm := New()
	tm.MaxThreadsPerSession = 100

	var wg sync.WaitGroup
	threadCount := 50

	for i := 0; i < threadCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			p := CreateParams{
				SessionID: "session-1",
				AgentID:   "agent-1",
				Task:      "task",
			}
			_, _ = tm.Create(p)
		}(i)
	}

	wg.Wait()

	// Verify all were created
	list := tm.ListBySession("session-1")
	if len(list) != threadCount {
		t.Errorf("expected %d threads created, got %d", threadCount, len(list))
	}
}
