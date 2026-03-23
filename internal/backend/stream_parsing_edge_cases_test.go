package backend

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAnthropicBackend_MalformedSSEEvents tests edge cases in SSE parsing.
func TestAnthropicBackend_MalformedSSEEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{
			name:    "valid text_delta",
			body:    "event: content_block_delta\ndata: {\"index\": 0, \"delta\": {\"type\": \"text_delta\", \"text\": \"hello\"}}\n\n",
			wantErr: false,
		},
		{
			name:    "malformed JSON in data line",
			body:    "event: content_block_delta\ndata: {invalid json}\n\n",
			wantErr: false, // Should skip gracefully per slog.Debug
		},
		{
			name:    "missing data: prefix",
			body:    "event: content_block_delta\n{\"index\": 0}\n\n",
			wantErr: false, // Should skip non-data lines
		},
		{
			name:    "empty data payload",
			body:    "event: content_block_delta\ndata: \n\n",
			wantErr: false, // Should skip empty data
		},
		{
			name:    "mixed valid and invalid events",
			body:    "event: content_block_delta\ndata: {\"index\": 0, \"delta\": {\"type\": \"text_delta\", \"text\": \"a\"}}\n\nevent: content_block_delta\ndata: {bad json}\n\nevent: content_block_delta\ndata: {\"index\": 0, \"delta\": {\"type\": \"text_delta\", \"text\": \"b\"}}\n\n",
			wantErr: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, tc.body)
			}))
			t.Cleanup(srv.Close)

			b := NewAnthropicBackendWithEndpoint(
				func() (string, error) { return "test-key", nil },
				"claude-3-5-sonnet-20241022",
				srv.URL,
			)

			_, err := b.ChatCompletion(context.Background(), ChatRequest{
				Model:    "claude-3-5-sonnet-20241022",
				Messages: []Message{{Role: "user", Content: "test"}},
			})

			if (err != nil) != tc.wantErr {
				t.Errorf("ChatCompletion err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

// TestAnthropicBackend_ToolCallWithMalformedJSON tests tool call parsing with invalid JSON.
func TestAnthropicBackend_ToolCallWithMalformedJSON(t *testing.T) {
	t.Parallel()

	sseStream := `event: message_start
data: {"message":{"usage":{"input_tokens":10,"output_tokens":0}}}

event: content_block_start
data: {"index":0,"content_block":{"type":"tool_use","id":"call_123","name":"my_tool"}}

event: content_block_delta
data: {"index":0,"delta":{"type":"input_json_delta","partial_json":"{\"arg\": \"val"}}

event: content_block_delta
data: {"index":0,"delta":{"type":"input_json_delta","partial_json":"ue\", \"bad"}}

event: content_block_stop
data: {"index":0}

event: message_delta
data: {"delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}

event: message_stop
data: {}
`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseStream)
	}))
	t.Cleanup(srv.Close)

	b := NewAnthropicBackendWithEndpoint(
		func() (string, error) { return "test-key", nil },
		"claude-3-5-sonnet-20241022",
		srv.URL,
	)

	resp, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "claude-3-5-sonnet-20241022",
		Messages: []Message{{Role: "user", Content: "test"}},
	})

	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}

	// Tool call should NOT be dropped — truncated JSON falls back to empty args so
	// zero-arg tools (e.g. muninn_where_left_off) still execute rather than silently
	// disappearing. Tools that require parameters will fail at the server level with
	// a descriptive error the LLM can handle; silent drops cannot be recovered.
	if len(resp.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call (truncated JSON falls back to empty args), got %d", len(resp.ToolCalls))
	}
	if len(resp.ToolCalls) == 1 {
		if resp.ToolCalls[0].Function.Name != "my_tool" {
			t.Errorf("expected tool name 'my_tool', got %q", resp.ToolCalls[0].Function.Name)
		}
		if len(resp.ToolCalls[0].Function.Arguments) != 0 {
			t.Errorf("expected empty args fallback, got %v", resp.ToolCalls[0].Function.Arguments)
		}
	}
	// ParseErrors should be empty — the truncation is handled gracefully via fallback.
	if len(resp.ParseErrors) != 0 {
		t.Errorf("expected no ParseErrors with graceful fallback, got %v", resp.ParseErrors)
	}
}

