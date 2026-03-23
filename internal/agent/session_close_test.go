package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/storage"
)

// buildSummaryJSON constructs a minimal valid JSON summary response for tests.
func buildSummaryJSON(summary, file, decision, question string) string {
	return `{"summary":"` + summary + `","files_touched":["` + file + `"],"decisions":["` + decision + `"],"open_questions":["` + question + `"]}`
}

// multiResponseBackend is a test double that returns different responses per call.
type multiResponseBackend struct {
	responses []string
	errors    []error
	callIdx   int
	mu        sync.Mutex
}

func (m *multiResponseBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	m.mu.Lock()
	idx := m.callIdx
	m.callIdx++
	m.mu.Unlock()
	if idx < len(m.errors) && m.errors[idx] != nil {
		return nil, m.errors[idx]
	}
	if idx < len(m.responses) && req.OnToken != nil {
		req.OnToken(m.responses[idx])
	}
	return &backend.ChatResponse{DoneReason: "stop"}, nil
}

func (m *multiResponseBackend) Health(_ context.Context) error   { return nil }
func (m *multiResponseBackend) Shutdown(_ context.Context) error { return nil }

func openTestMemoryStore(t *testing.T, machineID string) *agents.MemoryStore {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return agents.NewMemoryStore(s, machineID)
}

func makeTestAgent(name, modelID string) *agents.Agent {
	return &agents.Agent{
		Name:    name,
		ModelID: modelID,
	}
}

