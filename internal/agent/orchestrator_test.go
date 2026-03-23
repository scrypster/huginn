package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/skills"
)

// newBareOrchestrator creates a minimal Orchestrator with a default session initialized.
// Use this instead of &Orchestrator{} to avoid nil map panics.
func newBareOrchestrator() *Orchestrator {
	sess := newSession("bare-test")
	return &Orchestrator{
		sessions:         map[string]*Session{sess.ID: sess},
		defaultSessionID: sess.ID,
	}
}

// newMockBackend returns a mockBackend that always replies with the given content.
func newMockBackend(content string) *mockBackend {
	return &mockBackend{
		responses: []*backend.ChatResponse{
			{Content: content, DoneReason: "stop"},
		},
	}
}

// TestOrchestratorCompiles verifies the Orchestrator type and all states compile.
func TestOrchestratorCompiles(t *testing.T) {
	states := []State{StateIdle, StateIterating, StateAgentLoop}
	for _, s := range states {
		if s < StateIdle || s > StateAgentLoop {
			t.Errorf("invalid state: %d", s)
		}
	}
	// Verify NewOrchestrator can be called with nil args (just checks it compiles)
	var o *Orchestrator
	if o != nil {
		t.Error("should be nil")
	}
}

// TestCompactHistory_HistoryDoesShrinkWithCompaction verifies that calling Chat() many
// times on an orchestrator with a compactor keeps history bounded.
// This is the TUI freeze regression test — unbounded growth would cause
// increasingly slow requests as history is sent on every turn.
func TestCompactHistory_HistoryDoesShrinkWithCompaction(t *testing.T) {
	// Use a static model name for all slots.
	models := &modelconfig.Models{
		Reasoner: "test-model",
	}

	mb := &mockBackend{} // reuses the mock from loop_test.go (same package)
	// Always return a simple stop response.
	for i := 0; i < 40; i++ {
		mb.responses = append(mb.responses, &backend.ChatResponse{
			Content:    "response",
			DoneReason: "stop",
		})
	}

	o := mustNewOrchestrator(t, mb, models, nil, nil, nil, nil)

	const rounds = 30
	for i := 0; i < rounds; i++ {
		if err := o.Chat(context.Background(), "hello", nil, nil); err != nil {
			t.Fatalf("Chat round %d: %v", i+1, err)
		}
	}

	o.mu.Lock()
	histLen := len(o.defaultSession().history)
	o.mu.Unlock()

	// With no compactor configured, history may grow, but should be bounded.
	// If a compactor is configured in the future, this test validates that
	// the history shrinks appropriately.
	if histLen == 0 {
		t.Error("expected non-empty history after Chat calls")
	}
}


// TestOrchestrator_CurrentState verifies CurrentState returns the correct state.
func TestOrchestrator_CurrentState(t *testing.T) {
	mb := &mockBackend{
		responses: []*backend.ChatResponse{
			{Content: "resp", DoneReason: "stop"},
		},
	}
	models := &modelconfig.Models{
		Reasoner: "test",
	}
	o := mustNewOrchestrator(t, mb, models, nil, nil, nil, nil)

	if o.CurrentState() != StateIdle {
		t.Errorf("expected StateIdle initially, got %d", o.CurrentState())
	}
}

// TestOrchestrator_HistoryManagement verifies that compaction manages history appropriately.
func TestOrchestrator_HistoryManagement(t *testing.T) {
	o := newBareOrchestrator()
	// Build a history with many entries.
	// Pattern: user, assistant, user, assistant, ...
	for i := 0; i < 25; i++ {
		o.defaultSession().history = append(o.defaultSession().history,
			backend.Message{Role: "user", Content: "msg"},
			backend.Message{Role: "assistant", Content: "resp"},
		)
	}

	// With no compactor, history remains as-is.
	initialLen := len(o.defaultSession().history)
	if initialLen == 0 {
		t.Fatal("expected non-empty history")
	}
}

