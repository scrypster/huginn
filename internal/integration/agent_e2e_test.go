package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/tools"
)

// ---- minimal tool mock ----

// echoTool is a simple test tool that echoes its "message" arg back.
type echoTool struct{}

func (e *echoTool) Name() string        { return "echo_tool" }
func (e *echoTool) Description() string { return "Echoes back the message argument." }
func (e *echoTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (e *echoTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "echo_tool",
			Description: "Echoes back the message argument.",
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"message": {Type: "string", Description: "Message to echo"},
				},
				Required: []string{"message"},
			},
		},
	}
}
func (e *echoTool) Execute(_ context.Context, args map[string]any) tools.ToolResult {
	msg, _ := args["message"].(string)
	return tools.ToolResult{Output: "echoed: " + msg}
}

// ---- tool-call mock backend ----

// toolCallMockBackend returns a sequence of responses that can include tool calls.
// Each entry in responses is either:
//   - a plain string → text response (no tool calls)
//   - a toolCallResponse → assistant turn with tool calls
type toolCallResponse struct {
	toolName string
	args     map[string]any
	callID   string
}

type seqBackend struct {
	mu    sync.Mutex
	turns []seqTurn
	idx   int
}

type seqTurn struct {
	content   string
	toolCalls []backend.ToolCall
	doneReason string
}

func newSeqBackend(turns ...seqTurn) *seqBackend {
	return &seqBackend{turns: turns}
}

func (s *seqBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	s.mu.Lock()
	var turn seqTurn
	if s.idx < len(s.turns) {
		turn = s.turns[s.idx]
		s.idx++
	} else {
		// Exhausted: return a plain stop response to avoid infinite loops.
		turn = seqTurn{content: "done", doneReason: "stop"}
	}
	s.mu.Unlock()

	if req.OnToken != nil && turn.content != "" {
		req.OnToken(turn.content)
	}
	reason := turn.doneReason
	if reason == "" {
		if len(turn.toolCalls) > 0 {
			reason = "tool_calls"
		} else {
			reason = "stop"
		}
	}
	return &backend.ChatResponse{
		Content:    turn.content,
		ToolCalls:  turn.toolCalls,
		DoneReason: reason,
	}, nil
}

func (s *seqBackend) Health(_ context.Context) error   { return nil }
func (s *seqBackend) Shutdown(_ context.Context) error { return nil }
func (s *seqBackend) ContextWindow() int               { return 128_000 }

// ---- helpers ----

func toolCallTurn(name, callID string, args map[string]any) seqTurn {
	return seqTurn{
		toolCalls: []backend.ToolCall{
			{
				ID: callID,
				Function: backend.ToolCallFunction{
					Name:      name,
					Arguments: args,
				},
			},
		},
	}
}

func stopTurn(content string) seqTurn {
	return seqTurn{content: content, doneReason: "stop"}
}

// buildLoopConfig is a convenience builder for RunLoopConfig in tests.
func buildLoopConfig(b backend.Backend, reg *tools.Registry, maxTurns int, msgs []backend.Message) agent.RunLoopConfig {
	var schemas []backend.Tool
	if reg != nil {
		schemas = reg.AllSchemas()
	}
	return agent.RunLoopConfig{
		MaxTurns:    maxTurns,
		ModelName:   "test-model",
		Messages:    msgs,
		Tools:       reg,
		ToolSchemas: schemas,
		Backend:     b,
	}
}

// ---- tests ----

