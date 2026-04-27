package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/repo"
	"github.com/scrypster/huginn/internal/stats"
	"github.com/scrypster/huginn/internal/tools"
)

// ---------------------------------------------------------------------------
// buildAgentSystemPrompt
// ---------------------------------------------------------------------------

func TestBuildAgentSystemPrompt_NonEmpty(t *testing.T) {
	reg := tools.NewRegistry()
	prompt := buildAgentSystemPrompt("", "", reg, "", "", "", "", "", "", "")
	if prompt == "" {
		t.Fatal("expected non-empty prompt")
	}
}

func TestBuildAgentSystemPrompt_ContainsKeywords(t *testing.T) {
	reg := tools.NewRegistry()
	prompt := buildAgentSystemPrompt("", "", reg, "", "", "", "", "", "", "")
	for _, kw := range []string{"Huginn", "tools", "assistant"} {
		if !strings.Contains(prompt, kw) {
			t.Errorf("expected keyword %q in prompt, got:\n%s", kw, prompt)
		}
	}
}

func TestBuildAgentSystemPrompt_WithContextText(t *testing.T) {
	reg := tools.NewRegistry()
	ctxText := "## Repository Structure\nsome/path/\n"
	prompt := buildAgentSystemPrompt(ctxText, "", reg, "", "", "", "", "", "", "")
	if !strings.Contains(prompt, ctxText) {
		t.Errorf("expected context text to appear in prompt")
	}
}

func TestBuildAgentSystemPrompt_EmptyContextText(t *testing.T) {
	reg := tools.NewRegistry()
	// With empty context text the two-newline suffix is NOT appended.
	prompt := buildAgentSystemPrompt("", "", reg, "", "", "", "", "", "", "")
	// The prompt should still be a valid non-empty string.
	if len(prompt) == 0 {
		t.Error("expected non-empty prompt even without context text")
	}
}

func TestBuildAgentSystemPrompt_NilRegistry(t *testing.T) {
	// Registry parameter is documented as unused (blank identifier in source),
	// so nil should be accepted without panic.
	prompt := buildAgentSystemPrompt("context", "", nil, "", "", "", "", "", "", "")
	if prompt == "" {
		t.Fatal("expected non-empty prompt with nil registry")
	}
}

// ---------------------------------------------------------------------------
// ContextBuilder.Build branches
// ---------------------------------------------------------------------------

// mockStatsCollector captures calls so we can assert they happened.
type mockStatsCollector struct {
	records []string
}

func (m *mockStatsCollector) Record(metric string, value float64, tags ...string) {
	m.records = append(m.records, metric)
}
func (m *mockStatsCollector) Histogram(metric string, value float64, tags ...string) {}

func TestContextBuilder_Build_NilIndex(t *testing.T) {
	cb := NewContextBuilder(nil, nil, stats.NoopCollector{})
	result := cb.Build("some query", "test-model")
	// With nil index there are no chunks or tree — result should be empty.
	if result != "" {
		t.Errorf("expected empty result for nil index, got %q", result)
	}
}

func TestContextBuilder_Build_NilIndexNilRegistry(t *testing.T) {
	cb := NewContextBuilder(nil, nil, nil)
	// nil stats should not panic.
	result := cb.Build("query", "test-model")
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
}

func TestContextBuilder_Build_WithIndex(t *testing.T) {
	idx := &repo.Index{
		Root: "/tmp",
		Chunks: []repo.FileChunk{
			{Path: "main.go", Content: "package main\nfunc main(){}", StartLine: 1},
		},
	}
	sc := &mockStatsCollector{}
	cb := NewContextBuilder(idx, nil, sc)
	result := cb.Build("main", "test-model")
	if !strings.Contains(result, "Repository") {
		t.Errorf("expected repository structure or context in result, got: %q", result)
	}
	if len(sc.records) == 0 {
		t.Error("expected stats.Record to be called")
	}
}

func TestContextBuilder_Build_EmptyQuery(t *testing.T) {
	idx := &repo.Index{
		Root: "/tmp",
		Chunks: []repo.FileChunk{
			{Path: "foo.go", Content: "package foo", StartLine: 1},
		},
	}
	cb := NewContextBuilder(idx, nil, stats.NoopCollector{})
	// Empty query: should still build the tree but skip chunk scoring.
	result := cb.Build("", "test-model")
	if !strings.Contains(result, "Repository Structure") {
		t.Errorf("expected tree in output for empty query, got: %q", result)
	}
}

