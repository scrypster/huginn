package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// orderTool records execution order into a shared log.
type orderTool struct {
	name   string
	result string
	mu     *sync.Mutex
	log    *[]string
}

func (t *orderTool) Name() string                      { return t.name }
func (t *orderTool) Description() string               { return "" }
func (t *orderTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *orderTool) Schema() backend.Tool {
	return backend.Tool{Function: backend.ToolFunction{Name: t.name}}
}
func (t *orderTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	t.mu.Lock()
	defer t.mu.Unlock()
	*t.log = append(*t.log, t.name)
	return tools.ToolResult{Output: t.result}
}

func a2aSchemas() []backend.Tool {
	return []backend.Tool{
		{Function: backend.ToolFunction{Name: "delegate_to_agent"}},
		{Function: backend.ToolFunction{Name: "wait_for_threads"}},
	}
}

const playbook = "I'll delegate and then wait.\n\n" +
	"```json\n{ \"name\": \"delegate_to_agent\", \"arguments\": { \"agent\": \"Reggie\", \"task\": \"Reply PONG\" } }\n```\n\n" +
	"Collecting the result next.\n\n" +
	"```json\n{ \"name\": \"wait_for_threads\", \"arguments\": {} }\n```\n"

// The crème bug end-to-end: fenced granted-tool JSON mixed with glue prose is
// promoted, executed in order, and the loop keeps running.
func TestRunLoop_FencedGrantedPlaybookExecutes(t *testing.T) {
	var mu sync.Mutex
	var log []string
	delegate := &orderTool{name: "delegate_to_agent", result: "Delegated to @Reggie (thread th_1)", mu: &mu, log: &log}
	wait := &orderTool{name: "wait_for_threads", result: "## Finished threads (1)\nPONG", mu: &mu, log: &log}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{Content: playbook, DoneReason: "stop"},
			stopResponse("Reggie says PONG."),
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
	if want := []string{"delegate_to_agent", "wait_for_threads"}; strings.Join(log, ",") != strings.Join(want, ",") {
		t.Fatalf("execution order = %v, want %v", log, want)
	}
	if res.StopReason != "stop" || res.TurnCount != 2 {
		t.Errorf("stop=%q turns=%d, want stop/2", res.StopReason, res.TurnCount)
	}
	if res.FinalContent != "Reggie says PONG." {
		t.Errorf("final = %q", res.FinalContent)
	}
	// The promoted assistant message keeps the glue prose, never the fences.
	for _, m := range res.Messages {
		if m.Role == "assistant" && strings.Contains(m.Content, "```") {
			t.Errorf("assistant message still holds a fence: %q", m.Content)
		}
	}
}

// An unknown tool name inside a fence is never executed — the message is the
// final answer and the fence stays put.
func TestRunLoop_UnknownNameInFenceNotExecuted(t *testing.T) {
	var mu sync.Mutex
	var log []string
	wait := &orderTool{name: "wait_for_threads", result: "nothing", mu: &mu, log: &log}

	content := "Example config:\n```json\n{\"name\": \"drop_database\", \"arguments\": {}}\n```\n"
	mb := &mockBackend{
		responses: []*backend.ChatResponse{{Content: content, DoneReason: "stop"}},
	}
	res, err := RunLoop(context.Background(), RunLoopConfig{
		Backend:     mb,
		Tools:       newRegistryWith(wait),
		ToolSchemas: []backend.Tool{{Function: backend.ToolFunction{Name: "wait_for_threads"}}},
		Messages:    []backend.Message{{Role: "user", Content: "show me an example"}},
	})
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}
	if len(log) != 0 {
		t.Fatalf("unexpected tool executions: %v", log)
	}
	if res.TurnCount != 1 || res.StopReason != "stop" {
		t.Errorf("stop=%q turns=%d, want stop/1", res.StopReason, res.TurnCount)
	}
	if !strings.Contains(res.FinalContent, "drop_database") {
		t.Errorf("example fence should stay visible, got %q", res.FinalContent)
	}
}

