package tools_test

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// minimalTool is a test-only Tool implementation.
type minimalTool struct{ name string }

func (m *minimalTool) Name() string        { return m.name }
func (m *minimalTool) Description() string { return "" }
func (m *minimalTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (m *minimalTool) Schema() backend.Tool {
	return backend.Tool{Type: "function", Function: backend.ToolFunction{Name: m.name}}
}
func (m *minimalTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	return tools.ToolResult{}
}

func TestSchemasByNames_ReturnsNamedTools(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&minimalTool{name: "read_file"})
	reg.Register(&minimalTool{name: "bash"})
	reg.Register(&minimalTool{name: "git_status"})

	schemas := reg.SchemasByNames([]string{"read_file", "git_status"})
	if len(schemas) != 2 {
		t.Fatalf("expected 2 schemas, got %d", len(schemas))
	}
	names := map[string]bool{}
	for _, s := range schemas {
		names[s.Function.Name] = true
	}
	if !names["read_file"] || !names["git_status"] {
		t.Errorf("expected read_file and git_status in result, got %v", names)
	}
}

func TestSchemasByNames_WildcardReturnsAll(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&minimalTool{name: "read_file"})
	reg.Register(&minimalTool{name: "bash"})
	reg.TagTools([]string{"read_file", "bash"}, "builtin")

	schemas := reg.AllBuiltinSchemas()
	if len(schemas) != 2 {
		t.Fatalf("expected 2 builtin schemas, got %d", len(schemas))
	}
}

func TestSchemasByNames_EmptySliceReturnsNone(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&minimalTool{name: "read_file"})
	schemas := reg.SchemasByNames([]string{})
	if len(schemas) != 0 {
		t.Fatalf("expected 0 schemas, got %d", len(schemas))
	}
}

func TestSchemasByNames_UnknownNameSkipped(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(&minimalTool{name: "read_file"})
	schemas := reg.SchemasByNames([]string{"read_file", "nonexistent"})
	if len(schemas) != 1 {
		t.Fatalf("expected 1 schema, got %d", len(schemas))
	}
}
