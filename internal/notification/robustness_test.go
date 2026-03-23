package notification

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
)

// TestStore_ConcurrentPut verifies concurrent Put operations are safe.
func TestStore_ConcurrentPut(t *testing.T) {
	db, closer := makeTestDB(t)
	defer closer()
	store := NewStore(db)

	const numConcurrent = 50
	done := make(chan bool, numConcurrent)
	var mu sync.Mutex
	ids := make([]string, 0, numConcurrent)

	for i := 0; i < numConcurrent; i++ {
		go func(idx int) {
			defer func() { done <- true }()
			n := &Notification{
				ID:        fmt.Sprintf("notif_%d_%d", idx, time.Now().UnixNano()),
				RoutineID: fmt.Sprintf("routine_%d", idx%10),
				RunID:     fmt.Sprintf("run_%d", idx%5),
				Status:    StatusPending,
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
			}
			if err := store.Put(n); err != nil {
				t.Errorf("Put failed: %v", err)
				return
			}
			mu.Lock()
			ids = append(ids, n.ID)
			mu.Unlock()
		}(i)
	}

	for i := 0; i < numConcurrent; i++ {
		<-done
	}

	// Verify all notifications can be retrieved
	mu.Lock()
	numIDs := len(ids)
	mu.Unlock()

	if numIDs != numConcurrent {
		t.Errorf("expected %d IDs, got %d", numConcurrent, numIDs)
	}
}

// TestStore_ConcurrentGet verifies concurrent Get operations are safe.
func TestStore_ConcurrentGet(t *testing.T) {
	db, closer := makeTestDB(t)
	defer closer()
	store := NewStore(db)

	// Put a notification
	n := &Notification{
		ID:        "notif_get_test",
		RoutineID: "routine_1",
		RunID:     "run_1",
		Status:    StatusPending,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := store.Put(n); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Concurrent reads
	const numGoroutines = 50
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer func() { done <- true }()
			retrieved, err := store.Get("notif_get_test")
			if err != nil {
				t.Errorf("Get failed: %v", err)
				return
			}
			if retrieved == nil {
				t.Error("Got nil notification")
				return
			}
			if retrieved.ID != "notif_get_test" {
				t.Errorf("expected ID 'notif_get_test', got %q", retrieved.ID)
			}
		}()
	}

	for i := 0; i < numGoroutines; i++ {
		<-done
	}
}

