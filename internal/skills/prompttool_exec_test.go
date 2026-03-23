package skills

import (
	"context"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Template mode
// ---------------------------------------------------------------------------

func TestPromptTool_Template_BasicSubstitution(t *testing.T) {
	pt := &PromptTool{
		name: "greet",
		mode: "template",
		body: "Hello, {{.name}}! Welcome to {{.project}}.",
	}
	result := pt.Execute(context.Background(), map[string]any{
		"name":    "Steve",
		"project": "huginn",
	})
	if result.IsError {
		t.Fatalf("Execute: %v", result.Error)
	}
	if !strings.Contains(result.Output, "Hello, Steve!") {
		t.Errorf("unexpected result: %q", result.Output)
	}
	if !strings.Contains(result.Output, "huginn") {
		t.Errorf("expected project name in result: %q", result.Output)
	}
}

func TestPromptTool_Template_LegacySyntaxNoDot(t *testing.T) {
	// Legacy {{key}} syntax (no dot) must still work via normalization.
	pt := &PromptTool{
		name: "legacy",
		mode: "template",
		body: "Hello {{name}}, you asked: {{question}}",
	}
	result := pt.Execute(context.Background(), map[string]any{
		"name":     "Alice",
		"question": "What time is it?",
	})
	if result.IsError {
		t.Fatalf("Execute: %v", result.Error)
	}
	if !strings.Contains(result.Output, "Alice") {
		t.Errorf("name not substituted: %q", result.Output)
	}
	if !strings.Contains(result.Output, "What time is it?") {
		t.Errorf("question not substituted: %q", result.Output)
	}
}

func TestPromptTool_Template_FuncMap_Upper(t *testing.T) {
	pt := &PromptTool{
		name: "safe",
		mode: "template",
		body: "{{upper .name}}",
	}
	result := pt.Execute(context.Background(), map[string]any{"name": "hello"})
	if result.IsError {
		t.Fatalf("upper func: %v", result.Error)
	}
	if result.Output != "HELLO" {
		t.Errorf("upper: got %q, want HELLO", result.Output)
	}
}

func TestPromptTool_Template_FuncMap_Lower(t *testing.T) {
	pt := &PromptTool{
		name: "lower-test",
		mode: "template",
		body: "{{lower .msg}}",
	}
	result := pt.Execute(context.Background(), map[string]any{"msg": "WORLD"})
	if result.IsError {
		t.Fatalf("lower func: %v", result.Error)
	}
	if result.Output != "world" {
		t.Errorf("lower: got %q, want 'world'", result.Output)
	}
}

func TestPromptTool_Template_FuncMap_Trim(t *testing.T) {
	pt := &PromptTool{
		name: "trim-test",
		mode: "template",
		body: "|{{trim .s}}|",
	}
	result := pt.Execute(context.Background(), map[string]any{"s": "  spaces  "})
	if result.IsError {
		t.Fatalf("trim func: %v", result.Error)
	}
	if result.Output != "|spaces|" {
		t.Errorf("trim: got %q, want '|spaces|'", result.Output)
	}
}

func TestPromptTool_Template_FuncMap_Replace(t *testing.T) {
	pt := &PromptTool{
		name: "replace-test",
		mode: "template",
		body: `{{replace .s "foo" "bar"}}`,
	}
	result := pt.Execute(context.Background(), map[string]any{"s": "foo foo foo"})
	if result.IsError {
		t.Fatalf("replace func: %v", result.Error)
	}
	if result.Output != "bar bar bar" {
		t.Errorf("replace: got %q, want 'bar bar bar'", result.Output)
	}
}

func TestPromptTool_Template_InvalidTemplate(t *testing.T) {
	pt := &PromptTool{
		name: "bad",
		mode: "template",
		body: "{{.unclosed",
	}
	result := pt.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for invalid template")
	}
}

func TestPromptTool_Template_MissingKeyReturnsEmpty(t *testing.T) {
	// Phase 2: missingkey=zero — unknown keys produce empty string, not an error.
	pt := &PromptTool{
		name: "missing-key",
		mode: "template",
		body: "Value: {{.missing}}",
	}
	result := pt.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.Output != "Value: " {
		t.Errorf("got %q, want 'Value: '", result.Output)
	}
}

func TestPromptTool_Template_NoExecFunctionAvailable(t *testing.T) {
	// The FuncMap must NOT expose exec or any dangerous function.
	// Attempting to use an unknown function should produce a parse error.
	pt := &PromptTool{
		name: "no-exec",
		mode: "template",
		body: "{{exec .cmd}}",
	}
	result := pt.Execute(context.Background(), map[string]any{"cmd": "id"})
	// text/template should error because "exec" is not in the FuncMap.
	if !result.IsError {
		t.Error("expected parse error for undefined 'exec' function in FuncMap")
	}
}

// ---------------------------------------------------------------------------
// Shell mode — body-as-template (no shellBin)
// ---------------------------------------------------------------------------

