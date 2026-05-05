package search_test

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/search"
)

func TestHybridSearcher_IndexSnapshotReplaceSemantics(t *testing.T) {
	t.Parallel()
	hybrid := search.NewHybridSearcher(search.NewBM25Searcher(), nil, nil)

	first := []search.Chunk{
		{ID: 1, Path: "first.go", Content: "alpha only"},
	}
	if err := hybrid.Index(context.Background(), first); err != nil {
		t.Fatalf("Index first snapshot: %v", err)
	}

	second := []search.Chunk{
		{ID: 2, Path: "second.go", Content: "beta only"},
	}
	if err := hybrid.Index(context.Background(), second); err != nil {
		t.Fatalf("Index second snapshot: %v", err)
	}

	alphaResults, err := hybrid.Search(context.Background(), "alpha", 5)
	if err != nil {
		t.Fatalf("Search alpha: %v", err)
	}
	for _, ch := range alphaResults {
		if ch.ID == 1 {
			t.Fatalf("stale chunk id=1 should not appear after snapshot replace: %+v", alphaResults)
		}
	}

	betaResults, err := hybrid.Search(context.Background(), "beta", 5)
	if err != nil {
		t.Fatalf("Search beta: %v", err)
	}
	if len(betaResults) == 0 || betaResults[0].ID != 2 {
		t.Fatalf("expected chunk id=2 after reindex, got %+v", betaResults)
	}
}
