package threadmgr

// threadmgr_coverage_test.go — additional tests to push internal/threadmgr coverage to 90%+.
// Covers: Permission(), Execute() missing-fn / conflicts / spawned / queued paths,
// estimateTokens zero-case, buildPersonaContent branches, buildArtifactMessages budget
// overflow, buildSnapshotMessages nil/error/overflow, formatFinishSummary branches,
// ParseMentions dedup, CreateFromMentions, manager Complete idempotent/cancelled,
// manager Start not-found, IsReady not-found, ResolveDependencies edge cases,
// SpawnThread double-start race, runOnce LLM-error / budget-exceeded / unknown-tool
// / context-cancel / length-done / max-turns paths.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/tools"
)

// ---------------------------------------------------------------------------
// DelegateToAgentTool.Permission — was 0% covered
// ---------------------------------------------------------------------------

func TestDelegateToAgentTool_Permission(t *testing.T) {
	d := &DelegateToAgentTool{}
	if d.Permission() != tools.PermExec {
		t.Errorf("expected PermExec, got %v", d.Permission())
	}
}

// ---------------------------------------------------------------------------
// DelegateToAgentTool.Execute — nil Fn path
// ---------------------------------------------------------------------------

func TestDelegateToAgentTool_Execute_NilFn(t *testing.T) {
	d := &DelegateToAgentTool{Fn: nil}
	result := d.Execute(context.Background(), map[string]any{"agent": "Stacy", "task": "do it"})
	if !result.IsError {
		t.Error("expected error when Fn is nil")
	}
	if !strings.Contains(result.Error, "not configured") {
		t.Errorf("expected 'not configured' in error, got %q", result.Error)
	}
}

// ---------------------------------------------------------------------------
// DelegateToAgentTool.Execute — task argument missing
// ---------------------------------------------------------------------------

func TestDelegateToAgentTool_Execute_MissingTask(t *testing.T) {
	d := &DelegateToAgentTool{
		Fn: func(_ context.Context, _ DelegateParams) DelegateResult {
			return DelegateResult{ThreadID: "t-1"}
		},
	}
	result := d.Execute(context.Background(), map[string]any{"agent": "Stacy"})
	if !result.IsError {
		t.Error("expected error for missing task")
	}
}

// ---------------------------------------------------------------------------
// DelegateToAgentTool.Execute — Fn returns error
// ---------------------------------------------------------------------------

func TestDelegateToAgentTool_Execute_FnError(t *testing.T) {
	d := &DelegateToAgentTool{
		Fn: func(_ context.Context, _ DelegateParams) DelegateResult {
			return DelegateResult{Err: fmt.Errorf("agent not found")}
		},
	}
	result := d.Execute(context.Background(), map[string]any{"agent": "X", "task": "do it"})
	if !result.IsError {
		t.Error("expected error from Fn error")
	}
}

// ---------------------------------------------------------------------------
// DelegateToAgentTool.Execute — Fn returns conflicts
// ---------------------------------------------------------------------------

func TestDelegateToAgentTool_Execute_Conflicts(t *testing.T) {
	d := &DelegateToAgentTool{
		Fn: func(_ context.Context, _ DelegateParams) DelegateResult {
			return DelegateResult{
				ThreadID:  "t-1",
				Conflicts: []string{"auth.go"},
			}
		},
	}
	result := d.Execute(context.Background(), map[string]any{"agent": "Stacy", "task": "fix auth"})
	if !result.IsError {
		t.Error("expected error for file conflicts")
	}
	if !strings.Contains(result.Error, "conflict") {
		t.Errorf("expected 'conflict' in error, got %q", result.Error)
	}
}

// ---------------------------------------------------------------------------
// DelegateToAgentTool.Execute — Fn returns Spawned=true
// ---------------------------------------------------------------------------

func TestDelegateToAgentTool_Execute_Spawned(t *testing.T) {
	d := &DelegateToAgentTool{
		Fn: func(_ context.Context, _ DelegateParams) DelegateResult {
			return DelegateResult{ThreadID: "t-99", Spawned: true}
		},
	}
	result := d.Execute(context.Background(), map[string]any{"agent": "Stacy", "task": "go"})
	if result.IsError {
		t.Errorf("expected success, got error: %s", result.Error)
	}
	status, _ := result.Metadata["status"].(string)
	if status != "spawned" {
		t.Errorf("expected status=spawned, got %q", status)
	}
}

