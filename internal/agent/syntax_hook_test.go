package agent

// syntax_hook_test.go — TDD coverage for G1 edit-time syntax validation,
// wired as a PreToolUse/PostToolUse pair (dogfooding G10).
//
// These tests run real write_file/edit_file tools through dispatchTools
// with SyntaxValidationHook registered, in a temp dir chdir'd as both the
// sandbox root and the process cwd — previewWrite (shared with
// OnBeforeWrite's diff preview) resolves paths relative to cwd, so tests
// must match that to exercise the real path the production code takes.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

func syntaxTestSandbox(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	return dir
}

func writeFileRegistry(sandbox string) *tools.Registry {
	return newRegistryWith(
		&tools.WriteFileTool{SandboxRoot: sandbox},
		&tools.EditFileTool{SandboxRoot: sandbox},
	)
}

const validGoSource = "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"
const brokenGoSource = "package main\n\nfunc main() {\n\tprintln(\"hi\"\n" // missing closing paren and brace

// TestSyntaxHook_ValidGoWritePasses verifies a syntactically valid write_file
// is allowed through unmodified in block mode (the default).
func TestSyntaxHook_ValidGoWritePasses(t *testing.T) {
	sandbox := syntaxTestSandbox(t)
	reg := writeFileRegistry(sandbox)

	hooks := NewHookRegistry()
	RegisterSyntaxValidation(hooks, nil) // nil modeFn -> default "block"
	cfg := &RunLoopConfig{Tools: reg, Hooks: hooks}

	calls := []backend.ToolCall{{
		ID: "c1",
		Function: backend.ToolCallFunction{
			Name: "write_file",
			Arguments: map[string]any{
				"file_path": "good.go",
				"content":   validGoSource,
			},
		},
	}}
	results := cfg.dispatchTools(context.Background(), calls)
	if strings.Contains(results[0].content, "blocked") {
		t.Fatalf("valid Go write was blocked: %q", results[0].content)
	}
	got, err := os.ReadFile(filepath.Join(sandbox, "good.go"))
	if err != nil {
		t.Fatalf("expected file to be written: %v", err)
	}
	if string(got) != validGoSource {
		t.Errorf("file content = %q, want %q", got, validGoSource)
	}
}