// TestSessionClose_GeneratesSummaryForAgentWithHistory verifies that SessionClose
// generates and persists a summary for agents that have conversation history.
func TestSessionClose_GeneratesSummaryForAgentWithHistory(t *testing.T) {
	mb := newMockBackend(buildSummaryJSON("agent worked on feature X", "a.go", "used approach Y", "need to test Z"))
	models := &modelconfig.Models{Reasoner: "test-model"}
	orch, err := NewOrchestrator(mb, models, nil, nil, nil, nil)
 if err != nil {
 	t.Fatalf("orch: %v", err)
 }

	ms := openTestMemoryStore(t, "test-machine")
	orch.SetMemoryStore(ms)

	reg := agents.NewRegistry()
	ag := makeTestAgent("Mark", "test-model")
	ag.AppendHistory(
		backend.Message{Role: "user", Content: "help me with task"},
		backend.Message{Role: "assistant", Content: "I'll help you with that task"},
	)
	reg.Register(ag)
	orch.SetAgentRegistry(reg)

	ctx := context.Background()
	if err := orch.SessionClose(ctx); err != nil {
		t.Fatalf("SessionClose: %v", err)
	}

	summaries, err := ms.LoadRecentSummaries(ctx, "Mark", 5)
	if err != nil {
		t.Fatalf("LoadRecentSummaries: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].Summary == "" {
		t.Error("expected non-empty summary")
	}
	if summaries[0].AgentName != "Mark" {
		t.Errorf("expected AgentName=Mark, got %q", summaries[0].AgentName)
	}
}

// TestSessionClose_SkipsAgentsWithNoHistory verifies that agents with no
// conversation history are not summarized.
func TestSessionClose_SkipsAgentsWithNoHistory(t *testing.T) {
	mb := newMockBackend(buildSummaryJSON("summary", "f.go", "d1", "q1"))
	models := &modelconfig.Models{Reasoner: "test-model"}
	orch, err := NewOrchestrator(mb, models, nil, nil, nil, nil)
 if err != nil {
 	t.Fatalf("orch: %v", err)
 }

	ms := openTestMemoryStore(t, "test-machine")
	orch.SetMemoryStore(ms)

	reg := agents.NewRegistry()
	// Register agent with no history
	ag := makeTestAgent("Chris", "test-model")
	reg.Register(ag)
	orch.SetAgentRegistry(reg)

	ctx := context.Background()
	if err := orch.SessionClose(ctx); err != nil {
		t.Fatalf("SessionClose: %v", err)
	}

	summaries, err := ms.LoadRecentSummaries(ctx, "Chris", 5)
	if err != nil {
		t.Fatalf("LoadRecentSummaries: %v", err)
	}
	if len(summaries) != 0 {
		t.Errorf("expected 0 summaries for agent with no history, got %d", len(summaries))
	}

	// Mock backend should not have been called since no agents had history
	mb.mu.Lock()
	callCount := mb.callCount
	mb.mu.Unlock()
	if callCount != 0 {
		t.Errorf("expected 0 backend calls, got %d", callCount)
	}
}

// contentAwareBackend is a test double that errors when the request contains a
// specific keyword and succeeds with a JSON summary otherwise.
type contentAwareBackend struct {
	failKeyword string
	successJSON string
	mu          sync.Mutex
}

func (c *contentAwareBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, msg := range req.Messages {
		if strings.Contains(msg.Content, c.failKeyword) {
			return nil, errors.New("llm error")
		}
	}
	if req.OnToken != nil {
		req.OnToken(c.successJSON)
	}
	return &backend.ChatResponse{DoneReason: "stop"}, nil
}

func (c *contentAwareBackend) Health(_ context.Context) error   { return nil }
func (c *contentAwareBackend) Shutdown(_ context.Context) error { return nil }
func (c *contentAwareBackend) ContextWindow() int               { return 128_000 }

// TestSessionClose_ContinuesOnOneAgentFailure verifies that SessionClose
// continues processing other agents even when one fails.
func TestSessionClose_ContinuesOnOneAgentFailure(t *testing.T) {
	ctx := context.Background()

	validJSON := buildSummaryJSON("chris did work", "c.go", "d1", "q1")
	// contentAwareBackend fails when it sees "mark work" in the request,
	// and succeeds with validJSON for all other requests.
	mb := &contentAwareBackend{
		failKeyword: "mark work",
		successJSON: validJSON,
	}

	models := modelconfig.DefaultModels()
	o := mustNewOrchestrator(t, mb, models, nil, nil, nil, nil)
	o.WithMachineID("test-machine")

	reg := agents.NewRegistry()

	// Mark: has history, LLM call will fail
	mark := &agents.Agent{Name: "Mark", ModelID: "test-model"}
	mark.AppendHistory(backend.Message{Role: "user", Content: "mark work"})
	reg.Register(mark)

	// Chris: has history, LLM call will succeed
	chris := &agents.Agent{Name: "Chris", ModelID: "test-model"}
	chris.AppendHistory(backend.Message{Role: "user", Content: "chris work"})
	reg.Register(chris)

	o.SetAgentRegistry(reg)

	dir := t.TempDir()
	s, _ := storage.Open(dir)
	defer s.Close()
	memStore := agents.NewMemoryStore(s, "test-machine")
	o.SetMemoryStore(memStore)

	// SessionClose should return nil even though Mark's summary failed.
	if err := o.SessionClose(ctx); err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}

	// Mark should have no summary (LLM failed).
	markSummaries, _ := memStore.LoadRecentSummaries(ctx, "Mark", 5)
	if len(markSummaries) != 0 {
		t.Errorf("expected 0 summaries for Mark (LLM errored), got %d", len(markSummaries))
	}

	// Chris should have a summary (LLM succeeded).
	chrisSummaries, _ := memStore.LoadRecentSummaries(ctx, "Chris", 5)
	if len(chrisSummaries) != 1 {
		t.Errorf("expected 1 summary for Chris, got %d", len(chrisSummaries))
	}
}

