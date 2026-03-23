package agents

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

// internalMockBackend is a test double for backend.Backend used in internal package tests.
type internalMockBackend struct {
	response string
}

func (m *internalMockBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	if req.OnToken != nil {
		req.OnToken(m.response)
	}
	return &backend.ChatResponse{Content: m.response, DoneReason: "stop"}, nil
}
func (m *internalMockBackend) Health(_ context.Context) error   { return nil }
func (m *internalMockBackend) Shutdown(_ context.Context) error { return nil }
func (m *internalMockBackend) ContextWindow() int               { return 128_000 }

func makeInternalTestRegistry() *AgentRegistry {
	reg := NewRegistry()
	reg.Register(&Agent{
		Name:    "Mark",
		ModelID: "test-model",
	})
	reg.Register(&Agent{
		Name:    "Chris",
		ModelID: "test-model",
	})
	return reg
}

// TestConsultAgentTool_PersistsDelegationEntry verifies that Execute stores a
// DelegationEntry in the MemoryStore when a memory store is configured.
func TestConsultAgentTool_PersistsDelegationEntry(t *testing.T) {
	ms := openTestMemoryStore(t, "test-machine")
	reg := makeInternalTestRegistry()
	mb := &internalMockBackend{response: "Here is my analysis."}
	var depth int32

	tool := NewConsultAgentToolWithMemory(reg, mb, &depth, nil, nil, nil, ms, "Mark")

	ctx := context.Background()
	result := tool.Execute(ctx, map[string]any{
		"agent_name": "Chris",
		"question":   "What do you think of this design?",
	})

	if result.IsError {
		t.Fatalf("Execute returned error: %v", result.Error)
	}

	entries, err := ms.LoadRecentDelegations(ctx, "Mark", "Chris", 10)
	if err != nil {
		t.Fatalf("LoadRecentDelegations: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 delegation entry, got %d", len(entries))
	}
	if entries[0].From != "Mark" {
		t.Errorf("From: got %q, want Mark", entries[0].From)
	}
	if entries[0].To != "Chris" {
		t.Errorf("To: got %q, want Chris", entries[0].To)
	}
	if entries[0].Question != "What do you think of this design?" {
		t.Errorf("Question: got %q", entries[0].Question)
	}
	if entries[0].Answer != "Here is my analysis." {
		t.Errorf("Answer: got %q", entries[0].Answer)
	}
}

// TestConsultAgentTool_NilMemoryStore_StillWorks verifies that ConsultAgentTool
// works normally when no memory store is configured (backward compatibility).
func TestConsultAgentTool_NilMemoryStore_StillWorks(t *testing.T) {
	reg := makeInternalTestRegistry()
	mb := &internalMockBackend{response: "No problem!"}
	var depth int32

	// Use the standard constructor (no memory store)
	tool := NewConsultAgentTool(reg, mb, &depth, nil, nil)

	ctx := context.Background()
	result := tool.Execute(ctx, map[string]any{
		"agent_name": "Chris",
		"question":   "Is this okay?",
	})

	if result.IsError {
		t.Fatalf("Execute with nil MemoryStore returned error: %v", result.Error)
	}
	if result.Output == "" {
		t.Error("expected non-empty output")
	}
}

// TestConsultAgentTool_UnknownFromAgent_UsesUnknown verifies that an empty
// fromAgentName is normalized to "unknown" in the stored delegation entry.
func TestConsultAgentTool_UnknownFromAgent_UsesUnknown(t *testing.T) {
	ms := openTestMemoryStore(t, "test-machine")
	reg := makeInternalTestRegistry()
	mb := &internalMockBackend{response: "Response"}
	var depth int32

	// fromAgentName is empty string — should default to "unknown"
	tool := NewConsultAgentToolWithMemory(reg, mb, &depth, nil, nil, nil, ms, "")

	ctx := context.Background()
	result := tool.Execute(ctx, map[string]any{
		"agent_name": "Chris",
		"question":   "test question",
	})

	if result.IsError {
		t.Fatalf("Execute returned error: %v", result.Error)
	}

	entries, _ := ms.LoadRecentDelegations(ctx, "unknown", "Chris", 10)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry stored under 'unknown', got %d", len(entries))
	}
	if entries[0].From != "unknown" {
		t.Errorf("From: got %q, want unknown", entries[0].From)
	}
}

// TestConsultAgentTool_DepthLimitPreventsMemoryWrite verifies that the delegation
// is not persisted when depth limit blocks execution.
func TestConsultAgentTool_DepthLimitPreventsMemoryWrite(t *testing.T) {
	ms := openTestMemoryStore(t, "test-machine")
	reg := makeInternalTestRegistry()
	mb := &internalMockBackend{response: "response"}
	depth := int32(1) // already at limit

	tool := NewConsultAgentToolWithMemory(reg, mb, &depth, nil, nil, nil, ms, "Mark")

	ctx := context.Background()
	result := tool.Execute(ctx, map[string]any{
		"agent_name": "Chris",
		"question":   "should not be stored",
	})

	if !result.IsError {
		t.Error("expected error when depth limit reached")
	}

	// Nothing should be stored
	entries, _ := ms.LoadRecentDelegations(ctx, "Mark", "Chris", 10)
	if len(entries) != 0 {
		t.Errorf("expected 0 entries when depth limit prevents execution, got %d", len(entries))
	}
}

// Verify that the new constructor uses the depth counter correctly.
func TestNewConsultAgentToolWithMemory_DepthAtomic(t *testing.T) {
	reg := makeInternalTestRegistry()
	mb := &internalMockBackend{response: "hello"}
	var depth int32
	ms := openTestMemoryStore(t, "m")

	tool := NewConsultAgentToolWithMemory(reg, mb, &depth, nil, nil, nil, ms, "Mark")

	// Execute should succeed and depth should return to 0
	result := tool.Execute(context.Background(), map[string]any{
		"agent_name": "Chris",
		"question":   "test",
	})
	if result.IsError {
		t.Fatalf("Execute: %v", result.Error)
	}
	if atomic.LoadInt32(&depth) != 0 {
		t.Errorf("expected depth to return to 0 after Execute, got %d", atomic.LoadInt32(&depth))
	}
}
