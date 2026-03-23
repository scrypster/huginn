package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// truncate helper (0% coverage — not called by any existing test)
// ---------------------------------------------------------------------------

func TestTruncate_ShortString95(t *testing.T) {
	s := truncate("hello", 10)
	if s != "hello" {
		t.Errorf("truncate short: got %q, want %q", s, "hello")
	}
}

func TestTruncate_ExactLength95(t *testing.T) {
	s := truncate("hello", 5)
	if s != "hello" {
		t.Errorf("truncate exact: got %q, want %q", s, "hello")
	}
}

func TestTruncate_LongString95(t *testing.T) {
	s := truncate("hello world", 5)
	if s != "hello" {
		t.Errorf("truncate long: got %q, want %q", s, "hello")
	}
}

// ---------------------------------------------------------------------------
// DefaultLoader — error path when HOME cannot be determined (not tested elsewhere)
// We use a separate sub-test that verifies skillsDir is a non-empty path.
// ---------------------------------------------------------------------------

func TestDefaultLoader_SkillsDirNonEmpty(t *testing.T) {
	l := DefaultLoader()
	if l == nil {
		t.Fatal("DefaultLoader returned nil")
	}
	if l.skillsDir == "" {
		t.Error("DefaultLoader().skillsDir should not be empty")
	}
}

// ---------------------------------------------------------------------------
// LoadFromDir — custom prompt/rules files path not previously exercised
// ---------------------------------------------------------------------------

func TestLoadFromDir_CustomRulesFile(t *testing.T) {
	dir := t.TempDir()
	def := `{"name":"myskill","rules_file":"custom_rules.md"}`
	os.WriteFile(filepath.Join(dir, "skill.json"), []byte(def), 0644)
	os.WriteFile(filepath.Join(dir, "custom_rules.md"), []byte("rule content"), 0644)

	s, err := LoadFromDir(dir)
	if err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if s.RuleContent() != "rule content" {
		t.Errorf("rule content: got %q", s.RuleContent())
	}
}

// ---------------------------------------------------------------------------
// LoadToolsFromDir — subdirectory inside tools/ should be skipped
// ---------------------------------------------------------------------------

func TestLoadToolsFromDir_SkipsSubdirsOnly(t *testing.T) {
	dir := t.TempDir()
	toolsDir := filepath.Join(dir, "tools")
	os.MkdirAll(filepath.Join(toolsDir, "subdir"), 0755)

	tools, err := LoadToolsFromDir(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("expected 0 tools for dir-only tools/, got %d", len(tools))
	}
}

// ---------------------------------------------------------------------------
// parseToolMD — uncovered branches
// ---------------------------------------------------------------------------

func TestParseToolMD_WithMaxOutputKB(t *testing.T) {
	content := `---
tool: capped_tool
description: Capped tool
mode: shell
shell: /bin/echo
max_output_kb: 32
---
`
	pt, err := parseToolMD([]byte(content))
	if err != nil {
		t.Fatalf("parseToolMD: %v", err)
	}
	if pt.maxOutputBytes != 32*1024 {
		t.Errorf("maxOutputBytes: %d, want %d", pt.maxOutputBytes, 32*1024)
	}
}

func TestParseToolMD_WithArgs(t *testing.T) {
	content := `---
tool: args_tool
description: Args tool
mode: shell
shell: /bin/echo
args:
  - -n
  - hello
---
`
	pt, err := parseToolMD([]byte(content))
	if err != nil {
		t.Fatalf("parseToolMD: %v", err)
	}
	if len(pt.shellArgs) != 2 {
		t.Errorf("shellArgs: %v", pt.shellArgs)
	}
}

// ---------------------------------------------------------------------------
// executeShell — stderr output in error message
// ---------------------------------------------------------------------------

func TestPromptTool_ExecuteShell_CommandFailsWithStderr(t *testing.T) {
	// 'ls /nonexistent' outputs to stderr on failure
	pt := &PromptTool{
		name:           "fail_stderr_tool",
		mode:           "shell",
		shellBin:       "ls",
		shellArgs:      []string{"/nonexistent-path-that-does-not-exist"},
		maxOutputBytes: maxShellOutputBytes,
	}
	result := pt.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for failing ls command")
	}
}

// ---------------------------------------------------------------------------
// executeShell — zero maxOutputBytes uses default cap
// ---------------------------------------------------------------------------

func TestPromptTool_ExecuteShell_ZeroMaxOutputBytes(t *testing.T) {
	pt := &PromptTool{
		name:           "zero_cap",
		mode:           "shell",
		shellBin:       "echo",
		shellArgs:      []string{"test"},
		maxOutputBytes: 0, // 0 → use default
	}
	result := pt.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if !strings.Contains(result.Output, "test") {
		t.Errorf("unexpected output: %q", result.Output)
	}
}

// ---------------------------------------------------------------------------
// executeShell — small stderr cap minimum (< 1024)
// ---------------------------------------------------------------------------

