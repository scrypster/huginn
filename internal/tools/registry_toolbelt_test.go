package tools

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

// stubTool is a minimal Tool for registry tests.
type stubTool struct{ name string }

func (s *stubTool) Name() string             { return s.name }
func (s *stubTool) Description() string      { return "" }
func (s *stubTool) Permission() PermissionLevel { return PermRead }
func (s *stubTool) Schema() backend.Tool     { return backend.Tool{Function: backend.ToolFunction{Name: s.name}} }
func (s *stubTool) Execute(_ context.Context, _ map[string]any) ToolResult { return ToolResult{} }

func TestTagTools_AndProviderFor(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubTool{name: "github_list_repos"})
	r.Register(&stubTool{name: "github_get_repo"})
	r.TagTools([]string{"github_list_repos", "github_get_repo"}, "github")

	if p := r.ProviderFor("github_list_repos"); p != "github" {
		t.Fatalf("expected github, got %q", p)
	}
	if p := r.ProviderFor("github_get_repo"); p != "github" {
		t.Fatalf("expected github, got %q", p)
	}
	if p := r.ProviderFor("unknown_tool"); p != "" {
		t.Fatalf("expected empty, got %q", p)
	}
}

func TestAllSchemasForProviders_Filters(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubTool{name: "github_list_repos"})
	r.Register(&stubTool{name: "slack_send_message"})
	r.TagTools([]string{"github_list_repos"}, "github")
	r.TagTools([]string{"slack_send_message"}, "slack")

	schemas := r.AllSchemasForProviders([]string{"github"})
	if len(schemas) != 1 {
		t.Fatalf("expected 1 schema, got %d", len(schemas))
	}
	if schemas[0].Function.Name != "github_list_repos" {
		t.Fatalf("unexpected schema: %s", schemas[0].Function.Name)
	}
}

func TestAllSchemasForProviders_EmptyReturnsNil(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubTool{name: "github_list_repos"})
	r.Register(&stubTool{name: "slack_send_message"})
	r.TagTools([]string{"github_list_repos"}, "github")
	r.TagTools([]string{"slack_send_message"}, "slack")

	// Empty/nil providers → default-deny → nothing returned.
	schemas := r.AllSchemasForProviders(nil)
	if len(schemas) != 0 {
		t.Fatalf("expected 0 schemas (default-deny), got %d", len(schemas))
	}
}

func TestAllSchemasForProviders_WildcardReturnsAllExternal(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubTool{name: "github_list_repos"})
	r.Register(&stubTool{name: "slack_send_message"})
	r.Register(&stubTool{name: "bash"})
	r.TagTools([]string{"github_list_repos"}, "github")
	r.TagTools([]string{"slack_send_message"}, "slack")
	r.TagTools([]string{"bash"}, "builtin")

	// ["*"] returns all non-builtin schemas.
	schemas := r.AllSchemasForProviders([]string{"*"})
	if len(schemas) != 2 {
		t.Fatalf("expected 2 schemas (all external), got %d", len(schemas))
	}
	for _, s := range schemas {
		if s.Function.Name == "bash" {
			t.Fatalf("builtin tool 'bash' should not be returned by wildcard")
		}
	}
}

func TestAllSchemasForProviders_NeverReturnsBuiltin(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubTool{name: "bash"})
	r.Register(&stubTool{name: "github_list_repos"})
	r.TagTools([]string{"bash"}, "builtin")
	r.TagTools([]string{"github_list_repos"}, "github")

	// Explicitly requesting "builtin" should return nothing (builtin is excluded).
	schemas := r.AllSchemasForProviders([]string{"builtin"})
	if len(schemas) != 0 {
		t.Fatalf("expected 0 schemas (builtin excluded), got %d: %v", len(schemas), schemas)
	}
}

func TestAllSchemasForProviders_MultipleProviders(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubTool{name: "github_list_repos"})
	r.Register(&stubTool{name: "slack_send_message"})
	r.Register(&stubTool{name: "jira_list_issues"})
	r.TagTools([]string{"github_list_repos"}, "github")
	r.TagTools([]string{"slack_send_message"}, "slack")
	r.TagTools([]string{"jira_list_issues"}, "jira")

	schemas := r.AllSchemasForProviders([]string{"github", "slack"})
	if len(schemas) != 2 {
		t.Fatalf("expected 2 schemas, got %d", len(schemas))
	}
}