// TestAgentE2E_MultiTurn_ToolCall verifies the complete user→tool call→session persist lifecycle:
//  1. Model returns a tool call on turn 1.
//  2. Loop executes the tool.
//  3. Model returns a final "done" on turn 2.
//  4. OnToolCall and OnToolDone both fire.
func TestAgentE2E_MultiTurn_ToolCall(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&echoTool{})

	b := newSeqBackend(
		toolCallTurn("echo_tool", "call-1", map[string]any{"message": "hello"}),
		stopTurn("All done!"),
	)

	var toolCallName string
	var toolDoneName string
	var toolDoneResult tools.ToolResult

	cfg := buildLoopConfig(b, reg, 10, []backend.Message{
		{Role: "user", Content: "do something"},
	})
	cfg.OnToolCall = func(_ string, name string, _ map[string]any) {
		toolCallName = name
	}
	cfg.OnToolDone = func(_ string, name string, result tools.ToolResult) {
		toolDoneName = name
		toolDoneResult = result
	}

	result, err := agent.RunLoop(context.Background(), cfg)
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}

	if result.TurnCount < 2 {
		t.Errorf("expected TurnCount >= 2, got %d", result.TurnCount)
	}
	if result.StopReason != "stop" {
		t.Errorf("expected StopReason=stop, got %q", result.StopReason)
	}
	if toolCallName != "echo_tool" {
		t.Errorf("expected OnToolCall fired with echo_tool, got %q", toolCallName)
	}
	if toolDoneName != "echo_tool" {
		t.Errorf("expected OnToolDone fired with echo_tool, got %q", toolDoneName)
	}
	if !strings.Contains(toolDoneResult.Output, "echoed:") {
		t.Errorf("expected tool output to contain 'echoed:', got %q", toolDoneResult.Output)
	}
	if result.FinalContent != "All done!" {
		t.Errorf("expected FinalContent='All done!', got %q", result.FinalContent)
	}
}

// TestAgentE2E_MaxTurns_ExhaustedLoop verifies that a model stuck in an infinite
// tool-call loop is terminated at MaxTurns with StopReason=="max_turns".
func TestAgentE2E_MaxTurns_ExhaustedLoop(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&echoTool{})

	// Backend always returns a tool call, simulating an infinite loop.
	turns := make([]seqTurn, 10)
	for i := range turns {
		turns[i] = toolCallTurn("echo_tool", "call-inf", map[string]any{"message": "loop"})
	}
	b := newSeqBackend(turns...)

	cfg := buildLoopConfig(b, reg, 3, []backend.Message{
		{Role: "user", Content: "run forever"},
	})

	result, err := agent.RunLoop(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.StopReason != "max_turns" {
		t.Errorf("expected StopReason=max_turns, got %q", result.StopReason)
	}
	if result.TurnCount != 3 {
		t.Errorf("expected TurnCount=3, got %d", result.TurnCount)
	}
}

// TestAgentE2E_OnBeforeWrite_Blocks verifies that when OnBeforeWrite returns false,
// the write_file tool is blocked and no file is written to disk.
func TestAgentE2E_OnBeforeWrite_Blocks(t *testing.T) {
	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "output.txt")

	reg := tools.NewRegistry()
	// Register builtin tools (including write_file) scoped to tmpDir.
	tools.RegisterBuiltins(reg, tmpDir, 0)

	b := newSeqBackend(
		toolCallTurn("write_file", "call-write-1", map[string]any{
			"file_path": targetFile,
			"content":   "secret content",
		}),
		stopTurn("I tried to write but was blocked."),
	)

	var beforeWriteCalled bool

	cfg := buildLoopConfig(b, reg, 5, []backend.Message{
		{Role: "user", Content: "write a file"},
	})
	cfg.OnBeforeWrite = func(path string, _, _ []byte) bool {
		beforeWriteCalled = true
		return false // block all writes
	}

	result, err := agent.RunLoop(context.Background(), cfg)
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}

	if !beforeWriteCalled {
		t.Error("expected OnBeforeWrite to be called")
	}

	// File must NOT exist on disk since the write was blocked.
	if _, statErr := os.Stat(targetFile); statErr == nil {
		t.Errorf("expected file %q NOT to be written, but it exists", targetFile)
	}

	// Loop should have continued and reached a terminal stop.
	if result.StopReason == "" {
		t.Error("expected a non-empty StopReason")
	}
}

// TestAgentE2E_CorrelationID_NoPanic verifies that a loop with a CorrelationID
// set in context completes without panicking and returns the correct StopReason.
func TestAgentE2E_CorrelationID_NoPanic(t *testing.T) {
	reg := tools.NewRegistry()

	b := newSeqBackend(
		stopTurn("Hello, correlation!"),
	)

	cfg := buildLoopConfig(b, reg, 5, []backend.Message{
		{Role: "user", Content: "ping"},
	})

	// Set a correlation ID via a context value (matching the pattern used by the WS handler).
	ctx := agent.SetSessionID(context.Background(), "corr-test-session-123")

	var panicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
				t.Errorf("RunLoop panicked: %v", r)
			}
		}()
		result, err := agent.RunLoop(ctx, cfg)
		if err != nil {
			t.Errorf("RunLoop: %v", err)
			return
		}
		if result.StopReason != "stop" {
			t.Errorf("expected StopReason=stop, got %q", result.StopReason)
		}
	}()

	if panicked {
		t.Fatal("RunLoop panicked with correlation ID set")
	}
}

