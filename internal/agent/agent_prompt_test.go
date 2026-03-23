package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/tools"
)

func TestBuildAgentSystemPrompt_NoMemories(t *testing.T) {
	result := buildAgentSystemPrompt("", "", nil, "", "", "", "", "", "", "")
	if strings.Contains(result, "## Your Expertise") {
		t.Error("expected no ## Your Expertise section when memories is nil")
	}
	if strings.Contains(result, "## Project & User Context") {
		t.Error("expected no ## Project & User Context section when memories is nil")
	}
}

func TestBuildAgentSystemPrompt_GlobalInstructions(t *testing.T) {
	result := buildAgentSystemPrompt("", "", nil, "global rules here", "", "", "", "", "", "")
	if !strings.HasPrefix(result, "global rules here") {
		t.Errorf("expected global instructions first, got: %q", result)
	}
}

func TestBuildAgentSystemPrompt_ProjectInstructions(t *testing.T) {
	result := buildAgentSystemPrompt("", "", nil, "", "project rules here", "", "", "", "", "")
	if !strings.Contains(result, "project rules here") {
		t.Errorf("expected project instructions in output, got: %q", result)
	}
}

func TestBuildAgentSystemPrompt_ContextText(t *testing.T) {
	result := buildAgentSystemPrompt("git context here", "", nil, "", "", "", "", "", "", "")
	if !strings.Contains(result, "git context here") {
		t.Errorf("expected context text in output, got: %q", result)
	}
}

// TestBuildAgentSystemPrompt_MemoryToolInstructionInjected verifies that the
// ## Memory section is injected when muninn_recall or muninn_where_left_off is
// registered in the tool registry, and is absent when neither is registered.
func TestBuildAgentSystemPrompt_MemoryToolInstructionInjected(t *testing.T) {
	t.Run("injected when muninn_recall registered", func(t *testing.T) {
		reg := tools.NewRegistry()
		// Register a minimal tool stub named "muninn_recall".
		reg.Register(&stubTool{name: "muninn_recall"})

		result := buildAgentSystemPrompt("", "", reg, "", "", "", "", "conversational", "test-vault", "test description")
		if !strings.Contains(result, "## Memory") {
			t.Errorf("expected ## Memory in prompt when muninn_recall is registered, got:\n%q", result)
		}
	})

	t.Run("injected when muninn_where_left_off registered", func(t *testing.T) {
		reg := tools.NewRegistry()
		// Register a minimal tool stub named "muninn_where_left_off".
		reg.Register(&stubTool{name: "muninn_where_left_off"})

		result := buildAgentSystemPrompt("", "", reg, "", "", "", "", "conversational", "test-vault", "test description")
		if !strings.Contains(result, "## Memory") {
			t.Errorf("expected ## Memory in prompt when muninn_where_left_off is registered, got:\n%q", result)
		}
	})

	t.Run("not injected when neither muninn tool present", func(t *testing.T) {
		reg := tools.NewRegistry()
		// Registry has only other tools — no muninn tools.
		reg.Register(&stubTool{name: "read_file"})
		result := buildAgentSystemPrompt("", "", reg, "", "", "", "", "conversational", "test-vault", "test description")
		if strings.Contains(result, "## Memory") {
			t.Error("expected no ## Memory when neither muninn_recall nor muninn_where_left_off is registered")
		}
	})

	t.Run("not injected when muninn_recall blocked", func(t *testing.T) {
		reg := tools.NewRegistry()
		reg.Register(&stubTool{name: "muninn_recall"})
		reg.SetBlocked([]string{"muninn_recall"})

		result := buildAgentSystemPrompt("", "", reg, "", "", "", "", "conversational", "test-vault", "test description")
		if strings.Contains(result, "## Memory") {
			t.Error("expected no ## Memory when muninn_recall is blocked")
		}
	})

	t.Run("nil registry is safe", func(t *testing.T) {
		// Must not panic when reg is nil.
		result := buildAgentSystemPrompt("", "", nil, "", "", "", "", "conversational", "test-vault", "test description")
		if strings.Contains(result, "## Memory") {
			t.Error("expected no ## Memory when reg is nil")
		}
	})
}

