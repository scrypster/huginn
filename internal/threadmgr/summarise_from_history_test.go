package threadmgr

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/session"
)

// --- summariseFromHistory ---

func TestSummariseFromHistory_EmptyHistory(t *testing.T) {
	s := summariseFromHistory(nil, "timeout", "error")
	if s.Summary != "timeout" {
		t.Errorf("expected 'timeout', got %q", s.Summary)
	}
	if s.Status != "error" {
		t.Errorf("expected status 'error', got %q", s.Status)
	}
}

func TestSummariseFromHistory_NoAssistantMessages(t *testing.T) {
	history := []backend.Message{
		{Role: "user", Content: "hello"},
		{Role: "user", Content: "world"},
	}
	s := summariseFromHistory(history, "done", "completed")
	if s.Summary != "done" {
		t.Errorf("expected 'done', got %q", s.Summary)
	}
}

func TestSummariseFromHistory_WithAssistantMessages(t *testing.T) {
	history := []backend.Message{
		{Role: "user", Content: "question"},
		{Role: "assistant", Content: "I fixed the bug"},
		{Role: "user", Content: "thanks"},
		{Role: "assistant", Content: "done"},
	}
	s := summariseFromHistory(history, "timeout", "error")
	if !strings.Contains(s.Summary, "I fixed the bug") && !strings.Contains(s.Summary, "done") {
		t.Errorf("expected assistant content in summary, got %q", s.Summary)
	}
}

func TestSummariseFromHistory_LongsummaryClipped(t *testing.T) {
	bigContent := strings.Repeat("x", 1000)
	history := []backend.Message{
		{Role: "assistant", Content: bigContent},
	}
	s := summariseFromHistory(history, "reason", "error")
	if len(s.Summary) > 503 { // 500 + "..."
		t.Errorf("summary too long: %d chars", len(s.Summary))
	}
}

// --- clipResult ---

func TestClipResult_ShortString(t *testing.T) {
	got := clipResult("hello", 100)
	if got != "hello" {
		t.Errorf("expected 'hello', got %q", got)
	}
}

func TestClipResult_ExactLength(t *testing.T) {
	s := strings.Repeat("a", 100)
	got := clipResult(s, 100)
	if got != s {
		t.Error("should not clip string at exact limit")
	}
}

func TestClipResult_TrimsSpaces(t *testing.T) {
	got := clipResult("  hello  ", 100)
	if got != "hello" {
		t.Errorf("expected trimmed 'hello', got %q", got)
	}
}

func TestClipResult_TruncatesLong(t *testing.T) {
	s := strings.Repeat("a", 200)
	got := clipResult(s, 50)
	if len(got) != 50 {
		t.Errorf("expected length 50, got %d", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("expected '...' suffix, got %q", got[47:])
	}
}

// --- waitForInputOnce ---

func TestWaitForInputOnce_ContextCancelled(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "claude-haiku-4")

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "test"})
	// Manually set thread to Blocked so InputCh exists
	tm.mu.Lock()
	tm.threads[thread.ID].Status = StatusBlocked
	tm.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, ok := tm.waitForInputOnce(ctx, thread.ID)
	if ok {
		t.Error("expected false (context cancelled), got true")
	}
}

func TestWaitForInputOnce_NonExistentThread(t *testing.T) {
	tm := New()
	ctx := context.Background()
	_, ok := tm.waitForInputOnce(ctx, "nonexistent-thread-id")
	if ok {
		t.Error("expected false for nonexistent thread, got true")
	}
}

func TestWaitForInputOnce_ReceivesInput(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "claude-haiku-4")

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "test"})
	tm.mu.Lock()
	tm.threads[thread.ID].Status = StatusBlocked
	inputCh := tm.threads[thread.ID].InputCh
	tm.mu.Unlock()

	go func() {
		time.Sleep(10 * time.Millisecond)
		inputCh <- "user response"
	}()

	input, ok := tm.waitForInputOnce(context.Background(), thread.ID)
	if !ok {
		t.Error("expected true (input received), got false")
	}
	if input != "user response" {
		t.Errorf("expected 'user response', got %q", input)
	}
}

// --- setBlocked ---

func TestSetBlocked_SetsStatus(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "claude-haiku-4")
	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "test"})

	tm.setBlocked(thread.ID, "need help")

	got, ok := tm.Get(thread.ID)
	if !ok {
		t.Fatal("thread not found")
	}
	if got.Status != StatusBlocked {
		t.Errorf("expected StatusBlocked, got %s", got.Status)
	}
}

func TestSetBlocked_NonExistentThread(t *testing.T) {
	tm := New()
	// Should not panic
	tm.setBlocked("nonexistent", "msg")
}

// --- getThread ---

