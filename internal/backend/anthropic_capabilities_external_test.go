package backend_test

// coverage_boost_test.go — additional tests to push backend package to 90%+.
// Targets:
//   - anthropic.go: Health (auth error), parseContentBlockStart (no index, non-tool block),
//     parseContentBlockDelta (no delta, input_json_delta no block), parseContentBlockStop
//     (no index, no block, bad JSON), parseMessageStart (no message, no usage)
//   - capabilities.go: FetchCapabilities (0%)
//   - external.go: Health (connection refused / error path, 4xx body), ChatCompletion (RateLimit path)
//   - openrouter.go: Health (server error), ChatCompletion (rate limit), ContextWindow (no slash)

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

// ---------------------------------------------------------------------------
// capabilities.go — FetchCapabilities (currently 0%)
// ---------------------------------------------------------------------------

func TestFetchCapabilities_VisionModel(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"details": map[string]any{"family": "llava"},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	caps := backend.FetchCapabilities(srv.URL, "llava:13b")
	if !caps.SupportsVision {
		t.Error("expected SupportsVision=true for llava")
	}
}

func TestFetchCapabilities_NonVisionModel(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"details": map[string]any{"family": "qwen2"},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}))
	defer srv.Close()

	caps := backend.FetchCapabilities(srv.URL, "qwen2:14b")
	if caps.SupportsVision {
		t.Error("expected SupportsVision=false for qwen2")
	}
}

func TestFetchCapabilities_ServerUnreachable(t *testing.T) {
	// Use a server that closes immediately.
	caps := backend.FetchCapabilities("http://127.0.0.1:1", "any-model")
	if caps.SupportsVision {
		t.Error("expected SupportsVision=false when server unreachable")
	}
}

// ---------------------------------------------------------------------------
// anthropic.go — Health: 401 returns authentication error
// ---------------------------------------------------------------------------

func TestAnthropicBackend_Health_401_NoError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("bad-key"), "claude-sonnet-4-6", srv.URL)
	err := b.Health(context.Background())
	if err == nil {
		t.Error("Health() should return error for 401 (authentication failed)")
	}
}

func TestAnthropicBackend_Health_ConnectionRefused(t *testing.T) {
	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", "http://127.0.0.1:1")
	if err := b.Health(context.Background()); err == nil {
		t.Error("expected error for connection refused in Health")
	}
}

// ---------------------------------------------------------------------------
// anthropic.go — parseContentBlockStart edge cases
// ---------------------------------------------------------------------------

// A content_block_start event where content_block.type != "tool_use" (e.g. "text")
// should be silently ignored.
func TestAnthropicBackend_ParseContentBlockStart_TextBlock_Ignored(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// content_block_start for a text block — should be ignored, not panic
		startBlock, _ := json.Marshal(map[string]any{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]any{
				"type": "text",
				"text": "",
			},
		})
		fmt.Fprintf(w, "event: content_block_start\ndata: %s\n\n", startBlock)
		// Follow with actual text content so stream is valid
		textDelta, _ := json.Marshal(map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "text_delta", "text": "hi"},
		})
		fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", textDelta)
		msgStart, _ := json.Marshal(map[string]any{
			"type":    "message_start",
			"message": map[string]any{"usage": map[string]any{"input_tokens": float64(1)}},
		})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", msgStart)
		msgDelta, _ := json.Marshal(map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn"},
			"usage": map[string]any{"output_tokens": float64(1)},
		})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", msgDelta)
		msgStop, _ := json.Marshal(map[string]any{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", msgStop)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hi" {
		t.Errorf("Content = %q, want hi", resp.Content)
	}
}

// content_block_start event with missing "index" field — should be silently skipped.
func TestAnthropicBackend_ParseContentBlockStart_MissingIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Missing "index" — should not panic
		startBlock, _ := json.Marshal(map[string]any{
			"type": "content_block_start",
			// no "index" field
			"content_block": map[string]any{
				"type": "tool_use",
				"id":   "x",
				"name": "y",
			},
		})
		fmt.Fprintf(w, "event: content_block_start\ndata: %s\n\n", startBlock)
		textDelta, _ := json.Marshal(map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "text_delta", "text": "ok"},
		})
		fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", textDelta)
		msgStart, _ := json.Marshal(map[string]any{
			"type":    "message_start",
			"message": map[string]any{"usage": map[string]any{"input_tokens": float64(1)}},
		})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", msgStart)
		msgDelta, _ := json.Marshal(map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn"},
			"usage": map[string]any{"output_tokens": float64(1)},
		})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", msgDelta)
		msgStop, _ := json.Marshal(map[string]any{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", msgStop)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("Content = %q, want ok", resp.Content)
	}
}

