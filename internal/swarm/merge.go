package swarm

import (
	"fmt"
	"strings"
	"time"
)

// SwarmResult holds the output from a single completed SwarmTask.
type SwarmResult struct {
	TaskID    string        // same as AgentID; both are provided for convenience
	AgentID   string
	AgentName string
	Output    string
	Err       error
	Duration  time.Duration // wall-clock time from task start to completion
	// CostUSD is an approximation of the LLM cost for this task derived from
	// token counts and the model's public pricing. Not suitable for billing —
	// use it for relative cost awareness and swarm budget tracking only.
	CostUSD float64
	// Model is the model identifier used for this task (e.g. "claude-sonnet-4-5").
	Model string
}

// MergeStrategy defines how to combine results from multiple completed tasks.
type MergeStrategy int

const (
	// MergeConcatenate joins all task outputs with a separator.
	MergeConcatenate MergeStrategy = iota
	// MergeStructured returns results with metadata headers.
	MergeStructured
	// MergeLLMSummarize uses a caller-provided function to synthesize results.
	MergeLLMSummarize
)

// MergeResults combines SwarmResults according to the given strategy.
// For MergeLLMSummarize, mergeFn must be non-nil; for others it is ignored.
func MergeResults(results []SwarmResult, strategy MergeStrategy, separator string, mergeFn func([]SwarmResult) (string, error)) (string, error) {
	switch strategy {
	case MergeConcatenate:
		parts := make([]string, 0, len(results))
		for _, r := range results {
			if r.Output != "" {
				parts = append(parts, r.Output)
			}
		}
		return strings.Join(parts, separator), nil
	case MergeStructured:
		var sb strings.Builder
		for _, r := range results {
			fmt.Fprintf(&sb, "=== %s (%.3fs, $%.4f) ===\n%s\n",
				r.AgentName, r.Duration.Seconds(), r.CostUSD, r.Output)
		}
		return sb.String(), nil
	case MergeLLMSummarize:
		if mergeFn == nil {
			return "", fmt.Errorf("mergeFn required for MergeLLMSummarize strategy")
		}
		return mergeFn(results)
	default:
		return "", fmt.Errorf("unknown merge strategy: %d", strategy)
	}
}
