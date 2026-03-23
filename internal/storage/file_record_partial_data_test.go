package storage

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// GetFileRecord with partial data (some sub-keys missing)
// ---------------------------------------------------------------------------

// TestGetFileRecord_PartialData verifies that GetFileRecord returns whatever
// sub-keys are present and zero-values for absent ones.  This can happen if a
// write is interrupted mid-batch or if SetFileRecord is called while another
// goroutine deletes part of the record.
func TestGetFileRecord_PartialData(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)

	path := "partial/file.go"
	// Write only the hash key directly (skip parser-version and indexed_at).
	if err := s.db.Set(keyFileHash(path), []byte("onlyhash"), nil); err != nil {
		t.Fatalf("direct Set: %v", err)
	}

	rec := s.GetFileRecord(path)
	if rec.Hash != "onlyhash" {
		t.Errorf("Hash: got %q, want %q", rec.Hash, "onlyhash")
	}
	// Parser version key absent → zero.
	if rec.ParserVersion != 0 {
		t.Errorf("ParserVersion: expected 0 for absent key, got %d", rec.ParserVersion)
	}
	// IndexedAt key absent → zero time.
	if !rec.IndexedAt.IsZero() {
		t.Errorf("IndexedAt: expected zero time for absent key, got %v", rec.IndexedAt)
	}
}

// ---------------------------------------------------------------------------
// SetEdge overwrite — same from/to with different payload
// ---------------------------------------------------------------------------

func TestSetEdge_Overwrite(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)

	from, to := "cmd/main.go", "internal/svc.go"

	e1 := Edge{From: from, To: to, Symbol: "OldSvc", Kind: "Import", Confidence: "LOW"}
	if err := s.SetEdge(from, to, e1); err != nil {
		t.Fatalf("first SetEdge: %v", err)
	}

	e2 := Edge{From: from, To: to, Symbol: "NewSvc", Kind: "Call", Confidence: "HIGH"}
	if err := s.SetEdge(from, to, e2); err != nil {
		t.Fatalf("second SetEdge: %v", err)
	}

	edges := s.GetEdgesFrom(from)
	if len(edges) != 1 {
		t.Fatalf("expected exactly 1 edge after overwrite, got %d", len(edges))
	}
	if edges[0].Symbol != "NewSvc" {
		t.Errorf("Symbol after overwrite: got %q, want %q", edges[0].Symbol, "NewSvc")
	}
	if edges[0].Confidence != "HIGH" {
		t.Errorf("Confidence after overwrite: got %q, want %q", edges[0].Confidence, "HIGH")
	}
}

// ---------------------------------------------------------------------------
// GetEdgesTo — correctness with many edges to non-target files
// ---------------------------------------------------------------------------

// TestGetEdgesTo_ManyEdges_FilterCorrect writes 20 edges where only 3 point
// to the target.  GetEdgesTo must return exactly those 3.
func TestGetEdgesTo_ManyEdges_FilterCorrect(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)

	target := "internal/target.go"
	const total = 20
	const wantCount = 3

	for i := 0; i < total; i++ {
		from := fmt.Sprintf("src/file%d.go", i)
		toFile := "internal/other.go"
		if i < wantCount {
			toFile = target
		}
		e := Edge{From: from, To: toFile, Kind: "Import", Confidence: "MEDIUM"}
		if err := s.SetEdge(from, toFile, e); err != nil {
			t.Fatalf("SetEdge %d: %v", i, err)
		}
	}

	edges := s.GetEdgesTo(target)
	if len(edges) != wantCount {
		t.Errorf("GetEdgesTo: got %d, want %d", len(edges), wantCount)
	}
	for _, e := range edges {
		if e.To != target {
			t.Errorf("unexpected edge.To: %q", e.To)
		}
	}
}

// ---------------------------------------------------------------------------
// WorkspaceSummary with nil map field
// ---------------------------------------------------------------------------

// TestWorkspaceSummary_NilMap verifies that a WorkspaceSummary with a nil
// InferredRepoRoles map round-trips cleanly (no panic, deserializes without error).
func TestWorkspaceSummary_NilMap(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)

	ws := WorkspaceSummary{
		TopFilesByRefCount: []string{"main.go"},
		CrossRepoHints:     nil,
		InferredRepoRoles:  nil, // explicitly nil
		UpdatedAt:          time.Now().Truncate(time.Second),
	}

	if err := s.SetWorkspaceSummary(ws); err != nil {
		t.Fatalf("SetWorkspaceSummary with nil map: %v", err)
	}

	got, ok := s.GetWorkspaceSummary()
	if !ok {
		t.Fatal("GetWorkspaceSummary: expected ok=true")
	}
	// nil map may deserialize as nil or empty — either is fine, but it must not panic.
	_ = got.InferredRepoRoles
	if len(got.TopFilesByRefCount) != 1 {
		t.Errorf("TopFilesByRefCount: got %v", got.TopFilesByRefCount)
	}
}

// ---------------------------------------------------------------------------
// Large symbol count
// ---------------------------------------------------------------------------

