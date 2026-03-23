package tui

// Regression tests for the "?" key behaviour.
//
// Bug: pressing "?" while the text input was focused triggered the keyboard-
// shortcuts help overlay instead of inserting a literal "?" character.
// Root cause: update_keys.go intercepted "?" without checking a.input.Focused().
// Fix: guard the help branch with !a.input.Focused().

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textinput"
)

// TestQuestionMarkKey_InputUnfocused_ShowsHelp verifies that pressing "?"
// when the input is NOT focused displays the keyboard-shortcut help text.
func TestQuestionMarkKey_InputUnfocused_ShowsHelp(t *testing.T) {
	a := newMinimalApp()
	a.state = stateChat
	// input is unfocused by default (zero value of textinput.Model)
	if a.input.Focused() {
		t.Skip("input unexpectedly focused at start")
	}

	prevLen := len(a.chat.history)
	a.handleKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}}, nil)

	if len(a.chat.history) == prevLen {
		t.Error("expected help text to be added to history when input is unfocused")
	}
	found := false
	for _, h := range a.chat.history {
		if strings.Contains(h.content, "Keyboard shortcuts") || strings.Contains(h.content, "ctrl+c") {
			found = true
		}
	}
	if !found {
		t.Error("expected keyboard shortcuts content in history entry")
	}
}

// TestQuestionMarkKey_InputFocused_DoesNotShowHelp verifies that pressing "?"
// when the text input IS focused does NOT trigger the help overlay.
// The "?" should be passed through to the input as a typed character.
func TestQuestionMarkKey_InputFocused_DoesNotShowHelp(t *testing.T) {
	a := newMinimalApp()
	a.state = stateChat
	ti := textinput.New()
	ti.Focus()
	a.input = ti

	if !a.input.Focused() {
		t.Fatal("input must be focused for this test")
	}

	prevLen := len(a.chat.history)
	a.handleKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}}, nil)

	// No new history entry should have been added for the help overlay.
	for i := prevLen; i < len(a.chat.history); i++ {
		if strings.Contains(a.chat.history[i].content, "Keyboard shortcuts") {
			t.Error("help overlay was shown even though input was focused — users cannot type '?' in messages")
		}
	}
}
