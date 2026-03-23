package search_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/scrypster/huginn/internal/search"
)

// ---------------------------------------------------------------------------
// Pre-computed index correctness
// ---------------------------------------------------------------------------

// TestBM25Precompute_ConsistentResults verifies that the pre-computed index
// produces the same ranking as the original on-the-fly computation would.
func TestBM25Precompute_ConsistentResults(t *testing.T) {
	chunks := []search.Chunk{
		{ID: 1, Path: "foo.go", Content: "package main func hello() println hello world"},
		{ID: 2, Path: "bar.go", Content: "package main func goodbye() println goodbye"},
		{ID: 3, Path: "baz.txt", Content: "this is a readme file with hello mentioned"},
	}

	s := search.NewBM25Searcher()
	if err := s.Index(context.Background(), chunks); err != nil {
		t.Fatalf("Index: %v", err)
	}

	results, err := s.Search(context.Background(), "hello", 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
	// Chunks containing "hello" should rank highest (IDs 1 and 3)
	topIDs := map[uint64]bool{results[0].ID: true, results[1].ID: true}
	if !topIDs[1] || !topIDs[3] {
		t.Errorf("top results should include IDs 1 and 3, got %d and %d", results[0].ID, results[1].ID)
	}
}

// TestBM25Precompute_ReindexOverwrites verifies that re-indexing replaces
// the precomputed data entirely.
func TestBM25Precompute_ReindexOverwrites(t *testing.T) {
	s := search.NewBM25Searcher()

	// First index
	s.Index(context.Background(), []search.Chunk{
		{ID: 1, Path: "a.go", Content: "alpha beta gamma"},
	})

	// Second index with different data
	s.Index(context.Background(), []search.Chunk{
		{ID: 10, Path: "b.go", Content: "delta epsilon zeta"},
	})

	results, _ := s.Search(context.Background(), "alpha", 5)
	// "alpha" only existed in the first index, so no matching doc
	for _, r := range results {
		if r.ID == 1 {
			t.Error("old chunk ID=1 should not appear after re-index")
		}
	}
}

// ---------------------------------------------------------------------------
// Benchmarks (5k and 10k)
// ---------------------------------------------------------------------------

func makeNChunks(n int) []search.Chunk {
	templates := []string{
		"func %s() error { return nil }",
		"// %s handles the request and writes a JSON response to the writer",
		"type %s struct { ID string; Name string; CreatedAt time.Time }",
		"var %s = map[string]interface{}{\"key\": \"value\", \"count\": 42}",
		"if err := %s.Validate(); err != nil { return fmt.Errorf(\"validation: %%w\", err) }",
	}
	paths := []string{"main.go", "handler.go", "model.go", "service.go", "store.go"}
	chunks := make([]search.Chunk, n)
	for i := range chunks {
		tmpl := templates[i%len(templates)]
		path := paths[i%len(paths)]
		chunks[i] = search.Chunk{
			ID:        uint64(i + 1),
			Path:      fmt.Sprintf("pkg/%s/%s", fmt.Sprintf("sub%d", i/100), path),
			Content:   fmt.Sprintf(tmpl, fmt.Sprintf("Symbol%d", i)),
			StartLine: (i % 200) * 5,
		}
	}
	return chunks
}

func BenchmarkBM25Search5000(b *testing.B) {
	chunks := makeNChunks(5000)
	s := search.NewBM25Searcher()
	if err := s.Index(context.Background(), chunks); err != nil {
		b.Fatalf("Index: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results, err := s.Search(context.Background(), "func error validate handler", 10)
		if err != nil {
			b.Fatalf("Search: %v", err)
		}
		_ = results
	}
}

func BenchmarkBM25Search10000(b *testing.B) {
	chunks := makeNChunks(10000)
	s := search.NewBM25Searcher()
	if err := s.Index(context.Background(), chunks); err != nil {
		b.Fatalf("Index: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		results, err := s.Search(context.Background(), "func error validate handler", 10)
		if err != nil {
			b.Fatalf("Search: %v", err)
		}
		_ = results
	}
}

// ---------------------------------------------------------------------------
// Hybrid fallback: embedder error → BM25-only
// ---------------------------------------------------------------------------

type failingEmbedder struct{}

func (f *failingEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, errors.New("embedding service unavailable")
}

func (f *failingEmbedder) Dimensions() int { return 128 }

func TestHybridSearcher_EmbedderError_FallbackToBM25(t *testing.T) {
	bm25 := search.NewBM25Searcher()
	chunks := []search.Chunk{
		{ID: 1, Path: "a.go", Content: "alpha beta gamma"},
		{ID: 2, Path: "b.go", Content: "delta epsilon zeta"},
		{ID: 3, Path: "c.go", Content: "alpha omega"},
	}

	hybrid := search.NewHybridSearcher(bm25, nil, &failingEmbedder{})
	if err := hybrid.Index(context.Background(), chunks); err != nil {
		t.Fatalf("Index: %v", err)
	}

	results, err := hybrid.Search(context.Background(), "alpha", 3)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	// Even with embedder failing, we should get BM25 results
	if len(results) == 0 {
		t.Fatal("expected non-empty results from BM25 fallback")
	}
	// At least one result should contain "alpha"
	found := false
	for _, r := range results {
		if r.ID == 1 || r.ID == 3 {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected BM25 fallback to return chunks containing 'alpha'")
	}
}