// ---------------------------------------------------------------------------
// DelegateToAgentTool.Execute — with depends_on and file_intents
// ---------------------------------------------------------------------------

func TestDelegateToAgentTool_Execute_WithDependsOnAndFileIntents(t *testing.T) {
	var captured DelegateParams
	d := &DelegateToAgentTool{
		Fn: func(_ context.Context, p DelegateParams) DelegateResult {
			captured = p
			return DelegateResult{ThreadID: "t-1"}
		},
	}
	result := d.Execute(context.Background(), map[string]any{
		"agent":        "Stacy",
		"task":         "refactor",
		"depends_on":   []any{"Alice", ""},    // empty string should be skipped
		"file_intents": []any{"main.go", ""}, // empty string should be skipped
	})
	if result.IsError {
		t.Errorf("unexpected error: %s", result.Error)
	}
	if len(captured.DependsOn) != 1 || captured.DependsOn[0] != "Alice" {
		t.Errorf("expected DependsOn=[Alice], got %v", captured.DependsOn)
	}
	if len(captured.FileIntents) != 1 || captured.FileIntents[0] != "main.go" {
		t.Errorf("expected FileIntents=[main.go], got %v", captured.FileIntents)
	}
}

// ---------------------------------------------------------------------------
// estimateTokens — zero-length path
// ---------------------------------------------------------------------------

func TestEstimateTokens_Empty(t *testing.T) {
	if estimateTokens("") != 0 {
		t.Error("expected 0 for empty string")
	}
}

func TestEstimateTokens_ExactMultiple(t *testing.T) {
	// "abcd" = 4 chars → 1 token
	got := estimateTokens("abcd")
	if got != 1 {
		t.Errorf("expected 1 token for 4-char string, got %d", got)
	}
}

// ---------------------------------------------------------------------------
// buildPersonaContent — all branches
// ---------------------------------------------------------------------------

func TestBuildPersonaContent_NilRegistry(t *testing.T) {
	thread := &Thread{AgentID: "Bob", Task: "do stuff"}
	got := buildPersonaContent(thread, nil)
	if !strings.Contains(got, "do stuff") {
		t.Errorf("expected task in persona content, got %q", got)
	}
}

func TestBuildPersonaContent_AgentNotFound(t *testing.T) {
	thread := &Thread{AgentID: "Unknown", Task: "mystery task"}
	reg := agents.NewRegistry()
	// Registry has no agent named "Unknown"
	got := buildPersonaContent(thread, reg)
	if !strings.Contains(got, "Unknown") {
		t.Errorf("expected agent name in persona content, got %q", got)
	}
	if !strings.Contains(got, "mystery task") {
		t.Errorf("expected task in persona content, got %q", got)
	}
}

