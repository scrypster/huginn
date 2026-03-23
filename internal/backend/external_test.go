package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestExternalBackend_ChatCompletion_streaming(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\" world\"},\"finish_reason\":null}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	b := NewExternalBackend(srv.URL)
	var tokens []string
	resp, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
		OnToken:  func(t string) { tokens = append(tokens, t) },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hello world" {
		t.Errorf("expected 'hello world', got %q", resp.Content)
	}
	if strings.Join(tokens, "") != "hello world" {
		t.Errorf("streaming tokens mismatch: %v", tokens)
	}
	if resp.DoneReason != "stop" {
		t.Errorf("expected stop, got %q", resp.DoneReason)
	}
	_ = json.Marshal // suppress unused import if needed
}

func TestExternalBackend_Health(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	b := NewExternalBackend(srv.URL)
	if err := b.Health(context.Background()); err != nil {
		t.Fatalf("unexpected health error: %v", err)
	}
}

// TestParseSSE_ToolCallArgumentsParsed verifies that arguments streamed across
// multiple chunks are concatenated and parsed into a map.
func TestParseSSE_ToolCallArgumentsParsed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Chunk 1: tool call header, empty arguments
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file","arguments":""}}]}}]}`+"\n\n")
		// Chunk 2: first argument fragment
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"file_path\":"}}]}}]}`+"\n\n")
		// Chunk 3: second argument fragment completing the JSON
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"main.go\"}"}}]}}]}`+"\n\n")
		// Final: finish reason
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	b := NewExternalBackend(srv.URL)
	resp, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "read a file"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.Function.Name != "read_file" {
		t.Errorf("expected function name 'read_file', got %q", tc.Function.Name)
	}
	if tc.Function.Arguments == nil {
		t.Fatal("expected non-nil Arguments")
	}
	fp, ok := tc.Function.Arguments["file_path"]
	if !ok {
		t.Fatal("expected 'file_path' key in Arguments")
	}
	if fp != "main.go" {
		t.Errorf("expected file_path 'main.go', got %v", fp)
	}
	if resp.DoneReason != "tool_calls" {
		t.Errorf("expected DoneReason 'tool_calls', got %q", resp.DoneReason)
	}
}

// TestParseSSE_ToolCallNoArguments verifies that a tool call with empty arguments
// results in nil Arguments (no error).
func TestParseSSE_ToolCallNoArguments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_2","type":"function","function":{"name":"no_args","arguments":""}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	b := NewExternalBackend(srv.URL)
	resp, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "do nothing"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Function.Arguments != nil {
		t.Errorf("expected nil Arguments for empty args, got %v", resp.ToolCalls[0].Function.Arguments)
	}
}

// TestParseSSE_ToolCallInvalidArguments verifies that malformed accumulated
// argument JSON returns an error containing "parse tool".
func TestParseSSE_ToolCallInvalidArguments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_3","type":"function","function":{"name":"bad_tool","arguments":"{invalid json"}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	b := NewExternalBackend(srv.URL)
	_, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "bad"}},
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON arguments, got nil")
	}
	if !strings.Contains(err.Error(), "parse tool") {
		t.Errorf("expected error to contain 'parse tool', got: %v", err)
	}
}

