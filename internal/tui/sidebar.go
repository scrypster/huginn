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
	// forYouGlyph is the follow/@me rail badge (Vue company-rail blue dot).
	// Distinct from the "●● working" activity indicator.
	forYouGlyph = "•"
)

// sidebarSection is a navigation group in the sidebar.
type sidebarSection int

const (
	sidebarSectionChannels sidebarSection = iota // channels render first (top)
	sidebarSectionDMs                            // DMs render second (below channels)
	sidebarSectionCompany                        // collapsed company rail
)

// SidebarSpace is one GET /spaces row mapped into the TUI rail.
// ForYou is the mention/@me badge. Unseen is the spectator quiet count.
type SidebarSpace struct {
	ID        string
	Name      string
	Kind      string
	LeadAgent string
	CompanyID string
	Unseen    int
	ForYou    bool
}

// SidebarCompany is one company row on the TUI rail.
type SidebarCompany struct {
	ID   string
	Name string
}

// sidebarItem represents one row in the sidebar.
type sidebarItem struct {
	kind      sidebarSection
	name      string // agent name, channel name, or company name
	color     string // for agents
	unread    int    // spectator unseen count (0 = none); not the for-you badge
	forYou    bool   // mention/@me rail badge
	active    bool   // true when agent has an in-flight task
	companyID string
	spaceID   string
	collapsed bool // company rows only
}

// sidebarModel is the Slack-style left navigation panel.
type sidebarModel struct {
	visible   bool
	focused   bool // true when keyboard focus is in sidebar (vs. chat)
	items     []sidebarItem
	cursor    int    // currently highlighted item index
	active    string // currently active DM / channel
	width     int
	spaces    []SidebarSpace
	companies []SidebarCompany
	collapsed map[string]bool
	dmAgents  []sidebarItem
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
	return sidebarModel{visible: false, collapsed: map[string]bool{}}
}

// IsVisible reports whether the sidebar should be rendered.
func (s sidebarModel) IsVisible() bool { return s.visible }

// AutoShow enables or disables the sidebar based on terminal width.
func (s *sidebarModel) AutoShow(termWidth int) {
	s.visible = termWidth >= sidebarMinTotal
}

// companyRailForYou reports the collapsed-company badge: follow/@me only.
// Spectator unseen_count must not light the rail (same as Vue forYou).
func companyRailForYou(companyID string, spaces []SidebarSpace) bool {
	if companyID == "" {
		return false
	}
	for _, sp := range spaces {
		if sp.CompanyID == companyID && sp.ForYou {
			return true
		}
	}
	return false
}

func spaceItem(sp SidebarSpace, kind sidebarSection) sidebarItem {
	name := sp.Name
	if kind == sidebarSectionDMs && name == "" {
		name = sp.LeadAgent
	}
	return sidebarItem{
		kind:      kind,
		name:      name,
		unread:    sp.Unseen,
		forYou:    sp.ForYou,
		companyID: sp.CompanyID,
		spaceID:   sp.ID,
	}
}

func findDMSpace(spaces []SidebarSpace, name string) (SidebarSpace, bool) {
	for _, sp := range spaces {
		if sp.Kind != "dm" {
			continue
		}
		if strings.EqualFold(sp.LeadAgent, name) || strings.EqualFold(sp.Name, name) {
			return sp, true
		}
	}
	return SidebarSpace{}, false
}

func (s *sidebarModel) rebuildRail() {
	if s.collapsed == nil {
		s.collapsed = map[string]bool{}
	}
	items := make([]sidebarItem, 0, len(s.spaces)+len(s.dmAgents)+len(s.companies))

	for _, sp := range s.spaces {
		if sp.Kind == "dm" {
			continue
		}
		if sp.CompanyID != "" {
			continue
		}
		items = append(items, spaceItem(sp, sidebarSectionChannels))
	}

	for _, dm := range s.dmAgents {
		if sp, ok := findDMSpace(s.spaces, dm.name); ok && sp.CompanyID != "" {
			continue
		}
		if sp, ok := findDMSpace(s.spaces, dm.name); ok {
			dm.unread = sp.Unseen
			dm.forYou = sp.ForYou
			dm.companyID = sp.CompanyID
			dm.spaceID = sp.ID
		}
		items = append(items, dm)
	}

	for _, c := range s.companies {
		collapsed := s.collapsed[c.ID]
		items = append(items, sidebarItem{
			kind:      sidebarSectionCompany,
			name:      c.Name,
			companyID: c.ID,
			collapsed: collapsed,
			forYou:    companyRailForYou(c.ID, s.spaces),
		})
		if collapsed {
			continue
		}
		for _, sp := range s.spaces {
			if sp.CompanyID != c.ID {
				continue
			}
			kind := sidebarSectionChannels
			if sp.Kind == "dm" {
				kind = sidebarSectionDMs
			}
			items = append(items, spaceItem(sp, kind))
		}
	}
	s.items = items
	s.clampCursor()
}

// SetAgents rebuilds the DM items from the agent registry.
// Channels are preserved at the FRONT so items[] matches visual render order
// (channels on top, DMs below), keeping cursor navigation visually consistent.
func (s *sidebarModel) SetAgents(reg *agents.AgentRegistry) {
	if reg == nil {
		s.dmAgents = nil
		s.items = nil
		return
	}
	all := reg.All()
	sort.Slice(all, func(i, j int) bool {
		return strings.ToLower(all[i].Name) < strings.ToLower(all[j].Name)
	})
	dms := make([]sidebarItem, 0, len(all))
	for _, ag := range all {
		dms = append(dms, sidebarItem{
			kind:  sidebarSectionDMs,
			name:  ag.Name,
			color: ag.Color,
		})
	}
	s.dmAgents = dms
	s.rebuildRail()
}

