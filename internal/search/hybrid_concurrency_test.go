package search_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/search"
)

func TestHybridSearcher_ConcurrentIndexAndSearch_NoConcurrentMapPanic(t *testing.T) {
	t.Parallel()

	bm25 := search.NewBM25Searcher()
	hs := search.NewHybridSearcher(bm25, nil, nil)
	ctx := context.Background()

	// Seed one chunk so early searches have content.
	if err := hs.Index(ctx, []search.Chunk{{ID: 1, Path: "seed.txt", Content: "seed content"}}); err != nil {
		t.Fatalf("seed Index: %v", err)
	}

	start := make(chan struct{})
	var wg sync.WaitGroup

	// Writer: continuously mutate hs.chunks via Index.
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 2; i < 500; i++ {
			ch := search.Chunk{
				ID:      uint64(i),
				Path:    fmt.Sprintf("doc-%d.txt", i),
				Content: fmt.Sprintf("content %d", i),
			}
			if err := hs.Index(ctx, []search.Chunk{ch}); err != nil {
				t.Errorf("Index failed: %v", err)
				return
			}
		}
	}()

	// Readers: search repeatedly while writes are happening.
	const readers = 8
	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < 400; i++ {
				if _, err := hs.Search(ctx, "content", 5); err != nil {
					t.Errorf("Search failed: %v", err)
					return
				}
			}
		}()
	}

	close(start)
	wg.Wait()
}
