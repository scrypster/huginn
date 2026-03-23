package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DMSwitchMsg is sent when the user selects an agent from the /dm picker.
type DMSwitchMsg struct{ Agent string }

// ChannelSwitchMsg is sent when the user selects a channel from the /channel picker.
type ChannelSwitchMsg struct{ Name string }

// pickerDismissMsg is sent when any picker overlay is dismissed without selection.
type pickerDismissMsg struct{}

// ── DM picker ────────────────────────────────────────────────────────────────

// dmPickerModel is the /dm slash-command overlay.
// It shows all registered agents with up/down navigation and Enter to switch.
type dmPickerModel struct {
	visible  bool
	filter   string
	allNames []string // full agent roster
	filtered []string // filtered list
	cursor   int
}

// dmPickerSelectMsg is the internal message sent when a DM agent is chosen.
type dmPickerSelectMsg struct{ name string }

func newDMPicker() dmPickerModel { return dmPickerModel{} }

func (m *dmPickerModel) SetNames(names []string) {
	m.allNames = names
}

func (m *dmPickerModel) Show(filter string) {
	m.filter = filter
	m.filtered = FilterAgentNames(m.allNames, filter)
	m.visible = len(m.filtered) > 0 || filter == ""
	if m.cursor >= len(m.filtered) {
		m.cursor = 0
	}
}

func (m *dmPickerModel) UpdateFilter(filter string) {
	m.filter = filter
	m.filtered = FilterAgentNames(m.allNames, filter)
	if m.cursor >= len(m.filtered) {
		m.cursor = 0
	}
}

func (m *dmPickerModel) Hide() {
	m.visible = false
	m.filter = ""
	m.filtered = nil
	m.cursor = 0
}

func (m dmPickerModel) Visible() bool { return m.visible }

func (m dmPickerModel) Update(msg tea.Msg) (dmPickerModel, tea.Cmd) {
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
		return m, func() tea.Msg { return pickerDismissMsg{} }
	case "enter", "tab":
		if len(m.filtered) > 0 {
			chosen := m.filtered[m.cursor]
			m.Hide()
			return m, func() tea.Msg { return DMSwitchMsg{Agent: chosen} }
		}
	case "up", "ctrl+p":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "ctrl+n":
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		}
	}
	return m, nil
}

func (m dmPickerModel) View(width int) string {
	if !m.visible {
		return ""
	}

	hint := StyleDim.Render("  Direct Messages · ↑↓ navigate · enter open · esc dismiss")

	if len(m.allNames) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left,
			hint,
			StyleDim.Render("  No agents configured — run /agents new to create one"),
		)
	}

	if len(m.filtered) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left,
			hint,
			StyleDim.Render("  No agents match"),
		)
	}

	const maxRows = 8
	nameCol := 26
	_ = width - nameCol - 4

	rows := []string{hint}
	for i, name := range m.filtered {
		if i >= maxRows {
			rows = append(rows, StyleDim.Render(fmt.Sprintf("  … %d more", len(m.filtered)-maxRows)))
			break
		}
		padded := "@ " + name
		for len(padded) < nameCol {
			padded += " "
		}
		line := "  " + padded
		if i == m.cursor {
			rows = append(rows, StyleWizardItemSelected.Render(line))
		} else {
			rows = append(rows, StyleWizardItem.Render(line))
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// ── Channel picker ────────────────────────────────────────────────────────────

// channelPickerModel is the /channel slash-command overlay.
// It lists known channel spaces with up/down navigation and Enter to switch.
type channelPickerModel struct {
	visible  bool
	filter   string
	allNames []string // full channel list
	filtered []string
	cursor   int
}

func newChannelPicker() channelPickerModel { return channelPickerModel{} }

func (m *channelPickerModel) SetChannels(names []string) {
	m.allNames = names
}

func (m *channelPickerModel) Show(filter string) {
	m.filter = filter
	m.filtered = FilterAgentNames(m.allNames, filter)
	m.visible = true
	if m.cursor >= len(m.filtered) {
		m.cursor = 0
	}
}

func (m *channelPickerModel) UpdateFilter(filter string) {
	m.filter = filter
	m.filtered = FilterAgentNames(m.allNames, filter)
	if m.cursor >= len(m.filtered) {
		m.cursor = 0
	}
}

func (m *channelPickerModel) Hide() {
	m.visible = false
	m.filter = ""
	m.filtered = nil
	m.cursor = 0
}

func (m channelPickerModel) Visible() bool { return m.visible }

func (m channelPickerModel) Update(msg tea.Msg) (channelPickerModel, tea.Cmd) {
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
		return m, func() tea.Msg { return pickerDismissMsg{} }
	case "enter", "tab":
		if len(m.filtered) > 0 {
			chosen := m.filtered[m.cursor]
			m.Hide()
			return m, func() tea.Msg { return ChannelSwitchMsg{Name: chosen} }
		}
	case "up", "ctrl+p":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "ctrl+n":
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		}
	}
	return m, nil
}

func (m channelPickerModel) View(width int) string {
	if !m.visible {
		return ""
	}

	hint := StyleDim.Render("  Channels · ↑↓ navigate · enter open · esc dismiss")

	if len(m.allNames) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left,
			hint,
			StyleDim.Render("  No channels yet — create one with: huginn serve → Channels"),
		)
	}

	if len(m.filtered) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left,
			hint,
			StyleDim.Render("  No channels match"),
		)
	}

	const maxRows = 8
	nameCol := 26

	rows := []string{hint}
	for i, name := range m.filtered {
		if i >= maxRows {
			rows = append(rows, StyleDim.Render(fmt.Sprintf("  … %d more", len(m.filtered)-maxRows)))
			break
		}
		padded := "# " + name
		for len(padded) < nameCol {
			padded += " "
		}
		line := "  " + padded
		if i == m.cursor {
			rows = append(rows, StyleWizardItemSelected.Render(line))
		} else {
			rows = append(rows, StyleWizardItem.Render(line))
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

