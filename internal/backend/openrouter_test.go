package backend_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

func openRouterSSEChunk(content string) string {
	data, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{"delta": map[string]any{"content": content}},
		},
	})
	return "data: " + string(data) + "\n\n"
}

func openRouterSSEUsage(prompt, completion int) string {
	data, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{"delta": map[string]any{"content": ""}, "finish_reason": "stop"},
		},
		"usage": map[string]any{
			"prompt_tokens":     prompt,
			"completion_tokens": completion,
		},
	})
	return "data: " + string(data) + "\n\n"
}

func TestOpenRouterBackend_New(t *testing.T) {
	b := backend.NewOpenRouterBackend(backend.NewKeyResolver("sk-or-test"), "anthropic/claude-sonnet-4-6")
	if b == nil {
		t.Fatal("NewOpenRouterBackend returned nil")
	}
}

func TestOpenRouterBackend_ImplementsInterface(t *testing.T) {
	var _ backend.Backend = backend.NewOpenRouterBackend(backend.NewKeyResolver("key"), "model")
}

func TestOpenRouterBackend_RequestHeaders(t *testing.T) {
	var capturedHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(openRouterSSEChunk("hi")))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	b := backend.NewOpenRouterBackendWithEndpoint(backend.NewKeyResolver("sk-or-mykey"), "anthropic/claude-sonnet-4-6", srv.URL)
	b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "anthropic/claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})

	if got := capturedHeaders.Get("Authorization"); got != "Bearer sk-or-mykey" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer sk-or-mykey")
	}
	if got := capturedHeaders.Get("HTTP-Referer"); got != "https://huginn.dev" {
		t.Errorf("HTTP-Referer = %q, want https://huginn.dev", got)
	}
	if got := capturedHeaders.Get("X-Title"); got != "Huginn" {
		t.Errorf("X-Title = %q, want Huginn", got)
	}
}

func TestOpenRouterBackend_StreamsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(openRouterSSEChunk("hello")))
		w.Write([]byte(openRouterSSEChunk(" world")))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	b := backend.NewOpenRouterBackendWithEndpoint(backend.NewKeyResolver("key"), "model", srv.URL)
	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "anthropic/claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if resp.Content != "hello world" {
		t.Errorf("Content = %q, want %q", resp.Content, "hello world")
	}
}

func TestOpenRouterBackend_TokenUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(openRouterSSEChunk("hi")))
		w.Write([]byte(openRouterSSEUsage(30, 9)))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	b := backend.NewOpenRouterBackendWithEndpoint(backend.NewKeyResolver("key"), "model", srv.URL)
	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "anthropic/claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if resp.PromptTokens != 30 {
		t.Errorf("PromptTokens = %d, want 30", resp.PromptTokens)
	}
	if resp.CompletionTokens != 9 {
		t.Errorf("CompletionTokens = %d, want 9", resp.CompletionTokens)
	}
}

func TestOpenRouterBackend_ProviderRouting_InRequestBody(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(openRouterSSEChunk("ok")))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	b := backend.NewOpenRouterBackendWithEndpoint(backend.NewKeyResolver("key"), "model", srv.URL)
	b.SetProviderOrder([]string{"anthropic", "openai"}, true)
	b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "anthropic/claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})

	var body map[string]any
	json.Unmarshal(captured, &body)
	provider, ok := body["provider"].(map[string]any)
	if !ok {
		t.Fatal("expected 'provider' field in request body")
	}
	order, _ := provider["order"].([]any)
	if len(order) != 2 {
		t.Errorf("provider.order length = %d, want 2", len(order))
	}
	if order[0] != "anthropic" {
		t.Errorf("provider.order[0] = %v, want anthropic", order[0])
	}
	allowFallbacks, _ := provider["allow_fallbacks"].(bool)
	if !allowFallbacks {
		t.Error("expected allow_fallbacks=true")
	}
}

func TestOpenRouterBackend_ContextWindow_Passthrough(t *testing.T) {
	b := backend.NewOpenRouterBackend(backend.NewKeyResolver("key"), "anthropic/claude-sonnet-4-6")
	got := b.ContextWindow()
	if got <= 0 {
		t.Errorf("ContextWindow() = %d, want > 0", got)
	}
}

func TestOpenRouterBackend_Health_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	b := backend.NewOpenRouterBackendWithEndpoint(backend.NewKeyResolver("key"), "model", srv.URL)
	if err := b.Health(context.Background()); err != nil {
		t.Errorf("Health() = %v, want nil", err)
	}
}

func TestOpenRouterBackend_Shutdown_NoOp(t *testing.T) {
	b := backend.NewOpenRouterBackend(backend.NewKeyResolver("key"), "model")
	if err := b.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown() = %v, want nil", err)
	}
}

func TestOpenRouterBackend_HTTP400_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"bad model"}}`))
	}))
	defer srv.Close()
	b := backend.NewOpenRouterBackendWithEndpoint(backend.NewKeyResolver("key"), "model", srv.URL)
	_, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "bad/model",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Error("expected error for 400 response")
	}
}