func TestBuildPersonaContent_AgentFound(t *testing.T) {
	thread := &Thread{AgentID: "Coder", Task: "implement auth"}
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:         "Coder",
		SystemPrompt: "You are Coder, an expert Go developer.",
		ModelID:      "claude-haiku-4",
	})
	got := buildPersonaContent(thread, reg)
	if !strings.Contains(got, "implement auth") {
		t.Errorf("expected task in output, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// buildArtifactMessages — budget overflow path
// ---------------------------------------------------------------------------

func TestBuildArtifactMessages_BudgetOverflow(t *testing.T) {
	tm := New()
	store := makeTestStore(t)

	upstream, _ := tm.Create(CreateParams{
		SessionID: "s1",
		AgentID:   "Worker",
		Task:      "big task",
	})
	// Set a very large summary so it exceeds any budget
	bigSummary := strings.Repeat("x", 10000)
	tm.Complete(upstream.ID, FinishSummary{
		Summary: bigSummary,
		Status:  "completed",
	})

	downstream, _ := tm.Create(CreateParams{
		SessionID: "s1",
		AgentID:   "Consumer",
		Task:      "use it",
		DependsOn: []string{upstream.ID},
	})

	_ = store

	// Budget so small the artifact won't fit.
	msgs := buildArtifactMessages(downstream, tm, 5)
	if len(msgs) != 0 {
		t.Errorf("expected 0 artifact messages when budget exceeded, got %d", len(msgs))
	}
}

// ---------------------------------------------------------------------------
// buildArtifactMessages — dep has no summary
// ---------------------------------------------------------------------------

func TestBuildArtifactMessages_DepNoSummary(t *testing.T) {
	tm := New()

	upstream, _ := tm.Create(CreateParams{SessionID: "s2", AgentID: "Dep", Task: "dep"})
	// Do NOT complete upstream — no summary

	downstream, _ := tm.Create(CreateParams{
		SessionID: "s2",
		AgentID:   "Main",
		Task:      "use dep",
		DependsOn: []string{upstream.ID},
	})

	msgs := buildArtifactMessages(downstream, tm, 4096)
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages for dep with no summary, got %d", len(msgs))
	}
}

// ---------------------------------------------------------------------------
// buildSnapshotMessages — nil store, zero budget, store error
// ---------------------------------------------------------------------------

func TestBuildSnapshotMessages_NilStore(t *testing.T) {
	msgs := buildSnapshotMessages("sess", nil, 4096)
	if msgs != nil {
		t.Errorf("expected nil for nil store, got %v", msgs)
	}
}

func TestBuildSnapshotMessages_ZeroBudget(t *testing.T) {
	store := makeTestStore(t)
	msgs := buildSnapshotMessages("sess", store, 0)
	if msgs != nil {
		t.Errorf("expected nil for zero budget, got %v", msgs)
	}
}

func TestBuildSnapshotMessages_EmptySession(t *testing.T) {
	store := makeTestStore(t)
	// Session has no messages
	msgs := buildSnapshotMessages("empty-session", store, 4096)
	if len(msgs) != 0 {
		t.Errorf("expected 0 messages for empty session, got %d", len(msgs))
	}
}

func TestBuildSnapshotMessages_BudgetExceededDropsOldest(t *testing.T) {
	store := makeTestStore(t)
	sess := store.New("test", "/tmp", "model")

	// Add messages that together exceed budget
	for i := 0; i < 5; i++ {
		_ = store.Append(sess, session.SessionMessage{
			Role:    "user",
			Content: fmt.Sprintf("message %d %s", i, strings.Repeat("a", 200)),
		})
	}

	// Very tight budget — should only include the most recent messages.
	msgs := buildSnapshotMessages(sess.ID, store, 50)
	// We don't care exactly how many, just that it's less than 5.
	if len(msgs) >= 5 {
		t.Errorf("expected budget trimming, got all %d messages", len(msgs))
	}
}

// ---------------------------------------------------------------------------
// formatFinishSummary — all fields present
// ---------------------------------------------------------------------------

func TestFormatFinishSummary_AllFields(t *testing.T) {
	fs := &FinishSummary{
		Summary:       "completed the task",
		FilesModified: []string{"a.go", "b.go"},
		KeyDecisions:  []string{"use JWT", "stateless"},
		Artifacts:     []string{"output.json"},
		Status:        "completed",
	}
	got := formatFinishSummary("Coder", fs)
	if !strings.Contains(got, "a.go") {
		t.Error("expected FilesModified in output")
	}
	if !strings.Contains(got, "JWT") {
		t.Error("expected KeyDecisions in output")
	}
	if !strings.Contains(got, "output.json") {
		t.Error("expected Artifacts in output")
	}
	if !strings.Contains(got, "completed") {
		t.Error("expected Status in output")
	}
}

func TestFormatFinishSummary_MinimalFields(t *testing.T) {
	fs := &FinishSummary{
		Summary: "minimal",
		Status:  "done",
	}
	got := formatFinishSummary("Agent", fs)
	if !strings.Contains(got, "minimal") {
		t.Error("expected summary in output")
	}
}

// ---------------------------------------------------------------------------
// ParseMentions — dedup path (same agent mentioned twice)
// ---------------------------------------------------------------------------

func TestParseMentions_DuplicateMentionDeduped(t *testing.T) {
	requests := ParseMentions("@Stacy and @stacy again", []string{"Stacy"})
	if len(requests) != 1 {
		t.Errorf("expected 1 deduplicated request, got %d", len(requests))
	}
}

func TestParseMentions_EmptyAgentNames(t *testing.T) {
	requests := ParseMentions("@Stacy do it", []string{})
	if len(requests) != 0 {
		t.Errorf("expected 0 requests with empty agent names, got %d", len(requests))
	}
}

// ---------------------------------------------------------------------------
// CreateFromMentions — covered via integration call
// ---------------------------------------------------------------------------

func TestCreateFromMentions_SpawnsThread(t *testing.T) {
	tm := New()
	store := makeTestStore(t)
	sess := store.New("test", "/tmp", "claude-haiku-4")

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:    "Coder",
		ModelID: "claude-haiku-4",
	})

	// A non-blocking backend for the spawned thread.
	fb := &fakeBackend{
		response: &backend.ChatResponse{
			ToolCalls: []backend.ToolCall{
				{
					ID: "tc-1",
					Function: backend.ToolCallFunction{
						Name:      "finish",
						Arguments: map[string]any{"summary": "done", "status": "completed"},
					},
				},
			},
			DoneReason: "tool_calls",
		},
	}

	var broadcastCalls []string
	var bmu sync.Mutex
	broadcast := func(_, msgType string, _ map[string]any) {
		bmu.Lock()
		broadcastCalls = append(broadcastCalls, msgType)
		bmu.Unlock()
	}

	ca := NewCostAccumulator(0)
	CreateFromMentions(
		context.Background(),
		sess.ID,
		"@Coder please implement auth",
		"", // parentMsgID
		reg,
		store,
		sess,
		fb,
		broadcast,
		ca,
		tm,
	)

	// Wait a bit for the spawned goroutine to complete.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		threads := tm.ListBySession(sess.ID)
		if len(threads) > 0 && threads[0].Status == StatusDone {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	threads := tm.ListBySession(sess.ID)
	if len(threads) == 0 {
		t.Fatal("expected a thread to be created")
	}
	if threads[0].AgentID != "Coder" {
		t.Errorf("expected AgentID=Coder, got %q", threads[0].AgentID)
	}
}

