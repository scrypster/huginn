package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/scrypster/huginn/internal/agents"
)

const (
	sidebarWidth    = 22  // columns for the sidebar panel
	sidebarMinTotal = 100 // terminal must be at least this wide to show sidebar
)

// sidebarSection is a navigation group in the sidebar.
type sidebarSection int

const (
	sidebarSectionChannels sidebarSection = iota // channels render first (top)
	sidebarSectionDMs                            // DMs render second (below channels)
)

// sidebarItem represents one row in the sidebar.
type sidebarItem struct {
	kind   sidebarSection
	name   string // agent name or channel name
	color  string // for agents
	unread int    // unread count (0 = none)
	active bool   // true when agent has an in-flight task
}

// sidebarModel is the Slack-style left navigation panel.
type sidebarModel struct {
	visible bool
	focused bool   // true when keyboard focus is in sidebar (vs. chat)
	items   []sidebarItem
	cursor  int    // currently highlighted item index
	active  string // currently active DM / channel
	width   int
}

// SidebarSelectMsg is dispatched when the user picks an item in the sidebar.
type SidebarSelectMsg struct {
	Kind sidebarSection
	Name string
}

// SidebarFocusMsg tells App to give keyboard focus to the sidebar.
type SidebarFocusMsg struct{}

// SidebarBlurMsg tells App to return keyboard focus to the chat input.
type SidebarBlurMsg struct{}

func newSidebarModel() sidebarModel {
	return sidebarModel{visible: false}
}

// IsVisible reports whether the sidebar should be rendered.
func (s sidebarModel) IsVisible() bool { return s.visible }

// AutoShow enables or disables the sidebar based on terminal width.
func (s *sidebarModel) AutoShow(termWidth int) {
	s.visible = termWidth >= sidebarMinTotal
}

// SetAgents rebuilds the DM items from the agent registry.
// Channels are preserved at the FRONT so items[] matches visual render order
// (channels on top, DMs below), keeping cursor navigation visually consistent.
func (s *sidebarModel) SetAgents(reg *agents.AgentRegistry) {
	if reg == nil {
		s.items = nil
		return
	}
	// Preserve existing channel items at front.
	var items []sidebarItem
	for _, existing := range s.items {
		if existing.kind == sidebarSectionChannels {
			items = append(items, existing)
		}
	}
	// Append DMs sorted alphabetically.
	all := reg.All()
	sort.Slice(all, func(i, j int) bool {
		return strings.ToLower(all[i].Name) < strings.ToLower(all[j].Name)
	})
	for _, ag := range all {
		items = append(items, sidebarItem{
			kind:  sidebarSectionDMs,
			name:  ag.Name,
			color: ag.Color,
		})
	}
	s.items = items
	s.clampCursor()
}

// SetChannels replaces channel items. Channels are stored at the FRONT of
// items[] so cursor index 0 maps to the topmost visual row.
func (s *sidebarModel) SetChannels(names []string) {
	// Keep existing DM items.
	var dms []sidebarItem
	for _, it := range s.items {
		if it.kind != sidebarSectionChannels {
			dms = append(dms, it)
		}
	}
	// Build: channels first, then DMs.
	items := make([]sidebarItem, 0, len(names)+len(dms))
	for _, ch := range names {
		items = append(items, sidebarItem{kind: sidebarSectionChannels, name: ch})
	}
	items = append(items, dms...)
	s.items = items
	s.clampCursor()
}

// SetAgentActive marks or clears the "active/working" indicator for a DM agent.
// When active=true, the agent shows "●● working" in the sidebar.
func (s *sidebarModel) SetAgentActive(name string, active bool) {
	for i := range s.items {
		if s.items[i].kind == sidebarSectionDMs && s.items[i].name == name {
			s.items[i].active = active
			return
		}
	}
}

// SetActive marks the currently active agent/channel and moves the cursor to it.
func (s *sidebarModel) SetActive(name string) {
	s.active = name
	for i, it := range s.items {
		if it.name == name {
			s.cursor = i
			return
		}
	}
}

// clampCursor ensures cursor is within [0, len(items)-1].
func (s *sidebarModel) clampCursor() {
	if len(s.items) == 0 {
		s.cursor = 0
		return
	}
	if s.cursor >= len(s.items) {
		s.cursor = len(s.items) - 1
	}
	if s.cursor < 0 {
		s.cursor = 0
	}
}

