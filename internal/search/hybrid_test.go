package search_test

import (
	"context"
	"math"
	"testing"
	"github.com/scrypster/huginn/internal/search"
	"github.com/scrypster/huginn/internal/search/hnsw"
)

func TestHybridSearcher_BM25Only(t *testing.T) {
	chunks := []search.Chunk{
		{ID: 1, Path: "foo.go", Content: "package main\n\nfunc hello() { println(\"hello\") }"},
		{ID: 2, Path: "bar.go", Content: "package main\n\nfunc goodbye() { println(\"goodbye\") }"},
	}

	bm25 := search.NewBM25Searcher()
	hybrid := search.NewHybridSearcher(bm25, nil, nil)
	hybrid.Index(context.Background(), chunks)

	results, err := hybrid.Search(context.Background(), "hello", 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) == 0 {
		t.Error("expected non-empty results")
	}

	// First result should contain "hello"
	found := false
	for _, r := range results {
		if r.ID == 1 {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected chunk 1 in results")
	}
}

func TestHybridSearcher_WithHNSW(t *testing.T) {
	chunks := []search.Chunk{
		{ID: 1, Path: "foo.go", Content: "package main\n\nfunc hello() { println(\"hello\") }"},
		{ID: 2, Path: "bar.go", Content: "package main\n\nfunc goodbye() { println(\"goodbye\") }"},
	}

	bm25 := search.NewBM25Searcher()
	hnswIdx := hnsw.New(8, 200)

	// Mock embedder that produces simple vectors
	embedder := &mockEmbedder{}

	hybrid := search.NewHybridSearcher(bm25, hnswIdx, embedder)
	hybrid.Index(context.Background(), chunks)

	results, err := hybrid.Search(context.Background(), "hello", 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) == 0 {
		t.Error("expected non-empty results")
	}
}

func TestHybridSearcher_Empty(t *testing.T) {
	bm25 := search.NewBM25Searcher()
	hybrid := search.NewHybridSearcher(bm25, nil, nil)
	hybrid.Index(context.Background(), []search.Chunk{})

	results, _ := hybrid.Search(context.Background(), "test", 5)
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty index, got %d", len(results))
	}
}

func TestHybridSearcher_Close(t *testing.T) {
	bm25 := search.NewBM25Searcher()
	hybrid := search.NewHybridSearcher(bm25, nil, nil)
	if err := hybrid.Close(); err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

// mockEmbedder produces deterministic embeddings for testing
type mockEmbedder struct{}

func (m *mockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	// Simple hash-based deterministic embedding
	hash := uint32(5381)
	for _, c := range text {
		hash = ((hash << 5) + hash) + uint32(c)
	}

	vec := make([]float32, 4)
	for i := range vec {
		val := math.Sin(float64(hash>>uint(i)) / 100.0)
		vec[i] = float32(val)
	}

	// Normalize
	var norm float32
	for _, v := range vec {
		norm += v * v
	}
	norm = float32(math.Sqrt(float64(norm)))
	for i := range vec {
		vec[i] /= norm
	}

	return vec, nil
}

func (m *mockEmbedder) Dimensions() int {
	return 4
}
