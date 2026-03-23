package skills

import (
	"context"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

// collisionTool implements tools.Tool for collision testing.
type collisionTool struct {
	name string
}

func (t *collisionTool) Name() string                                          { return t.name }
func (t *collisionTool) Description() string                                   { return "stub" }
func (t *collisionTool) Permission() tools.PermissionLevel                     { return tools.PermRead }
func (t *collisionTool) Schema() backend.Tool                                  { return backend.Tool{} }
func (t *collisionTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	return tools.ToolResult{}
}

// skillWithTools is a skill that returns the given tools.
type skillWithTools struct {
	name  string
	tools []tools.Tool
}

func (s *skillWithTools) Name() string                 { return s.name }
func (s *skillWithTools) Description() string          { return "" }
func (s *skillWithTools) SystemPromptFragment() string { return "" }
func (s *skillWithTools) RuleContent() string          { return "" }
func (s *skillWithTools) Tools() []tools.Tool          { return s.tools }

func TestSkillRegistry_Register_ToolNameCollision(t *testing.T) {
	reg := NewSkillRegistry()

	skill1 := &skillWithTools{
		name:  "analytics",
		tools: []tools.Tool{&collisionTool{name: "query_data"}, &collisionTool{name: "plot_chart"}},
	}
	if err := reg.Register(skill1); err != nil {
		t.Fatalf("first Register failed unexpectedly: %v", err)
	}

	// Second skill has a tool with the same name as skill1.
	skill2 := &skillWithTools{
		name:  "reporting",
		tools: []tools.Tool{&collisionTool{name: "query_data"}},
	}
	err := reg.Register(skill2)
	if err == nil {
		t.Fatal("expected error from Register with colliding tool name, got nil")
	}
	if !strings.Contains(err.Error(), "query_data") {
		t.Errorf("error should mention colliding tool name, got: %v", err)
	}
	if !strings.Contains(err.Error(), "analytics") {
		t.Errorf("error should mention existing skill name, got: %v", err)
	}

	// Registry should still have only the first skill.
	all := reg.All()
	if len(all) != 1 {
		t.Errorf("expected 1 skill registered, got %d", len(all))
	}
}

func TestSkillRegistry_Register_NoCollision_DifferentToolNames(t *testing.T) {
	reg := NewSkillRegistry()

	skill1 := &skillWithTools{
		name:  "analytics",
		tools: []tools.Tool{&collisionTool{name: "query_data"}},
	}
	skill2 := &skillWithTools{
		name:  "reporting",
		tools: []tools.Tool{&collisionTool{name: "generate_report"}},
	}

	if err := reg.Register(skill1); err != nil {
		t.Fatalf("Register skill1: %v", err)
	}
	if err := reg.Register(skill2); err != nil {
		t.Fatalf("Register skill2: %v", err)
	}
	if len(reg.All()) != 2 {
		t.Errorf("expected 2 skills, got %d", len(reg.All()))
	}
}

func TestSkillRegistry_Register_NilTools_NoCollision(t *testing.T) {
	reg := NewSkillRegistry()

	// Skills with nil tools should never collide.
	s1 := &stubSkill{name: "a", prompt: "p"}
	s2 := &stubSkill{name: "b", prompt: "q"}
	if err := reg.Register(s1); err != nil {
		t.Fatalf("Register s1: %v", err)
	}
	if err := reg.Register(s2); err != nil {
		t.Fatalf("Register s2: %v", err)
	}
}
