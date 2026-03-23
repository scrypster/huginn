package storage

import (
	"testing"
	"time"
)

// --- GitHead ---

func TestGitHead_SetGet(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	if err := s.SetGitHead("abc123def456"); err != nil {
		t.Fatalf("SetGitHead: %v", err)
	}
	got := s.GetGitHead()
	if got != "abc123def456" {
		t.Errorf("GetGitHead: expected abc123def456, got %q", got)
	}
}

func TestGitHead_NotSet(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	if got := s.GetGitHead(); got != "" {
		t.Errorf("expected empty string for unset git head, got %q", got)
	}
}

func TestGitHead_NilDB(t *testing.T) {
	t.Parallel()
	s := &Store{}
	if got := s.GetGitHead(); got != "" {
		t.Errorf("nil db: expected empty string, got %q", got)
	}
	if err := s.SetGitHead("sha"); err == nil {
		t.Error("nil db: expected error from SetGitHead")
	}
}

// --- FileHash ---

func TestFileHash_SetGet(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	path := "/workspace/file.go"
	hash := "deadbeefdeadbeef"
	if got := s.GetFileHash(path); got != "" {
		t.Errorf("expected empty hash, got %q", got)
	}
	rec := FileRecord{Path: path, Hash: hash, ParserVersion: 3}
	if err := s.SetFileRecord(rec); err != nil {
		t.Fatalf("SetFileRecord: %v", err)
	}
	if got := s.GetFileHash(path); got != hash {
		t.Errorf("GetFileHash: expected %q, got %q", hash, got)
	}
}

func TestFileHash_NilDB(t *testing.T) {
	t.Parallel()
	s := &Store{}
	if got := s.GetFileHash("/some/file"); got != "" {
		t.Errorf("nil db: expected empty string, got %q", got)
	}
}

// --- FileRecord ---

func TestFileRecord_SetGet(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	rec := FileRecord{
		Path:          "/workspace/main.go",
		Hash:          "sha256hexhash",
		ParserVersion: 42,
		IndexedAt:     now,
	}
	if err := s.SetFileRecord(rec); err != nil {
		t.Fatalf("SetFileRecord: %v", err)
	}
	got := s.GetFileRecord("/workspace/main.go")
	if got.Hash != rec.Hash {
		t.Errorf("Hash: expected %q, got %q", rec.Hash, got.Hash)
	}
	if got.ParserVersion != rec.ParserVersion {
		t.Errorf("ParserVersion: expected %d, got %d", rec.ParserVersion, got.ParserVersion)
	}
}

func TestFileRecord_NilDB(t *testing.T) {
	t.Parallel()
	s := &Store{}
	if err := s.SetFileRecord(FileRecord{Path: "/f", Hash: "h"}); err == nil {
		t.Error("nil db: expected error from SetFileRecord")
	}
	rec := s.GetFileRecord("/f")
	if rec.Hash != "" {
		t.Errorf("nil db: expected empty record, got %+v", rec)
	}
}

// --- Symbols ---

func TestSymbols_SetGet(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	path := "/workspace/symbols.go"
	syms := []Symbol{
		{Name: "Foo", Kind: "function", Line: 10},
		{Name: "Bar", Kind: "struct", Line: 25},
	}
	if err := s.SetSymbols(path, syms); err != nil {
		t.Fatalf("SetSymbols: %v", err)
	}
	got := s.GetSymbols(path)
	if len(got) != 2 {
		t.Fatalf("expected 2 symbols, got %d", len(got))
	}
	if got[0].Name != "Foo" {
		t.Errorf("expected Foo, got %q", got[0].Name)
	}
}

func TestSymbols_NilDB(t *testing.T) {
	t.Parallel()
	s := &Store{}
	if err := s.SetSymbols("/f", []Symbol{{Name: "X"}}); err == nil {
		t.Error("nil db: expected error from SetSymbols")
	}
	got := s.GetSymbols("/f")
	if len(got) != 0 {
		t.Errorf("nil db: expected empty symbols, got %d", len(got))
	}
}

// --- Chunks ---

func TestChunks_SetGet(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	path := "/workspace/chunks.go"
	chunks := []FileChunk{
		{Path: path, Content: "chunk1", StartLine: 1},
		{Path: path, Content: "chunk2", StartLine: 11},
	}
	if err := s.SetChunks(path, chunks); err != nil {
		t.Fatalf("SetChunks: %v", err)
	}
	got := s.GetChunks(path)
	if len(got) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(got))
	}
	if got[0].Content != "chunk1" {
		t.Errorf("expected chunk1, got %q", got[0].Content)
	}
}

func TestChunks_NilDB(t *testing.T) {
	t.Parallel()
	s := &Store{}
	if err := s.SetChunks("/f", []FileChunk{}); err == nil {
		t.Error("nil db: expected error from SetChunks")
	}
	got := s.GetChunks("/f")
	if len(got) != 0 {
		t.Errorf("nil db: expected empty chunks, got %d", len(got))
	}
}

// --- Edges ---