func TestCreateFromMentions_UnknownAgent_NoThread(t *testing.T) {
	tm := New()
	store := makeTestStore(t)
	sess := store.New("test", "/tmp", "model")
	reg := agents.NewRegistry()
	// No agents registered.

	broadcast := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	CreateFromMentions(
		context.Background(),
		sess.ID,
		"@Nobody do something",
		"", // parentMsgID
		reg,
		store,
		sess,
		&fakeBackend{},
		broadcast,
		ca,
		tm,
	)

	threads := tm.ListBySession(sess.ID)
	if len(threads) != 0 {
		t.Errorf("expected 0 threads for unknown agent, got %d", len(threads))
	}
}

func TestCreateFromMentions_ThreadLimitExceeded_Skips(t *testing.T) {
	tm := New()
	tm.MaxThreadsPerSession = 1
	store := makeTestStore(t)
	sess := store.New("test", "/tmp", "model")

	reg := agents.NewRegistry()

	// Fill the session limit first.
	_, _ = tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Filler", Task: "fill"})

	broadcast := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	// @Coder would hit the limit.
	CreateFromMentions(
		context.Background(),
		sess.ID,
		"@Coder fix auth",
		"", // parentMsgID
		reg,
		store,
		sess,
		&fakeBackend{},
		broadcast,
		ca,
		tm,
	)

	// Coder thread should NOT have been created (limit exceeded).
	threads := tm.ListBySession(sess.ID)
	for _, th := range threads {
		if th.AgentID == "Coder" {
			t.Error("Coder thread should not be created when limit is exceeded")
		}
	}
}

// ---------------------------------------------------------------------------
// ThreadManager.Complete — idempotent (calling on already-cancelled thread)
// ---------------------------------------------------------------------------

