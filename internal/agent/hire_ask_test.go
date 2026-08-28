package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/tools"
)

func TestParseNamedHire(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in         string
		name, role string
		ok         bool
	}{
		{"@Winston create an agent named driveprobe-1 who researches", "driveprobe-1", "researches", true},
		{"@Winston hire a teammate named driveprobe2-1 who researches the web", "driveprobe2-1", "researches the web", true},
		{"hire a teammate named Nova who researches", "Nova", "researches", true},
		{"create an agent named fableprobe who researches.", "fableprobe", "researches", true},
		{"hire someone who researches", "", "", false},
		{"create an agent", "", "", false},
		{"what time is it", "", "", false},
		{"hire Steve who researches the web", "Steve", "researches the web", true},
	}
	for _, tc := range cases {
		name, role, ok := ParseNamedHire(tc.in)
		if ok != tc.ok || name != tc.name || role != tc.role {
			t.Errorf("ParseNamedHire(%q) = %q, %q, %v; want %q, %q, %v", tc.in, name, role, ok, tc.name, tc.role, tc.ok)
		}
	}
}

func TestChatWithAgent_NamedHireFastPathPersists(t *testing.T) {
	mb := &mockBackend{responses: []*backend.ChatResponse{stopResponse("should not run")}}
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	rec := &hirePersistRec{}
	reg := tools.NewRegistry()
	tools.RegisterCreateAgentTool(reg, &tools.CreateAgentTool{Deps: tools.CreateAgentDeps{
		Persist: func(req tools.CreateAgentRequest) error {
			rec.got = req
			return nil
		},
		AgentExists:  func(string) bool { return false },
		ValidateName: func(string) error { return nil },
		MachineModel: "qwen2.5-coder:14b",
	}})
	o.SetTools(reg, permissions.NewGate(true, nil))
	ag := &agents.Agent{
		Name:       "Winston",
		ModelID:    "qwen2.5-coder:14b",
		LocalTools: []string{"create_agent"},
	}
	var tokens []string
	if err := o.ChatWithAgent(context.Background(), ag, "@Winston create an agent named driveprobe-unit who researches", "sess-hire-fast", func(tok string) {
		tokens = append(tokens, tok)
	}, nil, nil); err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}
	if rec.got.Name != "driveprobe-unit" || rec.got.Description != "researches" {
		t.Fatalf("persist = %+v", rec.got)
	}
	joined := strings.Join(tokens, "")
	if !strings.Contains(joined, "driveprobe-unit") || !strings.Contains(joined, "research") {
		t.Fatalf("speech %q", joined)
	}
	if strings.Contains(joined, "Delegated to") || strings.Contains(joined, "Local time now") {
		t.Fatalf("leftover speech %q", joined)
	}
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) != 0 {
		t.Fatalf("named hire must not call 14b; got %d requests", len(mb.lastRequests))
	}
}

// TestChatWithAgent_NamedHireFastPathAssignsVault covers defect #1: the hire
// fast path used to hardcode memory:false, so a named hire never got a
// vault — "No vault yet" forever, even with a healthy Muninn. It must now
// assign a vault the same way a hire that goes through the model does.
func TestChatWithAgent_NamedHireFastPathAssignsVault(t *testing.T) {
	mb := &mockBackend{responses: []*backend.ChatResponse{stopResponse("should not run")}}
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	rec := &hirePersistRec{}
	reg := tools.NewRegistry()
	tools.RegisterCreateAgentTool(reg, &tools.CreateAgentTool{Deps: tools.CreateAgentDeps{
		Persist: func(req tools.CreateAgentRequest) error {
			rec.got = req
			return nil
		},
		AgentExists:  func(string) bool { return false },
		ValidateName: func(string) error { return nil },
		// Stubbed healthy muninn: TryVault succeeds.
		TryVault:     func(vaultName, label string) bool { return true },
		MachineModel: "qwen2.5-coder:14b",
	}})
	o.SetTools(reg, permissions.NewGate(true, nil))
	ag := &agents.Agent{
		Name:       "Winston",
		ModelID:    "qwen2.5-coder:14b",
		LocalTools: []string{"create_agent"},
	}
	var tokens []string
	if err := o.ChatWithAgent(context.Background(), ag, "@Winston hire a teammate named fableprobe who researches the web", "sess-hire-vault-ok", func(tok string) {
		tokens = append(tokens, tok)
	}, nil, nil); err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}
	if !rec.got.Memory {
		t.Fatal("hire must assign a vault like a normal hire (memory=true)")
	}
	if rec.got.VaultName == "" {
		t.Fatal("hire must set VaultName so the first turn connects a vault")
	}
	joined := strings.Join(tokens, "")
	if strings.Contains(joined, "No vault yet") {
		t.Fatalf("healthy muninn should not say 'No vault yet': %q", joined)
	}
}

