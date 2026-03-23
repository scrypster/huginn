package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/stats"
	"github.com/scrypster/huginn/internal/workspace"
)

// mockBackend is a minimal backend.Backend for integration tests.
// It cycles through preset responses in order.
type mockBackend struct {
	mu        sync.Mutex
	responses []string
	idx       int
}

func newMockBackend(responses ...string) *mockBackend {
	return &mockBackend{responses: responses}
}

func (m *mockBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var content string
	if m.idx < len(m.responses) {
		content = m.responses[m.idx]
		m.idx++
	}
	if req.OnToken != nil && content != "" {
		req.OnToken(content)
	}
	return &backend.ChatResponse{Content: content, DoneReason: "stop"}, nil
}

func (m *mockBackend) Health(_ context.Context) error   { return nil }
func (m *mockBackend) Shutdown(_ context.Context) error { return nil }
func (m *mockBackend) ContextWindow() int               { return 128_000 }

// TestSmoke_Chat_AgentLoop is the end-to-end smoke test for agent chat with stats.
func TestSmoke_Chat_AgentLoop(t *testing.T) {
	reg := stats.NewRegistry()
	sc := reg.Collector()

	mb := newMockBackend("1. Step one: add Hello\n2. Step two: add tests")
	models := &modelconfig.Models{
		Reasoner: "test-model",
	}

	orch, err := agent.NewOrchestrator(mb, models, nil, nil, sc, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	var tokens []string
	if err := orch.Chat(context.Background(), "Add a Hello function", func(tok string) {
		tokens = append(tokens, tok)
	}, nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if orch.CurrentState() != agent.StateIdle {
		t.Errorf("expected StateIdle after Chat, got %v", orch.CurrentState())
	}

	if len(tokens) == 0 {
		t.Error("expected tokens from chat, got none")
	}

	// Verify latency histogram was recorded.
	snap := reg.Snapshot()
	var latencyRecorded bool
	for _, h := range snap.Histograms {
		if h.Metric == "agent.llm_latency_ms" {
			latencyRecorded = true
			break
		}
	}
	if !latencyRecorded {
		t.Error("expected 'agent.llm_latency_ms' histogram to be recorded")
	}
}

// TestSmoke_Chat_WithStats verifies that a plain Chat call records latency.
func TestSmoke_Chat_WithStats(t *testing.T) {
	reg := stats.NewRegistry()
	sc := reg.Collector()

	mb := newMockBackend("a helpful response")
	models := modelconfig.DefaultModels()
	orch, err := agent.NewOrchestrator(mb, models, nil, nil, sc, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}

	if err := orch.Chat(context.Background(), "what is Go?", nil, nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	snap := reg.Snapshot()
	var latencyFound bool
	for _, h := range snap.Histograms {
		if h.Metric == "agent.llm_latency_ms" {
			latencyFound = true
			break
		}
	}
	if !latencyFound {
		t.Error("expected 'agent.llm_latency_ms' histogram to be recorded after Chat()")
	}
}

// TestSmoke_WorkspaceDiscovery verifies that workspace.NewManager works end-to-end.
func TestSmoke_WorkspaceDiscovery(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	subDir := filepath.Join(root, "internal", "mypackage")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	mgr, err := workspace.NewManager(subDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	if mgr.Root() != root {
		t.Errorf("expected root %q, got %q", root, mgr.Root())
	}
	if mgr.Method() != "git" {
		t.Errorf("expected method 'git', got %q", mgr.Method())
	}
	if mgr.Config() != nil {
		t.Error("expected nil Config() when no workspace.json")
	}
}
