package backend

import (
	"strings"
	"testing"
)

func grantedTools(names ...string) []Tool {
	out := make([]Tool, 0, len(names))
	for _, n := range names {
		out = append(out, Tool{Function: ToolFunction{Name: n}})
	}
	return out
}

const playbookContent = "I'll delegate this and wait for the result.\n\n" +
	"```json\n{ \"name\": \"delegate_to_agent\", \"arguments\": { \"agent\": \"Reggie\", \"task\": \"Reply PONG\" } }\n```\n\n" +
	"Once Reggie is running I'll collect the answer.\n\n" +
	"```json\n{ \"name\": \"wait_for_threads\", \"arguments\": {} }\n```\n"

// The crème bug: a playbook of fenced tool JSON mixed with glue prose must
// execute in order, with the fences stripped from visible Content.
func TestPromoteGranted_FencedPlaybookWithGlueProse(t *testing.T) {
	resp := &ChatResponse{Content: playbookContent, DoneReason: "stop"}
	PromoteGrantedContentToolCalls(resp, grantedTools("delegate_to_agent", "wait_for_threads"))

	if len(resp.ToolCalls) != 2 {
		t.Fatalf("expected 2 promoted calls, got %d (%+v)", len(resp.ToolCalls), resp.ToolCalls)
	}
	if resp.ToolCalls[0].Function.Name != "delegate_to_agent" {
		t.Errorf("first call = %q, want delegate_to_agent", resp.ToolCalls[0].Function.Name)
	}
	if agent, _ := resp.ToolCalls[0].Function.Arguments["agent"].(string); agent != "Reggie" {
		t.Errorf("delegate agent = %q, want Reggie", agent)
	}
	if resp.ToolCalls[1].Function.Name != "wait_for_threads" {
		t.Errorf("second call = %q, want wait_for_threads", resp.ToolCalls[1].Function.Name)
	}
	if resp.DoneReason != "tool_calls" {
		t.Errorf("DoneReason = %q, want tool_calls", resp.DoneReason)
	}
	if strings.Contains(resp.Content, "```") || strings.Contains(resp.Content, "delegate_to_agent") {
		t.Errorf("visible content still holds harness JSON: %q", resp.Content)
	}
	if !strings.Contains(resp.Content, "I'll delegate this") || !strings.Contains(resp.Content, "collect the answer") {
		t.Errorf("glue prose was lost: %q", resp.Content)
	}
}

// Unknown tool names stay inert: nothing is promoted and the fence stays
// visible, so code samples are never executed.
func TestPromoteGranted_UnknownNameInFenceStaysInert(t *testing.T) {
	content := "Here is an example config:\n\n```json\n{ \"name\": \"made_up_tool\", \"arguments\": {} }\n```\n"
	resp := &ChatResponse{Content: content, DoneReason: "stop"}
	PromoteGrantedContentToolCalls(resp, grantedTools("delegate_to_agent", "wait_for_threads"))

	if len(resp.ToolCalls) != 0 {
		t.Fatalf("unknown name was promoted: %+v", resp.ToolCalls)
	}
	if resp.Content != content {
		t.Errorf("content was rewritten without promotion: %q", resp.Content)
	}
	if resp.DoneReason != "stop" {
		t.Errorf("DoneReason = %q, want stop", resp.DoneReason)
	}
}

// A granted fence and an unknown fence in one message: the granted call runs,
// the unknown fence remains visible.
func TestPromoteGranted_MixedGrantedAndUnknownFences(t *testing.T) {
	content := "```json\n{\"name\": \"wait_for_threads\"}\n```\n" +
		"Example only:\n```json\n{\"name\": \"launch_missiles\", \"arguments\": {}}\n```\n"
	resp := &ChatResponse{Content: content, DoneReason: "stop"}
	PromoteGrantedContentToolCalls(resp, grantedTools("wait_for_threads"))

	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Name != "wait_for_threads" {
		t.Fatalf("expected only wait_for_threads promoted, got %+v", resp.ToolCalls)
	}
	if !strings.Contains(resp.Content, "launch_missiles") {
		t.Errorf("unknown fence should stay visible, got %q", resp.Content)
	}
	if strings.Count(resp.Content, "```") != 2 {
		t.Errorf("unknown fence delimiters should survive: %q", resp.Content)
	}
}

// Bare (unfenced) granted tool JSON mixed into prose also executes.
func TestPromoteGranted_BareJSONWithProse(t *testing.T) {
	content := "Delegating now.\n{\"name\": \"delegate_to_agent\", \"arguments\": {\"agent\": \"Sam\", \"task\": \"ping\"}}\nDone soon."
	resp := &ChatResponse{Content: content, DoneReason: "stop"}
	PromoteGrantedContentToolCalls(resp, grantedTools("delegate_to_agent"))

	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Name != "delegate_to_agent" {
		t.Fatalf("expected bare JSON promoted, got %+v", resp.ToolCalls)
	}
	if strings.Contains(resp.Content, "{") {
		t.Errorf("tool JSON left in content: %q", resp.Content)
	}
	if !strings.Contains(resp.Content, "Delegating now.") || !strings.Contains(resp.Content, "Done soon.") {
		t.Errorf("prose lost: %q", resp.Content)
	}
}