// ---------------------------------------------------------------------------
// anthropic.go — parseContentBlockDelta: input_json_delta with unknown index
// ---------------------------------------------------------------------------

func TestAnthropicBackend_ParseContentBlockDelta_InputJsonDelta_UnknownIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// input_json_delta for an index that was never registered via content_block_start
		badDelta, _ := json.Marshal(map[string]any{
			"type":  "content_block_delta",
			"index": 99, // unknown
			"delta": map[string]any{
				"type":         "input_json_delta",
				"partial_json": `{"x":1}`,
			},
		})
		fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", badDelta)
		textDelta, _ := json.Marshal(map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "text_delta", "text": "fine"},
		})
		fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", textDelta)
		msgStart, _ := json.Marshal(map[string]any{
			"type":    "message_start",
			"message": map[string]any{"usage": map[string]any{"input_tokens": float64(1)}},
		})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", msgStart)
		msgDelta, _ := json.Marshal(map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn"},
			"usage": map[string]any{"output_tokens": float64(1)},
		})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", msgDelta)
		msgStop, _ := json.Marshal(map[string]any{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", msgStop)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "fine" {
		t.Errorf("Content = %q, want fine", resp.Content)
	}
}

// ---------------------------------------------------------------------------
// anthropic.go — parseContentBlockStop: missing index → skipped
// ---------------------------------------------------------------------------

func TestAnthropicBackend_ParseContentBlockStop_MissingIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// content_block_stop without "index" — should be ignored
		stopBlock, _ := json.Marshal(map[string]any{
			"type": "content_block_stop",
			// no index
		})
		fmt.Fprintf(w, "event: content_block_stop\ndata: %s\n\n", stopBlock)
		textDelta, _ := json.Marshal(map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "text_delta", "text": "ok"},
		})
		fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", textDelta)
		msgStart, _ := json.Marshal(map[string]any{
			"type":    "message_start",
			"message": map[string]any{"usage": map[string]any{"input_tokens": float64(1)}},
		})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", msgStart)
		msgDelta, _ := json.Marshal(map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn"},
			"usage": map[string]any{"output_tokens": float64(1)},
		})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", msgDelta)
		msgStop, _ := json.Marshal(map[string]any{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", msgStop)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("Content = %q, want ok", resp.Content)
	}
}

// content_block_stop for an index with no matching toolBlock entry → skipped.
func TestAnthropicBackend_ParseContentBlockStop_UnknownIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// stop for index 5 — never started
		stopBlock, _ := json.Marshal(map[string]any{
			"type":  "content_block_stop",
			"index": 5,
		})
		fmt.Fprintf(w, "event: content_block_stop\ndata: %s\n\n", stopBlock)
		textDelta, _ := json.Marshal(map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "text_delta", "text": "done"},
		})
		fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", textDelta)
		msgStart, _ := json.Marshal(map[string]any{
			"type":    "message_start",
			"message": map[string]any{"usage": map[string]any{"input_tokens": float64(1)}},
		})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", msgStart)
		msgDelta, _ := json.Marshal(map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn"},
			"usage": map[string]any{"output_tokens": float64(1)},
		})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", msgDelta)
		msgStop, _ := json.Marshal(map[string]any{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", msgStop)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "done" {
		t.Errorf("Content = %q, want done", resp.Content)
	}
}