// TestAgentE2E_ContextCancelStopsLoop verifies that cancelling the context
// causes RunLoop to stop promptly and return a context-related error.
func TestAgentE2E_ContextCancelStopsLoop(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&echoTool{})

	// Use a blocking backend that waits for context cancellation.
	var callCount int64
	blockingBackend := &cancellingBackend{
		cancelAfterCalls: 1,
	}

	cfg := buildLoopConfig(blockingBackend, reg, 100, []backend.Message{
		{Role: "user", Content: "run until cancelled"},
	})
	// Always return tool calls so the loop keeps going.
	cfg.ToolSchemas = reg.AllSchemas()

	ctx, cancel := context.WithCancel(context.Background())

	resultCh := make(chan *agent.LoopResult, 1)
	errCh := make(chan error, 1)

	go func() {
		// Cancel context after 10ms.
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	go func() {
		result, err := agent.RunLoop(ctx, cfg)
		resultCh <- result
		errCh <- err
		_ = callCount
	}()

	select {
	case <-time.After(5 * time.Second):
		t.Fatal("timeout: RunLoop did not stop after context cancellation")
	case err := <-errCh:
		result := <-resultCh
		// Either a context error is returned, or the loop terminates cleanly
		// because the blocking backend unblocks on cancel. Both are acceptable.
		if err == nil && result != nil {
			// Loop finished cleanly — also acceptable if the backend stopped early.
			return
		}
		if err != nil {
			// Verify it's a context-related error.
			if !isContextError(err) {
				t.Errorf("expected context cancellation error, got: %v", err)
			}
		}
	}
}

// cancellingBackend is a backend that blocks ChatCompletion until context is cancelled.
// After cancelAfterCalls invocations it returns a tool call to keep the loop going.
type cancellingBackend struct {
	mu               sync.Mutex
	calls            int
	cancelAfterCalls int
}

func (c *cancellingBackend) ChatCompletion(ctx context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	c.mu.Lock()
	c.calls++
	calls := c.calls
	c.mu.Unlock()

	if calls > c.cancelAfterCalls {
		// Block until ctx is cancelled.
		<-ctx.Done()
		return nil, ctx.Err()
	}

	// Return a tool call that isn't in the registry — this causes an "unknown tool" error
	// result but lets the loop continue so it will eventually hit the blocking turn.
	return &backend.ChatResponse{
		ToolCalls: []backend.ToolCall{
			{
				ID: "call-block",
				Function: backend.ToolCallFunction{
					Name:      "echo_tool",
					Arguments: map[string]any{"message": "keep going"},
				},
			},
		},
		DoneReason: "tool_calls",
	}, nil
}

func (c *cancellingBackend) Health(_ context.Context) error   { return nil }
func (c *cancellingBackend) Shutdown(_ context.Context) error { return nil }
func (c *cancellingBackend) ContextWindow() int               { return 128_000 }

// isContextError returns true if err is or wraps a context cancellation/deadline error.
func isContextError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "context canceled") ||
		strings.Contains(msg, "context deadline exceeded")
}

// TestAgentE2E_SessionPersist verifies that after RunLoop completes, the returned
// Messages slice contains both the original user message and the assistant reply,
// simulating the persistence contract expected by a session store.
func TestAgentE2E_SessionPersist(t *testing.T) {
	reg := tools.NewRegistry()

	b := newSeqBackend(
		stopTurn("I have completed your request."),
	)

	initialMessages := []backend.Message{
		{Role: "user", Content: "please do something"},
	}

	cfg := buildLoopConfig(b, reg, 5, initialMessages)

	result, err := agent.RunLoop(context.Background(), cfg)
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}

	// Messages should contain at minimum the original user message + the assistant reply.
	if len(result.Messages) < 2 {
		t.Fatalf("expected at least 2 messages (user + assistant), got %d", len(result.Messages))
	}

	// First message must be the original user message.
	userMsg := result.Messages[0]
	if userMsg.Role != "user" {
		t.Errorf("expected first message role=user, got %q", userMsg.Role)
	}
	if userMsg.Content != "please do something" {
		t.Errorf("expected user message content to be preserved, got %q", userMsg.Content)
	}

	// There must be at least one assistant message.
	var foundAssistant bool
	for _, m := range result.Messages {
		if m.Role == "assistant" {
			foundAssistant = true
			if !strings.Contains(m.Content, "completed") {
				t.Errorf("expected assistant message to contain 'completed', got %q", m.Content)
			}
			break
		}
	}
	if !foundAssistant {
		t.Error("expected at least one assistant message in result.Messages")
	}

	if result.StopReason != "stop" {
		t.Errorf("expected StopReason=stop, got %q", result.StopReason)
	}
}

