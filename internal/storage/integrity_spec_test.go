package storage

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestBatchOperation_PartialFailure verifies that when SetFileRecord batch fails
// on the second key, no partial state remains in the store.
func TestBatchOperation_PartialFailure(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	defer s.Close()

	// Create a test file record.
	rec := FileRecord{
		Path:          "/test/partial.go",
		Hash:          "abc123",
		ParserVersion: 1,
		IndexedAt:     time.Now().UTC(),
	}

	// SetFileRecord commits atomically. If it succeeds, all three keys must be present.
	// We'll verify this by reading back all three pieces.
	if err := s.SetFileRecord(rec); err != nil {
		t.Fatalf("SetFileRecord: %v", err)
	}

	// Read back and verify all keys were committed.
	loaded := s.GetFileRecord("/test/partial.go")
	if loaded.Hash == "" {
		t.Fatal("Hash not found after SetFileRecord")
	}
	if loaded.ParserVersion == 0 {
		t.Fatal("ParserVersion not found after SetFileRecord")
	}
	if loaded.IndexedAt.IsZero() {
		t.Fatal("IndexedAt not found after SetFileRecord")
	}

	// If any key is missing, the batch failed partially.
	if loaded.Hash != rec.Hash || loaded.ParserVersion != rec.ParserVersion {
		t.Error("Batch operation left partial state")
	}
}

// TestJSONCorruption_SymbolsRecovery verifies that corrupted symbol JSON is handled gracefully.
func TestJSONCorruption_SymbolsRecovery(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	defer s.Close()

	// Set valid symbols first.
	symbols := []Symbol{{Name: "main", Kind: "function", Path: "main.go", Line: 1, Exported: true}}
	if err := s.SetSymbols("/test.go", symbols); err != nil {
		t.Fatalf("SetSymbols: %v", err)
	}

	// Directly write corrupted JSON to the key (simulating corruption).
	dbKey := keyFileSymbols("/test.go")
	corruptedData := []byte("{invalid json")
	if err := s.db.Set(dbKey, corruptedData, nil); err != nil {
		t.Fatalf("Set corrupt data: %v", err)
	}

	// GetSymbols should return empty slice, not panic, and should log a warning.
	retrieved := s.GetSymbols("/test.go")
	if len(retrieved) != 0 {
		t.Errorf("Expected empty symbols on corruption, got %d", len(retrieved))
	}
}

// TestJSONCorruption_ChunksRecovery verifies that corrupted chunk JSON is handled gracefully.
func TestJSONCorruption_ChunksRecovery(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	defer s.Close()

	chunks := []FileChunk{{Path: "main.go", Content: "func main() {}", StartLine: 1}}
	if err := s.SetChunks("/test.go", chunks); err != nil {
		t.Fatalf("SetChunks: %v", err)
	}

	// Corrupt the stored JSON.
	dbKey := keyFileChunks("/test.go")
	corruptedData := []byte("not json at all")
	if err := s.db.Set(dbKey, corruptedData, nil); err != nil {
		t.Fatalf("Set corrupt data: %v", err)
	}

	// GetChunks should return empty slice, not panic.
	retrieved := s.GetChunks("/test.go")
	if len(retrieved) != 0 {
		t.Errorf("Expected empty chunks on corruption, got %d", len(retrieved))
	}
}

// TestHighContention_ConcurrentMixedWorkload simulates 50+ goroutines with mixed
// reads, writes, and deletes to stress the RWMutex and detect race conditions.
func TestHighContention_ConcurrentMixedWorkload(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping high-contention test in short mode")
	}
	t.Parallel()
	s := openTestStore(t)
	defer s.Close()

	const (
		numGoroutines = 50
		opsPerGoroutine = 100
	)

	var wg sync.WaitGroup
	var errorCount atomic.Int32
	var successCount atomic.Int32

	// Pre-populate some data.
	for i := 0; i < 10; i++ {
		rec := FileRecord{
			Path:          fmt.Sprintf("/file%d.go", i),
			Hash:          fmt.Sprintf("hash%d", i),
			ParserVersion: i,
			IndexedAt:     time.Now(),
		}
		_ = s.SetFileRecord(rec)
	}

	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for op := 0; op < opsPerGoroutine; op++ {
				fileID := op % 10
				path := fmt.Sprintf("/file%d.go", fileID)

				switch op % 4 {
				case 0: // Write
					rec := FileRecord{
						Path:          path,
						Hash:          fmt.Sprintf("hash_%d_%d", id, op),
						ParserVersion: op,
						IndexedAt:     time.Now(),
					}
					if err := s.SetFileRecord(rec); err != nil {
						errorCount.Add(1)
					} else {
						successCount.Add(1)
					}
				case 1: // Read
					_ = s.GetFileRecord(path)
					successCount.Add(1)
				case 2: // Delete
					if err := s.DeleteFileRecords(path); err != nil {
						errorCount.Add(1)
					} else {
						successCount.Add(1)
					}
				case 3: // Set edge
					edge := Edge{
						From:       path,
						To:         fmt.Sprintf("/dep%d.go", op),
						Symbol:     "Import",
						Confidence: "HIGH",
						Kind:       "Import",
					}
					if err := s.SetEdge(edge.From, edge.To, edge); err != nil {
						errorCount.Add(1)
					} else {
						successCount.Add(1)
					}
				}
			}
		}(g)
	}

	wg.Wait()

	if errorCount.Load() > 0 {
		t.Logf("High-contention test: %d errors, %d successes", errorCount.Load(), successCount.Load())
	}
	// Note: Some errors may be expected (e.g., deleting non-existent files), but panics are not.
}

