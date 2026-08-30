package permissions

import (
	"testing"

	"github.com/scrypster/huginn/internal/tools"
)

// --- SetExecRequiresPrompt: PermExec requires a prompt even under skipAll ---

func TestExecRequiresPrompt_BashPromptsUnderSkipAll(t *testing.T) {
	called := false
	g := NewGate(true, func(req PermissionRequest) Decision {
		called = true
		if req.ToolName != "bash" {
			t.Errorf("expected bash, got %s", req.ToolName)
		}
		return Deny
	})
	g.SetExecRequiresPrompt(true)

	req := PermissionRequest{ToolName: "bash", Level: tools.PermExec}
	if g.Check(req) {
		t.Error("expected bash to be denied (promptFunc returned Deny)")
	}
	if !called {
		t.Error("expected promptFunc to be called for PermExec under skipAll")
	}
}

func TestExecRequiresPrompt_OtherLevelsStillSkip(t *testing.T) {
	called := false
	g := NewGate(true, func(req PermissionRequest) Decision {
		called = true
		return Deny
	})
	g.SetExecRequiresPrompt(true)

	req := PermissionRequest{ToolName: "write_file", Level: tools.PermWrite}
	if !g.Check(req) {
		t.Error("expected write_file to still be auto-approved under skipAll")
	}
	if called {
		t.Error("promptFunc should not be called for non-exec levels")
	}
}

func TestExecRequiresPrompt_Disabled_NoPromptEvenUnderSkipAll(t *testing.T) {
	called := false
	g := NewGate(true, func(req PermissionRequest) Decision {
		called = true
		return Deny
	})
	// SetExecRequiresPrompt not called — default false.
	req := PermissionRequest{ToolName: "bash", Level: tools.PermExec}
	if !g.Check(req) {
		t.Error("expected bash to be auto-approved when execRequiresPrompt is off")
	}
	if called {
		t.Error("promptFunc should not be called when execRequiresPrompt is off")
	}
}

func TestExecRequiresPrompt_SessionAllowedSkipsPrompt(t *testing.T) {
	calls := 0
	g := NewGate(true, func(req PermissionRequest) Decision {
		calls++
		return AllowAll
	})
	g.SetExecRequiresPrompt(true)

	req := PermissionRequest{ToolName: "bash", Level: tools.PermExec}
	if !g.Check(req) {
		t.Fatal("expected first call to be allowed (AllowAll)")
	}
	if !g.Check(req) {
		t.Fatal("expected second call to be allowed via sessionAllowed")
	}
	if calls != 1 {
		t.Errorf("expected promptFunc called once, got %d", calls)
	}
}

func TestExecRequiresPrompt_PropagatesThroughFork(t *testing.T) {
	called := false
	g := NewGate(true, func(req PermissionRequest) Decision {
		called = true
		return Deny
	})
	g.SetExecRequiresPrompt(true)
	child := g.Fork(nil, nil)

	req := PermissionRequest{ToolName: "bash", Level: tools.PermExec}
	if child.Check(req) {
		t.Error("expected forked gate to still require a prompt for exec")
	}
	if !called {
		t.Error("expected forked gate's promptFunc to be invoked")
	}
}

// --- SetPromptFunc: late binding ---

func TestSetPromptFunc_LateBinding(t *testing.T) {
	g := NewGate(false, nil)
	req := PermissionRequest{ToolName: "bash", Level: tools.PermExec}

	result := g.CheckDetailed(req)
	if result.Allowed {
		t.Fatal("expected deny with no promptFunc bound")
	}
	if result.ReasonCode != ReasonPromptUnavailable {
		t.Errorf("expected ReasonPromptUnavailable, got %s", result.ReasonCode)
	}

	g.SetPromptFunc(func(PermissionRequest) Decision { return Allow })
	if !g.Check(req) {
		t.Error("expected bash to be allowed after SetPromptFunc")
	}
}

// --- SeedSessionAllowed: pre-seed persisted grants ---

func TestSeedSessionAllowed_SkipsPrompt(t *testing.T) {
	called := false
	g := NewGate(false, func(req PermissionRequest) Decision {
		called = true
		return Deny
	})
	g.SeedSessionAllowed([]string{"bash"})

	req := PermissionRequest{ToolName: "bash", Level: tools.PermExec}
	if !g.Check(req) {
		t.Error("expected seeded tool to be allowed without prompting")
	}
	if called {
		t.Error("promptFunc should not be called for a seeded tool")
	}
}

