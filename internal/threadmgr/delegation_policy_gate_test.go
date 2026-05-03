package threadmgr

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

func TestSpawnThread_HighRiskToolBlockedWithoutApprovalToken(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("policy-gate-no-token", "/tmp", "claude-haiku-4")
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Worker", ModelID: "claude-haiku-4", LocalTools: []string{"github_delete_issue"}})

	var mu sync.Mutex
	var executorCalls []string
	var toolDonePayload map[string]any
	tm.SetToolExecutor(func(_ context.Context, name string, _ map[string]any) (string, error) {
		mu.Lock()
		executorCalls = append(executorCalls, name)
		mu.Unlock()
		return "ok", nil
	})

	thread, err := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Worker", Task: "delete an issue"})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	backend := &scriptedToolBackend{toolName: "github_delete_issue", args: map[string]any{"issue": 123}}

	done := make(chan struct{})
	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, backend, func(_ string, msgType string, payload map[string]any) {
		if msgType == "thread_tool_done" {
			toolDonePayload = payload
		}
		if msgType == "thread_done" {
			close(done)
		}
	}, NewCostAccumulator(0), nil)

	waitDone(t, done)

	mu.Lock()
	defer mu.Unlock()
	if len(executorCalls) != 0 {
		t.Fatalf("expected executor to be blocked without token, got calls: %v", executorCalls)
	}
	if toolDonePayload == nil {
		t.Fatal("expected thread_tool_done payload")
	}
	summary, _ := toolDonePayload["result_summary"].(string)
	if !strings.Contains(summary, "approval token required") {
		t.Fatalf("expected approval token denial summary, got %q", summary)
	}
}

func TestSpawnThread_LowRiskReadToolBypassesApprovalToken(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("policy-gate-low-risk", "/tmp", "claude-haiku-4")
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Worker", ModelID: "claude-haiku-4", LocalTools: []string{"github_list_repos"}})

	var mu sync.Mutex
	var executorCalls []string
	tm.SetToolExecutor(func(_ context.Context, name string, _ map[string]any) (string, error) {
		mu.Lock()
		executorCalls = append(executorCalls, name)
		mu.Unlock()
		return "repos listed", nil
	})

	thread, err := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Worker", Task: "list repos"})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	backend := &scriptedToolBackend{toolName: "github_list_repos", args: map[string]any{"org": "acme"}}

	done := make(chan struct{})
	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, backend, func(_ string, msgType string, _ map[string]any) {
		if msgType == "thread_done" {
			close(done)
		}
	}, NewCostAccumulator(0), nil)

	waitDone(t, done)

	mu.Lock()
	defer mu.Unlock()
	if len(executorCalls) == 0 {
		t.Fatal("expected low-risk read tool to execute without approval token")
	}
}

func TestSpawnThread_HighRiskToolBlockedWithOutOfScopeToken(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("policy-gate-scope", "/tmp", "claude-haiku-4")
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Worker", ModelID: "claude-haiku-4", LocalTools: []string{"github_delete_issue"}})

	var mu sync.Mutex
	var executorCalls []string
	var toolDonePayload map[string]any
	tm.SetToolExecutor(func(_ context.Context, name string, _ map[string]any) (string, error) {
		mu.Lock()
		executorCalls = append(executorCalls, name)
		mu.Unlock()
		return "ok", nil
	})

	thread, err := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Worker", Task: "delete issue"})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}

	proposal, err := tm.proposalRegistry.Submit(ProposalRequest{
		SessionID: sess.ID,
		ThreadID:  thread.ID,
		AgentID:   "Worker",
		Provider:  "github",
		Action:    "update_issue",
		Risk:      RiskCritical,
	})
	if err != nil {
		t.Fatalf("submit proposal: %v", err)
	}
	token, err := tm.proposalRegistry.Approve(proposal.ID, "Lead", 10*time.Minute)
	if err != nil {
		t.Fatalf("approve proposal: %v", err)
	}

	backend := &scriptedToolBackend{
		toolName: "github_delete_issue",
		args: map[string]any{
			"issue":           456,
			"_approval_token": token.Token,
		},
	}

	done := make(chan struct{})
	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, backend, func(_ string, msgType string, payload map[string]any) {
		if msgType == "thread_tool_done" {
			toolDonePayload = payload
		}
		if msgType == "thread_done" {
			close(done)
		}
	}, NewCostAccumulator(0), nil)

	waitDone(t, done)

	mu.Lock()
	defer mu.Unlock()
	if len(executorCalls) != 0 {
		t.Fatalf("expected out-of-scope token to block execution, got calls: %v", executorCalls)
	}
	if toolDonePayload == nil {
		t.Fatal("expected thread_tool_done payload")
	}
	summary, _ := toolDonePayload["result_summary"].(string)
	if !strings.Contains(summary, "scope mismatch") {
		t.Fatalf("expected scope mismatch denial summary, got %q", summary)
	}
}

