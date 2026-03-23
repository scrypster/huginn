package storage

import (
	"sync"
	"testing"
	"time"
)

// TestCorruption_WriteAndReadBack verifies basic write/read round-trip for all record types.
func TestCorruption_WriteAndReadBack(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)

	// Write a FileRecord.
	rec := FileRecord{
		Path:          "/project/main.go",
		Hash:          "abc123def456",
		ParserVersion: 2,
		IndexedAt:     time.Now().UTC().Truncate(time.Second),
	}
	if err := s.SetFileRecord(rec); err != nil {
		t.Fatalf("SetFileRecord: %v", err)
	}

	got := s.GetFileRecord("/project/main.go")
	if got.Hash != rec.Hash {
		t.Errorf("Hash: got %q, want %q", got.Hash, rec.Hash)
	}
	if got.ParserVersion != rec.ParserVersion {
		t.Errorf("ParserVersion: got %d, want %d", got.ParserVersion, rec.ParserVersion)
	}
}

// TestCorruption_ClosedStore_ReturnsError verifies that operations on a closed store
// return errors rather than panicking.
func TestCorruption_ClosedStore_ReturnsError(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)

	// Close the store explicitly.
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// SetGitHead on a closed store should return an error, not panic.
	err := s.SetGitHead("deadbeef")
	if err == nil {
		t.Error("expected error on SetGitHead after close, got nil")
	}

	// GetGitHead should return empty string (not panic).
	val := s.GetGitHead()
	if val != "" {
		t.Errorf("expected empty string from GetGitHead on closed store, got %q", val)
	}
}

// TestCorruption_NilDB_DoesNotPanic verifies that a Store with nil db does not panic on reads.
func TestCorruption_NilDB_DoesNotPanic(t *testing.T) {
	t.Parallel()
	s := &Store{db: nil}

	// None of these should panic.
	val := s.GetGitHead()
	_ = val

	hash := s.GetFileHash("/some/path")
	_ = hash

	symbols := s.GetSymbols("/some/path")
	_ = symbols

	chunks := s.GetChunks("/some/path")
	_ = chunks

	rec := s.GetFileRecord("/some/path")
	_ = rec

	_, _ = s.GetWorkspaceSummary()

	edges := s.GetEdgesFrom("/some/path")
	_ = edges

	edges2 := s.GetEdgesTo("/some/path")
	_ = edges2
}

// TestCorruption_SafeGet_NilDB_ReturnsError verifies that safeGet handles a nil DB
// by recovering and returning an error rather than panicking.
func TestCorruption_SafeGet_NilDB_ReturnsError(t *testing.T) {
	t.Parallel()
	// safeGet is package-private; test via the exported Store methods on a nil-db store.
	s := &Store{db: nil}
	// GetFileHash calls safeGet internally with a nil db — should not panic.
	result := s.GetFileHash("any-key")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

// TestCorruption_ConcurrentReadWrite verifies no data races under concurrent read/write.
// Run with: go test -race ./internal/storage/...
func TestCorruption_ConcurrentReadWrite(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)

	const goroutines = 10
	var wg sync.WaitGroup

	// Writers.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			sha := "sha" + string(rune('a'+n))
			_ = s.SetGitHead(sha)
			_ = s.SetFileRecord(FileRecord{
				Path:  "/file.go",
				Hash:  sha,
				IndexedAt: time.Now(),
			})
		}(i)
	}

	// Readers.
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.GetGitHead()
			_ = s.GetFileHash("/file.go")
			_ = s.GetFileRecord("/file.go")
		}()
	}

	wg.Wait()
}

// TestCorruption_DoubleClose_NoError verifies that closing a store twice is safe.
func TestCorruption_DoubleClose_NoError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	// Second close should not panic or return an error.
	if err := s.Close(); err != nil {
		t.Errorf("second Close should be a no-op, got: %v", err)
	}
}

// TestCorruption_DB_NilAfterClose verifies that DB() returns nil after the store is closed.
func TestCorruption_DB_NilAfterClose(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if s.DB() != nil {
		t.Error("expected DB() to return nil after close")
	}
}