// Update handles keyboard events when the sidebar has focus.
func (s sidebarModel) Update(msg tea.Msg) (sidebarModel, tea.Cmd) {
	if !s.visible || !s.focused {
		return s, nil
	}

	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}

	switch km.String() {
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(s.items)-1 {
			s.cursor++
		}
	case "enter", " ":
		if len(s.items) > 0 && s.cursor < len(s.items) {
			it := s.items[s.cursor]
			s.focused = false
			return s, func() tea.Msg {
				return SidebarSelectMsg{Kind: it.kind, Name: it.name}
			}
		}
	case "esc", "tab", "ctrl+b":
		s.focused = false
		return s, func() tea.Msg { return SidebarBlurMsg{} }
	}
	return s, nil
}

// View renders the sidebar panel. Returns an empty string when not visible.
func (s sidebarModel) View(height int) string {
	if !s.visible {
		return ""
	}

	w := sidebarWidth

	accentSB := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	dimSB    := lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	activeSB := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Bold(true)
	normalSB := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

	var rows []string

	// ── Header ────────────────────────────────────────────────────────────────
	rows = append(rows, accentSB.Render(padRight("  huginn", w)))
	rows = append(rows, dimSB.Render(strings.Repeat("─", w)))

	// ── Channels section ──────────────────────────────────────────────────────
	rows = append(rows, dimSB.Render(padRight("  Channels", w)))

	chCount := 0
	for i, it := range s.items {
		if it.kind != sidebarSectionChannels {
			continue
		}
		chCount++
		nameStr := "# " + it.name
		if lipgloss.Width(nameStr) > w-4 {
			nameStr = nameStr[:w-7] + "…"
		}
		line := "  " + nameStr
		if it.unread > 0 {
			line += fmt.Sprintf(" (%d)", it.unread)
		}
		rows = append(rows, s.renderItem(i, line, it, activeSB, normalSB, w))
	}
	if chCount == 0 {
		rows = append(rows, dimSB.Render(padRight("  (none)", w)))
	}

	// ── DMs section ───────────────────────────────────────────────────────────
	rows = append(rows, dimSB.Render(padRight("", w))) // blank separator
	rows = append(rows, dimSB.Render(padRight("  DMs", w)))

	dmCount := 0
	for i, it := range s.items {
		if it.kind != sidebarSectionDMs {
			continue
		}
		dmCount++
		nameStr := it.name
		if lipgloss.Width(nameStr) > w-6 {
			nameStr = nameStr[:w-9] + "…"
		}
		line := "  @ " + nameStr
		if it.active {
			line += "  ●● working"
		}
		if it.unread > 0 {
			line += fmt.Sprintf(" (%d)", it.unread)
		}
		rows = append(rows, s.renderItem(i, line, it, activeSB, normalSB, w))
	}
	if dmCount == 0 {
		rows = append(rows, dimSB.Render(padRight("  (none)", w)))
	}

	// ── Footer hint ───────────────────────────────────────────────────────────
	rows = append(rows, dimSB.Render(padRight("", w))) // blank separator
	if s.focused {
		rows = append(rows, dimSB.Render(padRight("  ↑↓ ↵ select · Esc blur", w)))
	} else {
		rows = append(rows, dimSB.Render(padRight("  ctrl+b to focus", w)))
	}

	// Pad to fill height — each padding row must be exactly w chars wide.
	for len(rows) < height {
		rows = append(rows, strings.Repeat(" ", w))
	}

	// Render each row individually and join — avoids ANSI code wrapping issues
	// that arise from rendering a multi-line block with Width().
	lines := rows[:min(len(rows), height)]
	for i, row := range lines {
		// Ensure every row is exactly w visible chars (pad short ones, no-op for already-padded).
		if vw := lipgloss.Width(row); vw < w {
			lines[i] = row + strings.Repeat(" ", w-vw)
		}
	}
	return strings.Join(lines, "\n")
}

// renderItem renders a single sidebar item with correct highlighting.
func (s sidebarModel) renderItem(idx int, line string, it sidebarItem,
	activeSB, normalSB lipgloss.Style, w int) string {
	isActive  := it.name == s.active
	isCursor  := s.focused && idx == s.cursor

	switch {
	case isCursor:
		return StyleWizardItemSelected.Render(padRight(line, w))
	case isActive:
		return activeSB.Render(padRight("▶ "+strings.TrimPrefix(line, "  "), w))
	case it.color != "":
		return StyleAgentLabel(it.color).Render(padRight(line, w))
	default:
		return normalSB.Render(padRight(line, w))
	}
}

// padRight pads a string with spaces to width w (measures visible width, strips ANSI).
func padRight(s string, w int) string {
	visible := lipgloss.Width(s)
	if visible >= w {
		return s
	}
	return s + strings.Repeat(" ", w-visible)
}