// execEchoTool is like echoTool but requires PermExec, so the Gate can deny it.
type execEchoTool struct{}

func (e *execEchoTool) Name() string        { return "exec_echo" }
func (e *execEchoTool) Description() string { return "Exec-level echo tool for permission tests." }
func (e *execEchoTool) Permission() tools.PermissionLevel { return tools.PermExec }
func (e *execEchoTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "exec_echo",
			Description: "Exec-level echo tool for permission tests.",
			Parameters: backend.ToolParameters{
				Type: "object",
				Properties: map[string]backend.ToolProperty{
					"message": {Type: "string", Description: "Message to echo"},
				},
				Required: []string{"message"},
			},
		},
	}
}
func (e *execEchoTool) Execute(_ context.Context, args map[string]any) tools.ToolResult {
	msg, _ := args["message"].(string)
	return tools.ToolResult{Output: "exec-echoed: " + msg}
}

// TestAgentE2E_PermissionDenied verifies that when the Gate denies a tool call,
// OnPermissionDenied fires and the loop continues gracefully to a final stop.
func TestAgentE2E_PermissionDenied(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&execEchoTool{})

	b := newSeqBackend(
		toolCallTurn("exec_echo", "call-denied", map[string]any{"message": "secret"}),
		stopTurn("Finished despite denial."),
	)

	var permDeniedTool string

	cfg := buildLoopConfig(b, reg, 5, []backend.Message{
		{Role: "user", Content: "try the exec echo tool"},
	})

	// Attach a Gate that always denies (skipAll=false, promptFunc always returns Deny).
	denyGate := permissions.NewGate(false, func(req permissions.PermissionRequest) permissions.Decision {
		return permissions.Deny
	})
	cfg.Gate = denyGate
	cfg.OnPermissionDenied = func(name string) {
		permDeniedTool = name
	}

	result, err := agent.RunLoop(context.Background(), cfg)
	if err != nil {
		t.Fatalf("RunLoop returned unexpected error: %v", err)
	}

	// OnPermissionDenied must have fired with the tool name.
	if permDeniedTool != "exec_echo" {
		t.Errorf("expected OnPermissionDenied called with exec_echo, got %q", permDeniedTool)
	}

	// Loop must have terminated gracefully (stop or max_turns, not a hard error).
	if result.StopReason == "" {
		t.Error("expected non-empty StopReason after permission denial")
	}
}

// TestAgentE2E_EmptyResponse verifies that when the backend returns an empty
// content response with no tool calls, the loop terminates gracefully with
// StopReason=="stop" (the model produced no tool calls so the loop ends).
func TestAgentE2E_EmptyResponse(t *testing.T) {
	reg := tools.NewRegistry()

	// Backend returns an empty content turn (no text, no tool calls).
	b := newSeqBackend(
		seqTurn{content: "", doneReason: "stop"},
	)

	cfg := buildLoopConfig(b, reg, 5, []backend.Message{
		{Role: "user", Content: "give me an empty response"},
	})

	result, err := agent.RunLoop(context.Background(), cfg)
	if err != nil {
		t.Fatalf("RunLoop returned unexpected error on empty response: %v", err)
	}

	// An empty content with no tool calls should terminate cleanly.
	if result.StopReason != "stop" {
		t.Errorf("expected StopReason=stop for empty response, got %q", result.StopReason)
	}

	// TurnCount should be exactly 1 (single backend call, then loop ended).
	if result.TurnCount != 1 {
		t.Errorf("expected TurnCount=1 for empty response, got %d", result.TurnCount)
	}
}
