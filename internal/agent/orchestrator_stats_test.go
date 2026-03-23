package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/relay"
	"github.com/scrypster/huginn/internal/stats"
	"github.com/scrypster/huginn/internal/storage"
)

// TestOrchestrator_ChatRecordsLatency verifies that Chat() records a latency histogram.
func TestOrchestrator_ChatRecordsLatency(t *testing.T) {
	t.Parallel()

	reg := stats.NewRegistry()
	sc := reg.Collector()

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{Content: "response", DoneReason: "stop"},
		},
	}
	models := &modelconfig.Models{
		Reasoner: "test",
	}

	o := mustNewOrchestrator(t, mb, models, nil, nil, sc, nil)
	if err := o.Chat(context.Background(), "hello", nil, nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	snap := reg.Snapshot()
	if len(snap.Histograms) == 0 {
		t.Fatal("expected at least one histogram entry after Chat(), got 0")
	}

	var found bool
	for _, h := range snap.Histograms {
		if h.Metric == "agent.llm_latency_ms" {
			found = true
			if h.Value < 0 {
				t.Errorf("latency must be non-negative, got %v", h.Value)
			}
		}
	}
	if !found {
		t.Error("expected histogram metric 'agent.llm_latency_ms' not found")
	}
}

// TestOrchestrator_NilCollector_NoNilPanic verifies that nil stats.Collector
// is safe (orchestrator uses NoopCollector when nil).
func TestOrchestrator_NilCollector_NoNilPanic(t *testing.T) {
	t.Parallel()

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{Content: "response", DoneReason: "stop"},
		},
	}
	models := modelconfig.DefaultModels()

	// nil collector must not panic
	o := mustNewOrchestrator(t, mb, models, nil, nil, nil, nil)
	if err := o.Chat(context.Background(), "hello", nil, nil); err != nil {
		t.Fatalf("Chat with nil collector: %v", err)
	}
}

// TestSessionClose_SummarizeError_RecordsCounter verifies that SessionClose
// records an agent.summary_errors counter when summarization fails.
func TestSessionClose_SummarizeError_RecordsCounter(t *testing.T) {
	t.Parallel()

	// Create a registry with an agent that has history
	reg := agents.NewRegistry()
	ag := &agents.Agent{Name: "bob", ModelID: "test-model"}
	ag.AppendHistory(backend.Message{Role: "user", Content: "hello"})
	reg.Register(ag)

	// Create a backend that will fail the chat completion
	failBackend := &mockBackend{
		errors: []error{errors.New("network error")},
	}

	// Create stats collector and orchestrator
	regStats := stats.NewRegistry()
	sc := regStats.Collector()
	orch, err := NewOrchestrator(failBackend, modelconfig.DefaultModels(), nil, nil, sc, nil)
 if err != nil {
 	t.Fatalf("orch: %v", err)
 }
	orch.SetAgentRegistry(reg)

	// Set up a memory store so SessionClose proceeds
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ms := agents.NewMemoryStore(s, "test-machine")
	orch.SetMemoryStore(ms)

	// Call SessionClose which should hit the summarize error path
	_ = orch.SessionClose(context.Background())

	// Verify the counter was recorded
	snap := regStats.Snapshot()
	var found bool
	for _, e := range snap.Records {
		if e.Metric == "agent.summary_errors" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected agent.summary_errors counter, not found in stats snapshot")
	}
}

// TestOrchestrator_SetRelayHub_NilSafe verifies that SetRelayHub(nil) does not panic.
func TestOrchestrator_SetRelayHub_NilSafe(t *testing.T) {
	t.Parallel()

	mb := &mockBackend{}
	orch, err := NewOrchestrator(mb, modelconfig.DefaultModels(), nil, nil, stats.NoopCollector{}, nil)
 if err != nil {
 	t.Fatalf("orch: %v", err)
 }
	orch.SetRelayHub(nil) // must not panic
}

// TestOrchestrator_SetRelayHub_AcceptsHub verifies that SetRelayHub accepts a Hub instance.
func TestOrchestrator_SetRelayHub_AcceptsHub(t *testing.T) {
	t.Parallel()

	mb := &mockBackend{}
	orch, err := NewOrchestrator(mb, modelconfig.DefaultModels(), nil, nil, stats.NoopCollector{}, nil)
 if err != nil {
 	t.Fatalf("orch: %v", err)
 }
	hub := relay.NewInProcessHub()
	orch.SetRelayHub(hub) // must not panic
}