func TestBuildAgentSystemPrompt_AgentSkillsInjected(t *testing.T) {
	fragment := "## Go Expert\nAlways use errgroup for concurrent work."
	got := buildAgentSystemPrompt("", fragment, nil, "", "", "", "", "", "", "")
	if !strings.Contains(got, "errgroup") {
		t.Errorf("expected agent skills fragment in prompt, got: %s", got)
	}
}

func TestBuildAgentSystemPrompt_EmptySkillsFragmentOmitted(t *testing.T) {
	got := buildAgentSystemPrompt("", "", nil, "", "", "", "", "", "", "")
	if strings.Contains(got, "## Skills") {
		t.Errorf("expected no skills section when fragment is empty, got: %s", got)
	}
}

func TestBuildAgentSystemPrompt_PerAgentSkillsTakesPrecedence(t *testing.T) {
	agentFrag := "## TDD Expert\nAlways write failing tests first."
	// Workspace rules in contextText — must always appear
	got := buildAgentSystemPrompt("workspace rules here", agentFrag, nil, "", "", "coder", "", "", "", "")
	if !strings.Contains(got, "TDD Expert") {
		t.Errorf("expected per-agent skill in prompt, got: %s", got)
	}
	if !strings.Contains(got, "workspace rules here") {
		t.Errorf("workspace rules must always appear in prompt, got: %s", got)
	}
}

func TestBuildAgentSystemPrompt_GlobalFallbackWhenNoAgentSkills(t *testing.T) {
	globalFrag := "## Global Skills\nGlobal catch-all."
	// Global fallback passed as agentSkillsFragment (orchestrator's job to compute it)
	got := buildAgentSystemPrompt("workspace rules here", globalFrag, nil, "", "", "coder", "", "", "", "")
	if !strings.Contains(got, "Global catch-all") {
		t.Errorf("expected global fallback skill in prompt, got: %s", got)
	}
	if !strings.Contains(got, "workspace rules here") {
		t.Errorf("workspace rules must always appear, got: %s", got)
	}
}

func TestMemoryModeInstruction(t *testing.T) {
	tests := []struct {
		mode    string
		wantSub string
	}{
		{"passive", "only when the user explicitly asks"},
		{"conversational", "muninn_recall"},
		{"immersive", "muninn_where_left_off"},
		{"", "muninn_recall"}, // empty defaults to conversational
	}
	for _, tc := range tests {
		got := memoryModeInstruction(tc.mode, "huginn:agent:mj:alice", "Alice coding memory")
		if !strings.Contains(got, tc.wantSub) {
			t.Errorf("mode=%q: expected %q in instruction, got:\n%s", tc.mode, tc.wantSub, got)
		}
	}
}

// TestMemoryModeInstruction_ContextParameterNamed verifies that both conversational
// and immersive modes explicitly mention passing parameters "as the context parameter"
// or similar. This ensures the instruction is clear about which parameter to use.
func TestMemoryModeInstruction_ContextParameterNamed(t *testing.T) {
	tests := []struct {
		mode          string
		description   string
		wantSubstring string
	}{
		{
			mode:          "conversational",
			description:   "conversational mode should mention context parameter for user message",
			wantSubstring: "as the `context` parameter",
		},
		{
			mode:          "immersive",
			description:   "immersive mode should mention context parameter for relevant topic",
			wantSubstring: "as the `context` parameter",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := memoryModeInstruction(tc.mode, "test-vault", "test description")
			if !strings.Contains(got, tc.wantSubstring) {
				t.Errorf("mode=%q: expected %q in instruction, got:\n%s",
					tc.mode, tc.wantSubstring, got)
			}
		})
	}
}

// stubTool is a minimal tools.Tool implementation for testing prompt injection.
type stubTool struct {
	name string
}

func (s *stubTool) Name() string                              { return s.name }
func (s *stubTool) Description() string                       { return "stub" }
func (s *stubTool) Permission() tools.PermissionLevel         { return tools.PermRead }
func (s *stubTool) Schema() backend.Tool {
	return backend.Tool{Type: "function", Function: backend.ToolFunction{Name: s.name}}
}
func (s *stubTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	return tools.ToolResult{}
}
