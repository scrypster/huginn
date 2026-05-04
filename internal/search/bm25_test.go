package search_test

import (
	"context"
	"github.com/scrypster/huginn/internal/search"
	"testing"
)

func TestBM25Searcher_Basic(t *testing.T) {
	chunks := []search.Chunk{
		{ID: 1, Path: "foo.go", Content: "package main\n\nfunc hello() { println(\"hello world\") }"},
		{ID: 2, Path: "bar.go", Content: "package main\n\nfunc goodbye() { println(\"goodbye\") }"},
		{ID: 3, Path: "baz.txt", Content: "this is a readme file\nwith hello mentioned"},
	}

	s := search.NewBM25Searcher()
	if err := s.Index(context.Background(), chunks); err != nil {
		t.Fatalf("Index: %v", err)
	}

	results, err := s.Search(context.Background(), "hello", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) == 0 {
		t.Errorf("expected non-zero results, got %d", len(results))
	}

	// Chunks with hello should be in results
	hasHello := false
	for _, r := range results {
		if r.ID == 1 || r.ID == 3 {
			hasHello = true
			break
		}
	}
	if !hasHello {
		t.Error("expected at least one chunk with 'hello' in results")
	}
}

func TestBM25Searcher_EmptyQuery(t *testing.T) {
	chunks := []search.Chunk{
		{ID: 1, Path: "foo.go", Content: "content"},
		{ID: 2, Path: "bar.go", Content: "more content"},
	}

	s := search.NewBM25Searcher()
	s.Index(context.Background(), chunks)

	results, _ := s.Search(context.Background(), "", 2)
	if len(results) != 2 {
		t.Errorf("expected 2 results for empty query, got %d", len(results))
	}
}

func TestBM25Searcher_NoMatches(t *testing.T) {
	chunks := []search.Chunk{
		{ID: 1, Path: "foo.go", Content: "package main\nfunc main() {}"},
		{ID: 2, Path: "bar.go", Content: "another file"},
	}

	s := search.NewBM25Searcher()
	s.Index(context.Background(), chunks)

	// With a query that doesn't match, BM25 may still return results with zero score
	// This is acceptable behavior - they're ordered after matching results
	results, _ := s.Search(context.Background(), "xyz123notfound", 1)
	// Just verify search completes without error
	if len(results) > 1 {
		t.Errorf("expected at most 1 result, got %d", len(results))
	}
}

func TestBM25Searcher_Close(t *testing.T) {
	s := search.NewBM25Searcher()
	if err := s.Close(); err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

func TestBM25Searcher_SearchBeforeIndex(t *testing.T) {
	s := search.NewBM25Searcher()
	results, err := s.Search(context.Background(), "hello", 5)
	if err != nil {
		t.Fatalf("Search before Index should not error: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results, got %v", results)
	}
}

func TestBM25Searcher_IndexSnapshotReplaceSemantics(t *testing.T) {
	s := search.NewBM25Searcher()
	if err := s.Index(context.Background(), []search.Chunk{
		{ID: 1, Path: "old.go", Content: "legacy token"},
	}); err != nil {
		t.Fatalf("Index old: %v", err)
	}
	if err := s.Index(context.Background(), []search.Chunk{
		{ID: 2, Path: "new.go", Content: "fresh token"},
	}); err != nil {
		t.Fatalf("Index new: %v", err)
	}

	oldResults, err := s.Search(context.Background(), "legacy", 5)
	if err != nil {
		t.Fatalf("Search legacy: %v", err)
	}
	for _, ch := range oldResults {
		if ch.ID == 1 {
			t.Fatalf("stale chunk ID=1 found after snapshot replace: %+v", oldResults)
		}
	}

	newResults, err := s.Search(context.Background(), "fresh", 5)
	if err != nil {
		t.Fatalf("Search fresh: %v", err)
	}
	if len(newResults) == 0 || newResults[0].ID != 2 {
		t.Fatalf("expected chunk ID=2 in fresh results, got %+v", newResults)
	}
}