func TestPromptTool_Shell_BodyTemplate_BasicCommand(t *testing.T) {
	pt := &PromptTool{
		name:             "echo-body",
		mode:             "shell",
		body:             "echo hello",
		shellTimeoutSecs: 5,
	}
	result := pt.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("Execute shell: %v", result.Error)
	}
	if !strings.Contains(result.Output, "hello") {
		t.Errorf("expected 'hello' in output, got: %q", result.Output)
	}
}

func TestPromptTool_Shell_BodyTemplate_ArgSubstitution(t *testing.T) {
	pt := &PromptTool{
		name: "echo-arg",
		mode: "shell",
		body: "echo {{.word}}",
	}
	result := pt.Execute(context.Background(), map[string]any{"word": "huginn"})
	if result.IsError {
		t.Fatalf("Execute shell: %v", result.Error)
	}
	if !strings.Contains(result.Output, "huginn") {
		t.Errorf("expected 'huginn' in output, got: %q", result.Output)
	}
}

func TestPromptTool_Shell_NoShellInjection(t *testing.T) {
	// Semicolons in args must NOT be interpreted by a shell — exec.Command is used directly.
	// "echo hello; echo injected" with exec.Command("echo", "hello;", "echo", "injected")
	// produces literal output "hello; echo injected", not two separate commands.
	pt := &PromptTool{
		name: "injection-test",
		mode: "shell",
		body: "echo hello; echo injected",
	}
	result := pt.Execute(context.Background(), map[string]any{})
	// With exec.Command (no sh), semicolons are literal args, not command separators.
	// The output should NOT contain "injected" as a separate line.
	if result.IsError {
		// Command may error because ";" is not valid as an echo argument that spawns commands.
		// That's fine — the important thing is no injection.
		return
	}
	// If it succeeded, verify "injected" only appears as part of a literal string, not a
	// separately executed command. The output should be a single line.
	lines := strings.Split(strings.TrimSpace(result.Output), "\n")
	if len(lines) > 1 && lines[1] == "injected" {
		t.Error("shell injection succeeded — must use exec.Command, not sh -c")
	}
}