func TestContextBuilder_Build_WithRegistryContextWindow(t *testing.T) {
	models := &modelconfig.Models{
		Reasoner: "model-c",
	}
	reg := modelconfig.NewRegistry(models)
	// Inject a model with a known context window so the budget branch executes.
	reg.Available = []modelconfig.ModelInfo{
		{Name: "model-b", ContextWindow: 8192, SupportsTools: true},
	}
	cb := NewContextBuilder(nil, reg, stats.NoopCollector{})
	// Should not panic; the budget calculation branch should fire.
	result := cb.Build("query", "test-model")
	_ = result
}

func TestContextBuilder_Build_RegistryZeroContextWindow(t *testing.T) {
	models := &modelconfig.Models{Reasoner: "m"}
	reg := modelconfig.NewRegistry(models)
	// No Available entries → SlotContextWindow returns 0 → fallback to defaultContextBytes.
	cb := NewContextBuilder(nil, reg, stats.NoopCollector{})
	result := cb.Build("query", "test-model")
	_ = result
}

// ---------------------------------------------------------------------------
// ContextBuilder.BuildWithSymbols
// ---------------------------------------------------------------------------

func TestContextBuilder_BuildWithSymbols_NoSymbols(t *testing.T) {
	cb := NewContextBuilder(nil, nil, stats.NoopCollector{})
	// No symbol refs → result is identical to Build.
	base := cb.Build("query", "test-model")
	withSymbols := cb.BuildWithSymbols("query", "test-model", nil)
	if base != withSymbols {
		t.Errorf("expected BuildWithSymbols with nil refs == Build, got diff")
	}
}

func TestContextBuilder_BuildWithSymbols_EmptySlice(t *testing.T) {
	cb := NewContextBuilder(nil, nil, stats.NoopCollector{})
	base := cb.Build("query", "test-model")
	withSymbols := cb.BuildWithSymbols("query", "test-model", []string{})
	if base != withSymbols {
		t.Errorf("expected BuildWithSymbols with empty refs == Build")
	}
}

func TestContextBuilder_BuildWithSymbols_WithRefs(t *testing.T) {
	cb := NewContextBuilder(nil, nil, stats.NoopCollector{})
	refs := []string{"pkg.Foo", "pkg.Bar"}
	result := cb.BuildWithSymbols("query", "test-model", refs)
	if !strings.Contains(result, "Referenced Symbols") {
		t.Error("expected 'Referenced Symbols' section in output")
	}
	for _, ref := range refs {
		if !strings.Contains(result, ref) {
			t.Errorf("expected symbol ref %q in output", ref)
		}
	}
}

func TestContextBuilder_BuildWithSymbols_SingleRef(t *testing.T) {
	cb := NewContextBuilder(nil, nil, stats.NoopCollector{})
	result := cb.BuildWithSymbols("q", "test-model", []string{"mypackage.MyFunc"})
	if !strings.Contains(result, "mypackage.MyFunc") {
		t.Error("expected symbol ref in output")
	}
}

func newTestModels() *modelconfig.Models {
	return &modelconfig.Models{
		Reasoner: "test-model",
	}
}

// ---------------------------------------------------------------------------
// Orchestrator.Iterate
// ---------------------------------------------------------------------------

func TestOrchestrator_Iterate_Success(t *testing.T) {
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{Content: "refined v1", DoneReason: "stop"},
		},
	}
	o := mustNewOrchestrator(t, mb, newTestModels(), nil, nil, nil, nil)

	var tokens []string
	err := o.Iterate(context.Background(), 1, "harden auth", func(tok string) {
		tokens = append(tokens, tok)
	})
	if err != nil {
		t.Fatalf("Iterate: %v", err)
	}
	if o.CurrentState() != StateIdle {
		t.Errorf("expected StateIdle after Iterate, got %d", o.CurrentState())
	}
	combined := strings.Join(tokens, "")
	if combined == "" {
		t.Error("expected at least one token from Iterate")
	}
}

func TestOrchestrator_Iterate_MultipleRounds(t *testing.T) {
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{Content: "round1", DoneReason: "stop"},
			{Content: "round2", DoneReason: "stop"},
			{Content: "round3", DoneReason: "stop"},
		},
	}
	o := mustNewOrchestrator(t, mb, newTestModels(), nil, nil, nil, nil)

	var last string
	err := o.Iterate(context.Background(), 3, "section", func(tok string) {
		last = tok
	})
	if err != nil {
		t.Fatalf("Iterate 3 rounds: %v", err)
	}
	// onToken is called once at the end with the final accumulated result.
	if last == "" {
		t.Error("expected final token from 3-round iterate")
	}
}

