package storage_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/storage"
)

func TestStore_GetEdgesTo_Concurrent(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	target := "pkg/target.go"
	const writers = 5
	const readers = 3

	var wg sync.WaitGroup

	// Writers: each adds an edge to the shared target node.
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			from := fmt.Sprintf("pkg/src_%d.go", i)
			edge := storage.Edge{
				From:       from,
				To:         target,
				Symbol:     fmt.Sprintf("Func%d", i),
				Confidence: "HIGH",
				Kind:       "Call",
			}
			if err := s.SetEdge(from, target, edge); err != nil {
				t.Errorf("SetEdge(%d): %v", i, err)
			}
		}(i)
	}

	// Readers: concurrently read edges to the target.
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// May see partial results; the key invariant is no data race.
			_ = s.GetEdgesTo(target)
		}()
	}

	wg.Wait()

	// After all writes complete, verify the full set.
	edges := s.GetEdgesTo(target)
	if len(edges) != writers {
		t.Errorf("GetEdgesTo returned %d edges, want %d", len(edges), writers)
	}
}

func TestStore_Compact_WithConcurrentOps(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	// Pre-populate 100 file records.
	for i := 0; i < 100; i++ {
		path := fmt.Sprintf("file_%d.go", i)
		if err := s.SetFileRecord(storage.FileRecord{Path: path, Hash: fmt.Sprintf("sha_%d", i)}); err != nil {
			t.Fatalf("SetFileRecord(%d): %v", i, err)
		}
	}

	var wg sync.WaitGroup
	errs := make(chan error, 200)

	// Compact in a goroutine.
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := s.Compact(); err != nil {
			errs <- fmt.Errorf("Compact: %w", err)
		}
	}()

	// Concurrent reads.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := fmt.Sprintf("file_%d.go", i%100)
			_ = s.GetFileHash(path)
		}(i)
	}

	// Concurrent writes.
	for i := 100; i < 150; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := fmt.Sprintf("file_%d.go", i)
			if err := s.SetFileRecord(storage.FileRecord{Path: path, Hash: fmt.Sprintf("sha_%d", i)}); err != nil {
				errs <- fmt.Errorf("SetFileRecord(%d): %w", i, err)
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
}

// TestStore_HeavyConcurrentReadWrite verifies that the store handles extreme concurrent
// load with many readers and writers operating simultaneously without panicking or corrupting data.
func TestStore_HeavyConcurrentReadWrite(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	const writers = 20
	const readers = 50
	const filesPerWriter = 10

	var wg sync.WaitGroup
	errs := make(chan error, writers+readers+100)

	// Writers: each writes multiple file records and edges
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for f := 0; f < filesPerWriter; f++ {
				path := fmt.Sprintf("writer_%d_file_%d.go", writerID, f)
				hash := fmt.Sprintf("hash_w%d_f%d", writerID, f)
				if err := s.SetFileRecord(storage.FileRecord{
					Path: path,
					Hash: hash,
				}); err != nil {
					errs <- err
				}

				// Also set some edges
				from := fmt.Sprintf("writer_%d_src_%d.go", writerID, f)
				to := path
				edge := storage.Edge{
					From:       from,
					To:         to,
					Symbol:     fmt.Sprintf("Func_w%d_f%d", writerID, f),
					Confidence: "MEDIUM",
					Kind:       "Call",
				}
				if err := s.SetEdge(from, to, edge); err != nil {
					errs <- err
				}
			}
		}(w)
	}

	// Readers: each performs random reads
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			// Try reading from writers' data
			for w := 0; w < writers; w++ {
				for f := 0; f < filesPerWriter; f++ {
					path := fmt.Sprintf("writer_%d_file_%d.go", w, f)
					hash := s.GetFileHash(path)
					// It's okay if hash is empty (file not yet written by this reader's timing)
					if hash != "" && len(hash) < 4 {
						errs <- fmt.Errorf("reader %d: suspiciously short hash from GetFileHash(%q): %q",
							readerID, path, hash)
					}
				}
			}
			// Try reading edges
			for w := 0; w < writers; w++ {
				path := fmt.Sprintf("writer_%d_file_%d.go", w, 0)
				edges := s.GetEdgesTo(path)
				// Edges may be empty due to timing
				if edges != nil && len(edges) > 100 {
					errs <- fmt.Errorf("reader %d: unexpectedly many edges to %q: %d",
						readerID, path, len(edges))
				}
			}
		}(r)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
}

// TestStore_MultipleOpenClose_Consistency verifies that repeatedly opening and closing
// the same store directory preserves data consistently.
func TestStore_MultipleOpenClose_Consistency(t *testing.T) {
	dir := t.TempDir()

	// First open: write data
	s1, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	testData := []struct {
		path string
		hash string
	}{
		{"pkg1/file1.go", "hash_1"},
		{"pkg2/file2.go", "hash_2"},
		{"pkg3/file3.go", "hash_3"},
	}
	for _, td := range testData {
		if err := s1.SetFileRecord(storage.FileRecord{
			Path: td.path,
			Hash: td.hash,
		}); err != nil {
			t.Fatalf("SetFileRecord(%q): %v", td.path, err)
		}
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	// Verify data persists across opens
	for openAttempt := 0; openAttempt < 3; openAttempt++ {
		s, err := storage.Open(dir)
		if err != nil {
			t.Fatalf("Open attempt %d: %v", openAttempt, err)
		}

		for _, td := range testData {
			hash := s.GetFileHash(td.path)
			if hash != td.hash {
				t.Errorf("Open attempt %d: expected hash %q for %q, got %q",
					openAttempt, td.hash, td.path, hash)
			}
		}

		if err := s.Close(); err != nil {
			t.Fatalf("Close attempt %d: %v", openAttempt, err)
		}
	}
}
