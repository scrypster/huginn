package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/backend"
)

const maxRunTestsOutputBytes = 100 * 1024

// TestResult is the structured output of a run_tests invocation.
type TestResult struct {
	Passed bool     `json:"passed"`
	Failed []string `json:"failed"`
	Output string   `json:"output"`
}

// RunTestsTool implements tools.Tool for run_tests.
type RunTestsTool struct {
	SandboxRoot string
	Timeout     time.Duration
}

func (t *RunTestsTool) Name() string { return "run_tests" }

func (t *RunTestsTool) Permission() PermissionLevel { return PermExec }

func (t *RunTestsTool) Description() string {
	return "Run tests and return structured results. For go test commands, parses JSON output for structured failures."
}

func (t *RunTestsTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "run_tests",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"command"},
				Properties: map[string]backend.ToolProperty{
					"command": {
						Type:        "string",
						Description: "Test command (e.g. 'go test ./...' or 'make test')",
					},
					"working_dir": {
						Type:        "string",
						Description: "Working directory relative to sandbox (optional)",
					},
					"timeout": {
						Type:        "integer",
						Description: "Timeout in seconds (default 120)",
					},
				},
			},
		},
	}
}

func (t *RunTestsTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	command, ok := args["command"].(string)
	if !ok || strings.TrimSpace(command) == "" {
		return ToolResult{IsError: true, Error: "run_tests: 'command' argument required"}
	}

	timeout := t.Timeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	if secs, ok := args["timeout"]; ok {
		switch v := secs.(type) {
		case float64:
			if v > 0 {
				timeout = time.Duration(v) * time.Second
			}
		case int:
			if v > 0 {
				timeout = time.Duration(v) * time.Second
			}
		}
	}

	workDir := t.SandboxRoot
	if wd, ok := args["working_dir"].(string); ok && wd != "" {
		resolved, err := ResolveSandboxed(t.SandboxRoot, wd)
		if err != nil {
			return ToolResult{IsError: true, Error: err.Error()}
		}
		workDir = resolved
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	isGoTest := strings.Contains(command, "go test")
	actualCmd := command
	if isGoTest && !strings.Contains(command, "-json") {
		actualCmd = strings.Replace(command, "go test", "go test -json", 1)
	}

	cmd := exec.CommandContext(ctx, "bash", "-c", actualCmd)
	cmd.Dir = workDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	exitCode := 0
	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	combined := stdout.String() + stderr.String()
	if len(combined) > maxRunTestsOutputBytes {
		combined = combined[:maxRunTestsOutputBytes] + "... [truncated]"
	}

	var testResult TestResult
	if isGoTest {
		testResult = ParseGoTestJSON(stdout.Bytes())
		testResult.Output = combined
		if exitCode != 0 && testResult.Passed {
			testResult.Passed = false
		}
	} else {
		testResult = TestResult{Passed: exitCode == 0, Output: combined}
	}

	resultJSON, _ := json.Marshal(testResult)
	return ToolResult{
		Output:  string(resultJSON),
		Error:   stderr.String(),
		IsError: exitCode != 0,
		Metadata: map[string]any{
			"exit_code":    exitCode,
			"passed":       testResult.Passed,
			"failed_count": len(testResult.Failed),
			"failures":     testResult.Failed,
		},
	}
}

type goTestEvent struct {
	Action  string  `json:"Action"`
	Test    string  `json:"Test"`
	Output  string  `json:"Output"`
	Elapsed float64 `json:"Elapsed"`
}

// ParseGoTestJSON parses go test -json output and returns a TestResult.
// Exported for testing.
func ParseGoTestJSON(data []byte) TestResult {
	var failed []string
	packageFailed := false
	var outputBuf strings.Builder

	for _, line := range bytes.Split(data, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		var event goTestEvent
		if err := json.Unmarshal(trimmed, &event); err != nil {
			outputBuf.Write(line)
			outputBuf.WriteByte('\n')
			continue
		}
		switch event.Action {
		case "fail":
			if event.Test != "" {
				failed = append(failed, event.Test)
			} else {
				packageFailed = true
			}
		case "output":
			if event.Output != "" {
				outputBuf.WriteString(event.Output)
			}
		}
	}

	return TestResult{
		Passed: !packageFailed && len(failed) == 0,
		Failed: failed,
		Output: outputBuf.String(),
	}
}
