package storage

import (
	"testing"
	"time"
)

// TestMigration_OpenTwice_DataPreserved verifies that opening a store at an existing
// path on disk does not destroy existing data.
func TestMigration_OpenTwice_DataPreserved(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// First open: write some data.
	s1, err := Open(dir)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if err := s1.SetGitHead("abc123"); err != nil {
		t.Fatalf("SetGitHead: %v", err)
	}
	rec := FileRecord{
		Path:          "/src/main.go",
		Hash:          "deadbeef",
		ParserVersion: 3,
		IndexedAt:     time.Now().UTC().Truncate(time.Second),
	}
	if err := s1.SetFileRecord(rec); err != nil {
		t.Fatalf("SetFileRecord: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	// Second open: data should still be there.
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	t.Cleanup(func() { _ = s2.Close() })

	head := s2.GetGitHead()
	if head != "abc123" {
		t.Errorf("expected git head=abc123 after reopen, got %q", head)
	}
	got := s2.GetFileRecord("/src/main.go")
	if got.Hash != "deadbeef" {
		t.Errorf("expected file hash=deadbeef after reopen, got %q", got.Hash)
	}
	if got.ParserVersion != 3 {
		t.Errorf("expected parser version=3 after reopen, got %d", got.ParserVersion)
	}
}

// TestMigration_OpenIdempotent_NoError verifies that opening the same directory
// multiple times (sequentially) does not return an error.
func TestMigration_OpenIdempotent_NoError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	for i := 0; i < 3; i++ {
		s, err := Open(dir)
		if err != nil {
			t.Fatalf("Open attempt %d: %v", i+1, err)
		}
		if err := s.Close(); err != nil {
			t.Fatalf("Close attempt %d: %v", i+1, err)
		}
	}
}

// TestMigration_Invalidate_ThenReopen verifies that invalidated records are
// absent after reopening the store.
func TestMigration_Invalidate_ThenReopen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	s1, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s1.SetFileRecord(FileRecord{Path: "/app/x.ts", Hash: "hash1"}); err != nil {
		t.Fatalf("SetFileRecord: %v", err)
	}
	// Verify it was written.
	if s1.GetFileHash("/app/x.ts") == "" {
		t.Fatal("expected hash to be set before invalidation")
	}

	// Invalidate the path.
	if err := s1.Invalidate([]string{"/app/x.ts"}); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}
	// Hash should be gone.
	if s1.GetFileHash("/app/x.ts") != "" {
		t.Error("expected hash to be empty after invalidation")
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// After reopen, invalidated hash remains absent.
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	t.Cleanup(func() { _ = s2.Close() })

	if s2.GetFileHash("/app/x.ts") != "" {
		t.Error("expected hash to remain empty after reopen")
	}
}

// TestMigration_DeleteFileRecords_ThenReopen verifies deleted file records are absent after reopen.
func TestMigration_DeleteFileRecords_ThenReopen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	s1, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	path := "/project/util.go"
	if err := s1.SetFileRecord(FileRecord{
		Path:          path,
		Hash:          "cafebabe",
		ParserVersion: 1,
		IndexedAt:     time.Now(),
	}); err != nil {
		t.Fatalf("SetFileRecord: %v", err)
	}
	if err := s1.DeleteFileRecords(path); err != nil {
		t.Fatalf("DeleteFileRecords: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	t.Cleanup(func() { _ = s2.Close() })

	rec := s2.GetFileRecord(path)
	if rec.Hash != "" {
		t.Errorf("expected empty hash after delete+reopen, got %q", rec.Hash)
	}
}

// TestMigration_WorkspaceSummary_RoundTrip verifies workspace summary persists across reopen.
func TestMigration_WorkspaceSummary_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	s1, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ws := WorkspaceSummary{
		TopFilesByRefCount: []string{"/a.go", "/b.go"},
		CrossRepoHints:     []string{"hint1"},
		InferredRepoRoles:  map[string]string{"/a.go": "api"},
		UpdatedAt:          time.Now().UTC().Truncate(time.Second),
	}
	if err := s1.SetWorkspaceSummary(ws); err != nil {
		t.Fatalf("SetWorkspaceSummary: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	t.Cleanup(func() { _ = s2.Close() })

	got, ok := s2.GetWorkspaceSummary()
	if !ok {
		t.Fatal("expected workspace summary to be present after reopen")
	}
	if len(got.TopFilesByRefCount) != 2 {
		t.Errorf("expected 2 top files, got %d", len(got.TopFilesByRefCount))
	}
	if got.InferredRepoRoles["/a.go"] != "api" {
		t.Errorf("expected role=api for /a.go, got %q", got.InferredRepoRoles["/a.go"])
	}
}
