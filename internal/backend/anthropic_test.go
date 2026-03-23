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

func anthropicTextEvent(text string) string {
	data, _ := json.Marshal(map[string]any{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]any{"type": "text_delta", "text": text},
	})
	return "event: content_block_delta\ndata: " + string(data) + "\n\n"
}

func anthropicThinkingEvent(text string) string {
	data, _ := json.Marshal(map[string]any{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]any{"type": "thinking_delta", "thinking": text},
	})
	return "event: content_block_delta\ndata: " + string(data) + "\n\n"
}

func TestAnthropicBackend_New(t *testing.T) {
	b := backend.NewAnthropicBackend(backend.NewKeyResolver("sk-ant-test"), "claude-sonnet-4-6")
	if b == nil {
		t.Fatal("NewAnthropicBackend returned nil")
	}
}

func TestAnthropicBackend_ImplementsInterface(t *testing.T) {
	var _ backend.Backend = backend.NewAnthropicBackend(backend.NewKeyResolver("key"), "claude-sonnet-4-6")
}

func TestAnthropicBackend_ContextWindow_KnownModels(t *testing.T) {
	tests := []struct {
		model string
		want  int
	}{
		{"claude-opus-4-6", 200_000},
		{"claude-sonnet-4-6", 200_000},
		{"claude-3-5-sonnet-20241022", 200_000},
		{"claude-3-opus-20240229", 200_000},
		{"claude-3-haiku-20240307", 200_000},
	}
	for _, tt := range tests {
		b := backend.NewAnthropicBackend(backend.NewKeyResolver("key"), tt.model)
		got := b.ContextWindow()
		if got != tt.want {
			t.Errorf("ContextWindow(%q) = %d, want %d", tt.model, got, tt.want)
		}
	}
}

func TestAnthropicBackend_ContextWindow_Unknown(t *testing.T) {
	b := backend.NewAnthropicBackend(backend.NewKeyResolver("key"), "claude-future-model")
	got := b.ContextWindow()
	if got != 200_000 {
		t.Errorf("ContextWindow(unknown) = %d, want 200000", got)
	}
}

func TestAnthropicBackend_Health_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	if err := b.Health(context.Background()); err != nil {
		t.Errorf("Health() = %v, want nil", err)
	}
}

func TestAnthropicBackend_Health_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	if err := b.Health(context.Background()); err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestAnthropicBackend_SystemMessageExtracted(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, anthropicTextEvent("ok"))
		// message_start
		startData, _ := json.Marshal(map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]any{"input_tokens": 5, "output_tokens": 0}}})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", startData)
		// message_delta
		deltaData, _ := json.Marshal(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{"output_tokens": 1}})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", deltaData)
		stopData, _ := json.Marshal(map[string]any{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", stopData)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	_, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model: "claude-sonnet-4-6",
		Messages: []backend.Message{
			{Role: "system", Content: "You are a coding assistant."},
			{Role: "user", Content: "hello"},
		},
	})
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(captured, &body); err != nil {
		t.Fatalf("unmarshal request body: %v", err)
	}
	sys, ok := body["system"]
	if !ok {
		t.Fatal("expected top-level 'system' field in Anthropic request")
	}
	if sys != "You are a coding assistant." {
		t.Errorf("system = %v, want %q", sys, "You are a coding assistant.")
	}
	msgs, _ := body["messages"].([]any)
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm["role"] == "system" {
			t.Error("system message must not appear in messages array")
		}
	}
}

func TestAnthropicBackend_ToolResultMessage(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, anthropicTextEvent("done"))
		startData, _ := json.Marshal(map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]any{"input_tokens": 5, "output_tokens": 0}}})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", startData)
		deltaData, _ := json.Marshal(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{"output_tokens": 1}})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", deltaData)
		stopData, _ := json.Marshal(map[string]any{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", stopData)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	_, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model: "claude-sonnet-4-6",
		Messages: []backend.Message{
			{Role: "user", Content: "run tool"},
			{
				Role: "assistant",
				ToolCalls: []backend.ToolCall{
					{ID: "tc_001", Function: backend.ToolCallFunction{Name: "read_file", Arguments: map[string]any{"path": "main.go"}}},
				},
			},
			{Role: "tool", Content: "file contents here", ToolName: "read_file", ToolCallID: "tc_001"},
		},
	})
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}

	var body map[string]any
	json.Unmarshal(captured, &body)
	msgs, _ := body["messages"].([]any)

	foundToolResult := false
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm["role"] != "user" {
			continue
		}
		contentArr, ok := mm["content"].([]any)
		if !ok {
			continue
		}
		for _, c := range contentArr {
			cm, _ := c.(map[string]any)
			if cm["type"] == "tool_result" {
				foundToolResult = true
				if cm["tool_use_id"] != "tc_001" {
					t.Errorf("tool_result.tool_use_id = %v, want tc_001", cm["tool_use_id"])
				}
			}
		}
	}
	if !foundToolResult {
		t.Error("expected tool_result content block in user message")
	}
}

