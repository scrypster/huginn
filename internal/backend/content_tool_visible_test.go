package backend

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// liveMixedJSONProse is the 2026-08-25 #mention-proof Steve bubble:
// tool JSON immediately followed by the model's prose answer.
const liveMixedJSONProse = `{"name": "bash", "arguments": {"command": "echo PONG"}}PONG`

func TestVisibleAssistantContent_JSONThenProse(t *testing.T) {
	got := VisibleAssistantContent(liveMixedJSONProse)
	if got != "PONG" {
		t.Errorf("VisibleAssistantContent = %q, want PONG", got)
	}
}

func TestVisibleAssistantContent_PureJSON(t *testing.T) {
	if got := VisibleAssistantContent(qwen14bContentJSON); got != "" {
		t.Errorf("pure tool JSON should be hidden, got %q", got)
	}
}

func TestVisibleAssistantContent_ProseUnchanged(t *testing.T) {
	if got := VisibleAssistantContent("hello"); got != "hello" {
		t.Errorf("prose rewritten: %q", got)
	}
}

func TestVisibleAssistantContent_FencedSampleUnchanged(t *testing.T) {
	content := "Here is an example:\n```json\n" + qwen14bContentJSON + "\n```"
	if got := VisibleAssistantContent(content); got != content {
		t.Errorf("fenced sample rewritten: %q", got)
	}
}

func TestVisibleAssistantContent_EmbeddedInSentenceUnchanged(t *testing.T) {
	content := `Sure, run {"name": "bash", "arguments": {"command": "hostname"}}`
	if got := VisibleAssistantContent(content); got != content {
		t.Errorf("embedded sample rewritten: %q", got)
	}
}

func TestVisibleAssistantContent_TwoObjectsPlusThanks(t *testing.T) {
	content := winstonTwoObjectContent + "\n\nthanks"
	if got := VisibleAssistantContent(content); got != "thanks" {
		t.Errorf("VisibleAssistantContent = %q, want thanks", got)
	}
}

func TestPromoteContentToolCalls_MixedJSONProseStillNotExecuted(t *testing.T) {
	resp := &ChatResponse{Content: liveMixedJSONProse, DoneReason: "stop"}
	PromoteContentToolCalls(resp)
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("mixed JSON+prose produced ToolCalls %+v, want none", resp.ToolCalls)
	}
	if resp.Content != liveMixedJSONProse {
		t.Errorf("Promote must leave mixed content unchanged, got %q", resp.Content)
	}
	RevealContentToolCalls(resp)
	if resp.Content != "PONG" {
		t.Errorf("Reveal Content = %q, want PONG", resp.Content)
	}
	if len(resp.ToolCalls) != 0 {
		t.Errorf("Reveal must not promote leftover prose: %+v", resp.ToolCalls)
	}
}

func TestContentToolCallTokenGate_JSONThenProse(t *testing.T) {
	var got strings.Builder
	gate := NewContentToolCallTokenGate(func(s string) { got.WriteString(s) }, nil)
	for _, r := range liveMixedJSONProse {
		gate.Push(string(r))
	}
	gate.Finish(VisibleAssistantContent(liveMixedJSONProse))
	if got.String() != "PONG" {
		t.Errorf("streamed %q, want PONG", got.String())
	}
}

func TestContentToolCallTokenGate_PureJSONSilent(t *testing.T) {
	var got strings.Builder
	gate := NewContentToolCallTokenGate(func(s string) { got.WriteString(s) }, nil)
	gate.Push(qwen14bContentJSON)
	gate.Finish("")
	if got.String() != "" {
		t.Errorf("pure tool JSON leaked to stream: %q", got.String())
	}
}

func TestContentToolCallTokenGate_ProsePassesThrough(t *testing.T) {
	var got strings.Builder
	gate := NewContentToolCallTokenGate(func(s string) { got.WriteString(s) }, nil)
	gate.Push("hel")
	gate.Push("lo")
	gate.Finish("hello")
	if got.String() != "hello" {
		t.Errorf("streamed %q, want hello", got.String())
	}
}

func TestParseSSE_JSONThenProse_DoesNotStreamJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, r := range liveMixedJSONProse {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", string(r))
		}
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	var streamed strings.Builder
	b := NewExternalBackend(srv.URL)
	resp, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "qwen2.5-coder:14b",
		Messages: []Message{{Role: "user", Content: "@Steve say PONG and nothing else"}},
		OnToken:  func(s string) { streamed.WriteString(s) },
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if strings.Contains(streamed.String(), `{"name"`) || strings.Contains(streamed.String(), `"arguments"`) {
		t.Errorf("tool JSON leaked into streamed tokens: %q", streamed.String())
	}
	if streamed.String() != "PONG" {
		t.Errorf("streamed %q, want PONG", streamed.String())
	}
	if resp.Content != "PONG" {
		t.Errorf("Content = %q, want PONG", resp.Content)
	}
	if len(resp.ToolCalls) != 0 {
		t.Errorf("leftover prose must not be executed, ToolCalls=%+v", resp.ToolCalls)
	}
}

func TestParseSSE_PureJSON_DoesNotStreamJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", qwen14bContentJSON)
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	var streamed strings.Builder
	b := NewExternalBackend(srv.URL)
	resp, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "qwen2.5-coder:14b",
		Messages: []Message{{Role: "user", Content: "hostname"}},
		OnToken:  func(s string) { streamed.WriteString(s) },
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if streamed.String() != "" {
		t.Errorf("pure tool JSON leaked into stream: %q", streamed.String())
	}
	if resp.Content != "" {
		t.Errorf("Content = %q, want empty", resp.Content)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Name != "bash" {
		t.Fatalf("expected promoted bash call, got %+v", resp.ToolCalls)
	}
}

func TestParseSSE_FencedSampleStillStreams(t *testing.T) {
	content := "Here is an example:\n```json\n" + qwen14bContentJSON + "\n```"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", content)
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	var streamed strings.Builder
	b := NewExternalBackend(srv.URL)
	resp, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "qwen2.5-coder:14b",
		Messages: []Message{{Role: "user", Content: "show me"}},
		OnToken:  func(s string) { streamed.WriteString(s) },
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if streamed.String() != content {
		t.Errorf("fenced sample streamed %q, want original", streamed.String())
	}
	if resp.Content != content {
		t.Errorf("Content = %q, want original sample", resp.Content)
	}
	if len(resp.ToolCalls) != 0 {
		t.Errorf("fenced sample must not execute: %+v", resp.ToolCalls)
	}
}
