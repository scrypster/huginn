package permissions

import (
	"testing"

	"github.com/scrypster/huginn/internal/tools"
)

func TestCheckDetailed_DeniesProviderWithReasonCode(t *testing.T) {
	g := NewGate(false, nil)
	t.Cleanup(g.Close)
	g.SetAllowedProviders(map[string]bool{"github": true})

	res := g.CheckDetailed(PermissionRequest{
		ToolName: "slack_post",
		Level:    tools.PermWrite,
		Provider: "slack",
	})
	if res.Allowed {
		t.Fatal("expected deny")
	}
	if res.ReasonCode != ReasonProviderNotAllowed {
		t.Fatalf("ReasonCode = %q, want %q", res.ReasonCode, ReasonProviderNotAllowed)
	}
	if res.Reason == "" {
		t.Fatal("expected non-empty safe reason")
	}
}

func TestCheckDetailed_DeniesWhenPromptUnavailable(t *testing.T) {
	g := NewGate(false, nil)
	t.Cleanup(g.Close)

	res := g.CheckDetailed(PermissionRequest{
		ToolName: "write_file",
		Level:    tools.PermWrite,
	})
	if res.Allowed {
		t.Fatal("expected deny")
	}
	if res.ReasonCode != ReasonPromptUnavailable {
		t.Fatalf("ReasonCode = %q, want %q", res.ReasonCode, ReasonPromptUnavailable)
	}
}

func TestCheckDetailed_DeniesWhenUserRejects(t *testing.T) {
	g := NewGate(false, func(PermissionRequest) Decision { return Deny })
	t.Cleanup(g.Close)

	res := g.CheckDetailed(PermissionRequest{
		ToolName: "write_file",
		Level:    tools.PermWrite,
	})
	if res.Allowed {
		t.Fatal("expected deny")
	}
	if res.ReasonCode != ReasonUserDenied {
		t.Fatalf("ReasonCode = %q, want %q", res.ReasonCode, ReasonUserDenied)
	}
}
