package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/stats"
)

// mockBackend is a mock backend for testing.
type mockBackend struct {
	chatResponse string
	chatError    error
}

func (m *mockBackend) ChatCompletion(ctx context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	if m.chatError != nil {
		return nil, m.chatError
	}
	// Call OnToken if provided to simulate streaming
	if req.OnToken != nil {
		req.OnToken(m.chatResponse)
	}
	return &backend.ChatResponse{
		PromptTokens:     10,
		CompletionTokens: 20,
	}, nil
}

func (m *mockBackend) Shutdown(ctx context.Context) error {
	return nil
}

func (m *mockBackend) ContextWindow() int {
	return 8000 // default context window
}

func (m *mockBackend) Health(ctx context.Context) error {
	return nil
}

// TestOrchestrator_ExecuteAgentTool verifies ExecuteAgentTool makes LLM calls correctly.
func TestOrchestrator_ExecuteAgentTool(t *testing.T) {
	mockBE := &mockBackend{
		chatResponse: "Here is the summary of your text.",
	}
	models := modelconfig.DefaultModels()
	orch, err := agent.NewOrchestrator(mockBE, models, nil, nil, nil, nil)
 if err != nil {
 	t.Fatalf("orch: %v", err)
 }

	result, err := orch.ExecuteAgentTool(context.Background(), "gpt-4", 2000, "Summarize this text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "Here is the summary of your text." {
		t.Errorf("expected response, got %q", result)
	}
}

// TestOrchestrator_ExecuteAgentTool_Error verifies errors are propagated.
func TestOrchestrator_ExecuteAgentTool_Error(t *testing.T) {
	mockBE := &mockBackend{
		chatError: errors.New("backend error"),
	}
	models := modelconfig.DefaultModels()
	orch, err := agent.NewOrchestrator(mockBE, models, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("orch: %v", err)
	}

	_, err = orch.ExecuteAgentTool(context.Background(), "gpt-4", 2000, "Test prompt")
	if err == nil {
		t.Error("expected error")
	}
	if err.Error() != "backend error" {
		t.Errorf("expected backend error, got %v", err)
	}
}

// TestOrchestrator_ExecuteAgentTool_NoBackend verifies error when backend is nil.
func TestOrchestrator_ExecuteAgentTool_NoBackend(t *testing.T) {
	models := modelconfig.DefaultModels()
	orch, err := agent.NewOrchestrator(nil, models, nil, nil, stats.NoopCollector{}, nil)
	if err != nil {
		t.Fatalf("orch: %v", err)
	}

	_, err = orch.ExecuteAgentTool(context.Background(), "gpt-4", 2000, "Test")
	if err == nil {
		t.Error("expected error when backend is nil")
	}
}

// TestOrchestrator_ExecuteAgentTool_RecordsUsage verifies usage tokens are recorded.
func TestOrchestrator_ExecuteAgentTool_RecordsUsage(t *testing.T) {
	mockBE := &mockBackend{
		chatResponse: "Response",
	}
	models := modelconfig.DefaultModels()
	orch, err := agent.NewOrchestrator(mockBE, models, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("orch: %v", err)
	}

	_, err = orch.ExecuteAgentTool(context.Background(), "gpt-4", 2000, "Test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	prompt, completion := orch.LastUsage()
	if prompt != 10 {
		t.Errorf("expected prompt tokens 10, got %d", prompt)
	}
	if completion != 20 {
		t.Errorf("expected completion tokens 20, got %d", completion)
	}
}

// TestExecuteAgentTool_CancelledContext verifies that ExecuteAgentTool handles
// a pre-cancelled context correctly and returns an error.
func TestExecuteAgentTool_CancelledContext(t *testing.T) {
	// Create a mock backend that explicitly checks context cancellation
	mockBE := &mockBackendWithContextCheck{}
	models := modelconfig.DefaultModels()
	orch, err := agent.NewOrchestrator(mockBE, models, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("orch: %v", err)
	}

	// Pre-cancel the context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Call ExecuteAgentTool with cancelled context
	_, err = orch.ExecuteAgentTool(ctx, "gpt-4", 2000, "Test prompt")

	// Verify it returns a non-nil error
	if err == nil {
		t.Error("expected error from cancelled context, got nil")
	}
}

// mockBackendWithContextCheck is a mock backend that verifies context handling.
type mockBackendWithContextCheck struct{}

func (m *mockBackendWithContextCheck) ChatCompletion(ctx context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	// Check if context is already cancelled
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return &backend.ChatResponse{
		PromptTokens:     10,
		CompletionTokens: 20,
	}, nil
}

func (m *mockBackendWithContextCheck) Shutdown(ctx context.Context) error {
	return nil
}

func (m *mockBackendWithContextCheck) ContextWindow() int {
	return 8000
}

func (m *mockBackendWithContextCheck) Health(ctx context.Context) error {
	return nil
}