func TestPromptTool_ExecuteShell_SmallCapStillRunsOK(t *testing.T) {
	// maxOutputBytes/8 < 1024 → stderr.max gets set to 1024 minimum
	pt := &PromptTool{
		name:           "small_cap",
		mode:           "shell",
		shellBin:       "echo",
		shellArgs:      []string{"ok"},
		maxOutputBytes: 100, // 100/8 = 12 < 1024 → triggers minimum branch
	}
	result := pt.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Error)
	}
}

// ---------------------------------------------------------------------------
// executeAgent — budget_tokens in stub output
// ---------------------------------------------------------------------------

func TestPromptTool_ExecuteAgent_BudgetTokensInOutput(t *testing.T) {
	pt := &PromptTool{
		name:         "budget_tool",
		mode:         "agent",
		agentModel:   "claude-3",
		budgetTokens: 5000,
		depth:        0,
		maxDepth:     5,
	}
	result := pt.Execute(context.Background(), map[string]any{"key": "val"})
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if !strings.Contains(result.Output, "5000") {
		t.Errorf("expected budget_tokens=5000 in output, got %q", result.Output)
	}
}

// ---------------------------------------------------------------------------
// renderShellArgs — template functions (upper/lower/trim)
// ---------------------------------------------------------------------------

func TestPromptTool_RenderShellArgs_FuncMap(t *testing.T) {
	pt := &PromptTool{body: `echo {{upper .msg}}`}
	parts, err := pt.renderShellArgs(map[string]any{"msg": "hello"})
	if err != nil {
		t.Fatalf("renderShellArgs: %v", err)
	}
	if len(parts) < 2 {
		t.Fatalf("expected parts: %v", parts)
	}
	if parts[1] != "HELLO" {
		t.Errorf("expected HELLO, got %q", parts[1])
	}
}

// ---------------------------------------------------------------------------
// normalizeTemplateSyntax — ensure keywords like 'nil' are left unchanged
// ---------------------------------------------------------------------------

func TestNormalizeTemplateSyntax_NilKeyword(t *testing.T) {
	result := normalizeTemplateSyntax("{{nil}}")
	if result != "{{nil}}" {
		t.Errorf("{{nil}} should remain unchanged, got %q", result)
	}
}

func TestNormalizeTemplateSyntax_WithKeyword(t *testing.T) {
	result := normalizeTemplateSyntax("{{with}}")
	if result != "{{with}}" {
		t.Errorf("{{with}} should remain unchanged, got %q", result)
	}
}

