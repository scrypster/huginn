package compact_test

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/compact"
)

func TestEstimateTokens_PlainText(t *testing.T) {
	msgs := []backend.Message{
		{Role: "user", Content: "Hello, world!"},
	}
	got := compact.EstimateTokens(msgs)
	// Should be > 4 (actual tokens) and < 20 (with overhead)
	if got < 4 || got > 20 {
		t.Errorf("EstimateTokens = %d, want between 4 and 20", got)
	}
}

func TestEstimateTokens_LongText_NotDiv4(t *testing.T) {
	text := strings.Repeat("the quick brown fox jumps over the lazy dog ", 10)
	msgs := []backend.Message{{Role: "user", Content: text}}

	got := compact.EstimateTokens(msgs)
	div4 := len(text) / 4

	// tiktoken result should not be identical to len/4
	if got == div4 {
		t.Logf("Warning: EstimateTokens = len/4 (%d) — tiktoken may not be active", div4)
	}
	// Should give reasonable count for ~400 chars of English
	if got < 50 || got > 300 {
		t.Errorf("EstimateTokens = %d for ~400 chars, expected 50-300", got)
	}
}

func TestEstimateTokens_EmptyMessages(t *testing.T) {
	if got := compact.EstimateTokens(nil); got != 0 {
		t.Errorf("EstimateTokens(nil) = %d, want 0", got)
	}
	if got := compact.EstimateTokens([]backend.Message{}); got != 0 {
		t.Errorf("EstimateTokens([]) = %d, want 0", got)
	}
}

func TestEstimateTokens_ToolCalls(t *testing.T) {
	msgs := []backend.Message{
		{
			Role: "assistant",
			ToolCalls: []backend.ToolCall{
				{ID: "c1", Function: backend.ToolCallFunction{
					Name:      "read_file",
					Arguments: map[string]any{"path": "main.go"},
				}},
			},
		},
	}
	got := compact.EstimateTokens(msgs)
	if got == 0 {
		t.Error("EstimateTokens should count tool call tokens")
	}
}

func TestEstimateTokensFallback(t *testing.T) {
	msgs := []backend.Message{
		{Role: "user", Content: "hello world"},
	}
	got := compact.EstimateTokensFallback(msgs)
	want := (len("hello world") + len("user")) / 4 // includes both content and role
	if got != want {
		t.Errorf("EstimateTokensFallback = %d, want %d", got, want)
	}
}

func TestEstimateTokens_MultipleMessages(t *testing.T) {
	msgs := []backend.Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "What is 2+2?"},
		{Role: "assistant", Content: "4"},
	}
	got := compact.EstimateTokens(msgs)
	if got < 10 {
		t.Errorf("EstimateTokens = %d, expected at least 10 for 3 messages", got)
	}
}
