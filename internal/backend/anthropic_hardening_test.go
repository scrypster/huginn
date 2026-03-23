package backend_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

// ---------------------------------------------------------------------------
// Typed SSE event struct parsing
// ---------------------------------------------------------------------------

func TestAnthropicBackend_TypedSSE_ContentBlockStart_ToolUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// message_start
		startData, _ := json.Marshal(map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]any{"input_tokens": 5, "output_tokens": 0}}})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", startData)
		// content_block_start with tool_use
		startBlock, _ := json.Marshal(map[string]any{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]any{
				"type": "tool_use",
				"id":   "toolu_typed",
				"name": "bash",
			},
		})
		fmt.Fprintf(w, "event: content_block_start\ndata: %s\n\n", startBlock)
		// input_json_delta
		delta, _ := json.Marshal(map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": `{"cmd":"ls"}`},
		})
		fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", delta)
		// content_block_stop
		stop, _ := json.Marshal(map[string]any{"type": "content_block_stop", "index": 0})
		fmt.Fprintf(w, "event: content_block_stop\ndata: %s\n\n", stop)
		// message_delta
		msgDelta, _ := json.Marshal(map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "tool_use"},
			"usage": map[string]any{"output_tokens": 5},
		})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", msgDelta)
		msgStop, _ := json.Marshal(map[string]any{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", msgStop)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "run ls"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "toolu_typed" {
		t.Errorf("tool call ID = %q, want toolu_typed", resp.ToolCalls[0].ID)
	}
	if resp.ToolCalls[0].Function.Name != "bash" {
		t.Errorf("tool call name = %q, want bash", resp.ToolCalls[0].Function.Name)
	}
}

func TestAnthropicBackend_TypedSSE_TextDelta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, anthropicTextEvent("typed "))
		fmt.Fprint(w, anthropicTextEvent("event"))
		startData, _ := json.Marshal(map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]any{"input_tokens": 3, "output_tokens": 0}}})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", startData)
		deltaData, _ := json.Marshal(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{"output_tokens": 2}})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", deltaData)
		stopData, _ := json.Marshal(map[string]any{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", stopData)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if resp.Content != "typed event" {
		t.Errorf("Content = %q, want %q", resp.Content, "typed event")
	}
}

func TestAnthropicBackend_TypedSSE_ThinkingDelta(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, anthropicThinkingEvent("pondering..."))
		fmt.Fprint(w, anthropicTextEvent("result"))
		startData, _ := json.Marshal(map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]any{"input_tokens": 3, "output_tokens": 0}}})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", startData)
		deltaData, _ := json.Marshal(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{"output_tokens": 2}})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", deltaData)
		stopData, _ := json.Marshal(map[string]any{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", stopData)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	var thoughts []string
	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
		OnEvent: func(e backend.StreamEvent) {
			if e.Type == backend.StreamThought {
				thoughts = append(thoughts, e.Content)
			}
		},
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if resp.Content != "result" {
		t.Errorf("Content = %q, want result", resp.Content)
	}
	if len(thoughts) != 1 || thoughts[0] != "pondering..." {
		t.Errorf("thoughts = %v, want [pondering...]", thoughts)
	}
}

func TestAnthropicBackend_TypedSSE_MessageStartUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, anthropicTextEvent("x"))
		startData, _ := json.Marshal(map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]any{"input_tokens": 42, "output_tokens": 0}}})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", startData)
		deltaData, _ := json.Marshal(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{"output_tokens": 7}})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", deltaData)
		stopData, _ := json.Marshal(map[string]any{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", stopData)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if resp.PromptTokens != 42 {
		t.Errorf("PromptTokens = %d, want 42", resp.PromptTokens)
	}
	if resp.CompletionTokens != 7 {
		t.Errorf("CompletionTokens = %d, want 7", resp.CompletionTokens)
	}
}