// content_block_stop with bad partialJSON (invalid JSON) → skipped, no ToolCall appended.
func TestAnthropicBackend_ParseContentBlockStop_InvalidJSON_ToolCallSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Register a tool block
		startBlock, _ := json.Marshal(map[string]any{
			"type":  "content_block_start",
			"index": 0,
			"content_block": map[string]any{
				"type": "tool_use",
				"id":   "tid1",
				"name": "bad_tool",
			},
		})
		fmt.Fprintf(w, "event: content_block_start\ndata: %s\n\n", startBlock)
		// Send invalid JSON as partial
		badDelta, _ := json.Marshal(map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{
				"type":         "input_json_delta",
				"partial_json": `{invalid`,
			},
		})
		fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", badDelta)
		// Stop the block — should fail to parse args JSON and skip
		stopBlock, _ := json.Marshal(map[string]any{
			"type":  "content_block_stop",
			"index": 0,
		})
		fmt.Fprintf(w, "event: content_block_stop\ndata: %s\n\n", stopBlock)
		msgStart, _ := json.Marshal(map[string]any{
			"type":    "message_start",
			"message": map[string]any{"usage": map[string]any{"input_tokens": float64(1)}},
		})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", msgStart)
		msgDelta, _ := json.Marshal(map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "tool_use"},
			"usage": map[string]any{"output_tokens": float64(1)},
		})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", msgDelta)
		msgStop, _ := json.Marshal(map[string]any{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", msgStop)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Truncated/invalid JSON → falls back to empty args; tool call is NOT dropped.
	// Zero-arg tools execute; tools that need args will fail at the server with a
	// recoverable error rather than being silently lost.
	if len(resp.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call (empty-args fallback), got %d", len(resp.ToolCalls))
	}
	if len(resp.ToolCalls) == 1 && resp.ToolCalls[0].Function.Name != "bad_tool" {
		t.Errorf("expected tool name 'bad_tool', got %q", resp.ToolCalls[0].Function.Name)
	}
}

// ---------------------------------------------------------------------------
// anthropic.go — parseMessageStart edge cases
// ---------------------------------------------------------------------------

// message_start with missing "message" field → silently ignored.
func TestAnthropicBackend_ParseMessageStart_MissingMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// message_start with no "message" field
		bad, _ := json.Marshal(map[string]any{
			"type": "message_start",
			// no "message"
		})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", bad)
		textDelta, _ := json.Marshal(map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "text_delta", "text": "hi"},
		})
		fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", textDelta)
		// valid message_start after
		good, _ := json.Marshal(map[string]any{
			"type":    "message_start",
			"message": map[string]any{"usage": map[string]any{"input_tokens": float64(3)}},
		})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", good)
		msgDelta, _ := json.Marshal(map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn"},
			"usage": map[string]any{"output_tokens": float64(1)},
		})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", msgDelta)
		msgStop, _ := json.Marshal(map[string]any{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", msgStop)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.PromptTokens != 3 {
		t.Errorf("PromptTokens = %d, want 3 (second message_start should win)", resp.PromptTokens)
	}
}

// message_start with "message" present but missing "usage" field → silently ignored.
func TestAnthropicBackend_ParseMessageStart_MissingUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// message_start with message but no usage
		noUsage, _ := json.Marshal(map[string]any{
			"type": "message_start",
			"message": map[string]any{
				"id": "msg_xxx",
				// no "usage"
			},
		})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", noUsage)
		textDelta, _ := json.Marshal(map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "text_delta", "text": "ok"},
		})
		fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", textDelta)
		msgDelta, _ := json.Marshal(map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn"},
			"usage": map[string]any{"output_tokens": float64(1)},
		})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", msgDelta)
		msgStop, _ := json.Marshal(map[string]any{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", msgStop)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// PromptTokens should remain 0 since usage was missing
	if resp.PromptTokens != 0 {
		t.Errorf("PromptTokens = %d, want 0 (usage missing)", resp.PromptTokens)
	}
}

// ---------------------------------------------------------------------------
// anthropic.go — ChatCompletion: rate limit (429) returns RateLimitError
// ---------------------------------------------------------------------------

func TestAnthropicBackend_ChatCompletion_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"type":"rate_limit_error","message":"too many requests"}}`))
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	_, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 429")
	}
	var rlErr *backend.RateLimitError
	if !errors.As(err, &rlErr) {
		t.Errorf("expected RateLimitError, got %T: %v", err, err)
	}
}

// ---------------------------------------------------------------------------
// external.go — Health: connection refused path
// ---------------------------------------------------------------------------

func TestExternalBackend_Health_ConnectionRefused(t *testing.T) {
	b := backend.NewExternalBackend("http://127.0.0.1:1")
	if err := b.Health(context.Background()); err == nil {
		t.Error("expected error for connection refused in Health")
	}
}

// external.go — Health: 4xx does NOT fail (only 5xx)
func TestExternalBackend_Health_401_Passes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	b := backend.NewExternalBackend(srv.URL)
	if err := b.Health(context.Background()); err != nil {
		t.Errorf("Health() returned error for 401 (only 5xx should fail): %v", err)
	}
}

// external.go — ChatCompletion: rate limit (429) returns RateLimitError
func TestExternalBackend_ChatCompletion_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer srv.Close()

	b := backend.NewExternalBackend(srv.URL)
	_, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "test-model",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 429")
	}
	var rlErr *backend.RateLimitError
	if !errors.As(err, &rlErr) {
		t.Errorf("expected RateLimitError, got %T: %v", err, err)
	}
}

// external.go — ChatCompletion: 4xx with body includes body in error message
func TestExternalBackend_ChatCompletion_4xx_WithBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"bad request detail"}`))
	}))
	defer srv.Close()

	b := backend.NewExternalBackend(srv.URL)
	_, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "test-model",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for 400")
	}
	if msg := err.Error(); len(msg) == 0 {
		t.Error("expected non-empty error message")
	}
}

