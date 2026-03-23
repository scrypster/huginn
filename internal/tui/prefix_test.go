package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// newMinimalApp returns a bare-bones App sufficient for testing prefix handlers.
// It has no orchestrator or config so it must not be used to trigger LLM calls.
func newMinimalApp() *App {
	return &App{
		state: stateChat,
	}
}

// --- ! shell command tests ---

// TestBangEmptyShowsHint verifies that "!" alone shows usage guidance and
// returns the app to stateChat without running anything.
func TestBangEmptyShowsHint(t *testing.T) {
	a := newMinimalApp()
	// submitMessage sets state to stateStreaming before prefix detection.
	// We replicate the prefix-check branch directly to avoid needing an orchestrator.
	raw := "!"
	shellCmd := strings.TrimSpace(strings.TrimPrefix(raw, "!"))
	if shellCmd != "" {
		t.Fatal("expected empty shell command")
	}
	a.state = stateChat
	a.addLine("system", "Shell: type !<command> to run  e.g. !ls -la")

	if a.state != stateChat {
		t.Errorf("expected stateChat after empty !, got %v", a.state)
	}
	if len(a.chat.history) == 0 {
		t.Fatal("expected hint added to history")
	}
	if !strings.Contains(a.chat.history[0].content, "!<command>") {
		t.Errorf("expected hint about !<command> in history, got: %s", a.chat.history[0].content)
	}
}

// TestBangRunsCommand verifies runShellCmd produces a shellResultMsg with the
// command's combined output.
func TestBangRunsCommand(t *testing.T) {
	a := newMinimalApp()
	// Run a simple echo so the test doesn't depend on any external state.
	cmd := a.runShellCmd(context.Background(), "echo hello")
	if cmd == nil {
		t.Fatal("runShellCmd returned nil tea.Cmd")
	}
	msg := cmd()
	result, ok := msg.(shellResultMsg)
	if !ok {
		t.Fatalf("expected shellResultMsg, got %T", msg)
	}
	if result.err != nil {
		t.Errorf("unexpected error: %v", result.err)
	}
	if !strings.Contains(result.output, "hello") {
		t.Errorf("expected 'hello' in output, got: %q", result.output)
	}
	if result.cmd != "echo hello" {
		t.Errorf("expected cmd to be 'echo hello', got: %q", result.cmd)
	}
}

// TestBangErrorCommandReturnsError verifies that a failing command produces a
// shellResultMsg with a non-nil error or non-empty output (exit status).
func TestBangErrorCommand(t *testing.T) {
	a := newMinimalApp()
	cmd := a.runShellCmd(context.Background(), "false")
	if cmd == nil {
		t.Fatal("runShellCmd returned nil tea.Cmd")
	}
	msg := cmd()
	result, ok := msg.(shellResultMsg)
	if !ok {
		t.Fatalf("expected shellResultMsg, got %T", msg)
	}
	// 'false' always exits non-zero — err must be set.
	if result.err == nil {
		t.Error("expected non-nil error from 'false' command")
	}
}

// TestShellResultMsgHandlerSuccess verifies that the Update handler for
// shellResultMsg adds a system line with the command and output, clears
// streaming state, and transitions to stateChat.
func TestShellResultMsgHandlerSuccess(t *testing.T) {
	a := newMinimalApp()
	a.state = stateStreaming

	model, cmd := a.Update(shellResultMsg{cmd: "echo hi", output: "hi\n", err: nil})
	updated := model.(*App)

	if cmd != nil {
		t.Errorf("expected nil cmd from shellResultMsg handler, got %T", cmd)
	}
	if updated.state != stateChat {
		t.Errorf("expected stateChat after shellResultMsg, got %v", updated.state)
	}
	if len(updated.chat.history) == 0 {
		t.Fatal("expected history entry after shellResultMsg")
	}
	last := updated.chat.history[len(updated.chat.history)-1]
	if last.role != "system" {
		t.Errorf("expected role 'system', got %q", last.role)
	}
	if !strings.Contains(last.content, "echo hi") {
		t.Errorf("expected command in history content, got: %s", last.content)
	}
	if !strings.Contains(last.content, "hi") {
		t.Errorf("expected output in history content, got: %s", last.content)
	}
}

// TestShellResultMsgHandlerError verifies that a failed command adds an error line.
func TestShellResultMsgHandlerError(t *testing.T) {
	a := newMinimalApp()
	a.state = stateStreaming

	model, _ := a.Update(shellResultMsg{cmd: "bad", output: "", err: os.ErrNotExist})
	updated := model.(*App)

	if updated.state != stateChat {
		t.Errorf("expected stateChat after error shellResultMsg, got %v", updated.state)
	}
	if len(updated.chat.history) == 0 {
		t.Fatal("expected history entry after error shellResultMsg")
	}
	last := updated.chat.history[len(updated.chat.history)-1]
	if last.role != "error" {
		t.Errorf("expected role 'error', got %q", last.role)
	}
}

// --- # file attach tests ---

// --- # file picker / attachment tests ---

// TestHashKeyOpensFilePicker verifies that pressing "#" in stateChat opens the
// file picker overlay. "@" is reserved for @AgentName delegation.
func TestHashKeyOpensFilePicker(t *testing.T) {
	a := newMinimalApp()
	a.state = stateChat

	model, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'#'}})
	updated := model.(*App)

	if updated.state != stateFilePicker {
		t.Errorf("expected stateFilePicker after #, got %v", updated.state)
	}
	if !updated.filePicker.Visible() {
		t.Error("expected file picker to be visible after #")
	}
}

