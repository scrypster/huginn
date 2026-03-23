package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestPromptTool_TemplateMode verifies that mode:"template" renders the template correctly.
func TestPromptTool_TemplateMode(t *testing.T) {
	body := "Hello {{name}}, you asked: {{question}}"
	schemaJSON := `{"type":"object","properties":{"name":{"type":"string"},"question":{"type":"string"}},"required":["name","question"]}`

	pt := &PromptTool{
		name:        "greet",
		description: "Greets a user",
		schemaJSON:  schemaJSON,
		body:        body,
		mode:        "template",
	}

	args := map[string]any{"name": "Alice", "question": "What time is it?"}
	result := pt.Execute(context.Background(), args)
	if result.IsError {
		t.Fatalf("Execute returned error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "Alice") || !strings.Contains(result.Output, "What time is it?") {
		t.Errorf("unexpected output: %q", result.Output)
	}
}

// TestPromptTool_ShellMode verifies shell mode executes a command and returns output.
func TestPromptTool_ShellMode(t *testing.T) {
	pt := &PromptTool{
		name:        "echo_tool",
		description: "Echoes input",
		schemaJSON:  `{"type":"object","properties":{"msg":{"type":"string"}},"required":["msg"]}`,
		body:        `echo {{msg}}`,
		mode:        "shell",
		shellBin:    "echo",
		shellArgs:   []string{"hello-from-shell"},
	}

	result := pt.Execute(context.Background(), map[string]any{"msg": "world"})
	if result.IsError {
		t.Fatalf("shell Execute failed: %s", result.Error)
	}
	if !strings.Contains(result.Output, "hello-from-shell") {
		t.Errorf("expected shell output, got: %q", result.Output)
	}
}

// TestPromptTool_ShellMode_Timeout verifies that shell mode respects timeouts.
func TestPromptTool_ShellMode_Timeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	pt := &PromptTool{
		name:             "sleep_tool",
		mode:             "shell",
		shellBin:         "sleep",
		shellArgs:        []string{"10"},
		shellTimeoutSecs: 1,
	}

	result := pt.Execute(ctx, map[string]any{})
	if !result.IsError {
		t.Error("expected timeout error from shell mode")
	}
}

// TestLoadToolsWithMode verifies that mode: field in frontmatter is parsed correctly.
func TestLoadToolsWithMode(t *testing.T) {
	dir := t.TempDir()
	// LoadToolsFromDir looks in skillDir/tools/*.md
	toolsDir := filepath.Join(dir, "tools")
	if err := os.MkdirAll(toolsDir, 0o750); err != nil {
		t.Fatalf("mkdir tools: %v", err)
	}
	toolMd := filepath.Join(toolsDir, "my_tool.md")
	content := "---\ntool: my_tool\ndescription: A test tool\nmode: shell\nshell: echo\nargs: [\"hello\"]\nschema:\n  type: object\n  properties:\n    x: {type: string}\n---\nsome body"
	if err := os.WriteFile(toolMd, []byte(content), 0644); err != nil {
		t.Fatalf("write tool md: %v", err)
	}

	loaded, err := LoadToolsFromDir(dir)
	if err != nil {
		t.Fatalf("LoadToolsFromDir: %v", err)
	}
	if len(loaded) == 0 {
		t.Fatal("expected at least 1 tool")
	}
	found := false
	for _, tool := range loaded {
		if tool.Name() == "my_tool" {
			found = true
			if tool.mode != "shell" {
				t.Errorf("expected mode 'shell', got %q", tool.mode)
			}
			if tool.shellBin != "echo" {
				t.Errorf("expected shellBin 'echo', got %q", tool.shellBin)
			}
			if len(tool.shellArgs) != 1 || tool.shellArgs[0] != "hello" {
				t.Errorf("expected shellArgs [hello], got %v", tool.shellArgs)
			}
		}
	}
	if !found {
		t.Error("my_tool not found in loaded tools")
	}
}

// TestPromptTool_AgentMode verifies agent mode returns a stub message (no orchestrator).
func TestPromptTool_AgentMode(t *testing.T) {
	pt := &PromptTool{
		name:         "agent_tool",
		description:  "An agent tool",
		mode:         "agent",
		agentModel:   "gpt-4o",
		budgetTokens: 1000,
	}

	result := pt.Execute(context.Background(), map[string]any{"task": "summarize this"})
	if result.IsError {
		t.Fatalf("agent mode returned error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "agent mode stub") {
		t.Errorf("expected stub message, got: %q", result.Output)
	}
	if !strings.Contains(result.Output, "gpt-4o") {
		t.Errorf("expected model name in output, got: %q", result.Output)
	}
}

// TestPromptTool_ShellMode_OutputCap verifies output is capped at maxOutputBytes.
func TestPromptTool_ShellMode_OutputCap(t *testing.T) {
	pt := &PromptTool{
		name:           "cap_test",
		mode:           "shell",
		shellBin:       "echo",
		shellArgs:      []string{"hello world this is a long output that exceeds the cap"},
		maxOutputBytes: 5, // cap at 5 bytes
	}

	result := pt.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	// The raw captured bytes are limited to 5; a truncation notice may be appended.
	// Verify the raw prefix is at most 5 bytes (the notice is appended after the cap).
	// The total output must be much smaller than the full echo output (~55 bytes).
	if len(result.Output) > 60 { // 5 bytes + len("\n[output truncated at 64KB]") = ~32
		t.Errorf("output far exceeds cap, got %d bytes: %q", len(result.Output), result.Output)
	}
	// Verify the captured content begins with "hello" (first 5 bytes).
	if !strings.HasPrefix(result.Output, "hello") {
		t.Errorf("expected output to start with 'hello', got: %q", result.Output)
	}
}