func TestAnthropicBackend_TypedSSE_MessageDelta_StopReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, anthropicTextEvent("ok"))
		startData, _ := json.Marshal(map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]any{"input_tokens": 3, "output_tokens": 0}}})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", startData)
		deltaData, _ := json.Marshal(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "max_tokens"}, "usage": map[string]any{"output_tokens": 100}})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", deltaData)
		stopData, _ := json.Marshal(map[string]any{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", stopData)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if resp.DoneReason != "max_tokens" {
		t.Errorf("DoneReason = %q, want max_tokens", resp.DoneReason)
	}
}

// ---------------------------------------------------------------------------
// Health() error on 401 and 403
// ---------------------------------------------------------------------------

func TestAnthropicBackend_Health_401_ReturnsAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("bad-key"), "claude-sonnet-4-6", srv.URL)
	err := b.Health(context.Background())
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	if !strings.Contains(err.Error(), "authentication failed (401)") {
		t.Errorf("error = %q, want to contain 'authentication failed (401)'", err.Error())
	}
}

func TestAnthropicBackend_Health_403_ReturnsForbiddenError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	err := b.Health(context.Background())
	if err == nil {
		t.Fatal("expected error for 403 response")
	}
	if !strings.Contains(err.Error(), "forbidden (403)") {
		t.Errorf("error = %q, want to contain 'forbidden (403)'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// MaxOutputTokens from config used in request
// ---------------------------------------------------------------------------

func TestAnthropicBackend_MaxOutputTokens_Default(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, anthropicTextEvent("ok"))
		startData, _ := json.Marshal(map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]any{"input_tokens": 3, "output_tokens": 0}}})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", startData)
		deltaData, _ := json.Marshal(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{"output_tokens": 1}})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", deltaData)
		stopData, _ := json.Marshal(map[string]any{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", stopData)
	}))
	defer srv.Close()

	// Use a known model (claude-sonnet-4-6 → 16384 from registry)
	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	_, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal(captured, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	maxTokens, ok := body["max_tokens"].(float64)
	if !ok {
		t.Fatal("max_tokens not found in request body")
	}
	// claude-sonnet-4-6 should resolve to 65536 from the model registry (64k output)
	if int(maxTokens) != 65536 {
		t.Errorf("max_tokens = %d, want 65536", int(maxTokens))
	}
}

func TestAnthropicBackend_MaxOutputTokens_ExplicitConfig(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, anthropicTextEvent("ok"))
		startData, _ := json.Marshal(map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]any{"input_tokens": 3, "output_tokens": 0}}})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", startData)
		deltaData, _ := json.Marshal(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{"output_tokens": 1}})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", deltaData)
		stopData, _ := json.Marshal(map[string]any{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", stopData)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	b.SetMaxOutputTokens(32000)
	_, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal(captured, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	maxTokens, ok := body["max_tokens"].(float64)
	if !ok {
		t.Fatal("max_tokens not found in request body")
	}
	if int(maxTokens) != 32000 {
		t.Errorf("max_tokens = %d, want 32000 (explicit config)", int(maxTokens))
	}
}

func TestAnthropicBackend_MaxOutputTokens_UnknownModel_Fallback(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, anthropicTextEvent("ok"))
		startData, _ := json.Marshal(map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]any{"input_tokens": 3, "output_tokens": 0}}})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", startData)
		deltaData, _ := json.Marshal(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{"output_tokens": 1}})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", deltaData)
		stopData, _ := json.Marshal(map[string]any{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", stopData)
	}))
	defer srv.Close()

	// Unknown model with no explicit config → should fallback to 8096
	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "unknown-model-xyz", srv.URL)
	_, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "unknown-model-xyz",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}

	var body map[string]any
	if err := json.Unmarshal(captured, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	maxTokens, ok := body["max_tokens"].(float64)
	if !ok {
		t.Fatal("max_tokens not found in request body")
	}
	if int(maxTokens) != 8096 {
		t.Errorf("max_tokens = %d, want 8096 (default fallback)", int(maxTokens))
	}
}
