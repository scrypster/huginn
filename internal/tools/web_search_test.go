package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestWebSearchTool_Name(t *testing.T) {
	tool := &WebSearchTool{APIKey: "key"}
	if tool.Name() != "web_search" {
		t.Errorf("Name() = %q, want web_search", tool.Name())
	}
}

func TestWebSearchTool_PermRead(t *testing.T) {
	tool := &WebSearchTool{APIKey: "key"}
	if tool.Permission() != PermRead {
		t.Error("expected PermRead")
	}
}

func TestWebSearchTool_MissingQuery(t *testing.T) {
	tool := &WebSearchTool{APIKey: "key"}
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for missing query")
	}
	if !strings.Contains(result.Error, "query") {
		t.Errorf("error should mention 'query', got %q", result.Error)
	}
}

func TestWebSearchTool_EmptyQuery(t *testing.T) {
	tool := &WebSearchTool{APIKey: "key"}
	result := tool.Execute(context.Background(), map[string]any{"query": "   "})
	if !result.IsError {
		t.Error("expected error for blank query")
	}
}

func TestWebSearchTool_SuccessfulSearch(t *testing.T) {
	response := map[string]any{
		"web": map[string]any{
			"results": []map[string]any{
				{"title": "Result One", "url": "https://example.com/1", "description": "First result"},
				{"title": "Result Two", "url": "https://example.com/2", "description": "Second result"},
			},
		},
	}
	body, _ := json.Marshal(response)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Subscription-Token") != "test-key" {
			t.Error("missing or wrong X-Subscription-Token")
		}
		if r.Header.Get("Accept") != "application/json" {
			t.Error("missing Accept: application/json")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	tool := &WebSearchTool{
		APIKey: "test-key",
		client: newTestClientRedirect(srv.URL),
	}

	result := tool.Execute(context.Background(), map[string]any{"query": "golang testing"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "Result One") {
		t.Errorf("output missing 'Result One': %q", result.Output)
	}
	if !strings.Contains(result.Output, "https://example.com/1") {
		t.Errorf("output missing URL: %q", result.Output)
	}
	if !strings.Contains(result.Output, "Result Two") {
		t.Errorf("output missing 'Result Two': %q", result.Output)
	}
}

func TestWebSearchTool_EmptyResults(t *testing.T) {
	response := map[string]any{
		"web": map[string]any{"results": []any{}},
	}
	body, _ := json.Marshal(response)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	tool := &WebSearchTool{APIKey: "key", client: newTestClientRedirect(srv.URL)}
	result := tool.Execute(context.Background(), map[string]any{"query": "very obscure query xyz"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "No results") {
		t.Errorf("expected 'No results' message, got %q", result.Output)
	}
}

func TestWebSearchTool_CountClamped(t *testing.T) {
	var capturedCount string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCount = r.URL.Query().Get("count")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"web":{"results":[]}}`))
	}))
	defer srv.Close()

	tool := &WebSearchTool{APIKey: "key", client: newTestClientRedirect(srv.URL)}
	tool.Execute(context.Background(), map[string]any{"query": "q", "count": float64(99)})
	if capturedCount != "10" {
		t.Errorf("expected count=10, got %q", capturedCount)
	}
}

func newTestClientRedirect(baseURL string) *http.Client {
	return &http.Client{
		Transport: &redirectTransport{base: baseURL},
	}
}

type redirectTransport struct {
	base string
}

func (rt *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	parsed, _ := url.Parse(rt.base)
	cloned.URL.Scheme = parsed.Scheme
	cloned.URL.Host = parsed.Host
	return http.DefaultTransport.RoundTrip(cloned)
}

func TestWebSearchTool_Schema(t *testing.T) {
	tool := &WebSearchTool{APIKey: "key"}
	schema := tool.Schema()
	if schema.Type != "function" {
		t.Errorf("expected type 'function', got %q", schema.Type)
	}
	if schema.Function.Name != "web_search" {
		t.Errorf("expected function name 'web_search', got %q", schema.Function.Name)
	}
	if _, ok := schema.Function.Parameters.Properties["query"]; !ok {
		t.Error("expected 'query' property")
	}
}

func TestWebSearchTool_Description(t *testing.T) {
	tool := &WebSearchTool{APIKey: "key"}
	desc := tool.Description()
	if desc == "" {
		t.Error("expected non-empty description")
	}
	if !strings.Contains(desc, "Brave") {
		t.Errorf("description should mention Brave, got %q", desc)
	}
}

func TestWebSearchTool_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "unauthorized"}`))
	}))
	defer srv.Close()

	tool := &WebSearchTool{APIKey: "wrong-key", client: newTestClientRedirect(srv.URL)}
	result := tool.Execute(context.Background(), map[string]any{"query": "test"})
	if !result.IsError {
		t.Error("expected error for HTTP 401")
	}
	if !strings.Contains(result.Error, "401") {
		t.Errorf("error should mention 401, got %q", result.Error)
	}
}

func TestWebSearchTool_JSONDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not valid json"))
	}))
	defer srv.Close()

	tool := &WebSearchTool{APIKey: "key", client: newTestClientRedirect(srv.URL)}
	result := tool.Execute(context.Background(), map[string]any{"query": "test"})
	if !result.IsError {
		t.Error("expected error for invalid JSON")
	}
	if !strings.Contains(result.Error, "decode") {
		t.Errorf("error should mention decode, got %q", result.Error)
	}
}

func TestWebSearchTool_CountMinimum(t *testing.T) {
	var capturedCount string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCount = r.URL.Query().Get("count")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"web":{"results":[]}}`))
	}))
	defer srv.Close()

	tool := &WebSearchTool{APIKey: "key", client: newTestClientRedirect(srv.URL)}
	tool.Execute(context.Background(), map[string]any{"query": "q", "count": float64(0)})
	if capturedCount != "1" {
		t.Errorf("expected count=1 for 0 input, got %q", capturedCount)
	}
}

func TestWebSearchTool_CountDefault(t *testing.T) {
	var capturedCount string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedCount = r.URL.Query().Get("count")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"web":{"results":[]}}`))
	}))
	defer srv.Close()

	tool := &WebSearchTool{APIKey: "key", client: newTestClientRedirect(srv.URL)}
	tool.Execute(context.Background(), map[string]any{"query": "q"})
	if capturedCount != "5" {
		t.Errorf("expected default count=5, got %q", capturedCount)
	}
}
