package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/notepad"
	"github.com/scrypster/huginn/internal/session"
)

// ============================================================
// handleNotepadCmd — success paths (show, delete with existing notepads)
// ============================================================

func TestHandleNotepadCmd_Show_ExistingNotepad(t *testing.T) {
	a := newTestApp()
	dir := t.TempDir()
	mgr := notepad.NewManager(dir, dir)
	if err := mgr.Create("testpad", "Test content here"); err != nil {
		t.Fatalf("failed to create notepad: %v", err)
	}
	a.notepadMgr = mgr
	_ = a.handleNotepadCmd("/notepad show testpad")
	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry after /notepad show <existing>")
	}
	last := a.chat.history[len(a.chat.history)-1]
	if last.role != "system" {
		t.Errorf("expected system role for show success, got %q", last.role)
	}
}

func TestHandleNotepadCmd_Show_NotFound_ReturnsError(t *testing.T) {
	a := newTestApp()
	dir := t.TempDir()
	mgr := notepad.NewManager(dir, dir)
	a.notepadMgr = mgr
	_ = a.handleNotepadCmd("/notepad show nonexistent")
	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry after /notepad show nonexistent")
	}
	last := a.chat.history[len(a.chat.history)-1]
	if last.role != "error" {
		t.Errorf("expected error role for not-found notepad, got %q", last.role)
	}
}

func TestHandleNotepadCmd_Delete_ExistingNotepad(t *testing.T) {
	a := newTestApp()
	dir := t.TempDir()
	mgr := notepad.NewManager(dir, dir)
	if err := mgr.Create("delme", "to be deleted"); err != nil {
		t.Fatalf("failed to create notepad: %v", err)
	}
	a.notepadMgr = mgr
	_ = a.handleNotepadCmd("/notepad delete delme")
	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry after /notepad delete <existing>")
	}
	last := a.chat.history[len(a.chat.history)-1]
	if last.role != "system" {
		t.Errorf("expected system role for delete success, got %q", last.role)
	}
}

func TestHandleNotepadCmd_List_WithEntries(t *testing.T) {
	a := newTestApp()
	dir := t.TempDir()
	mgr := notepad.NewManager(dir, dir)
	if err := mgr.Create("note1", "content1"); err != nil {
		t.Fatalf("failed to create notepad: %v", err)
	}
	a.notepadMgr = mgr
	_ = a.handleNotepadCmd("/notepad list")
	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry after /notepad list with entries")
	}
}

// ============================================================
// handleSlashCommand — radar path (no store/workspace)
// ============================================================

func TestHandleSlashCommand_Radar_NilStoreAddsMessage(t *testing.T) {
	a := newTestApp()
	a.store = nil
	_ = a.handleSlashCommand(SlashCommand{Name: "radar"})
	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry for /radar without store")
	}
}

// ============================================================
// handleSlashCommand — stats with nil registry
// ============================================================

func TestHandleSlashCommand_Stats_NilReg(t *testing.T) {
	a := newTestApp()
	a.statsReg = nil
	_ = a.handleSlashCommand(SlashCommand{Name: "stats"})
	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry for /stats with nil registry")
	}
}

// ============================================================
// handleSlashCommand — resume path with nil store
// ============================================================

func TestHandleSlashCommand_Resume_NilStoreAddsMessage(t *testing.T) {
	a := newTestApp()
	a.sessionStore = nil
	_ = a.handleSlashCommand(SlashCommand{Name: "resume"})
	if len(a.chat.history) == 0 {
		t.Fatal("expected history for /resume with nil store")
	}
}

// ============================================================
// handleSlashCommand — agents with new arg
// ============================================================

func TestHandleSlashCommand_Agents_NewOpensWizard(t *testing.T) {
	a := newTestApp()
	_ = a.handleSlashCommand(SlashCommand{Name: "agents", Args: "new"})
	if a.state != stateAgentWizard {
		t.Errorf("expected stateAgentWizard after /agents new, got %v", a.state)
	}
}

// ============================================================
// handleAgentsCommand — edge cases
// ============================================================

// newTestAppWithRegistry creates an app with a minimal agent registry.
func newTestAppWithRegistry() *App {
	a := newTestApp()
	reg := agents.NewRegistry()
	def := agents.AgentDef{
		Name:  "testagent",
		Model: "gpt-4",
		Color: "#ff0000",
		Icon:  "T",
	}
	reg.Register(agents.FromDef(def))
	a.agentReg = reg
	return a
}

