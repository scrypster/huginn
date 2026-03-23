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

// fakeBackend drives a scripted sequence of LLM responses.
type fakeBackend struct {
	mu       sync.Mutex
	calls    int
	response *backend.ChatResponse
	err      error
}

func (f *fakeBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if req.OnToken != nil && f.response != nil {
		req.OnToken(f.response.Content)
	}
	return f.response, f.err
}
func (f *fakeBackend) Health(_ context.Context) error   { return nil }
func (f *fakeBackend) Shutdown(_ context.Context) error { return nil }
func (f *fakeBackend) ContextWindow() int               { return 8192 }

type capturedBroadcast struct {
	sessionID string
	msgType   string
	payload   map[string]any
}

func TestSpawnThread_CompletesViaFinishTool(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "claude-haiku-4")

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:    "Coder",
		ModelID: "claude-haiku-4",
	})

	// Backend returns a finish() tool call
	fb := &fakeBackend{
		response: &backend.ChatResponse{
			ToolCalls: []backend.ToolCall{
				{
					ID: "tc-1",
					Function: backend.ToolCallFunction{
						Name: "finish",
						Arguments: map[string]any{
							"summary": "all done",
							"status":  "completed",
						},
					},
				},
			},
			DoneReason:       "tool_calls",
			PromptTokens:     100,
			CompletionTokens: 50,
		},
	}

	thread, _ := tm.Create(CreateParams{
		SessionID: sess.ID,
		AgentID:   "Coder",
		Task:      "fix the thing",
	})

	var broadcasts []capturedBroadcast
	var bmu sync.Mutex
	broadcastFn := func(sessionID, msgType string, payload map[string]any) {
		bmu.Lock()
		broadcasts = append(broadcasts, capturedBroadcast{sessionID, msgType, payload})
		bmu.Unlock()
	}

	ca := NewCostAccumulator(0)
	ctx := context.Background()

	tm.SpawnThread(ctx, thread.ID, store, sess, reg, fb, broadcastFn, ca, nil)

	// Wait for goroutine to finish
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := tm.Get(thread.ID)
		if got != nil && got.Status == StatusDone {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	got, ok := tm.Get(thread.ID)
	if !ok {
		t.Fatal("thread not found")
	}
	if got.Status != StatusDone {
		t.Errorf("expected StatusDone, got %s", got.Status)
	}
	if got.Summary == nil || got.Summary.Summary != "all done" {
		t.Errorf("expected summary 'all done', got: %+v", got.Summary)
	}

	// Verify thread_started and thread_done were broadcast
	bmu.Lock()
	types := make([]string, len(broadcasts))
	for i, b := range broadcasts {
		types[i] = b.msgType
	}
	bmu.Unlock()

	var hasStarted, hasDone bool
	for _, tp := range types {
		if tp == "thread_started" {
			hasStarted = true
		}
		if tp == "thread_done" {
			hasDone = true
		}
	}
	if !hasStarted {
		t.Error("expected thread_started broadcast")
	}
	if !hasDone {
		t.Error("expected thread_done broadcast")
	}
}

func TestSpawnThread_CancelStopsGoroutine(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "claude-haiku-4")
	reg := agents.NewRegistry()

	blockCh := make(chan struct{})
	blockingBackend := &blockingFakeBackend{block: blockCh}

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Bot", Task: "block"})

	broadcastFn := func(_, _ string, _ map[string]any) {}
	ca := NewCostAccumulator(0)

	// SpawnThread decouples the thread context from the caller's context to prevent
	// Tom's WS request context from killing Sam's thread when Tom's response finishes.
	// Use tm.Cancel(threadID) to stop threads — that's the proper cancellation API.
	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, blockingBackend, broadcastFn, ca, nil)

	time.Sleep(20 * time.Millisecond)
	tm.Cancel(thread.ID)
	close(blockCh)

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := tm.Get(thread.ID)
		if got != nil && (got.Status == StatusCancelled || got.Status == StatusError) {
			return // success
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("goroutine did not stop after cancel")
}

// blockingFakeBackend blocks until block channel is closed.
type blockingFakeBackend struct {
	block chan struct{}
}

func (b *blockingFakeBackend) ChatCompletion(ctx context.Context, _ backend.ChatRequest) (*backend.ChatResponse, error) {
	select {
	case <-b.block:
		return nil, context.Canceled
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (b *blockingFakeBackend) Health(_ context.Context) error   { return nil }
func (b *blockingFakeBackend) Shutdown(_ context.Context) error { return nil }
func (b *blockingFakeBackend) ContextWindow() int               { return 8192 }

func TestSpawnThread_HelpEscalationBlocksThread(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("test", "/tmp", "claude-haiku-4")
	reg := agents.NewRegistry()

	fb := &fakeBackend{
		response: &backend.ChatResponse{
			ToolCalls: []backend.ToolCall{
				{
					ID: "tc-2",
					Function: backend.ToolCallFunction{
						Name:      "request_help",
						Arguments: map[string]any{"message": "need clarification"},
					},
				},
			},
			DoneReason: "tool_calls",
		},
	}

	thread, _ := tm.Create(CreateParams{SessionID: sess.ID, AgentID: "Helper", Task: "unclear task"})

	var broadcastedTypes []string
	var bmu sync.Mutex
	broadcastFn := func(_, msgType string, _ map[string]any) {
		bmu.Lock()
		broadcastedTypes = append(broadcastedTypes, msgType)
		bmu.Unlock()
	}

	ca := NewCostAccumulator(0)
	tm.SpawnThread(context.Background(), thread.ID, store, sess, reg, fb, broadcastFn, ca, nil)

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := tm.Get(thread.ID)
		if got != nil && got.Status == StatusBlocked {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	got, _ := tm.Get(thread.ID)
	if got == nil || got.Status != StatusBlocked {
		t.Errorf("expected StatusBlocked, got %v", got)
	}

	bmu.Lock()
	var hasHelp bool
	for _, tp := range broadcastedTypes {
		if strings.Contains(tp, "thread_help") {
			hasHelp = true
		}
	}
	bmu.Unlock()
	if !hasHelp {
		t.Errorf("expected thread_help broadcast, got types: %v", broadcastedTypes)
	}
}
