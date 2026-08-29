package agents_test

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
)

func TestBuildPersonaPrompt_ListsLocalTools(t *testing.T) {
	ag := &agents.Agent{Name: "Steve", LocalTools: []string{"bash", "read_file"}}
	got := agents.BuildPersonaPrompt(ag, "ctx")
	if !strings.Contains(got, "bash") || !strings.Contains(got, "read_file") {
		t.Fatalf("expected listed local tools, got:\n%s", got)
	}
	if strings.Contains(got, "no local tools") {
		t.Fatalf("named tools should not say no local tools:\n%s", got)
	}
}

func TestBuildPersonaPrompt_NoLocalTools(t *testing.T) {
	ag := &agents.Agent{Name: "Reggie"}
	got := agents.BuildPersonaPrompt(ag, "ctx")
	if !strings.Contains(got, "no local tools") {
		t.Fatalf("expected 'no local tools', got:\n%s", got)
	}
}

func TestBuildPersonaPrompt_NoImageGeneration(t *testing.T) {
	ag := &agents.Agent{Name: "Reggie"}
	got := agents.BuildPersonaPrompt(ag, "ctx")
	if !strings.Contains(got, "You do not have image generation.") {
		t.Fatalf("expected no-image line, got:\n%s", got)
	}
}

func TestBuildPersonaPrompt_HasGenerateImage_OmitsNoImage(t *testing.T) {
	ag := &agents.Agent{Name: "Pixie", LocalTools: []string{"generate_image"}}
	got := agents.BuildPersonaPrompt(ag, "ctx")
	if strings.Contains(got, "You do not have image generation.") {
		t.Fatalf("granted generate_image should omit no-image line:\n%s", got)
	}
	if !strings.Contains(got, "generate_image") {
		t.Fatalf("expected generate_image in local tools, got:\n%s", got)
	}
}

func TestBuildPersonaPrompt_TierLow_CannotDelegate(t *testing.T) {
	ag := &agents.Agent{Name: "Tiny", ModelID: "qwen2.5-coder:7b"}
	got := agents.BuildPersonaPrompt(ag, "ctx")
	if !strings.Contains(got, "You cannot delegate.") {
		t.Fatalf("expected cannot-delegate for 7b, got:\n%s", got)
	}
}

func TestBuildPersonaPrompt_HighTier_OmitsCannotDelegate(t *testing.T) {
	ag := &agents.Agent{Name: "Winston", ModelID: "claude-sonnet-4"}
	got := agents.BuildPersonaPrompt(ag, "ctx")
	if strings.Contains(got, "You cannot delegate.") {
		t.Fatalf("high-tier should not say cannot delegate:\n%s", got)
	}
}

func TestBuildPersonaPromptWithRoster_TierLow_OmitsDelegateHint(t *testing.T) {
	ag := &agents.Agent{Name: "Tiny", ModelID: "qwen2.5-coder:7b"}
	got := agents.BuildPersonaPromptWithRoster(ag, "ctx", "Available team members:\n- Steve")
	if !strings.Contains(got, "Steve") {
		t.Fatalf("expected roster member, got:\n%s", got)
	}
	if strings.Contains(got, "delegate_to_agent") {
		t.Fatalf("7b roster must not instruct delegate_to_agent:\n%s", got)
	}
	if !strings.Contains(got, "You cannot delegate.") {
		t.Fatalf("expected cannot-delegate addendum, got:\n%s", got)
	}
}

// TestBuildSkeletonPersonaPrompt_OmitsCapabilityAddendum verifies the
// skeleton prompt (perf wave step 2a) never includes the capabilities
// block that BuildPersonaPrompt always adds — that block only matters for
// tool-calling turns, never for a trivial ack/ping.
func TestBuildSkeletonPersonaPrompt_OmitsCapabilityAddendum(t *testing.T) {
	ag := &agents.Agent{Name: "Steve", LocalTools: []string{"bash", "read_file"}}
	got := agents.BuildSkeletonPersonaPrompt(ag)
	if strings.Contains(got, "## Your capabilities") {
		t.Fatalf("skeleton prompt must omit capability addendum, got:\n%s", got)
	}
	if !strings.Contains(got, "Steve") {
		t.Fatalf("skeleton prompt must keep the agent identity line, got:\n%s", got)
	}
}

// TestBuildSkeletonPersonaPrompt_KeepsPersonalityAddendum verifies the
// short personality addendum (task 2a explicitly calls it out as cheap
// enough to keep) still reaches the skeleton prompt.
func TestBuildSkeletonPersonaPrompt_KeepsPersonalityAddendum(t *testing.T) {
	ag := &agents.Agent{Name: "Reggie", Personality: agents.PersonalityTerseOperator}
	got := agents.BuildSkeletonPersonaPrompt(ag)
	if !strings.Contains(got, "Terse Operator") {
		t.Fatalf("skeleton prompt must keep the personality addendum, got:\n%s", got)
	}
}

// TestBuildSkeletonPersonaPrompt_MaterialSmallerThanFull demonstrates the
// prompt-budget win: a skeleton prompt for an agent with a real persona,
// local-tool grants, and a long SystemPrompt is materially smaller than the
// full BuildPersonaPrompt output for the same agent.
func TestBuildSkeletonPersonaPrompt_MaterialSmallerThanFull(t *testing.T) {
	ag := &agents.Agent{
		Name:         "Winston",
		SystemPrompt: "You are Winston, the Chief of Staff. You coordinate the team, triage incoming requests, and delegate work to specialists based on their strengths.",
		LocalTools:   []string{"*"},
	}
	full := agents.BuildPersonaPrompt(ag, "")
	skeleton := agents.BuildSkeletonPersonaPrompt(ag)
	if len(skeleton) >= len(full) {
		t.Fatalf("skeleton (%d bytes) must be smaller than full prompt (%d bytes)", len(skeleton), len(full))
	}
	// Not just smaller — meaningfully smaller, not a rounding error.
	if len(skeleton) > len(full)/2 {
		t.Fatalf("skeleton (%d bytes) not materially smaller than full prompt (%d bytes)", len(skeleton), len(full))
	}
}