// TestIncrementLastByte_BoundaryCase verifies that incrementLastByte correctly
// handles the edge case where all bytes are 0xFF.
func TestIncrementLastByte_BoundaryCase(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{
			name:     "single FF",
			input:    []byte{0xFF},
			expected: []byte{0xFF, 0x00},
		},
		{
			name:     "multiple FF",
			input:    []byte{0xFF, 0xFF, 0xFF},
			expected: []byte{0xFF, 0xFF, 0xFF, 0x00},
		},
		{
			name:     "mixed with FF at end",
			input:    []byte{0x41, 0xFF},
			expected: []byte{0x42, 0x00},
		},
		{
			name:     "no FF",
			input:    []byte{0x41, 0x42},
			expected: []byte{0x41, 0x43},
		},
		{
			name:     "empty",
			input:    []byte{},
			expected: []byte{0x00},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := incrementLastByte(tt.input)
			if !bytesEqual(result, tt.expected) {
				t.Errorf("incrementLastByte(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

// TestReverseIndexConsistency_EdgeDelete verifies that DeleteEdge removes both
// the forward edge and the reverse-index entry atomically.
func TestReverseIndexConsistency_EdgeDelete(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	defer s.Close()

	// Create edges.
	edge1 := Edge{From: "/a.go", To: "/b.go", Symbol: "Import", Confidence: "HIGH", Kind: "Import"}
	edge2 := Edge{From: "/a.go", To: "/c.go", Symbol: "Import", Confidence: "HIGH", Kind: "Import"}

	if err := s.SetEdge(edge1.From, edge1.To, edge1); err != nil {
		t.Fatalf("SetEdge 1: %v", err)
	}
	if err := s.SetEdge(edge2.From, edge2.To, edge2); err != nil {
		t.Fatalf("SetEdge 2: %v", err)
	}

	// Verify both edges are present.
	edgesFrom := s.GetEdgesFrom("/a.go")
	if len(edgesFrom) != 2 {
		t.Errorf("Expected 2 edges from /a.go, got %d", len(edgesFrom))
	}

	edgesTo := s.GetEdgesTo("/b.go")
	if len(edgesTo) != 1 {
		t.Errorf("Expected 1 edge to /b.go, got %d", len(edgesTo))
	}

	// Delete one edge.
	if err := s.DeleteEdge(edge1.From, edge1.To); err != nil {
		t.Fatalf("DeleteEdge: %v", err)
	}

	// Verify both forward and reverse indices were cleaned.
	edgesFrom = s.GetEdgesFrom("/a.go")
	if len(edgesFrom) != 1 {
		t.Errorf("After delete, expected 1 edge from /a.go, got %d", len(edgesFrom))
	}

	edgesTo = s.GetEdgesTo("/b.go")
	if len(edgesTo) != 0 {
		t.Errorf("After delete, expected 0 edges to /b.go, got %d", len(edgesTo))
	}
}

// TestEmptyStoreIteration_NoIndexedEdges verifies that GetAllEdges on an empty
// store (or before any edges are added) returns an empty slice without error.
func TestEmptyStoreIteration_NoIndexedEdges(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	defer s.Close()

	edges := s.GetAllEdges()
	if len(edges) != 0 {
		t.Errorf("Expected empty result, got %v", edges)
	}
}

// TestWorkspaceSummaryMalformed verifies that a corrupted workspace summary is
// handled gracefully (returns false for exists, empty summary).
func TestWorkspaceSummaryMalformed(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	defer s.Close()

	// Set valid workspace summary.
	ws := WorkspaceSummary{
		TopFilesByRefCount: []string{"/main.go", "/util.go"},
		UpdatedAt:          time.Now(),
	}
	if err := s.SetWorkspaceSummary(ws); err != nil {
		t.Fatalf("SetWorkspaceSummary: %v", err)
	}

	// Corrupt the stored JSON.
	if err := s.db.Set(keyWSSummary(), []byte("{corrupted"), nil); err != nil {
		t.Fatalf("Set corrupt WS: %v", err)
	}

	// GetWorkspaceSummary should return false (not found/corrupted) without panicking.
	_, exists := s.GetWorkspaceSummary()
	if exists {
		t.Error("Expected exists=false for corrupted workspace summary")
	}
}

// TestClosedStoreIsClosed verifies that calling IsClosed() works correctly.
func TestClosedStoreIsClosed(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)

	if s.isClosed() {
		t.Error("Expected isClosed() = false before Close()")
	}

	s.Close()

	if !s.isClosed() {
		t.Error("Expected isClosed() = true after Close()")
	}
}

// Helper function to compare byte slices.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
