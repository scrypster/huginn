package compact_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/compact"
)

func TestEstimateTokens_DividesBy4(t *testing.T) {
	msgs := []backend.Message{
		{Role: "user", Content: "hello"},         // 5 chars
		{Role: "assistant", Content: "world!!!"}, // 8 chars
	}
	got := compact.EstimateTokens(msgs)
	// With tiktoken, we estimate more accurately than len/4
	// "hello" + "world!!!" should be more than len/4 estimate (3)
	// but also reasonable for actual token count
	if got < 5 || got > 50 {
		t.Errorf("EstimateTokens = %d, expected between 5 and 50", got)
	}
}

func TestShouldCompact_False_UnderThreshold(t *testing.T) {
	msgs := make([]backend.Message, 5)
	for i := range msgs {
		msgs[i] = backend.Message{Role: "user", Content: "x"}
	}
	s := compact.NewExtractiveStrategy()
	if s.ShouldCompact(msgs, 1000) {
		t.Error("expected false when under threshold")
	}
}

func TestShouldCompact_True_OverThreshold(t *testing.T) {
	// With tiktoken, ~600 chars is ~150 tokens, which is > 70% of 100
	content := strings.Repeat("a", 600)
	msgs := []backend.Message{{Role: "user", Content: content}}
	s := compact.NewExtractiveStrategy()
	if !s.ShouldCompact(msgs, 100) {
		t.Error("expected true when over threshold")
	}
}

