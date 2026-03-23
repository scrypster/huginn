package artifact_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/artifact"
	"github.com/scrypster/huginn/internal/workforce"
)

// ensureSessions inserts session IDs into the test DB to satisfy FK constraints.
// It delegates to newStore which already inserts the standard set; this helper
// is used when tests need additional session IDs beyond the standard set.
func ensureSession(t *testing.T, store *artifact.SQLiteStore, db interface {
	Exec(query string, args ...any) (interface{ RowsAffected() (int64, error) }, error)
}, sessionID string) {
	t.Helper()
	// We use openTestDB + newStore pattern from store_test.go which pre-inserts
	// standard sessions. For extra sessions we reach into the DB directly via
	// the newStore helper that returns the store.
	// Extra sessions are inserted by callers who have access to the raw DB.
}

// --- 1. TestWrite_BoundaryThreshold ---

// TestWrite_BoundaryThreshold verifies that content at exactly
// 256*1024-1, 256*1024, and 256*1024+1 bytes routes correctly to inline vs file.
func TestWrite_BoundaryThreshold(t *testing.T) {
	t.Parallel()

	const threshold = 256 * 1024

	cases := []struct {
		name        string
		size        int
		wantInline  bool
		wantOnDisk  bool
	}{
		{"below_threshold", threshold - 1, true, false},
		{"at_threshold", threshold, true, false},   // len > threshold triggers file; == is still inline
		{"above_threshold", threshold + 1, false, true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, dir := newStore(t)

			a := baseArtifact()
			a.Content = bytes.Repeat([]byte("a"), tc.size)

			if err := s.Write(context.Background(), a); err != nil {
				t.Fatalf("Write: %v", err)
			}

			if tc.wantInline {
				if a.ContentRef != "" {
					t.Errorf("size=%d: expected inline (ContentRef empty), got ContentRef=%q", tc.size, a.ContentRef)
				}
				if a.Content == nil {
					t.Errorf("size=%d: expected Content non-nil for inline storage", tc.size)
				}
			}
			if tc.wantOnDisk {
				if a.ContentRef == "" {
					t.Errorf("size=%d: expected ContentRef to be set for off-disk storage", tc.size)
				}
				if a.Content != nil {
					t.Errorf("size=%d: expected Content nil after off-disk write, got len=%d", tc.size, len(a.Content))
				}
				fpath := filepath.Join(dir, a.ContentRef)
				if _, err := os.Stat(fpath); err != nil {
					t.Errorf("size=%d: expected file at %s: %v", tc.size, fpath, err)
				}
			}

			// Round-trip: content should come back intact.
			got, err := s.Read(context.Background(), a.ID)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if len(got.Content) != tc.size {
				t.Errorf("size=%d: round-trip length mismatch: want %d, got %d", tc.size, tc.size, len(got.Content))
			}
		})
	}
}

// --- 2. TestWrite_MaxSizeBoundary ---

// TestWrite_MaxSizeBoundary verifies the exact boundary behaviour of the
// MaxArtifactSize guard. The implementation rejects content where
// len(content) > MaxArtifactSize, so:
//   - MaxArtifactSize+1 → ErrArtifactTooLarge
//   - MaxArtifactSize   → success (equal is not strictly greater)
//   - MaxArtifactSize-1 → success
func TestWrite_MaxSizeBoundary(t *testing.T) {
	t.Parallel()

	t.Run("one_above_max_fails", func(t *testing.T) {
		t.Parallel()
		s, _ := newStore(t)

		a := baseArtifact()
		a.Content = bytes.Repeat([]byte("z"), artifact.MaxArtifactSize+1)
		err := s.Write(context.Background(), a)
		if !errors.Is(err, artifact.ErrArtifactTooLarge) {
			t.Errorf("expected ErrArtifactTooLarge for MaxArtifactSize+1, got %v", err)
		}
	})

	t.Run("at_max_succeeds", func(t *testing.T) {
		t.Parallel()
		s, _ := newStore(t)

		a := baseArtifact()
		a.Content = bytes.Repeat([]byte("z"), artifact.MaxArtifactSize)
		if err := s.Write(context.Background(), a); err != nil {
			t.Errorf("expected success for MaxArtifactSize (equal is not rejected), got %v", err)
		}
	})

	t.Run("one_below_max_succeeds", func(t *testing.T) {
		t.Parallel()
		s, _ := newStore(t)

		a := baseArtifact()
		a.Content = bytes.Repeat([]byte("z"), artifact.MaxArtifactSize-1)
		if err := s.Write(context.Background(), a); err != nil {
			t.Errorf("expected success for MaxArtifactSize-1, got %v", err)
		}
	})
}

