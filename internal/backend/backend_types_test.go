package backend_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

func TestStreamEventTypes_Constants(t *testing.T) {
	if backend.StreamText != "text" {
		t.Errorf("StreamText = %q, want %q", backend.StreamText, "text")
	}
	if backend.StreamThought != "thought" {
		t.Errorf("StreamThought = %q, want %q", backend.StreamThought, "thought")
	}
	if backend.StreamDone != "done" {
		t.Errorf("StreamDone = %q, want %q", backend.StreamDone, "done")
	}
}

func TestChatResponse_HasTokenFields(t *testing.T) {
	resp := backend.ChatResponse{
		Content:          "hello",
		PromptTokens:     10,
		CompletionTokens: 5,
	}
	if resp.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d, want 10", resp.PromptTokens)
	}
	if resp.CompletionTokens != 5 {
		t.Errorf("CompletionTokens = %d, want 5", resp.CompletionTokens)
	}
}

func TestChatRequest_OnEvent_Field(t *testing.T) {
	var received []backend.StreamEvent
	req := backend.ChatRequest{
		Model: "test",
		OnEvent: func(e backend.StreamEvent) {
			received = append(received, e)
		},
	}
	req.OnEvent(backend.StreamEvent{Type: backend.StreamText, Content: "hello"})
	req.OnEvent(backend.StreamEvent{Type: backend.StreamDone})
	if len(received) != 2 {
		t.Fatalf("expected 2 events, got %d", len(received))
	}
	if received[0].Type != backend.StreamText {
		t.Errorf("event[0].Type = %q, want StreamText", received[0].Type)
	}
	if received[0].Content != "hello" {
		t.Errorf("event[0].Content = %q, want hello", received[0].Content)
	}
}

func TestExternalBackend_ContextWindow_KnownModels(t *testing.T) {
	tests := []struct {
		model string
		want  int
	}{
		{"gpt-4o", 128_000},
		{"gpt-4o-mini", 128_000},
		{"gpt-4-turbo", 128_000},
		{"gpt-3.5-turbo", 16_385},
		{"o1", 200_000},
		{"o1-mini", 128_000},
	}
	for _, tt := range tests {
		b := backend.NewExternalBackend("http://localhost:11434")
		b.SetModel(tt.model)
		got := b.ContextWindow()
		if got != tt.want {
			t.Errorf("ContextWindow(%q) = %d, want %d", tt.model, got, tt.want)
		}
	}
}

func TestExternalBackend_ContextWindow_UnknownModel_Returns8k(t *testing.T) {
	b := backend.NewExternalBackend("http://localhost:11434")
	b.SetModel("some-unknown-model-xyz")
	got := b.ContextWindow()
	if got != 8192 {
		t.Errorf("ContextWindow(unknown) = %d, want 8192", got)
	}
}

func TestExternalBackend_ParsesTokenUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		usageChunk := map[string]any{
			"choices": []map[string]any{
				{
					"delta":         map[string]any{"content": "hello"},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]any{
				"prompt_tokens":     42,
				"completion_tokens": 7,
			},
		}
		data, _ := json.Marshal(usageChunk)
		w.Write([]byte("data: " + string(data) + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	b := backend.NewExternalBackend(srv.URL)
	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "gpt-4o",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}
	if resp.PromptTokens != 42 {
		t.Errorf("PromptTokens = %d, want 42", resp.PromptTokens)
	}
	if resp.CompletionTokens != 7 {
		t.Errorf("CompletionTokens = %d, want 7", resp.CompletionTokens)
	}
}

func TestExternalBackend_OnEvent_ReceivesTextEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, tok := range []string{"foo", " bar"} {
			chunk := map[string]any{
				"choices": []map[string]any{
					{"delta": map[string]any{"content": tok}},
				},
			}
			data, _ := json.Marshal(chunk)
			w.Write([]byte("data: " + string(data) + "\n\n"))
		}
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	b := backend.NewExternalBackend(srv.URL)
	var events []backend.StreamEvent
	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "gpt-4o",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
		OnEvent: func(e backend.StreamEvent) {
			events = append(events, e)
		},
	})
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}
	if resp.Content != "foo bar" {
		t.Errorf("Content = %q, want %q", resp.Content, "foo bar")
	}
	textCount := 0
	for _, e := range events {
		if e.Type == backend.StreamText {
			textCount++
		}
	}
	if textCount != 2 {
		t.Errorf("expected 2 StreamText events, got %d", textCount)
	}
}

func TestExternalBackend_OnToken_StillWorks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := map[string]any{
			"choices": []map[string]any{
				{"delta": map[string]any{"content": "tok1"}},
			},
		}
		data, _ := json.Marshal(chunk)
		w.Write([]byte("data: " + string(data) + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	b := backend.NewExternalBackend(srv.URL)
	var tokens []string
	_, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "gpt-4o",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
		OnToken:  func(s string) { tokens = append(tokens, s) },
	})
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}
	if len(tokens) != 1 || tokens[0] != "tok1" {
		t.Errorf("OnToken got %v, want [tok1]", tokens)
	}
}

// Test that both OnEvent and OnToken fire when both are set
func TestExternalBackend_OnEvent_And_OnToken_BothFire(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := map[string]any{
			"choices": []map[string]any{
				{"delta": map[string]any{"content": "hello"}},
			},
		}
		data, _ := json.Marshal(chunk)
		w.Write([]byte("data: " + string(data) + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	b := backend.NewExternalBackend(srv.URL)
	var events []backend.StreamEvent
	var tokens []string
	_, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "gpt-4o",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
		OnEvent:  func(e backend.StreamEvent) { events = append(events, e) },
		OnToken:  func(s string) { tokens = append(tokens, s) },
	})
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}
	if len(tokens) != 1 || tokens[0] != "hello" {
		t.Errorf("OnToken got %v, want [hello]", tokens)
	}
	textEvents := 0
	for _, e := range events {
		if e.Type == backend.StreamText {
			textEvents++
		}
	}
	if textEvents != 1 {
		t.Errorf("expected 1 StreamText event, got %d", textEvents)
	}
}

// Test that StreamDone is emitted after stream completes
func TestExternalBackend_OnEvent_EmitsStreamDone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := map[string]any{
			"choices": []map[string]any{
				{"delta": map[string]any{"content": "hi"}},
			},
		}
		data, _ := json.Marshal(chunk)
		w.Write([]byte("data: " + string(data) + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	b := backend.NewExternalBackend(srv.URL)
	var doneCount int
	_, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "gpt-4o",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
		OnEvent:  func(e backend.StreamEvent) {
			if e.Type == backend.StreamDone {
				doneCount++
			}
		},
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if doneCount != 1 {
		t.Errorf("expected 1 StreamDone event, got %d", doneCount)
	}
}