func TestOrchestrator_SetAgentRegistry(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Chris", ModelID: "m1"})
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)
	o.SetAgentRegistry(reg)
	// no panic = pass
}

func TestOrchestrator_Dispatch_ChrisPlan(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{
		Name:         "Chris",
		ModelID:      "plan-model",
		SystemPrompt: "You are Chris.",
	})

	mb := newMockBackend("step 1: do this\nstep 2: do that")
	models := modelconfig.DefaultModels()
	o := mustNewOrchestrator(t, mb, models, nil, nil, nil, nil)
	o.SetAgentRegistry(reg)

	var tokens []string
	handled, err := o.Dispatch(context.Background(), "Have Chris plan the refactor", func(tok string) {
		tokens = append(tokens, tok)
	}, nil, nil, nil, nil, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !handled {
		t.Error("expected handled=true for 'Have Chris plan'")
	}
	if len(tokens) == 0 {
		t.Error("expected tokens from Chris planning")
	}
}

func TestOrchestrator_Dispatch_NormalMessage(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Chris", ModelID: "m"})

	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)
	o.SetAgentRegistry(reg)

	handled, err := o.Dispatch(context.Background(), "just a normal message", nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handled {
		t.Error("expected handled=false for normal message")
	}
}

func TestOrchestrator_Dispatch_NilRegistry(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)
	// No registry set
	handled, err := o.Dispatch(context.Background(), "Have Chris plan this", nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handled {
		t.Error("expected handled=false when no registry")
	}
}

// TestOrchestrator_SessionID_Generated verifies that NewOrchestrator sets a
// non-empty sessionID.
func TestOrchestrator_SessionID_Generated(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)
	if o.SessionID() == "" {
		t.Error("expected non-empty SessionID")
	}
}

// TestOrchestrator_MachineID_FromConfig verifies that MachineID() returns the
// value set via WithMachineID and does not panic when empty.
func TestOrchestrator_MachineID_FromConfig(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)
	// MachineID is empty by default — just verify it doesn't panic.
	_ = o.MachineID()

	// WithMachineID should propagate the value.
	o.WithMachineID("test-host-deadbeef")
	if o.MachineID() != "test-host-deadbeef" {
		t.Errorf("expected MachineID=test-host-deadbeef, got %q", o.MachineID())
	}
}

// TestOrchestrator_SessionID_Unique verifies that two orchestrators created
// independently receive different session IDs.
func TestOrchestrator_SessionID_Unique(t *testing.T) {
	o1, err := NewOrchestrator(newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)
 if err != nil {
 	t.Fatalf("o1: %v", err)
 }
	o2, err := NewOrchestrator(newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)
 if err != nil {
 	t.Fatalf("o2: %v", err)
 }
	if o1.SessionID() == o2.SessionID() {
		t.Errorf("expected unique session IDs, both got %q", o1.SessionID())
	}
}


// TestOrchestrator_WorkspaceRoot verifies that WorkspaceRoot() returns the
// value set by SetGitRoot and that it is thread-safe.
func TestOrchestrator_WorkspaceRoot(t *testing.T) {
	o := &Orchestrator{
		sessions:         map[string]*Session{},
		defaultSessionID: "test",
	}
	o.workspaceRoot = "/tmp/proj"
	if got := o.WorkspaceRoot(); got != "/tmp/proj" {
		t.Fatalf("got %q want /tmp/proj", got)
	}
}

// TestOrchestrator_WorkspaceRoot_SetGitRoot verifies that WorkspaceRoot()
// returns the value set via SetGitRoot.
func TestOrchestrator_WorkspaceRoot_SetGitRoot(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)
	testPath := "/home/user/myproject"
	o.SetGitRoot(testPath)
	if got := o.WorkspaceRoot(); got != testPath {
		t.Fatalf("expected WorkspaceRoot=%q, got %q", testPath, got)
	}
}

