package artifact_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/artifact"
	"github.com/scrypster/huginn/internal/sqlitedb"
	"github.com/scrypster/huginn/internal/workforce"
)

// openTestDB opens an in-memory SQLite DB with the full schema applied.
func openTestDB(tb testing.TB) *sqlitedb.DB {
	tb.Helper()
	db, err := sqlitedb.Open(filepath.Join(tb.TempDir(), "test.db"))
	if err != nil {
		tb.Fatalf("sqlitedb.Open: %v", err)
	}
	if err := db.ApplySchema(); err != nil {
		tb.Fatalf("ApplySchema: %v", err)
	}
	tb.Cleanup(func() { db.Close() })
	return db
}

func newStore(tb testing.TB) (*artifact.SQLiteStore, string) {
	tb.Helper()
	db := openTestDB(tb)
	// Pre-insert test sessions used by baseArtifact and other helpers.
	for _, id := range []string{"sess-001", "session-A", "session-B", "sess-del"} {
		if _, err := db.Write().Exec(`INSERT OR IGNORE INTO sessions (id) VALUES (?)`, id); err != nil {
			tb.Fatalf("insert test session %s: %v", id, err)
		}
	}
	dir := tb.TempDir()
	return artifact.NewStore(db.Write(), dir), dir
}

func baseArtifact() *workforce.Artifact {
	return &workforce.Artifact{
		Kind:      workforce.KindDocument,
		Title:     "Test artifact",
		MimeType:  "text/markdown",
		AgentName: "test-agent",
		SessionID: "sess-001",
		Status:    workforce.StatusDraft,
	}
}

// TestWrite_SmallContent verifies that content <= 256 KB is stored inline.
func TestWrite_SmallContent(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)

	a := baseArtifact()
	a.Content = []byte("small content")

	if err := s.Write(context.Background(), a); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if a.ID == "" {
		t.Fatal("expected ID to be set")
	}
	if a.ContentRef != "" {
		t.Errorf("expected ContentRef to be empty for small content, got %q", a.ContentRef)
	}
}

// TestWrite_LargeContent verifies that content > 256 KB is written to disk.
func TestWrite_LargeContent(t *testing.T) {
	t.Parallel()
	s, dir := newStore(t)

	a := baseArtifact()
	a.Content = bytes.Repeat([]byte("x"), 257*1024)

	if err := s.Write(context.Background(), a); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if a.ContentRef == "" {
		t.Fatal("expected ContentRef to be set for large content")
	}
	if a.Content != nil {
		t.Error("expected Content to be nil after large-content write")
	}

	fpath := filepath.Join(dir, a.ContentRef)
	if _, err := os.Stat(fpath); err != nil {
		t.Errorf("expected file at %s, got: %v", fpath, err)
	}
}

// TestRead_SmallContent verifies round-trip for inline content.
func TestRead_SmallContent(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)

	a := baseArtifact()
	a.Content = []byte("hello world")

	if err := s.Write(context.Background(), a); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := s.Read(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(got.Content, a.Content) {
		t.Errorf("content mismatch: want %q, got %q", a.Content, got.Content)
	}
}

// TestRead_LargeContent verifies that large content is loaded from the file.
func TestRead_LargeContent(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)

	original := bytes.Repeat([]byte("y"), 257*1024)
	a := baseArtifact()
	a.Content = original

	if err := s.Write(context.Background(), a); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := s.Read(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(got.Content, original) {
		t.Errorf("large content mismatch: lengths %d vs %d", len(original), len(got.Content))
	}
}

// TestRead_NotFound verifies ErrArtifactNotFound is returned for unknown IDs.
func TestRead_NotFound(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)

	_, err := s.Read(context.Background(), "nonexistent-id")
	if !errors.Is(err, workforce.ErrArtifactNotFound) {
		t.Errorf("expected ErrArtifactNotFound, got %v", err)
	}
}

