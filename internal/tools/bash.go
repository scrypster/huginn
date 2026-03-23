package tools

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/scrypster/huginn/internal/agent/session"
	"github.com/scrypster/huginn/internal/backend"
)

const (
	maxOutputBytes = 100 * 1024 // 100KB output cap
	bashMaxTimeout = 3600       // maximum allowed timeout in seconds (1 hour)
)

// BashTool executes shell commands in the sandbox root.
type BashTool struct {
	SandboxRoot string
	Timeout     time.Duration
}

func (t *BashTool) Name() string { return "bash" }
func (t *BashTool) Description() string {
	return "Execute a bash command in the project directory. Returns stdout and stderr."
}
func (t *BashTool) Permission() PermissionLevel { return PermExec }

func (t *BashTool) Schema() backend.Tool {
	return backend.Tool{
		Type: "function",
		Function: backend.ToolFunction{
			Name:        "bash",
			Description: t.Description(),
			Parameters: backend.ToolParameters{
				Type:     "object",
				Required: []string{"command"},
				Properties: map[string]backend.ToolProperty{
					"command": {Type: "string", Description: "The bash command to execute"},
					"timeout": {Type: "integer", Description: "Timeout in seconds (default 120)"},
				},
			},
		},
	}
}

func (t *BashTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	command, ok := args["command"].(string)
	if !ok || strings.TrimSpace(command) == "" {
		return ToolResult{IsError: true, Error: "bash: 'command' argument required"}
	}

	timeout := t.Timeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	if secs, ok := args["timeout"]; ok {
		var rawSecs float64
		switch v := secs.(type) {
		case float64:
			rawSecs = v
		case int:
			rawSecs = float64(v)
		case int64:
			rawSecs = float64(v)
		}
		if rawSecs < 0 {
			return ToolResult{IsError: true, Error: "bash: timeout must be non-negative"}
		}
		if rawSecs > 0 {
			if rawSecs > bashMaxTimeout {
				slog.Warn("bash: timeout exceeds maximum; capping", "requested_seconds", rawSecs, "cap_seconds", bashMaxTimeout)
				rawSecs = bashMaxTimeout
			}
			timeout = time.Duration(rawSecs) * time.Second
		}
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "--norc", "--noprofile", "-c", command)
	cmd.Dir = t.SandboxRoot
	if sessionEnv := session.EnvFrom(ctx); len(sessionEnv) > 0 {
		cmd.Env = mergeEnv(os.Environ(), sessionEnv)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
	}

	outStr := truncate(stdout.String(), maxOutputBytes)
	errStr := truncate(stderr.String(), maxOutputBytes)

	isError := exitCode != 0
	return ToolResult{
		Output:  outStr,
		Error:   errStr,
		IsError: isError,
		Metadata: map[string]any{
			"exit_code": exitCode,
		},
	}
}

func truncate(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes] + fmt.Sprintf("\n... [truncated, %d bytes total]", len(s))
}

// mergeEnv merges base env with overrides. Overrides take precedence.
// Entries with empty values (e.g., "BASH_ENV=") are kept as-is (they unset the var).
func mergeEnv(base, overrides []string) []string {
	env := make(map[string]string, len(base))
	for _, e := range base {
		k, v, _ := strings.Cut(e, "=")
		env[k] = v
	}
	for _, e := range overrides {
		k, v, _ := strings.Cut(e, "=")
		env[k] = v
	}
	result := make([]string, 0, len(env))
	for k, v := range env {
		result = append(result, k+"="+v)
	}
	return result
}
