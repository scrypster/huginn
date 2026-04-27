package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/tools"
)

// TestRunnerFunc runs a test command and returns the result.
type TestRunnerFunc func(ctx context.Context, command string, workDir string, timeout time.Duration) tools.TestResult

// DebugLoop runs tests, feeds failures to the LLM, and retries until passing or exhausted.
// maxAttempts <= 0 defaults to 3.
// testRunnerOverride is optional; if provided, replaces the real test runner (for testing).
func (o *Orchestrator) DebugLoop(
	ctx context.Context,
	testCmd string,
	maxAttempts int,
	sandboxRoot string,
	timeout time.Duration,
	onToken func(string),
	onToolCall func(string, string, map[string]any),
	onToolDone func(string, string, tools.ToolResult),
	testRunnerOverride ...TestRunnerFunc,
) error {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	if timeout <= 0 {
		timeout = 120 * time.Second
	}

	runner := TestRunnerFunc(defaultTestRunner)
	if len(testRunnerOverride) > 0 && testRunnerOverride[0] != nil {
		runner = testRunnerOverride[0]
	}

	const maxAgentTurns = 20
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		o.sc.Record("debug_loop.attempt", float64(attempt+1), "cmd:"+testCmd)

		result := runner(ctx, testCmd, sandboxRoot, timeout)
		if result.Passed {
			o.sc.Record("debug_loop.passed", 1, fmt.Sprintf("attempt:%d", attempt+1))
			return nil
		}

		failureMsg := buildDebugPrompt(result, attempt+1, maxAttempts, testCmd)
		if err := o.AgentChat(ctx, failureMsg, maxAgentTurns, onToken, onToolCall, onToolDone, nil, nil, nil); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			o.sc.Record("debug_loop.llm_error", 1, fmt.Sprintf("attempt:%d", attempt+1))
		}
	}

	o.sc.Record("debug_loop.exhausted", 1, "cmd:"+testCmd)
	return fmt.Errorf("debug_loop: %d attempts exhausted without passing tests", maxAttempts)
}

func buildDebugPrompt(result tools.TestResult, attempt, maxAttempts int, testCmd string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Tests failed (attempt %d/%d). Command: %s\n\n", attempt, maxAttempts, testCmd)
	if len(result.Failed) > 0 {
		sb.WriteString("Failing tests:\n")
		for _, f := range result.Failed {
			fmt.Fprintf(&sb, "  - %s\n", f)
		}
		sb.WriteString("\n")
	}
	if result.Output != "" {
		output := result.Output
		if len(output) > 4000 {
			output = output[:4000] + "\n... [truncated]"
		}
		sb.WriteString("Test output:\n")
		sb.WriteString(output)
		sb.WriteString("\n")
	}
	sb.WriteString("\nPlease analyze the failures, fix the code, and make the tests pass.")
	return sb.String()
}

func defaultTestRunner(ctx context.Context, command string, workDir string, timeout time.Duration) tools.TestResult {
	t := &tools.RunTestsTool{SandboxRoot: workDir, Timeout: timeout}
	result := t.Execute(ctx, map[string]any{"command": command})
	var tr tools.TestResult
	if err := json.Unmarshal([]byte(result.Output), &tr); err != nil {
		tr.Passed = !result.IsError
		tr.Output = result.Output
	}
	return tr
}