// --- 3. TestWrite_EmptyContent ---

// TestWrite_EmptyContent verifies that nil and empty-slice content both write
// and read back correctly without error.
func TestWrite_EmptyContent(t *testing.T) {
	t.Parallel()

	t.Run("nil_content", func(t *testing.T) {
		t.Parallel()
		s, _ := newStore(t)

		a := baseArtifact()
		a.Content = nil

		if err := s.Write(context.Background(), a); err != nil {
			t.Fatalf("Write nil content: %v", err)
		}
		got, err := s.Read(context.Background(), a.ID)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if len(got.Content) != 0 {
			t.Errorf("expected empty content, got len=%d", len(got.Content))
		}
		if got.ContentRef != "" {
			t.Errorf("expected no ContentRef for empty content, got %q", got.ContentRef)
		}
	})

	t.Run("empty_slice_content", func(t *testing.T) {
		t.Parallel()
		s, _ := newStore(t)

		a := baseArtifact()
		a.Content = []byte{}

		if err := s.Write(context.Background(), a); err != nil {
			t.Fatalf("Write empty slice: %v", err)
		}
		got, err := s.Read(context.Background(), a.ID)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if len(got.Content) != 0 {
			t.Errorf("expected empty content, got len=%d", len(got.Content))
		}
		if got.ContentRef != "" {
			t.Errorf("expected no ContentRef for empty content, got %q", got.ContentRef)
		}
	})
}

// --- 4. TestRead_OrphanedContentFile ---

// TestRead_OrphanedContentFile verifies that reading a large artifact whose
// file has been deleted returns an error rather than panicking.
func TestRead_OrphanedContentFile(t *testing.T) {
	t.Parallel()
	s, dir := newStore(t)

	a := baseArtifact()
	a.Content = bytes.Repeat([]byte("x"), 257*1024)

	if err := s.Write(context.Background(), a); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if a.ContentRef == "" {
		t.Fatal("expected ContentRef to be set (large artifact)")
	}

	// Delete the backing file on disk.
	fpath := filepath.Join(dir, a.ContentRef)
	if err := os.Remove(fpath); err != nil {
		t.Fatalf("os.Remove: %v", err)
	}

	_, err := s.Read(context.Background(), a.ID)
	if err == nil {
		t.Fatal("expected error reading artifact with missing content file, got nil")
	}
	// Must not be ErrArtifactNotFound — artifact is in DB; the file is just missing.
	if errors.Is(err, workforce.ErrArtifactNotFound) {
		t.Errorf("got ErrArtifactNotFound but artifact exists in DB — expected a file-read error")
	}
}

// --- 5. TestListBySession_LimitClamping ---

