package tools

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

// badSchemaTool is a Tool whose schema has an empty function name — invalid.
type badSchemaTool struct{ name string }

func (b *badSchemaTool) Name() string             { return b.name }
func (b *badSchemaTool) Description() string      { return "" }
func (b *badSchemaTool) Permission() PermissionLevel { return PermRead }
func (b *badSchemaTool) Schema() backend.Tool {
	// Empty Function.Name — intentionally invalid.
	return backend.Tool{Type: "function"}
}
func (b *badSchemaTool) Execute(_ context.Context, _ map[string]any) ToolResult {
	return ToolResult{}
}

// illegalNameTool has a function name with forbidden characters.
type illegalNameTool struct{}

func (i *illegalNameTool) Name() string             { return "illegal name tool" }
func (i *illegalNameTool) Description() string      { return "" }
func (i *illegalNameTool) Permission() PermissionLevel { return PermRead }
func (i *illegalNameTool) Schema() backend.Tool {
	return backend.Tool{
		Type:     "function",
		Function: backend.ToolFunction{Name: "has spaces!"},
	}
}
func (i *illegalNameTool) Execute(_ context.Context, _ map[string]any) ToolResult {
	return ToolResult{}
}

// TestValidateToolSchema_Valid verifies a well-formed schema passes.
func TestValidateToolSchema_Valid(t *testing.T) {
	t.Parallel()
	tool := &mockTool{name: "valid_tool"}
	if err := validateToolSchema(tool); err != nil {
		t.Errorf("expected valid schema to pass, got: %v", err)
	}
}

// TestValidateToolSchema_EmptyName rejects a tool with a missing function name.
func TestValidateToolSchema_EmptyName(t *testing.T) {
	t.Parallel()
	tool := &badSchemaTool{name: "bad"}
	if err := validateToolSchema(tool); err == nil {
		t.Error("expected error for empty function name, got nil")
	}
}

// TestValidateToolSchema_IllegalCharacters rejects names with spaces and punctuation.
func TestValidateToolSchema_IllegalCharacters(t *testing.T) {
	t.Parallel()
	tool := &illegalNameTool{}
	if err := validateToolSchema(tool); err == nil {
		t.Error("expected error for illegal characters in tool name, got nil")
	}
}

// TestValidateToolSchema_TooLong rejects names exceeding 64 characters.
func TestValidateToolSchema_TooLong(t *testing.T) {
	t.Parallel()
	longName := "a_tool_with_a_very_long_name_that_exceeds_the_limit_12345678901234"
	if len(longName) <= 64 {
		longName = longName + "x" // ensure > 64
	}
	type longNameTool struct{ mockTool }
	// We can't easily embed and override, so build via validateToolSchema directly.
	_ = longName

	// Use validToolName regexp directly to assert the boundary.
	if validToolName.MatchString(longName) {
		t.Errorf("expected regexp to reject name of length %d (>64)", len(longName))
	}
}

// TestRegistry_Register_InvalidSchema_IsSkipped verifies that Register silently
// skips a tool with an invalid schema and does NOT register it.
func TestRegistry_Register_InvalidSchema_IsSkipped(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	r.Register(&badSchemaTool{name: "bad"})

	// The tool's Name() is "bad" but its schema function name is empty → skipped.
	_, ok := r.Get("bad")
	if ok {
		t.Error("expected tool with invalid schema to be skipped from registry")
	}
}

// TestRegistry_RegisterStrict_InvalidSchema_ReturnsError verifies that
// RegisterStrict surfaces the schema error rather than silently swallowing it.
func TestRegistry_RegisterStrict_InvalidSchema_ReturnsError(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	err := r.RegisterStrict(&badSchemaTool{name: "bad"})
	if err == nil {
		t.Error("expected RegisterStrict to return error for invalid schema, got nil")
	}
}

// TestRegistry_RegisterStrict_ValidSchema_Succeeds verifies normal happy path.
func TestRegistry_RegisterStrict_ValidSchema_Succeeds(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	err := r.RegisterStrict(&mockTool{name: "good_tool"})
	if err != nil {
		t.Errorf("expected RegisterStrict to succeed, got: %v", err)
	}
	_, ok := r.Get("good_tool")
	if !ok {
		t.Error("expected tool to be findable after RegisterStrict")
	}
}

// TestRegistry_RegisterStrict_Duplicate_ReturnsError verifies collision detection.
func TestRegistry_RegisterStrict_Duplicate_ReturnsError(t *testing.T) {
	t.Parallel()
	r := NewRegistry()
	if err := r.RegisterStrict(&mockTool{name: "dup_tool"}); err != nil {
		t.Fatalf("first registration should succeed: %v", err)
	}
	if err := r.RegisterStrict(&mockTool{name: "dup_tool"}); err == nil {
		t.Error("expected error for duplicate registration, got nil")
	}
}

// TestValidToolName_Boundaries verifies edge cases of the name regexp.
func TestValidToolName_Boundaries(t *testing.T) {
	t.Parallel()
	valid := []string{
		"a", "A", "z", "Z", "0", "9",
		"bash", "read_file", "git-diff", "tool123",
		"a_tool_with_exactly_64_chars_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx64", // exactly 64
	}
	// Trim to exactly 64 for that last entry
	valid[len(valid)-1] = "a"
	for i := 0; i < 63; i++ {
		valid[len(valid)-1] += "a"
	}

	for _, n := range valid {
		if !validToolName.MatchString(n) {
			t.Errorf("expected %q (len=%d) to be a valid tool name", n, len(n))
		}
	}

	invalid := []string{
		"",
		"has space",
		"tool.name",
		"tool@name",
		"tool/name",
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", // 65 chars
	}
	for _, n := range invalid {
		if validToolName.MatchString(n) {
			t.Errorf("expected %q (len=%d) to be an invalid tool name", n, len(n))
		}
	}
}
