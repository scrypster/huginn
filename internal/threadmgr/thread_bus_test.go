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

func TestThreadBus_SiblingContextIsSessionScoped(t *testing.T) {
	bus := NewThreadBus(8)
	bus.Publish("sess-a", ThreadContextMessage{ThreadID: "t-a1", AgentID: "Alpha", Content: "alpha update"})
	bus.Publish("sess-a", ThreadContextMessage{ThreadID: "t-a2", AgentID: "Beta", Content: "beta update"})
	bus.Publish("sess-b", ThreadContextMessage{ThreadID: "t-b1", AgentID: "Gamma", Content: "gamma update"})

	got := bus.SiblingContext("sess-a", "t-a1", "", 10)
	if len(got) != 1 {
		t.Fatalf("expected 1 sibling message in sess-a, got %d", len(got))
	}
	if got[0].ThreadID != "t-a2" {
		t.Fatalf("expected sibling from t-a2, got %q", got[0].ThreadID)
	}
	if strings.Contains(got[0].Content, "gamma") {
		t.Fatal("expected no cross-session leakage into sess-a")
	}

	gotB := bus.SiblingContext("sess-b", "t-b1", "", 10)
	if len(gotB) != 0 {
		t.Fatalf("expected no sibling messages in sess-b when excluding the only thread, got %d", len(gotB))
	}
}

func TestThreadBus_BoundedCapacityKeepsNewestEntries(t *testing.T) {
	bus := NewThreadBus(2)
	bus.Publish("sess", ThreadContextMessage{ThreadID: "t1", Content: "first"})
	bus.Publish("sess", ThreadContextMessage{ThreadID: "t2", Content: "second"})
	bus.Publish("sess", ThreadContextMessage{ThreadID: "t3", Content: "third"})

	got := bus.SiblingContext("sess", "", "", 10)
	if len(got) != 2 {
		t.Fatalf("expected 2 entries after capacity trim, got %d", len(got))
	}
	if got[0].Content != "second" || got[1].Content != "third" {
		t.Fatalf("expected newest entries [second third], got [%s %s]", got[0].Content, got[1].Content)
	}
}

func TestThreadBus_ConcurrentPublishAndRead_NoDeadlock(t *testing.T) {
	bus := NewThreadBus(32)
	var wg sync.WaitGroup

	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				bus.Publish("sess", ThreadContextMessage{
					ThreadID: "t-pub",
					AgentID:  "Publisher",
					Content:  "update",
				})
				_ = bus.SiblingContext("sess", "t-reader", "", 6)
			}
		}(i)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("concurrent Publish/SiblingContext timed out; possible deadlock")
	}
}

func TestSpawnThread_IncludesSiblingContextBlock(t *testing.T) {
	tm := New()
	store := session.NewStore(t.TempDir())
	sess := store.New("thread-bus-sess", "/tmp", "claude-haiku-4")

	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Alpha", ModelID: "claude-haiku-4"})
	reg.Register(&agents.Agent{Name: "Beta", ModelID: "claude-haiku-4"})

	seedThread, err := tm.Create(CreateParams{
		SessionID: sess.ID,
		AgentID:   "Alpha",
		Task:      "Seed context",
	})
	if err != nil {
		t.Fatalf("create seed thread: %v", err)
	}
	targetThread, err := tm.Create(CreateParams{
		SessionID: sess.ID,
		AgentID:   "Beta",
		Task:      "Consume sibling context",
	})
	if err != nil {
		t.Fatalf("create target thread: %v", err)
	}

	tm.PublishSiblingContext(seedThread.ID, "Investigated timeout root cause and narrowed it to scheduler retries.")

	cb := &captureRequestBackend{
		response: &backend.ChatResponse{
			ToolCalls: []backend.ToolCall{{
				ID: "tc-finish",
				Function: backend.ToolCallFunction{
					Name: "finish",
					Arguments: map[string]any{
						"summary": "done",
						"status":  "completed",
					},
				},
			}},
			DoneReason: "tool_calls",
		},
	}

	tm.SpawnThread(context.Background(), targetThread.ID, store, sess, reg, cb, func(string, string, map[string]any) {}, NewCostAccumulator(0), nil)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := tm.Get(targetThread.ID)
		if got != nil && got.Status == StatusDone {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	req := cb.LastRequest()
	if len(req.Messages) == 0 {
		t.Fatal("expected captured chat request messages")
	}

	var found bool
	for _, msg := range req.Messages {
		if !strings.Contains(msg.Content, "Team Context Updates") {
			continue
		}
		if strings.Contains(msg.Content, "Alpha: Investigated timeout root cause") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected sibling context block with Alpha update, got messages: %+v", req.Messages)
	}
}

type captureRequestBackend struct {
	mu       sync.Mutex
	req      backend.ChatRequest
	response *backend.ChatResponse
}

func (b *captureRequestBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	b.mu.Lock()
	clone := req
	clone.Messages = append([]backend.Message(nil), req.Messages...)
	clone.Tools = append([]backend.Tool(nil), req.Tools...)
	b.req = clone
	b.mu.Unlock()
	return b.response, nil
}

func (b *captureRequestBackend) LastRequest() backend.ChatRequest {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.req
}

func (b *captureRequestBackend) Health(_ context.Context) error   { return nil }
func (b *captureRequestBackend) Shutdown(_ context.Context) error { return nil }
func (b *captureRequestBackend) ContextWindow() int               { return 8192 }
