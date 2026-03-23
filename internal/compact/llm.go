package compact

import (
	"context"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/backend"
)

// llmCompactionTimeout bounds LLM-based compaction so a hung backend cannot
// block the agent loop indefinitely. On timeout the strategy falls back to
// extractive compaction, which is deterministic and fast.
const llmCompactionTimeout = 30 * time.Second

const summaryPrompt = `Summarize this conversation. Preserve:
1. ALL file paths created or modified
2. All architectural decisions
3. Current task state
4. Any errors and resolutions

Return a ## Summary block only.`

// LLMStrategy implements CompactionStrategy using an LLM to generate summaries.
type LLMStrategy struct {
	trigger  float64
	fallback *ExtractiveStrategy
	verbatim int
}

// NewLLMStrategy creates a new LLMStrategy with the given trigger threshold.
func NewLLMStrategy(trigger float64) *LLMStrategy {
	return &LLMStrategy{
		trigger:  trigger,
		fallback: NewExtractiveStrategyWithTrigger(trigger),
		verbatim: 3,
	}
}

// ShouldCompact delegates to the fallback extractive strategy.
func (s *LLMStrategy) ShouldCompact(messages []backend.Message, budgetTokens int) bool {
	return s.fallback.ShouldCompact(messages, budgetTokens)
}

// Compact generates an LLM summary, falling back to extractive if LLM fails.
func (s *LLMStrategy) Compact(ctx context.Context, messages []backend.Message, budget int, b backend.Backend, model string) ([]backend.Message, error) {
	// If no backend, fall back to extractive
	if b == nil {
		return s.fallback.Compact(ctx, messages, budget, nil, "")
	}

	conv := buildConvText(messages)
	summaryText, err := callLLM(ctx, b, model, summaryPrompt+"\n\nConversation:\n"+conv)
	if err != nil {
		return s.fallback.Compact(ctx, messages, budget, nil, "")
	}

	// Check if summary contains the expected structure
	if !strings.Contains(summaryText, "## Summary") {
		// Retry once
		summaryText, err = callLLM(ctx, b, model, summaryPrompt+"\n\nConversation:\n"+conv)
		if err != nil || !strings.Contains(summaryText, "## Summary") {
			return s.fallback.Compact(ctx, messages, budget, nil, "")
		}
	}

	tail := lastNExchanges(messages, s.verbatim)
	result := make([]backend.Message, 0, 1+len(tail))
	result = append(result, backend.Message{Role: "user", Content: summaryText})
	result = append(result, tail...)

	// If the compacted result exceeds the budget, the LLM summary is too large.
	// Fall back to extractive which produces a deterministic, shorter summary.
	if EstimateTokens(result) > budget {
		return s.fallback.Compact(ctx, messages, budget, nil, "")
	}

	return result, nil
}

// callLLM sends a prompt to the backend and collects the response.
// A per-call timeout (llmCompactionTimeout) is applied so a hung backend
// cannot block compaction indefinitely — the caller falls back to extractive.
func callLLM(ctx context.Context, b backend.Backend, model, prompt string) (string, error) {
	llmCtx, cancel := context.WithTimeout(ctx, llmCompactionTimeout)
	defer cancel()
	var buf strings.Builder
	_, err := b.ChatCompletion(llmCtx, backend.ChatRequest{
		Model: model,
		Messages: []backend.Message{{Role: "user", Content: prompt}},
		OnToken: func(tok string) { buf.WriteString(tok) },
	})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

// buildConvText converts messages to a readable conversation format.
func buildConvText(messages []backend.Message) string {
	var sb strings.Builder
	for _, m := range messages {
		sb.WriteString(m.Role)
		sb.WriteString(": ")
		sb.WriteString(m.Content)
		for _, tc := range m.ToolCalls {
			sb.WriteString(" [tool_call: ")
			sb.WriteString(tc.Function.Name)
			sb.WriteString("]")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