func TestPromptTool_Shell_Timeout(t *testing.T) {
	pt := &PromptTool{
		name:             "slow",
		mode:             "shell",
		shellBin:         "sleep",
		shellArgs:        []string{"10"},
		shellTimeoutSecs: 0, // rely on context timeout below
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result := pt.Execute(ctx, map[string]any{})
	if !result.IsError {
		t.Error("expected timeout error for slow command")
	}
	if !strings.Contains(result.Error, "timed out") {
		t.Errorf("error should mention timeout: %v", result.Error)
	}
}

func TestPromptTool_Shell_PerToolTimeout(t *testing.T) {
	// shellTimeoutSecs=1 with a sleep 10 command — should time out in ~1 second.
	// Use a very short context to avoid slow test.
	pt := &PromptTool{
		name:             "slow2",
		mode:             "shell",
		shellBin:         "sleep",
		shellArgs:        []string{"10"},
		shellTimeoutSecs: 1,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result := pt.Execute(ctx, map[string]any{})
	if !result.IsError {
		t.Error("expected timeout error from per-tool timeout")
	}
}

func TestPromptTool_Shell_LargeOutputCapped(t *testing.T) {
	// "yes huginn" generates infinite output; should be capped at 64KB + notice.
	pt := &PromptTool{
		name:             "bigout",
		mode:             "shell",
		shellBin:         "yes",
		shellArgs:        []string{"huginn"},
		shellTimeoutSecs: 0,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	result := pt.Execute(ctx, map[string]any{})
	// Whether it times out or is capped, output should not exceed ~70KB
	if len(result.Output) > 70000 {
		t.Errorf("output not capped: %d bytes", len(result.Output))
	}
}

func TestPromptTool_Shell_EmptyBody_Error(t *testing.T) {
	// Empty body with no shellBin should produce an error, not a panic.
	pt := &PromptTool{
		name: "empty-cmd",
		mode: "shell",
		body: "   ", // whitespace only
	}
	result := pt.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for empty command")
	}
}

// ---------------------------------------------------------------------------
// Schema validation
// ---------------------------------------------------------------------------

func TestPromptTool_Schema_RequiredArgMissing(t *testing.T) {
	pt := &PromptTool{
		name: "strict",
		mode: "template",
		body: "{{.branch}}",
		schemaJSON: `{
			"type": "object",
			"required": ["branch"],
			"properties": {
				"branch": {"type": "string"}
			}
		}`,
	}

	// Should fail without required arg — validateArgs is called from Execute indirectly.
	// (Phase 2: validateArgs is available as a helper but Execute does not gate on it by default.
	//  Tests here exercise validateArgs directly since Execute does not call it yet.)
	err := pt.validateArgs(map[string]any{})
	if err == nil {
		t.Error("expected error for missing required arg")
	}
	if !strings.Contains(err.Error(), "branch") {
		t.Errorf("error should mention 'branch': %v", err)
	}
}

func TestPromptTool_Schema_RequiredArgPresent(t *testing.T) {
	pt := &PromptTool{
		name: "strict",
		mode: "template",
		body: "{{.branch}}",
		schemaJSON: `{
			"type": "object",
			"required": ["branch"],
			"properties": {
				"branch": {"type": "string"}
			}
		}`,
	}

	err := pt.validateArgs(map[string]any{"branch": "main"})
	if err != nil {
		t.Fatalf("expected no error with required arg present: %v", err)
	}
}

func TestPromptTool_Schema_NoSchema_AcceptsAnything(t *testing.T) {
	pt := &PromptTool{
		name:       "open",
		mode:       "template",
		body:       "ok",
		schemaJSON: "{}",
	}
	err := pt.validateArgs(map[string]any{"anything": "goes"})
	if err != nil {
		t.Fatalf("expected no error with empty schema: %v", err)
	}
}

func TestPromptTool_Schema_PatternConstraint_Valid(t *testing.T) {
	pt := &PromptTool{
		name: "pattern-test",
		mode: "template",
		body: "{{.branch}}",
		schemaJSON: `{
			"type": "object",
			"properties": {
				"branch": {"type": "string", "pattern": "^[a-z0-9/-]+$"}
			}
		}`,
	}
	err := pt.validateArgs(map[string]any{"branch": "feature/my-branch-123"})
	if err != nil {
		t.Fatalf("valid pattern should not error: %v", err)
	}
}

func TestPromptTool_Schema_PatternConstraint_Invalid(t *testing.T) {
	pt := &PromptTool{
		name: "pattern-test",
		mode: "template",
		body: "{{.branch}}",
		schemaJSON: `{
			"type": "object",
			"properties": {
				"branch": {"type": "string", "pattern": "^[a-z0-9/-]+$"}
			}
		}`,
	}
	err := pt.validateArgs(map[string]any{"branch": "UPPERCASE_NOT_ALLOWED"})
	if err == nil {
		t.Error("expected pattern mismatch error")
	}
	if !strings.Contains(err.Error(), "pattern") {
		t.Errorf("error should mention 'pattern': %v", err)
	}
}

// ---------------------------------------------------------------------------
// Agent mode — depth limit
// ---------------------------------------------------------------------------

func TestPromptTool_AgentMode_Stub(t *testing.T) {
	pt := &PromptTool{
		name:         "agent_tool",
		mode:         "agent",
		agentModel:   "gpt-4o",
		budgetTokens: 1000,
	}
	result := pt.Execute(context.Background(), map[string]any{"task": "summarize this"})
	if result.IsError {
		t.Fatalf("agent mode returned unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "agent mode stub") {
		t.Errorf("expected stub message, got: %q", result.Output)
	}
	if !strings.Contains(result.Output, "gpt-4o") {
		t.Errorf("expected model name in output, got: %q", result.Output)
	}
}

func TestPromptTool_AgentMode_DepthLimitReached(t *testing.T) {
	pt := &PromptTool{
		name:     "nested",
		mode:     "agent",
		body:     "You are a helper",
		maxDepth: 3,
		depth:    3, // already at max
	}
	result := pt.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected depth limit error")
	}
	if !strings.Contains(result.Error, "depth") {
		t.Errorf("error should mention depth: %v", result.Error)
	}
}

func TestPromptTool_AgentMode_DepthLimitNotYetReached(t *testing.T) {
	pt := &PromptTool{
		name:     "nested",
		mode:     "agent",
		maxDepth: 3,
		depth:    2, // one below max — should not error
	}
	result := pt.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Errorf("should not error when depth < maxDepth: %v", result.Error)
	}
}

func TestPromptTool_AgentMode_DefaultDepthLimit(t *testing.T) {
	// maxDepth=0 should use the default (5). depth=5 should trigger the limit.
	pt := &PromptTool{
		name:     "default-depth",
		mode:     "agent",
		maxDepth: 0, // use default
		depth:    5, // equals default max of 5
	}
	result := pt.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected depth limit error at default max depth")
	}
}

// ---------------------------------------------------------------------------
// normalizeTemplateSyntax helper
// ---------------------------------------------------------------------------

func TestNormalizeTemplateSyntax_BareIdent(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"{{name}}", "{{.name}}"},
		{"{{foo_bar}}", "{{.foo_bar}}"},
		{"{{.name}}", "{{.name}}"}, // already has dot — unchanged
		{"{{upper .name}}", "{{upper .name}}"}, // function call — unchanged
		{"{{if .x}}ok{{end}}", "{{if .x}}ok{{end}}"}, // directives — unchanged
		{"Hello {{name}}, {{.surname}}", "Hello {{.name}}, {{.surname}}"},
	}
	for _, tc := range cases {
		got := normalizeTemplateSyntax(tc.in)
		if got != tc.want {
			t.Errorf("normalizeTemplateSyntax(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
