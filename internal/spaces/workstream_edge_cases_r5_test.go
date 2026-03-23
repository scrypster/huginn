package spaces_test

// workstream_edge_cases_r5_test.go — edge-case tests for the WorkstreamStore.

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// TestWorkstream_Create_NameWithSQLInjection verifies that a workstream name
// containing SQL injection syntax is stored and retrieved safely.
func TestWorkstream_Create_NameWithSQLInjection(t *testing.T) {
	t.Parallel()
	store := openWS(t)
	ctx := context.Background()

	injectionName := `'; DROP TABLE workstreams; --`

	ws, err := store.Create(ctx, injectionName, "test description")
	if err != nil {
		t.Fatalf("Create with SQL injection name: %v", err)
	}

	got, err := store.Get(ctx, ws.ID)
	if err != nil {
		t.Fatalf("Get after SQL injection create: %v", err)
	}
	if got.Name != injectionName {
		t.Errorf("name roundtrip mismatch: want %q, got %q", injectionName, got.Name)
	}

	// Verify the workstreams table still exists and is queryable.
	list, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List after SQL injection name: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 workstream, got %d (table may have been dropped)", len(list))
	}
}

// TestWorkstream_Create_MaxLengthName verifies that a 1000-char name stores and retrieves
// without truncation.
func TestWorkstream_Create_MaxLengthName(t *testing.T) {
	t.Parallel()
	store := openWS(t)
	ctx := context.Background()

	longName := ""
	for i := 0; i < 1000; i++ {
		longName += "x"
	}

	ws, err := store.Create(ctx, longName, "")
	if err != nil {
		t.Fatalf("Create with 1000-char name: %v", err)
	}

	got, err := store.Get(ctx, ws.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Name) != 1000 {
		t.Errorf("name length: want 1000, got %d", len(got.Name))
	}
	if got.Name != longName {
		t.Error("name content mismatch after roundtrip")
	}
}

// TestWorkstream_TagSession_MaxSessions verifies that tagging 100 sessions to one workstream
// and calling ListSessions returns all 100.
func TestWorkstream_TagSession_MaxSessions(t *testing.T) {
	t.Parallel()
	store := openWS(t)
	ctx := context.Background()

	ws, err := store.Create(ctx, "high-volume-project", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	const n = 100
	for i := 0; i < n; i++ {
		sessID := fmt.Sprintf("sess-%04d", i)
		if err := store.TagSession(ctx, ws.ID, sessID); err != nil {
			t.Fatalf("TagSession[%d]: %v", i, err)
		}
	}

	ids, err := store.ListSessions(ctx, ws.ID)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(ids) != n {
		t.Errorf("expected %d sessions, got %d", n, len(ids))
	}
}

// TestWorkstream_Concurrent_CreateAndList verifies that 5 goroutines creating
// and 5 goroutines listing concurrently does not produce deadlocks or errors.
func TestWorkstream_Concurrent_CreateAndList(t *testing.T) {
	t.Parallel()
	store := openWS(t)
	ctx := context.Background()

	const writers = 5
	const readers = 5

	var wg sync.WaitGroup
	errs := make(chan error, writers+readers)

	// Writers.
	wg.Add(writers)
	for i := 0; i < writers; i++ {
		i := i
		go func() {
			defer wg.Done()
			_, err := store.Create(ctx, fmt.Sprintf("concurrent-ws-%d", i), "")
			if err != nil {
				errs <- fmt.Errorf("Create[%d]: %w", i, err)
			}
		}()
	}

	// Readers.
	wg.Add(readers)
	for i := 0; i < readers; i++ {
		i := i
		go func() {
			defer wg.Done()
			_, err := store.List(ctx)
			if err != nil {
				errs <- fmt.Errorf("List[%d]: %w", i, err)
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent operation error: %v", err)
	}
}

// TestWorkstream_Delete_AlreadyDeletedWorkstream_ReturnsError verifies that deleting
// a workstream twice returns an error on the second attempt.
func TestWorkstream_Delete_AlreadyDeletedWorkstream_ReturnsError(t *testing.T) {
	t.Parallel()
	store := openWS(t)
	ctx := context.Background()

	ws, err := store.Create(ctx, "double-delete", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := store.Delete(ctx, ws.ID); err != nil {
		t.Fatalf("first Delete: %v", err)
	}

	// Second delete must return an error (workstream no longer exists).
	if err := store.Delete(ctx, ws.ID); err == nil {
		t.Fatal("expected error on second Delete of already-deleted workstream, got nil")
	}
}

// TestWorkstream_ListSessions_AfterPartialDelete verifies that after deleting a workstream
// and re-creating one with the same name (but a new ID), old session IDs from the deleted
// workstream do not appear in the new workstream's session list.
func TestWorkstream_ListSessions_AfterPartialDelete(t *testing.T) {
	t.Parallel()
	store := openWS(t)
	ctx := context.Background()

	const wsName = "recycled-project"

	// Step 1: create a workstream and tag 5 sessions.
	ws1, err := store.Create(ctx, wsName, "")
	if err != nil {
		t.Fatalf("Create ws1: %v", err)
	}
	oldSessions := []string{"old-1", "old-2", "old-3", "old-4", "old-5"}
	for _, sessID := range oldSessions {
		if err := store.TagSession(ctx, ws1.ID, sessID); err != nil {
			t.Fatalf("TagSession %q to ws1: %v", sessID, err)
		}
	}

	// Step 2: delete the workstream (cascades session tags).
	if err := store.Delete(ctx, ws1.ID); err != nil {
		t.Fatalf("Delete ws1: %v", err)
	}

	// Step 3: re-create a workstream with the same name (new ID).
	ws2, err := store.Create(ctx, wsName, "")
	if err != nil {
		t.Fatalf("Create ws2 (recycled name): %v", err)
	}
	if ws2.ID == ws1.ID {
		t.Fatal("re-created workstream must have a different ID")
	}

	// Step 4: tag 3 new sessions to the new workstream.
	newSessions := []string{"new-A", "new-B", "new-C"}
	for _, sessID := range newSessions {
		if err := store.TagSession(ctx, ws2.ID, sessID); err != nil {
			t.Fatalf("TagSession %q to ws2: %v", sessID, err)
		}
	}

	// Step 5: ListSessions should return exactly the 3 new sessions.
	ids, err := store.ListSessions(ctx, ws2.ID)
	if err != nil {
		t.Fatalf("ListSessions ws2: %v", err)
	}
	if len(ids) != 3 {
		t.Errorf("expected 3 sessions in ws2, got %d: %v", len(ids), ids)
	}

	// Verify none of the old session IDs appear.
	oldSet := make(map[string]bool)
	for _, id := range oldSessions {
		oldSet[id] = true
	}
	for _, id := range ids {
		if oldSet[id] {
			t.Errorf("old session ID %q should not appear in re-created workstream's sessions", id)
		}
	}
}
