package search_test

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/search"
	"github.com/scrypster/huginn/internal/search/hnsw"
)

func TestBM25Search_ZeroN(t *testing.T) {
	s := search.NewBM25Searcher()
	s.Index(context.Background(), []search.Chunk{
		{ID: 1, Path: "a.go", Content: "hello world"},
	})
	results, err := s.Search(context.Background(), "hello", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for n=0, got %d", len(results))
	}
}

func TestBM25Search_NegativeN(t *testing.T) {
	s := search.NewBM25Searcher()
	s.Index(context.Background(), []search.Chunk{
		{ID: 1, Path: "a.go", Content: "hello world"},
	})
	results, err := s.Search(context.Background(), "hello", -5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for n=-5, got %d", len(results))
	}
}

func TestHNSW_SearchK0(t *testing.T) {
	idx := hnsw.New(8, 200)
	idx.Insert(1, []float32{1, 0, 0, 0})
	results, err := idx.Search([]float32{1, 0, 0, 0}, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for k=0, got %d", len(results))
	}
}

func TestHNSW_SearchKNegative(t *testing.T) {
	idx := hnsw.New(8, 200)
	idx.Insert(1, []float32{1, 0, 0, 0})
	results, err := idx.Search([]float32{1, 0, 0, 0}, -1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for k=-1, got %d", len(results))
	}
}

func TestHNSW_SearchKGreaterThanNodes(t *testing.T) {
	idx := hnsw.New(8, 200)
	idx.Insert(1, []float32{1, 0, 0, 0})
	idx.Insert(2, []float32{0, 1, 0, 0})
	results, err := idx.Search([]float32{1, 0, 0, 0}, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) > 2 {
		t.Errorf("expected at most 2 results, got %d", len(results))
	}
}

func TestHybridSearch_ZeroN(t *testing.T) {
	bm25 := search.NewBM25Searcher()
	hybrid := search.NewHybridSearcher(bm25, nil, nil)
	hybrid.Index(context.Background(), []search.Chunk{
		{ID: 1, Path: "a.go", Content: "hello world"},
	})
	results, err := hybrid.Search(context.Background(), "hello", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for n=0, got %d", len(results))
	}
}

func TestHybridSearch_NegativeN(t *testing.T) {
	bm25 := search.NewBM25Searcher()
	hybrid := search.NewHybridSearcher(bm25, nil, nil)
	hybrid.Index(context.Background(), []search.Chunk{
		{ID: 1, Path: "a.go", Content: "hello world"},
	})
	results, err := hybrid.Search(context.Background(), "hello", -3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for n=-3, got %d", len(results))
	}
}
