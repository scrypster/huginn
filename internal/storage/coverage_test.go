package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Open — invalid path (MkdirAll failure via read-only parent)
// ---------------------------------------------------------------------------

func TestOpen_InvalidPath_PermissionDenied(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root — permission tests are unreliable")
	}
	// Create a read-only directory and try to open a store inside it.
	parent := t.TempDir()
	if err := os.Chmod(parent, 0400); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(parent, 0755) })

	_, err := Open(parent + "/subdir/store")
	if err == nil {
		t.Error("expected error opening store in read-only parent, got nil")
	}
}

// TestOpen_PebbleOpenFailure exercises the pebble.Open error branch by
// placing a regular file where the DB directory should go.
func TestOpen_PebbleOpenFailure(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "huginn.pebble")
	// A regular file where pebble expects a directory causes Open to fail.
	if err := os.WriteFile(dbPath, []byte("not a db"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := Open(dir)
	if err == nil {
		t.Error("expected error when pebble path is a file, got nil")
	}
}

// ---------------------------------------------------------------------------
// Close — idempotent
// ---------------------------------------------------------------------------

func TestClose_Idempotent(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close (idempotent): %v", err)
	}
}

// ---------------------------------------------------------------------------
// DB accessor
// ---------------------------------------------------------------------------

func TestDB_ReturnsUnderlying(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if s.DB() == nil {
		t.Error("DB() should return non-nil pebble.DB")
	}
}

// ---------------------------------------------------------------------------
// nil-db guards — write methods
// ---------------------------------------------------------------------------

func TestNilDB_WriteMethods(t *testing.T) {
	s := &Store{} // db == nil

	if err := s.SetGitHead("abc"); err == nil {
		t.Error("SetGitHead with nil db should error")
	}
	if err := s.SetFileRecord(FileRecord{Path: "f"}); err == nil {
		t.Error("SetFileRecord with nil db should error")
	}
	if err := s.SetSymbols("f", nil); err == nil {
		t.Error("SetSymbols with nil db should error")
	}
	if err := s.SetChunks("f", nil); err == nil {
		t.Error("SetChunks with nil db should error")
	}
	if err := s.SetEdge("a", "b", Edge{}); err == nil {
		t.Error("SetEdge with nil db should error")
	}
	if err := s.SetWorkspaceSummary(WorkspaceSummary{}); err == nil {
		t.Error("SetWorkspaceSummary with nil db should error")
	}
	if err := s.Invalidate([]string{"f"}); err == nil {
		t.Error("Invalidate with nil db should error")
	}
	if err := s.DeleteFileRecords("f"); err == nil {
		t.Error("DeleteFileRecords with nil db should error")
	}
}

// ---------------------------------------------------------------------------
// nil-db guards — read methods
// ---------------------------------------------------------------------------

func TestNilDB_ReadMethods(t *testing.T) {
	s := &Store{}

	if s.GetGitHead() != "" {
		t.Error("GetGitHead with nil db should return \"\"")
	}
	if s.GetFileHash("f") != "" {
		t.Error("GetFileHash with nil db should return \"\"")
	}
	rec := s.GetFileRecord("f")
	if rec.Hash != "" {
		t.Error("GetFileRecord with nil db should return zero FileRecord")
	}
	if syms := s.GetSymbols("f"); len(syms) != 0 {
		t.Error("GetSymbols with nil db should return empty slice")
	}
	if chunks := s.GetChunks("f"); len(chunks) != 0 {
		t.Error("GetChunks with nil db should return empty slice")
	}
	if edges := s.GetEdgesFrom("f"); len(edges) != 0 {
		t.Error("GetEdgesFrom with nil db should return empty slice")
	}
	if edges := s.GetEdgesTo("f"); len(edges) != 0 {
		t.Error("GetEdgesTo with nil db should return empty slice")
	}
	if edges := s.GetAllEdges(); edges != nil {
		t.Error("GetAllEdges with nil db should return nil")
	}
	if _, ok := s.GetWorkspaceSummary(); ok {
		t.Error("GetWorkspaceSummary with nil db should return false")
	}
}

// ---------------------------------------------------------------------------
// Closed-store error paths (previously unreachable — now caught by safeGet)
// ---------------------------------------------------------------------------