func TestAnthropicBackend_AssistantToolUse_ContentBlocks(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, anthropicTextEvent("ok"))
		startData, _ := json.Marshal(map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]any{"input_tokens": 5, "output_tokens": 0}}})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", startData)
		deltaData, _ := json.Marshal(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{"output_tokens": 1}})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", deltaData)
		stopData, _ := json.Marshal(map[string]any{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", stopData)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	_, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model: "claude-sonnet-4-6",
		Messages: []backend.Message{
			{Role: "user", Content: "call tool"},
			{
				Role: "assistant",
				ToolCalls: []backend.ToolCall{
					{ID: "tc_002", Function: backend.ToolCallFunction{Name: "bash", Arguments: map[string]any{"cmd": "ls"}}},
				},
			},
			{Role: "tool", Content: "file1.go", ToolName: "bash", ToolCallID: "tc_002"},
		},
	})
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}

	var body map[string]any
	json.Unmarshal(captured, &body)
	msgs, _ := body["messages"].([]any)

	foundToolUse := false
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm["role"] != "assistant" {
			continue
		}
		contentArr, ok := mm["content"].([]any)
		if !ok {
			continue
		}
		for _, c := range contentArr {
			cm, _ := c.(map[string]any)
			if cm["type"] == "tool_use" {
				foundToolUse = true
				if cm["id"] != "tc_002" {
					t.Errorf("tool_use.id = %v, want tc_002", cm["id"])
				}
				if cm["name"] != "bash" {
					t.Errorf("tool_use.name = %v, want bash", cm["name"])
				}
			}
		}
	}
	if !foundToolUse {
		t.Error("expected tool_use content block in assistant message")
	}
}

func TestAnthropicBackend_ToolDefinitions_UseInputSchema(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, anthropicTextEvent("ok"))
		startData, _ := json.Marshal(map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]any{"input_tokens": 5, "output_tokens": 0}}})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", startData)
		deltaData, _ := json.Marshal(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{"output_tokens": 1}})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", deltaData)
		stopData, _ := json.Marshal(map[string]any{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", stopData)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	_, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model: "claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
		Tools: []backend.Tool{
			{
				Type: "function",
				Function: backend.ToolFunction{
					Name:        "read_file",
					Description: "Read a file",
					Parameters: backend.ToolParameters{
						Type: "object",
						Properties: map[string]backend.ToolProperty{
							"path": {Type: "string", Description: "File path"},
						},
						Required: []string{"path"},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}

	var body map[string]any
	json.Unmarshal(captured, &body)
	tools, _ := body["tools"].([]any)
	if len(tools) == 0 {
		t.Fatal("expected tools in request body")
	}
	tool, _ := tools[0].(map[string]any)
	if tool["name"] != "read_file" {
		t.Errorf("tool.name = %v, want read_file", tool["name"])
	}
	if _, ok := tool["input_schema"]; !ok {
		t.Error("expected input_schema field in Anthropic tool definition")
	}
	if _, ok := tool["function"]; ok {
		t.Error("must not have 'function' wrapper in Anthropic tool definition")
	}
}

func TestAnthropicBackend_StreamsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, anthropicTextEvent("Hello"))
		fmt.Fprint(w, anthropicTextEvent(", world"))
		startData, _ := json.Marshal(map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]any{"input_tokens": 10, "output_tokens": 0}}})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", startData)
		deltaData, _ := json.Marshal(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{"output_tokens": 4}})
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
	if resp.Content != "Hello, world" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello, world")
	}
}

