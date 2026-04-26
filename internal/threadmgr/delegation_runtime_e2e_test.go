package threadmgr

// End-to-end regression for issue #4 (missing MuninnDB tool calls):
//
// When a lead agent delegates via @mention, each spawned worker thread must
// receive its own per-agent runtime — so muninn_recall and other vault tools
// are exposed in the LLM ChatCompletion request, not just the global toolbelt.
//
// This test wires CreateFromMentions exactly the way main.go does:
//   tm.SetAgentRuntimePreparer(orch.PrepareAgentRuntime) // wired in main
//   CreateFromMentions(...)                              // emits per-mention thread
//   tm.SpawnThread(...)                                  // runs runtime preparer
//
// The recording backend captures the Tools slice from each spawned thread's
// first ChatCompletion call. The test asserts both spawned agents see their
// own muninn_recall schema and that the per-agent ExtraSystem text reaches
// the system prompt.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/session"
)

// captureBackend records the first request received per agent (keyed by
// the assistant prompt's identifying agent persona text). It returns a
// finish call so the spawned thread terminates cleanly.
type captureBackend struct {
	mu      sync.Mutex
	calls   map[string][]backend.Tool   // agentName -> tool schemas seen on first call
	systems map[string]string           // agentName -> first system prompt seen
	seen    map[string]bool             // tracks first call per agent
}

func newCaptureBackend() *captureBackend {
	return &captureBackend{
		calls:   make(map[string][]backend.Tool),
		systems: make(map[string]string),
		seen:    make(map[string]bool),
	}
}

func (b *captureBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	b.mu.Lock()
	// Identify the agent from the system prompt's persona block. The persona
	// builder embeds the agent name in the system content, so we can match by
	// substring. We just record once per system content; subsequent calls
	// in the same thread are post-tool messages and shouldn't overwrite.
	var system string
	for _, m := range req.Messages {
		if m.Role == "system" {
			system = m.Content
			break
		}
	}
	if !b.seen[system] {
		b.seen[system] = true
		b.calls[system] = append([]backend.Tool(nil), req.Tools...)
		b.systems[system] = system
	}
	b.mu.Unlock()

	return &backend.ChatResponse{
		DoneReason: "tool_use",
		ToolCalls: []backend.ToolCall{{
			ID: "fin",
			Function: backend.ToolCallFunction{
				Name:      "finish",
				Arguments: map[string]any{"summary": "done", "status": "success"},
			},
		}},
	}, nil
}
func (b *captureBackend) Health(_ context.Context) error   { return nil }
func (b *captureBackend) Shutdown(_ context.Context) error { return nil }
func (b *captureBackend) ContextWindow() int               { return 8192 }

// lookupAgent finds the captured ChatCompletion request whose system prompt
// belongs to the given agent. We can't search for the bare agent name because
// the spawned thread's Task is the lead agent's reply (which mentions BOTH
// workers), so name-substring matches are racy under parallel execution.
// Instead, match on the persona prefix "You are <agent>" emitted by
// agents.BuildPersonaPrompt — that string is unique to each agent's system
// prompt regardless of what's in the task body.
func (b *captureBackend) lookupAgent(name string) (tools []backend.Tool, system string, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	prefix := "You are " + name + ","
	for sys, ts := range b.calls {
		if strings.Contains(sys, prefix) {
			return ts, sys, true
		}
	}
	return nil, "", false
}

// TestDelegation_E2E_SpawnedThreadsReceiveMuninnSchemas is the headline
// regression for issue #4. It proves that a markdown-wrapped @mention from a
// lead agent results in two spawned threads, each of which receives its own
// muninn_recall tool schema (per-agent runtime), not just the global toolbelt.
func TestDelegation_E2E_SpawnedThreadsReceiveMuninnSchemas(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("delegation-e2e", "/tmp", "claude-haiku-4")

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Lead", ModelID: "claude-haiku-4"})
	reg.Register(&agents.Agent{Name: "Worker1", ModelID: "claude-haiku-4", VaultName: "worker1-vault"})
	reg.Register(&agents.Agent{Name: "Worker2", ModelID: "claude-haiku-4", VaultName: "worker2-vault"})

	// Wire the runtime preparer just like main.go does. Each agent receives
	// its own muninn_recall schema and a vault-specific ExtraSystem block.
	tm.SetAgentRuntimePreparer(func(_ context.Context, agentName string) (*AgentRuntime, error) {
		muninnSchema := backend.Tool{
			Type:     "function",
			Function: backend.ToolFunction{Name: "muninn_recall"},
		}
		return &AgentRuntime{
			Schemas: []backend.Tool{muninnSchema},
			ExecuteTool: func(_ context.Context, _ string, _ map[string]any) (string, error) {
				return `{"hits":0}`, nil
			},
			ExtraSystem: "\n\nMemory mode: Immersive — vault `" + agentName + "-vault`",
			Cleanup:     func() {},
		}, nil
	})

	cap := newCaptureBackend()

	// Seed the session so buildContext has at least one user message.
	_ = store.Append(sess, session.SessionMessage{
		Role:    "user",
		Content: "Lead, coordinate the team to investigate the bug.",
	})

	leadReply := "Working on it. Delegating to **@Worker1** for triage and **@Worker2** for the fix."

	var bmu sync.Mutex
	var broadcasts []capturedBroadcast
	broadcastFn := func(sid, msgType string, payload map[string]any) {
		bmu.Lock()
		broadcasts = append(broadcasts, capturedBroadcast{sid, msgType, payload})
		bmu.Unlock()
	}

	CreateFromMentions(
		context.Background(),
		sess.ID,
		leadReply,
		"parent-msg-1",
		reg,
		store,
		sess,
		cap,
		broadcastFn,
		NewCostAccumulator(0),
		tm,
		"Lead",
	)

	// Wait until both spawned threads finish. Without this we'd race the
	// goroutines and TempDir cleanup.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		allDone := true
		threads := tm.ListBySession(sess.ID)
		if len(threads) < 2 {
			allDone = false
		} else {
			for _, thr := range threads {
				if thr.Status != StatusDone && thr.Status != StatusError {
					allDone = false
					break
				}
			}
		}
		if allDone {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	for _, name := range []string{"Worker1", "Worker2"} {
		toolSchemas, system, ok := cap.lookupAgent(name)
		if !ok {
			t.Errorf("no ChatCompletion captured for %s — spawn likely never reached the backend", name)
			continue
		}

		var hasMuninn, hasFinish bool
		var seenNames []string
		for _, ts := range toolSchemas {
			seenNames = append(seenNames, ts.Function.Name)
			switch ts.Function.Name {
			case "muninn_recall":
				hasMuninn = true
			case "finish":
				hasFinish = true
			}
		}
		if !hasMuninn {
			t.Errorf("%s spawn: muninn_recall NOT in LLM tools (got %v) — per-agent runtime didn't reach the spawn loop", name, seenNames)
		}
		if !hasFinish {
			t.Errorf("%s spawn: finish missing from tools (got %v)", name, seenNames)
		}

		expectedExtra := "vault `" + name + "-vault`"
		if !strings.Contains(system, expectedExtra) {
			t.Errorf("%s spawn: ExtraSystem text %q not found in system prompt", name, expectedExtra)
		}
	}
}
