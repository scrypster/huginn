package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

func TestRunLoop_StopAfterCompanyWallDeny(t *testing.T) {
	var mu sync.Mutex
	var log []string
	delegate := &mockTool{
		name:   "delegate_to_agent",
		result: tools.ToolResult{IsError: true, Error: "Steve isn't in this company."},
	}
	wait := &orderTool{name: "wait_for_threads", result: "nothing", mu: &mu, log: &log}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("delegate_to_agent", "call_1"),
			stopResponse("Since Steve isn't available, I'll ask Sam again."),
		},
	}
	res, err := RunLoop(context.Background(), RunLoopConfig{
		Backend:     mb,
		Tools:       newRegistryWith(delegate, wait),
		ToolSchemas: a2aSchemas(),
		Messages:    []backend.Message{{Role: "user", Content: "Ask Steve for the hostname"}},
	})
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}
	if mb.callCount != 1 {
		t.Fatalf("model completions = %d, want 1 (no glue turn)", mb.callCount)
	}
	if len(log) != 0 {
		t.Fatalf("wait ran after wall deny: %v", log)
	}
	if res.StopReason != "stop" {
		t.Errorf("stop = %q", res.StopReason)
	}
	if res.FinalContent != "Steve isn't in Lab. Sam is." {
		t.Errorf("final = %q, want Lab wall", res.FinalContent)
	}
	if strings.Contains(res.FinalContent, "I'll ask Sam") || strings.Contains(res.FinalContent, "has been given the task") {
		t.Errorf("glue leaked: %q", res.FinalContent)
	}
}

func TestRunLoop_StopAfterConsultCompanyWallDeny(t *testing.T) {
	consult := &mockTool{
		name:   "consult_agent",
		result: tools.ToolResult{IsError: true, Error: "Steve isn't in this company."},
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("consult_agent", "call_1"),
			stopResponse("I'll ask Sam again."),
		},
	}
	res, err := RunLoop(context.Background(), RunLoopConfig{
		Backend:     mb,
		Tools:       newRegistryWith(consult),
		ToolSchemas: []backend.Tool{{Function: backend.ToolFunction{Name: "consult_agent"}}},
		Messages:    []backend.Message{{Role: "user", Content: "Ask Steve for the hostname"}},
	})
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}
	if mb.callCount != 1 {
		t.Fatalf("model completions = %d, want 1", mb.callCount)
	}
	if res.FinalContent != "Steve isn't in Lab. Sam is." {
		t.Errorf("final = %q", res.FinalContent)
	}
}

func TestRunLoop_CompanyWallDoesNotStopIfUserAskedSam(t *testing.T) {
	delegate := &mockTool{
		name:   "delegate_to_agent",
		result: tools.ToolResult{IsError: true, Error: "Steve isn't in this company."},
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("delegate_to_agent", "call_1"),
			stopResponse("Sam is on it."),
		},
	}
	res, err := RunLoop(context.Background(), RunLoopConfig{
		Backend:     mb,
		Tools:       newRegistryWith(delegate),
		ToolSchemas: a2aSchemas(),
		Messages:    []backend.Message{{Role: "user", Content: "Ask Sam for the hostname"}},
	})
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}
	if mb.callCount != 2 {
		t.Fatalf("model completions = %d, want 2 (user asked Sam)", mb.callCount)
	}
	if res.FinalContent != "Sam is on it." {
		t.Errorf("final = %q", res.FinalContent)
	}
}

func TestRunLoop_StopAfterWaitWithSpecialistAnswer(t *testing.T) {
	var mu sync.Mutex
	var log []string
	delegate := &orderTool{name: "delegate_to_agent", result: "Delegated to @Reggie (thread th_1)", mu: &mu, log: &log}
	wait := &orderTool{name: "wait_for_threads", result: "## Finished threads (1)\n\n## Result from agent \"Reggie\"\n\nPONG\n", mu: &mu, log: &log}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("delegate_to_agent", "call_1"),
			toolCallResponse("wait_for_threads", "call_2"),
			stopResponse("Since Steve isn't available, I'll ask Sam again. Reggie said PONG."),
		},
	}
	res, err := RunLoop(context.Background(), RunLoopConfig{
		Backend:     mb,
		Tools:       newRegistryWith(delegate, wait),
		ToolSchemas: a2aSchemas(),
		Messages:    []backend.Message{{Role: "user", Content: "ping Reggie"}},
	})
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}
	if mb.callCount != 2 {
		t.Fatalf("model completions = %d, want 2 (no recap turn)", mb.callCount)
	}
	if want := "delegate_to_agent,wait_for_threads"; strings.Join(log, ",") != want {
		t.Fatalf("executed %v, want %s", log, want)
	}
	if res.FinalContent != "PONG" {
		t.Errorf("final = %q, want PONG", res.FinalContent)
	}
	if strings.Contains(res.FinalContent, "I'll ask Sam") {
		t.Errorf("glue leaked: %q", res.FinalContent)
	}
}