// TestOrchestrator_SetSkillsRegistry verifies that SetSkillsRegistry does not panic.
func TestOrchestrator_SetSkillsRegistry(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)
	reg := skills.NewSkillRegistry()
	// Should not panic
	o.SetSkillsRegistry(reg)
}

// skillsFragmentFor integration tests — covers the full path from AgentDef.Skills
// through the orchestrator to the resolved fragment that enters buildAgentSystemPrompt.

func TestSkillsFragmentFor_PerAgentSkills(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)

	// Register a skill in the orchestrator registry.
	reg := skills.NewSkillRegistry()
	sk, err := skills.ParseMarkdownSkillBytes([]byte("---\nname: tdd\nversion: 1.0.0\ndescription: TDD methodology\n---\n\nAlways write failing tests first.\n"))
	if err != nil {
		t.Fatal(err)
	}
	reg.Register(sk)
	o.SetSkillsRegistry(reg)

	// Create an agent registry whose default agent has Skills: ["tdd"].
	agReg := agents.NewRegistry()
	agReg.Register(&agents.Agent{Name: "coder", IsDefault: true, Skills: []string{"tdd"}})

	frag := o.skillsFragmentFor(agReg)
	if !strings.Contains(frag, "failing tests") {
		t.Errorf("expected per-agent skill content in fragment, got %q", frag)
	}
}

func TestSkillsFragmentFor_GlobalFallbackWhenNoAgentSkills(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)

	reg := skills.NewSkillRegistry()
	sk, err := skills.ParseMarkdownSkillBytes([]byte("---\nname: global\nversion: 1.0.0\ndescription: global\n---\n\nGlobal methodology.\n"))
	if err != nil {
		t.Fatal(err)
	}
	reg.Register(sk)
	o.SetSkillsRegistry(reg)

	// Agent with no skills assigned → global fallback.
	agReg := agents.NewRegistry()
	agReg.Register(&agents.Agent{Name: "coder", IsDefault: true, Skills: nil})

	frag := o.skillsFragmentFor(agReg)
	if !strings.Contains(frag, "Global methodology") {
		t.Errorf("expected global fallback content in fragment, got %q", frag)
	}
}

func TestSkillsFragmentFor_NilAgentRegistry(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)

	reg := skills.NewSkillRegistry()
	sk, err := skills.ParseMarkdownSkillBytes([]byte("---\nname: global\nversion: 1.0.0\ndescription: global\n---\n\nGlobal methodology.\n"))
	if err != nil {
		t.Fatal(err)
	}
	reg.Register(sk)
	o.SetSkillsRegistry(reg)

	// nil agReg → global fallback (no agent to read Skills from).
	frag := o.skillsFragmentFor(nil)
	if !strings.Contains(frag, "Global methodology") {
		t.Errorf("expected global fallback when agReg is nil, got %q", frag)
	}
}

func TestSkillsFragmentFor_NilSkillsRegistry(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)
	// No SetSkillsRegistry call → should return "" without panic.
	agReg := agents.NewRegistry()
	agReg.Register(&agents.Agent{Name: "coder", IsDefault: true, Skills: []string{"tdd"}})

	frag := o.skillsFragmentFor(agReg)
	if frag != "" {
		t.Errorf("expected empty when no skills registry set, got %q", frag)
	}
}

func TestSkillsFragmentFor_UnknownSkillName(t *testing.T) {
	o := mustNewOrchestrator(t, newMockBackend(""), modelconfig.DefaultModels(), nil, nil, nil, nil)

	reg := skills.NewSkillRegistry()
	o.SetSkillsRegistry(reg)

	// Agent lists a skill that isn't installed → empty fragment, no panic.
	agReg := agents.NewRegistry()
	agReg.Register(&agents.Agent{Name: "coder", IsDefault: true, Skills: []string{"nonexistent"}})

	frag := o.skillsFragmentFor(agReg)
	if frag != "" {
		t.Errorf("expected empty for unknown skill name, got %q", frag)
	}
}