// TestListBySession verifies session filtering.
func TestListBySession(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)

	for i := 0; i < 2; i++ {
		a := baseArtifact()
		a.SessionID = "session-A"
		a.Content = []byte("data")
		if err := s.Write(context.Background(), a); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	b := baseArtifact()
	b.SessionID = "session-B"
	b.Content = []byte("other")
	if err := s.Write(context.Background(), b); err != nil {
		t.Fatalf("Write: %v", err)
	}

	list, err := s.ListBySession(context.Background(), "session-A", 0, "")
	if err != nil {
		t.Fatalf("ListBySession: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 artifacts for session-A, got %d", len(list))
	}
}

// TestListByAgent verifies agent + since filtering.
func TestListByAgent(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)

	for i := 0; i < 2; i++ {
		a := baseArtifact()
		a.AgentName = "Tom"
		a.Content = []byte("data")
		if err := s.Write(context.Background(), a); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	list, err := s.ListByAgent(context.Background(), "Tom", time.Now().Add(-1*time.Hour), 0, "")
	if err != nil {
		t.Fatalf("ListByAgent: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 artifacts for agent Tom, got %d", len(list))
	}
}

// TestUpdateStatus_DraftToAccepted verifies a valid draft→accepted transition.
func TestUpdateStatus_DraftToAccepted(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)

	a := baseArtifact()
	a.Content = []byte("patch")
	if err := s.Write(context.Background(), a); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := s.UpdateStatus(context.Background(), a.ID, workforce.StatusAccepted, ""); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	got, err := s.Read(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Status != workforce.StatusAccepted {
		t.Errorf("expected status accepted, got %s", got.Status)
	}
}

// TestUpdateStatus_DraftToRejected verifies rejection with a reason.
func TestUpdateStatus_DraftToRejected(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)

	a := baseArtifact()
	a.Content = []byte("draft")
	if err := s.Write(context.Background(), a); err != nil {
		t.Fatalf("Write: %v", err)
	}

	reason := "not what I asked for"
	if err := s.UpdateStatus(context.Background(), a.ID, workforce.StatusRejected, reason); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	got, err := s.Read(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got.Status != workforce.StatusRejected {
		t.Errorf("expected status rejected, got %s", got.Status)
	}
	if got.RejectionReason != reason {
		t.Errorf("expected rejection_reason %q, got %q", reason, got.RejectionReason)
	}
}

// TestSupersede verifies that oldID's status becomes superseded.
func TestSupersede(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)

	old := baseArtifact()
	old.Content = []byte("v1")
	if err := s.Write(context.Background(), old); err != nil {
		t.Fatalf("Write old: %v", err)
	}

	newA := baseArtifact()
	newA.Content = []byte("v2")
	if err := s.Write(context.Background(), newA); err != nil {
		t.Fatalf("Write new: %v", err)
	}

	if err := s.Supersede(context.Background(), old.ID, newA.ID); err != nil {
		t.Fatalf("Supersede: %v", err)
	}

	got, err := s.Read(context.Background(), old.ID)
	if err != nil {
		t.Fatalf("Read old: %v", err)
	}
	if got.Status != workforce.StatusSuperseded {
		t.Errorf("expected status superseded for old artifact, got %s", got.Status)
	}
}

// TestWrite_TooLarge verifies ErrArtifactTooLarge is returned for oversized content.
func TestWrite_TooLarge(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)

	a := baseArtifact()
	a.Content = bytes.Repeat([]byte("z"), artifact.MaxArtifactSize+1)

	err := s.Write(context.Background(), a)
	if !errors.Is(err, artifact.ErrArtifactTooLarge) {
		t.Errorf("expected ErrArtifactTooLarge, got %v", err)
	}
}

// TestUpdateStatus_Idempotent verifies that updating to the same status is a no-op.
func TestUpdateStatus_Idempotent(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)

	a := baseArtifact()
	a.Content = []byte("data")
	if err := s.Write(context.Background(), a); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// accepted → accepted should succeed.
	if err := s.UpdateStatus(context.Background(), a.ID, workforce.StatusAccepted, ""); err != nil {
		t.Fatalf("first UpdateStatus: %v", err)
	}
	if err := s.UpdateStatus(context.Background(), a.ID, workforce.StatusAccepted, ""); err != nil {
		t.Errorf("idempotent UpdateStatus returned error: %v", err)
	}
}

