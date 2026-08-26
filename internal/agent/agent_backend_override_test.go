package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
)

// sentinelBackend is identity-only: tests assert on the pointer, so they can
// tell WHICH resolution path produced the backend.
type sentinelBackend struct{ name string }

func (s *sentinelBackend) ChatCompletion(context.Context, backend.ChatRequest) (*backend.ChatResponse, error) {
	return &backend.ChatResponse{Content: s.name}, nil
}
func (s *sentinelBackend) Health(context.Context) error   { return nil }
func (s *sentinelBackend) Shutdown(context.Context) error { return nil }
func (s *sentinelBackend) ContextWindow() int             { return 1000 }

// overrideTestOrch builds an orchestrator whose cache resolves provider
// "anthropic" to a known instance, so "did this go through bc.For?" is
// observable by pointer identity.
func overrideTestOrch(t *testing.T) (*Orchestrator, *sentinelBackend, *sentinelBackend) {
	t.Helper()
	fallback := &sentinelBackend{name: "fallback"}
	viaCache := &sentinelBackend{name: "via-cache"}
	o := mustNewOrchestrator(t, fallback, newTestModels(), nil, nil, nil, nil)
	bc := backend.NewBackendCache(fallback)
	bc.SetProviderBackend("anthropic", viaCache)
	o.SetBackendCache(bc)
	return o, fallback, viaCache
}

// TestBackendFor_NilOverrideIsUnchanged pins the default path. Every native
// Huginn agent resolves this way, so a regression here breaks the product,
// not just the Claude Code feature.
func TestBackendFor_NilOverrideIsUnchanged(t *testing.T) {
	o, fallback, viaCache := overrideTestOrch(t)

	got, err := o.backendFor(&agents.Agent{Name: "Tom", Provider: "anthropic", ModelID: "sonnet"})
	if err != nil {
		t.Fatalf("backendFor: %v", err)
	}
	if got != viaCache {
		t.Fatalf("a normal agent did not resolve through bc.For: got %#v", got)
	}

	got, err = o.backendFor(nil)
	if err != nil {
		t.Fatalf("backendFor(nil): %v", err)
	}
	if got != fallback {
		t.Fatalf("nil agent must resolve to the cache's fallback, got %#v", got)
	}
}

// TestBackendFor_OverrideClaimsItsOwnAgents: a claude-code agent reaches the
// hook. It must NOT fall through to bc.For — "claude-code" is not a provider
// the factory knows, so the cache would fail to build anything.
func TestBackendFor_OverrideClaimsItsOwnAgents(t *testing.T) {
	o, _, viaCache := overrideTestOrch(t)
	viaOverride := &sentinelBackend{name: "via-override"}

	o.SetAgentBackendOverride(func(ag *agents.Agent) (backend.Backend, bool, error) {
		if ag.Provider != "claude-code" {
			return nil, false, nil
		}
		return viaOverride, true, nil
	})

	got, err := o.backendFor(&agents.Agent{
		Name:            "Codey",
		Provider:        "claude-code",
		ClaudeSessionID: "11111111-2222-3333-4444-555555555555",
	})
	if err != nil {
		t.Fatalf("backendFor: %v", err)
	}
	if got != viaOverride {
		t.Fatalf("claude-code agent did not resolve through the override: got %#v", got)
	}
	if got == viaCache {
		t.Fatal("claude-code agent fell through to the backend cache")
	}
}

// TestBackendFor_DecliningOverrideLeavesNormalAgentsUnchanged is the half that
// matters most: installing the hook must not perturb the path every existing
// agent takes.
func TestBackendFor_DecliningOverrideLeavesNormalAgentsUnchanged(t *testing.T) {
	o, fallback, viaCache := overrideTestOrch(t)

	var consulted []string
	o.SetAgentBackendOverride(func(ag *agents.Agent) (backend.Backend, bool, error) {
		consulted = append(consulted, ag.Name)
		return nil, false, nil // declines everything
	})

	got, err := o.backendFor(&agents.Agent{Name: "Tom", Provider: "anthropic", ModelID: "sonnet"})
	if err != nil {
		t.Fatalf("backendFor: %v", err)
	}
	if got != viaCache {
		t.Fatalf("a declined agent stopped resolving through bc.For: got %#v", got)
	}
	if len(consulted) != 1 || consulted[0] != "Tom" {
		t.Fatalf("override consulted = %v, want it offered exactly [Tom]", consulted)
	}

	// A nil agent carries no per-agent state, so there is nothing for the hook
	// to inspect; it must be skipped entirely rather than handed a nil.
	got, err = o.backendFor(nil)
	if err != nil {
		t.Fatalf("backendFor(nil): %v", err)
	}
	if got != fallback {
		t.Fatalf("nil agent resolution changed: got %#v", got)
	}
	if len(consulted) != 1 {
		t.Fatalf("override was consulted for a nil agent: %v", consulted)
	}
}

func TestBackendFor_OverrideErrorPropagates(t *testing.T) {
	o, _, _ := overrideTestOrch(t)
	boom := errors.New("claude binary missing")
	o.SetAgentBackendOverride(func(*agents.Agent) (backend.Backend, bool, error) {
		return nil, false, boom
	})

	got, err := o.backendFor(&agents.Agent{Name: "Codey", Provider: "claude-code"})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want %v", err, boom)
	}
	if got != nil {
		t.Fatalf("a failed resolution must not return a backend, got %#v", got)
	}
}
