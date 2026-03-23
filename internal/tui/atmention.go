package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// AtMentionSelectMsg is sent when the user picks an agent from the @-mention dropdown.
type AtMentionSelectMsg struct {
	// Name is the chosen agent name (without the leading @).
	Name string
}

// AtMentionDismissMsg is sent when the dropdown is dismissed without a selection.
type AtMentionDismissMsg struct{}

// atMentionModel is the @AgentName autocomplete dropdown.
// It appears above the input box whenever the user types "@" followed by
// at least 1 character that matches a known agent name.
type atMentionModel struct {
	visible  bool
	prefix   string   // text typed after @
	names    []string // filtered agent names to display
	allNames []string // full agent roster (set once via SetNames)
	cursor   int
}

// SetNames replaces the full agent roster used for filtering.
func (m *atMentionModel) SetNames(names []string) {
	m.allNames = names
}

// Show filters the roster by prefix and makes the dropdown visible.
// Hides itself if there are no matches.
func (m *atMentionModel) Show(prefix string) {
	m.prefix = prefix
	m.names = FilterAgentNames(m.allNames, prefix)
	if len(m.names) == 0 {
		m.visible = false
		return
	}
	m.visible = true
	if m.cursor >= len(m.names) {
		m.cursor = 0
	}
}

// Hide collapses the dropdown and resets state.
func (m *atMentionModel) Hide() {
	m.visible = false
	m.prefix = ""
	m.names = nil
	m.cursor = 0
}

// Visible reports whether the dropdown is currently open.
func (m atMentionModel) Visible() bool { return m.visible }

// Selected returns the currently highlighted agent name.
func (m atMentionModel) Selected() string {
	if len(m.names) == 0 {
		return ""
	}
	return m.names[m.cursor]
}

// Update handles keyboard navigation while the dropdown is visible.
// Returns (model, cmd) where cmd is non-nil only on selection or dismiss.
func (m atMentionModel) Update(msg tea.Msg) (atMentionModel, tea.Cmd) {
	if !m.visible {
		return m, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch km.String() {
	case "esc":
		m.Hide()
		return m, func() tea.Msg { return AtMentionDismissMsg{} }
	case "enter", "tab":
		if len(m.names) > 0 {
			chosen := m.names[m.cursor]
			m.Hide()
			return m, func() tea.Msg { return AtMentionSelectMsg{Name: chosen} }
		}
	case "up", "ctrl+p":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "ctrl+n":
		if m.cursor < len(m.names)-1 {
			m.cursor++
		}
	}
	return m, nil
}

// View renders the dropdown as a compact list positioned above the input.
// Returns an empty string when not visible.
func (m atMentionModel) View(width int) string {
	if !m.visible || len(m.names) == 0 {
		return ""
	}

	hint := StyleDim.Render("  @mention · ↑↓ navigate · enter select · esc dismiss")

	nameCol := 24
	descWidth := width - nameCol - 4
	if descWidth < 10 {
		descWidth = 10
	}

	// Limit visible rows to avoid swamping the viewport.
	const maxRows = 6
	rows := []string{hint}

	for i, name := range m.names {
		if i >= maxRows {
			more := StyleDim.Render(fmt.Sprintf("  … %d more", len(m.names)-maxRows))
			rows = append(rows, more)
			break
		}
		padded := "@" + name
		for len(padded) < nameCol {
			padded += " "
		}
		// Highlight the matched prefix in the name.
		line := "  " + padded
		if i == m.cursor {
			rows = append(rows, StyleWizardItemSelected.Render(line))
		} else {
			rows = append(rows, StyleWizardItem.Render(line))
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// replaceAtMention replaces the "@prefix" at the end of input with "@name ".
// e.g. "ask @st" + "steve" → "ask @steve "
func replaceAtMention(input, name string) string {
	idx := strings.LastIndex(input, "@")
	if idx < 0 {
		return input + "@" + name + " "
	}
	return input[:idx] + "@" + name + " "
}
