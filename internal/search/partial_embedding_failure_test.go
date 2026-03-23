package search

import (
	"context"
	"errors"
	"testing"

	"github.com/scrypster/huginn/internal/search/hnsw"
)

// flakeyEmbedder fails on every other call, simulating a 50% embedding error rate.
type flakeyEmbedder struct {
	callCount int
}

func (e *flakeyEmbedder) Dimensions() int { return 3 }
func (e *flakeyEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	e.callCount++
	if e.callCount%2 == 0 {
		return nil, errors.New("embedder: simulated failure")
	}
	// Return a minimal 3-dimensional vector.
	return []float32{float32(e.callCount), 0.0, 0.0}, nil
}

// TestHybridSearcher_PartialEmbeddingFailure_NoPanic verifies that when an
// embedder fails on 50% of chunks, Index() does not panic, does not return an
// error (graceful degradation), and partially indexed chunks are searchable.
//
// Context (P3-2): hybrid.go silently continues on embedding error. The test
// confirms this is intentional, not a hidden bug: partial results are better
// than a hard failure, and the BM25 path always succeeds.
func TestHybridSearcher_PartialEmbeddingFailure_NoPanic(t *testing.T) {
	bm25 := NewBM25Searcher()
	idx := hnsw.New(4, 16)
	embedder := &flakeyEmbedder{}

	hs := NewHybridSearcher(bm25, idx, embedder)

	chunks := []Chunk{
		{ID: 1, Content: "alpha zero"},
		{ID: 2, Content: "beta one"},
		{ID: 3, Content: "gamma two"},
		{ID: 4, Content: "delta three"},
		{ID: 5, Content: "epsilon four"},
		{ID: 6, Content: "zeta five"},
	}

	// Index must succeed (nil error) even with 50% embedding failures.
	if err := hs.Index(context.Background(), chunks); err != nil {
		t.Fatalf("Index returned error with partial embedding failures: %v", err)
	}

	// Search must return results (BM25 always works regardless of embedding).
	results, err := hs.Search(context.Background(), "alpha", 3)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected at least one result from BM25 fallback, got none")
	}
	t.Logf("partial-embedding index: %d results for 'alpha'", len(results))
}

// TestHybridSearcher_AllEmbeddingFailures_NoPanic verifies that even with 100%
// embedding failures, the searcher degrades to BM25-only without panicking.
func TestHybridSearcher_AllEmbeddingFailures_NoPanic(t *testing.T) {
	bm25 := NewBM25Searcher()
	idx := hnsw.New(4, 16)
	alwaysFail := &alwaysFailEmbedder{}

	hs := NewHybridSearcher(bm25, idx, alwaysFail)

	chunks := []Chunk{
		{ID: 1, Content: "hello world"},
		{ID: 2, Content: "foo bar"},
	}

	if err := hs.Index(context.Background(), chunks); err != nil {
		t.Fatalf("Index with all-failing embedder returned error: %v", err)
	}

	results, err := hs.Search(context.Background(), "hello", 5)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	// BM25 path must find results even without any HNSW vectors.
	if len(results) == 0 {
		t.Error("expected BM25 results when all embeddings fail, got none")
	}
}

type alwaysFailEmbedder struct{}

func (e *alwaysFailEmbedder) Dimensions() int { return 3 }
func (e *alwaysFailEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return nil, errors.New("embedder: always fails")
}