func TestComplete_IdempotentOnCancelledThread(t *testing.T) {
	tm := New()
	thread, _ := tm.Create(CreateParams{SessionID: "s", AgentID: "a", Task: "t"})

	ctx, cancel := context.WithCancel(context.Background())
	tm.Start(thread.ID, ctx, cancel)
	tm.Cancel(thread.ID)

	// Complete on an already-cancelled thread should be a no-op.
	tm.Complete(thread.ID, FinishSummary{Summary: "late finish", Status: "completed"})

	got, _ := tm.Get(thread.ID)
	if got.Status != StatusCancelled {
		t.Errorf("expected StatusCancelled to persist, got %s", got.Status)
	}
}

// ---------------------------------------------------------------------------
// ThreadManager.Start — thread not found
// ---------------------------------------------------------------------------

func TestStart_ThreadNotFound(t *testing.T) {
	tm := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ok := tm.Start("nonexistent", ctx, cancel)
	if ok {
		t.Error("expected false for nonexistent thread")
	}
}

// ---------------------------------------------------------------------------
// ThreadManager.IsReady — thread not found
// ---------------------------------------------------------------------------

func TestIsReady_ThreadNotFound(t *testing.T) {
	tm := New()
	if tm.IsReady("nonexistent") {
		t.Error("expected false for nonexistent thread")
	}
}

// ---------------------------------------------------------------------------
// ThreadManager.ResolveDependencies — thread not found
// ---------------------------------------------------------------------------

func TestResolveDependencies_ThreadNotFound(t *testing.T) {
	tm := New()
	got := tm.ResolveDependencies("nonexistent")
	if got != nil {
		t.Errorf("expected nil for nonexistent thread, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// ThreadManager.ResolveDependencies — no hints (returns existing deps)
// ---------------------------------------------------------------------------

func TestResolveDependencies_NoHints(t *testing.T) {
	tm := New()
	thread, _ := tm.Create(CreateParams{
		SessionID: "s",
		AgentID:   "a",
		Task:      "t",
		DependsOn: []string{"dep-1"},
	})

	got := tm.ResolveDependencies(thread.ID)
	if len(got) != 1 || got[0] != "dep-1" {
		t.Errorf("expected [dep-1], got %v", got)
	}
}

// ---------------------------------------------------------------------------
// SpawnThread — double-start race (second call should be no-op)
// ---------------------------------------------------------------------------

func TestSpawnThread_DoubleStart_SecondIsNoOp(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "m")
	reg := agents.NewRegistry()

	// Backend that blocks so we can call SpawnThread twice before it finishes.
	finishCh := make(chan struct{})
	fb := &sequencedBackend{
		responses: []*backend.ChatResponse{
			{
				ToolCalls: []backend.ToolCall{
					{
						ID: "tc-1",
						Function: backend.ToolCallFunction{
							Name:      "finish",
							Arguments: map[string]any{"summary": "done", "status": "completed"},
						},
					},
				},
				DoneReason: "tool_calls",
			},
		},
		afterFirst: finishCh,
	}

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "x"})
	broadcast := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, fb, broadcast, ca, nil)
	// Second call — thread is no longer StatusQueued so Start() should fail.
	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, fb, broadcast, ca, nil)

	// Signal the backend to proceed.
	close(finishCh)

	// Wait for completion.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := tm.Get(thread.ID)
		if got != nil && got.Status == StatusDone {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	got, _ := tm.Get(thread.ID)
	if got == nil || got.Status != StatusDone {
		t.Errorf("expected StatusDone after double-spawn, got %v", got)
	}
}

// sequencedBackend is a backend that returns scripted responses and optionally
// waits on a channel before the first call returns.
type sequencedBackend struct {
	mu         sync.Mutex
	calls      int
	responses  []*backend.ChatResponse
	afterFirst chan struct{} // if non-nil, first call blocks until closed
}