func TestOrchestrator_Iterate_BackendError(t *testing.T) {
	mb := &mockBackend{
		errors: []error{errors.New("reasoner down")},
	}
	o := mustNewOrchestrator(t, mb, newTestModels(), nil, nil, nil, nil)

	err := o.Iterate(context.Background(), 1, "section", nil)
	if err == nil {
		t.Fatal("expected error from Iterate")
	}
	// State should be reset to Idle by the defer.
	if o.CurrentState() != StateIdle {
		t.Errorf("expected StateIdle after Iterate error, got %d", o.CurrentState())
	}
}

func TestOrchestrator_Iterate_NilOnToken(t *testing.T) {
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{Content: "result", DoneReason: "stop"},
		},
	}
	o := mustNewOrchestrator(t, mb, newTestModels(), nil, nil, nil, nil)
	// nil onToken must not panic.
	if err := o.Iterate(context.Background(), 1, "section", nil); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------------
// Orchestrator.SetTools
// ---------------------------------------------------------------------------

func TestOrchestrator_SetTools_SetsFields(t *testing.T) {
	o := mustNewOrchestrator(t, &mockBackend{}, newTestModels(), nil, nil, nil, nil)
	reg := tools.NewRegistry()
	gate := permissions.NewGate(true, nil)

	o.SetTools(reg, gate)

	o.mu.Lock()
	defer o.mu.Unlock()
	if o.toolRegistry != reg {
		t.Error("expected toolRegistry to be set")
	}
	if o.permGate != gate {
		t.Error("expected permGate to be set")
	}
}

func TestOrchestrator_SetTools_NilValues(t *testing.T) {
	o := mustNewOrchestrator(t, &mockBackend{}, newTestModels(), nil, nil, nil, nil)
	// Should not panic with nil args.
	o.SetTools(nil, nil)

	o.mu.Lock()
	defer o.mu.Unlock()
	if o.toolRegistry != nil {
		t.Error("expected nil toolRegistry")
	}
	if o.permGate != nil {
		t.Error("expected nil permGate")
	}
}

func TestOrchestrator_SetTools_Idempotent(t *testing.T) {
	o := mustNewOrchestrator(t, &mockBackend{}, newTestModels(), nil, nil, nil, nil)
	reg1 := tools.NewRegistry()
	reg2 := tools.NewRegistry()
	gate1 := permissions.NewGate(false, nil)
	gate2 := permissions.NewGate(true, nil)

	o.SetTools(reg1, gate1)
	o.SetTools(reg2, gate2)

	o.mu.Lock()
	defer o.mu.Unlock()
	if o.toolRegistry != reg2 {
		t.Error("expected second registry to win")
	}
	if o.permGate != gate2 {
		t.Error("expected second gate to win")
	}
}

// ---------------------------------------------------------------------------
// Orchestrator.AgentChat
// ---------------------------------------------------------------------------

func TestOrchestrator_AgentChat_FallsBackToChat_NoRegistry(t *testing.T) {
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{Content: "chat response", DoneReason: "stop"},
		},
	}
	o := mustNewOrchestrator(t, mb, newTestModels(), nil, nil, nil, nil)
	// No SetTools called → toolRegistry is nil → falls back to plain Chat.
	var tokens []string
	err := o.AgentChat(context.Background(), "hello", 5,
		func(tok string) { tokens = append(tokens, tok) },
		nil, nil, nil,
		nil, nil,
	)
	if err != nil {
		t.Fatalf("AgentChat: %v", err)
	}
	if o.CurrentState() != StateIdle {
		t.Errorf("expected StateIdle, got %d", o.CurrentState())
	}
	if len(tokens) == 0 {
		t.Error("expected onToken called")
	}
}

func TestOrchestrator_AgentChat_WithRegistry_StopsOnResponse(t *testing.T) {
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{Content: "agent response", DoneReason: "stop"},
		},
	}
	o := mustNewOrchestrator(t, mb, newTestModels(), nil, nil, nil, nil)
	reg := tools.NewRegistry()
	gate := permissions.NewGate(true, nil)
	o.SetTools(reg, gate)

	var tokens []string
	err := o.AgentChat(context.Background(), "do something", 5,
		func(tok string) { tokens = append(tokens, tok) },
		nil, nil, nil,
		nil, nil,
	)
	if err != nil {
		t.Fatalf("AgentChat: %v", err)
	}
	if o.CurrentState() != StateIdle {
		t.Errorf("expected StateIdle, got %d", o.CurrentState())
	}
}