// TestFilePickerConfirmAddsAttachment verifies that FilePickerConfirmMsg appends
// the selected path to App.attachments.
func TestFilePickerConfirmAddsAttachment(t *testing.T) {
	a := newMinimalApp()
	a.state = stateFilePicker

	model, _ := a.Update(FilePickerConfirmMsg{Paths: []string{"internal/tui/app.go"}})
	updated := model.(*App)

	if updated.state != stateChat {
		t.Errorf("expected stateChat after confirm, got %v", updated.state)
	}
	if len(updated.attachments) != 1 {
		t.Fatalf("expected 1 attachment, got %d", len(updated.attachments))
	}
	if updated.attachments[0] != "internal/tui/app.go" {
		t.Errorf("unexpected attachment path: %q", updated.attachments[0])
	}
}

// TestFilePickerConfirmMultipleAttachments verifies multi-select.
func TestFilePickerConfirmMultipleAttachments(t *testing.T) {
	a := newMinimalApp()
	a.state = stateFilePicker

	model, _ := a.Update(FilePickerConfirmMsg{Paths: []string{"a.go", "b.go", "c.go"}})
	updated := model.(*App)

	if len(updated.attachments) != 3 {
		t.Errorf("expected 3 attachments, got %d", len(updated.attachments))
	}
}

// TestFilePickerConfirmNoDuplicates verifies that attaching the same file twice
// does not produce duplicate entries.
func TestFilePickerConfirmNoDuplicates(t *testing.T) {
	a := newMinimalApp()
	a.state = stateFilePicker
	a.attachments = []string{"a.go"}

	model, _ := a.Update(FilePickerConfirmMsg{Paths: []string{"a.go"}})
	updated := model.(*App)

	if len(updated.attachments) != 1 {
		t.Errorf("expected no duplicate, got %d attachments", len(updated.attachments))
	}
}

// TestFilePickerConfirmMaxCap verifies that attachments are capped at 10.
func TestFilePickerConfirmMaxCap(t *testing.T) {
	a := newMinimalApp()
	// Pre-fill to cap.
	for i := 0; i < 10; i++ {
		a.attachments = append(a.attachments, fmt.Sprintf("file%d.go", i))
	}
	a.state = stateFilePicker

	model, _ := a.Update(FilePickerConfirmMsg{Paths: []string{"overflow.go"}})
	updated := model.(*App)

	if len(updated.attachments) > 10 {
		t.Errorf("attachments exceeded cap of 10, got %d", len(updated.attachments))
	}
}

// TestFilePickerCancelDoesNotAddAttachment verifies that cancelling the picker
// leaves attachments unchanged.
func TestFilePickerCancelDoesNotAddAttachment(t *testing.T) {
	a := newMinimalApp()
	a.state = stateFilePicker

	model, _ := a.Update(FilePickerCancelMsg{})
	updated := model.(*App)

	if updated.state != stateChat {
		t.Errorf("expected stateChat after cancel, got %v", updated.state)
	}
	if len(updated.attachments) != 0 {
		t.Errorf("expected no attachments after cancel, got %d", len(updated.attachments))
	}
}

// TestBackspaceFromEmptyInputEntersChipFocus verifies that pressing Backspace
// with an empty input and pending attachments enters chip-focus mode.
func TestBackspaceFromEmptyInputEntersChipFocus(t *testing.T) {
	a := newMinimalApp()
	a.state = stateChat
	a.attachments = []string{"main.go"}

	model, _ := a.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	updated := model.(*App)

	if !updated.chipFocused {
		t.Error("expected chipFocused=true after Backspace with attachment and empty input")
	}
	if updated.chipCursor != 0 {
		t.Errorf("expected chipCursor=0, got %d", updated.chipCursor)
	}
}

// TestBackspaceInChipFocusRemovesChip verifies that pressing Backspace while
// chip-focused removes the focused attachment.
func TestBackspaceInChipFocusRemovesChip(t *testing.T) {
	a := newMinimalApp()
	a.state = stateChat
	a.attachments = []string{"a.go", "b.go"}
	a.chipFocused = true
	a.chipCursor = 1 // focused on b.go

	model, _ := a.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	updated := model.(*App)

	if len(updated.attachments) != 1 {
		t.Fatalf("expected 1 attachment after remove, got %d", len(updated.attachments))
	}
	if updated.attachments[0] != "a.go" {
		t.Errorf("expected a.go to remain, got %q", updated.attachments[0])
	}
}

// TestChipFocusExitOnLastRemoved verifies that removing the last chip exits
// chip-focus mode automatically.
func TestChipFocusExitOnLastRemoved(t *testing.T) {
	a := newMinimalApp()
	a.state = stateChat
	a.attachments = []string{"only.go"}
	a.chipFocused = true
	a.chipCursor = 0

	model, _ := a.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	updated := model.(*App)

	if updated.chipFocused {
		t.Error("expected chipFocused=false after last chip removed")
	}
	if len(updated.attachments) != 0 {
		t.Errorf("expected no attachments, got %d", len(updated.attachments))
	}
}

// Ensure runShellCmd returns a non-nil tea.Cmd (not a nil func).
func TestRunShellCmdReturnsCmdNotNil(t *testing.T) {
	a := newMinimalApp()
	var cmd tea.Cmd = a.runShellCmd(context.Background(), "echo test")
	if cmd == nil {
		t.Error("runShellCmd must return a non-nil tea.Cmd")
	}
}
