package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/storage"
	"github.com/scrypster/huginn/internal/tools"
)

// ---------------------------------------------------------------------------
// Accessor coverage: SetSkillsFragment, Backend, ToolRegistry
// ---------------------------------------------------------------------------

func TestOrchestrator_SetSkillsFragment(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)
	o.SetSkillsFragment("## Skills\n- skill_a\n")
	// Build context to verify fragment is injected.
	cb := o.contextBuilder
	result := cb.Build("query", "test-model")
	if !strings.Contains(result, "skill_a") {
		t.Error("expected skills fragment to appear in context after SetSkillsFragment")
	}
}

func TestOrchestrator_Backend(t *testing.T) {
	mb := newMockBackend("")
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	if o.Backend() != mb {
		t.Error("Backend() should return the injected backend")
	}
}

func TestOrchestrator_ToolRegistry_Nil(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)
	if o.ToolRegistry() != nil {
		t.Error("expected nil ToolRegistry before SetTools")
	}
}

func TestOrchestrator_ToolRegistry_Set(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)
	reg := tools.NewRegistry()
	o.SetTools(reg, nil)
	if o.ToolRegistry() != reg {
		t.Error("ToolRegistry() should return the set registry")
	}
}

// ---------------------------------------------------------------------------
// ChatWithAgent
// ---------------------------------------------------------------------------

func TestOrchestrator_ChatWithAgent_Success(t *testing.T) {
	mb := newMockBackend("agent reply")
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)

	ag := &agents.Agent{
		Name:         "Chris",
		ModelID:      "test-model",
		SystemPrompt: "You are Chris, an architect.",
	}

	var tokens []string
	err := o.ChatWithAgent(context.Background(), ag, "build the feature", "", func(tok string) {
		tokens = append(tokens, tok)
	}, nil, nil)
	if err != nil {
		t.Fatalf("ChatWithAgent: %v", err)
	}
	if o.CurrentState() != StateIdle {
		t.Errorf("expected StateIdle after ChatWithAgent, got %d", o.CurrentState())
	}
	if len(tokens) == 0 {
		t.Error("expected onToken to be called")
	}

	// History should contain user+assistant.
	o.mu.Lock()
	histLen := len(o.defaultSession().history)
	o.mu.Unlock()
	if histLen < 2 {
		t.Errorf("expected at least 2 history entries, got %d", histLen)
	}
}

func TestOrchestrator_ChatWithAgent_BackendError(t *testing.T) {
	mb := &mockBackend{
		errors: []error{errors.New("agent chat down")},
	}
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)

	ag := &agents.Agent{Name: "Mark", ModelID: "m"}
	err := o.ChatWithAgent(context.Background(), ag, "help", "", nil, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "chat(Mark)") {
		t.Errorf("expected error to contain 'chat(Mark)', got %v", err)
	}
	if o.CurrentState() != StateIdle {
		t.Errorf("expected StateIdle after error, got %d", o.CurrentState())
	}
}

// TestOrchestrator_ChatWithAgent_NoModel verifies that ChatWithAgent returns a
// clear, actionable error when the agent has no model configured, rather than
// silently failing or returning a cryptic provider error.
// Regression: agents created without a model were passing "" as the model ID
// to the backend, causing a confusing auth/validation error from the LLM API.
func TestOrchestrator_ChatWithAgent_NoModel(t *testing.T) {
	mb := newMockBackend("should not be called")
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)

	ag := &agents.Agent{Name: "Mike", ModelID: ""}
	err := o.ChatWithAgent(context.Background(), ag, "hello", "", nil, nil, nil)
	if err == nil {
		t.Fatal("expected error when agent has no model")
	}
	if !strings.Contains(err.Error(), "Mike") {
		t.Errorf("expected error to mention agent name 'Mike', got: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "model") {
		t.Errorf("expected error to mention 'model', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ReasonWithAgent
// ---------------------------------------------------------------------------

func TestOrchestrator_ReasonWithAgent_Success(t *testing.T) {
	mb := newMockBackend("reasoning output")
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)

	ag := &agents.Agent{Name: "Mark", ModelID: "m"}
	var tokens []string
	err := o.ReasonWithAgent(context.Background(), ag, "analyze this", func(tok string) {
		tokens = append(tokens, tok)
	}, nil)
	if err != nil {
		t.Fatalf("ReasonWithAgent: %v", err)
	}
	if o.CurrentState() != StateIdle {
		t.Errorf("expected StateIdle, got %d", o.CurrentState())
	}
}

// ---------------------------------------------------------------------------
// CodeWithAgent
// ---------------------------------------------------------------------------

func TestOrchestrator_CodeWithAgent_NoToolsFallsBackToChat(t *testing.T) {
	mb := newMockBackend("code response")
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	// No SetTools → toolRegistry nil → falls back to ChatWithAgent.

	ag := &agents.Agent{Name: "Steve", ModelID: "m"}
	var tokens []string
	err := o.CodeWithAgent(context.Background(), ag, "implement X", 5,
		func(tok string) { tokens = append(tokens, tok) }, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("CodeWithAgent: %v", err)
	}
	if o.CurrentState() != StateIdle {
		t.Errorf("expected StateIdle, got %d", o.CurrentState())
	}
}

func TestOrchestrator_CodeWithAgent_WithTools(t *testing.T) {
	tool := &mockTool{name: "read_file", result: tools.ToolResult{Output: "file contents"}}
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{Content: "code output", DoneReason: "stop"},
		},
	}
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	reg := newRegistryWith(tool)
	gate := permissions.NewGate(true, nil)
	o.SetTools(reg, gate)

	ag := &agents.Agent{Name: "Steve", ModelID: "m"}
	err := o.CodeWithAgent(context.Background(), ag, "implement Y", 5, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("CodeWithAgent with tools: %v", err)
	}
	if o.CurrentState() != StateIdle {
		t.Errorf("expected StateIdle, got %d", o.CurrentState())
	}
}