func TestAnthropicBackend_ThinkingTokens_EmittedAsThoughtEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, anthropicThinkingEvent("let me think..."))
		fmt.Fprint(w, anthropicTextEvent("answer"))
		startData, _ := json.Marshal(map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]any{"input_tokens": 10, "output_tokens": 0}}})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", startData)
		deltaData, _ := json.Marshal(map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{"output_tokens": 5}})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", deltaData)
		stopData, _ := json.Marshal(map[string]any{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", stopData)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	var events []backend.StreamEvent
	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
		OnEvent:  func(e backend.StreamEvent) { events = append(events, e) },
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if resp.Content != "answer" {
		t.Errorf("Content = %q, want answer", resp.Content)
	}
	var thoughtEvents, textEvents int
	for _, e := range events {
		switch e.Type {
		case backend.StreamThought:
			thoughtEvents++
			if e.Content != "let me think..." {
				t.Errorf("thought content = %q, want %q", e.Content, "let me think...")
			}
		case backend.StreamText:
			textEvents++
		}
	}
	if thoughtEvents != 1 {
		t.Errorf("expected 1 thought event, got %d", thoughtEvents)
	}
	if textEvents != 1 {
		t.Errorf("expected 1 text event, got %d", textEvents)
	}
}

func TestAnthropicBackend_TokenUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, anthropicTextEvent("hi"))
		startData, _ := json.Marshal(map[string]any{
			"type":    "message_start",
			"message": map[string]any{"usage": map[string]any{"input_tokens": 25, "output_tokens": 0}},
		})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", startData)
		deltaData, _ := json.Marshal(map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn"},
			"usage": map[string]any{"output_tokens": 8},
		})
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
	if resp.PromptTokens != 25 {
		t.Errorf("PromptTokens = %d, want 25", resp.PromptTokens)
	}
	if resp.CompletionTokens != 8 {
		t.Errorf("CompletionTokens = %d, want 8", resp.CompletionTokens)
	}
}

func TestAnthropicBackend_RequestHeaders(t *testing.T) {
	var capturedHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
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

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("sk-ant-mykey"), "claude-sonnet-4-6", srv.URL)
	b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})

	if got := capturedHeaders.Get("x-api-key"); got != "sk-ant-mykey" {
		t.Errorf("x-api-key = %q, want sk-ant-mykey", got)
	}
	if got := capturedHeaders.Get("anthropic-version"); got != "2023-06-01" {
		t.Errorf("anthropic-version = %q, want 2023-06-01", got)
	}
	if got := capturedHeaders.Get("anthropic-beta"); !strings.Contains(got, "interleaved-thinking") {
		t.Errorf("anthropic-beta = %q, want to contain interleaved-thinking", got)
	}
}

func TestAnthropicBackend_Shutdown_NoOp(t *testing.T) {
	b := backend.NewAnthropicBackend(backend.NewKeyResolver("key"), "claude-sonnet-4-6")
	if err := b.Shutdown(context.Background()); err != nil {
		t.Errorf("Shutdown() = %v, want nil", err)
	}
}

func TestAnthropicBackend_HTTP401_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"type":"authentication_error","message":"invalid api key"}}`))
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("bad-key"), "claude-sonnet-4-6", srv.URL)
	_, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Error("expected error for 401 response")
	}
}

func TestAnthropicBackend_HTTP529_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(529)
		w.Write([]byte(`{"error":{"type":"overloaded_error","message":"overloaded"}}`))
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	_, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Error("expected error for 529 overloaded response")
	}
}

func TestAnthropicBackend_ToolCallResponse_ParsedFromSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// message_start
		startData, _ := json.Marshal(map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]any{"input_tokens": 10, "output_tokens": 0}}})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", startData)
		// content_block_start with tool_use
		startBlock, _ := json.Marshal(map[string]any{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]any{
				"type": "tool_use",
				"id":   "toolu_01ABC",
				"name": "read_file",
			},
		})
		fmt.Fprintf(w, "event: content_block_start\ndata: %s\n\n", startBlock)
		// input_json_delta (partial JSON)
		delta1, _ := json.Marshal(map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": `{"path":"`},
		})
		fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", delta1)
		delta2, _ := json.Marshal(map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "input_json_delta", "partial_json": `main.go"}`},
		})
		fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", delta2)
		// content_block_stop
		stop, _ := json.Marshal(map[string]any{"type": "content_block_stop", "index": 0})
		fmt.Fprintf(w, "event: content_block_stop\ndata: %s\n\n", stop)
		// message_delta with stop_reason tool_use
		msgDelta, _ := json.Marshal(map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "tool_use"},
			"usage": map[string]any{"output_tokens": 10},
		})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", msgDelta)
		msgStop, _ := json.Marshal(map[string]any{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", msgStop)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "read main.go"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "toolu_01ABC" {
		t.Errorf("ToolCall.ID = %q, want toolu_01ABC", tc.ID)
	}
	if tc.Function.Name != "read_file" {
		t.Errorf("ToolCall.Function.Name = %q, want read_file", tc.Function.Name)
	}
	path, _ := tc.Function.Arguments["path"].(string)
	if path != "main.go" {
		t.Errorf("ToolCall.Function.Arguments[path] = %q, want main.go", path)
	}
	if resp.DoneReason != "tool_use" {
		t.Errorf("DoneReason = %q, want tool_use", resp.DoneReason)
	}
}