func TestGetThread_ReturnsNilForMissing(t *testing.T) {
	tm := New()
	if tm.getThread("missing") != nil {
		t.Error("expected nil for missing thread")
	}
}

// --- resolveModelID ---

func TestResolveModelID_FallsBackToSession(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "claude-sonnet-4")
	sess.Manifest.Model = "claude-sonnet-4"

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Unknown", Task: "test"})
	reg := agents.NewRegistry() // empty registry

	modelID := tm.resolveModelID(thread.ID, reg, sess)
	if modelID != "claude-sonnet-4" {
		t.Errorf("expected session model 'claude-sonnet-4', got %q", modelID)
	}
}

func TestResolveModelID_UsesAgentModel(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "session-model")
	sess.Manifest.Model = "session-model"

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Coder", ModelID: "agent-specific-model"})

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Coder", Task: "test"})

	modelID := tm.resolveModelID(thread.ID, reg, sess)
	if modelID != "agent-specific-model" {
		t.Errorf("expected 'agent-specific-model', got %q", modelID)
	}
}

func TestResolveModelID_NilRegistry(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "fallback-model")
	sess.Manifest.Model = "fallback-model"
	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "test"})

	modelID := tm.resolveModelID(thread.ID, nil, sess)
	if modelID != "fallback-model" {
		t.Errorf("expected 'fallback-model', got %q", modelID)
	}
}

// --- SpawnThread — LLM error path ---

func TestSpawnThread_LLMErrorMarksThreadError(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "claude-haiku-4")
	reg := agents.NewRegistry()

	errBackend := &fakeBackend{err: errors.New("connection refused")}
	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "fail"})

	var broadcasts []string
	var bmu sync.Mutex
	broadcastFn := func(_, msgType string, _ map[string]any) {
		bmu.Lock()
		broadcasts = append(broadcasts, msgType)
		bmu.Unlock()
	}

	ca := NewCostAccumulator(0)
	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, errBackend, broadcastFn, ca, nil)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := tm.Get(thread.ID)
		if got != nil && got.Status == StatusDone {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	got, ok := tm.Get(thread.ID)
	if !ok {
		t.Fatal("thread not found")
	}
	if got.Status != StatusDone {
		t.Errorf("expected StatusDone after LLM error, got %s", got.Status)
	}

	bmu.Lock()
	var hasDone bool
	for _, tp := range broadcasts {
		if tp == "thread_done" {
			hasDone = true
		}
	}
	bmu.Unlock()
	if !hasDone {
		t.Error("expected thread_done broadcast on LLM error")
	}
}

// --- SpawnThread — budget exceeded ---

func TestSpawnThread_BudgetExceededStopsThread(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "claude-haiku-4")
	reg := agents.NewRegistry()

	// budget of $0.00001 — exceeded immediately after first real call would record
	ca := NewCostAccumulator(0.00001)
	// Pre-exceed the budget
	ca.Record("prior-thread", 1_000_000, 1_000_000, "claude-haiku-4")

	fb := &fakeBackend{
		response: &backend.ChatResponse{
			Content:    "I'll do it",
			DoneReason: "stop",
		},
	}

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "test budget"})

	var broadcasts []string
	var bmu sync.Mutex
	broadcastFn := func(_, msgType string, _ map[string]any) {
		bmu.Lock()
		broadcasts = append(broadcasts, msgType)
		bmu.Unlock()
	}

	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, fb, broadcastFn, ca, nil)

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
		t.Errorf("expected StatusDone after budget exceeded, got %v", got)
	}

	bmu.Lock()
	var hasDone bool
	for _, tp := range broadcasts {
		if tp == "thread_done" {
			hasDone = true
		}
	}
	bmu.Unlock()
	if !hasDone {
		t.Error("expected thread_done broadcast on budget exceeded")
	}
}

// --- SpawnThread — dagFn called ---

func TestSpawnThread_DagFnCalledOnCompletion(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "claude-haiku-4")
	reg := agents.NewRegistry()

	fb := &fakeBackend{
		response: &backend.ChatResponse{
			ToolCalls: []backend.ToolCall{
				{
					ID: "tc-1",
					Function: backend.ToolCallFunction{
						Name:      "finish",
						Arguments: map[string]any{"summary": "dag test done", "status": "completed"},
					},
				},
			},
			DoneReason: "tool_calls",
		},
	}

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "dag test"})
	ca := NewCostAccumulator(0)

	dagCalled := make(chan struct{}, 1)
	dagFn := func() {
		dagCalled <- struct{}{}
	}

	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, fb, func(_, _ string, _ map[string]any) {}, ca, dagFn)

	select {
	case <-dagCalled:
		// success
	case <-time.After(2 * time.Second):
		t.Error("dagFn was not called within timeout")
	}
}