func TestHandleAgentsCommand_Swap_UnknownAgent(t *testing.T) {
	a := newTestAppWithRegistry()
	result := a.handleAgentsCommand("swap nonexistent gpt-4")
	if !strings.Contains(result, "Unknown agent") {
		t.Errorf("expected 'Unknown agent' error, got %q", result)
	}
}

func TestHandleAgentsCommand_Rename_UnknownAgent(t *testing.T) {
	a := newTestAppWithRegistry()
	result := a.handleAgentsCommand("rename nonexistent newname")
	if !strings.Contains(result, "Unknown agent") {
		t.Errorf("expected 'Unknown agent' error, got %q", result)
	}
}

func TestHandleAgentsCommand_Persona_NoCustomPrompt(t *testing.T) {
	a := newTestAppWithRegistry()
	result := a.handleAgentsCommand("persona testagent")
	if result == "" {
		t.Error("expected non-empty result from /agents persona")
	}
}

func TestHandleAgentsCommand_Delete_UnknownAgent(t *testing.T) {
	a := newTestAppWithRegistry()
	result := a.handleAgentsCommand("delete nonexistent")
	if !strings.Contains(result, "Unknown agent") {
		t.Errorf("expected 'Unknown agent' error, got %q", result)
	}
}

func TestHandleAgentsCommand_Create_AlreadyExists(t *testing.T) {
	a := newTestAppWithRegistry()
	result := a.handleAgentsCommand("create testagent planner claude-3")
	if !strings.Contains(result, "already exists") {
		t.Errorf("expected 'already exists' error, got %q", result)
	}
}

func TestHandleAgentsCommand_Create_MissingArgs(t *testing.T) {
	a := newTestAppWithRegistry()
	result := a.handleAgentsCommand("create")
	if !strings.Contains(result, "Usage") {
		t.Errorf("expected usage message, got %q", result)
	}
}

func TestHandleAgentsCommand_Swap_MissingArgs(t *testing.T) {
	a := newTestAppWithRegistry()
	result := a.handleAgentsCommand("swap")
	if !strings.Contains(result, "Usage") {
		t.Errorf("expected usage message, got %q", result)
	}
}

func TestHandleAgentsCommand_Rename_MissingArgs(t *testing.T) {
	a := newTestAppWithRegistry()
	result := a.handleAgentsCommand("rename")
	if !strings.Contains(result, "Usage") {
		t.Errorf("expected usage message, got %q", result)
	}
}

func TestHandleAgentsCommand_Persona_MissingArgs(t *testing.T) {
	a := newTestAppWithRegistry()
	result := a.handleAgentsCommand("persona")
	if !strings.Contains(result, "Usage") {
		t.Errorf("expected usage message, got %q", result)
	}
}

func TestHandleAgentsCommand_Delete_MissingArgs(t *testing.T) {
	a := newTestAppWithRegistry()
	result := a.handleAgentsCommand("delete")
	if !strings.Contains(result, "Usage") {
		t.Errorf("expected usage message, got %q", result)
	}
}

func TestHandleAgentsCommand_Unknown_SubcommandReturnsError(t *testing.T) {
	a := newTestAppWithRegistry()
	result := a.handleAgentsCommand("frobnicate")
	if !strings.Contains(result, "Unknown sub-command") {
		t.Errorf("expected 'Unknown sub-command' error, got %q", result)
	}
}

func TestHandleAgentsCommand_Persona_UnknownAgent(t *testing.T) {
	a := newTestAppWithRegistry()
	result := a.handleAgentsCommand("persona nobody")
	if !strings.Contains(result, "Unknown agent") {
		t.Errorf("expected 'Unknown agent' error, got %q", result)
	}
}

// ============================================================
// handleParallelCommand — with tasks but nil orch
// ============================================================

func TestHandleParallelCommand_WithTasks_NilOrchAddsMsg(t *testing.T) {
	a := newTestApp()
	a.orch = nil
	cmd := a.handleParallelCommand("task one | task two | task three")
	if cmd != nil {
		t.Error("expected nil cmd when orch is nil")
	}
}

// ============================================================
// saveSession — with valid store and session (fires goroutine)
// ============================================================