func (s *sequencedBackend) ChatCompletion(ctx context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	s.mu.Lock()
	idx := s.calls
	s.calls++
	s.mu.Unlock()

	// Block on the first call if afterFirst is set.
	if idx == 0 && s.afterFirst != nil {
		select {
		case <-s.afterFirst:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if idx < len(s.responses) {
		resp := s.responses[idx]
		if req.OnToken != nil && resp.Content != "" {
			req.OnToken(resp.Content)
		}
		return resp, nil
	}
	return &backend.ChatResponse{Content: "done", DoneReason: "stop"}, nil
}
func (s *sequencedBackend) Health(_ context.Context) error   { return nil }
func (s *sequencedBackend) Shutdown(_ context.Context) error { return nil }
func (s *sequencedBackend) ContextWindow() int               { return 8192 }

// ---------------------------------------------------------------------------
// runOnce — LLM permanent error path
// ---------------------------------------------------------------------------

func TestRunOnce_LLMPermanentError(t *testing.T) {
	tm := New()
	store := makeTestStore(t)
	sess := store.New("test", "/tmp", "m")
	reg := agents.NewRegistry()

	errBackend := &alwaysErrorBackend{err: fmt.Errorf("permanent api error")}

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "work"})
	broadcast := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	ctx := context.Background()
	result := tm.runOnce(ctx, thread.ID, "", "", sess, store, reg, errBackend, broadcast, ca, nil, nil, nil, nil, nil)
	if result.kind != loopDone {
		t.Errorf("expected loopDone on LLM error, got %v", result.kind)
	}
}

// alwaysErrorBackend always returns an error (after 2 attempts due to retry).
type alwaysErrorBackend struct {
	err error
}

func (a *alwaysErrorBackend) ChatCompletion(_ context.Context, _ backend.ChatRequest) (*backend.ChatResponse, error) {
	return nil, a.err
}
func (a *alwaysErrorBackend) Health(_ context.Context) error   { return nil }
func (a *alwaysErrorBackend) Shutdown(_ context.Context) error { return nil }
func (a *alwaysErrorBackend) ContextWindow() int               { return 8192 }

// ---------------------------------------------------------------------------
// runOnce — budget exceeded path
// ---------------------------------------------------------------------------

func TestRunOnce_BudgetExceeded(t *testing.T) {
	tm := New()
	store := makeTestStore(t)
	sess := store.New("test", "/tmp", "m")
	reg := agents.NewRegistry()

	// Backend that returns a normal stop response but budget is already exceeded.
	fb := &fakeBackend{
		response: &backend.ChatResponse{Content: "response", DoneReason: "stop"},
	}

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "expensive"})

	// Set budget to 1 satoshi and immediately mark it exceeded.
	ca := NewCostAccumulator(0.000001) // 0.1 microcent budget
	// Record a cost that blows the budget.
	ca.Record("other-thread", 1000000, 1000000, "expensive-model")

	broadcast := func(_, _ string, _ map[string]any) {}
	ctx := context.Background()
	result := tm.runOnce(ctx, thread.ID, "", "", sess, store, reg, fb, broadcast, ca, nil, nil, nil, nil, nil)
	if result.kind != loopDone {
		t.Errorf("expected loopDone on budget exceeded, got %v", result.kind)
	}
}

// ---------------------------------------------------------------------------
// runOnce — unknown tool name
// ---------------------------------------------------------------------------

func TestRunOnce_UnknownTool(t *testing.T) {
	tm := New()
	store := makeTestStore(t)
	sess := store.New("test", "/tmp", "m")
	reg := agents.NewRegistry()

	// Backend: first call returns an unknown tool, second call returns stop.
	fb := &sequenceBackend{
		responses: []*backend.ChatResponse{
			{
				ToolCalls: []backend.ToolCall{
					{
						ID: "tc-unk",
						Function: backend.ToolCallFunction{
							Name:      "unknown_magic_tool",
							Arguments: map[string]any{},
						},
					},
				},
				DoneReason: "tool_calls",
			},
			{
				ToolCalls: []backend.ToolCall{
					{
						ID: "tc-fin",
						Function: backend.ToolCallFunction{
							Name:      "finish",
							Arguments: map[string]any{"summary": "done", "status": "completed"},
						},
					},
				},
				DoneReason: "tool_calls",
			},
		},
	}

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "x"})
	var broadcastTypes []string
	var bmu sync.Mutex
	broadcast := func(_, msgType string, _ map[string]any) {
		bmu.Lock()
		broadcastTypes = append(broadcastTypes, msgType)
		bmu.Unlock()
	}
	ca := NewCostAccumulator(0)

	result := tm.runOnce(context.Background(), thread.ID, "", "", sess, store, reg, fb, broadcast, ca, nil, nil, nil, nil, nil)
	if result.kind != loopDone {
		t.Errorf("expected loopDone, got %v", result.kind)
	}

	// thread_tool_done should have been broadcast for the unknown tool.
	bmu.Lock()
	defer bmu.Unlock()
	var hasToolDone bool
	for _, tp := range broadcastTypes {
		if tp == "thread_tool_done" {
			hasToolDone = true
		}
	}
	if !hasToolDone {
		t.Errorf("expected thread_tool_done broadcast for unknown tool, got: %v", broadcastTypes)
	}
}