func TestSpawnThread_HighRiskToolBroadcastsPermissionDenied(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("policy-gate-broadcast", "/tmp", "claude-haiku-4")
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Worker", ModelID: "claude-haiku-4", LocalTools: []string{"github_delete_issue"}})

	tm.SetToolExecutor(func(_ context.Context, name string, _ map[string]any) (string, error) {
		return "ok", nil
	})

	thread, err := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Worker", Task: "delete an issue"})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	backend := &scriptedToolBackend{toolName: "github_delete_issue", args: map[string]any{"issue": 123}}

	var mu sync.Mutex
	var deniedEvents []map[string]any
	done := make(chan struct{})
	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, backend, func(_ string, msgType string, payload map[string]any) {
		if msgType == "thread_permission_denied" {
			mu.Lock()
			deniedEvents = append(deniedEvents, payload)
			mu.Unlock()
		}
		if msgType == "thread_done" {
			close(done)
		}
	}, NewCostAccumulator(0), nil)

	waitDone(t, done)

	mu.Lock()
	defer mu.Unlock()
	if len(deniedEvents) != 1 {
		t.Fatalf("expected exactly 1 thread_permission_denied event, got %d", len(deniedEvents))
	}
	tool, _ := deniedEvents[0]["tool"].(string)
	if tool != "github_delete_issue" {
		t.Fatalf("expected denied tool to be github_delete_issue, got %q", tool)
	}
	if deniedEvents[0]["thread_id"] == nil {
		t.Fatal("expected thread_id in thread_permission_denied payload")
	}
	if deniedEvents[0]["agent_id"] == nil {
		t.Fatal("expected agent_id in thread_permission_denied payload")
	}
}

func TestSpawnThread_HighRiskToolDedupPermissionDeniedBroadcast(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("policy-gate-dedup", "/tmp", "claude-haiku-4")
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Worker", ModelID: "claude-haiku-4", LocalTools: []string{"github_delete_issue"}})

	tm.SetToolExecutor(func(_ context.Context, name string, _ map[string]any) (string, error) {
		return "ok", nil
	})

	thread, err := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Worker", Task: "delete issues"})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	// Backend that returns the same denied tool twice in a single turn response.
	backend := &scriptedDualToolBackend{toolName: "github_delete_issue"}

	var mu sync.Mutex
	var deniedEvents []map[string]any
	done := make(chan struct{})
	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, backend, func(_ string, msgType string, payload map[string]any) {
		if msgType == "thread_permission_denied" {
			mu.Lock()
			deniedEvents = append(deniedEvents, payload)
			mu.Unlock()
		}
		if msgType == "thread_done" {
			close(done)
		}
	}, NewCostAccumulator(0), nil)

	waitDone(t, done)

	mu.Lock()
	defer mu.Unlock()
	// Even though the same tool was denied twice in one turn, only one broadcast should fire.
	if len(deniedEvents) != 1 {
		t.Fatalf("expected dedup to emit exactly 1 thread_permission_denied event, got %d", len(deniedEvents))
	}
}

// scriptedDualToolBackend returns two calls to the same high-risk tool on the
// first turn, then a finish on the second — used to exercise dedup logic.
type scriptedDualToolBackend struct {
	toolName string
	calls    int
}

func (b *scriptedDualToolBackend) ChatCompletion(_ context.Context, _ backend.ChatRequest) (*backend.ChatResponse, error) {
	b.calls++
	if b.calls == 1 {
		return &backend.ChatResponse{
			DoneReason: "tool_use",
			ToolCalls: []backend.ToolCall{
				{
					ID: "tool-a",
					Function: backend.ToolCallFunction{
						Name:      b.toolName,
						Arguments: map[string]any{"issue": 1},
					},
				},
				{
					ID: "tool-b",
					Function: backend.ToolCallFunction{
						Name:      b.toolName,
						Arguments: map[string]any{"issue": 2},
					},
				},
			},
		}, nil
	}
	return &backend.ChatResponse{
		DoneReason: "tool_use",
		ToolCalls: []backend.ToolCall{{
			ID: "finish-1",
			Function: backend.ToolCallFunction{
				Name:      "finish",
				Arguments: map[string]any{"summary": "done", "status": "completed"},
			},
		}},
	}, nil
}

func (b *scriptedDualToolBackend) Health(_ context.Context) error   { return nil }
func (b *scriptedDualToolBackend) Shutdown(_ context.Context) error { return nil }
func (b *scriptedDualToolBackend) ContextWindow() int               { return 8192 }

func TestDelegatedToolRisk(t *testing.T) {
	provider, action, highRisk := delegatedToolRisk("github_delete_issue")
	if !highRisk || provider != "github" || action != "delete_issue" {
		t.Fatalf("expected github_delete_issue to be high risk, got provider=%q action=%q highRisk=%v", provider, action, highRisk)
	}
	if _, _, highRisk = delegatedToolRisk("github_list_repos"); highRisk {
		t.Fatal("expected github_list_repos to be low risk")
	}
	if _, _, highRisk = delegatedToolRisk("custom_tool"); highRisk {
		t.Fatal("expected unrecognized provider tool to be low risk")
	}
}

type scriptedToolBackend struct {
	toolName string
	args     map[string]any
	calls    int
}

func (b *scriptedToolBackend) ChatCompletion(_ context.Context, _ backend.ChatRequest) (*backend.ChatResponse, error) {
	b.calls++
	if b.calls == 1 {
		args := map[string]any{}
		for k, v := range b.args {
			args[k] = v
		}
		return &backend.ChatResponse{
			DoneReason: "tool_use",
			ToolCalls: []backend.ToolCall{{
				ID: "tool-1",
				Function: backend.ToolCallFunction{
					Name:      b.toolName,
					Arguments: args,
				},
			}},
		}, nil
	}
	return &backend.ChatResponse{
		DoneReason: "tool_use",
		ToolCalls: []backend.ToolCall{{
			ID: "finish-1",
			Function: backend.ToolCallFunction{
				Name:      "finish",
				Arguments: map[string]any{"summary": "done", "status": "completed"},
			},
		}},
	}, nil
}

func (b *scriptedToolBackend) Health(_ context.Context) error   { return nil }
func (b *scriptedToolBackend) Shutdown(_ context.Context) error { return nil }
func (b *scriptedToolBackend) ContextWindow() int               { return 8192 }

func waitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for thread completion")
	}
}