// ---------------------------------------------------------------------------
// loadAgentSummaries
// ---------------------------------------------------------------------------

func TestOrchestrator_loadAgentSummaries_NilStore(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)
	// No memory store set → returns nil.
	result := o.loadAgentSummaries(context.Background(), "Chris")
	if result != nil {
		t.Errorf("expected nil for nil store, got %v", result)
	}
}

func TestOrchestrator_loadAgentSummaries_WithStore(t *testing.T) {
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	ms := agents.NewMemoryStore(s, "test-machine")
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)
	o.SetMemoryStore(ms)
	o.WithMachineID("test-machine")

	// No summaries stored → should return empty slice, not error.
	result := o.loadAgentSummaries(context.Background(), "Chris")
	if len(result) != 0 {
		t.Errorf("expected empty summaries, got %d", len(result))
	}
}

// ---------------------------------------------------------------------------
// Dispatch with maxTurnsPtr
// ---------------------------------------------------------------------------

func TestOrchestrator_Dispatch_WithMaxTurns(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:    "Chris",
		ModelID: "test-model",
	})

	mb := newMockBackend("planned")
	o := mustNewOrchestrator(t, mb, modelconfig.DefaultModels(), nil, nil, nil, nil)
	o.SetAgentRegistry(reg)

	maxTurns := 10
	handled, err := o.Dispatch(context.Background(), "Have Chris plan the migration",
		func(string) {}, nil, nil, nil, &maxTurns, nil)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !handled {
		t.Error("expected handled=true")
	}
}

// ---------------------------------------------------------------------------
// ContextBuilder with skills fragment
// ---------------------------------------------------------------------------

func TestContextBuilder_SetSkillsFragment(t *testing.T) {
	cb := NewContextBuilder(nil, nil, nil)
	cb.SetSkillsFragment("rule: always test")
	result := cb.Build("query", "test-model")
	if !strings.Contains(result, "Skills & Workspace Rules") {
		t.Error("expected Skills section in output")
	}
	if !strings.Contains(result, "rule: always test") {
		t.Error("expected skills fragment content")
	}
}

// ---------------------------------------------------------------------------
// summarizeAgent JSON edge cases
// ---------------------------------------------------------------------------

func TestSummarizeAgent_MalformedJSON(t *testing.T) {
	mb := newMockBackend("not valid json at all")
	models := &modelconfig.Models{Reasoner: "m"}
	o := mustNewOrchestrator(t, mb, models, nil, nil, nil, nil)

	ms := openTestMemoryStore(t, "test-machine")
	o.SetMemoryStore(ms)

	reg := agents.NewRegistry()
	ag := makeTestAgent("BadJSON", "m")
	ag.AppendHistory(backend.Message{Role: "user", Content: "work"})
	reg.Register(ag)
	o.SetAgentRegistry(reg)

	// SessionClose should not return an error even with bad JSON.
	err := o.SessionClose(context.Background())
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}

	// No summary should be stored.
	summaries, _ := ms.LoadRecentSummaries(context.Background(), "BadJSON", 5)
	if len(summaries) != 0 {
		t.Errorf("expected 0 summaries for malformed JSON, got %d", len(summaries))
	}
}

func TestSummarizeAgent_MarkdownFencedJSON(t *testing.T) {
	// Some models wrap JSON in markdown fences.
	fenced := "```json\n" + buildSummaryJSON("fenced summary", "f.go", "dec1", "q1") + "\n```"
	mb := newMockBackend(fenced)
	models := &modelconfig.Models{Reasoner: "m"}
	o := mustNewOrchestrator(t, mb, models, nil, nil, nil, nil)

	ms := openTestMemoryStore(t, "test-machine")
	o.SetMemoryStore(ms)

	reg := agents.NewRegistry()
	ag := makeTestAgent("Fenced", "m")
	ag.AppendHistory(backend.Message{Role: "user", Content: "work"})
	reg.Register(ag)
	o.SetAgentRegistry(reg)

	_ = o.SessionClose(context.Background())

	summaries, _ := ms.LoadRecentSummaries(context.Background(), "Fenced", 5)
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].Summary != "fenced summary" {
		t.Errorf("expected 'fenced summary', got %q", summaries[0].Summary)
	}
}