func TestSeedSessionAllowed_EmptyIsNoop(t *testing.T) {
	g := NewGate(false, nil)
	g.SeedSessionAllowed(nil)
	g.SeedSessionAllowed([]string{})
	// No panic, no allowed entries.
	req := PermissionRequest{ToolName: "bash", Level: tools.PermExec}
	result := g.CheckDetailed(req)
	if result.Allowed {
		t.Fatal("expected deny — nothing seeded and no promptFunc")
	}
}

// TestExecRequiresPrompt_ExemptToolSkipsPrompt covers delegate_to_agent: it is
// PermExec (so exec-requires-prompt would catch it) but runs no code of its
// own and has its own approval UX, so it must not raise a permission banner.
func TestExecRequiresPrompt_ExemptToolSkipsPrompt(t *testing.T) {
	prompted := 0
	g := NewGate(true, func(PermissionRequest) Decision {
		prompted++
		return Deny
	})
	defer g.Close()
	g.SetExecRequiresPrompt(true)
	g.SetExecPromptExempt([]string{"delegate_to_agent"})

	if !g.Check(PermissionRequest{ToolName: "delegate_to_agent", Level: tools.PermExec}) {
		t.Error("exempt PermExec tool should be allowed under skipAll without prompting")
	}
	if prompted != 0 {
		t.Errorf("exempt tool prompted %d times, want 0", prompted)
	}

	// A non-exempt PermExec tool must still prompt.
	if g.Check(PermissionRequest{ToolName: "bash", Level: tools.PermExec}) {
		t.Error("bash should still be denied by the prompt")
	}
	if prompted != 1 {
		t.Errorf("bash prompted %d times, want 1", prompted)
	}
}

// TestExecRequiresPrompt_ExemptSetPropagatesThroughFork verifies a per-agent
// forked gate inherits the exemption — every agent run uses a fork, so an
// exemption that did not survive Fork would be dead configuration.
func TestExecRequiresPrompt_ExemptSetPropagatesThroughFork(t *testing.T) {
	prompted := 0
	parent := NewGate(true, func(PermissionRequest) Decision {
		prompted++
		return Deny
	})
	defer parent.Close()
	parent.SetExecRequiresPrompt(true)
	parent.SetExecPromptExempt([]string{"delegate_to_agent"})

	child := parent.Fork(nil, nil)
	defer child.Close()

	if !child.Check(PermissionRequest{ToolName: "delegate_to_agent", Level: tools.PermExec}) {
		t.Error("forked gate should inherit the exec-prompt exemption")
	}
	if prompted != 0 {
		t.Errorf("forked gate prompted %d times for an exempt tool, want 0", prompted)
	}
	if child.Check(PermissionRequest{ToolName: "bash", Level: tools.PermExec}) {
		t.Error("forked gate must still prompt (and deny) for bash")
	}
}

// TestSetExecPromptExempt_EmptyClears verifies passing an empty list removes a
// previously configured exemption rather than silently keeping it.
func TestSetExecPromptExempt_EmptyClears(t *testing.T) {
	prompted := 0
	g := NewGate(true, func(PermissionRequest) Decision {
		prompted++
		return Allow
	})
	defer g.Close()
	g.SetExecRequiresPrompt(true)
	g.SetExecPromptExempt([]string{"delegate_to_agent"})
	g.SetExecPromptExempt(nil)

	if !g.Check(PermissionRequest{ToolName: "delegate_to_agent", Level: tools.PermExec}) {
		t.Fatal("expected Allow from promptFunc")
	}
	if prompted != 1 {
		t.Errorf("expected the cleared exemption to prompt once, got %d", prompted)
	}
}

// MJ's "approve everything for this agent" wildcard: approved_tools ["*"]
// suppresses exec prompting for every tool on the seeded gate.
func TestSeedSessionAllowed_WildcardCoversAllExecTools(t *testing.T) {
	g := NewGate(true, nil)
	g.SetExecRequiresPrompt(true)
	prompted := 0
	g.SetPromptFunc(func(req PermissionRequest) Decision { prompted++; return Deny })
	fork := g.Fork(nil, nil)
	fork.SeedSessionAllowed([]string{"*"})
	for _, tool := range []string{"bash", "run_tests"} {
		res := fork.CheckDetailed(PermissionRequest{ToolName: tool, Level: tools.PermExec})
		if !res.Allowed {
			t.Fatalf("wildcard grant should allow %s without prompting: %+v", tool, res)
		}
	}
	if prompted != 0 {
		t.Fatalf("promptFunc fired %d times despite wildcard grant", prompted)
	}
}
