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
