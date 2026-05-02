package agent

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/threadmgr"
)

// finishBackend is a minimal backend stub that immediately returns a finish()
// tool call, causing SpawnThread to transition its thread to StatusDone.
type finishBackend struct{}

func (f *finishBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	return &backend.ChatResponse{
		ToolCalls: []backend.ToolCall{
			{
				ID: "finish-1",
				Function: backend.ToolCallFunction{
					Name:      "finish",
					Arguments: map[string]any{"summary": "done", "status": "completed"},
				},
			},
		},
		DoneReason:       "tool_calls",
		PromptTokens:     10,
		CompletionTokens: 5,
	}, nil
}
func (f *finishBackend) Health(_ context.Context) error   { return nil }
func (f *finishBackend) Shutdown(_ context.Context) error { return nil }
func (f *finishBackend) ContextWindow() int               { return 8192 }

// TestSpawnThread_DAGFnUnblocksDownstreamThread verifies that after SpawnThread
// completes t1, the dagFn closure fires EvaluateDAG, which then spawns t2
// (which DependsOn t1). Without the fix (nil dagFn), t2 would remain
// StatusQueued forever.
func TestSpawnThread_DAGFnUnblocksDownstreamThread(t *testing.T) {
	fb := &finishBackend{}

	// Build an Orchestrator with only the fields SpawnThread reads under RLock.
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:    "Worker",
		ModelID: "claude-haiku-4",
	})

	store := session.NewStore(t.TempDir())
	sess := store.New("dag-swarm-test", "/tmp", "claude-haiku-4")

	orch := &Orchestrator{
		sessions:         map[string]*Session{},
		defaultSessionID: "",
		agentReg:         reg,
		sessionStore:     store,
		backend:          fb,
	}

	tm := threadmgr.New()

	t1, err := tm.Create(threadmgr.CreateParams{
		SessionID: sess.ID,
		AgentID:   "Worker",
		Task:      "upstream task",
	})
	if err != nil {
		t.Fatalf("create t1: %v", err)
	}

	t2, err := tm.Create(threadmgr.CreateParams{
		SessionID: sess.ID,
		AgentID:   "Worker",
		Task:      "downstream task",
		DependsOn: []string{t1.ID},
	})
	if err != nil {
		t.Fatalf("create t2: %v", err)
	}

	broadcast := func(_, _ string, _ map[string]any) {}
	ca := threadmgr.NewCostAccumulator(0)

	// Spawn t1 via the orchestrator. With the bug, dagFn is nil and t2 stays
	// StatusQueued. With the fix, dagFn calls EvaluateDAG and t2 is spawned.
	orch.SpawnThread(context.Background(), t1.ID, sess, tm, broadcast, nil, ca)

	// Wait for t2 to leave StatusQueued (it should be spawned → run → done).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		thread2, ok := tm.Get(t2.ID)
		if ok && thread2.Status != threadmgr.StatusQueued {
			// t2 was unblocked — verify it eventually reaches StatusDone.
			for time.Now().Before(deadline) {
				thread2, _ = tm.Get(t2.ID)
				if thread2 != nil && thread2.Status == threadmgr.StatusDone {
					return // success
				}
				time.Sleep(20 * time.Millisecond)
			}
			t2final, _ := tm.Get(t2.ID)
			t.Errorf("t2 left StatusQueued but did not reach StatusDone: got %v", t2final.Status)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t2final, _ := tm.Get(t2.ID)
	t.Errorf("t2 never left StatusQueued after t1 completed (dagFn not wired); t2.Status=%v", t2final.Status)
}
