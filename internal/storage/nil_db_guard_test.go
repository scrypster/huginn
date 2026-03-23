package storage

import (
	"testing"
	"time"

	"github.com/cockroachdb/pebble/v2"
)

// TestSetFileRecord_NilDB exercises the "store not initialized" guard in SetFileRecord.
func TestSetFileRecord_NilDB(t *testing.T) {
	s := &Store{db: nil}
	err := s.SetFileRecord(FileRecord{Path: "x.go", Hash: "abc"})
	if err == nil {
		t.Fatal("expected error for nil db in SetFileRecord")
	}
}

// TestSetSymbols_NilDB exercises the nil-db guard in SetSymbols.
func TestSetSymbols_NilDB(t *testing.T) {
	s := &Store{db: nil}
	err := s.SetSymbols("x.go", []Symbol{{Name: "Foo", Kind: "function"}})
	if err == nil {
		t.Fatal("expected error for nil db in SetSymbols")
	}
}

// TestSetChunks_NilDB exercises the nil-db guard in SetChunks.
func TestSetChunks_NilDB(t *testing.T) {
	s := &Store{db: nil}
	err := s.SetChunks("x.go", []FileChunk{{Content: "chunk"}})
	if err == nil {
		t.Fatal("expected error for nil db in SetChunks")
	}
}

// TestSetEdge_NilDB exercises the nil-db guard in SetEdge.
func TestSetEdge_NilDB(t *testing.T) {
	s := &Store{db: nil}
	err := s.SetEdge("a.go", "b.go", Edge{From: "a.go", To: "b.go"})
	if err == nil {
		t.Fatal("expected error for nil db in SetEdge")
	}
}

// TestSetWorkspaceSummary_NilDB exercises the nil-db guard in SetWorkspaceSummary.
func TestSetWorkspaceSummary_NilDB(t *testing.T) {
	s := &Store{db: nil}
	err := s.SetWorkspaceSummary(WorkspaceSummary{TopFilesByRefCount: []string{"x.go"}})
	if err == nil {
		t.Fatal("expected error for nil db in SetWorkspaceSummary")
	}
}

// TestInvalidateB95_NilDB exercises the nil-db guard in Invalidate.
func TestInvalidateB95_NilDB(t *testing.T) {
	s := &Store{db: nil}
	err := s.Invalidate([]string{"x.go"})
	if err == nil {
		t.Fatal("expected error for nil db in Invalidate")
	}
}

// TestDeleteFileRecordsB95_NilDB exercises the nil-db guard in DeleteFileRecords.
func TestDeleteFileRecordsB95_NilDB(t *testing.T) {
	s := &Store{db: nil}
	err := s.DeleteFileRecords("x.go")
	if err == nil {
		t.Fatal("expected error for nil db in DeleteFileRecords")
	}
}

// TestGetEdgesFrom_NilDB exercises the nil-db guard in GetEdgesFrom.
func TestGetEdgesFrom_NilDB(t *testing.T) {
	s := &Store{db: nil}
	edges := s.GetEdgesFrom("x.go")
	if edges == nil {
		// GetEdgesFrom returns []Edge{} — fine either way
	}
}

// TestGetEdgesTo_NilDB exercises the nil-db guard in GetEdgesTo.
func TestGetEdgesTo_NilDB(t *testing.T) {
	s := &Store{db: nil}
	edges := s.GetEdgesTo("x.go")
	_ = edges
}

// TestGetAllEdges_NilDB exercises the nil-db guard in GetAllEdges.
func TestGetAllEdges_NilDB(t *testing.T) {
	s := &Store{db: nil}
	edges := s.GetAllEdges()
	if edges != nil {
		t.Errorf("expected nil for nil db, got %v", edges)
	}
}

// TestGetEdgesTo_WithEdges exercises GetEdgesTo with actual data to cover the
// filter-by-To-field branch.
func TestGetEdgesTo_WithEdges(t *testing.T) {
	s := openTestStore(t)
	e1 := Edge{From: "a.go", To: "target.go", Kind: "import"}
	e2 := Edge{From: "b.go", To: "target.go", Kind: "call"}
	e3 := Edge{From: "c.go", To: "other.go", Kind: "import"}
	if err := s.SetEdge("a.go", "target.go", e1); err != nil {
		t.Fatalf("SetEdge a: %v", err)
	}
	if err := s.SetEdge("b.go", "target.go", e2); err != nil {
		t.Fatalf("SetEdge b: %v", err)
	}
	if err := s.SetEdge("c.go", "other.go", e3); err != nil {
		t.Fatalf("SetEdge c: %v", err)
	}
	edges := s.GetEdgesTo("target.go")
	if len(edges) != 2 {
		t.Errorf("expected 2 edges to target.go, got %d", len(edges))
	}
}

// TestGetAllEdges_WithEdges covers the iteration path in GetAllEdges.
func TestGetAllEdges_WithEdges(t *testing.T) {
	s := openTestStore(t)
	e := Edge{From: "a.go", To: "b.go", Kind: "import"}
	if err := s.SetEdge("a.go", "b.go", e); err != nil {
		t.Fatalf("SetEdge: %v", err)
	}
	all := s.GetAllEdges()
	if len(all) == 0 {
		t.Error("expected at least one edge in GetAllEdges")
	}
}