func TestEdges_SetGetFrom(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	edge := Edge{From: "a.go", To: "b.go", Kind: "Import"}
	if err := s.SetEdge("a.go", "b.go", edge); err != nil {
		t.Fatalf("SetEdge: %v", err)
	}
	edges := s.GetEdgesFrom("a.go")
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
	if edges[0].To != "b.go" {
		t.Errorf("expected To=b.go, got %q", edges[0].To)
	}
}

func TestEdges_GetEdgesTo(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	if err := s.SetEdge("src.go", "dst.go", Edge{From: "src.go", To: "dst.go", Kind: "Call"}); err != nil {
		t.Fatalf("SetEdge: %v", err)
	}
	edges := s.GetEdgesTo("dst.go")
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge to dst.go, got %d", len(edges))
	}
	if edges[0].From != "src.go" {
		t.Errorf("expected From=src.go, got %q", edges[0].From)
	}
}

func TestEdges_GetAllEdges(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	s.SetEdge("x.go", "y.go", Edge{From: "x.go", To: "y.go"})
	s.SetEdge("y.go", "z.go", Edge{From: "y.go", To: "z.go"})
	all := s.GetAllEdges()
	if len(all) < 2 {
		t.Errorf("expected at least 2 edges, got %d", len(all))
	}
}

func TestEdges_NilDB(t *testing.T) {
	t.Parallel()
	s := &Store{}
	if err := s.SetEdge("a", "b", Edge{}); err == nil {
		t.Error("nil db: expected error from SetEdge")
	}
	if got := s.GetEdgesFrom("a"); len(got) != 0 {
		t.Errorf("nil db: expected empty edges from, got %d", len(got))
	}
	if got := s.GetEdgesTo("b"); len(got) != 0 {
		t.Errorf("nil db: expected empty edges to, got %d", len(got))
	}
}

// --- WorkspaceSummary ---

func TestWorkspaceSummary_SetGet(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ws := WorkspaceSummary{
		TopFilesByRefCount: []string{"main.go", "util.go"},
		CrossRepoHints:     []string{"monorepo"},
		UpdatedAt:          time.Now().UTC().Truncate(time.Second),
	}
	if err := s.SetWorkspaceSummary(ws); err != nil {
		t.Fatalf("SetWorkspaceSummary: %v", err)
	}
	got, ok := s.GetWorkspaceSummary()
	if !ok {
		t.Fatal("GetWorkspaceSummary: expected ok=true")
	}
	if len(got.TopFilesByRefCount) != 2 {
		t.Errorf("expected 2 top files, got %d", len(got.TopFilesByRefCount))
	}
	if got.TopFilesByRefCount[0] != "main.go" {
		t.Errorf("expected main.go, got %q", got.TopFilesByRefCount[0])
	}
}

func TestWorkspaceSummary_Missing(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	_, ok := s.GetWorkspaceSummary()
	if ok {
		t.Error("expected ok=false for missing workspace summary")
	}
}

func TestWorkspaceSummary_NilDB(t *testing.T) {
	t.Parallel()
	s := &Store{}
	if err := s.SetWorkspaceSummary(WorkspaceSummary{}); err == nil {
		t.Error("nil db: expected error from SetWorkspaceSummary")
	}
	_, ok := s.GetWorkspaceSummary()
	if ok {
		t.Error("nil db: expected ok=false from GetWorkspaceSummary")
	}
}

// --- Invalidate ---

func TestInvalidate_FileRecords(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	// Set some records first
	s.SetFileRecord(FileRecord{Path: "/a.go", Hash: "h1"})
	s.SetFileRecord(FileRecord{Path: "/b.go", Hash: "h2"})
	s.SetSymbols("/a.go", []Symbol{{Name: "X"}})
	s.SetChunks("/a.go", []FileChunk{{Path: "/a.go", Content: "c"}})

	// Invalidate
	if err := s.Invalidate([]string{"/a.go"}); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}
	// After invalidation, hash should be empty
	if got := s.GetFileHash("/a.go"); got != "" {
		t.Errorf("expected empty hash after invalidation, got %q", got)
	}
	// /b.go should be unaffected
	if got := s.GetFileHash("/b.go"); got != "h2" {
		t.Errorf("expected h2 for /b.go, got %q", got)
	}
}

func TestInvalidate_NilDB(t *testing.T) {
	t.Parallel()
	s := &Store{}
	if err := s.Invalidate([]string{"/a.go"}); err == nil {
		t.Error("nil db: expected error from Invalidate")
	}
}

// --- DeleteFileRecords ---

func TestDeleteFileRecords(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	s.SetFileRecord(FileRecord{Path: "/c.go", Hash: "h3"})
	if err := s.DeleteFileRecords("/c.go"); err != nil {
		t.Fatalf("DeleteFileRecords: %v", err)
	}
	if got := s.GetFileHash("/c.go"); got != "" {
		t.Errorf("expected empty after delete, got %q", got)
	}
}

func TestDeleteFileRecords_NilDB(t *testing.T) {
	t.Parallel()
	s := &Store{}
	if err := s.DeleteFileRecords("/x.go"); err == nil {
		t.Error("nil db: expected error from DeleteFileRecords")
	}
}