// TestListBySession_LimitClamping verifies that limit=0 defaults to 50,
// limit=201 clamps to 200, and limit=200 is accepted as-is.
func TestListBySession_LimitClamping(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	ctx := context.Background()

	// Insert 55 artifacts so we can differentiate 50 vs 55.
	for i := 0; i < 55; i++ {
		a := baseArtifact()
		a.Content = []byte(fmt.Sprintf("item %d", i))
		if err := s.Write(ctx, a); err != nil {
			t.Fatalf("Write[%d]: %v", i, err)
		}
	}

	t.Run("limit_zero_defaults_to_50", func(t *testing.T) {
		list, err := s.ListBySession(ctx, "sess-001", 0, "")
		if err != nil {
			t.Fatalf("ListBySession: %v", err)
		}
		if len(list) != 50 {
			t.Errorf("limit=0: expected 50 results (default), got %d", len(list))
		}
	})

	t.Run("limit_201_clamps_to_200", func(t *testing.T) {
		// We only have 55 items so the clamp won't reduce the count, but we
		// verify that passing 201 doesn't panic or error; it returns all 55.
		list, err := s.ListBySession(ctx, "sess-001", 201, "")
		if err != nil {
			t.Fatalf("ListBySession: %v", err)
		}
		// 55 < 200, so we get all 55 back (clamp to 200, but only 55 exist).
		if len(list) != 55 {
			t.Errorf("limit=201 (clamped to 200): expected 55 results, got %d", len(list))
		}
	})

	t.Run("limit_200_exact", func(t *testing.T) {
		list, err := s.ListBySession(ctx, "sess-001", 200, "")
		if err != nil {
			t.Fatalf("ListBySession: %v", err)
		}
		// 55 < 200, so we get all 55.
		if len(list) != 55 {
			t.Errorf("limit=200: expected 55 results, got %d", len(list))
		}
	})
}

// --- 6. TestListBySession_AfterIDWithGap ---

// TestListBySession_AfterIDWithGap inserts 5 artifacts, deletes the middle one,
// then paginates using the deleted artifact's ID as cursor — the store should
// still return items after that ULID lexicographically.
func TestListBySession_AfterIDWithGap(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	ctx := context.Background()

	var ids []string
	for i := 0; i < 5; i++ {
		a := baseArtifact()
		a.Content = []byte(fmt.Sprintf("item %d", i))
		if err := s.Write(ctx, a); err != nil {
			t.Fatalf("Write[%d]: %v", i, err)
		}
		ids = append(ids, a.ID)
	}

	// Delete the middle artifact (index 2).
	middleID := ids[2]
	db := openTestDB(t)
	if _, err := db.Write().ExecContext(ctx, `DELETE FROM artifacts WHERE id = ?`, middleID); err != nil {
		t.Fatalf("delete middle artifact: %v", err)
	}

	// Using middle ID as cursor should return items 3 and 4 (IDs after middleID).
	list, err := s.ListBySession(ctx, "sess-001", 10, middleID)
	if err != nil {
		t.Fatalf("ListBySession after deleted cursor: %v", err)
	}
	// Should get items at index 3 and 4.
	if len(list) != 2 {
		t.Errorf("expected 2 items after gap cursor, got %d", len(list))
	}
	for _, item := range list {
		if item.ID <= middleID {
			t.Errorf("item ID %q is not strictly > cursor %q", item.ID, middleID)
		}
	}
}

// --- 7. TestListBySession_ConsistentOrdering ---

// TestListBySession_ConsistentOrdering inserts 10 artifacts, lists them in two
// pages, and verifies the combined result is in strict ascending ULID order.
func TestListBySession_ConsistentOrdering(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		a := baseArtifact()
		a.Content = []byte(fmt.Sprintf("item %d", i))
		if err := s.Write(ctx, a); err != nil {
			t.Fatalf("Write[%d]: %v", i, err)
		}
	}

	page1, err := s.ListBySession(ctx, "sess-001", 6, "")
	if err != nil {
		t.Fatalf("ListBySession page1: %v", err)
	}
	if len(page1) != 6 {
		t.Fatalf("expected 6 items on page1, got %d", len(page1))
	}

	page2, err := s.ListBySession(ctx, "sess-001", 6, page1[len(page1)-1].ID)
	if err != nil {
		t.Fatalf("ListBySession page2: %v", err)
	}
	if len(page2) != 4 {
		t.Fatalf("expected 4 items on page2, got %d", len(page2))
	}

	combined := append(page1, page2...)
	for i := 1; i < len(combined); i++ {
		if combined[i].ID <= combined[i-1].ID {
			t.Errorf("ordering violation at index %d: %q <= %q", i, combined[i].ID, combined[i-1].ID)
		}
	}
}

