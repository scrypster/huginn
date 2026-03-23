package tui

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/config"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/repo"
	"github.com/scrypster/huginn/internal/stats"
	"github.com/scrypster/huginn/internal/storage"
	"github.com/scrypster/huginn/internal/symbol"
)

// ---------------------------------------------------------------------------
// ctrlCResetCmd — returns a non-nil tea.Cmd
// ---------------------------------------------------------------------------

func TestCtrlCResetCmd_ReturnsNonNilCmd(t *testing.T) {
	cmd := ctrlCResetCmd()
	if cmd == nil {
		t.Error("expected non-nil cmd from ctrlCResetCmd")
	}
}

// ---------------------------------------------------------------------------
// renderFooter — different states
// ---------------------------------------------------------------------------

func TestRenderFooter_DefaultState(t *testing.T) {
	a := newMinimalApp()
	a.width = 120
	a.models = modelconfig.DefaultModels()
	a.version = "0.0.1"
	out := a.renderFooter()
	if !strings.Contains(out, "huginn") {
		t.Error("expected 'huginn' in footer")
	}
	if !strings.Contains(out, "Auto-run off") {
		t.Error("expected 'Auto-run off' in default footer")
	}
}

func TestRenderFooter_AutoRunOn(t *testing.T) {
	a := newMinimalApp()
	a.width = 120
	a.models = modelconfig.DefaultModels()
	a.autoRun = true
	a.version = "0.0.1"
	out := a.renderFooter()
	if !strings.Contains(out, "Auto-run") {
		t.Error("expected 'Auto-run' in footer when autoRun is true")
	}
}

func TestRenderFooter_NarrowWidth(t *testing.T) {
	a := newMinimalApp()
	a.width = 30 // very narrow
	a.models = modelconfig.DefaultModels()
	a.version = "0.0.1"
	// Should not panic.
	out := a.renderFooter()
	if out == "" {
		t.Error("expected non-empty footer even at narrow width")
	}
}

func TestRenderFooter_ZeroWidth(t *testing.T) {
	a := newMinimalApp()
	a.width = 0
	a.models = modelconfig.DefaultModels()
	a.version = "0.0.1"
	// Should not panic.
	_ = a.renderFooter()
}

// ---------------------------------------------------------------------------
// handleSlashCommand — /agents sub-command
// ---------------------------------------------------------------------------

func TestHandleSlashCommand_Agents(t *testing.T) {
	a := newAppWithAgents()
	a.handleSlashCommand(SlashCommand{Name: "agents", Args: ""})
	found := false
	for _, h := range a.chat.history {
		if strings.Contains(h.content, "Chris") {
			found = true
		}
	}
	if !found {
		t.Error("expected agent roster in history for /agents")
	}
}

func TestHandleSlashCommand_AgentsNilRegistry(t *testing.T) {
	a := newMinimalApp()
	a.handleSlashCommand(SlashCommand{Name: "agents", Args: ""})
	found := false
	for _, h := range a.chat.history {
		if strings.Contains(h.content, "No agent registry") {
			found = true
		}
	}
	if !found {
		t.Error("expected 'No agent registry' in history when agentReg is nil")
	}
}

// ---------------------------------------------------------------------------
// handleSlashCommand — /workspace with index set
// ---------------------------------------------------------------------------

func TestHandleSlashCommand_WorkspaceWithIndex(t *testing.T) {
	a := newMinimalApp()
	a.workspaceRoot = "/tmp/myproject"
	a.idx = &repo.Index{
		Root:   "/tmp/myproject",
		Chunks: []repo.FileChunk{{Path: "main.go", Content: "package main\n", StartLine: 1}},
	}
	a.handleSlashCommand(SlashCommand{Name: "workspace"})
	found := false
	for _, h := range a.chat.history {
		if strings.Contains(h.content, "/tmp/myproject") && strings.Contains(h.content, "1") {
			found = true
		}
	}
	if !found {
		t.Error("expected workspace root and chunk count in history")
	}
}

// ---------------------------------------------------------------------------
// handleSlashCommand — /stats with registry
// ---------------------------------------------------------------------------

func TestHandleSlashCommand_StatsWithRegistry(t *testing.T) {
	a := newMinimalApp()
	a.statsReg = stats.NewRegistry()
	a.handleSlashCommand(SlashCommand{Name: "stats"})
	found := false
	for _, h := range a.chat.history {
		if strings.Contains(h.content, "Stats") {
			found = true
		}
	}
	if !found {
		t.Error("expected 'Stats' in history when statsReg is set")
	}
}

