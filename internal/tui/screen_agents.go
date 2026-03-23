package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/scrypster/huginn/internal/agents"
)

// agentsScreenMode controls what sub-state the Agents screen is in.
type agentsScreenMode int

const (
	agentsScreenList    agentsScreenMode = iota // normal list browse
	agentsScreenConfirm                         // confirm delete dialog
)

// agentsScreenModel is the full-screen agent management view.
// Activated via /agents (no args) or the screen router screenAgents.
type agentsScreenModel struct {
	agents  []*agents.Agent // cached roster
	cursor  int
	mode    agentsScreenMode
	width   int
	height  int
	msgLine string // transient status line (e.g. "Agent deleted")
}

// agentsScreenSelectMsg is sent when the user selects an agent as primary from this screen.
type agentsScreenSelectMsg struct{ name string }

// agentsScreenCreateMsg triggers opening the agent creation wizard.
type agentsScreenCreateMsg struct{}

// agentsScreenDeleteMsg triggers deletion of the named agent.
type agentsScreenDeleteMsg struct{ name string }

func newAgentsScreen() agentsScreenModel { return agentsScreenModel{} }

// refresh reloads the agent list from the registry.
func (s *agentsScreenModel) refresh(reg *agents.AgentRegistry) {
	if reg == nil {
		s.agents = nil
		return
	}
	all := reg.All()
	sort.Slice(all, func(i, j int) bool {
		return strings.ToLower(all[i].Name) < strings.ToLower(all[j].Name)
	})
	s.agents = all
	if s.cursor >= len(s.agents) {
		s.cursor = max(0, len(s.agents)-1)
	}
}

func (s agentsScreenModel) selected() *agents.Agent {
	if len(s.agents) == 0 || s.cursor < 0 || s.cursor >= len(s.agents) {
		return nil
	}
	return s.agents[s.cursor]
}

// Update handles keyboard input for the Agents screen.
// Returns (model, cmd) — cmd is non-nil when an action should bubble up to App.
func (s agentsScreenModel) Update(msg tea.Msg) (agentsScreenModel, tea.Cmd) {
	switch s.mode {
	case agentsScreenConfirm:
		return s.updateConfirm(msg)
	default:
		return s.updateList(msg)
	}
}

func (s agentsScreenModel) updateList(msg tea.Msg) (agentsScreenModel, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	s.msgLine = "" // clear transient status on any key
	switch km.String() {
	case "esc", "q":
		return s, func() tea.Msg { return backToChatMsg{} }

	case "up", "ctrl+p", "k":
		if s.cursor > 0 {
			s.cursor--
		}

	case "down", "ctrl+n", "j":
		if s.cursor < len(s.agents)-1 {
			s.cursor++
		}

	case "enter", " ":
		// Set selected agent as primary.
		if ag := s.selected(); ag != nil {
			name := ag.Name
			return s, func() tea.Msg { return agentsScreenSelectMsg{name: name} }
		}

	case "n", "ctrl+a":
		return s, func() tea.Msg { return agentsScreenCreateMsg{} }

	case "d":
		if ag := s.selected(); ag != nil {
			s.mode = agentsScreenConfirm
		}
	}
	return s, nil
}

func (s agentsScreenModel) updateConfirm(msg tea.Msg) (agentsScreenModel, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	switch strings.ToLower(km.String()) {
	case "y", "enter":
		ag := s.selected()
		if ag == nil {
			s.mode = agentsScreenList
			return s, nil
		}
		name := ag.Name
		s.mode = agentsScreenList
		return s, func() tea.Msg { return agentsScreenDeleteMsg{name: name} }
	case "n", "esc", "q":
		s.mode = agentsScreenList
	}
	return s, nil
}

// ── View ─────────────────────────────────────────────────────────────────────

