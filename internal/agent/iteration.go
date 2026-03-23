package agent

import (
	"context"
	"fmt"
	"strings"
)

// StreamFunc is the minimal streaming interface used by the iteration engine.
// msgs is a flat slice alternating user/assistant messages (index 0=user, 1=assistant, etc.)
type StreamFunc func(ctx context.Context, msgs []string, onToken func(string)) error

// iterate runs n refinement passes, feeding each output back as context.
// Each pass appends the previous result and asks the model to critique and harden.
// n is clamped to a minimum of 1.
func iterate(ctx context.Context, n int, section string, stream StreamFunc) (string, error) {
	if n < 1 {
		n = 1
	}

	var history []string
	var last string

	for i := 0; i < n; i++ {
		prompt := buildIterationPrompt(section, last, i+1, n)
		history = append(history, prompt)

		var buf strings.Builder
		// Pass a snapshot so the stream function cannot accidentally mutate our history.
		historyCopy := make([]string, len(history))
		copy(historyCopy, history)
		if err := stream(ctx, historyCopy, func(token string) {
			buf.WriteString(token)
		}); err != nil {
			return last, fmt.Errorf("iteration %d/%d: %w", i+1, n, err)
		}
		last = buf.String()
		// Append the model's response to history so next round has full context.
		history = append(history, last)
	}
	return last, nil
}

// buildIterationPrompt constructs the prompt for a given refinement round.
func buildIterationPrompt(section, prev string, round, total int) string {
	if round == 1 {
		return fmt.Sprintf(
			"Please analyze and implement the following, reasoning carefully:\n\n%s",
			section,
		)
	}
	return fmt.Sprintf(
		"Round %d of %d. Critique your previous response below and produce a hardened, "+
			"improved version. Fix edge cases, improve error handling, make it production-ready.\n\n"+
			"Previous response:\n%s\n\nSection to refine:\n%s",
		round, total, prev, section,
	)
}