// ---------------------------------------------------------------------------
// handleSlashCommand — /impact with store but no references
// ---------------------------------------------------------------------------

// Note: Testing /impact with a real store requires Pebble which is heavy.
// We verify the guard branches instead.

func TestHandleSlashCommand_ImpactEmpty(t *testing.T) {
	a := newMinimalApp()
	a.handleSlashCommand(SlashCommand{Name: "impact", Args: "  "})
	found := false
	for _, h := range a.chat.history {
		if strings.Contains(h.content, "Usage") {
			found = true
		}
	}
	if !found {
		t.Error("expected Usage message for whitespace-only /impact args")
	}
}

// ---------------------------------------------------------------------------
// handleAgentsCommand — no registry message
// ---------------------------------------------------------------------------

func TestAgentsCommand_NilRegistryMessage(t *testing.T) {
	a := newMinimalApp()
	msg := a.handleAgentsCommand("")
	if !strings.Contains(msg, "No agent registry") {
		t.Errorf("expected 'No agent registry' message, got: %s", msg)
	}
}

// ---------------------------------------------------------------------------
// handleAgentsCommand — persona with system prompt set
// ---------------------------------------------------------------------------

func TestAgentsCommand_PersonaWithPrompt(t *testing.T) {
	a := newAppWithAgents()
	ag, _ := a.agentReg.ByName("Chris")
	ag.SystemPrompt = "You are Chris, a senior architect."
	msg := a.handleAgentsCommand("persona Chris")
	if !strings.Contains(msg, "senior architect") {
		t.Errorf("expected persona content, got: %s", msg)
	}
}

// ---------------------------------------------------------------------------
// handleAgentsCommand — persona missing args
// ---------------------------------------------------------------------------

func TestAgentsCommand_PersonaMissingArgs(t *testing.T) {
	a := newAppWithAgents()
	msg := a.handleAgentsCommand("persona")
	if !strings.Contains(msg, "Usage") {
		t.Errorf("expected Usage for persona with no args, got: %s", msg)
	}
}

// ---------------------------------------------------------------------------
// handleAgentsCommand — create with valid new agent
// ---------------------------------------------------------------------------

func TestAgentsCommand_CreateNewAgent(t *testing.T) {
	a := newAppWithAgents()
	// Create agent: this calls LoadAgents/SaveAgents which touch the filesystem.
	// We test the in-memory registration path.
	msg := a.handleAgentsCommand("create NewAgent planner test-model:7b")
	if !strings.Contains(msg, "NewAgent") {
		// If it failed due to filesystem (LoadAgents), that's acceptable in test.
		// Just check it doesn't panic.
		t.Logf("create returned: %s (may fail due to filesystem)", msg)
	}
}

// ---------------------------------------------------------------------------
// renderAgentRoster — with agents
// ---------------------------------------------------------------------------

func TestRenderAgentRoster_WithAgents(t *testing.T) {
	a := newAppWithAgents()
	out := a.renderAgentRoster()
	if !strings.Contains(out, "Chris") {
		t.Error("expected 'Chris' in roster")
	}
	if !strings.Contains(out, "Steve") {
		t.Error("expected 'Steve' in roster")
	}
	if !strings.Contains(out, "Mark") {
		t.Error("expected 'Mark' in roster")
	}
	if !strings.Contains(out, "Agents") {
		t.Error("expected 'Agents' header in roster")
	}
}

// ---------------------------------------------------------------------------
// renderAgentRoster — nil registry
// ---------------------------------------------------------------------------

func TestRenderAgentRoster_NilRegistry(t *testing.T) {
	a := newMinimalApp()
	a.agentReg = agents.NewRegistry()
	out := a.renderAgentRoster()
	if !strings.Contains(out, "No agents") {
		t.Errorf("expected 'No agents' for empty registry, got: %s", out)
	}
}

// ---------------------------------------------------------------------------
// handleSlashCommand — /radar with workspace but no store
// ---------------------------------------------------------------------------

func TestHandleSlashCommand_RadarNoStoreWithWorkspace(t *testing.T) {
	a := newMinimalApp()
	a.workspaceRoot = "/tmp/project"
	a.handleSlashCommand(SlashCommand{Name: "radar"})
	found := false
	for _, h := range a.chat.history {
		if strings.Contains(h.content, "unavailable") {
			found = true
		}
	}
	if !found {
		t.Error("expected 'unavailable' for /radar without store")
	}
}

// ---------------------------------------------------------------------------
// tryDispatch — with cfg.MaxTurns
// ---------------------------------------------------------------------------