// TestParseSSE_MultipleToolCalls verifies that two tool calls with different
// indices are both returned with the correct arguments.
func TestParseSSE_MultipleToolCalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Index 0: read_file
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"read_file","arguments":""}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"file_path\":\"a.go\"}"}}]}}]}`+"\n\n")
		// Index 1: bash
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"bash","arguments":""}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"command\":\"ls\"}"}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	b := NewExternalBackend(srv.URL)
	resp, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "do two things"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(resp.ToolCalls))
	}

	tc0 := resp.ToolCalls[0]
	if tc0.Function.Name != "read_file" {
		t.Errorf("expected tool_calls[0].Function.Name = 'read_file', got %q", tc0.Function.Name)
	}
	if tc0.Function.Arguments == nil {
		t.Fatal("expected non-nil Arguments for tool_calls[0]")
	}
	if tc0.Function.Arguments["file_path"] != "a.go" {
		t.Errorf("expected file_path='a.go', got %v", tc0.Function.Arguments["file_path"])
	}

	tc1 := resp.ToolCalls[1]
	if tc1.Function.Name != "bash" {
		t.Errorf("expected tool_calls[1].Function.Name = 'bash', got %q", tc1.Function.Name)
	}
	if tc1.Function.Arguments == nil {
		t.Fatal("expected non-nil Arguments for tool_calls[1]")
	}
	if tc1.Function.Arguments["command"] != "ls" {
		t.Errorf("expected command='ls', got %v", tc1.Function.Arguments["command"])
	}
}

// TestParseSSE_MixedContentAndToolCall verifies that a response containing both
// text content and a tool call returns both correctly.
func TestParseSSE_MixedContentAndToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"I will read the file"}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_mix","type":"function","function":{"name":"read_file","arguments":"{\"file_path\":\"x.go\"}"}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	b := NewExternalBackend(srv.URL)
	resp, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "read it"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content == "" {
		t.Error("expected non-empty Content")
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
}

// TestParseSSE_MalformedChunkSkipped verifies that a malformed JSON chunk is
// silently skipped and valid chunks on either side are still processed.
func TestParseSSE_MalformedChunkSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"hello"}}]}`+"\n\n")
		fmt.Fprint(w, "data: {INVALID\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":" world"}}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	b := NewExternalBackend(srv.URL)
	resp, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hello world" {
		t.Errorf("expected 'hello world', got %q", resp.Content)
	}
}

// TestParseSSE_PrematureEOF verifies that a server closing the connection before
// sending [DONE] with no data returns an error.
func TestParseSSE_PrematureEOF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Close immediately with no data
	}))
	defer srv.Close()

	b := NewExternalBackend(srv.URL)
	_, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for premature EOF, got nil")
	}
}

// TestChatCompletion_HTTP500 verifies that a 500 response from the server
// returns an error containing the status code.
func TestChatCompletion_HTTP500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	b := NewExternalBackend(srv.URL)
	_, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test-model",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to contain '500', got: %v", err)
	}
}

// TestHealth_ServerError verifies that a health check against a 500-returning
// server returns an error.
func TestHealth_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	b := NewExternalBackend(srv.URL)
	err := b.Health(context.Background())
	if err == nil {
		t.Fatal("expected error for health check against 500 server, got nil")
	}
}

// TestShutdown_NoOp verifies that Shutdown returns nil for ExternalBackend.
func TestShutdown_NoOp(t *testing.T) {
	b := NewExternalBackend("http://localhost:11434")
	if err := b.Shutdown(context.Background()); err != nil {
		t.Errorf("expected nil from Shutdown, got: %v", err)
	}
}

// TestNewExternalBackend_TrailingSlashTrimmed verifies that trailing slashes
// in the endpoint are stripped so URLs are formed correctly.
func TestNewExternalBackend_TrailingSlashTrimmed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s (double slash?)", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"ok"}}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	// Pass endpoint with trailing slash
	b := NewExternalBackend(srv.URL + "/")
	resp, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("expected 'ok', got %q", resp.Content)
	}
}

// TestChatCompletion_ContextCancelled verifies that a cancelled context
// causes ChatCompletion to return an error.
func TestChatCompletion_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the client disconnects
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	b := NewExternalBackend(srv.URL)

	errCh := make(chan error, 1)
	go func() {
		_, err := b.ChatCompletion(ctx, ChatRequest{
			Model:    "test",
			Messages: []Message{{Role: "user", Content: "hi"}},
		})
		errCh <- err
	}()

	cancel()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error after context cancellation")
		}
	}
}

// TestBuildRequest_ToolCallSerialization verifies that tool calls in assistant
// messages are correctly serialized.
func TestBuildRequest_ToolCallSerialization(t *testing.T) {
	b := NewExternalBackend("http://localhost")
	req := ChatRequest{
		Model: "test",
		Messages: []Message{
			{
				Role: "assistant",
				ToolCalls: []ToolCall{
					{
						ID: "tc1",
						Function: ToolCallFunction{
							Name:      "read_file",
							Arguments: map[string]any{"file_path": "main.go"},
						},
					},
				},
			},
		},
	}
	data, err := b.buildRequest(req)
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	// Verify it's valid JSON.
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("buildRequest produced invalid JSON: %v", err)
	}
	messages := parsed["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	msg := messages[0].(map[string]any)
	toolCalls, ok := msg["tool_calls"].([]any)
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("expected tool_calls array with 1 entry, got %v", msg["tool_calls"])
	}
}