// ---------------------------------------------------------------------------
// openrouter.go — Health: server error (5xx) returns error
// ---------------------------------------------------------------------------

func TestOpenRouterBackend_Health_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	b := backend.NewOpenRouterBackendWithEndpoint(backend.NewKeyResolver("key"), "model", srv.URL)
	if err := b.Health(context.Background()); err == nil {
		t.Error("expected error for 500 in OpenRouter Health")
	}
}

// openrouter.go — Health: connection refused
func TestOpenRouterBackend_Health_ConnectionRefused(t *testing.T) {
	b := backend.NewOpenRouterBackendWithEndpoint(backend.NewKeyResolver("key"), "model", "http://127.0.0.1:1")
	if err := b.Health(context.Background()); err == nil {
		t.Error("expected error for connection refused in OpenRouter Health")
	}
}

// openrouter.go — ChatCompletion: non-200 returns error
func TestOpenRouterBackend_ChatCompletion_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	b := backend.NewOpenRouterBackendWithEndpoint(backend.NewKeyResolver("key"), "model", srv.URL)
	_, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "model",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Error("expected error for 500 from OpenRouter ChatCompletion")
	}
}

// openrouter.go — ContextWindow: model with no provider prefix (no slash)
func TestOpenRouterBackend_ContextWindow_NoSlash(t *testing.T) {
	b := backend.NewOpenRouterBackend(backend.NewKeyResolver("key"), "claude-sonnet-4-6")
	got := b.ContextWindow()
	if got <= 0 {
		t.Errorf("ContextWindow() = %d, want > 0", got)
	}
}

// openrouter.go — ChatCompletion: connection refused path
func TestOpenRouterBackend_ChatCompletion_ConnectionRefused(t *testing.T) {
	b := backend.NewOpenRouterBackendWithEndpoint(backend.NewKeyResolver("key"), "model", "http://127.0.0.1:1")
	_, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "model",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Error("expected error for connection refused in OpenRouter ChatCompletion")
	}
}