// TestSymbols_LargeCount stores 1 000 symbols and retrieves them, verifying
// count and one spot-checked field.
func TestSymbols_LargeCount(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)

	path := "big_file.go"
	const count = 1000
	syms := make([]Symbol, count)
	for i := 0; i < count; i++ {
		syms[i] = Symbol{
			Name:     fmt.Sprintf("Sym%d", i),
			Kind:     "function",
			Path:     path,
			Line:     i + 1,
			Exported: i%2 == 0,
		}
	}

	if err := s.SetSymbols(path, syms); err != nil {
		t.Fatalf("SetSymbols large: %v", err)
	}

	got := s.GetSymbols(path)
	if len(got) != count {
		t.Fatalf("GetSymbols: got %d, want %d", len(got), count)
	}
	if got[500].Name != "Sym500" {
		t.Errorf("spot-check Sym500: got %q", got[500].Name)
	}
}

// ---------------------------------------------------------------------------
// Concurrent mixed reads and writes
// ---------------------------------------------------------------------------

// TestStore_ConcurrentMixedReadWrite exercises concurrent goroutines performing
// reads and writes to the same set of keys, exercising the race detector.
func TestStore_ConcurrentMixedReadWrite(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)

	const goroutines = 30
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			path := fmt.Sprintf("file%d.go", id%5) // share 5 paths across 30 goroutines

			if id%3 == 0 {
				// Writer
				rec := FileRecord{Path: path, Hash: fmt.Sprintf("h%d", id), ParserVersion: id, IndexedAt: time.Now()}
				_ = s.SetFileRecord(rec)
				_ = s.SetSymbols(path, []Symbol{{Name: fmt.Sprintf("Sym%d", id), Kind: "func"}})
			} else {
				// Reader
				_ = s.GetFileHash(path)
				_ = s.GetFileRecord(path)
				_ = s.GetSymbols(path)
			}
		}(i)
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// DeleteFileRecords leaves edges intact
// ---------------------------------------------------------------------------

// TestDeleteFileRecords_DoesNotDeleteEdges verifies that DeleteFileRecords only
// removes the file-scoped keys (hash, symbols, chunks, parser_version, indexed_at)
// and does NOT touch edge keys for the same path.
func TestDeleteFileRecords_DoesNotDeleteEdges(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)

	path := "src/important.go"
	_ = s.SetFileRecord(FileRecord{Path: path, Hash: "abc", ParserVersion: 1, IndexedAt: time.Now()})
	_ = s.SetEdge(path, "dst/other.go", Edge{From: path, To: "dst/other.go", Kind: "Import"})

	if err := s.DeleteFileRecords(path); err != nil {
		t.Fatalf("DeleteFileRecords: %v", err)
	}

	// File keys gone.
	if h := s.GetFileHash(path); h != "" {
		t.Errorf("hash still present after DeleteFileRecords: %q", h)
	}
	// Edge should still be there.
	edges := s.GetEdgesFrom(path)
	if len(edges) != 1 {
		t.Errorf("expected edge to survive DeleteFileRecords, got %d edges", len(edges))
	}
}

// ---------------------------------------------------------------------------
// Invalidate preserves symbols and chunks (only removes hash)
// ---------------------------------------------------------------------------

// TestInvalidate_PreservesSymbolsAndChunks verifies that Invalidate only
// deletes the file hash key, leaving symbols and chunks intact.
func TestInvalidate_PreservesSymbolsAndChunks(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)

	path := "keep/symbols.go"
	_ = s.SetFileRecord(FileRecord{Path: path, Hash: "xyz", ParserVersion: 2, IndexedAt: time.Now()})
	_ = s.SetSymbols(path, []Symbol{{Name: "KeepMe", Kind: "function"}})
	_ = s.SetChunks(path, []FileChunk{{Path: path, Content: "package keep", StartLine: 1}})

	if err := s.Invalidate([]string{path}); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}

	// Hash gone.
	if h := s.GetFileHash(path); h != "" {
		t.Errorf("hash should be gone after Invalidate, got %q", h)
	}
	// Symbols intact.
	syms := s.GetSymbols(path)
	if len(syms) != 1 || syms[0].Name != "KeepMe" {
		t.Errorf("symbols should survive Invalidate, got %v", syms)
	}
	// Chunks intact.
	chunks := s.GetChunks(path)
	if len(chunks) != 1 {
		t.Errorf("chunks should survive Invalidate, got %v", chunks)
	}
}

// ---------------------------------------------------------------------------
// GetAllEdges returns nil (not empty slice) on empty store
// ---------------------------------------------------------------------------

// This test pins the observable contract of GetAllEdges when no edges exist:
// nil (not []Edge{}) so callers can distinguish "no data" from "empty data".
func TestGetAllEdges_ReturnsNilNotEmptySlice(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)

	result := s.GetAllEdges()
	// The implementation returns nil when the iteration body never executes.
	// We pin this as the expected contract.
	if result != nil {
		t.Errorf("GetAllEdges on empty store: expected nil, got %v (len=%d)", result, len(result))
	}
}