// openAndClose opens a real store, writes some data, then closes the DB
// so that subsequent read calls trigger pebble panics → safeGet catches them.
func openAndClose(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Write some data so keys exist.
	_ = s.SetGitHead("deadbeef")
	_ = s.SetSymbols("f.go", []Symbol{{Name: "Foo", Kind: "func"}})
	_ = s.SetChunks("f.go", []FileChunk{{Path: "f.go", Content: "x"}})
	_ = s.SetWorkspaceSummary(WorkspaceSummary{TopFilesByRefCount: []string{"a.go"}})
	_ = s.SetEdge("a.go", "b.go", Edge{From: "a.go", To: "b.go"})
	// Close the underlying pebble DB directly (bypassing s.Close's guard)
	// so s.db is non-nil but closed — triggers panics in pebble.
	if err := s.db.Close(); err != nil {
		t.Fatalf("db.Close: %v", err)
	}
	return s
}

func TestClosedStore_GetGitHead(t *testing.T) {
	s := openAndClose(t)
	// Must not panic; safeGet converts the panic to an error → log.Printf branch.
	result := s.GetGitHead()
	if result != "" {
		t.Errorf("expected empty string on error, got %q", result)
	}
}

func TestClosedStore_GetFileHash(t *testing.T) {
	s := openAndClose(t)
	result := s.GetFileHash("f.go")
	if result != "" {
		t.Errorf("expected empty string on error, got %q", result)
	}
}

func TestClosedStore_GetSymbols(t *testing.T) {
	s := openAndClose(t)
	syms := s.GetSymbols("f.go")
	if len(syms) != 0 {
		t.Errorf("expected empty symbols on error, got %v", syms)
	}
}

func TestClosedStore_GetChunks(t *testing.T) {
	s := openAndClose(t)
	chunks := s.GetChunks("f.go")
	if len(chunks) != 0 {
		t.Errorf("expected empty chunks on error, got %v", chunks)
	}
}

func TestClosedStore_GetWorkspaceSummary(t *testing.T) {
	s := openAndClose(t)
	_, ok := s.GetWorkspaceSummary()
	if ok {
		t.Error("expected false on error, got true")
	}
}

func TestClosedStore_GetEdgesFrom(t *testing.T) {
	s := openAndClose(t)
	edges := s.GetEdgesFrom("a.go")
	if len(edges) != 0 {
		t.Errorf("expected empty edges on error, got %v", edges)
	}
}

func TestClosedStore_GetEdgesTo(t *testing.T) {
	s := openAndClose(t)
	edges := s.GetEdgesTo("b.go")
	if len(edges) != 0 {
		t.Errorf("expected empty edges on error, got %v", edges)
	}
}

func TestClosedStore_GetAllEdges(t *testing.T) {
	s := openAndClose(t)
	edges := s.GetAllEdges()
	// GetAllEdges returns nil on error — just verify no panic.
	_ = edges
}

// ---------------------------------------------------------------------------
// Corrupt JSON in pebble store (unmarshal error branches)
// ---------------------------------------------------------------------------

func TestGetSymbols_CorruptJSON(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	// Write raw corrupt bytes directly.
	if err := s.db.Set(keyFileSymbols("bad.go"), []byte("not-json"), nil); err != nil {
		t.Fatalf("Set: %v", err)
	}
	syms := s.GetSymbols("bad.go")
	if len(syms) != 0 {
		t.Errorf("expected empty symbols on corrupt JSON, got %v", syms)
	}
}

func TestGetChunks_CorruptJSON(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.db.Set(keyFileChunks("bad.go"), []byte("{bad}"), nil); err != nil {
		t.Fatalf("Set: %v", err)
	}
	chunks := s.GetChunks("bad.go")
	if len(chunks) != 0 {
		t.Errorf("expected empty chunks on corrupt JSON, got %v", chunks)
	}
}

func TestGetWorkspaceSummary_CorruptJSON(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.db.Set(keyWSSummary(), []byte("{bad}"), nil); err != nil {
		t.Fatalf("Set: %v", err)
	}
	_, ok := s.GetWorkspaceSummary()
	if ok {
		t.Error("expected false on corrupt JSON, got true")
	}
}

func TestGetEdgesFrom_CorruptJSON(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	// Write corrupt edge data.
	key := keyEdge("a.go", "b.go")
	if err := s.db.Set(key, []byte("{bad}"), nil); err != nil {
		t.Fatalf("Set: %v", err)
	}
	edges := s.GetEdgesFrom("a.go")
	// Corrupt entry is skipped — result is empty but no panic.
	_ = edges
}

// ---------------------------------------------------------------------------
// SetSymbols / SetChunks with nil slice
// ---------------------------------------------------------------------------