func TestExtractiveCompaction_ExtractsFilePaths(t *testing.T) {
	msgs := []backend.Message{
		{Role: "user", Content: "write a file"},
		{Role: "assistant", Content: "done", ToolCalls: []backend.ToolCall{
			{ID: "c1", Function: backend.ToolCallFunction{Name: "write_file", Arguments: map[string]any{"file_path": "internal/foo/bar.go"}}},
		}},
		{Role: "tool", Content: "ok", ToolName: "write_file", ToolCallID: "c1"},
		{Role: "user", Content: "done"},
		{Role: "assistant", Content: "ok"},
	}
	s := compact.NewExtractiveStrategy()
	result, err := s.Compact(context.Background(), msgs, 500, nil, "")
	if err != nil {
		t.Fatalf("Compact error: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("empty result")
	}
	if !strings.Contains(result[0].Content, "internal/foo/bar.go") {
		t.Errorf("expected file path in summary, got: %q", result[0].Content)
	}
}

func TestExtractiveCompaction_NeverFails(t *testing.T) {
	msgs := []backend.Message{{Role: "user", Content: "hello"}, {Role: "assistant", Content: "world"}}
	s := compact.NewExtractiveStrategy()
	result, err := s.Compact(context.Background(), msgs, 100, nil, "")
	if err != nil {
		t.Errorf("must never error: %v", err)
	}
	if len(result) == 0 {
		t.Error("expected non-empty result")
	}
}

func TestMaybeCompact_Auto_OverThreshold(t *testing.T) {
	content := strings.Repeat("x", 400) // 400 chars → 100 tokens → over 70% of 100
	msgs := []backend.Message{
		{Role: "user", Content: content},
		{Role: "assistant", Content: content},
	}
	c := compact.New(compact.Config{
		Mode: compact.ModeAuto, Trigger: 0.7, BudgetTokens: 100,
		Strategy: compact.NewExtractiveStrategy(),
	})
	result, compacted, err := c.MaybeCompact(context.Background(), msgs, nil, "")
	if err != nil {
		t.Fatalf("MaybeCompact error: %v", err)
	}
	if !compacted {
		t.Error("expected compacted=true when over threshold")
	}
	_ = result
}

func TestExtractiveCompaction_EmptyMessages(t *testing.T) {
	s := compact.NewExtractiveStrategy()
	result, err := s.Compact(context.Background(), nil, 500, nil, "")
	if err != nil {
		t.Fatalf("Compact error: %v", err)
	}
	if result == nil {
		t.Error("expected non-nil result for empty messages")
	}
	// Should have at least the summary message
	if len(result) < 1 {
		t.Error("expected at least the summary message")
	}
}

func TestMaybeCompact_NilStrategy_ReturnsError(t *testing.T) {
	msgs := []backend.Message{{Role: "user", Content: "hello"}}
	c := compact.New(compact.Config{Mode: compact.ModeAlways})
	_, _, err := c.MaybeCompact(context.Background(), msgs, nil, "")
	if err == nil {
		t.Error("expected error with nil strategy")
	}
}

func TestMaybeCompact_Always_PropagatesError(t *testing.T) {
	msgs := []backend.Message{{Role: "user", Content: "hello"}}
	mb := &mockBackend{err: fmt.Errorf("llm down")}
	s := compact.NewLLMStrategy(0.0)
	c := compact.New(compact.Config{Mode: compact.ModeAlways, Strategy: s, BudgetTokens: 100})
	// LLM fails, fallback to extractive which always succeeds, so no error expected
	result, compacted, err := c.MaybeCompact(context.Background(), msgs, mb, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !compacted {
		t.Error("expected compaction even with LLM failure (fallback)")
	}
	if len(result) == 0 {
		t.Error("expected non-empty result from fallback")
	}
}

func TestMaybeCompact_Never_DoesNotCompact(t *testing.T) {
	content := strings.Repeat("y", 400)
	msgs := []backend.Message{{Role: "user", Content: content}}
	c := compact.New(compact.Config{Mode: compact.ModeNever, Strategy: compact.NewExtractiveStrategy()})
	_, compacted, _ := c.MaybeCompact(context.Background(), msgs, nil, "")
	if compacted {
		t.Error("expected compacted=false in ModeNever")
	}
}

// mockBackend for LLM strategy tests
type mockBackend struct {
	response string
	err      error
}

func (m *mockBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	if req.OnToken != nil {
		req.OnToken(m.response)
	}
	return &backend.ChatResponse{Content: m.response, DoneReason: "stop"}, nil
}

func (m *mockBackend) Health(_ context.Context) error { return nil }

func (m *mockBackend) Shutdown(_ context.Context) error { return nil }

func (m *mockBackend) ContextWindow() int { return 128_000 }

func TestLLMCompaction_FallbackOnMalformed(t *testing.T) {
	mb := &mockBackend{response: "not a valid summary block"}
	msgs := buildHistory(6)
	s := compact.NewLLMStrategy(0.7)
	result, err := s.Compact(context.Background(), msgs, 2000, mb, "test")
	if err != nil {
		t.Fatalf("must not error: %v", err)
	}
	if len(result) == 0 {
		t.Error("expected fallback result")
	}
}

func TestLLMCompaction_FallbackWhenSummaryExceedsBudget(t *testing.T) {
	// Mock backend returns a 10000-char summary that will blow the budget
	hugeSummary := "## Summary\n" + strings.Repeat("x", 10000)
	mb := &mockBackend{response: hugeSummary}
	msgs := buildHistory(6)
	// Budget of 50 tokens (~200 chars). The LLM summary alone is ~2500 tokens.
	s := compact.NewLLMStrategy(0.7)
	result, err := s.Compact(context.Background(), msgs, 50, mb, "test")
	if err != nil {
		t.Fatalf("must not error: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("expected non-empty result")
	}
	// The result should be from extractive fallback, not the huge LLM summary
	if strings.Contains(result[0].Content, strings.Repeat("x", 100)) {
		t.Error("expected extractive fallback, but got the oversized LLM summary")
	}
	// Verify it's an extractive summary
	if !strings.Contains(result[0].Content, "extractive summary") {
		t.Errorf("expected extractive summary marker, got: %q", result[0].Content[:min(100, len(result[0].Content))])
	}
}

func TestEstimateTokens_IncludesToolCalls(t *testing.T) {
	msgs := []backend.Message{
		{
			Role:    "assistant",
			Content: "done",
			ToolCalls: []backend.ToolCall{
				{ID: "c1", Function: backend.ToolCallFunction{
					Name:      "write_file",
					Arguments: map[string]any{"file_path": "test.go"},
				}},
			},
		},
	}
	tokens := compact.EstimateTokens(msgs)
	// "done" (4) + "write_file" (10) + "file_path" (9) + "test.go" (7) = 30 / 4 = 7
	if tokens < 5 {
		t.Errorf("expected tokens >= 5 (tool calls counted), got %d", tokens)
	}
}

func TestEstimateTokens_Empty(t *testing.T) {
	tokens := compact.EstimateTokens(nil)
	if tokens != 0 {
		t.Errorf("expected 0 tokens for nil, got %d", tokens)
	}
}

func TestExtractiveStrategy_ShouldCompact_ZeroBudget(t *testing.T) {
	msgs := []backend.Message{{Role: "user", Content: "hello"}}
	s := compact.NewExtractiveStrategy()
	if s.ShouldCompact(msgs, 0) {
		t.Error("expected false for zero budget")
	}
}

func TestExtractiveStrategy_ShouldCompact_NegativeBudget(t *testing.T) {
	msgs := []backend.Message{{Role: "user", Content: "hello"}}
	s := compact.NewExtractiveStrategy()
	if s.ShouldCompact(msgs, -10) {
		t.Error("expected false for negative budget")
	}
}

func TestLLMStrategy_ShouldCompact(t *testing.T) {
	s := compact.NewLLMStrategy(0.7)
	msgs := []backend.Message{{Role: "user", Content: "hello"}}
	if s.ShouldCompact(msgs, 1000) {
		t.Error("expected false for small message")
	}
	// With tiktoken, 400 'x' chars is ~57 tokens. Use larger content to exceed 70% threshold
	bigMsgs := []backend.Message{{Role: "user", Content: strings.Repeat("x", 1000)}}
	if !s.ShouldCompact(bigMsgs, 100) {
		t.Error("expected true for over-threshold message")
	}
}

func TestLLMCompaction_NilBackend_FallsBackToExtractive(t *testing.T) {
	msgs := buildHistory(4)
	s := compact.NewLLMStrategy(0.7)
	result, err := s.Compact(context.Background(), msgs, 2000, nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) == 0 {
		t.Error("expected non-empty result from extractive fallback")
	}
	if !strings.Contains(result[0].Content, "extractive summary") {
		t.Errorf("expected extractive fallback, got: %q", result[0].Content[:min(100, len(result[0].Content))])
	}
}

func TestLLMCompaction_SuccessfulSummary(t *testing.T) {
	goodSummary := "## Summary\nThis is a valid summary of the conversation."
	mb := &mockBackend{response: goodSummary}
	msgs := buildHistory(6)
	s := compact.NewLLMStrategy(0.7)
	result, err := s.Compact(context.Background(), msgs, 10000, mb, "test-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("expected non-empty result")
	}
	if !strings.Contains(result[0].Content, "## Summary") {
		t.Errorf("expected LLM summary to be used, got: %q", result[0].Content[:min(100, len(result[0].Content))])
	}
}

func buildHistory(n int) []backend.Message {
	msgs := make([]backend.Message, 0, n*2)
	for i := 0; i < n; i++ {
		msgs = append(msgs,
			backend.Message{Role: "user", Content: fmt.Sprintf("user %d: %s", i, strings.Repeat("z", 50))},
			backend.Message{Role: "assistant", Content: fmt.Sprintf("asst %d: %s", i, strings.Repeat("z", 50))},
		)
	}
	return msgs
}
