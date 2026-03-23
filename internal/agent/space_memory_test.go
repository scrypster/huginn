package agent

import (
	"context"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
)

// --- helpers ---

// mockMemoryStore is a simple in-memory MemoryStoreIface for tests.
type mockMemoryStore struct {
	summaries   []agents.SessionSummary
	delegations []agents.DelegationEntry
}

func (m *mockMemoryStore) SaveSummary(_ context.Context, s agents.SessionSummary) error {
	m.summaries = append(m.summaries, s)
	return nil
}
func (m *mockMemoryStore) LoadRecentSummaries(_ context.Context, _ string, _ int) ([]agents.SessionSummary, error) {
	return m.summaries, nil
}
func (m *mockMemoryStore) AppendDelegation(_ context.Context, e agents.DelegationEntry) error {
	m.delegations = append(m.delegations, e)
	return nil
}
func (m *mockMemoryStore) LoadRecentDelegations(_ context.Context, _, _ string, _ int) ([]agents.DelegationEntry, error) {
	return m.delegations, nil
}

// stubSummaryBackend replies with a canned JSON summary.
type stubSummaryBackend struct{}

func (s *stubSummaryBackend) ChatCompletion(_ context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	if req.OnToken != nil {
		req.OnToken(`{"summary":"worked on channel task","files_touched":[],"decisions":[],"open_questions":[]}`)
	}
	return &backend.ChatResponse{DoneReason: "stop"}, nil
}
func (s *stubSummaryBackend) ContextWindow() int         { return 4096 }
func (s *stubSummaryBackend) Health(_ context.Context) error { return nil }
func (s *stubSummaryBackend) Shutdown(_ context.Context) error { return nil }

// newAgentForTest creates a named agent using FromDef with an inline model name.
func newAgentForTest(name string) *agents.Agent {
	return agents.FromDef(agents.AgentDef{Name: name, Model: "test-model"})
}

// newAgentRegistry creates an AgentRegistry containing the provided agents.
func newAgentRegistry(agts ...*agents.Agent) *agents.AgentRegistry {
	reg := agents.NewRegistry()
	for _, a := range agts {
		reg.Register(a)
	}
	return reg
}

// --- tests ---

// TestSetSpaceContext_StoresValues stores and reads spaceID/spaceName from the orchestrator.
func TestSetSpaceContext_StoresValues(t *testing.T) {
	o := newBareOrchestrator()
	o.SetSpaceContext("space-123", "general")

	o.mu.RLock()
	sid := o.spaceID
	sname := o.spaceName
	o.mu.RUnlock()

	if sid != "space-123" {
		t.Errorf("spaceID = %q; want space-123", sid)
	}
	if sname != "general" {
		t.Errorf("spaceName = %q; want general", sname)
	}
}

