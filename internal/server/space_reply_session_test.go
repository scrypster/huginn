package server

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agent"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/spaces"
	"github.com/scrypster/huginn/internal/threadmgr"
	"github.com/scrypster/huginn/internal/tools"
)

func TestSpaceThreadToolSession_UsesHallwayWhenPresent(t *testing.T) {
	db := openTestSQLiteDB(t)
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatal(err)
	}
	spaceStore := spaces.NewSQLiteSpaceStore(db)
	sessStore := session.NewSQLiteSessionStore(db)
	srv := testServer(t)
	srv.store = sessStore
	srv.SetSpaceStore(spaceStore)

	dm, err := spaceStore.OpenDM("Steve")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := sessStore.SaveManifest(&session.Session{
		ID: "85ff-hallway",
		Manifest: session.Manifest{
			ID: "85ff-hallway", SessionID: "85ff-hallway", SpaceID: dm.ID,
			Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now,
		},
	}); err != nil {
		t.Fatal(err)
	}
	got := srv.spaceThreadToolSession(dm.ID, "parent1", "Steve")
	if got != "85ff-hallway" {
		t.Fatalf("tool session = %q, want hallway 85ff-hallway", got)
	}
}

func TestSpaceThreadToolSession_FallbackPersistsWithSpaceID(t *testing.T) {
	srv, store, ch := spaceReplyServer(t)
	sid := srv.spaceThreadToolSession(ch.ID, "parent9", "Steve")
	if !strings.HasPrefix(sid, "space-thread-parent9-") {
		t.Fatalf("fallback id = %q", sid)
	}
	srv.ensureDelegateSession(sid, ch.ID)
	loaded, err := srv.store.Load(sid)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SpaceID() != ch.ID {
		t.Fatalf("persisted space = %q want %q", loaded.SpaceID(), ch.ID)
	}
	_ = store
}

type captureSIDTool struct {
	mu  sync.Mutex
	sid string
}

func (t *captureSIDTool) Name() string                      { return "capture_session" }
func (t *captureSIDTool) Description() string               { return "capture session" }
func (t *captureSIDTool) Permission() tools.PermissionLevel { return tools.PermRead }
func (t *captureSIDTool) Schema() backend.Tool {
	return backend.Tool{Function: backend.ToolFunction{Name: t.Name()}}
}
func (t *captureSIDTool) Execute(ctx context.Context, _ map[string]any) tools.ToolResult {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sid = agent.GetSessionID(ctx)
	return tools.ToolResult{Output: t.sid}
}

type scriptedBackend struct {
	responses []*backend.ChatResponse
	n         int
}

func (b *scriptedBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	idx := b.n
	b.n++
	if idx < len(b.responses) {
		return b.responses[idx], nil
	}
	return &backend.ChatResponse{Content: "done", DoneReason: "stop"}, nil
}
func (b *scriptedBackend) Health(_ context.Context) error   { return nil }
func (b *scriptedBackend) Shutdown(_ context.Context) error { return nil }
func (b *scriptedBackend) ContextWindow() int               { return 4096 }

