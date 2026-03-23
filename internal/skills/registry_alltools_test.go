package skills

import (
	"testing"

	"github.com/scrypster/huginn/internal/tools"
)

// TestSkillRegistry_AllTools verifies AllTools collects tools from all skills.
func TestSkillRegistry_AllTools(t *testing.T) {
	// Create mock skills with tools
	skill1 := &mockSkill{
		name:    "skill1",

		tools: []tools.Tool{
			NewPromptTool("tool1", "Tool 1", "{}", "body1"),
			NewPromptTool("tool2", "Tool 2", "{}", "body2"),
		},
	}

	skill2 := &mockSkill{
		name:    "skill2",

		tools: []tools.Tool{
			NewPromptTool("tool3", "Tool 3", "{}", "body3"),
		},
	}

	reg := NewSkillRegistry()
	reg.Register(skill1)
	reg.Register(skill2)

	allTools := reg.AllTools()
	if len(allTools) != 3 {
		t.Errorf("expected 3 tools, got %d", len(allTools))
	}

	// Verify tool names
	names := make(map[string]bool)
	for _, tool := range allTools {
		names[tool.Name()] = true
	}

	if !names["tool1"] || !names["tool2"] || !names["tool3"] {
		t.Errorf("expected tools tool1, tool2, tool3; got %v", names)
	}
}

// TestSkillRegistry_AllTools_Empty verifies AllTools returns empty for skills without tools.
func TestSkillRegistry_AllTools_Empty(t *testing.T) {
	skill := &mockSkill{
		name:    "skill_no_tools",

		tools:   nil, // no tools
	}

	reg := NewSkillRegistry()
	reg.Register(skill)

	allTools := reg.AllTools()
	if len(allTools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(allTools))
	}
}

// TestSkillRegistry_AllTools_Multiple verifies AllTools handles multiple skills with varying tool counts.
func TestSkillRegistry_AllTools_Multiple(t *testing.T) {
	skill1 := &mockSkill{
		name:    "s1",

		tools: []tools.Tool{
			NewPromptTool("a", "A", "{}", "body"),
		},
	}
	skill2 := &mockSkill{
		name:    "s2",

		tools: []tools.Tool{
			NewPromptTool("b", "B", "{}", "body"),
			NewPromptTool("c", "C", "{}", "body"),
			NewPromptTool("d", "D", "{}", "body"),
		},
	}
	skill3 := &mockSkill{
		name:    "s3",

		tools:   nil, // no tools
	}

	reg := NewSkillRegistry()
	reg.Register(skill1)
	reg.Register(skill2)
	reg.Register(skill3)

	allTools := reg.AllTools()
	if len(allTools) != 4 {
		t.Errorf("expected 4 tools, got %d", len(allTools))
	}
}

// mockSkill is a test implementation of Skill.
type mockSkill struct {
	name           string
	promptFragment string
	ruleContent    string
	tools          []tools.Tool
}

func (m *mockSkill) Name() string                 { return m.name }
func (m *mockSkill) Description() string          { return "" }
func (m *mockSkill) SystemPromptFragment() string { return m.promptFragment }
func (m *mockSkill) RuleContent() string          { return m.ruleContent }
func (m *mockSkill) Tools() []tools.Tool          { return m.tools }