// TestSessionClose_SummaryPromptContainsHistory verifies that the summarization
// prompt includes the agent's history content.
func TestSessionClose_SummaryPromptContainsHistory(t *testing.T) {
	mb := newMockBackend(buildSummaryJSON("agent accomplished X and Y", "x.go", "used approach Z", ""))
	models := &modelconfig.Models{Reasoner: "test-model"}
	orch, err := NewOrchestrator(mb, models, nil, nil, nil, nil)
 if err != nil {
 	t.Fatalf("orch: %v", err)
 }

	ms := openTestMemoryStore(t, "test-machine")
	orch.SetMemoryStore(ms)

	reg := agents.NewRegistry()
	ag := makeTestAgent("Odin", "test-model")
	ag.AppendHistory(
		backend.Message{Role: "user", Content: "analyze this codebase"},
		backend.Message{Role: "assistant", Content: "I found several interesting patterns"},
	)
	reg.Register(ag)
	orch.SetAgentRegistry(reg)

	ctx := context.Background()
	_ = orch.SessionClose(ctx)

	// Verify the backend was called with a message containing history context
	mb.mu.Lock()
	reqs := mb.lastRequests
	mb.mu.Unlock()

	if len(reqs) == 0 {
		t.Fatal("expected at least one backend call")
	}

	// Find the user message in the last request
	req := reqs[len(reqs)-1]
	var foundHistory bool
	for _, msg := range req.Messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "analyze this codebase") {
			foundHistory = true
			break
		}
	}
	if !foundHistory {
		t.Error("expected summarization prompt to contain agent history content")
	}
}

// TestSessionClose_NilMemoryStore is a safety check — SessionClose should be a no-op when no store.
func TestSessionClose_NilMemoryStore(t *testing.T) {
	mb := newMockBackend(buildSummaryJSON("summary", "f.go", "d1", "q1"))
	models := &modelconfig.Models{Reasoner: "test-model"}
	orch, err := NewOrchestrator(mb, models, nil, nil, nil, nil)
 if err != nil {
 	t.Fatalf("orch: %v", err)
 }
	// No memory store set

	reg := agents.NewRegistry()
	ag := makeTestAgent("Mark", "test-model")
	ag.AppendHistory(backend.Message{Role: "user", Content: "test"})
	reg.Register(ag)
	orch.SetAgentRegistry(reg)

	ctx := context.Background()
	if err := orch.SessionClose(ctx); err != nil {
		t.Errorf("SessionClose with nil store should not error: %v", err)
	}
}

// TestSnapshotHistory verifies SnapshotHistory returns a copy with correct limit.
func TestSnapshotHistory(t *testing.T) {
	ag := makeTestAgent("TestAgent", "model")
	for i := 0; i < 10; i++ {
		ag.AppendHistory(backend.Message{Role: "user", Content: "msg"})
	}

	snap := ag.SnapshotHistory(5)
	if len(snap) != 5 {
		t.Errorf("expected 5 messages, got %d", len(snap))
	}

	// Verify it's a copy
	snap[0].Content = "modified"
	fresh := ag.SnapshotHistory(5)
	if fresh[0].Content == "modified" {
		t.Error("SnapshotHistory should return a copy, not a reference")
	}
}



// TestSessionClose_TimestampSet verifies the summary has a recent timestamp.
func TestSummarizeAgent_TimestampSet(t *testing.T) {
	mb := newMockBackend(buildSummaryJSON("did some work", "f.go", "d1", "q1"))
	models := &modelconfig.Models{Reasoner: "test-model"}
	orch, err := NewOrchestrator(mb, models, nil, nil, nil, nil)
 if err != nil {
 	t.Fatalf("orch: %v", err)
 }

	ms := openTestMemoryStore(t, "machine-ts")
	orch.SetMemoryStore(ms)

	reg := agents.NewRegistry()
	ag := makeTestAgent("Frigg", "test-model")
	ag.AppendHistory(backend.Message{Role: "user", Content: "work item"})
	reg.Register(ag)
	orch.SetAgentRegistry(reg)

	before := time.Now().Add(-time.Second)
	ctx := context.Background()
	_ = orch.SessionClose(ctx)
	after := time.Now().Add(time.Second)

	summaries, _ := ms.LoadRecentSummaries(ctx, "Frigg", 5)
	if len(summaries) == 0 {
		t.Fatal("no summaries stored")
	}
	ts := summaries[0].Timestamp
	if ts.Before(before) || ts.After(after) {
		t.Errorf("timestamp %v not in expected range [%v, %v]", ts, before, after)
	}
}