func TestRunSpaceThreadAgent_ToolContextHasSession(t *testing.T) {
	db := openTestSQLiteDB(t)
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatal(err)
	}
	spaceStore := spaces.NewSQLiteSpaceStore(db)
	sessStore := session.NewSQLiteSessionStore(db)
	dm, err := spaceStore.OpenDM("Steve")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := sessStore.SaveManifest(&session.Session{
		ID: "steve-hallway",
		Manifest: session.Manifest{
			ID: "steve-hallway", SessionID: "steve-hallway", SpaceID: dm.ID,
			Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now,
		},
	}); err != nil {
		t.Fatal(err)
	}

	cap := &captureSIDTool{}
	reg := tools.NewRegistry()
	reg.Register(cap)
	b := &scriptedBackend{responses: []*backend.ChatResponse{
		{
			DoneReason: "tool_calls",
			ToolCalls: []backend.ToolCall{{
				ID: "c1",
				Function: backend.ToolCallFunction{Name: "capture_session", Arguments: map[string]any{}},
			}},
		},
		{Content: "ok", DoneReason: "stop"},
	}}
	orch, err := agent.NewOrchestrator(b, modelconfig.DefaultModels(), nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	orch.SetTools(reg, permissions.NewGate(true, nil))
	agReg := agents.NewRegistry()
	agReg.Register(&agents.Agent{Name: "Steve", ModelID: "test-model", LocalTools: []string{"capture_session"}})
	orch.SetAgentRegistry(agReg)

	srv := testServer(t)
	srv.orch = orch
	srv.store = sessStore
	srv.SetSpaceStore(spaceStore)

	root, err := spaceStore.PostSpaceMessage(dm.ID, "root", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.RunSpaceThreadAgent(context.Background(), dm.ID, root.ID, "Steve", "ask Winston the time"); err != nil {
		t.Fatalf("RunSpaceThreadAgent: %v", err)
	}
	cap.mu.Lock()
	defer cap.mu.Unlock()
	if cap.sid != "steve-hallway" {
		t.Fatalf("space-thread tool session = %q, want steve-hallway", cap.sid)
	}
}

func TestSteveDeskDM_DelegateWinston_ToolLayer(t *testing.T) {
	db := openTestSQLiteDB(t)
	if err := db.Migrate(spaces.Migrations()); err != nil {
		t.Fatal(err)
	}
	spaceStore := spaces.NewSQLiteSpaceStore(db)
	steveDM, err := spaceStore.OpenDM("Steve")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := spaceStore.OpenDM("Winston"); err != nil {
		t.Fatal(err)
	}

	sessStore := session.NewStore(t.TempDir())
	now := time.Now().UTC()
	if err := sessStore.SaveManifest(&session.Session{
		ID: "steve-dm",
		Manifest: session.Manifest{
			ID: "steve-dm", SessionID: "steve-dm", SpaceID: steveDM.ID,
			Status: "active", Version: 1, CreatedAt: now, UpdatedAt: now,
		},
	}); err != nil {
		t.Fatal(err)
	}

	loaded, err := session.LoadForDelegate(sessStore, "steve-dm", steveDM.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SpaceID() != steveDM.ID {
		t.Fatalf("space = %q", loaded.SpaceID())
	}

	tm := threadmgr.New()
	tm.SetMembershipChecker(spaceStore)
	tm.SetCompanyGate(spaceStore)
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Steve", ModelID: "test-model"})
	reg.Register(&agents.Agent{Name: "Winston", ModelID: "test-model"})
	tool := &threadmgr.DelegateToAgentTool{
		Fn: func(_ context.Context, p threadmgr.DelegateParams) threadmgr.DelegateResult {
			if _, found := reg.ByName(p.AgentName); !found {
				return threadmgr.DelegateResult{Err: err}
			}
			th, createErr := tm.Create(threadmgr.CreateParams{
				SessionID: loaded.ID,
				AgentID:   p.AgentName,
				Task:      p.Task,
				SpaceID:   loaded.SpaceID(),
			})
			if createErr != nil {
				return threadmgr.DelegateResult{Err: createErr}
			}
			return threadmgr.DelegateResult{ThreadID: th.ID, Spawned: true}
		},
	}
	result := tool.Execute(context.Background(), map[string]any{
		"agent": "Winston",
		"task":  "what time is it",
	})
	if result.IsError {
		t.Fatalf("Steve desk DM → Winston must succeed, got: %s", result.Error)
	}
	threads := tm.ListBySession("steve-dm")
	if len(threads) != 1 || threads[0].AgentID != "Winston" {
		t.Fatalf("threads = %+v", threads)
	}
}

func TestDelegateLoad_NoSessionID_NoStub(t *testing.T) {
	store := session.NewStore(t.TempDir())
	sess, err := session.LoadForDelegate(store, "", "desk")
	if err == nil || sess != nil {
		t.Fatal("empty session ID must not stub")
	}
	if !strings.Contains(err.Error(), "no session ID in context") {
		t.Fatalf("err = %v", err)
	}
}
