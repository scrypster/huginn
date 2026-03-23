package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/scrypster/huginn/internal/config"
)

// settingsScreenModel is the full-screen workspace and feature configuration view.
type settingsScreenModel struct {
	cfg    *config.Config
	width  int
	height int
	cursor int // selected setting row (for future toggle editing)
}

func newSettingsScreen() settingsScreenModel { return settingsScreenModel{} }

func (s *settingsScreenModel) SetConfig(cfg *config.Config) { s.cfg = cfg }

func (s settingsScreenModel) Update(msg tea.Msg) (settingsScreenModel, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	switch km.String() {
	case "esc", "q":
		return s, func() tea.Msg { return backToChatMsg{} }
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < settingsRowCount-1 {
			s.cursor++
		}
	}
	return s, nil
}

// settingsRowCount is used for cursor bounds. Keep in sync with renderRows().
const settingsRowCount = 12

func (s settingsScreenModel) View(width, height int) string {
	s.width = width
	s.height = height

	header := s.renderHeader()
	footer := s.renderFooter()
	bodyH := height - strings.Count(header, "\n") - 2 - strings.Count(footer, "\n") - 1
	if bodyH < 3 {
		bodyH = 3
	}

	body := s.renderBody(width, bodyH)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (s settingsScreenModel) renderHeader() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Render("  Settings")
	nav := StyleDim.Render("  ← Esc to return to chat")
	gap := strings.Repeat(" ", max(0, s.width-lipgloss.Width(title)-lipgloss.Width(nav)))
	return title + gap + nav
}

func (s settingsScreenModel) renderFooter() string {
	return StyleDim.Render("  Esc back · edit ~/.huginn/config.json to modify settings")
}

func (s settingsScreenModel) renderBody(width, height int) string {
	if s.cfg == nil {
		return lipgloss.NewStyle().MarginLeft(4).MarginTop(2).
			Foreground(lipgloss.Color("196")).Render("Configuration not available.")
	}

	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	dim := StyleDim
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	value := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	good := lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	off := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	boolVal := func(b bool) string {
		if b {
			return good.Render("enabled")
		}
		return off.Render("disabled")
	}
	strVal := func(v, fallback string) string {
		if v == "" {
			return dim.Render(fallback)
		}
		return value.Render(v)
	}

	section := func(title string) string {
		return "\n" + accent.Render("  "+title) + "\n" + dim.Render("  "+strings.Repeat("─", 50))
	}
	row := func(k, v string) string {
		return label.Render("    "+k+":") + "  " + v
	}

	var lines []string
	lines = append(lines, "")

	// ── Workspace ─────────────────────────────────────────────────────────────
	lines = append(lines, section("Workspace"))
	lines = append(lines, row("Root", strVal(s.cfg.WorkspacePath, "(auto-detected from .git)")))
	lines = append(lines, row("Notepads", boolVal(s.cfg.NotepadsEnabled)))
	if s.cfg.NotepadsMaxTokens > 0 {
		lines = append(lines, row("Notepads max tokens", fmt.Sprintf("%d", s.cfg.NotepadsMaxTokens)))
	}

	// ── Tools ─────────────────────────────────────────────────────────────────
	lines = append(lines, section("Tools"))
	lines = append(lines, row("Tools enabled", boolVal(s.cfg.ToolsEnabled)))
	lines = append(lines, row("Auto-run (approve all)", boolVal(true))) // shown as current autoRun state — always true at startup

	bashTimeout := s.cfg.BashTimeoutSecs
	if bashTimeout == 0 {
		bashTimeout = 120
	}
	lines = append(lines, row("Bash timeout", fmt.Sprintf("%ds", bashTimeout)))

	maxTurns := s.cfg.MaxTurns
	if maxTurns == 0 {
		maxTurns = 50
	}
	lines = append(lines, row("Max agent turns", fmt.Sprintf("%d", maxTurns)))

	if len(s.cfg.AllowedTools) > 0 {
		lines = append(lines, row("Allowed tools", value.Render(strings.Join(s.cfg.AllowedTools, ", "))))
	}
	if len(s.cfg.DisallowedTools) > 0 {
		lines = append(lines, row("Disallowed tools", value.Render(strings.Join(s.cfg.DisallowedTools, ", "))))
	}

	// ── Display ───────────────────────────────────────────────────────────────
	lines = append(lines, section("Display"))
	lines = append(lines, row("Theme", strVal(s.cfg.Theme, "dark (default)")))

	compactMode := s.cfg.CompactMode
	if compactMode == "" {
		compactMode = "auto (default)"
	}
	lines = append(lines, row("Compact mode", value.Render(compactMode)))

	// ── AI Features ───────────────────────────────────────────────────────────
	lines = append(lines, section("AI Features"))
	lines = append(lines, row("Vision (image attachments)", boolVal(s.cfg.VisionEnabled)))
	if s.cfg.MaxImageSizeKB > 0 {
		lines = append(lines, row("Max image size", fmt.Sprintf("%d KB", s.cfg.MaxImageSizeKB)))
	}

	diffMode := s.cfg.DiffReviewMode
	if diffMode == "" {
		diffMode = "auto (default)"
	}
	lines = append(lines, row("Diff review mode", value.Render(diffMode)))

	semanticSearch := s.cfg.SemanticSearch
	lines = append(lines, row("Semantic search", boolVal(semanticSearch)))
	if s.cfg.EmbeddingModel != "" {
		lines = append(lines, row("Embedding model", value.Render(s.cfg.EmbeddingModel)))
	}

	if s.cfg.BraveAPIKey != "" {
		lines = append(lines, row("Web search (Brave)", good.Render("configured")))
	} else {
		lines = append(lines, row("Web search (Brave)", off.Render("no API key")))
	}

	// ── Misc ──────────────────────────────────────────────────────────────────
	lines = append(lines, section("Misc"))
	lines = append(lines, row("Scheduler", boolVal(s.cfg.SchedulerEnabled)))
	lines = append(lines, row("Git stage on write", boolVal(s.cfg.GitStageOnWrite)))
	if s.cfg.MachineID != "" {
		masked := s.cfg.MachineID
		if len(masked) > 12 {
			masked = masked[:12] + "…"
		}
		lines = append(lines, row("Machine ID", dim.Render(masked)))
	}

	// Pad to fill height
	for len(lines) < height {
		lines = append(lines, "")
	}

	content := strings.Join(lines[:min(len(lines), height)], "\n")
	return lipgloss.NewStyle().MarginLeft(2).Render(content)
}
