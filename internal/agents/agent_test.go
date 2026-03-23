package agents_test

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/backend"
)

func TestAgent_SwapModel_ThreadSafe(t *testing.T) {
	a := &agents.Agent{Name: "Chris", ModelID: "old-model"}
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			a.SwapModel("new-model")
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	if a.GetModelID() != "new-model" {
		t.Errorf("expected new-model, got %s", a.GetModelID())
	}
}

func TestAgent_DelegationContext_TrimsToSix(t *testing.T) {
	a := &agents.Agent{Name: "Chris"}
	for i := 0; i < 10; i++ {
		a.AppendHistory(backend.Message{Role: "user", Content: "msg"})
	}
	ctx := a.DelegationContext()
	if len(ctx) != 6 {
		t.Errorf("expected 6 messages, got %d", len(ctx))
	}
}

func TestAgent_DelegationContext_FewerThanSix(t *testing.T) {
	a := &agents.Agent{Name: "Chris"}
	a.AppendHistory(backend.Message{Role: "user", Content: "hello"})
	ctx := a.DelegationContext()
	if len(ctx) != 1 {
		t.Errorf("expected 1, got %d", len(ctx))
	}
}

func TestAgent_AppendHistory_TrimsTo20(t *testing.T) {
	a := &agents.Agent{Name: "Chris"}
	for i := 0; i < 25; i++ {
		a.AppendHistory(backend.Message{Role: "user", Content: "msg"})
	}
	if a.HistoryLen() > 20 {
		t.Errorf("history exceeded 20: %d", a.HistoryLen())
	}
}

func TestAgentRegistry_ByName_CaseInsensitive(t *testing.T) {
	reg := agents.NewRegistry()
	a := &agents.Agent{Name: "Chris"}
	reg.Register(a)

	got, ok := reg.ByName("chris")
	if !ok {
		t.Fatal("expected to find 'chris'")
	}
	if got.Name != "Chris" {
		t.Errorf("expected Chris, got %s", got.Name)
	}

	got2, ok2 := reg.ByName("CHRIS")
	if !ok2 || got2.Name != "Chris" {
		t.Error("CHRIS lookup failed")
	}
}

func TestAgentRegistry_BySlot(t *testing.T) {
	reg := agents.NewRegistry()
	a := &agents.Agent{Name: "Steve"}
	reg.Register(a)

	got, ok := reg.ByName("Steve")
	if !ok {
		t.Fatal("expected to find agent Steve")
	}
	if got.Name != "Steve" {
		t.Errorf("expected Steve, got %s", got.Name)
	}
}

func TestAgentRegistry_All(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Chris"})
	reg.Register(&agents.Agent{Name: "Steve"})
	all := reg.All()
	if len(all) != 2 {
		t.Errorf("expected 2, got %d", len(all))
	}
}

func TestAgentRegistry_ByName_NotFound(t *testing.T) {
	reg := agents.NewRegistry()
	_, ok := reg.ByName("nobody")
	if ok {
		t.Error("expected not found")
	}
}

func TestRegistry_Unregister_RemovesAgent(t *testing.T) {
	reg := agents.NewRegistry()
	ag := &agents.Agent{Name: "Steve", ModelID: "test:7b"}
	reg.Register(ag)
	reg.Unregister("Steve")
	if _, ok := reg.ByName("Steve"); ok {
		t.Error("ByName(Steve) should return false after Unregister")
	}
}

func TestRegistry_Unregister_CaseInsensitive(t *testing.T) {
	reg := agents.NewRegistry()
	ag := &agents.Agent{Name: "Steve", ModelID: "test:7b"}
	reg.Register(ag)
	reg.Unregister("STEVE") // uppercase
	if _, ok := reg.ByName("Steve"); ok {
		t.Error("ByName(Steve) should return false after Unregister with STEVE")
	}
}

func TestRegistry_Unregister_NoOp_Unknown(t *testing.T) {
	reg := agents.NewRegistry()
	// Should not panic
	reg.Unregister("NonExistent")
}

func TestRegistry_Unregister_PreservesNewAgentSlot(t *testing.T) {
	reg := agents.NewRegistry()
	// Register Steve
	steve := &agents.Agent{Name: "Steve", ModelID: "test:7b"}
	reg.Register(steve)
	// Register Steven
	steven := &agents.Agent{Name: "Steven", ModelID: "test:7b"}
	reg.Register(steven)
	// Unregister Steve — Steven should still be in the registry
	reg.Unregister("Steve")
	got, ok := reg.ByName("Steven")
	if !ok {
		t.Fatal("ByName(Steven) should still return Steven after Unregistering Steve")
	}
	if got.Name != "Steven" {
		t.Errorf("expected Steven, got %q", got.Name)
	}
}