func TestOrchestrator_AgentChat_WithRegistry_ToolExecution(t *testing.T) {
	tool := &mockTool{
		name:   "echo",
		result: tools.ToolResult{Output: "echoed"},
	}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("echo", "call-1"),
			{Content: "done", DoneReason: "stop"},
		},
	}
	reg := newRegistryWith(tool)
	gate := permissions.NewGate(true, nil)

	o := mustNewOrchestrator(t, mb, newTestModels(), nil, nil, nil, nil)
	o.SetTools(reg, gate)

	var toolCalled string
	err := o.AgentChat(context.Background(), "use echo tool", 5,
		nil,
		func(_ string, name string, args map[string]any) { toolCalled = name },
		nil,
		nil,
		nil, nil,
	)
	if err != nil {
		t.Fatalf("AgentChat: %v", err)
	}
	if toolCalled != "echo" {
		t.Errorf("expected tool 'echo' to be called, got %q", toolCalled)
	}
}

func TestOrchestrator_AgentChat_FallsBackToChat_NoToolSupport(t *testing.T) {
	// Build a registry with a model that does NOT support tools.
	models := &modelconfig.Models{Reasoner: "no-tools"}
	reg := modelconfig.NewRegistry(models)
	reg.Available = []modelconfig.ModelInfo{
		{Name: "no-tools", ContextWindow: 4096, SupportsTools: false},
	}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{Content: "fallback chat response", DoneReason: "stop"},
		},
	}
	o := mustNewOrchestrator(t, mb, models, nil, reg, nil, nil)

	toolReg := tools.NewRegistry()
	gate := permissions.NewGate(true, nil)
	o.SetTools(toolReg, gate)

	err := o.AgentChat(context.Background(), "hello", 5, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("AgentChat fallback: %v", err)
	}
}

func TestOrchestrator_AgentChat_HistoryAppended(t *testing.T) {
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{Content: "response text", DoneReason: "stop"},
		},
	}
	o := mustNewOrchestrator(t, mb, newTestModels(), nil, nil, nil, nil)
	reg := tools.NewRegistry()
	o.SetTools(reg, permissions.NewGate(true, nil))

	if err := o.AgentChat(context.Background(), "msg", 5, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	o.mu.Lock()
	histLen := len(o.defaultSession().history)
	o.mu.Unlock()

	if histLen == 0 {
		t.Error("expected history to be populated after AgentChat")
	}
}

func TestOrchestrator_AgentChat_PermissionDenied(t *testing.T) {
	writeTool := &mockTool{
		name:   "write_tool",
		result: tools.ToolResult{Output: "written"},
	}
	// Override permission level to PermWrite so Gate.Check gets invoked.
	// mockTool returns PermRead by default; we need a custom one.
	permTool := &permWriteTool{name: "write_tool", result: tools.ToolResult{Output: "written"}}

	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			toolCallResponse("write_tool", "call-perm"),
			{Content: "handled", DoneReason: "stop"},
		},
	}
	reg := newRegistryWith(permTool)
	// Gate with no promptFunc → denies non-read tools.
	gate := permissions.NewGate(false, nil)

	o := mustNewOrchestrator(t, mb, newTestModels(), nil, nil, nil, nil)
	o.SetTools(reg, gate)
	_ = writeTool // suppress unused warning

	var denied string
	err := o.AgentChat(context.Background(), "write something", 5,
		nil, nil, nil,
		func(name string) { denied = name },
		nil, nil,
	)
	if err != nil {
		t.Fatalf("AgentChat: %v", err)
	}
	if denied != "write_tool" {
		t.Errorf("expected onPermDenied for 'write_tool', got %q", denied)
	}
}

// permWriteTool is a tool that reports PermWrite so the gate can deny it.
type permWriteTool struct {
	name   string
	result tools.ToolResult
}

func (p *permWriteTool) Name() string                      { return p.name }
func (p *permWriteTool) Description() string               { return "" }
func (p *permWriteTool) Permission() tools.PermissionLevel { return tools.PermWrite }
func (p *permWriteTool) Schema() backend.Tool {
	return backend.Tool{Function: backend.ToolFunction{Name: p.name}}
}
func (p *permWriteTool) Execute(_ context.Context, _ map[string]any) tools.ToolResult {
	return p.result
}
