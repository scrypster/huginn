package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

// The huginn-dev161 S5 speech turn, verbatim: after delegate + wait had run
// and Reggie answered PONG, the CoS emitted this and then delegated again.
const liveSpeechTurnV2 = "After Reggie responds with PONG:\n\n{\n  \"name\": \"recall_thread_result\",\n  \"arguments\": {\n    \"thread_id\": \"thread-12345\"  // Replace with the actual thread ID\n  }\n}\n\n7 times 8 is 56.\"PONG\"\n56PONG\n56"

func a2aSchemasWithRecall() []backend.Tool {
	return append(a2aSchemas(), backend.Tool{Function: backend.ToolFunction{Name: "recall_thread_result"}})
}

// After a wait_for_threads that already has the specialist answer (PONG),
// the loop stops without a recap completion so leftover glue is never generated.
func TestRunLoop_SpeechOnlyTurnAfterSpecialistResult(t *testing.T) {
	var mu sync.Mutex
	var log []string
	delegate := &orderTool{name: "delegate_to_agent", result: "Delegated to @Reggie (thread th_1)", mu: &mu, log: &log}
	wait := &orderTool{name: "wait_for_threads", result: "## Finished threads (1)\n\n## Result from agent \"Reggie\"\n\nPONG\n\nThread ID: `th_1`\n", mu: &mu, log: &log}
	recall := &orderTool{name: "recall_thread_result", result: "PONG", mu: &mu, log: &log}

	var streamed strings.Builder
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("delegate_to_agent", "call_1"),
			toolCallResponse("wait_for_threads", "call_2"),
			{Content: liveSpeechTurnV2, DoneReason: "tool_calls", ToolCalls: []backend.ToolCall{{
				ID: "call_3", Function: backend.ToolCallFunction{Name: "delegate_to_agent", Arguments: map[string]any{"agent": "Reggie", "task": "Reply PONG"}},
			}}},
			stopResponse("should never be requested"),
		},
	}
	res, err := RunLoop(context.Background(), RunLoopConfig{
		Backend:     mb,
		Tools:       newRegistryWith(delegate, wait, recall),
		ToolSchemas: a2aSchemasWithRecall(),
		Messages:    []backend.Message{{Role: "user", Content: "ping Reggie, then 7*8"}},
		OnToken:     func(s string) { streamed.WriteString(s) },
	})
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}
	if want := "delegate_to_agent,wait_for_threads"; strings.Join(log, ",") != want {
		t.Fatalf("executed %v, want %s (no second delegate, no recall)", log, want)
	}
	if mb.callCount != 2 {
		t.Errorf("model completions = %d, want 2 (stop after wait, no recap)", mb.callCount)
	}
	if res.StopReason != "stop" {
		t.Errorf("stop reason = %q", res.StopReason)
	}
	if res.FinalContent != "PONG" {
		t.Errorf("final = %q, want PONG", res.FinalContent)
	}
	for _, leak := range []string{"recall_thread_result", "thread-12345", "I'll ask Sam", "has been given the task"} {
		if strings.Contains(res.FinalContent, leak) || strings.Contains(streamed.String(), leak) {
			t.Errorf("leaked %q: final=%q stream=%q", leak, res.FinalContent, streamed.String())
		}
	}
	last := res.Messages[len(res.Messages)-1]
	if last.Role != "assistant" || last.Content != "PONG" {
		t.Errorf("last history message = %+v", last)
	}
}

// The same rule applies when the barrier was the synthetic auto-wait.
func TestRunLoop_SpeechOnlyTurnAfterAutoWait(t *testing.T) {
	var mu sync.Mutex
	var log []string
	delegate := &orderTool{name: "delegate_to_agent", result: "Delegated", mu: &mu, log: &log}
	wait := &orderTool{name: "wait_for_threads", result: "## Finished threads (1)\nPONG", mu: &mu, log: &log}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("delegate_to_agent", "call_1"),
			stopResponse("Reggie is on it."),
			// Post-wait turn tries to delegate again via content JSON.
			stopResponse("Reggie said PONG.\n{\"name\": \"delegate_to_agent\", \"arguments\": {\"agent\": \"Reggie\", \"task\": \"again\"}}"),
			stopResponse("never"),
		},
	}
	res, err := RunLoop(context.Background(), RunLoopConfig{
		Backend:     mb,
		Tools:       newRegistryWith(delegate, wait),
		ToolSchemas: a2aSchemas(),
		Messages:    []backend.Message{{Role: "user", Content: "go"}},
	})
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}
	if want := "delegate_to_agent,wait_for_threads"; strings.Join(log, ",") != want {
		t.Fatalf("executed %v, want %s", log, want)
	}
	if res.FinalContent != "PONG" || mb.callCount != 2 {
		t.Errorf("final=%q completions=%d, want PONG/2 (stop after auto-wait)", res.FinalContent, mb.callCount)
	}
}