// TestExternalBackend_StreamingParseEdgeCases tests ExternalBackend SSE parsing edge cases.
func TestExternalBackend_StreamingParseEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		wantErr bool
		wantContent string
	}{
		{
			name: "no [DONE] marker, has content",
			body: `data: {"choices":[{"delta":{"content":"hello"}}]}
data: {"choices":[{"delta":{"content":" world"}}]}
`,
			wantErr: false,
			wantContent: "hello world",
		},
		{
			name: "incomplete stream with [DONE]",
			body: `data: {"choices":[{"delta":{"content":"partial"}}]}
data: [DONE]
`,
			wantErr: false,
			wantContent: "partial",
		},
		{
			name: "final chunk with usage, no content",
			body: `data: {"choices":[{"delta":{"content":"test"}}]}
data: {"usage":{"prompt_tokens":5,"completion_tokens":3}}
data: [DONE]
`,
			wantErr: false,
			wantContent: "test",
		},
		{
			name: "empty stream, no [DONE]",
			body: "",
			wantErr: true,
		},
		{
			name: "malformed JSON chunk, continues parsing",
			body: `data: {"choices":[{"delta":{"content":"a"}}]}
data: {not valid json}
data: {"choices":[{"delta":{"content":"b"}}]}
data: [DONE]
`,
			wantErr: false,
			wantContent: "ab",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				fmt.Fprint(w, tc.body)
			}))
			t.Cleanup(srv.Close)

			b := NewExternalBackend(srv.URL)
			resp, err := b.ChatCompletion(context.Background(), ChatRequest{
				Model:    "test-model",
				Messages: []Message{{Role: "user", Content: "test"}},
			})

			if (err != nil) != tc.wantErr {
				t.Errorf("ChatCompletion err=%v, wantErr=%v", err, tc.wantErr)
			}
			if !tc.wantErr && resp.Content != tc.wantContent {
				t.Errorf("Content: got %q, want %q", resp.Content, tc.wantContent)
			}
		})
	}
}

// TestExternalBackend_ToolCall_MultiChunk tests tool call accumulation across chunks.
func TestExternalBackend_ToolCall_MultiChunk(t *testing.T) {
	t.Parallel()

	// Tool call fragmented across multiple chunks
	sseStream := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"my_func","arguments":"{\"arg"}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\":\"val"}}]}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ue\"}"}}]}}]}
data: [DONE]
`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseStream)
	}))
	t.Cleanup(srv.Close)

	b := NewExternalBackend(srv.URL)
	resp, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "test"}},
	})

	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}

	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}

	tc := resp.ToolCalls[0]
	if tc.Function.Name != "my_func" {
		t.Errorf("tool name: got %q, want my_func", tc.Function.Name)
	}
	if id, ok := tc.Function.Arguments["arg"]; !ok || id != "value" {
		t.Errorf("tool arguments: got %v, want {\"arg\":\"value\"}", tc.Function.Arguments)
	}
}

// TestExternalBackend_PrematureEOF tests stream ending without [DONE] marker.
func TestExternalBackend_PrematureEOF(t *testing.T) {
	t.Parallel()

	// Stream ends without [DONE] and with no data
	sseStream := ""

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, sseStream)
	}))
	t.Cleanup(srv.Close)

	b := NewExternalBackend(srv.URL)
	_, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "test"}},
	})

	if err == nil {
		t.Error("expected error for premature EOF, got nil")
	}
	if !strings.Contains(err.Error(), "SSE stream ended without data") {
		t.Errorf("error message: got %q, want to contain 'SSE stream ended without data'", err.Error())
	}
}
