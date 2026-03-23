package skills_test

import (
	"context"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/skills"
	"github.com/scrypster/huginn/internal/tools"
)

func TestPromptTool_Name(t *testing.T) {
	pt := skills.NewPromptTool("run_tests", "Run the test suite", `{}`, "Run go test {{path}} -v")
	if pt.Name() != "run_tests" {
		t.Errorf("expected 'run_tests', got %q", pt.Name())
	}
}

func TestPromptTool_Description(t *testing.T) {
	pt := skills.NewPromptTool("run_tests", "Run the test suite", `{}`, "body")
	if pt.Description() != "Run the test suite" {
		t.Errorf("unexpected description: %q", pt.Description())
	}
}

func TestPromptTool_Execute_SubstitutesParams(t *testing.T) {
	pt := skills.NewPromptTool("run_tests", "desc", `{}`, "Run `go test {{path}} -v` and return output.")
	result := pt.Execute(context.Background(), map[string]any{"path": "./internal/..."})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !strings.Contains(result.Output, "./internal/...") {
		t.Errorf("expected substituted path in output, got: %q", result.Output)
	}
	if strings.Contains(result.Output, "{{path}}") {
		t.Error("placeholder {{path}} was not substituted")
	}
}

func TestPromptTool_Execute_MultipleParams(t *testing.T) {
	pt := skills.NewPromptTool("greet", "desc", `{}`, "Hello {{name}}, you are {{age}} years old.")
	result := pt.Execute(context.Background(), map[string]any{"name": "Alice", "age": "30"})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	want := "Hello Alice, you are 30 years old."
	if result.Output != want {
		t.Errorf("expected %q, got %q", want, result.Output)
	}
}

func TestPromptTool_Execute_UnknownParamLeftAsIs(t *testing.T) {
	// Phase 2: text/template with missingkey=zero renders unknown keys as empty string.
	// The legacy "leave placeholder as-is" behavior is replaced by empty-string emission.
	pt := skills.NewPromptTool("tool", "desc", `{}`, "Value: {{missing}}")
	result := pt.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Errorf("expected no error for unknown placeholder, got: %s", result.Error)
	}
	// Unknown arg "missing" becomes empty string; "Value: " is the expected output.
	if result.Output != "Value: " {
		t.Errorf("expected 'Value: ' (missing key = empty), got: %q", result.Output)
	}
}

func TestPromptTool_Permission(t *testing.T) {
	pt := skills.NewPromptTool("tool", "desc", `{}`, "body")
	if pt.Permission() != tools.PermRead {
		t.Errorf("expected PermRead, got %v", pt.Permission())
	}
}

func TestPromptTool_Schema_ContainsName(t *testing.T) {
	pt := skills.NewPromptTool("run_tests", "Run tests", `{"type":"object","properties":{"path":{"type":"string"}}}`, "body")
	schema := pt.Schema()
	if schema.Function.Name != "run_tests" {
		t.Errorf("expected schema function name 'run_tests', got %q", schema.Function.Name)
	}
	if schema.Function.Description != "Run tests" {
		t.Errorf("expected description 'Run tests', got %q", schema.Function.Description)
	}
}

func TestPromptTool_Schema_ParsesSchemaJSON(t *testing.T) {
	schemaJSON := `{
		"type":"object",
		"properties":{
			"path":{"type":"string","description":"File path to test"},
			"verbose":{"type":"boolean","description":"Enable verbose output"}
		},
		"required":["path"]
	}`
	pt := skills.NewPromptTool("run_tests", "Run tests", schemaJSON, "body")
	schema := pt.Schema()

	if schema.Type != "function" {
		t.Errorf("expected schema type 'function', got %q", schema.Type)
	}
	if schema.Function.Parameters.Type != "object" {
		t.Errorf("expected parameters type 'object', got %q", schema.Function.Parameters.Type)
	}
	if len(schema.Function.Parameters.Properties) != 2 {
		t.Errorf("expected 2 properties, got %d", len(schema.Function.Parameters.Properties))
	}
	if pathProp, ok := schema.Function.Parameters.Properties["path"]; !ok {
		t.Error("expected 'path' property not found")
	} else if pathProp.Type != "string" {
		t.Errorf("expected path type 'string', got %q", pathProp.Type)
	}
	if len(schema.Function.Parameters.Required) != 1 {
		t.Errorf("expected 1 required field, got %d", len(schema.Function.Parameters.Required))
	}
	if schema.Function.Parameters.Required[0] != "path" {
		t.Errorf("expected 'path' in required, got %q", schema.Function.Parameters.Required[0])
	}
}

func TestPromptTool_Schema_EmptySchemaJSON(t *testing.T) {
	pt := skills.NewPromptTool("tool", "desc", `{}`, "body")
	schema := pt.Schema()
	if schema.Function.Parameters.Type != "object" {
		t.Errorf("expected parameters type 'object', got %q", schema.Function.Parameters.Type)
	}
}

func TestPromptTool_Schema_DefaultsTypeToObject(t *testing.T) {
	pt := skills.NewPromptTool("tool", "desc", `{"properties":{"arg":{"type":"string"}}}`, "body")
	schema := pt.Schema()
	if schema.Function.Parameters.Type != "object" {
		t.Errorf("expected parameters type 'object', got %q", schema.Function.Parameters.Type)
	}
}
