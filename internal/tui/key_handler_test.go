package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// newKeyHandlerTestApp returns a minimal App for testing key handling.
func newKeyHandlerTestApp() *App {
	input := textinput.New()
	input.Placeholder = "Test..."
	return &App{
		state:        stateChat,
		input:        input,
		chat:         ChatModel{history: []chatLine{}},
		ctrlCPending: false,
		attachments:  []string{},
		chipFocused:  false,
		chipCursor:   0,
		queuedMsg:    "",
	}
}

// TestKeyHandler_CtrlC_CancelsInput verifies that Ctrl+C clears the input when in chat mode.
func TestKeyHandler_CtrlC_CancelsInput(t *testing.T) {
	app := newKeyHandlerTestApp()
	app.input.SetValue("hello world")

	msg := tea.KeyMsg{Type: tea.KeyCtrlC}
	result, _ := app.handleKeyMsg(msg, []tea.Cmd{})
	resultApp := result.(*App)

	if resultApp.input.Value() != "" {
		t.Errorf("Ctrl+C should clear input, got: %q", resultApp.input.Value())
	}
	if resultApp.ctrlCPending {
		t.Errorf("ctrlCPending should be false after clearing input, got: %v", resultApp.ctrlCPending)
	}
}

// TestKeyHandler_CtrlC_ExitPrompt verifies that Ctrl+C without input shows exit prompt on first press.
func TestKeyHandler_CtrlC_ExitPrompt(t *testing.T) {
	app := newKeyHandlerTestApp()
	app.input.SetValue("")

	msg := tea.KeyMsg{Type: tea.KeyCtrlC}
	result, _ := app.handleKeyMsg(msg, []tea.Cmd{})
	resultApp := result.(*App)

	// First Ctrl+C without input sets ctrlCPending
	if !resultApp.ctrlCPending {
		t.Errorf("ctrlCPending should be true on first Ctrl+C, got: %v", resultApp.ctrlCPending)
	}
	// Check that system message was added
	if len(resultApp.chat.history) == 0 {
		t.Errorf("expected system message for exit prompt, got empty history")
	}
	if !strings.Contains(resultApp.chat.history[0].content, "ctrl+c") {
		t.Errorf("expected exit prompt message, got: %s", resultApp.chat.history[0].content)
	}
}

// TestKeyHandler_Enter_ReturnsModel verifies that Enter key returns a valid model without panic.
// (Full submit requires session/backend state; this verifies the key handler doesn't crash.)
func TestKeyHandler_Enter_ReturnsModel(t *testing.T) {
	app := newKeyHandlerTestApp()
	// Empty input — Enter with no text is a no-op.
	app.input.SetValue("")

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	result, _ := app.handleKeyMsg(msg, []tea.Cmd{})
	if result == nil {
		t.Error("handleKeyMsg should return non-nil model")
	}
	if _, ok := result.(*App); !ok {
		t.Error("handleKeyMsg should return *App")
	}
}

// TestKeyHandler_Enter_IgnoresEmptyInput verifies that Enter with empty input does nothing.
func TestKeyHandler_Enter_IgnoresEmptyInput(t *testing.T) {
	app := newKeyHandlerTestApp()
	app.input.SetValue("")

	msg := tea.KeyMsg{Type: tea.KeyEnter}
	result, _ := app.handleKeyMsg(msg, []tea.Cmd{})
	resultApp := result.(*App)

	// Input should still be empty
	if resultApp.input.Value() != "" {
		t.Errorf("Enter with empty input should not submit, got: %q", resultApp.input.Value())
	}
}

// TestKeyHandler_CtrlP_CyclesAgent verifies Ctrl+P cycles through agents.
func TestKeyHandler_CtrlP_CyclesAgent(t *testing.T) {
	app := newKeyHandlerTestApp()
	app.primaryAgent = "agent1"

	msg := tea.KeyMsg{Type: tea.KeyCtrlP}
	// Should not crash even without agentReg set (agentReg is nil)
	result, _ := app.handleKeyMsg(msg, []tea.Cmd{})
	resultApp := result.(*App)

	// Agent should be unchanged if registry is nil
	if resultApp.primaryAgent != "agent1" {
		t.Errorf("Ctrl+P with nil registry should not change agent, got: %s", resultApp.primaryAgent)
	}
}

// TestKeyHandler_Escape_ClearsQueuedMsg verifies Escape clears queued message.
func TestKeyHandler_Escape_ClearsQueuedMsg(t *testing.T) {
	app := newKeyHandlerTestApp()
	app.queuedMsg = "queued message"

	msg := tea.KeyMsg{Type: tea.KeyEsc}
	result, _ := app.handleKeyMsg(msg, []tea.Cmd{})
	resultApp := result.(*App)

	if resultApp.queuedMsg != "" {
		t.Errorf("Escape should clear queuedMsg, got: %q", resultApp.queuedMsg)
	}
}

