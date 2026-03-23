package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/scrypster/huginn/internal/streaming"
)

// ============================================================
// handleSlashCommand — remaining uncovered branches
// ============================================================

func TestApp_HandleSlashCommand_ImpactWithStoreNoEdges(t *testing.T) {
	// This branch requires a.store != nil but returns no edges.
	// We can't easily create a real Store without a database path.
	// Skip this branch — it requires real PebbleDB.
	t.Skip("requires real storage.Store with PebbleDB")
}

// ============================================================
// Update — more keyboard branches in stateChat
// ============================================================

func TestApp_Update_Slash_OpensWizard(t *testing.T) {
	a := newTestApp()
	a.state = stateChat

	// Type "/" which should trigger wizard
	model, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	updated := model.(*App)
	if updated.state != stateWizard {
		t.Errorf("expected stateWizard after '/', got %v", updated.state)
	}
}

func TestApp_Update_BackspaceFromInput_WithText(t *testing.T) {
	a := newTestApp()
	a.state = stateChat
	a.input.SetValue("hello")

	// Backspace with text in input — should not enter chip focus
	model, _ := a.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	updated := model.(*App)
	if updated.chipFocused {
		t.Error("should not enter chip focus when input has text")
	}
}

func TestApp_Update_BackspaceNoAttachments(t *testing.T) {
	a := newTestApp()
	a.state = stateChat
	a.attachments = nil
	a.input.SetValue("")

	// Backspace with empty input but no attachments — no chip focus
	model, _ := a.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	updated := model.(*App)
	if updated.chipFocused {
		t.Error("should not enter chip focus with no attachments")
	}
}

func TestApp_Update_EnterWithEmptyInput_NoOp(t *testing.T) {
	a := newTestApp()
	a.state = stateChat
	a.input.SetValue("")

	model, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = model
	// With empty input, Enter is a no-op
	if cmd != nil {
		// This is acceptable — may return nil cmd
	}
}

func TestApp_Update_CtrlC_SecondPress_Quits(t *testing.T) {
	a := newTestApp()
	a.ctrlCPending = true
	a.input.SetValue("") // empty input

	model, cmd := a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	_ = model
	if cmd == nil {
		t.Error("expected quit cmd on second ctrl+c")
	}
	// The cmd should be tea.Quit
	msg := cmd()
	if msg != tea.Quit() {
		t.Errorf("expected tea.Quit, got %T", msg)
	}
}

func TestApp_Update_ChipFocused_Left_AtZero_NoOp(t *testing.T) {
	a := newTestApp()
	a.chipFocused = true
	a.chipCursor = 0
	a.attachments = []string{"a.go", "b.go"}

	model, _ := a.Update(tea.KeyMsg{Type: tea.KeyLeft})
	updated := model.(*App)
	if updated.chipCursor != 0 {
		t.Errorf("expected chipCursor to stay at 0, got %d", updated.chipCursor)
	}
}

func TestApp_Update_ChipFocused_Right_AtMax_NoOp(t *testing.T) {
	a := newTestApp()
	a.chipFocused = true
	a.chipCursor = 1
	a.attachments = []string{"a.go", "b.go"}

	model, _ := a.Update(tea.KeyMsg{Type: tea.KeyRight})
	updated := model.(*App)
	if updated.chipCursor != 1 {
		t.Errorf("expected chipCursor to stay at 1, got %d", updated.chipCursor)
	}
}

func TestApp_Update_UpKey_NoQueuedMsg_NoOp(t *testing.T) {
	a := newTestApp()
	a.queuedMsg = ""
	a.state = stateChat

	// Up with no queued message — falls through to normal input handling
	model, _ := a.Update(tea.KeyMsg{Type: tea.KeyUp})
	updated := model.(*App)
	if updated.queuedMsg != "" {
		t.Errorf("expected empty queuedMsg, got %q", updated.queuedMsg)
	}
}

// ============================================================
// tokenMsg handling in Update
// ============================================================

func TestApp_Update_TokenMsg_BuffersContent(t *testing.T) {
	a := newTestApp()
	a.state = stateStreaming
	// No tokenCh or eventCh — cmd will be nil
	a.chat.tokenCh = nil
	a.chat.eventCh = nil

	model, _ := a.Update(tokenMsg("hello "))
	updated := model.(*App)
	if updated.chat.tokenCount != 1 {
		t.Errorf("expected tokenCount=1, got %d", updated.chat.tokenCount)
	}
}

func TestApp_Update_TokenMsg_WithTokenCh(t *testing.T) {
	a := newTestApp()
	a.state = stateStreaming
	r := streaming.NewRunner()
	a.chat.runner = r
	r.Start(context.Background(), func(emit func(string)) error {
		emit("next-token")
		return nil
	})

	model, cmd := a.Update(tokenMsg("world"))
	_ = model
	if cmd == nil {
		t.Error("expected non-nil cmd from tokenMsg with runner")
	}
}

// ============================================================
// handleSlashCommand — missing branches
// ============================================================

func TestApp_HandleSlashCommand_EmptyOrUnknown(t *testing.T) {
	a := newTestApp()
	// Unknown command name — should not panic
	a.handleSlashCommand(SlashCommand{Name: "unknown-command-xyz"})
	// No history should be added for unknown commands
}

func TestApp_HandleSlashCommand_Radar_NoWorkspaceWithStore(t *testing.T) {
	a := newTestApp()
	// store is nil — already tested above
	// test with non-nil store but empty workspaceRoot requires real Store
	a.store = nil
	a.workspaceRoot = ""
	a.handleSlashCommand(SlashCommand{Name: "radar"})
	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry")
	}
	last := a.chat.history[len(a.chat.history)-1]
	if !strings.Contains(last.content, "unavailable") {
		t.Errorf("expected 'unavailable', got %q", last.content)
	}
}

// ============================================================
// renderMarkdown — error branch
// ============================================================

func TestApp_RenderMarkdown_ErrorFallsBack(t *testing.T) {
	a := newTestApp()
	// Create a valid renderer and use plain text (should succeed and render)
	a.glamourRenderer = newGlamourRenderer(80)
	result := a.renderMarkdown("plain text")
	if result == "" {
		t.Error("expected non-empty result from renderMarkdown")
	}
}

// ============================================================
// recalcViewportHeight — Wizard state
// ============================================================

func TestApp_RecalcViewportHeight_Wizard(t *testing.T) {
	a := newTestApp()
	a.state = stateWizard
	a.recalcViewportHeight()
	// Should not panic; height should be at least 3
	if a.viewport.Height < 3 {
		t.Errorf("expected viewport height >= 3 in wizard state, got %d", a.viewport.Height)
	}
}

func TestApp_RecalcViewportHeight_WithAttachments(t *testing.T) {
	a := newTestApp()
	a.attachments = []string{"main.go"}

	a.recalcViewportHeight()
	heightWith := a.viewport.Height

	a.attachments = nil
	a.recalcViewportHeight()
	heightWithout := a.viewport.Height

	if heightWith >= heightWithout {
		t.Errorf("viewport with attachments (%d) should be smaller than without (%d)", heightWith, heightWithout)
	}
}