// TestChatWithAgent_NamedHireFastPathNoMuninnKeepsNoVaultYet covers the
// other half of defect #1's TDD: with no muninn configured (TryVault unset,
// mirroring the tool's real "connect failed" path), the ack still honestly
// says "No vault yet" instead of claiming a vault that isn't reachable.
func TestChatWithAgent_NamedHireFastPathNoMuninnKeepsNoVaultYet(t *testing.T) {
	mb := &mockBackend{responses: []*backend.ChatResponse{stopResponse("should not run")}}
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	rec := &hirePersistRec{}
	reg := tools.NewRegistry()
	tools.RegisterCreateAgentTool(reg, &tools.CreateAgentTool{Deps: tools.CreateAgentDeps{
		Persist: func(req tools.CreateAgentRequest) error {
			rec.got = req
			return nil
		},
		AgentExists:  func(string) bool { return false },
		ValidateName: func(string) error { return nil },
		// No TryVault wired: mirrors "no muninn config" — TryVault is nil.
		MachineModel: "qwen2.5-coder:14b",
	}})
	o.SetTools(reg, permissions.NewGate(true, nil))
	ag := &agents.Agent{
		Name:       "Winston",
		ModelID:    "qwen2.5-coder:14b",
		LocalTools: []string{"create_agent"},
	}
	var tokens []string
	if err := o.ChatWithAgent(context.Background(), ag, "@Winston hire a teammate named fableprobe who researches the web", "sess-hire-vault-none", func(tok string) {
		tokens = append(tokens, tok)
	}, nil, nil); err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}
	joined := strings.Join(tokens, "")
	if !strings.Contains(joined, "No vault yet") {
		t.Fatalf("unreachable muninn should still say 'No vault yet': %q", joined)
	}
	if rec.got.VaultName == "" {
		t.Fatal("VaultName should still be assigned on the agent record even when the live connect attempt fails")
	}
}

func TestChatWithAgent_HireStripsDelegationKeepsCreateAgent(t *testing.T) {
	o, mb, ag := leadWithDelegationTools(t)
	ag.LocalTools = []string{"create_agent"}
	if err := o.ChatWithAgent(context.Background(), ag, "hire someone who can research", "sess-hire-strip", nil, nil, nil); err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}
	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.lastRequests) == 0 {
		t.Fatal("ambiguous hire should still reach the model")
	}
	names := map[string]bool{}
	for _, n := range toolNames(mb.lastRequests[0]) {
		names[n] = true
	}
	if names["delegate_to_agent"] || names["wait_for_threads"] || names["consult_agent"] {
		t.Fatalf("hire must not see 14b delegation tools; got %v", toolNames(mb.lastRequests[0]))
	}
	if !names["create_agent"] {
		t.Fatalf("hire must keep create_agent; got %v", toolNames(mb.lastRequests[0]))
	}
}

type hirePersistRec struct {
	got tools.CreateAgentRequest
}