// TestKeyHandler_Escape_UnfocusesChips verifies Escape unfocuses attachment chips.
func TestKeyHandler_Escape_UnfocusesChips(t *testing.T) {
	app := newKeyHandlerTestApp()
	app.chipFocused = true
	app.attachments = []string{"file.txt"}

	msg := tea.KeyMsg{Type: tea.KeyEsc}
	result, _ := app.handleKeyMsg(msg, []tea.Cmd{})
	resultApp := result.(*App)

	if resultApp.chipFocused {
		t.Errorf("Escape should unfocus chips, got: %v", resultApp.chipFocused)
	}
}

// TestKeyHandler_Up_RestoresQueuedMsg verifies Up arrow restores queued message to input.
func TestKeyHandler_Up_RestoresQueuedMsg(t *testing.T) {
	app := newKeyHandlerTestApp()
	app.queuedMsg = "previous message"

	msg := tea.KeyMsg{Type: tea.KeyUp}
	result, _ := app.handleKeyMsg(msg, []tea.Cmd{})
	resultApp := result.(*App)

	if resultApp.input.Value() != "previous message" {
		t.Errorf("Up should restore queuedMsg to input, got: %q", resultApp.input.Value())
	}
	if resultApp.queuedMsg != "" {
		t.Errorf("Up should clear queuedMsg, got: %q", resultApp.queuedMsg)
	}
}

// TestKeyHandler_Backspace_RemovesAttachment verifies Backspace removes focused attachment.
func TestKeyHandler_Backspace_RemovesAttachment(t *testing.T) {
	app := newKeyHandlerTestApp()
	app.attachments = []string{"file1.txt", "file2.txt"}
	app.chipFocused = true
	app.chipCursor = 0

	msg := tea.KeyMsg{Type: tea.KeyBackspace}
	result, _ := app.handleKeyMsg(msg, []tea.Cmd{})
	resultApp := result.(*App)

	if len(resultApp.attachments) != 1 {
		t.Errorf("Backspace should remove attachment, got %d attachments", len(resultApp.attachments))
	}
	if resultApp.attachments[0] != "file2.txt" {
		t.Errorf("expected remaining file to be file2.txt, got: %s", resultApp.attachments[0])
	}
}

// TestKeyHandler_LeftArrow_MovesChipCursor verifies Left arrow moves chip cursor left.
func TestKeyHandler_LeftArrow_MovesChipCursor(t *testing.T) {
	app := newKeyHandlerTestApp()
	app.attachments = []string{"file1.txt", "file2.txt", "file3.txt"}
	app.chipFocused = true
	app.chipCursor = 2

	msg := tea.KeyMsg{Type: tea.KeyLeft}
	result, _ := app.handleKeyMsg(msg, []tea.Cmd{})
	resultApp := result.(*App)

	if resultApp.chipCursor != 1 {
		t.Errorf("Left arrow should move cursor left, expected 1, got: %d", resultApp.chipCursor)
	}
}

// TestKeyHandler_RightArrow_MovesChipCursor verifies Right arrow moves chip cursor right.
func TestKeyHandler_RightArrow_MovesChipCursor(t *testing.T) {
	app := newKeyHandlerTestApp()
	app.attachments = []string{"file1.txt", "file2.txt", "file3.txt"}
	app.chipFocused = true
	app.chipCursor = 0

	msg := tea.KeyMsg{Type: tea.KeyRight}
	result, _ := app.handleKeyMsg(msg, []tea.Cmd{})
	resultApp := result.(*App)

	if resultApp.chipCursor != 1 {
		t.Errorf("Right arrow should move cursor right, expected 1, got: %d", resultApp.chipCursor)
	}
}

// TestKeyHandler_Question_ShowsHelp verifies ? key shows help text.
func TestKeyHandler_Question_ShowsHelp(t *testing.T) {
	app := newKeyHandlerTestApp()

	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}}
	result, _ := app.handleKeyMsg(msg, []tea.Cmd{})
	resultApp := result.(*App)

	if len(resultApp.chat.history) == 0 {
		t.Errorf("? should add help text to history")
	}
	if !strings.Contains(resultApp.chat.history[0].content, "Keyboard") {
		t.Errorf("expected help text containing 'Keyboard', got: %s", resultApp.chat.history[0].content)
	}
}

// TestKeyHandler_ShiftTab_TogglesAutoRun verifies Shift+Tab toggles auto-run.
func TestKeyHandler_ShiftTab_TogglesAutoRun(t *testing.T) {
	app := newKeyHandlerTestApp()
	initialAutoRun := app.autoRun

	msg := tea.KeyMsg{Type: tea.KeyShiftTab}
	result, _ := app.handleKeyMsg(msg, []tea.Cmd{})
	resultApp := result.(*App)

	if resultApp.autoRun == initialAutoRun {
		t.Errorf("Shift+Tab should toggle autoRun, got same value: %v", resultApp.autoRun)
	}
	// Check for system message
	if len(resultApp.chat.history) == 0 {
		t.Errorf("Shift+Tab should add status message to history")
	}
}
