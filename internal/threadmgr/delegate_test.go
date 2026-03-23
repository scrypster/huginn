package threadmgr_test

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/threadmgr"
)

// makeDelegateFn constructs a DelegateFn that uses threadmgr and agents.
// sessionID is the session to create threads under.
func makeDelegateFn(
	sessionID string,
	tm *threadmgr.ThreadManager,
	reg *agents.AgentRegistry,
) threadmgr.DelegateFn {
	return func(ctx context.Context, p threadmgr.DelegateParams) threadmgr.DelegateResult {
		// Validate agent
		if _, found := reg.ByName(p.AgentName); !found {
			return threadmgr.DelegateResult{
				Err: &unknownAgentError{name: p.AgentName},
			}
		}

		// Create the thread
		t, _ := tm.Create(threadmgr.CreateParams{
			SessionID:      sessionID,
			AgentID:        p.AgentName,
			Task:           p.Task,
			DependsOnHints: p.DependsOn,
		})

		// Resolve dependency hints to thread IDs
		tm.ResolveDependencies(t.ID)

		// Acquire file leases if provided
		if len(p.FileIntents) > 0 {
			conflicts, err := tm.AcquireLeases(t.ID, p.FileIntents)
			if err != nil {
				return threadmgr.DelegateResult{Err: err}
			}
			if len(conflicts) > 0 {
				tm.Cancel(t.ID) // prevent orphan queued thread
				return threadmgr.DelegateResult{ThreadID: t.ID, Conflicts: conflicts}
			}
		}

		return threadmgr.DelegateResult{ThreadID: t.ID, Spawned: false}
	}
}

// unknownAgentError is a test-local sentinel error.
type unknownAgentError struct{ name string }

func (e *unknownAgentError) Error() string {
	return "unknown agent: " + e.name
}

func makeTestDelegateTool(t *testing.T) (*threadmgr.DelegateToAgentTool, *threadmgr.ThreadManager) {
	t.Helper()
	tm := threadmgr.New()
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Stacy", ModelID: "m"})

	tool := &threadmgr.DelegateToAgentTool{
		Fn: makeDelegateFn("test-sess", tm, reg),
	}
	return tool, tm
}

func TestDelegateToAgentTool_Name(t *testing.T) {
	tool, _ := makeTestDelegateTool(t)
	if tool.Name() != "delegate_to_agent" {
		t.Errorf("expected 'delegate_to_agent', got %q", tool.Name())
	}
}

func TestDelegateToAgentTool_ValidAgent_CreatesThread(t *testing.T) {
	tool, tm := makeTestDelegateTool(t)
	args := map[string]any{
		"agent":        "Stacy",
		"task":         "fix the login bug",
		"depends_on":   []any{},
		"file_intents": []any{"auth/login.go"},
	}
	result := tool.Execute(context.Background(), args)
	if result.IsError {
		t.Fatalf("expected no error, got: %s", result.Error)
	}

	// Assert status metadata is either "spawned" or "queued"
	status, ok := result.Metadata["status"].(string)
	if !ok {
		t.Fatalf("expected result.Metadata[\"status\"] to be a string, got %T", result.Metadata["status"])
	}
	if status != "spawned" && status != "queued" {
		t.Errorf("expected status to be 'spawned' or 'queued', got %q", status)
	}

	threads := tm.ListBySession("test-sess")
	if len(threads) != 1 {
		t.Errorf("expected 1 thread, got %d", len(threads))
	}
	if threads[0].AgentID != "Stacy" {
		t.Errorf("expected AgentID 'Stacy', got %q", threads[0].AgentID)
	}
}

func TestDelegateToAgentTool_UnknownAgent_ReturnsError(t *testing.T) {
	tool, _ := makeTestDelegateTool(t)
	args := map[string]any{
		"agent": "NonExistentAgent",
		"task":  "do something",
	}
	result := tool.Execute(context.Background(), args)
	if !result.IsError {
		t.Error("expected error for unknown agent, got success")
	}
}

func TestDelegateToAgentTool_MissingAgent_ReturnsError(t *testing.T) {
	tool, _ := makeTestDelegateTool(t)
	args := map[string]any{"task": "do something"}
	result := tool.Execute(context.Background(), args)
	if !result.IsError {
		t.Error("expected error for missing 'agent' field")
	}
}

func TestDelegateToAgentTool_Schema(t *testing.T) {
	tool, _ := makeTestDelegateTool(t)
	schema := tool.Schema()
	if schema.Function.Name != "delegate_to_agent" {
		t.Errorf("schema name mismatch: %q", schema.Function.Name)
	}
	if _, ok := schema.Function.Parameters.Properties["agent"]; !ok {
		t.Error("schema missing 'agent' parameter")
	}
	if _, ok := schema.Function.Parameters.Properties["task"]; !ok {
		t.Error("schema missing 'task' parameter")
	}
	if _, ok := schema.Function.Parameters.Properties["depends_on"]; !ok {
		t.Error("schema missing 'depends_on' parameter")
	}
	if _, ok := schema.Function.Parameters.Properties["file_intents"]; !ok {
		t.Error("schema missing 'file_intents' parameter")
	}
}

func TestDelegateToAgentTool_EmptyAgent_ReturnsError(t *testing.T) {
	tool, _ := makeTestDelegateTool(t)
	args := map[string]any{
		"agent": "",
		"task":  "do something",
	}
	result := tool.Execute(context.Background(), args)
	if !result.IsError {
		t.Error("expected error for empty 'agent', got success")
	}
}

func TestDelegateToAgentTool_EmptyTask_ReturnsError(t *testing.T) {
	tool, _ := makeTestDelegateTool(t)
	args := map[string]any{
		"agent": "Stacy",
		"task":  "",
	}
	result := tool.Execute(context.Background(), args)
	if !result.IsError {
		t.Error("expected error for empty 'task', got success")
	}
}