// sequenceBackend returns scripted responses in order.
type sequenceBackend struct {
	mu    sync.Mutex
	calls int
	responses []*backend.ChatResponse
}

func (s *sequenceBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	s.mu.Lock()
	idx := s.calls
	s.calls++
	s.mu.Unlock()
	if idx < len(s.responses) {
		r := s.responses[idx]
		if req.OnToken != nil && r.Content != "" {
			req.OnToken(r.Content)
		}
		return r, nil
	}
	return &backend.ChatResponse{Content: "done", DoneReason: "stop"}, nil
}
func (s *sequenceBackend) Health(_ context.Context) error   { return nil }
func (s *sequenceBackend) Shutdown(_ context.Context) error { return nil }
func (s *sequenceBackend) ContextWindow() int               { return 8192 }

// ---------------------------------------------------------------------------
// runOnce — context cancelled during retry sleep (second attempt)
// ---------------------------------------------------------------------------

func TestRunOnce_ContextCancelledDuringRetry(t *testing.T) {
	tm := New()
	store := makeTestStore(t)
	sess := store.New("test", "/tmp", "m")
	reg := agents.NewRegistry()

	ctx, cancel := context.WithCancel(context.Background())

	// Backend that errors on every call so the retry path is hit.
	// On the second attempt the context will be cancelled.
	callCount := 0
	cancellingBackend := &callbackBackend{
		fn: func(c context.Context, _ backend.ChatRequest) (*backend.ChatResponse, error) {
			callCount++
			if callCount == 1 {
				cancel() // cancel on first error → retry will check ctx.Done()
			}
			return nil, fmt.Errorf("error %d", callCount)
		},
	}

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "x"})
	broadcast := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	result := tm.runOnce(ctx, thread.ID, "", "", sess, store, reg, cancellingBackend, broadcast, ca, nil, nil, nil, nil, nil)
	if result.kind != loopDone {
		t.Errorf("expected loopDone when context cancelled, got %v", result.kind)
	}
}

// callbackBackend delegates to a user-provided function.
type callbackBackend struct {
	fn func(context.Context, backend.ChatRequest) (*backend.ChatResponse, error)
}

func (c *callbackBackend) ChatCompletion(ctx context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	return c.fn(ctx, req)
}
func (c *callbackBackend) Health(_ context.Context) error   { return nil }
func (c *callbackBackend) Shutdown(_ context.Context) error { return nil }
func (c *callbackBackend) ContextWindow() int               { return 8192 }

// ---------------------------------------------------------------------------
// runOnce — "length" done reason path
// ---------------------------------------------------------------------------

func TestRunOnce_LengthDoneReason(t *testing.T) {
	tm := New()
	store := makeTestStore(t)
	sess := store.New("test", "/tmp", "m")
	reg := agents.NewRegistry()

	fb := &fakeBackend{
		response: &backend.ChatResponse{
			Content:    "partial response due to length",
			DoneReason: "length",
		},
	}

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "big task"})
	var doneBroadcast bool
	broadcast := func(_, msgType string, payload map[string]any) {
		if msgType == "thread_done" {
			if st, _ := payload["status"].(string); st == "completed-with-timeout" {
				doneBroadcast = true
			}
		}
	}
	ca := NewCostAccumulator(0)

	result := tm.runOnce(context.Background(), thread.ID, "", "", sess, store, reg, fb, broadcast, ca, nil, nil, nil, nil, nil)
	if result.kind != loopDone {
		t.Errorf("expected loopDone for length reason, got %v", result.kind)
	}
	if !doneBroadcast {
		t.Error("expected thread_done with completed-with-timeout for length reason")
	}
}

