package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// keepAliveCaptureServer returns an httptest server that decodes the request
// body as an openAIRequest-shaped map and reports the keep_alive field (or
// "" / not-present) back through the returned pointer, then answers a
// minimal non-streaming-compatible SSE response so ChatCompletion succeeds.
func keepAliveCaptureServer(t *testing.T, captured *string, present *bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		if v, ok := body["keep_alive"]; ok {
			*present = true
			s, _ := v.(string)
			*captured = s
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

// TestExternalBackend_KeepAlive_DefaultSentOnOllamaBackend verifies a
// freshly-constructed ExternalBackend (the ollama-family constructor) sends
// DefaultOllamaKeepAlive ("10m") on every chat request without any explicit
// SetKeepAlive call.
func TestExternalBackend_KeepAlive_DefaultSentOnOllamaBackend(t *testing.T) {
	var captured string
	var present bool
	srv := keepAliveCaptureServer(t, &captured, &present)
	defer srv.Close()

	b := NewExternalBackend(srv.URL)
	if _, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "qwen2.5-coder:14b",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if !present {
		t.Fatal("expected keep_alive field in request body, got none")
	}
	if captured != DefaultOllamaKeepAlive {
		t.Fatalf("expected keep_alive=%q, got %q", DefaultOllamaKeepAlive, captured)
	}
}

// TestExternalBackend_KeepAlive_SetKeepAliveOverrides verifies an explicit
// SetKeepAlive call changes the value sent on the wire.
func TestExternalBackend_KeepAlive_SetKeepAliveOverrides(t *testing.T) {
	var captured string
	var present bool
	srv := keepAliveCaptureServer(t, &captured, &present)
	defer srv.Close()

	b := NewExternalBackend(srv.URL)
	b.SetKeepAlive("1h")
	if _, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "qwen2.5-coder:14b",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if captured != "1h" {
		t.Fatalf("expected keep_alive=%q, got %q", "1h", captured)
	}
}

// TestExternalBackend_KeepAlive_EmptyOmitsField verifies SetKeepAlive("")
// drops the field from the request entirely rather than sending an empty
// string (json:",omitempty" behavior).
func TestExternalBackend_KeepAlive_EmptyOmitsField(t *testing.T) {
	var captured string
	var present bool
	srv := keepAliveCaptureServer(t, &captured, &present)
	defer srv.Close()

	b := NewExternalBackend(srv.URL)
	b.SetKeepAlive("")
	if _, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "qwen2.5-coder:14b",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if present {
		t.Fatalf("expected keep_alive field to be omitted, got %q", captured)
	}
}

// TestExternalBackend_KeepAlive_APIKeyConstructorOptsOut verifies
// NewExternalBackendWithAPIKey (used for openai/deepseek/zai/custom — never
// ollama) never sends keep_alive, matching "ONLY the ollama backend".
func TestExternalBackend_KeepAlive_APIKeyConstructorOptsOut(t *testing.T) {
	var captured string
	var present bool
	srv := keepAliveCaptureServer(t, &captured, &present)
	defer srv.Close()

	b := NewExternalBackendWithAPIKey(srv.URL, func() (string, error) { return "sk-test", nil })
	if _, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if present {
		t.Fatalf("cloud OpenAI-compatible backend must never send keep_alive, got %q", captured)
	}
}

// TestBackendCache_SetOllamaKeepAlive_AppliesToOllamaBackends verifies the
// cache-level override reaches per-agent ollama backends built through For().
func TestBackendCache_SetOllamaKeepAlive_AppliesToOllamaBackends(t *testing.T) {
	var captured string
	var present bool
	srv := keepAliveCaptureServer(t, &captured, &present)
	defer srv.Close()

	c := NewBackendCache(nil)
	c.SetOllamaKeepAlive("30m")
	b, err := c.For("ollama", srv.URL, "", "qwen2.5-coder:14b")
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if _, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "qwen2.5-coder:14b",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if captured != "30m" {
		t.Fatalf("expected keep_alive=%q from BackendCache override, got %q", "30m", captured)
	}
}

// TestBackendCache_SetOllamaKeepAlive_DoesNotAffectOpenAIProvider verifies
// the cache-level ollama override never leaks onto a cloud OpenAI-compatible
// backend built for a different provider.
func TestBackendCache_SetOllamaKeepAlive_DoesNotAffectOpenAIProvider(t *testing.T) {
	var captured string
	var present bool
	srv := keepAliveCaptureServer(t, &captured, &present)
	defer srv.Close()

	c := NewBackendCache(nil)
	c.SetOllamaKeepAlive("30m")
	b, err := c.For("openai", srv.URL, "sk-test", "gpt-4o")
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if _, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if present {
		t.Fatalf("openai provider must never receive keep_alive from the ollama override, got %q", captured)
	}
}