// A speech turn that opens with tool JSON has nothing to say: decoding is cut
// (context cancelled for that request only) and the pre-wait prose stands.
func TestRunLoop_SpeechOnlyTurnCutsDecodeOnLeadingJSON(t *testing.T) {
	var mu sync.Mutex
	var log []string
	delegate := &orderTool{name: "delegate_to_agent", result: "Delegated", mu: &mu, log: &log}
	wait := &orderTool{name: "wait_for_threads", result: "## Finished threads (1)\nPONG", mu: &mu, log: &log}

	var cutCtx context.Context
	mb := &ctxAwareBackend{
		mockBackend: mockBackend{responses: []*backend.ChatResponse{
			toolCallResponse("delegate_to_agent", "call_1"),
			toolCallResponse("wait_for_threads", "call_2"),
			stopResponse("{\"name\": \"delegate_to_agent\", \"arguments\": {\"agent\": \"Reggie\", \"task\": \"again\"}}\nAnyway."),
		}},
		onCall: func(ctx context.Context, idx int) {
			if idx == 2 {
				cutCtx = ctx
			}
		},
	}
	res, err := RunLoop(context.Background(), RunLoopConfig{
		Backend:     mb,
		Tools:       newRegistryWith(delegate, wait),
		ToolSchemas: a2aSchemas(),
		Messages:    []backend.Message{{Role: "user", Content: "go"}},
		OnToken:     func(string) {},
	})
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}
	if mb.callCount != 2 {
		t.Fatalf("model completions = %d, want 2 (no speech recap after PONG wait)", mb.callCount)
	}
	if len(log) != 2 {
		t.Fatalf("executed %v", log)
	}
	if res.StopReason != "stop" {
		t.Errorf("stop reason = %q", res.StopReason)
	}
	if res.FinalContent != "PONG" {
		t.Errorf("final = %q, want PONG", res.FinalContent)
	}
	if strings.Contains(res.FinalContent, "delegate_to_agent") {
		t.Errorf("final leaked tool JSON: %q", res.FinalContent)
	}
	_ = cutCtx
}

// A backend that returns a cancelled-context error once the loop cuts decode
// must not turn the run into an error.
func TestRunLoop_SpeechOnlyTurnCutIsNotAnError(t *testing.T) {
	var mu sync.Mutex
	var log []string
	delegate := &orderTool{name: "delegate_to_agent", result: "Delegated", mu: &mu, log: &log}
	wait := &orderTool{name: "wait_for_threads", result: "## Finished threads (1)\nPONG", mu: &mu, log: &log}

	mb := &ctxAwareBackend{
		mockBackend: mockBackend{responses: []*backend.ChatResponse{
			toolCallResponse("delegate_to_agent", "call_1"),
			stopResponse("Reggie is on it."),
			stopResponse("{\"name\": \"delegate_to_agent\", \"arguments\": {}}"),
		}},
		errOnCancel: true,
	}
	res, err := RunLoop(context.Background(), RunLoopConfig{
		Backend:     mb,
		Tools:       newRegistryWith(delegate, wait),
		ToolSchemas: a2aSchemas(),
		Messages:    []backend.Message{{Role: "user", Content: "go"}},
		OnToken:     func(string) {},
	})
	if err != nil {
		t.Fatalf("stop-after-wait surfaced as error: %v", err)
	}
	if res.StopReason != "stop" || res.FinalContent != "PONG" {
		t.Errorf("stop=%q final=%q, want stop/PONG", res.StopReason, res.FinalContent)
	}
	if mb.callCount != 2 {
		t.Errorf("completions=%d, want 2", mb.callCount)
	}
}

// A wait that returned nothing (still running / timed out) does not trigger
// the speech-only stop — the lead may keep waiting.
func TestRunLoop_NoSpeechOnlyStopWithoutSpecialistResult(t *testing.T) {
	var mu sync.Mutex
	var log []string
	delegate := &orderTool{name: "delegate_to_agent", result: "Delegated", mu: &mu, log: &log}
	wait := &orderTool{name: "wait_for_threads", result: "## Still running (1) — timed out after 30s", mu: &mu, log: &log}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("delegate_to_agent", "call_1"),
			toolCallResponse("wait_for_threads", "call_2"),
			toolCallResponse("wait_for_threads", "call_3"),
			stopResponse("still nothing."),
		},
	}
	res, err := RunLoop(context.Background(), RunLoopConfig{
		Backend:     mb,
		Tools:       newRegistryWith(delegate, wait),
		ToolSchemas: a2aSchemas(),
		Messages:    []backend.Message{{Role: "user", Content: "go"}},
	})
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}
	if want := "delegate_to_agent,wait_for_threads,wait_for_threads"; strings.Join(log, ",") != want {
		t.Fatalf("executed %v, want %s", log, want)
	}
	if res.TurnCount != 4 {
		t.Errorf("turns = %d", res.TurnCount)
	}
}

// ctxAwareBackend is mockBackend plus visibility into the per-call context.
type ctxAwareBackend struct {
	mockBackend
	onCall      func(ctx context.Context, idx int)
	errOnCancel bool
}

func (m *ctxAwareBackend) ChatCompletion(ctx context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	m.mu.Lock()
	idx := m.callCount
	m.mu.Unlock()
	if m.onCall != nil {
		m.onCall(ctx, idx)
	}
	resp, err := m.mockBackend.ChatCompletion(ctx, req)
	if m.errOnCancel && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return resp, err
}