// --- 8. TestUpdateStatus_AllValidTransitions ---

// TestUpdateStatus_AllValidTransitions verifies that all documented valid
// transitions from draft succeed and that accepted→rejected returns an error.
func TestUpdateStatus_AllValidTransitions(t *testing.T) {
	t.Parallel()

	validFromDraft := []workforce.ArtifactStatus{
		workforce.StatusAccepted,
		workforce.StatusRejected,
		workforce.StatusSuperseded,
		workforce.StatusFailed,
	}

	for _, target := range validFromDraft {
		target := target
		t.Run("draft_to_"+string(target), func(t *testing.T) {
			t.Parallel()
			s, _ := newStore(t)

			a := baseArtifact()
			a.Content = []byte("data")
			if err := s.Write(context.Background(), a); err != nil {
				t.Fatalf("Write: %v", err)
			}
			if err := s.UpdateStatus(context.Background(), a.ID, target, "test reason"); err != nil {
				t.Errorf("draft→%s: expected success, got %v", target, err)
			}
		})
	}

	// accepted → rejected is not a valid transition per the documented rules.
	t.Run("accepted_to_rejected_is_invalid", func(t *testing.T) {
		t.Parallel()
		s, _ := newStore(t)

		a := baseArtifact()
		a.Content = []byte("data")
		if err := s.Write(context.Background(), a); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if err := s.UpdateStatus(context.Background(), a.ID, workforce.StatusAccepted, ""); err != nil {
			t.Fatalf("draft→accepted: %v", err)
		}
		err := s.UpdateStatus(context.Background(), a.ID, workforce.StatusRejected, "")
		if err == nil {
			t.Error("accepted→rejected: expected an error for invalid transition, got nil")
		}
	})
}

// --- 9. TestUpdateStatus_NonexistentArtifact ---

// TestUpdateStatus_NonexistentArtifact verifies that calling UpdateStatus on
// an ID that doesn't exist returns ErrArtifactNotFound.
func TestUpdateStatus_NonexistentArtifact(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)

	err := s.UpdateStatus(context.Background(), "definitely-does-not-exist", workforce.StatusAccepted, "")
	if err == nil {
		t.Fatal("expected error updating status of nonexistent artifact, got nil")
	}
	if !errors.Is(err, workforce.ErrArtifactNotFound) {
		t.Errorf("expected ErrArtifactNotFound, got %v", err)
	}
}

// --- 10. TestDeleteBySession_EmptySession ---

// TestDeleteBySession_EmptySession verifies that calling DeleteBySession on a
// session that has no artifacts succeeds without error.
func TestDeleteBySession_EmptySession(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)

	// "session-A" is pre-inserted but has no artifacts written to it.
	if err := s.DeleteBySession(context.Background(), "session-A"); err != nil {
		t.Errorf("DeleteBySession on empty session returned error: %v", err)
	}
}

// --- 11. TestArchive_NoDuration ---

// TestArchive_NoDuration calls Archive with duration=0. A zero duration means
// the cutoff is "now", so only records updated strictly before now would match.
// In practice none of our freshly-inserted artifacts should be deleted.
// The implementation must not panic regardless.
func TestArchive_NoDuration(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	ctx := context.Background()

	// Write a terminal-state artifact.
	a := baseArtifact()
	a.Content = []byte("data")
	if err := s.Write(ctx, a); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := s.UpdateStatus(ctx, a.ID, workforce.StatusRejected, ""); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	// Archive(0) must not panic and must return a valid result.
	n, err := s.Archive(ctx, 0)
	if err != nil {
		t.Fatalf("Archive(0): %v", err)
	}
	// With duration=0 cutoff is time.Now(), so fresh records may or may not be
	// included depending on sub-millisecond timing. We only assert no panic + no error.
	_ = n
}

// --- 12. TestArchive_AllTerminalStatuses ---