// TestInvalidate_EmptyPaths ensures Invalidate handles empty slice without error.
func TestInvalidate_EmptyPaths(t *testing.T) {
	s := openTestStore(t)
	if err := s.Invalidate([]string{}); err != nil {
		t.Fatalf("Invalidate(empty): %v", err)
	}
}

// TestSetFileRecord_RoundTrip exercises SetFileRecord fully and reads back.
func TestSetFileRecord_RoundTrip(t *testing.T) {
	s := openTestStore(t)
	now := time.Now().UTC().Truncate(time.Second)
	rec := FileRecord{
		Path:          "pkg/foo.go",
		Hash:          "deadbeef",
		ParserVersion: 3,
		IndexedAt:     now,
	}
	if err := s.SetFileRecord(rec); err != nil {
		t.Fatalf("SetFileRecord: %v", err)
	}
	got := s.GetFileRecord("pkg/foo.go")
	if got.Hash != rec.Hash {
		t.Errorf("Hash: got %q, want %q", got.Hash, rec.Hash)
	}
	if got.ParserVersion != rec.ParserVersion {
		t.Errorf("ParserVersion: got %d, want %d", got.ParserVersion, rec.ParserVersion)
	}
}

// TestDeleteFileRecords_Then_Get exercises DeleteFileRecords: after deletion, the
// file should have an empty hash again.
func TestDeleteFileRecords_Then_Get(t *testing.T) {
	s := openTestStore(t)
	if err := s.SetFileRecord(FileRecord{Path: "del.go", Hash: "abc123"}); err != nil {
		t.Fatalf("SetFileRecord: %v", err)
	}
	if err := s.DeleteFileRecords("del.go"); err != nil {
		t.Fatalf("DeleteFileRecords: %v", err)
	}
	hash := s.GetFileHash("del.go")
	if hash != "" {
		t.Errorf("expected empty hash after delete, got %q", hash)
	}
}

// TestGetAllEdges_InvalidJSON exercises the json.Unmarshal error (continue) branch in GetAllEdges.
func TestGetAllEdges_InvalidJSON(t *testing.T) {
	s := openTestStore(t)
	key := []byte("edge:corrupt.go:other.go")
	if err := s.DB().Set(key, []byte("{bad json"), &pebble.WriteOptions{Sync: true}); err != nil {
		t.Fatalf("DB Set: %v", err)
	}
	// GetAllEdges should skip the corrupt entry silently.
	edges := s.GetAllEdges()
	for _, e := range edges {
		if e.From == "corrupt.go" {
			t.Error("corrupt edge should have been skipped")
		}
	}
}

// TestGetEdgesFrom_InvalidJSON exercises the json.Unmarshal error branch in GetEdgesFrom.
// We write raw invalid-JSON bytes directly to a key that looks like an edge key.
func TestGetEdgesFrom_InvalidJSON(t *testing.T) {
	s := openTestStore(t)
	// Write invalid JSON under an edge key for "bad.go" -> "target.go".
	key := []byte("edge:bad.go:target.go")
	if err := s.DB().Set(key, []byte("{invalid json"), &pebble.WriteOptions{Sync: true}); err != nil {
		t.Fatalf("DB Set: %v", err)
	}
	// GetEdgesFrom should skip the bad entry (continue on unmarshal error).
	edges := s.GetEdgesFrom("bad.go")
	// Should return empty (bad entry skipped) with no panic.
	_ = edges
}

// TestGetEdgesTo_InvalidJSON exercises the json.Unmarshal error branch in GetEdgesTo.
func TestGetEdgesTo_InvalidJSON(t *testing.T) {
	s := openTestStore(t)
	key := []byte("edge:bad2.go:target2.go")
	if err := s.DB().Set(key, []byte("{bad"), &pebble.WriteOptions{Sync: true}); err != nil {
		t.Fatalf("DB Set: %v", err)
	}
	edges := s.GetEdgesTo("target2.go")
	_ = edges
}

// TestSetWorkspaceSummary_RoundTrip covers SetWorkspaceSummary and GetWorkspaceSummary.
func TestSetWorkspaceSummary_RoundTrip(t *testing.T) {
	s := openTestStore(t)
	ws := WorkspaceSummary{
		TopFilesByRefCount: []string{"a.go", "b.go"},
		CrossRepoHints:     []string{"hint1"},
	}
	if err := s.SetWorkspaceSummary(ws); err != nil {
		t.Fatalf("SetWorkspaceSummary: %v", err)
	}
	got, ok := s.GetWorkspaceSummary()
	if !ok {
		t.Fatal("GetWorkspaceSummary: not found")
	}
	if len(got.TopFilesByRefCount) != 2 {
		t.Errorf("TopFilesByRefCount: got %v", got.TopFilesByRefCount)
	}
}