func TestRunLoop_StopAfterWaitWithClock(t *testing.T) {
	stamp := "Thursday, August 27, 2026, 9:23 AM ET"
	var mu sync.Mutex
	var log []string
	delegate := &orderTool{name: "delegate_to_agent", result: "Delegated", mu: &mu, log: &log}
	wait := &orderTool{name: "wait_for_threads", result: "## Finished threads (1)\nLocal time now: " + stamp, mu: &mu, log: &log}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("delegate_to_agent", "call_1"),
			toolCallResponse("wait_for_threads", "call_2"),
			stopResponse("Winston reported the current time. Let me clarify."),
		},
	}
	res, err := RunLoop(context.Background(), RunLoopConfig{
		Backend:     mb,
		Tools:       newRegistryWith(delegate, wait),
		ToolSchemas: a2aSchemas(),
		Messages:    []backend.Message{{Role: "user", Content: "ask Winston what time it is"}},
	})
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}
	if mb.callCount != 2 {
		t.Fatalf("model completions = %d, want 2", mb.callCount)
	}
	if res.FinalContent != "It's "+stamp+"." {
		t.Errorf("final = %q", res.FinalContent)
	}
}

// TestRunLoop_WallDenyRetriesDirectlyWhenAskDidNotNameAgent is the
// regression test for the "Steve isn't in this company." /
// "Buggy isn't in this company." non-sequitur: the human asked something
// that never mentioned the delegated-to agent at all (the model chose to
// delegate on its own), so the refusal must never be spoken verbatim — the
// loop retries once with delegation withheld and answers directly.
func TestRunLoop_WallDenyRetriesDirectlyWhenAskDidNotNameAgent(t *testing.T) {
	delegate := &mockTool{
		name:   "delegate_to_agent",
		result: tools.ToolResult{IsError: true, Error: "Buggy isn't in this company."},
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("delegate_to_agent", "call_1"),
			stopResponse("FOXTROT"),
		},
	}
	res, err := RunLoop(context.Background(), RunLoopConfig{
		Backend:     mb,
		Tools:       newRegistryWith(delegate),
		ToolSchemas: a2aSchemas(),
		Messages:    []backend.Message{{Role: "user", Content: "burst two-C: say FOXTROT"}},
	})
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}
	if mb.callCount != 2 {
		t.Fatalf("model completions = %d, want 2 (one retry answering directly)", mb.callCount)
	}
	if res.FinalContent != "FOXTROT" {
		t.Fatalf("final = %q, want the direct retry answer, not the refusal", res.FinalContent)
	}
	if strings.Contains(res.FinalContent, "isn't in this company") {
		t.Fatalf("refusal leaked into final answer: %q", res.FinalContent)
	}
	// The retry must withhold delegation tools so the model cannot just
	// refuse again the same way.
	retryReq := mb.lastRequests[1]
	for _, tool := range retryReq.Tools {
		if tool.Function.Name == "delegate_to_agent" || tool.Function.Name == "consult_agent" {
			t.Fatalf("retry offered %q — delegation must be withheld", tool.Function.Name)
		}
	}
}

// TestRunLoop_WallDenyFallsBackToHonestRewriteWhenRetryAlsoRefuses verifies
// that if the direct-answer retry itself produces nothing sayable (another
// refusal, another tool call, or emptiness), the loop falls back to an
// honest rewrite instead of ever persisting the raw "X isn't in this
// company." teammate-refusal string as the answer.
func TestRunLoop_WallDenyFallsBackToHonestRewriteWhenRetryAlsoRefuses(t *testing.T) {
	delegate := &mockTool{
		name:   "delegate_to_agent",
		result: tools.ToolResult{IsError: true, Error: "Buggy isn't in this company."},
	}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("delegate_to_agent", "call_1"),
			stopResponse("Buggy isn't in this company."),
		},
	}
	res, err := RunLoop(context.Background(), RunLoopConfig{
		Backend:     mb,
		Tools:       newRegistryWith(delegate),
		ToolSchemas: a2aSchemas(),
		Messages:    []backend.Message{{Role: "user", Content: "burst two-C: say FOXTROT"}},
	})
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}
	if strings.Contains(res.FinalContent, "isn't in this company") {
		t.Fatalf("raw refusal persisted as final answer: %q", res.FinalContent)
	}
	if !strings.Contains(res.FinalContent, "Couldn't hand that off") {
		t.Fatalf("final = %q, want an honest rewrite", res.FinalContent)
	}
}

func TestUserAskedSam(t *testing.T) {
	if !userAskedSam("Ask Sam for the hostname") {
		t.Fatal("Ask Sam")
	}
	if userAskedSam("Ask Steve for the hostname") {
		t.Fatal("Ask Steve is not Ask Sam")
	}
}
