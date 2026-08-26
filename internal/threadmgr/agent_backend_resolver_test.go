package threadmgr

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/session"
)

// spawnAndWait runs one delegated thread to completion (or failure) and
// returns it.
func spawnAndWait(t *testing.T, tm *ThreadManager, reg *agents.AgentRegistry, agentID string, fallback backend.Backend) *Thread {
	t.Helper()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "claude-sonnet-4")
	_ = store.Append(sess, session.SessionMessage{Role: "user", Content: "do the thing"})

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: agentID, Task: "do the thing"})
	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, fallback,
		func(string, string, map[string]any) {}, NewCostAccumulator(0), nil)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := tm.Get(thread.ID)
		if got != nil && (got.Status == StatusDone || got.Status == StatusError || got.Status == StatusCancelled) {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	got, _ := tm.Get(thread.ID)
	return got
}

// TestSpawnThreadRoutesClaudeCodeAgentsThroughTheAgentResolver is Finding 5's
// delegation half. SetBackendResolver takes (provider, endpoint, apiKey,
// model) — a four-tuple that structurally cannot express "this agent's Claude
// Code session id and cwd" — so @-mentioning a claude-code agent fell into
// newFromResolvedConfig's default arm and failed with
// `unknown provider "claude-code"`. The agent-aware resolver must get first
// refusal, and it must receive the AGENT, not four strings.
func TestSpawnThreadRoutesClaudeCodeAgentsThroughTheAgentResolver(t *testing.T) {
	tm := New()
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:            "Codey",
		ModelID:         "claude-sonnet-4",
		Provider:        "claude-code",
		ClaudeSessionID: "b1f3c9d2-6e4a-4a11-9e0a-2f7d4c1a9b3e",
	})

	sessionBound := &trackingBackend{
		name:        "claude-code-session",
		fakeBackend: fakeBackend{response: &backend.ChatResponse{Content: "on it", DoneReason: "stop"}},
	}
	fallback := &trackingBackend{
		name:        "fallback-ollama",
		fakeBackend: fakeBackend{response: &backend.ChatResponse{Content: "wrong backend", DoneReason: "stop"}},
	}

	var sawAgent *agents.Agent
	tm.SetAgentBackendResolver(func(ag *agents.Agent) (backend.Backend, bool, error) {
		if ag == nil || ag.Provider != "claude-code" {
			return nil, false, nil
		}
		sawAgent = ag
		return sessionBound, true, nil
	})
	// The pre-existing four-tuple resolver still installed, and it must never
	// be consulted for this agent — it is the path that produced the failure.
	tm.SetBackendResolver(func(provider, endpoint, apiKey, model string) (backend.Backend, error) {
		return nil, fmt.Errorf("backend: unknown provider %q", provider)
	})

	got := spawnAndWait(t, tm, reg, "Codey", fallback)

	if sawAgent == nil {
		t.Fatal("the agent-aware resolver was never consulted; a claude-code agent cannot be delegated to")
	}
	if sawAgent.ClaudeSessionID == "" {
		t.Error("the resolver got an agent with no ClaudeSessionID — the whole point is that it receives per-agent state the four-tuple cannot carry")
	}
	if !sessionBound.wasUsed() {
		t.Error("the session-bound backend was not used")
	}
	if fallback.wasUsed() {
		t.Error("the fallback backend answered in the agent's name")
	}
	if got == nil || got.Status != StatusDone {
		t.Errorf("thread status = %v, want done", got)
	}
}

// A resolver that CLAIMS an agent and then fails must fail the thread with its
// own reason. Falling through to the generic resolver (or the fallback
// backend) would let a different provider answer in that agent's name, which
// looks like a working reply — the failure mode this whole hook exists to
// avoid.
func TestSpawnThreadFailsLoudlyWhenTheAgentResolverRefuses(t *testing.T) {
	tm := New()
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Codey", ModelID: "m", Provider: "claude-code"})

	fallback := &trackingBackend{
		name:        "fallback-ollama",
		fakeBackend: fakeBackend{response: &backend.ChatResponse{Content: "wrong backend", DoneReason: "stop"}},
	}
	refusal := errors.New("agent \"Codey\" has no claude_session_id")

	tm.SetAgentBackendResolver(func(ag *agents.Agent) (backend.Backend, bool, error) {
		return nil, true, refusal
	})
	tm.SetBackendResolver(func(provider, endpoint, apiKey, model string) (backend.Backend, error) {
		t.Error("the four-tuple resolver was consulted after the agent resolver claimed and refused")
		return fallback, nil
	})

	got := spawnAndWait(t, tm, reg, "Codey", fallback)

	if fallback.wasUsed() {
		t.Fatal("the fallback backend answered for an agent whose own resolver refused it")
	}
	if got == nil {
		t.Fatal("thread vanished")
	}
	if got.Summary == nil {
		t.Fatalf("thread finished with no summary; status = %s", got.Status)
	}
	if got.Summary.Status != "error" {
		t.Errorf("summary status = %q, want error — a refused agent must not look like a successful reply", got.Summary.Status)
	}
	if !strings.Contains(got.Summary.Summary, "claude_session_id") {
		t.Errorf("summary = %q, want the resolver's own reason so the user can act on it", got.Summary.Summary)
	}
}

// A declining resolver must be a byte-for-byte no-op: every existing agent
// keeps taking the four-tuple path exactly as before.
func TestSpawnThreadDecliningAgentResolverLeavesNormalAgentsUnchanged(t *testing.T) {
	tm := New()
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Sam", ModelID: "claude-sonnet-4", Provider: "anthropic"})

	resolved := &trackingBackend{
		name:        "resolved-anthropic",
		fakeBackend: fakeBackend{response: &backend.ChatResponse{Content: "Four.", DoneReason: "stop"}},
	}
	fallback := &trackingBackend{
		name:        "fallback-ollama",
		fakeBackend: fakeBackend{response: &backend.ChatResponse{Content: "wrong", DoneReason: "stop"}},
	}

	tm.SetAgentBackendResolver(func(ag *agents.Agent) (backend.Backend, bool, error) {
		return nil, false, nil // declines everything
	})
	tm.SetBackendResolver(func(provider, endpoint, apiKey, model string) (backend.Backend, error) {
		if provider == "anthropic" {
			return resolved, nil
		}
		return nil, fmt.Errorf("unknown provider %q", provider)
	})

	got := spawnAndWait(t, tm, reg, "Sam", fallback)

	if !resolved.wasUsed() {
		t.Error("the four-tuple resolver was skipped for a normal agent")
	}
	if fallback.wasUsed() {
		t.Error("fallback used for an agent the four-tuple resolver could handle")
	}
	if got == nil || got.Status != StatusDone {
		t.Errorf("thread status = %v, want done", got)
	}
}
