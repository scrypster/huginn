package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/scrypster/huginn/internal/session"
)

// SessionPickerMsg is sent when the user selects a session.
type SessionPickerMsg struct {
	ID string
}

// SessionPickerDismissMsg is sent when the user cancels the picker.
type SessionPickerDismissMsg struct{}

// sessionPickerModel is the TUI model for picking a past session to resume.
type sessionPickerModel struct {
	visible   bool
	filter    string
	cursor    int
	manifests []session.Manifest
	filtered  []session.Manifest
	workspace string
	allMode   bool
	width     int
	height    int
}

// newSessionPickerModel creates a new session picker pre-filtered to the given workspace.
func newSessionPickerModel(manifests []session.Manifest, workspace string) sessionPickerModel {
	m := sessionPickerModel{
		manifests: manifests,
		workspace: workspace,
	}
	m.applyFilter()
	return m
}

// applyFilter re-filters the manifest list based on allMode and filter string.
func (m *sessionPickerModel) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(m.filter))

	var result []session.Manifest
	for _, s := range m.manifests {
		// Workspace scope: when not in allMode, only show sessions from this workspace.
		// Sessions with no WorkspaceRoot set are always included (global sessions).
		if !m.allMode && s.WorkspaceRoot != "" && s.WorkspaceRoot != m.workspace {
			continue
		}

		// Text filter: match against title, workspace name, or model.
		if query != "" {
			titleMatch := strings.Contains(strings.ToLower(s.Title), query)
			wsMatch := strings.Contains(strings.ToLower(s.WorkspaceName), query)
			modelMatch := strings.Contains(strings.ToLower(s.Model), query)
			if !titleMatch && !wsMatch && !modelMatch {
				continue
			}
		}

		result = append(result, s)
	}

	m.filtered = result

	// Keep cursor in bounds.
	if m.cursor >= len(m.filtered) {
		m.cursor = 0
	}
}

// Update handles keypresses while the session picker is visible.
func (m sessionPickerModel) Update(msg tea.Msg) (sessionPickerModel, tea.Cmd) {
	if !m.visible {
		return m, nil
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch keyMsg.String() {
	case "esc":
		m.visible = false
		return m, func() tea.Msg { return SessionPickerDismissMsg{} }

	case "enter":
		if len(m.filtered) > 0 {
			id := m.filtered[m.cursor].SessionID
			m.visible = false
			return m, func() tea.Msg { return SessionPickerMsg{ID: id} }
		}

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}

	case "down", "j":
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		}

	case "tab":
		m.allMode = !m.allMode
		m.applyFilter()

	case "backspace":
		if len(m.filter) > 0 {
			// Remove last rune.
			runes := []rune(m.filter)
			m.filter = string(runes[:len(runes)-1])
			m.applyFilter()
		}

	default:
		// Append typed runes to filter.
		if len(keyMsg.Runes) > 0 {
			m.filter += string(keyMsg.Runes)
			m.applyFilter()
		}
	}

	return m, nil
}

// View renders the session picker overlay.
func (m sessionPickerModel) View() string {
	if !m.visible {
		return ""
	}

	var rows []string

	// Header.
	scope := "this workspace"
	if m.allMode {
		scope = "all workspaces"
	}
	header := StyleDim.Render(fmt.Sprintf("  Sessions (%s)  filter: %s", scope, m.filter))
	rows = append(rows, header)

	// Hint line.
	hint := StyleDim.Render("  ↑↓/jk navigate · tab toggle scope · enter resume · esc dismiss")
	rows = append(rows, hint)

	if len(m.filtered) == 0 {
		rows = append(rows, StyleDim.Render("  no sessions found"))
		return lipgloss.JoinVertical(lipgloss.Left, rows...)
	}

	// Determine column widths.
	width := m.width
	if width < 60 {
		width = 80
	}
	titleWidth := 36
	ageWidth := 10
	wsWidth := width - titleWidth - ageWidth - 8
	if wsWidth < 12 {
		wsWidth = 12
	}

	for i, s := range m.filtered {
		title := pickerTruncate(s.Title, titleWidth)
		if title == "" {
			title = pickerTruncate(s.SessionID, titleWidth)
		}
		age := pickerFormatAge(s.UpdatedAt)
		ws := pickerTruncate(s.WorkspaceName, wsWidth)

		// Pad title.
		for len([]rune(title)) < titleWidth {
			title += " "
		}
		// Pad workspace.
		for len([]rune(ws)) < wsWidth {
			ws += " "
		}
		// Pad age.
		for len(age) < ageWidth {
			age = " " + age
		}

		line := fmt.Sprintf("  %s  %s  %s", title, ws, age)

		if i == m.cursor {
			rows = append(rows, StyleWizardItemSelected.Render(line))
		} else {
			rows = append(rows, StyleWizardItem.Render(line))
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// pickerFormatAge returns a human-readable age string for a session's update time.
func pickerFormatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// pickerTruncate shortens s to at most max visible characters, appending "…" if needed.
func pickerTruncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}
