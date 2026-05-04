package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/scrypster/huginn/internal/search"
)

func TestHandleHealth_IncludesSearchTelemetryWhenAvailable(t *testing.T) {
	srv, ts := newTestServer(t)
	hybrid := search.NewHybridSearcher(search.NewBM25Searcher(), nil, nil)
	if err := hybrid.Index(context.Background(), []search.Chunk{
		{ID: 1, Path: "x.go", Content: "hello"},
	}); err != nil {
		t.Fatalf("hybrid index: %v", err)
	}
	srv.orch.SetSearcher(hybrid)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/health", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	searchSection, ok := payload["search"].(map[string]any)
	if !ok {
		t.Fatalf("expected health.search object, got %#v", payload["search"])
	}
	if searchSection["last_index_total_chunks"] == nil {
		t.Fatalf("expected last_index_total_chunks metric in health.search, got %#v", searchSection)
	}
}