// A non-tool language fence never promotes, even when the body would parse.
func TestPromoteGranted_CodeLangFenceStaysInert(t *testing.T) {
	content := "Sample:\n```go\n{\"name\": \"wait_for_threads\"}\n```\n"
	resp := &ChatResponse{Content: content, DoneReason: "stop"}
	PromoteGrantedContentToolCalls(resp, grantedTools("wait_for_threads"))
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("go fence was promoted: %+v", resp.ToolCalls)
	}
	if resp.Content != content {
		t.Errorf("content rewritten: %q", resp.Content)
	}
}

// function_name alias and an inline one-line fence both promote.
func TestPromoteGranted_FunctionNameAliasAndInlineFence(t *testing.T) {
	content := "First step.\n```json { \"function_name\": \"wait_for_threads\" } ```\nSecond step."
	resp := &ChatResponse{Content: content, DoneReason: "stop"}
	PromoteGrantedContentToolCalls(resp, grantedTools("wait_for_threads"))
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Name != "wait_for_threads" {
		t.Fatalf("expected inline fence promoted, got %+v", resp.ToolCalls)
	}
}

// Native tool calls always win — content is left alone.
func TestPromoteGranted_NativeToolCallsWin(t *testing.T) {
	resp := &ChatResponse{
		Content:    playbookContent,
		DoneReason: "tool_calls",
		ToolCalls:  []ToolCall{{ID: "x", Function: ToolCallFunction{Name: "bash"}}},
	}
	PromoteGrantedContentToolCalls(resp, grantedTools("delegate_to_agent", "wait_for_threads"))
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "x" {
		t.Fatalf("native calls were replaced: %+v", resp.ToolCalls)
	}
	if resp.Content != playbookContent {
		t.Errorf("content rewritten despite native calls")
	}
}

// 158 regression: name-only JSON as the entire content still promotes through
// PromoteContentToolCalls, and the granted pass afterwards is a no-op.
func TestPromoteGranted_NameOnlyEntireContentStillWorks(t *testing.T) {
	resp := &ChatResponse{Content: `{"name": "wait_for_threads"}`, DoneReason: "stop"}
	PromoteContentToolCalls(resp)
	PromoteGrantedContentToolCalls(resp, grantedTools("wait_for_threads"))
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Function.Name != "wait_for_threads" {
		t.Fatalf("name-only entire content no longer promotes: %+v", resp.ToolCalls)
	}
	if resp.Content != "" {
		t.Errorf("content should be cleared, got %q", resp.Content)
	}
}

// Promoted calls get sequential content_call IDs in document order.
func TestPromoteGranted_SequentialIDs(t *testing.T) {
	resp := &ChatResponse{Content: playbookContent, DoneReason: "stop"}
	PromoteGrantedContentToolCalls(resp, grantedTools("delegate_to_agent", "wait_for_threads"))
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "content_call_1" || resp.ToolCalls[1].ID != "content_call_2" {
		t.Errorf("ids = %q, %q", resp.ToolCalls[0].ID, resp.ToolCalls[1].ID)
	}
}

// Streaming: the token gate must never paint a granted tool fence into the
// chat bubble, while glue prose still streams.
func TestTokenGate_HoldsGrantedFencesMidStream(t *testing.T) {
	var emitted strings.Builder
	gate := NewContentToolCallTokenGate(func(tok string) { emitted.WriteString(tok) }, nil)
	gate.SetGrantedTools(grantedTools("delegate_to_agent", "wait_for_threads"))

	// Stream the playbook in small chunks.
	for i := 0; i < len(playbookContent); i += 7 {
		end := i + 7
		if end > len(playbookContent) {
			end = len(playbookContent)
		}
		gate.OnToken(playbookContent[i:end])
	}

	out := emitted.String()
	if strings.Contains(out, "delegate_to_agent") || strings.Contains(out, "wait_for_threads") || strings.Contains(out, "```") {
		t.Errorf("gate leaked harness JSON into the stream: %q", out)
	}
	if !strings.Contains(out, "I'll delegate this") || !strings.Contains(out, "collect the answer") {
		t.Errorf("glue prose did not stream: %q", out)
	}
}

// Streaming: a non-tool fence (code sample) still streams through the gate
// once complete.
func TestTokenGate_EmitsNonToolFence(t *testing.T) {
	content := "Example:\n```go\nfunc main() {}\n```\nend."
	var emitted strings.Builder
	gate := NewContentToolCallTokenGate(func(tok string) { emitted.WriteString(tok) }, nil)
	gate.SetGrantedTools(grantedTools("wait_for_threads"))
	for i := 0; i < len(content); i += 5 {
		end := i + 5
		if end > len(content) {
			end = len(content)
		}
		gate.OnToken(content[i:end])
	}
	gate.Finish(content)
	if emitted.String() != content {
		t.Errorf("code fence mangled: got %q want %q", emitted.String(), content)
	}
}
