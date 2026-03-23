package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchURLTool_Name(t *testing.T) {
	tool := &FetchURLTool{}
	if tool.Name() != "fetch_url" {
		t.Errorf("Name() = %q, want fetch_url", tool.Name())
	}
}

func TestFetchURLTool_PermRead(t *testing.T) {
	tool := &FetchURLTool{}
	if tool.Permission() != PermRead {
		t.Error("expected PermRead")
	}
}

func TestFetchURLTool_MissingURL(t *testing.T) {
	tool := &FetchURLTool{}
	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for missing url")
	}
}

func TestFetchURLTool_UnsupportedScheme(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"file scheme", "file:///etc/passwd"},
		{"ftp scheme", "ftp://example.com/file"},
		{"data scheme", "data:text/html,<h1>hi</h1>"},
		{"javascript scheme", "javascript:alert(1)"},
	}
	tool := &FetchURLTool{}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tool.Execute(context.Background(), map[string]any{"url": tc.url})
			if !result.IsError {
				t.Errorf("expected error for %q", tc.url)
			}
			if !strings.Contains(result.Error, "scheme") {
				t.Errorf("error should mention scheme, got %q", result.Error)
			}
		})
	}
}

func TestFetchURLTool_HTMLConvertedToMarkdown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<html><body><h1>Hello</h1><p>World</p></body></html>`))
	}))
	defer srv.Close()

	tool := &FetchURLTool{client: &http.Client{}}
	result := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "Hello") {
		t.Errorf("output missing 'Hello': %q", result.Output)
	}
}

func TestFetchURLTool_PlainTextPassthrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("plain text content"))
	}))
	defer srv.Close()

	tool := &FetchURLTool{client: &http.Client{}}
	result := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Output != "plain text content" {
		t.Errorf("expected passthrough, got %q", result.Output)
	}
}

func TestFetchURLTool_HTTPErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	tool := &FetchURLTool{client: &http.Client{}}
	result := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if !result.IsError {
		t.Error("expected error for 404")
	}
	if !strings.Contains(result.Error, "404") {
		t.Errorf("error should mention 404, got %q", result.Error)
	}
}

func TestFetchURLTool_ContentLimitEnforced(t *testing.T) {
	big := make([]byte, fetchURLMaxBytes+1024)
	for i := range big {
		big[i] = 'A'
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write(big)
	}))
	defer srv.Close()

	tool := &FetchURLTool{client: &http.Client{}}
	result := tool.Execute(context.Background(), map[string]any{"url": srv.URL})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if len(result.Output) > fetchURLMaxBytes {
		t.Errorf("output exceeds limit: len=%d, limit=%d", len(result.Output), fetchURLMaxBytes)
	}
}

func TestFetchURLTool_DescriptionMentionsJS(t *testing.T) {
	tool := &FetchURLTool{}
	if !strings.Contains(strings.ToLower(tool.Description()), "javascript") {
		t.Error("description should mention JavaScript rendering limitation")
	}
}