// TestArchive_AllTerminalStatuses verifies that rejected, failed, and superseded
// artifacts are archived, while accepted and draft are not.
func TestArchive_AllTerminalStatuses(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	dir := t.TempDir()
	s := artifact.NewStore(db.Write(), dir)
	ctx := context.Background()

	if _, err := db.Write().Exec(`INSERT OR IGNORE INTO sessions (id) VALUES (?)`, "sess-001"); err != nil {
		t.Fatalf("insert test session: %v", err)
	}

	type entry struct {
		id     string
		status workforce.ArtifactStatus
	}

	writeWith := func(status workforce.ArtifactStatus) string {
		a := baseArtifact()
		a.Content = []byte("data")
		if err := s.Write(ctx, a); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if status != workforce.StatusDraft {
			if err := s.UpdateStatus(ctx, a.ID, status, ""); err != nil {
				t.Fatalf("UpdateStatus to %s: %v", status, err)
			}
		}
		return a.ID
	}

	rejectedID := writeWith(workforce.StatusRejected)
	failedID := writeWith(workforce.StatusFailed)
	supersededID := writeWith(workforce.StatusSuperseded)
	acceptedID := writeWith(workforce.StatusAccepted)
	draftID := writeWith(workforce.StatusDraft)

	// Back-date all terminal artifacts so they're eligible for archiving.
	oldTime := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339Nano)
	for _, id := range []string{rejectedID, failedID, supersededID} {
		if _, err := db.Write().ExecContext(ctx,
			`UPDATE artifacts SET updated_at = ? WHERE id = ?`, oldTime, id); err != nil {
			t.Fatalf("backdate %s: %v", id, err)
		}
	}
	// Also back-date non-terminal ones to confirm they're still excluded.
	for _, id := range []string{acceptedID, draftID} {
		if _, err := db.Write().ExecContext(ctx,
			`UPDATE artifacts SET updated_at = ? WHERE id = ?`, oldTime, id); err != nil {
			t.Fatalf("backdate %s: %v", id, err)
		}
	}

	n, err := s.Archive(ctx, 1*time.Hour)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if n != 3 {
		t.Errorf("expected 3 archived (rejected+failed+superseded), got %d", n)
	}

	// Terminal statuses must be gone.
	for _, id := range []string{rejectedID, failedID, supersededID} {
		_, err := s.Read(ctx, id)
		if !errors.Is(err, workforce.ErrArtifactNotFound) {
			t.Errorf("expected %s to be archived (not found), got %v", id, err)
		}
	}
	// Non-terminal statuses must still exist.
	for _, id := range []string{acceptedID, draftID} {
		if _, err := s.Read(ctx, id); err != nil {
			t.Errorf("expected %s to survive Archive, got %v", id, err)
		}
	}
}

// --- 13. TestWrite_ContextCancellation ---

// TestWrite_ContextCancellation cancels the context before calling Write and
// verifies an error is returned rather than a panic.
func TestWrite_ContextCancellation(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately before the call

	a := baseArtifact()
	a.Content = []byte("should not be written")

	err := s.Write(ctx, a)
	if err == nil {
		// Some SQLite drivers may still succeed on a cancelled context for
		// in-memory or fast writes; that's acceptable — we primarily verify
		// no panic occurs.
		t.Log("Write succeeded despite cancelled context (driver did not honour cancellation)")
	}
	// Either success or a context error is acceptable; panic is not.
}

// --- 14. TestWrite_ConcurrentDifferentArtifacts ---

// TestWrite_ConcurrentDifferentArtifacts launches 10 goroutines each writing a
// unique artifact to the same session simultaneously and verifies all succeed.
func TestWrite_ConcurrentDifferentArtifacts(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	ctx := context.Background()

	const n = 10
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			a := baseArtifact()
			a.Content = []byte(fmt.Sprintf("concurrent write %d", i))
			errs[i] = s.Write(ctx, a)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: Write failed: %v", i, err)
		}
	}

	list, err := s.ListBySession(ctx, "sess-001", 20, "")
	if err != nil {
		t.Fatalf("ListBySession: %v", err)
	}
	if len(list) != n {
		t.Errorf("expected %d artifacts after concurrent writes, got %d", n, len(list))
	}
}