func TestSetSymbols_NilSlice(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.SetSymbols("f.go", nil); err != nil {
		t.Errorf("SetSymbols with nil slice: %v", err)
	}
}

func TestSetChunks_NilSlice(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if err := s.SetChunks("f.go", nil); err != nil {
		t.Errorf("SetChunks with nil slice: %v", err)
	}
}

// ---------------------------------------------------------------------------
// GetFileRecord with empty path
// ---------------------------------------------------------------------------

func TestGetFileRecord_EmptyPath(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	rec := s.GetFileRecord("")
	if rec.Hash != "" || rec.ParserVersion != 0 {
		t.Errorf("expected zero FileRecord for empty path, got %+v", rec)
	}
}

// ---------------------------------------------------------------------------
// Round-trip: GitHead, FileRecord, Symbols, Chunks, Edge, WorkspaceSummary
// ---------------------------------------------------------------------------

func TestRoundTrip_AllTypes(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	// GitHead
	if err := s.SetGitHead("abc123"); err != nil {
		t.Fatalf("SetGitHead: %v", err)
	}
	if got := s.GetGitHead(); got != "abc123" {
		t.Errorf("GetGitHead = %q, want %q", got, "abc123")
	}

	// FileRecord
	now := time.Now().Truncate(time.Second)
	rec := FileRecord{Path: "main.go", Hash: "deadbeef", ParserVersion: 3, IndexedAt: now}
	if err := s.SetFileRecord(rec); err != nil {
		t.Fatalf("SetFileRecord: %v", err)
	}
	got := s.GetFileRecord("main.go")
	if got.Hash != "deadbeef" || got.ParserVersion != 3 {
		t.Errorf("GetFileRecord = %+v, want hash=deadbeef pv=3", got)
	}

	// FileHash
	if h := s.GetFileHash("main.go"); h != "deadbeef" {
		t.Errorf("GetFileHash = %q, want %q", h, "deadbeef")
	}

	// Symbols
	syms := []Symbol{{Name: "Foo", Kind: "func", Line: 10}}
	if err := s.SetSymbols("main.go", syms); err != nil {
		t.Fatalf("SetSymbols: %v", err)
	}
	gotSyms := s.GetSymbols("main.go")
	if len(gotSyms) != 1 || gotSyms[0].Name != "Foo" {
		t.Errorf("GetSymbols = %v, want [{Foo func 10}]", gotSyms)
	}

	// Chunks
	chunks := []FileChunk{{Path: "main.go", Content: "func Foo() {}"}}
	if err := s.SetChunks("main.go", chunks); err != nil {
		t.Fatalf("SetChunks: %v", err)
	}
	gotChunks := s.GetChunks("main.go")
	if len(gotChunks) != 1 || gotChunks[0].Content != "func Foo() {}" {
		t.Errorf("GetChunks = %v", gotChunks)
	}

	// Edges
	edge := Edge{From: "a.go", To: "b.go", Kind: "import"}
	if err := s.SetEdge("a.go", "b.go", edge); err != nil {
		t.Fatalf("SetEdge: %v", err)
	}
	fromEdges := s.GetEdgesFrom("a.go")
	if len(fromEdges) != 1 {
		t.Errorf("GetEdgesFrom = %v, want 1 edge", fromEdges)
	}
	toEdges := s.GetEdgesTo("b.go")
	if len(toEdges) != 1 {
		t.Errorf("GetEdgesTo = %v, want 1 edge", toEdges)
	}
	allEdges := s.GetAllEdges()
	if len(allEdges) != 1 {
		t.Errorf("GetAllEdges = %v, want 1 edge", allEdges)
	}

	// WorkspaceSummary
	ws := WorkspaceSummary{TopFilesByRefCount: []string{"main.go"}}
	if err := s.SetWorkspaceSummary(ws); err != nil {
		t.Fatalf("SetWorkspaceSummary: %v", err)
	}
	gotWS, ok := s.GetWorkspaceSummary()
	if !ok || len(gotWS.TopFilesByRefCount) != 1 {
		t.Errorf("GetWorkspaceSummary = %+v ok=%v", gotWS, ok)
	}

	// Invalidate + DeleteFileRecords
	if err := s.Invalidate([]string{"main.go"}); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}
	if h := s.GetFileHash("main.go"); h != "" {
		t.Errorf("after Invalidate, GetFileHash = %q, want \"\"", h)
	}
	if err := s.DeleteFileRecords("main.go"); err != nil {
		t.Fatalf("DeleteFileRecords: %v", err)
	}
}