// After a successful delegate_to_agent, a model that stops without
// wait_for_threads gets one synthetic barrier so specialists are not abandoned.
func TestRunLoop_AutoWaitAfterDelegateWithoutWait(t *testing.T) {
	var mu sync.Mutex
	var log []string
	delegate := &orderTool{name: "delegate_to_agent", result: "Delegated to @Reggie (thread th_1)", mu: &mu, log: &log}
	wait := &orderTool{name: "wait_for_threads", result: "## Finished threads (1)\nPONG", mu: &mu, log: &log}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("delegate_to_agent", "call_1"),
			stopResponse("Reggie is on it."),       // stops without waiting
			stopResponse("Reggie reported: PONG."), // answer after auto-wait
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
	if want := []string{"delegate_to_agent", "wait_for_threads"}; strings.Join(log, ",") != strings.Join(want, ",") {
		t.Fatalf("execution order = %v, want %v", log, want)
	}
	if res.FinalContent != "Reggie reported: PONG." {
		t.Errorf("final = %q", res.FinalContent)
	}
	// The synthetic barrier's tool result must be in history for the model.
	found := false
	for _, m := range res.Messages {
		if m.Role == "tool" && m.ToolName == "wait_for_threads" {
			found = true
		}
	}
	if !found {
		t.Errorf("no wait_for_threads tool result in history")
	}
}

// Auto-wait fires at most once: if the model stops again right after the
// barrier, the loop ends instead of waiting forever.
func TestRunLoop_AutoWaitOnlyOnce(t *testing.T) {
	var mu sync.Mutex
	var log []string
	delegate := &orderTool{name: "delegate_to_agent", result: "Delegated", mu: &mu, log: &log}
	wait := &orderTool{name: "wait_for_threads", result: "## Still running (1) — timed out", mu: &mu, log: &log}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("delegate_to_agent", "call_1"),
			stopResponse("delegated."),
			stopResponse(""), // model has nothing to add after the barrier
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
	waits := 0
	for _, name := range log {
		if name == "wait_for_threads" {
			waits++
		}
	}
	if waits != 1 {
		t.Fatalf("wait_for_threads ran %d times, want 1", waits)
	}
	// Empty post-barrier answer falls back to the pre-barrier prose.
	if res.FinalContent != "delegated." {
		t.Errorf("final = %q, want pre-wait prose", res.FinalContent)
	}
}

// No auto-wait when the model already called wait_for_threads itself.
func TestRunLoop_NoAutoWaitWhenModelWaited(t *testing.T) {
	var mu sync.Mutex
	var log []string
	delegate := &orderTool{name: "delegate_to_agent", result: "Delegated", mu: &mu, log: &log}
	wait := &orderTool{name: "wait_for_threads", result: "## Finished threads (1)", mu: &mu, log: &log}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("delegate_to_agent", "call_1"),
			toolCallResponse("wait_for_threads", "call_2"),
			stopResponse("all done"),
		},
	}
	_, err := RunLoop(context.Background(), RunLoopConfig{
		Backend:     mb,
		Tools:       newRegistryWith(delegate, wait),
		ToolSchemas: a2aSchemas(),
		Messages:    []backend.Message{{Role: "user", Content: "go"}},
	})
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}
	waits := 0
	for _, name := range log {
		if name == "wait_for_threads" {
			waits++
		}
	}
	if waits != 1 {
		t.Fatalf("wait_for_threads ran %d times, want 1 (no synthetic barrier)", waits)
	}
}

// No auto-wait when the delegate itself failed — there is nothing to wait on.
func TestRunLoop_NoAutoWaitAfterFailedDelegate(t *testing.T) {
	var mu sync.Mutex
	var log []string
	wait := &orderTool{name: "wait_for_threads", result: "nothing", mu: &mu, log: &log}
	failingDelegate := &mockTool{
		name:   "delegate_to_agent",
		result: tools.ToolResult{IsError: true, Error: "delegate_to_agent: unknown agent \"Nobody\""},
	}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("delegate_to_agent", "call_1"),
			stopResponse("that agent does not exist."),
		},
	}
	_, err := RunLoop(context.Background(), RunLoopConfig{
		Backend:     mb,
		Tools:       newRegistryWith(failingDelegate, wait),
		ToolSchemas: a2aSchemas(),
		Messages:    []backend.Message{{Role: "user", Content: "go"}},
	})
	if err != nil {
		t.Fatalf("RunLoop error: %v", err)
	}
	if len(log) != 0 {
		t.Fatalf("synthetic wait ran after failed delegate: %v", log)
	}
}