func TestSaveSession_ValidStoreAndSession_ReturnsNilCmd(t *testing.T) {
	a := newTestApp()
	// Use os.MkdirTemp without t.TempDir() cleanup to avoid goroutine holding dir open.
	dir, err := os.MkdirTemp("", "huginn-save-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	store := session.NewStore(dir)
	a.sessionStore = store
	a.activeSession = &session.Session{}
	cmd := a.saveSession()
	if cmd != nil {
		t.Error("expected nil cmd (goroutine fire-and-forget), got non-nil")
	}
}

// ============================================================
// resumeSession — with store but non-existent session
// ============================================================

func TestResumeSession_WithStore_NonExistentSession_NilMsg(t *testing.T) {
	a := newTestApp()
	store := session.NewStore(t.TempDir())
	a.sessionStore = store
	cmd := a.resumeSession("nonexistent-id")
	if cmd == nil {
		t.Fatal("expected non-nil cmd from resumeSession")
	}
	msg := cmd()
	if msg != nil {
		t.Errorf("expected nil msg for non-existent session, got %T", msg)
	}
}

// ============================================================
// Update — agentWizard key forwarding
// ============================================================

func TestApp_Update_AgentWizard_ForwardsKeys(t *testing.T) {
	a := newTestApp()
	a.state = stateAgentWizard
	a.agentWizard = newAgentWizardModel()

	model, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	updated := model.(*App)
	_ = updated
}

// ============================================================
// Update — sessionPicker key forwarding
// ============================================================

func TestApp_Update_SessionPicker_ForwardsKeys(t *testing.T) {
	a := newTestApp()
	a.state = stateSessionPicker
	a.sessionPicker = newSessionPickerModel(nil, "")
	a.sessionPicker.visible = true

	model, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	updated := model.(*App)
	_ = updated
}

// ============================================================
// Update — parallelDoneMsg
// ============================================================

func TestApp_Update_ParallelDoneMsg_TransitionsToChat(t *testing.T) {
	a := newTestApp()
	a.state = stateStreaming
	model, _ := a.Update(parallelDoneMsg{output: "parallel result"})
	updated := model.(*App)
	if updated.state != stateChat {
		t.Errorf("expected stateChat after parallelDoneMsg, got %v", updated.state)
	}
	if len(updated.chat.history) == 0 {
		t.Fatal("expected history entry after parallelDoneMsg")
	}
}

// ============================================================
// recalcViewportHeight — session picker path
// ============================================================

func TestApp_RecalcViewportHeight_SessionPicker(t *testing.T) {
	a := newTestApp()
	a.state = stateSessionPicker
	a.sessionPicker = newSessionPickerModel(nil, "")
	a.sessionPicker.visible = true
	a.recalcViewportHeight()
	if a.viewport.Height < 3 {
		t.Errorf("viewport height should be at least 3, got %d", a.viewport.Height)
	}
}

// ============================================================
// View — agentWizard overlay
// ============================================================

func TestApp_View_AgentWizard(t *testing.T) {
	a := newTestApp()
	a.state = stateAgentWizard
	a.agentWizard = newAgentWizardModel()
	result := a.View()
	if result == "" {
		t.Error("View() should be non-empty in stateAgentWizard")
	}
}

// ============================================================
// renderChips — with shellContext and chipFocused paths
// ============================================================

func TestRenderChips_WithShellContextAndAttachments(t *testing.T) {
	a := newTestApp()
	a.attachments = []string{"main.go"}
	a.shellContext = "shell output line1\nline2"
	result := a.renderChips()
	if result == "" {
		t.Error("expected non-empty renderChips with shellContext")
	}
}

func TestRenderChips_WithChipFocused(t *testing.T) {
	a := newTestApp()
	a.attachments = []string{"main.go", "app.go"}
	a.chipFocused = true
	a.chipCursor = 0
	result := a.renderChips()
	if result == "" {
		t.Error("expected non-empty renderChips when chipFocused")
	}
}

// ============================================================
// handleSlashCommand — parallel with nil orch
// ============================================================

func TestHandleSlashCommand_Parallel_NilOrch(t *testing.T) {
	a := newTestApp()
	a.orch = nil
	_ = a.handleSlashCommand(SlashCommand{Name: "parallel", Args: "task1 | task2"})
	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry for /parallel with nil orch")
	}
}