// --- SpawnThread — unknown tool ---

func TestSpawnThread_UnknownToolContinues(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "claude-haiku-4")
	reg := agents.NewRegistry()

	callCount := 0
	var mu sync.Mutex
	// First call returns unknown tool; second call finishes
	customBackend := &multiResponseBackend{
		responses: []*backend.ChatResponse{
			{
				ToolCalls: []backend.ToolCall{
					{
						ID: "tc-unknown",
						Function: backend.ToolCallFunction{
							Name:      "nonexistent_tool",
							Arguments: map[string]any{},
						},
					},
				},
				DoneReason: "tool_calls",
			},
			{
				ToolCalls: []backend.ToolCall{
					{
						ID: "tc-finish",
						Function: backend.ToolCallFunction{
							Name:      "finish",
							Arguments: map[string]any{"summary": "done after unknown tool", "status": "completed"},
						},
					},
				},
				DoneReason: "tool_calls",
			},
		},
		mu:        &mu,
		callCount: &callCount,
	}

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "unknown tool test"})
	ca := NewCostAccumulator(0)

	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, customBackend, func(_, _ string, _ map[string]any) {}, ca, nil)

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
		t.Errorf("expected StatusDone, got %v", got)
	}
	mu.Lock()
	calls := callCount
	mu.Unlock()
	if calls < 2 {
		t.Errorf("expected at least 2 LLM calls, got %d", calls)
	}
}

// multiResponseBackend returns scripted responses in sequence.
type multiResponseBackend struct {
	responses []*backend.ChatResponse
	mu        *sync.Mutex
	callCount *int
}

func (m *multiResponseBackend) ChatCompletion(_ context.Context, _ backend.ChatRequest) (*backend.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := *m.callCount
	*m.callCount++
	if idx < len(m.responses) {
		return m.responses[idx], nil
	}
	// All responses consumed — finish
	return &backend.ChatResponse{
		ToolCalls: []backend.ToolCall{
			{ID: "tc-final", Function: backend.ToolCallFunction{
				Name:      "finish",
				Arguments: map[string]any{"summary": "extra finish", "status": "completed"},
			}},
		},
		DoneReason: "tool_calls",
	}, nil
}
func (m *multiResponseBackend) Health(_ context.Context) error   { return nil }
func (m *multiResponseBackend) Shutdown(_ context.Context) error { return nil }
func (m *multiResponseBackend) ContextWindow() int               { return 8192 }

// --- SpawnThread — stop without tool calls (implicit finish) ---

func TestSpawnThread_StopWithNoToolCalls(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "claude-haiku-4")
	reg := agents.NewRegistry()

	fb := &fakeBackend{
		response: &backend.ChatResponse{
			Content:    "I completed the task",
			DoneReason: "stop",
		},
	}

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "implicit finish"})
	ca := NewCostAccumulator(0)

	var broadcasts []string
	var bmu sync.Mutex
	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, fb, func(_, msgType string, _ map[string]any) {
		bmu.Lock()
		broadcasts = append(broadcasts, msgType)
		bmu.Unlock()
	}, ca, nil)

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
		t.Errorf("expected StatusDone for stop response, got %v", got)
	}

	bmu.Lock()
	var hasDone bool
	for _, tp := range broadcasts {
		if tp == "thread_done" {
			hasDone = true
		}
	}
	bmu.Unlock()
	if !hasDone {
		t.Error("expected thread_done broadcast for stop response")
	}
}

// --- CostAccumulator concurrent access ---

func TestCostAccumulator_ConcurrentRecordAndCheck(t *testing.T) {
	ca := NewCostAccumulator(10.0) // $10 budget

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				ca.Record("thread", 1000, 500, "claude-haiku-4")
				_ = ca.CheckBudget()
				_ = ca.Total()
			}
		}(i)
	}
	wg.Wait()

	total := ca.Total()
	if total <= 0 {
		t.Error("expected positive total after concurrent records")
	}
}

func TestCostAccumulator_ZeroBudgetNeverExceeds(t *testing.T) {
	ca := NewCostAccumulator(0)
	// Record a lot of cost
	for i := 0; i < 100; i++ {
		ca.Record("t", 1_000_000, 1_000_000, "claude-opus-4")
	}
	if err := ca.CheckBudget(); err != nil {
		t.Errorf("zero budget should never exceed: %v", err)
	}
}

func TestCostAccumulator_BudgetExceededAfterRecord(t *testing.T) {
	ca := NewCostAccumulator(0.001) // very small budget
	ca.Record("t", 1_000_000, 1_000_000, "claude-opus-4")
	if err := ca.CheckBudget(); err == nil {
		t.Error("expected ErrBudgetExceeded after large record")
	}
}
