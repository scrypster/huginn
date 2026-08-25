package backend

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// qwen14bContentJSON is the live 2026-08-25 Ollama sample from
// qwen2.5-coder:14b on both /api/chat and /v1/chat/completions:
// finish_reason=stop, tool_calls absent, content is the function-call object.
const qwen14bContentJSON = `{"name": "bash", "arguments": {"command": "hostname"}}`

func TestPromoteContentToolCalls_JSONObject(t *testing.T) {
	resp := &ChatResponse{
		Content:    qwen14bContentJSON,
		DoneReason: "stop",
	}
	PromoteContentToolCalls(resp)

	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.Function.Name != "bash" {
		t.Errorf("Name = %q, want bash", tc.Function.Name)
	}
	if tc.Function.Arguments["command"] != "hostname" {
		t.Errorf("arguments.command = %v, want hostname", tc.Function.Arguments["command"])
	}
	if tc.ID == "" {
		t.Error("expected a synthetic tool call id")
	}
	// Mutual exclusion: promoted content is the invocation, not a final answer.
	if resp.Content != "" {
		t.Errorf("Content = %q, want empty after promote", resp.Content)
	}
	if resp.DoneReason != "tool_calls" {
		t.Errorf("DoneReason = %q, want tool_calls", resp.DoneReason)
	}
}

func TestPromoteContentToolCalls_ProseHello(t *testing.T) {
	resp := &ChatResponse{Content: "hello", DoneReason: "stop"}
	PromoteContentToolCalls(resp)

	if len(resp.ToolCalls) != 0 {
		t.Fatalf("ToolCalls = %+v, want none for normal prose", resp.ToolCalls)
	}
	if resp.Content != "hello" {
		t.Errorf("Content = %q, want unchanged hello", resp.Content)
	}
	if resp.DoneReason != "stop" {
		t.Errorf("DoneReason = %q, want stop", resp.DoneReason)
	}
}

func TestPromoteContentToolCalls_NativeWins(t *testing.T) {
	native := ToolCall{
		ID: "call_native",
		Function: ToolCallFunction{
			Name:      "read_file",
			Arguments: map[string]any{"file_path": "main.go"},
		},
	}
	resp := &ChatResponse{
		Content:    qwen14bContentJSON,
		DoneReason: "tool_calls",
		ToolCalls:  []ToolCall{native},
	}
	PromoteContentToolCalls(resp)

	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1 (native only)", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "call_native" || resp.ToolCalls[0].Function.Name != "read_file" {
		t.Errorf("native tool call was rewritten: %+v", resp.ToolCalls[0])
	}
	if resp.Content != qwen14bContentJSON {
		t.Errorf("Content changed when native tool_calls were present: %q", resp.Content)
	}
}

func TestPromoteContentToolCalls_ToolCallFence(t *testing.T) {
	resp := &ChatResponse{
		Content:    "<tool_call>\n" + qwen14bContentJSON + "\n</tool_call>",
		DoneReason: "stop",
	}
	PromoteContentToolCalls(resp)
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Name != "bash" {
		t.Fatalf("fence was not promoted: %+v", resp.ToolCalls)
	}
	if resp.ToolCalls[0].Function.Arguments["command"] != "hostname" {
		t.Errorf("arguments.command = %v", resp.ToolCalls[0].Function.Arguments["command"])
	}
}

func TestPromoteContentToolCalls_QwenXML(t *testing.T) {
	resp := &ChatResponse{
		Content: `<tool_call>
<function=bash>
<parameter=command>
hostname
</parameter>
</function>
</tool_call>`,
		DoneReason: "stop",
	}
	PromoteContentToolCalls(resp)
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Name != "bash" {
		t.Fatalf("Qwen XML was not promoted: %+v", resp.ToolCalls)
	}
	if resp.ToolCalls[0].Function.Arguments["command"] != "hostname" {
		t.Errorf("arguments.command = %v", resp.ToolCalls[0].Function.Arguments["command"])
	}
}

func TestPromoteContentToolCalls_RejectsEmbeddedSample(t *testing.T) {
	cases := []string{
		`Sure, run {"name": "bash", "arguments": {"command": "hostname"}}`,
		"Here is an example:\n```json\n" + qwen14bContentJSON + "\n```",
		`{"hello": "world"}`,
		`{"name": "bash"}`,
		`[{"name": "bash", "arguments": {"command": "hostname"}}]`,
	}
	for _, content := range cases {
		resp := &ChatResponse{Content: content, DoneReason: "stop"}
		PromoteContentToolCalls(resp)
		if len(resp.ToolCalls) != 0 {
			t.Errorf("content %q produced ToolCalls %+v, want none", content, resp.ToolCalls)
		}
		if resp.Content != content {
			t.Errorf("content was rewritten: got %q", resp.Content)
		}
	}
}

func TestPromoteContentToolCalls_ArgumentsJSONString(t *testing.T) {
	resp := &ChatResponse{
		Content:    `{"name":"bash","arguments":"{\"command\":\"hostname\"}"}`,
		DoneReason: "stop",
	}
	PromoteContentToolCalls(resp)
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Function.Arguments["command"] != "hostname" {
		t.Errorf("arguments.command = %v", resp.ToolCalls[0].Function.Arguments["command"])
	}
}

func TestPromoteContentToolCalls_NilSafe(t *testing.T) {
	PromoteContentToolCalls(nil)
}

func TestParseSSE_ContentJSONPromoted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", qwen14bContentJSON)
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	b := NewExternalBackend(srv.URL)
	resp, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "qwen2.5-coder:14b",
		Messages: []Message{{Role: "user", Content: "what is this host?"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1; content=%q", len(resp.ToolCalls), resp.Content)
	}
	if resp.ToolCalls[0].Function.Name != "bash" {
		t.Errorf("Name = %q, want bash", resp.ToolCalls[0].Function.Name)
	}
	if resp.ToolCalls[0].Function.Arguments["command"] != "hostname" {
		t.Errorf("arguments.command = %v", resp.ToolCalls[0].Function.Arguments["command"])
	}
	if resp.Content != "" {
		t.Errorf("Content = %q, want empty", resp.Content)
	}
	if resp.DoneReason != "tool_calls" {
		t.Errorf("DoneReason = %q, want tool_calls", resp.DoneReason)
	}
}

func TestParseSSE_NativeToolCallsNotDoubleParsed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", qwen14bContentJSON)
		fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_native","type":"function","function":{"name":"read_file","arguments":"{\"file_path\":\"x.go\"}"}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	b := NewExternalBackend(srv.URL)
	resp, err := b.ChatCompletion(context.Background(), ChatRequest{
		Model:    "qwen3.6:35b",
		Messages: []Message{{Role: "user", Content: "read it"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1 native call", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "call_native" || resp.ToolCalls[0].Function.Name != "read_file" {
		t.Errorf("expected native read_file, got %+v", resp.ToolCalls[0])
	}
	if resp.Content != qwen14bContentJSON {
		t.Errorf("native path should keep content, got %q", resp.Content)
	}
}
