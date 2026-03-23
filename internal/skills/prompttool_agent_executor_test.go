package skills

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// mockAgentExecutor is a mock implementation of AgentExecutor for testing.
type mockAgentExecutor struct {
	lastModel   string
	lastBudget  int
	lastPrompt  string
	returnValue string
	returnError error
}

func (m *mockAgentExecutor) ExecuteAgentTool(ctx context.Context, model string, budgetTokens int, prompt string) (string, error) {
	m.lastModel = model
	m.lastBudget = budgetTokens
	m.lastPrompt = prompt
	return m.returnValue, m.returnError
}

// TestAgentExecutor_InjectionAndCall verifies that SetAgentExecutor injects the executor
// and executeAgent delegates to it correctly.
func TestAgentExecutor_InjectionAndCall(t *testing.T) {
	pt := &PromptTool{
		name:         "test_agent",
		description:  "Test agent tool",
		mode:         "agent",
		body:         "Summarize {{text}}",
		schemaJSON:   `{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`,
		agentModel:   "gpt-4",
		budgetTokens: 2000,
	}

	executor := &mockAgentExecutor{
		returnValue: "Summarized: This is a test.",
	}

	pt.SetAgentExecutor(executor)

	result := pt.Execute(context.Background(), map[string]any{"text": "Long document here"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	// Verify the executor was called with the right parameters
	if executor.lastModel != "gpt-4" {
		t.Errorf("expected model 'gpt-4', got %q", executor.lastModel)
	}
	if executor.lastBudget != 2000 {
		t.Errorf("expected budget 2000, got %d", executor.lastBudget)
	}
	if executor.lastPrompt != "Summarize Long document here" {
		t.Errorf("expected prompt 'Summarize Long document here', got %q", executor.lastPrompt)
	}
	if result.Output != "Summarized: This is a test." {
		t.Errorf("expected executor output, got %q", result.Output)
	}
}

// TestAgentExecutor_NilFallback verifies that nil executor returns stub message.
func TestAgentExecutor_NilFallback(t *testing.T) {
	pt := &PromptTool{
		name:         "test_agent",
		description:  "Test agent tool",
		mode:         "agent",
		body:         "Test prompt",
		agentModel:   "gpt-4",
		budgetTokens: 1000,
	}

	// Don't set an executor (nil)
	result := pt.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	// Should return stub message
	if !contains(result.Output, "agent mode stub") {
		t.Errorf("expected stub message, got %q", result.Output)
	}
}

// TestAgentExecutor_RecursionDepth verifies max recursion depth enforcement.
func TestAgentExecutor_RecursionDepth(t *testing.T) {
	pt := &PromptTool{
		name:     "test_agent",
		mode:     "agent",
		body:     "Test",
		depth:    3,
		maxDepth: 3, // depth 3 should fail with maxDepth 3
	}

	executor := &mockAgentExecutor{returnValue: "OK"}
	pt.SetAgentExecutor(executor)

	result := pt.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected recursion depth error")
	}
	if !contains(result.Error, "maximum recursion depth") {
		t.Errorf("expected recursion depth error message, got %q", result.Error)
	}
}

// TestAgentExecutor_DefaultBudget verifies default budget is used when not set.
func TestAgentExecutor_DefaultBudget(t *testing.T) {
	pt := &PromptTool{
		name:         "test_agent",
		mode:         "agent",
		body:         "Test",
		budgetTokens: 0, // will use default
	}

	executor := &mockAgentExecutor{returnValue: "OK"}
	pt.SetAgentExecutor(executor)

	result := pt.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}

	if executor.lastBudget != 5000 {
		t.Errorf("expected default budget 5000, got %d", executor.lastBudget)
	}
}