func TestAgentRegistry_DefaultAgent_ReturnsIsDefaultAgent(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Alpha", IsDefault: false})
	reg.Register(&agents.Agent{Name: "Beta", IsDefault: true})
	ag := reg.DefaultAgent()
	if ag == nil || ag.Name != "Beta" {
		t.Errorf("expected Beta, got %v", ag)
	}
}

func TestAgentRegistry_DefaultAgent_FallsBackToFirst(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Alpha", IsDefault: false})
	ag := reg.DefaultAgent()
	if ag == nil {
		t.Error("expected fallback agent, got nil")
	}
}

func TestAgentRegistry_DefaultAgent_EmptyRegistryReturnsNil(t *testing.T) {
	reg := agents.NewRegistry()
	if reg.DefaultAgent() != nil {
		t.Error("expected nil from empty registry")
	}
}

func TestAgentRegistry_SetDefault_SwitchesMark(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Alpha", IsDefault: true})
	reg.Register(&agents.Agent{Name: "Beta", IsDefault: false})
	reg.SetDefault("Beta")
	ag := reg.DefaultAgent()
	if ag == nil || ag.Name != "Beta" {
		t.Errorf("expected Beta after SetDefault, got %v", ag)
	}
	alpha, _ := reg.ByName("Alpha")
	if alpha.IsDefault {
		t.Error("Alpha should no longer be IsDefault after SetDefault(Beta)")
	}
}

func TestAgentRegistry_SetDefault_UnknownName_IsNoOp(t *testing.T) {
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Alpha", IsDefault: true})
	reg.SetDefault("DoesNotExist")
	ag := reg.DefaultAgent()
	if ag == nil || ag.Name != "Alpha" {
		t.Errorf("expected Alpha to remain default, got %v", ag)
	}
}

func TestFromDef_IsDefault(t *testing.T) {
	def := agents.AgentDef{Name: "X", IsDefault: true}
	ag := agents.FromDef(def)
	if !ag.IsDefault {
		t.Error("expected IsDefault=true")
	}
}

func TestBuildPersonaPromptWithRoster_AppendsRoster(t *testing.T) {
	ag := &agents.Agent{Name: "Alex"}
	result := agents.BuildPersonaPromptWithRoster(ag, "ctx", "Available team members:\n- Stacy")
	if !strings.Contains(result, "Your Team") {
		t.Error("expected 'Your Team' section in prompt")
	}
	if !strings.Contains(result, "Stacy") {
		t.Error("expected Stacy in prompt")
	}
	if !strings.Contains(result, "delegate_to_agent") {
		t.Error("expected delegation instruction in prompt")
	}
}

func TestBuildPersonaPromptWithRoster_EmptyRoster_UnchangedPrompt(t *testing.T) {
	ag := &agents.Agent{Name: "Alex"}
	base := agents.BuildPersonaPrompt(ag, "ctx")
	result := agents.BuildPersonaPromptWithRoster(ag, "ctx", "")
	if result != base {
		t.Error("expected prompt unchanged when roster is empty")
	}
}

func TestRegistry_Rename_UpdatesMapKey(t *testing.T) {
	reg := agents.NewAgentRegistry()
	ag := &agents.Agent{Name: "Chris", }
	reg.Register(ag)

	ag.Rename(reg, "Christopher")

	// New name must be found
	found, ok := reg.ByName("christopher")
	if !ok || found == nil {
		t.Fatal("expected agent findable by new name")
	}
	// Old name must be gone
	_, oldOk := reg.ByName("chris")
	if oldOk {
		t.Fatal("expected old name to be removed from registry")
	}
	// Name field must be updated
	if found.Name != "Christopher" {
		t.Errorf("expected Name=Christopher, got %q", found.Name)
	}
}

func TestRegistry_DefaultAgent_Deterministic(t *testing.T) {
	// Ensure DefaultAgent returns the same agent consistently when none is marked default
	reg := agents.NewAgentRegistry()
	reg.Register(&agents.Agent{Name: "Zara", })
	reg.Register(&agents.Agent{Name: "Alice", })
	reg.Register(&agents.Agent{Name: "Bob", })

	var results []string
	for i := 0; i < 20; i++ {
		ag := reg.DefaultAgent()
		if ag == nil {
			t.Fatal("expected non-nil agent")
		}
		results = append(results, ag.Name)
	}
	for _, r := range results[1:] {
		if r != results[0] {
			t.Errorf("DefaultAgent non-deterministic: got %v", results)
			break
		}
	}
	if results[0] != "Alice" {
		t.Errorf("expected first agent alphabetically (Alice), got %q", results[0])
	}
}
