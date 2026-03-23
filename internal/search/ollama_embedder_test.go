package search_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"github.com/scrypster/huginn/internal/search"
)

func TestOllamaEmbedder_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			http.Error(w, "not found", 404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"embedding": []float32{0.1, 0.2, 0.3}})
	}))
	defer srv.Close()

	e := search.NewOllamaEmbedder(srv.URL, "nomic-embed-text")
	vec, err := e.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != 3 {
		t.Errorf("expected 3 dims, got %d", len(vec))
	}
	if e.Dimensions() != 3 {
		t.Errorf("expected Dimensions()=3")
	}
}

func TestOllamaEmbedder_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", 500)
	}))
	defer srv.Close()

	e := search.NewOllamaEmbedder(srv.URL, "nomic-embed-text")
	_, err := e.Embed(context.Background(), "test")
	if err == nil {
		t.Error("expected error on 500")
	}
}

func TestOllamaEmbedder_Probe_Unreachable(t *testing.T) {
	e := search.NewOllamaEmbedder("http://127.0.0.1:1", "nomic-embed-text")
	err := e.Probe(context.Background())
	if err == nil {
		t.Error("expected error when unreachable")
	}
}