// TestSyntaxHook_BrokenGoEditRejected_BlockMode verifies a syntactically
// broken edit_file result is REJECTED with the parser error surfaced to the
// model, and that the file on disk is left completely unchanged.
func TestSyntaxHook_BrokenGoEditRejected_BlockMode(t *testing.T) {
	sandbox := syntaxTestSandbox(t)
	reg := writeFileRegistry(sandbox)

	original := validGoSource
	path := filepath.Join(sandbox, "target.go")
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	hooks := NewHookRegistry()
	RegisterSyntaxValidation(hooks, func() string { return "block" })
	cfg := &RunLoopConfig{Tools: reg, Hooks: hooks}

	calls := []backend.ToolCall{{
		ID: "c1",
		Function: backend.ToolCallFunction{
			Name: "edit_file",
			Arguments: map[string]any{
				"file_path":  "target.go",
				"old_string": "println(\"hi\")\n}\n",
				"new_string": "println(\"hi\"\n", // introduces the syntax break
			},
		},
	}}
	results := cfg.dispatchTools(context.Background(), calls)

	if !strings.Contains(results[0].content, "blocked") {
		t.Fatalf("expected a blocked result, got %q", results[0].content)
	}
	if !strings.Contains(results[0].content, "syntax validation failed") {
		t.Errorf("expected the parser error in the result, got %q", results[0].content)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(after) != original {
		t.Errorf("file was modified despite block-mode rejection: got %q, want %q", after, original)
	}
}

// TestSyntaxHook_WarnMode_WritesAndAnnotates verifies warn mode lets the
// write through but appends the syntax warning to the tool result.
func TestSyntaxHook_WarnMode_WritesAndAnnotates(t *testing.T) {
	sandbox := syntaxTestSandbox(t)
	reg := writeFileRegistry(sandbox)

	hooks := NewHookRegistry()
	RegisterSyntaxValidation(hooks, func() string { return "warn" })
	cfg := &RunLoopConfig{Tools: reg, Hooks: hooks}

	calls := []backend.ToolCall{{
		ID: "c1",
		Function: backend.ToolCallFunction{
			Name: "write_file",
			Arguments: map[string]any{
				"file_path": "warn.go",
				"content":   brokenGoSource,
			},
		},
	}}
	results := cfg.dispatchTools(context.Background(), calls)

	if strings.Contains(results[0].content, "blocked") {
		t.Fatalf("warn mode must not block, got %q", results[0].content)
	}
	if !strings.Contains(results[0].content, "syntax warning") {
		t.Errorf("expected an annotated warning in the result, got %q", results[0].content)
	}
	got, err := os.ReadFile(filepath.Join(sandbox, "warn.go"))
	if err != nil {
		t.Fatalf("expected file to be written in warn mode: %v", err)
	}
	if string(got) != brokenGoSource {
		t.Errorf("file content = %q, want the broken content actually written", got)
	}
}

// TestSyntaxHook_OffMode_Disabled verifies "off" writes broken content
// silently, with no denial and no annotation.
func TestSyntaxHook_OffMode_Disabled(t *testing.T) {
	sandbox := syntaxTestSandbox(t)
	reg := writeFileRegistry(sandbox)

	hooks := NewHookRegistry()
	RegisterSyntaxValidation(hooks, func() string { return "off" })
	cfg := &RunLoopConfig{Tools: reg, Hooks: hooks}

	calls := []backend.ToolCall{{
		ID: "c1",
		Function: backend.ToolCallFunction{
			Name: "write_file",
			Arguments: map[string]any{
				"file_path": "off.go",
				"content":   brokenGoSource,
			},
		},
	}}
	results := cfg.dispatchTools(context.Background(), calls)

	if strings.Contains(results[0].content, "blocked") || strings.Contains(results[0].content, "syntax") {
		t.Errorf("off mode must not deny or annotate, got %q", results[0].content)
	}
	if _, err := os.ReadFile(filepath.Join(sandbox, "off.go")); err != nil {
		t.Fatalf("expected file to be written in off mode: %v", err)
	}
}

// TestSyntaxHook_MissingPython3SkipsGracefully verifies that when python3 is
// not on PATH, a syntactically broken .py write is neither blocked nor
// warned about — the validator is simply not run, and the write proceeds.
func TestSyntaxHook_MissingPython3SkipsGracefully(t *testing.T) {
	sandbox := syntaxTestSandbox(t)
	reg := writeFileRegistry(sandbox)

	t.Setenv("PATH", "") // python3 cannot be found via exec.LookPath

	hooks := NewHookRegistry()
	RegisterSyntaxValidation(hooks, func() string { return "block" })
	cfg := &RunLoopConfig{Tools: reg, Hooks: hooks}

	brokenPython := "def broken(:\n    pass\n"
	calls := []backend.ToolCall{{
		ID: "c1",
		Function: backend.ToolCallFunction{
			Name: "write_file",
			Arguments: map[string]any{
				"file_path": "broken.py",
				"content":   brokenPython,
			},
		},
	}}
	results := cfg.dispatchTools(context.Background(), calls)

	if strings.Contains(results[0].content, "blocked") {
		t.Fatalf("missing python3 must skip validation, not block: %q", results[0].content)
	}
	got, err := os.ReadFile(filepath.Join(sandbox, "broken.py"))
	if err != nil {
		t.Fatalf("expected file to be written when validator is unavailable: %v", err)
	}
	if string(got) != brokenPython {
		t.Errorf("file content = %q, want %q", got, brokenPython)
	}
}

// TestSyntaxHook_NonGoNonPyExtension_PassesThroughUnchecked verifies an
// unsupported extension (e.g. .ts) is never blocked, and no warning is
// attached, per the documented v1 gap.
func TestSyntaxHook_NonGoNonPyExtension_PassesThroughUnchecked(t *testing.T) {
	sandbox := syntaxTestSandbox(t)
	reg := writeFileRegistry(sandbox)

	hooks := NewHookRegistry()
	RegisterSyntaxValidation(hooks, func() string { return "block" })
	cfg := &RunLoopConfig{Tools: reg, Hooks: hooks}

	brokenTS := "function broken( {\n"
	calls := []backend.ToolCall{{
		ID: "c1",
		Function: backend.ToolCallFunction{
			Name: "write_file",
			Arguments: map[string]any{
				"file_path": "broken.ts",
				"content":   brokenTS,
			},
		},
	}}
	results := cfg.dispatchTools(context.Background(), calls)
	if strings.Contains(results[0].content, "blocked") || strings.Contains(results[0].content, "syntax") {
		t.Errorf("TS/JS is a documented v1 gap and must pass through unchecked, got %q", results[0].content)
	}
}

// EnableToolHooks must actually wire G1 into the RunLoop the orchestrator
// drives — the hook agent flagged that the config field existed but no
// production call site populated it (Fable wiring, 2026-08-29).
func TestOrchestrator_EnableToolHooks_WiresRegistry(t *testing.T) {
	o := &Orchestrator{contextBuilder: NewContextBuilder(nil, nil, nil)}
	if o.toolHooks() != nil {
		t.Fatal("hooks should be nil before EnableToolHooks")
	}
	o.EnableToolHooks()
	reg := o.toolHooks()
	if reg == nil {
		t.Fatal("EnableToolHooks did not populate the registry")
	}
	// A syntactically broken Go write must be vetoed by the registered pre-hook.
	allow, reason := reg.runPre(context.Background(), "write_file",
		map[string]any{"file_path": "broken.go", "content": "package x\nfunc {"})
	if allow {
		t.Fatalf("broken Go write should be vetoed by syntax validation; got allow=%v reason=%q", allow, reason)
	}
}

// TestSyntaxHook_EmptyScaffoldNotBlocked verifies that write_file creating an
// empty (or whitespace-only) .go file is NOT blocked in block mode. Agents
// routinely scaffold an empty file and fill it in a following step; an absent
// file is not a malformed one, and every language validator would otherwise
// reject "" as a syntax error. Regression for the create-then-fill trap.
func TestSyntaxHook_EmptyScaffoldNotBlocked(t *testing.T) {
	for _, content := range []string{"", "   \n\t\n"} {
		sandbox := syntaxTestSandbox(t)
		reg := writeFileRegistry(sandbox)
		hooks := NewHookRegistry()
		RegisterSyntaxValidation(hooks, func() string { return "block" })
		cfg := &RunLoopConfig{Tools: reg, Hooks: hooks}

		calls := []backend.ToolCall{{
			ID: "c1",
			Function: backend.ToolCallFunction{
				Name: "write_file",
				Arguments: map[string]any{
					"file_path": "scaffold.go",
					"content":   content,
				},
			},
		}}
		results := cfg.dispatchTools(context.Background(), calls)
		if strings.Contains(results[0].content, "blocked") {
			t.Fatalf("empty/whitespace scaffold was blocked (content=%q): %q", content, results[0].content)
		}
		if _, err := os.Stat(filepath.Join(sandbox, "scaffold.go")); err != nil {
			t.Errorf("expected scaffold file to be written (content=%q): %v", content, err)
		}
	}
}