// TestSetSpaceContext_OverwritesSafelyUnderLock verifies concurrent writes don't panic.
func TestSetSpaceContext_OverwritesSafelyUnderLock(t *testing.T) {
	o := newBareOrchestrator()
	done := make(chan struct{}, 10)
	for i := 0; i < 10; i++ {
		go func() {
			o.SetSpaceContext("space-x", "channel-x")
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	// No panic = pass.
}

// TestSummarizeAgent_IncludesSpaceContext verifies summarizeAgent populates SpaceID/SpaceName.
func TestSummarizeAgent_IncludesSpaceContext(t *testing.T) {
	b := &stubSummaryBackend{}
	o := newBareOrchestrator()
	o.backend = b

	ag := newAgentForTest("Sam")
	ag.AppendHistory(
		backend.Message{Role: "user", Content: "what is 2+2"},
		backend.Message{Role: "assistant", Content: "4"},
	)

	summary, err := o.summarizeAgent(context.Background(), ag, "sess-1", "machine-1", "space-42", "ops-room")
	if err != nil {
		t.Fatalf("summarizeAgent: %v", err)
	}
	if summary.SpaceID != "space-42" {
		t.Errorf("SpaceID = %q; want space-42", summary.SpaceID)
	}
	if summary.SpaceName != "ops-room" {
		t.Errorf("SpaceName = %q; want ops-room", summary.SpaceName)
	}
}

// TestSummarizeAgent_EmptySpaceContext_FieldsEmpty verifies that non-space sessions have empty fields.
func TestSummarizeAgent_EmptySpaceContext_FieldsEmpty(t *testing.T) {
	b := &stubSummaryBackend{}
	o := newBareOrchestrator()
	o.backend = b

	ag := newAgentForTest("Tom")
	ag.AppendHistory(
		backend.Message{Role: "user", Content: "hello"},
		backend.Message{Role: "assistant", Content: "hi"},
	)

	summary, err := o.summarizeAgent(context.Background(), ag, "sess-1", "machine-1", "", "")
	if err != nil {
		t.Fatalf("summarizeAgent: %v", err)
	}
	if summary.SpaceID != "" {
		t.Errorf("expected empty SpaceID; got %q", summary.SpaceID)
	}
	if summary.SpaceName != "" {
		t.Errorf("expected empty SpaceName; got %q", summary.SpaceName)
	}
}

// TestSessionClose_PassesSpaceContextToSummary verifies SessionClose reads spaceID/spaceName
// from the orchestrator and persists them in the saved summary.
func TestSessionClose_PassesSpaceContextToSummary(t *testing.T) {
	b := &stubSummaryBackend{}
	ms := &mockMemoryStore{}

	o := newBareOrchestrator()
	o.backend = b
	o.memoryStore = ms
	o.machineID = "m1"

	ag := newAgentForTest("Sam")
	ag.AppendHistory(
		backend.Message{Role: "user", Content: "analyze logs"},
		backend.Message{Role: "assistant", Content: "done"},
	)

	reg := newAgentRegistry(ag)
	o.agentReg = reg

	o.SetSpaceContext("space-99", "incident-room")

	if err := o.SessionClose(context.Background()); err != nil {
		t.Fatalf("SessionClose: %v", err)
	}

	if len(ms.summaries) != 1 {
		t.Fatalf("want 1 summary; got %d", len(ms.summaries))
	}
	got := ms.summaries[0]
	if got.SpaceID != "space-99" {
		t.Errorf("summary.SpaceID = %q; want space-99", got.SpaceID)
	}
	if got.SpaceName != "incident-room" {
		t.Errorf("summary.SpaceName = %q; want incident-room", got.SpaceName)
	}
}

// TestSessionClose_NoSpaceContext_SummaryHasEmptySpaceFields verifies DM sessions
// (no space context) produce summaries without space metadata.
func TestSessionClose_NoSpaceContext_SummaryHasEmptySpaceFields(t *testing.T) {
	b := &stubSummaryBackend{}
	ms := &mockMemoryStore{}

	o := newBareOrchestrator()
	o.backend = b
	o.memoryStore = ms
	o.machineID = "m1"

	ag := newAgentForTest("Tom")
	ag.AppendHistory(
		backend.Message{Role: "user", Content: "what time is it"},
		backend.Message{Role: "assistant", Content: "now"},
	)

	reg := newAgentRegistry(ag)
	o.agentReg = reg

	// Deliberately do NOT call SetSpaceContext — simulates DM/TUI session.

	if err := o.SessionClose(context.Background()); err != nil {
		t.Fatalf("SessionClose: %v", err)
	}

	if len(ms.summaries) != 1 {
		t.Fatalf("want 1 summary; got %d", len(ms.summaries))
	}
	got := ms.summaries[0]
	if got.SpaceID != "" || got.SpaceName != "" {
		t.Errorf("expected empty space fields; got SpaceID=%q SpaceName=%q", got.SpaceID, got.SpaceName)
	}
}

// TestSessionSummary_SpaceFields_ZeroValueByDefault confirms struct zero values are empty strings.
func TestSessionSummary_SpaceFields_ZeroValueByDefault(t *testing.T) {
	s := agents.SessionSummary{
		SessionID: "s1",
		AgentName: "Tom",
		Timestamp: time.Now(),
		Summary:   "some work",
	}
	if s.SpaceID != "" || s.SpaceName != "" {
		t.Error("zero-value SpaceID and SpaceName should be empty string")
	}
}