// TestDeleteBySession verifies session artifacts and files are removed.
func TestDeleteBySession(t *testing.T) {
	t.Parallel()
	s, dir := newStore(t)
	ctx := context.Background()

	a := baseArtifact()
	a.SessionID = "sess-del"
	a.Content = bytes.Repeat([]byte("x"), 257*1024) // large, will be on disk
	if err := s.Write(ctx, a); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := s.DeleteBySession(ctx, "sess-del"); err != nil {
		t.Fatalf("DeleteBySession: %v", err)
	}
	// Artifact should be gone from DB.
	_, err := s.Read(ctx, a.ID)
	if !errors.Is(err, workforce.ErrArtifactNotFound) {
		t.Errorf("expected ErrArtifactNotFound after delete, got %v", err)
	}
	// Session directory should be gone from disk.
	if _, err := os.Stat(filepath.Join(dir, "sess-del")); !os.IsNotExist(err) {
		t.Errorf("expected session dir to be removed, got: %v", err)
	}
}

// TestListBySession_Pagination verifies limit and afterID work correctly.
func TestListBySession_Pagination(t *testing.T) {
	t.Parallel()
	s, _ := newStore(t)
	ctx := context.Background()

	var ids []string
	for i := 0; i < 5; i++ {
		a := baseArtifact()
		a.Content = []byte("data")
		if err := s.Write(ctx, a); err != nil {
			t.Fatalf("Write: %v", err)
		}
		ids = append(ids, a.ID)
	}

	page1, err := s.ListBySession(ctx, "sess-001", 3, "")
	if err != nil {
		t.Fatalf("ListBySession page1: %v", err)
	}
	if len(page1) != 3 {
		t.Errorf("expected 3, got %d", len(page1))
	}

	page2, err := s.ListBySession(ctx, "sess-001", 3, page1[2].ID)
	if err != nil {
		t.Fatalf("ListBySession page2: %v", err)
	}
	if len(page2) != 2 {
		t.Errorf("expected 2 remaining, got %d", len(page2))
	}
}

// TestArchive verifies cleanup of terminal-state artifacts and preservation of drafts.
func TestArchive(t *testing.T) {
	t.Parallel()
	db := openTestDB(t)
	dir := t.TempDir()
	s := artifact.NewStore(db.Write(), dir)

	ctx := context.Background()
	// Insert the session referenced by baseArtifact (sess-001).
	if _, err := db.Write().Exec(`INSERT OR IGNORE INTO sessions (id) VALUES (?)`, "sess-001"); err != nil {
		t.Fatalf("insert test session: %v", err)
	}

	writeAndSetStatus := func(status workforce.ArtifactStatus) string {
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

	rejID := writeAndSetStatus(workforce.StatusRejected)
	failedID := writeAndSetStatus(workforce.StatusFailed)
	supersededID := writeAndSetStatus(workforce.StatusSuperseded)
	draftID := writeAndSetStatus(workforce.StatusDraft)

	// Back-date the terminal artifacts so they fall before the cutoff.
	oldTime := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339Nano)
	for _, id := range []string{rejID, failedID, supersededID} {
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
		t.Errorf("expected 3 artifacts archived, got %d", n)
	}

	// Draft should still exist.
	if _, err := s.Read(ctx, draftID); err != nil {
		t.Errorf("draft artifact should still exist, got: %v", err)
	}

	// Terminal artifacts should be gone.
	for _, id := range []string{rejID, failedID, supersededID} {
		_, err := s.Read(ctx, id)
		if !errors.Is(err, workforce.ErrArtifactNotFound) {
			t.Errorf("expected ErrArtifactNotFound for archived %s, got %v", id, err)
		}
	}
}
