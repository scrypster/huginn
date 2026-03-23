package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/scrypster/huginn/internal/skills"
)

// SlashCommand represents a selectable slash command.
type SlashCommand struct {
	Name        string
	Description string
	Args        string
}

// utilityCommands is the hardcoded list of utility slash commands.
// The skill-based commands (plan, code, reason, iterate, parallel, agents, switch-model)
// are now provided by the SkillRegistry as built-in skills.
var utilityCommands = []SlashCommand{
	{Name: "help", Description: "Show help and keybindings"},
	{Name: "impact", Description: "Show call graph for a symbol — who references it and with what confidence", Args: "<symbol>"},
	{Name: "stats", Description: "Show live index stats: model latency, cache hit rate, files indexed"},
	{Name: "workspace", Description: "Show active workspace, repos discovered, and index status"},
	{Name: "radar", Description: "Show Proactive Impact Radar findings — drift, cycles, high-risk changes"},
	{Name: "notepad", Description: "Manage persistent notepads — list, show, create, delete", Args: "[list|show|create|delete] [name]"},
	{Name: "resume", Description: "Resume a previous session"},
	{Name: "save", Description: "Force-save current session"},
	{Name: "title", Description: "Rename current session", Args: "<text>"},
	{Name: "swarm", Description: "Show swarm agent progress"},
}

// skillToSlashCommand converts a Skill to a SlashCommand for display in the wizard.
func skillToSlashCommand(s skills.Skill) SlashCommand {
	return SlashCommand{Name: s.Name(), Description: s.Description()}
}

// WizardModel is the slash-command picker sub-model.
type WizardModel struct {
	visible  bool
	filter   string
	cursor   int
	filtered []SlashCommand
	registry *skills.SkillRegistry
}

// WizardSelectMsg is sent when the user selects a command.
type WizardSelectMsg struct {
	Command SlashCommand
}

// WizardDismissMsg is sent when the wizard is dismissed without selection.
type WizardDismissMsg struct{}

// WizardTabCompleteMsg is sent when Tab is pressed — carries the text to set
// in the input after the "/", completing to the only match or common prefix.
type WizardTabCompleteMsg struct {
	Text string
}

func newWizardModel() WizardModel {
	return WizardModel{}
}

// SetRegistry wires a SkillRegistry into the wizard so skills appear in the picker.
func (w *WizardModel) SetRegistry(reg *skills.SkillRegistry) {
	w.registry = reg
}

// Show makes the wizard visible and applies an initial filter.
func (w *WizardModel) Show(filter string) {
	w.visible = true
	w.filter = filter
	w.filtered = w.filteredCommands(filter)
	if w.cursor >= len(w.filtered) {
		w.cursor = 0
	}
}

// UpdateFilter re-filters the command list without resetting cursor unless needed.
func (w *WizardModel) UpdateFilter(filter string) {
	w.filter = filter
	w.filtered = w.filteredCommands(filter)
	if w.cursor >= len(w.filtered) {
		w.cursor = 0
	}
}

// Hide dismisses the wizard.
func (w *WizardModel) Hide() {
	w.visible = false
	w.filter = ""
	w.cursor = 0
	w.filtered = nil
}

// Visible returns whether the wizard is showing.
func (w WizardModel) Visible() bool { return w.visible }

// Update handles keypresses while the wizard is visible.
func (w WizardModel) Update(msg tea.Msg) (WizardModel, tea.Cmd) {
	if !w.visible {
		return w, nil
	}

	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.String() {
		case "esc":
			w.Hide()
			return w, func() tea.Msg { return WizardDismissMsg{} }

		case "enter":
			if len(w.filtered) > 0 {
				cmd := w.filtered[w.cursor]
				w.Hide()
				return w, func() tea.Msg { return WizardSelectMsg{Command: cmd} }
			}

		case "tab":
			if len(w.filtered) == 0 {
				return w, nil
			}
			text := commonPrefix(w.filtered)
			return w, func() tea.Msg { return WizardTabCompleteMsg{Text: text} }

		case "up", "ctrl+p":
			if w.cursor > 0 {
				w.cursor--
			}

		case "down", "ctrl+n":
			if w.cursor < len(w.filtered)-1 {
				w.cursor++
			}
		}
	}

	return w, nil
}

// commonPrefix returns the longest common prefix shared by all filtered command names.
func commonPrefix(cmds []SlashCommand) string {
	if len(cmds) == 0 {
		return ""
	}
	prefix := cmds[0].Name
	for _, c := range cmds[1:] {
		for !strings.HasPrefix(c.Name, prefix) {
			prefix = prefix[:len(prefix)-1]
			if prefix == "" {
				return ""
			}
		}
	}
	return prefix
}

// View renders the wizard as a compact two-column list (no border box).
func (w WizardModel) View(width int) string {
	if !w.visible {
		return ""
	}

	// Hint line.
	hint := StyleDim.Render("  ↑↓ navigate · tab complete · enter select · esc dismiss")

	if len(w.filtered) == 0 {
		return lipgloss.JoinVertical(lipgloss.Left,
			hint,
			StyleDim.Render("  no commands match"),
		)
	}

	// Column widths: name col is fixed at 18 chars, description fills the rest.
	nameCol := 18
	descWidth := width - nameCol - 4
	if descWidth < 20 {
		descWidth = 20
	}

	var rows []string
	rows = append(rows, hint)

	for i, cmd := range w.filtered {
		name := "/" + cmd.Name
		if cmd.Args != "" {
			name += " " + cmd.Args
		}
		// Pad name to column width.
		padded := name
		for len(padded) < nameCol {
			padded += " "
		}

		// Truncate description.
		desc := cmd.Description
		if len(desc) > descWidth {
			desc = desc[:descWidth-1] + "…"
		}

		line := "  " + padded + "  " + desc

		if i == w.cursor {
			rows = append(rows, StyleWizardItemSelected.Render(line))
		} else {
			rows = append(rows, StyleWizardItem.Render(line))
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// filteredCommands returns commands matching the typed text after '/'.
// It combines registry skills (if a registry is wired) with utilityCommands.
func (w *WizardModel) filteredCommands(after string) []SlashCommand {
	// Build source list: registry skills first, then utility commands.
	var source []SlashCommand
	if w.registry != nil {
		for _, s := range w.registry.All() {
			source = append(source, skillToSlashCommand(s))
		}
	}
	source = append(source, utilityCommands...)

	after = strings.ToLower(strings.TrimSpace(after))
	if after == "" {
		return source
	}
	var result []SlashCommand
	for _, c := range source {
		if strings.HasPrefix(c.Name, after) || strings.Contains(c.Description, after) {
			result = append(result, c)
		}
	}
	return result
}