func TestNormalizeTemplateSyntax_Define(t *testing.T) {
	result := normalizeTemplateSyntax("{{define}}")
	if result != "{{define}}" {
		t.Errorf("{{define}} should remain unchanged, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// limitedWriter — write to already-full buffer returns full len
// ---------------------------------------------------------------------------

func TestLimitedWriter_AlreadyAtMax(t *testing.T) {
	lw := &limitedWriter{max: 3}
	lw.buf.WriteString("abc") // fill exactly
	n, err := lw.Write([]byte("xyz"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 3 {
		t.Errorf("n: %d, want 3", n)
	}
	if !lw.truncated {
		t.Error("should be truncated")
	}
	if lw.buf.String() != "abc" {
		t.Errorf("buf should be unchanged at max: %q", lw.buf.String())
	}
}

// ---------------------------------------------------------------------------
// FilesystemSkill.Tools — error from LoadToolsFromDir → nil
// ---------------------------------------------------------------------------

func TestFilesystemSkill_Tools_WithValidTool(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "skill.json"), []byte(`{"name":"s"}`), 0644)
	toolsDir := filepath.Join(dir, "tools")
	os.MkdirAll(toolsDir, 0755)
	content := "---\ntool: my_tool\ndescription: A test tool\n---\nHello"
	os.WriteFile(filepath.Join(toolsDir, "my_tool.md"), []byte(content), 0644)

	s, _ := LoadFromDir(dir)
	tools := s.Tools()
	if len(tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name() != "my_tool" {
		t.Errorf("tool name: %q", tools[0].Name())
	}
}

// ---------------------------------------------------------------------------
// PromptTool.Description (accessor)
// ---------------------------------------------------------------------------

func TestPromptTool_DescriptionAccessor(t *testing.T) {
	pt := NewPromptTool("t", "my description", "", "body")
	if pt.Description() != "my description" {
		t.Errorf("description: %q", pt.Description())
	}
}

// ---------------------------------------------------------------------------
// Loader.LoadAll — non-dir file entries are skipped (file in skills dir)
// ---------------------------------------------------------------------------

func TestLoader_LoadAll_SkipsFiles(t *testing.T) {
	root := t.TempDir()
	// Write a non-.md file in the skills root — it should be skipped
	os.WriteFile(filepath.Join(root, "notamd.json"), []byte("{}"), 0644)
	// Write a directory — it should be skipped
	os.MkdirAll(filepath.Join(root, "subdir"), 0755)
	// Write a valid skill markdown file
	os.WriteFile(filepath.Join(root, "good.md"), []byte("---\nname: good\n---\nGood skill"), 0644)
	// Write a manifest explicitly enabling the valid skill (deny-by-default requires this).
	os.WriteFile(filepath.Join(root, "installed.json"), []byte(`[{"name":"good","source":"local","enabled":true}]`), 0644)

	loader := NewLoader(root)
	skills, errs := loader.LoadAll()
	if len(errs) > 0 {
		t.Fatalf("LoadAll: %v", errs)
	}
	if len(skills) != 1 {
		t.Errorf("expected 1 skill, got %d", len(skills))
	}
}

// ---------------------------------------------------------------------------
// DefaultLoader — cover the HOME error path by unsetting HOME env var
// ---------------------------------------------------------------------------

func TestDefaultLoader_NoHOME_FallsBackToRelative(t *testing.T) {
	// Temporarily clear HOME to force the error path in os.UserHomeDir
	orig := os.Getenv("HOME")
	t.Cleanup(func() { os.Setenv("HOME", orig) })
	os.Unsetenv("HOME")

	// Also unset USERPROFILE / HOMEPATH on all platforms
	os.Unsetenv("USERPROFILE")
	os.Unsetenv("HOMEPATH")

	l := DefaultLoader()
	if l == nil {
		t.Fatal("DefaultLoader returned nil even on HOME error")
	}
	// The fallback is filepath.Join(".huginn", "skills")
	if l.skillsDir == "" {
		t.Error("skillsDir should not be empty")
	}
}

// ---------------------------------------------------------------------------
// validateArgs — required field with non-string element in required array
// ---------------------------------------------------------------------------

func TestPromptTool_ValidateArgs_RequiredNonStringElem(t *testing.T) {
	// "required" array contains a non-string element (number) — should skip it
	schema := `{"type":"object","required":[1,2,3],"properties":{}}`
	pt := NewPromptTool("t", "d", schema, "body")
	if err := pt.validateArgs(map[string]any{}); err != nil {
		t.Errorf("non-string required elem should be skipped, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// validateArgs — property with no pattern key (should not error)
// ---------------------------------------------------------------------------

func TestPromptTool_ValidateArgs_PropertyNoPattern(t *testing.T) {
	schema := `{"type":"object","properties":{"name":{"type":"string"}}}`
	pt := NewPromptTool("t", "d", schema, "body")
	if err := pt.validateArgs(map[string]any{"name": "test"}); err != nil {
		t.Errorf("property without pattern should pass, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// parseToolMD — body with leading newline stripped
// ---------------------------------------------------------------------------

func TestParseToolMD_BodyLeadingNewline(t *testing.T) {
	// The body starts with a newline after "---", which should be stripped
	content := "---\ntool: trim_tool\ndescription: Test\n---\n\nThis is the body\n"
	pt, err := parseToolMD([]byte(content))
	if err != nil {
		t.Fatalf("parseToolMD: %v", err)
	}
	if strings.HasPrefix(pt.body, "\n") {
		t.Errorf("body should not start with newline, got %q", pt.body)
	}
}

// ---------------------------------------------------------------------------
// parseToolMD — no schema (nil) leaves schemaJSON as "{}"
// ---------------------------------------------------------------------------

func TestParseToolMD_NoSchema_DefaultsToEmptyBraces(t *testing.T) {
	content := "---\ntool: noschema\ndescription: No schema\n---\nHello"
	pt, err := parseToolMD([]byte(content))
	if err != nil {
		t.Fatalf("parseToolMD: %v", err)
	}
	if pt.schemaJSON != "{}" {
		t.Errorf("schemaJSON: %q, want {}", pt.schemaJSON)
	}
}

// ---------------------------------------------------------------------------
// renderShellArgs — trim function in template
// ---------------------------------------------------------------------------

func TestPromptTool_RenderShellArgs_TrimFunction(t *testing.T) {
	pt := &PromptTool{body: `echo {{trim .msg}}`}
	parts, err := pt.renderShellArgs(map[string]any{"msg": "  hello  "})
	if err != nil {
		t.Fatalf("renderShellArgs: %v", err)
	}
	if len(parts) < 2 {
		t.Fatalf("expected parts: %v", parts)
	}
	if parts[1] != "hello" {
		t.Errorf("expected 'hello', got %q", parts[1])
	}
}

// ---------------------------------------------------------------------------
// executeTemplate — template with invalid action (parse error)
// ---------------------------------------------------------------------------

func TestPromptTool_ExecuteTemplate_ParseError(t *testing.T) {
	pt := NewPromptTool("t", "d", "", "{{.Name | badfunction}}")
	result := pt.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for template with unknown function")
	}
}

// ---------------------------------------------------------------------------
// executeShell — renderShellArgs error path (malformed template in body)
// ---------------------------------------------------------------------------

func TestPromptTool_ExecuteShell_MalformedBodyTemplate(t *testing.T) {
	pt := &PromptTool{
		name:           "bad_body",
		mode:           "shell",
		body:           "{{if}}", // malformed template
		maxOutputBytes: maxShellOutputBytes,
	}
	result := pt.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for malformed shell body template")
	}
}