// ---------------------------------------------------------------------------
// runOnce — thread not found (returns immediately)
// ---------------------------------------------------------------------------

func TestRunOnce_ThreadNotFound(t *testing.T) {
	tm := New()
	store := makeTestStore(t)
	sess := store.New("test", "/tmp", "m")
	reg := agents.NewRegistry()

	fb := &fakeBackend{response: &backend.ChatResponse{Content: "noop", DoneReason: "stop"}}
	broadcast := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	// Thread ID doesn't exist in the manager.
	result := tm.runOnce(context.Background(), "nonexistent-id", "", "", sess, store, reg, fb, broadcast, ca, nil, nil, nil, nil, nil)
	if result.kind != loopDone {
		t.Errorf("expected loopDone for not-found thread, got %v", result.kind)
	}
}

// ---------------------------------------------------------------------------
// runOnce — injected user input (resume after help)
// ---------------------------------------------------------------------------

func TestRunOnce_WithInjectedInput(t *testing.T) {
	tm := New()
	store := makeTestStore(t)
	sess := store.New("test", "/tmp", "m")
	reg := agents.NewRegistry()

	fb := &fakeBackend{
		response: &backend.ChatResponse{
			ToolCalls: []backend.ToolCall{
				{
					ID: "tc-fin",
					Function: backend.ToolCallFunction{
						Name:      "finish",
						Arguments: map[string]any{"summary": "resumed", "status": "completed"},
					},
				},
			},
			DoneReason: "tool_calls",
		},
	}

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "task"})
	broadcast := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	// Pass injected input — exercises the "injectedInput != ''" branch.
	result := tm.runOnce(context.Background(), thread.ID, "", "user clarification", sess, store, reg, fb, broadcast, ca, nil, nil, nil, nil, nil)
	if result.kind != loopDone {
		t.Errorf("expected loopDone with injected input, got %v", result.kind)
	}
}

// ---------------------------------------------------------------------------
// AcquireLeases — empty threadID
// ---------------------------------------------------------------------------

func TestAcquireLeases_EmptyThreadID(t *testing.T) {
	tm := New()
	_, err := tm.AcquireLeases("", []string{"file.go"})
	if err == nil {
		t.Error("expected error for empty threadID")
	}
}

// ---------------------------------------------------------------------------
// GetInputCh — not found
// ---------------------------------------------------------------------------

func TestGetInputCh_NotFound(t *testing.T) {
	tm := New()
	ch, ok := tm.GetInputCh("nonexistent")
	if ok || ch != nil {
		t.Error("expected (nil, false) for nonexistent thread")
	}
}

// ---------------------------------------------------------------------------
// GetInputCh — found
// ---------------------------------------------------------------------------

func TestGetInputCh_Found(t *testing.T) {
	tm := New()
	thread, _ := tm.Create(CreateParams{SessionID: "s", AgentID: "a", Task: "t"})
	ch, ok := tm.GetInputCh(thread.ID)
	if !ok {
		t.Error("expected to find InputCh")
	}
	if ch == nil {
		t.Error("expected non-nil InputCh")
	}
}

// ---------------------------------------------------------------------------
// CleanupSession — basic coverage
// ---------------------------------------------------------------------------

func TestCleanupSession_CancelsQueuedThreads(t *testing.T) {
	tm := New()
	t1, _ := tm.Create(CreateParams{SessionID: "clean-sess", AgentID: "a", Task: "t"})
	// Start one thread
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tm.Start(t1.ID, ctx, cancel)

	tm.CleanupSession("clean-sess")

	// Threads for this session should be removed.
	threads := tm.ListBySession("clean-sess")
	if len(threads) != 0 {
		t.Errorf("expected 0 threads after cleanup, got %d", len(threads))
	}
}