// TestBuildRequest_ToolMessageSerialization verifies that tool result messages
// include tool_call_id and name fields.
func TestBuildRequest_ToolMessageSerialization(t *testing.T) {
	b := NewExternalBackend("http://localhost")
	req := ChatRequest{
		Model: "test",
		Messages: []Message{
			{
				Role:       "tool",
				Content:    "the result",
				ToolCallID: "tc1",
				ToolName:   "read_file",
			},
		},
	}
	data, err := b.buildRequest(req)
	if err != nil {
		t.Fatalf("buildRequest: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	messages := parsed["messages"].([]any)
	msg := messages[0].(map[string]any)
	if msg["tool_call_id"] != "tc1" {
		t.Errorf("expected tool_call_id=tc1, got %v", msg["tool_call_id"])
	}
	if msg["name"] != "read_file" {
		t.Errorf("expected name=read_file, got %v", msg["name"])
	}
}

// TestHealth_404 verifies that a 404 response does not fail the health check
// (only 5xx responses are treated as errors).
func TestHealth_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	b := NewExternalBackend(srv.URL)
	err := b.Health(context.Background())
	if err != nil {
		t.Errorf("expected nil for 404 health check (only 5xx fail), got: %v", err)
	}
}

// TestParseSSE_OnlyDONE verifies that a stream with only "[DONE]" and no
// data previously received does NOT return an error (sawDone=true satisfies check).
func TestParseSSE_OnlyDONE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// No actual content chunks — just DONE
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	b := NewExternalBackend(srv.URL)
	// sawDone=true but no content and no tool calls.
	// The current implementation returns error only when !sawDone && no data.
	// With sawDone=true it should succeed with empty content.
	resp, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error for DONE-only stream: %v", err)
	}
	if resp.Content != "" {
		t.Errorf("expected empty content, got %q", resp.Content)
	}
}

// TestParseSSE_StreamingWithNoOnToken verifies collection mode works (no OnToken callback).
func TestParseSSE_StreamingWithNoOnToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"collected"}}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	b := NewExternalBackend(srv.URL)
	resp, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test",
		Messages: []Message{{Role: "user", Content: "hi"}},
		// OnToken is nil — collection mode
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "collected" {
		t.Errorf("expected 'collected', got %q", resp.Content)
	}
}

// TestChatCompletion_HTTP404 verifies that a non-200/206 response returns an error.
func TestChatCompletion_HTTP404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	b := NewExternalBackend(srv.URL)
	_, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for HTTP 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected '404' in error, got: %v", err)
	}
}

// TestChatCompletion_PartialChunks verifies that a response split across
// partial TCP writes (half a line, flush, rest of line) is reassembled correctly.
func TestChatCompletion_PartialChunks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("ResponseWriter does not implement Flusher")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Write first half of a data line and flush
		fmt.Fprint(w, `data: {"choices":[{"delta":{"conten`)
		flusher.Flush()

		time.Sleep(5 * time.Millisecond)

		// Write the rest of the line
		fmt.Fprint(w, `t":"partial"}}]}`)
		fmt.Fprint(w, "\n\n")
		flusher.Flush()

		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	b := NewExternalBackend(srv.URL)
	resp, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "partial" {
		t.Errorf("expected 'partial', got %q", resp.Content)
	}
}

// TestChatCompletion_ConnectionRefused verifies that a connection-refused error
// returns a meaningful error (not a panic or nil).
func TestChatCompletion_ConnectionRefused(t *testing.T) {
	// Use a port that is not listening.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not find free port: %v", err)
	}
	addr := l.Addr().String()
	l.Close() // immediately close — connection refused

	b := NewExternalBackend("http://" + addr)
	_, err = b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "test",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for connection refused, got nil")
	}
}