func (s agentsScreenModel) View(width, height int) string {
	s.width = width
	s.height = height

	header := s.renderHeader()
	footer := s.renderFooterHint()
	bodyH := height - strings.Count(header, "\n") - 1 - strings.Count(footer, "\n") - 1
	if bodyH < 3 {
		bodyH = 3
	}

	if s.mode == agentsScreenConfirm {
		return lipgloss.JoinVertical(lipgloss.Left,
			header,
			s.renderConfirm(width, bodyH),
			footer,
		)
	}

	body := s.renderBody(width, bodyH)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (s agentsScreenModel) renderHeader() string {
	count := fmt.Sprintf("(%d)", len(s.agents))
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Render("  Agents " + count)
	nav := StyleDim.Render("  ← Esc to return to chat")
	gap := strings.Repeat(" ", max(0, s.width-lipgloss.Width(title)-lipgloss.Width(nav)))
	return title + gap + nav
}

func (s agentsScreenModel) renderFooterHint() string {
	if s.msgLine != "" {
		return StyleDim.Render("  " + s.msgLine)
	}
	return StyleDim.Render("  ↑↓ navigate · Enter set primary · n new · d delete · Esc back")
}

func (s agentsScreenModel) renderBody(width, height int) string {
	if len(s.agents) == 0 {
		return lipgloss.NewStyle().MarginLeft(4).MarginTop(2).
			Foreground(lipgloss.Color("244")).
			Render("No agents configured.\n\nPress n or Ctrl+A to create your first agent.")
	}

	// Two-panel layout when wide enough (≥ 80 cols).
	const minWideWidth = 80
	if width >= minWideWidth {
		return s.renderTwoPanel(width, height)
	}
	return s.renderSinglePanel(width, height)
}

const listPanelWidth = 32

func (s agentsScreenModel) renderTwoPanel(width, height int) string {
	listW := listPanelWidth
	detailW := width - listW - 3 // 3 for separator

	listLines := s.renderList(listW, height)
	detailLines := s.renderDetail(detailW, height)

	sep := StyleDim.Render(strings.Repeat("│\n", height))

	return lipgloss.JoinHorizontal(lipgloss.Top, listLines, sep, detailLines)
}

func (s agentsScreenModel) renderSinglePanel(width, height int) string {
	return s.renderList(width, height)
}

func (s agentsScreenModel) renderList(width, height int) string {
	var rows []string
	for i, ag := range s.agents {
		icon := ag.Icon
		if icon == "" && len(ag.Name) > 0 {
			icon = string([]rune(ag.Name)[:1])
		}

		defaultMark := "  "
		if ag.IsDefault {
			defaultMark = "★ "
		}

		modelStr := ag.ModelID
		if ag.ModelID == "" {
			modelStr = ag.Provider
		}
		if len(modelStr) > 18 {
			modelStr = modelStr[:17] + "…"
		}

		nameStr := ag.Name
		if len(nameStr) > 14 {
			nameStr = nameStr[:13] + "…"
		}

		line := fmt.Sprintf(" %s%s %-14s  %s", defaultMark, icon, nameStr, modelStr)

		if ag.Color != "" {
			colorStyle := StyleAgentLabel(ag.Color)
			if i == s.cursor {
				line = StyleWizardItemSelected.Render(line)
			} else {
				line = colorStyle.Render(line)
			}
		} else {
			if i == s.cursor {
				line = StyleWizardItemSelected.Render(line)
			} else {
				line = StyleWizardItem.Render(line)
			}
		}
		rows = append(rows, line)
	}

	// Pad to fill height.
	for len(rows) < height {
		rows = append(rows, "")
	}

	return strings.Join(rows[:min(len(rows), height)], "\n")
}

func (s agentsScreenModel) renderDetail(width, height int) string {
	ag := s.selected()
	if ag == nil {
		return ""
	}

	dim := StyleDim
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	value := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	pad := func(k, v string) string {
		return label.Render("  "+k+": ") + value.Render(v)
	}

	var lines []string
	lines = append(lines, "")

	// Name + icon
	icon := ag.Icon
	if icon == "" && len(ag.Name) > 0 {
		icon = string([]rune(ag.Name)[:1])
	}
	title := icon + "  " + ag.Name
	if ag.IsDefault {
		title += "  ★"
	}
	lines = append(lines, accent.Render("  "+title))
	lines = append(lines, "")

	// Model + provider
	modelID := ag.ModelID
	if modelID == "" {
		modelID = "(inherits default)"
	}
	lines = append(lines, pad("Model", modelID))
	if ag.Provider != "" {
		lines = append(lines, pad("Provider", ag.Provider))
	}

	// Color
	if ag.Color != "" {
		swatch := StyleAgentLabel(ag.Color).Render("  ██  ")
		lines = append(lines, label.Render("  Color: ")+swatch+dim.Render(" "+ag.Color))
	}

	lines = append(lines, "")

	// Memory
	if ag.MemoryEnabled {
		lines = append(lines, pad("Memory", "enabled · "+ag.MemoryMode))
	} else {
		lines = append(lines, pad("Memory", "disabled"))
	}

	// Skills
	if len(ag.Skills) > 0 {
		lines = append(lines, pad("Skills", strings.Join(ag.Skills, ", ")))
	}

	// Tools
	if len(ag.LocalTools) > 0 {
		toolStr := strings.Join(ag.LocalTools, ", ")
		if len(toolStr) > width-14 {
			toolStr = toolStr[:width-17] + "…"
		}
		lines = append(lines, pad("Tools", toolStr))
	}

	// System prompt preview
	if ag.SystemPrompt != "" {
		lines = append(lines, "")
		lines = append(lines, label.Render("  System prompt:"))
		// Show first 3 lines of system prompt
		promptLines := strings.Split(ag.SystemPrompt, "\n")
		shown := 0
		for _, pl := range promptLines {
			if shown >= 3 {
				lines = append(lines, dim.Render("  …"))
				break
			}
			trimmed := strings.TrimSpace(pl)
			if trimmed == "" {
				continue
			}
			if len(trimmed) > width-4 {
				trimmed = trimmed[:width-7] + "…"
			}
			lines = append(lines, dim.Render("    "+trimmed))
			shown++
		}
	}

	// Pad to fill height
	for len(lines) < height {
		lines = append(lines, "")
	}

	return strings.Join(lines[:min(len(lines), height)], "\n")
}

func (s agentsScreenModel) renderConfirm(width, height int) string {
	ag := s.selected()
	if ag == nil {
		return ""
	}
	prompt := fmt.Sprintf("  Delete agent %q? This cannot be undone.", ag.Name)
	hint := StyleDim.Render("  [y] yes  [n / Esc] cancel")
	warning := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Render(prompt)

	var lines []string
	for i := 0; i < height/2-1; i++ {
		lines = append(lines, "")
	}
	lines = append(lines, warning)
	lines = append(lines, hint)

	return strings.Join(lines, "\n")
}