// TestInjectAgentExecutor_SkillRegistry verifies InjectAgentExecutor injects into all PromptTools.
func TestInjectAgentExecutor_SkillRegistry(t *testing.T) {
	// Create a tool registry with a PromptTool
	reg := tools.NewRegistry()
	pt1 := &PromptTool{
		name:         "agent_tool_1",
		mode:         "agent",
		body:         "Task {{task}}",
		agentModel:   "gpt-4",
		budgetTokens: 1000,
	}
	pt2 := &PromptTool{
		name:         "agent_tool_2",
		mode:         "agent",
		body:         "Task {{task}}",
		agentModel:   "gpt-3.5",
		budgetTokens: 500,
	}
	reg.Register(pt1)
	reg.Register(pt2)

	executor := &mockAgentExecutor{returnValue: "Result"}

	// Inject the executor
	InjectAgentExecutor(reg, executor)

	// Verify both tools were wired
	result1 := pt1.Execute(context.Background(), map[string]any{"task": "summarize"})
	if result1.IsError {
		t.Fatalf("pt1 execute failed: %s", result1.Error)
	}
	if executor.lastModel != "gpt-4" {
		t.Errorf("pt1 not wired correctly, model=%q", executor.lastModel)
	}

	result2 := pt2.Execute(context.Background(), map[string]any{"task": "translate"})
	if result2.IsError {
		t.Fatalf("pt2 execute failed: %s", result2.Error)
	}
	if executor.lastModel != "gpt-3.5" {
		t.Errorf("pt2 not wired correctly, model=%q", executor.lastModel)
	}
}

// TestInjectAgentExecutor_SkipsNonPromptTools verifies only PromptTools are wired.
func TestInjectAgentExecutor_SkipsNonPromptTools(t *testing.T) {
	reg := tools.NewRegistry()

	// Register a non-PromptTool (mockTool)
	mockTool := &mockTool{name: "mock_tool"}
	reg.Register(mockTool)

	executor := &mockAgentExecutor{returnValue: "OK"}

	// Should not panic, just skip the mock tool
	InjectAgentExecutor(reg, executor)
	// Pass if no panic
}

// TestAgentExecutor_TimeoutPropagated verifies that executeAgent wraps the context with a
// timeout so a long-running executor does not block indefinitely. The mock executor captures
// the context; we confirm it carries a deadline.
func TestAgentExecutor_TimeoutPropagated(t *testing.T) {
	var capturedCtx context.Context
	captureExec := &captureContextExecutor{}
	captureExec.capture = func(ctx context.Context) {
		capturedCtx = ctx
	}

	pt := &PromptTool{
		name:         "test_agent_timeout",
		mode:         "agent",
		body:         "Test prompt",
		agentModel:   "gpt-4",
		budgetTokens: 100,
	}
	pt.SetAgentExecutor(captureExec)

	// Use a background context that has no deadline of its own.
	pt.Execute(context.Background(), map[string]any{})

	if capturedCtx == nil {
		t.Fatal("executor was not called")
	}
	if _, ok := capturedCtx.Deadline(); !ok {
		t.Error("expected the context passed to ExecuteAgentTool to carry a deadline (timeout)")
	}
}

// captureContextExecutor is an AgentExecutor that records the context it receives.
type captureContextExecutor struct {
	capture func(context.Context)
}

func (c *captureContextExecutor) ExecuteAgentTool(ctx context.Context, model string, budgetTokens int, prompt string) (string, error) {
	if c.capture != nil {
		c.capture(ctx)
	}
	return "ok", nil
}

// Helper function to check string contains substring
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// mockTool is a minimal Tool implementation for testing.
type mockTool struct{ name string }

func (m *mockTool) Name() string {
	return m.name
}

func (m *mockTool) Description() string {
	return ""
}

func (m *mockTool) Permission() tools.PermissionLevel {
	return tools.PermRead
}

func (m *mockTool) Schema() backend.Tool {
	return backend.Tool{}
}

func (m *mockTool) Execute(ctx context.Context, args map[string]any) tools.ToolResult {
	return tools.ToolResult{}
}
