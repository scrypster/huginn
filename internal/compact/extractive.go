package compact

import (
	"context"
	"fmt"
	"strings"

	"github.com/scrypster/huginn/internal/backend"
)

// ExtractiveStrategy implements CompactionStrategy using simple extraction and summarization.
type ExtractiveStrategy struct {
	trigger float64
}

// NewExtractiveStrategy creates a new ExtractiveStrategy with default trigger (0.7).
func NewExtractiveStrategy() *ExtractiveStrategy {
	return &ExtractiveStrategy{trigger: 0.7}
}

// NewExtractiveStrategyWithTrigger creates a new ExtractiveStrategy with a custom trigger.
func NewExtractiveStrategyWithTrigger(t float64) *ExtractiveStrategy {
	return &ExtractiveStrategy{trigger: t}
}

// ShouldCompact returns true if token usage exceeds the trigger threshold.
func (s *ExtractiveStrategy) ShouldCompact(messages []backend.Message, budgetTokens int) bool {
	if budgetTokens <= 0 {
		return false
	}
	return float64(EstimateTokens(messages))/float64(budgetTokens) >= s.trigger
}

// Compact creates a summary by extracting file paths and keeping recent exchanges.
func (s *ExtractiveStrategy) Compact(_ context.Context, messages []backend.Message, _ int, _ backend.Backend, _ string) ([]backend.Message, error) {
	filePaths := extractFilePaths(messages)
	tail := lastNExchanges(messages, 2)

	var sb strings.Builder
	sb.WriteString("## Summary\n_[Conversation compacted — extractive summary]_\n\n")

	if len(filePaths) > 0 {
		sb.WriteString("**Files touched:**\n")
		for _, p := range filePaths {
			fmt.Fprintf(&sb, "- %s\n", p)
		}
		sb.WriteString("\n")
	}

	fmt.Fprintf(&sb, "_(Prior context summarized; %d messages condensed.)_\n", len(messages))

	result := make([]backend.Message, 0, 1+len(tail))
	result = append(result, backend.Message{Role: "user", Content: sb.String()})
	result = append(result, tail...)

	return result, nil
}

// extractFilePaths extracts all unique file paths from tool call arguments.
func extractFilePaths(messages []backend.Message) []string {
	seen := make(map[string]struct{})
	var paths []string

	for _, m := range messages {
		for _, tc := range m.ToolCalls {
			for _, key := range []string{"file_path", "path"} {
				if v, ok := tc.Function.Arguments[key]; ok {
					if p, ok := v.(string); ok && p != "" {
						if _, dup := seen[p]; !dup {
							seen[p] = struct{}{}
							paths = append(paths, p)
						}
					}
				}
			}
		}
	}

	return paths
}

// lastNExchanges returns the last N user messages and all associated assistant/tool messages.
func lastNExchanges(messages []backend.Message, n int) []backend.Message {
	if len(messages) == 0 {
		return []backend.Message{}
	}

	var userIdx []int
	for i, m := range messages {
		if m.Role == "user" {
			userIdx = append(userIdx, i)
		}
	}

	if len(userIdx) <= n {
		return messages
	}

	return messages[userIdx[len(userIdx)-n]:]
}