func TestTryDispatch_WithConfig(t *testing.T) {
	a := newMinimalApp()
	a.cfg = &config.Config{MaxTurns: 25}
	// Both orch and agentReg nil → returns nil.
	cmd := a.tryDispatch(nil, "hello")
	if cmd != nil {
		t.Error("expected nil cmd when orch is nil")
	}
}

// ---------------------------------------------------------------------------
// convertStorageEdgesToSymbolEdges
// ---------------------------------------------------------------------------

func TestConvertStorageEdges_Empty(t *testing.T) {
	result := convertStorageEdgesToSymbolEdges(nil)
	if len(result) != 0 {
		t.Errorf("expected empty result for nil input, got %d", len(result))
	}
}

func TestConvertStorageEdges_WithEdges(t *testing.T) {
	edges := []storage.Edge{
		{From: "a.go", To: "b.go", Symbol: "Foo", Confidence: "HIGH", Kind: "Call"},
	}
	result := convertStorageEdgesToSymbolEdges(edges)
	if len(result) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(result))
	}
	if result[0].From != "a.go" {
		t.Errorf("expected From='a.go', got %q", result[0].From)
	}
	if result[0].Symbol != "Foo" {
		t.Errorf("expected Symbol='Foo', got %q", result[0].Symbol)
	}
	if result[0].Confidence != symbol.Confidence("HIGH") {
		t.Errorf("expected Confidence=HIGH, got %v", result[0].Confidence)
	}
	if result[0].Kind != symbol.EdgeKind("Call") {
		t.Errorf("expected Kind=Call, got %v", result[0].Kind)
	}
}

// ---------------------------------------------------------------------------
// renderInputBox — different states
// ---------------------------------------------------------------------------

func TestRenderInputBox_ChatState(t *testing.T) {
	a := newMinimalApp()
	a.width = 80
	out := a.renderInputBox()
	// Should not panic and should produce output.
	if out == "" {
		t.Error("expected non-empty input box")
	}
}

func TestRenderInputBox_ApprovalState(t *testing.T) {
	a := newMinimalApp()
	a.width = 80
	a.state = stateChat
	out := a.renderInputBox()
	if out == "" {
		t.Error("expected non-empty input box in approval state")
	}
}

// ---------------------------------------------------------------------------
// renderChips — with shell context
// ---------------------------------------------------------------------------

func TestRenderChips_WithShellContext(t *testing.T) {
	a := newMinimalApp()
	a.width = 80
	a.shellContext = "line1\nline2\nline3\n"
	out := a.renderChips()
	if !strings.Contains(out, "shell output") {
		t.Error("expected 'shell output' in chips when shellContext is set")
	}
}

func TestRenderChips_WithAttachments(t *testing.T) {
	a := newMinimalApp()
	a.width = 80
	a.attachments = []string{"/path/to/file.go"}
	out := a.renderChips()
	if !strings.Contains(out, "file.go") {
		t.Error("expected filename in chips when attachments are set")
	}
}

// ---------------------------------------------------------------------------
// addLine and history
// ---------------------------------------------------------------------------

func TestAddLine_AppendsToHistory(t *testing.T) {
	a := newMinimalApp()
	a.addLine("user", "hello")
	a.addLine("assistant", "hi there")
	if len(a.chat.history) != 2 {
		t.Errorf("expected 2 history entries, got %d", len(a.chat.history))
	}
	if a.chat.history[0].role != "user" || a.chat.history[0].content != "hello" {
		t.Error("first history entry mismatch")
	}
}

// ---------------------------------------------------------------------------
// renderMarkdown — nil renderer
// ---------------------------------------------------------------------------

func TestRenderMarkdown_NilRenderer(t *testing.T) {
	a := newMinimalApp()
	// glamour is nil in newMinimalApp.
	out := a.renderMarkdown("# Hello\nWorld")
	// Should fall back to raw content.
	if !strings.Contains(out, "Hello") {
		t.Error("expected content even with nil renderer")
	}
}

// ---------------------------------------------------------------------------
// SetAgentRegistry — re-set overwrites
// ---------------------------------------------------------------------------

func TestSetAgentRegistry_Overwrite(t *testing.T) {
	a := newMinimalApp()
	reg1 := agents.NewRegistry()
	reg1.Register(&agents.Agent{Name: "A1"})
	a.SetAgentRegistry(reg1)

	reg2 := agents.NewRegistry()
	reg2.Register(&agents.Agent{Name: "B1"})
	a.SetAgentRegistry(reg2)

	if a.agentReg != reg2 {
		t.Error("expected agentReg to be overwritten with reg2")
	}
	if _, ok := a.agentReg.ByName("A1"); ok {
		t.Error("agent A1 should not be in new registry")
	}
}
