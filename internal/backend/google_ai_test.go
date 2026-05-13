package backend

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGoogleAI_Health_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("key") != "test-key" {
			t.Errorf("key = %q", r.URL.Query().Get("key"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"models":[]}`))
	}))
	defer srv.Close()

	b := NewGoogleAIBackendWithEndpoint(NewKeyResolver("test-key"), "gemini-2.5-pro", srv.URL)
	if err := b.Health(context.Background()); err != nil {
		t.Fatalf("Health = %v", err)
	}
}

func TestGoogleAI_Health_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad key", http.StatusUnauthorized)
	}))
	defer srv.Close()

	b := NewGoogleAIBackendWithEndpoint(NewKeyResolver("bad-key"), "gemini-2.5-pro", srv.URL)
	err := b.Health(context.Background())
	if err == nil || !strings.Contains(err.Error(), "invalid API key") {
		t.Errorf("Health err = %v", err)
	}
}

func TestGoogleAI_ChatCompletion_RejectsNonGemini(t *testing.T) {
	b := NewGoogleAIBackendWithEndpoint(NewKeyResolver("k"), "gpt-4", "http://localhost:0")
	_, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "gpt-4",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported model") {
		t.Errorf("err = %v, want unsupported-model error", err)
	}
}

func TestGoogleAI_ChatCompletion_RequiresKey(t *testing.T) {
	b := NewGoogleAIBackendWithEndpoint(NewKeyResolver(""), "gemini-2.5-pro", "http://localhost:0")
	_, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "gemini-2.5-pro",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil || !strings.Contains(err.Error(), "API key is required") {
		t.Errorf("err = %v, want missing-key error", err)
	}
}

func TestGoogleAI_ChatCompletion_E2E(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/v1beta/models/gemini-2.5-pro:streamGenerateContent"
		if r.URL.Path != wantPath {
			t.Errorf("path = %q, want %q", r.URL.Path, wantPath)
		}
		if r.URL.Query().Get("alt") != "sse" {
			t.Errorf("alt = %q", r.URL.Query().Get("alt"))
		}
		if r.URL.Query().Get("key") != "test-key" {
			t.Errorf("key = %q", r.URL.Query().Get("key"))
		}
		// Body must not include `model` field (model is in URL) and must include
		// the user message converted via buildGeminiBody.
		bodyBytes, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(bodyBytes, &body)
		if _, has := body["model"]; has {
			t.Error("body must not contain model field")
		}
		if _, has := body["contents"]; !has {
			t.Error("body missing contents")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(`data: {"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"STOP"}]}` + "\n\n"))
	}))
	defer srv.Close()

	b := NewGoogleAIBackendWithEndpoint(NewKeyResolver("test-key"), "gemini-2.5-pro", srv.URL)
	resp, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "gemini-2.5-pro",
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if resp.Content != "hi" {
		t.Errorf("Content = %q", resp.Content)
	}
	if resp.DoneReason != "STOP" {
		t.Errorf("DoneReason = %q", resp.DoneReason)
	}
}

func TestGoogleAI_ContextWindow(t *testing.T) {
	b := NewGoogleAIBackendWithEndpoint(NewKeyResolver("k"), "gemini-2.5-pro", "http://x")
	if got := b.ContextWindow(); got != 1_048_576 {
		t.Errorf("ContextWindow = %d", got)
	}
}