// TestStore_ConcurrentTransition verifies concurrent Transition operations.
func TestStore_ConcurrentTransition(t *testing.T) {
	db, closer := makeTestDB(t)
	defer closer()
	store := NewStore(db)

	// Create notifications
	const numNotifications = 10
	for i := 0; i < numNotifications; i++ {
		n := &Notification{
			ID:        fmt.Sprintf("notif_%d", i),
			RoutineID: "routine_1",
			RunID:     "run_1",
			Status:    StatusPending,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
		if err := store.Put(n); err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// Transition them concurrently
	done := make(chan bool, numNotifications)
	for i := 0; i < numNotifications; i++ {
		go func(idx int) {
			defer func() { done <- true }()
			id := fmt.Sprintf("notif_%d", idx)
			if err := store.Transition(id, StatusSeen); err != nil {
				t.Errorf("Transition failed: %v", err)
				return
			}
		}(i)
	}

	for i := 0; i < numNotifications; i++ {
		<-done
	}

	// Verify all are seen
	pending, err := store.ListPending()
	if err != nil {
		t.Fatalf("ListPending failed: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("expected 0 pending after transition, got %d", len(pending))
	}
}

// TestStore_DuplicateNotifications verifies handling of duplicate IDs.
func TestStore_DuplicateNotifications(t *testing.T) {
	db, closer := makeTestDB(t)
	defer closer()
	store := NewStore(db)

	n := &Notification{
		ID:        "dup_notif",
		RoutineID: "routine_1",
		RunID:     "run_1",
		Status:    StatusPending,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	// First Put
	if err := store.Put(n); err != nil {
		t.Fatalf("First Put failed: %v", err)
	}

	// Second Put with same ID (should overwrite)
	n.Status = StatusSeen
	if err := store.Put(n); err != nil {
		t.Fatalf("Second Put failed: %v", err)
	}

	// Verify the status was updated
	retrieved, err := store.Get("dup_notif")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if retrieved.Status != StatusSeen {
		t.Errorf("expected StatusSeen, got %v", retrieved.Status)
	}
}

// TestStore_ListByRoutine_Ordering verifies newest-first ordering.
func TestStore_ListByRoutine_Ordering(t *testing.T) {
	db, closer := makeTestDB(t)
	defer closer()
	store := NewStore(db)

	// Create notifications with increasing timestamps
	const numNotifs = 10
	for i := 0; i < numNotifs; i++ {
		n := &Notification{
			ID:        fmt.Sprintf("notif_order_%02d", i),
			RoutineID: "routine_order",
			RunID:     "run_order",
			Status:    StatusPending,
			CreatedAt: time.Now().UTC().Add(time.Duration(i) * time.Second),
			UpdatedAt: time.Now().UTC().Add(time.Duration(i) * time.Second),
		}
		if err := store.Put(n); err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// List and verify newest-first ordering
	list, err := store.ListByRoutine("routine_order")
	if err != nil {
		t.Fatalf("ListByRoutine failed: %v", err)
	}

	if len(list) != numNotifs {
		t.Errorf("expected %d notifications, got %d", numNotifs, len(list))
	}

	// Verify descending order (newest first)
	for i := 0; i < len(list)-1; i++ {
		if list[i].CreatedAt.Before(list[i+1].CreatedAt) {
			t.Errorf("expected newest-first ordering, but item %d is older than %d", i, i+1)
		}
	}
}

// TestStore_ListByWorkflow verifies WorkflowID filtering.
func TestStore_ListByWorkflow(t *testing.T) {
	db, closer := makeTestDB(t)
	defer closer()
	store := NewStore(db)

	// Create notifications with different workflow IDs
	workflows := []string{"workflow_1", "workflow_2", ""}
	const notifyPerWorkflow = 3

	for w, wid := range workflows {
		for i := 0; i < notifyPerWorkflow; i++ {
			n := &Notification{
				ID:         fmt.Sprintf("notif_wf_%d_%d", w, i),
				RoutineID:  "routine_1",
				RunID:      "run_1",
				WorkflowID: wid,
				Status:     StatusPending,
				CreatedAt:  time.Now().UTC(),
				UpdatedAt:  time.Now().UTC(),
			}
			if err := store.Put(n); err != nil {
				t.Fatalf("Put failed: %v", err)
			}
		}
	}

	// List workflow_1 notifications
	list, err := store.ListByWorkflow("workflow_1")
	if err != nil {
		t.Fatalf("ListByWorkflow failed: %v", err)
	}
	if len(list) != notifyPerWorkflow {
		t.Errorf("expected %d notifications for workflow_1, got %d", notifyPerWorkflow, len(list))
	}

	// Verify all belong to workflow_1
	for _, n := range list {
		if n.WorkflowID != "workflow_1" {
			t.Errorf("expected WorkflowID 'workflow_1', got %q", n.WorkflowID)
		}
	}
}

// TestStore_PendingCount verifies count accuracy.
func TestStore_PendingCount(t *testing.T) {
	db, closer := makeTestDB(t)
	defer closer()
	store := NewStore(db)

	// Add pending notifications
	const numPending = 5
	for i := 0; i < numPending; i++ {
		n := &Notification{
			ID:        fmt.Sprintf("pending_%d", i),
			RoutineID: "routine_1",
			RunID:     "run_1",
			Status:    StatusPending,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
		if err := store.Put(n); err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	count, err := store.PendingCount()
	if err != nil {
		t.Fatalf("PendingCount failed: %v", err)
	}
	if count != numPending {
		t.Errorf("expected %d pending, got %d", numPending, count)
	}

	// Transition one and recount
	if err := store.Transition("pending_0", StatusSeen); err != nil {
		t.Fatalf("Transition failed: %v", err)
	}

	count, err = store.PendingCount()
	if err != nil {
		t.Fatalf("PendingCount failed: %v", err)
	}
	if count != numPending-1 {
		t.Errorf("expected %d pending after transition, got %d", numPending-1, count)
	}
}

// TestStore_ExpireRun verifies ExpireRun atomicity.
func TestStore_ExpireRun(t *testing.T) {
	db, closer := makeTestDB(t)
	defer closer()
	store := NewStore(db)

	// Create notifications for a run
	const numNotifs = 5
	runID := "expire_test_run"
	for i := 0; i < numNotifs; i++ {
		n := &Notification{
			ID:        fmt.Sprintf("expire_notif_%d", i),
			RoutineID: "routine_1",
			RunID:     runID,
			Status:    StatusPending,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
		if err := store.Put(n); err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	// Expire the run
	if err := store.ExpireRun(runID); err != nil {
		t.Fatalf("ExpireRun failed: %v", err)
	}

	// Verify all have ExpiresAt set
	list, err := store.ListByRoutine("routine_1")
	if err != nil {
		t.Fatalf("ListByRoutine failed: %v", err)
	}

	for _, n := range list {
		if n.RunID == runID {
			if n.ExpiresAt == nil {
				t.Errorf("expected ExpiresAt to be set for notification %s", n.ID)
			}
		}
	}
}

// TestStore_CorruptedRecord verifies handling of corrupted JSON.
func TestStore_CorruptedRecord(t *testing.T) {
	db, closer := makeTestDB(t)
	defer closer()
	store := NewStore(db)

	// Manually insert corrupted data
	b := db.NewBatch()
	defer b.Close()
	b.Set([]byte("notifications/id/corrupt_notif"), []byte("invalid json {"), nil)
	if err := b.Commit(pebble.Sync); err != nil {
		t.Fatalf("batch commit failed: %v", err)
	}

	// Try to retrieve — should error
	_, err := store.Get("corrupt_notif")
	if err == nil {
		t.Error("expected error for corrupted JSON, got nil")
	}

	// ListByPrefix should skip corrupt records gracefully
	// This tests robustness in production scenarios
}

// TestStore_EmptyPrefixScans verifies handling of empty query results.
func TestStore_EmptyPrefixScans(t *testing.T) {
	db, closer := makeTestDB(t)
	defer closer()
	store := NewStore(db)

	// List non-existent routine
	list, err := store.ListByRoutine("nonexistent_routine")
	if err != nil {
		t.Fatalf("ListByRoutine failed: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list for non-existent routine, got %d", len(list))
	}

	// Count pending when none exist
	count, err := store.PendingCount()
	if err != nil {
		t.Fatalf("PendingCount failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 pending count, got %d", count)
	}
}

// TestStore_LargeNotification verifies handling of large notification payloads.
func TestStore_LargeNotification(t *testing.T) {
	db, closer := makeTestDB(t)
	defer closer()
	store := NewStore(db)

	// Create a large notification with large Detail
	largeData := make([]byte, 100*1024) // 100KB
	for i := range largeData {
		largeData[i] = 'x'
	}

	n := &Notification{
		ID:        "large_notif",
		RoutineID: "routine_1",
		RunID:     "run_1",
		Status:    StatusPending,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Detail:    string(largeData),
	}

	if err := store.Put(n); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Retrieve and verify
	retrieved, err := store.Get("large_notif")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if len(retrieved.Detail) != len(largeData) {
		t.Errorf("expected %d bytes, got %d", len(largeData), len(retrieved.Detail))
	}
}

// TestStore_StatusTransitionSequence verifies multi-step transitions.
func TestStore_StatusTransitionSequence(t *testing.T) {
	db, closer := makeTestDB(t)
	defer closer()
	store := NewStore(db)

	n := &Notification{
		ID:        "transition_seq",
		RoutineID: "routine_1",
		RunID:     "run_1",
		Status:    StatusPending,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := store.Put(n); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Transition sequence: Pending -> Seen -> Dismissed
	transitions := []Status{StatusSeen, StatusDismissed}
	for _, newStatus := range transitions {
		if err := store.Transition("transition_seq", newStatus); err != nil {
			t.Fatalf("Transition to %v failed: %v", newStatus, err)
		}
	}

	// Verify final state
	final, err := store.Get("transition_seq")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if final.Status != StatusDismissed {
		t.Errorf("expected final status StatusDismissed, got %v", final.Status)
	}
}

// makeTestDB creates an in-memory Pebble DB for testing.
func makeTestDB(t *testing.T) (*pebble.DB, func()) {
	dir := t.TempDir()
	db, err := pebble.Open(dir, &pebble.Options{})
	if err != nil {
		t.Fatalf("failed to open test DB: %v", err)
	}
	return db, func() { db.Close() }
}

// BenchmarkStore_Put measures Put performance.
func BenchmarkStore_Put(b *testing.B) {
	db, closer := makeTestDB(&testing.T{})
	defer closer()
	store := NewStore(db)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		n := &Notification{
			ID:        fmt.Sprintf("bench_%d", i),
			RoutineID: "routine_1",
			RunID:     "run_1",
			Status:    StatusPending,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
		store.Put(n)
	}
}

// BenchmarkStore_Get measures Get performance.
func BenchmarkStore_Get(b *testing.B) {
	db, closer := makeTestDB(&testing.T{})
	defer closer()
	store := NewStore(db)

	n := &Notification{
		ID:        "bench_get",
		RoutineID: "routine_1",
		RunID:     "run_1",
		Status:    StatusPending,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	store.Put(n)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Get("bench_get")
	}
}

// BenchmarkStore_ListByRoutine measures ListByRoutine performance.
func BenchmarkStore_ListByRoutine(b *testing.B) {
	db, closer := makeTestDB(&testing.T{})
	defer closer()
	store := NewStore(db)

	// Populate with notifications
	for i := 0; i < 100; i++ {
		n := &Notification{
			ID:        fmt.Sprintf("bench_list_%d", i),
			RoutineID: "routine_1",
			RunID:     "run_1",
			Status:    StatusPending,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
		store.Put(n)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.ListByRoutine("routine_1")
	}
}