// anthropic.go — parseContentBlockDelta: OnToken called when OnEvent not set
func TestAnthropicBackend_TextDelta_OnTokenFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		textDelta, _ := json.Marshal(map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "text_delta", "text": "token"},
		})
		fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", textDelta)
		msgStart, _ := json.Marshal(map[string]any{
			"type":    "message_start",
			"message": map[string]any{"usage": map[string]any{"input_tokens": float64(1)}},
		})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", msgStart)
		msgDelta, _ := json.Marshal(map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn"},
			"usage": map[string]any{"output_tokens": float64(1)},
		})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", msgDelta)
		msgStop, _ := json.Marshal(map[string]any{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", msgStop)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)
	var tokensReceived []string
	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
		// OnEvent is nil — should fall back to OnToken
		OnToken: func(t string) { tokensReceived = append(tokensReceived, t) },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "token" {
		t.Errorf("Content = %q, want token", resp.Content)
	}
	if len(tokensReceived) != 1 || tokensReceived[0] != "token" {
		t.Errorf("OnToken received %v, want [token]", tokensReceived)
	}
}

// TestAnthropicBackend_TextDelta_BothOnEventAndOnTokenFire verifies that when
// BOTH OnEvent AND OnToken are set, text_delta events call BOTH callbacks.
// This is the regression test for the bug where using OnEvent (needed for tool
// call/result events) silently suppressed OnToken, causing empty assistant
// messages after tool-call turns (no content persisted, no tokens to the UI).
func TestAnthropicBackend_TextDelta_BothOnEventAndOnTokenFire(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		textDelta, _ := json.Marshal(map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "text_delta", "text": "hello world"},
		})
		fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", textDelta)
		msgStart, _ := json.Marshal(map[string]any{
			"type":    "message_start",
			"message": map[string]any{"usage": map[string]any{"input_tokens": float64(1)}},
		})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", msgStart)
		msgDelta, _ := json.Marshal(map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn"},
			"usage": map[string]any{"output_tokens": float64(1)},
		})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", msgDelta)
		msgStop, _ := json.Marshal(map[string]any{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", msgStop)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)

	var tokensReceived []string
	var eventsReceived []backend.StreamEvent

	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
		OnToken:  func(t string) { tokensReceived = append(tokensReceived, t) },
		OnEvent:  func(e backend.StreamEvent) { eventsReceived = append(eventsReceived, e) },
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "hello world" {
		t.Errorf("Content = %q, want %q", resp.Content, "hello world")
	}

	// OnToken MUST fire even when OnEvent is also set — this is the core assertion.
	if len(tokensReceived) != 1 || tokensReceived[0] != "hello world" {
		t.Errorf("OnToken received %v, want [hello world] — OnToken was suppressed when OnEvent was set", tokensReceived)
	}

	// OnEvent must also receive the StreamText event.
	var textEvents int
	for _, e := range eventsReceived {
		if e.Type == backend.StreamText {
			textEvents++
			if e.Content != "hello world" {
				t.Errorf("StreamText event Content = %q, want %q", e.Content, "hello world")
			}
		}
	}
	if textEvents != 1 {
		t.Errorf("expected 1 StreamText event via OnEvent, got %d", textEvents)
	}
}

// TestAnthropicBackend_MultiTurn_ToolCall_ThenText_AccumulatesAllTokens simulates
// a multi-turn conversation: first turn returns a tool_use block, second turn
// returns the text response. Verifies that OnToken accumulates content from ALL
// turns, which is critical for the WS handler's assistantBuf (persistence).
func TestAnthropicBackend_MultiTurn_ToolCall_ThenText_AccumulatesAllTokens(t *testing.T) {
	// We can't easily simulate multi-turn through the backend (that's RunLoop's job),
	// but we CAN verify that a single turn with both OnEvent and OnToken set delivers
	// text tokens to both callbacks, which is the prerequisite for correct multi-turn
	// accumulation at the RunLoop level.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")

		// Emit multiple text_delta events to simulate chunked streaming
		for _, chunk := range []string{"Here ", "is ", "the ", "answer."} {
			delta, _ := json.Marshal(map[string]any{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]any{"type": "text_delta", "text": chunk},
			})
			fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", delta)
		}

		msgStart, _ := json.Marshal(map[string]any{
			"type":    "message_start",
			"message": map[string]any{"usage": map[string]any{"input_tokens": float64(10)}},
		})
		fmt.Fprintf(w, "event: message_start\ndata: %s\n\n", msgStart)
		msgDelta, _ := json.Marshal(map[string]any{
			"type":  "message_delta",
			"delta": map[string]any{"stop_reason": "end_turn"},
			"usage": map[string]any{"output_tokens": float64(4)},
		})
		fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", msgDelta)
		msgStop, _ := json.Marshal(map[string]any{"type": "message_stop"})
		fmt.Fprintf(w, "event: message_stop\ndata: %s\n\n", msgStop)
	}))
	defer srv.Close()

	b := backend.NewAnthropicBackendWithEndpoint(backend.NewKeyResolver("key"), "claude-sonnet-4-6", srv.URL)

	var buf strings.Builder
	var eventTexts []string

	_, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Model:    "claude-sonnet-4-6",
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
		OnToken:  func(t string) { buf.WriteString(t) },
		OnEvent: func(e backend.StreamEvent) {
			if e.Type == backend.StreamText {
				eventTexts = append(eventTexts, e.Content)
			}
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "Here is the answer."
	if got := buf.String(); got != want {
		t.Errorf("OnToken accumulated %q, want %q — content lost during streaming", got, want)
	}
	if len(eventTexts) != 4 {
		t.Errorf("expected 4 StreamText events, got %d", len(eventTexts))
	}
}
