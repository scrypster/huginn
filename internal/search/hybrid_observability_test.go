package search_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/search"
	"github.com/scrypster/huginn/internal/search/hnsw"
)

type flakyEmbedder struct{}

func (f *flakyEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	switch {
	case strings.Contains(text, "query-embed-fail"):
		return nil, fmt.Errorf("embedder unavailable")
	case strings.Contains(text, "dim4"):
		return []float32{0.1, 0.2, 0.3, 0.4}, nil
	default:
		return []float32{0.1, 0.2, 0.3}, nil
	}
}

func (f *flakyEmbedder) Dimensions() int { return 3 }

func TestHybridSearcher_SearchHealth_QueryFallbackCounter(t *testing.T) {
	t.Parallel()
	hybrid := search.NewHybridSearcher(search.NewBM25Searcher(), hnsw.New(8, 200), &flakyEmbedder{})
	chunks := []search.Chunk{
		{ID: 1, Path: "a.go", Content: "hello world"},
		{ID: 2, Path: "b.go", Content: "hello dim4"},
	}
	if err := hybrid.Index(context.Background(), chunks); err != nil {
		t.Fatalf("Index: %v", err)
	}

	results, err := hybrid.Search(context.Background(), "hello query-embed-fail", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected BM25 fallback results")
	}

	health := hybrid.SearchHealth()
	if health.QueryEmbedFailures < 1 {
		t.Fatalf("QueryEmbedFailures = %d, want >= 1", health.QueryEmbedFailures)
	}
	if health.SemanticFallbacks < 1 {
		t.Fatalf("SemanticFallbacks = %d, want >= 1", health.SemanticFallbacks)
	}
	if health.HNSWInsertFailures < 1 {
		t.Fatalf("HNSWInsertFailures = %d, want >= 1", health.HNSWInsertFailures)
	}
}