// SetChannels replaces channel items. Channels are stored at the FRONT of
// items[] so cursor index 0 maps to the topmost visual row.
func (s *sidebarModel) SetChannels(names []string) {
	next := make([]SidebarSpace, 0, len(names))
	for _, ch := range names {
		next = append(next, SidebarSpace{Name: ch, Kind: "channel"})
	}
	s.spaces = next
	s.rebuildRail()
}

// SetSpaceRail maps GET /spaces (+ companies) onto the sidebar/company rail.
// Badge (forYou) is independent of Unseen — spectator chatter stays a count.
func (s *sidebarModel) SetSpaceRail(spaces []SidebarSpace, companies []SidebarCompany) {
	s.spaces = append([]SidebarSpace(nil), spaces...)
	s.companies = append([]SidebarCompany(nil), companies...)
	s.rebuildRail()
}

// SetAgentActive marks or clears the "active/working" indicator for a DM agent.
// When active=true, the agent shows "●● working" in the sidebar.
func (s *sidebarModel) SetAgentActive(name string, active bool) {
	for i := range s.dmAgents {
		if s.dmAgents[i].name == name {
			s.dmAgents[i].active = active
		}
	}
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
			if it.kind == sidebarSectionCompany {
				if s.collapsed == nil {
					s.collapsed = map[string]bool{}
				}
				s.collapsed[it.companyID] = !s.collapsed[it.companyID]
				s.rebuildRail()
				return s, nil
			}
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

// spaceLine is the label for a channel/DM row: for-you badge, then quiet count.
func spaceLine(it sidebarItem, prefix string, w int) string {
	nameStr := prefix + it.name
	budget := w - 4
	if budget < 6 {
		budget = 6
	}
	if lipgloss.Width(nameStr) > budget {
		if budget > 3 {
			nameStr = nameStr[:budget-3] + "…"
		}
	}
	line := "  " + nameStr
	if it.kind == sidebarSectionDMs && it.active {
		line += "  ●● working"
	}
	if it.forYou {
		line += " " + forYouGlyph
	}
	if it.unread > 0 {
		line += fmt.Sprintf(" (%d)", it.unread)
	}
	return line
}

// View renders the sidebar panel. Returns an empty string when not visible.
func (s sidebarModel) View(height int) string {
	if !s.visible {
		return ""
	}

	w := sidebarWidth

	accentSB := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	dimSB := lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	activeSB := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Bold(true)
	normalSB := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	forYouSB := lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true) // Vue huginn-blue

	var rows []string

	// ── Header ────────────────────────────────────────────────────────────────
	rows = append(rows, accentSB.Render(padRight("  huginn", w)))
	rows = append(rows, dimSB.Render(strings.Repeat("─", w)))

	// ── Channels section ──────────────────────────────────────────────────────
	rows = append(rows, dimSB.Render(padRight("  Channels", w)))

	chCount := 0
	for i, it := range s.items {
		if it.kind != sidebarSectionChannels || it.companyID != "" {
			continue
		}
		chCount++
		rows = append(rows, s.renderItem(i, spaceLine(it, "# ", w), it, activeSB, normalSB, w))
	}
	if chCount == 0 {
		rows = append(rows, dimSB.Render(padRight("  (none)", w)))
	}

	// ── DMs section ───────────────────────────────────────────────────────────
	rows = append(rows, dimSB.Render(padRight("", w))) // blank separator
	rows = append(rows, dimSB.Render(padRight("  DMs", w)))

	dmCount := 0
	for i, it := range s.items {
		if it.kind != sidebarSectionDMs || it.companyID != "" {
			continue
		}
		dmCount++
		rows = append(rows, s.renderItem(i, spaceLine(it, "@ ", w), it, activeSB, normalSB, w))
	}
	if dmCount == 0 {
		rows = append(rows, dimSB.Render(padRight("  (none)", w)))
	}

	// ── Company rail ──────────────────────────────────────────────────────────
	if len(s.companies) > 0 {
		rows = append(rows, dimSB.Render(padRight("", w)))
		rows = append(rows, dimSB.Render(padRight("  Companies", w)))
		for i, it := range s.items {
			if it.kind == sidebarSectionCompany {
				mark := "▸"
				if !it.collapsed {
					mark = "▾"
				}
				name := it.name
				if lipgloss.Width(name) > w-8 {
					name = name[:w-11] + "…"
				}
				line := "  " + mark + " " + name
				if it.collapsed && it.forYou {
					line += " " + forYouSB.Render(forYouGlyph)
				}
				rows = append(rows, s.renderItem(i, line, it, activeSB, normalSB, w))
				continue
			}
			if it.companyID == "" {
				continue
			}
			prefix := "# "
			if it.kind == sidebarSectionDMs {
				prefix = "@ "
			}
			rows = append(rows, s.renderItem(i, spaceLine(it, "  "+prefix, w), it, activeSB, normalSB, w))
		}
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
	isActive := it.name == s.active
	isCursor := s.focused && idx == s.cursor

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
