package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/scrypster/huginn/internal/config"
)

// modelsScreenModel is the full-screen LLM provider and model configuration view.
type modelsScreenModel struct {
	cfg    *config.Config
	width  int
	height int
}

func newModelsScreen() modelsScreenModel { return modelsScreenModel{} }

func (s *modelsScreenModel) SetConfig(cfg *config.Config) { s.cfg = cfg }

// Update handles keyboard input for the Models screen.
func (s modelsScreenModel) Update(msg tea.Msg) (modelsScreenModel, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, nil
	}
	switch km.String() {
	case "esc", "q":
		return s, func() tea.Msg { return backToChatMsg{} }
	}
	return s, nil
}

// View renders the Models screen.
func (s modelsScreenModel) View(width, height int) string {
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

func (s modelsScreenModel) renderHeader() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Render("  Models & Providers")
	nav := StyleDim.Render("  ← Esc to return to chat")
	gap := strings.Repeat(" ", max(0, s.width-lipgloss.Width(title)-lipgloss.Width(nav)))
	return title + gap + nav
}

func (s modelsScreenModel) renderFooter() string {
	return StyleDim.Render("  Esc back · edit ~/.huginn/config.json to modify")
}

func (s modelsScreenModel) renderBody(width, height int) string {
	if s.cfg == nil {
		return lipgloss.NewStyle().MarginLeft(4).MarginTop(2).
			Foreground(lipgloss.Color("196")).
			Render("Configuration not available.")
	}

	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	value := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	dim := StyleDim
	good := lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	bad := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

	section := func(title string) string {
		return "\n" + accent.Render("  "+title) + "\n" + dim.Render("  "+strings.Repeat("─", 50))
	}
	row := func(k, v string) string {
		return label.Render("    "+k+":") + "  " + value.Render(v)
	}
	status := func(ok bool, msg string) string {
		if ok {
			return good.Render("  ✓  " + msg)
		}
		return warn.Render("  ⚠  " + msg)
	}

	var lines []string
	lines = append(lines, "")

	// ── Backend ───────────────────────────────────────────────────────────────
	lines = append(lines, section("Backend"))
	bc := s.cfg.Backend

	providerDisplay := bc.Provider
	if providerDisplay == "" {
		providerDisplay = bc.Type
	}
	if providerDisplay == "" {
		providerDisplay = "(not configured)"
	}
	lines = append(lines, row("Provider", providerDisplay))
	lines = append(lines, row("Type", bc.Type))

	if bc.Endpoint != "" {
		lines = append(lines, row("Endpoint", bc.Endpoint))
	}

	// API key status
	resolvedKey := bc.ResolvedAPIKey()
	if resolvedKey != "" {
		maskedKey := resolvedKey[:min(8, len(resolvedKey))] + strings.Repeat("*", 8)
		lines = append(lines, status(true, "API key configured  "+dim.Render(maskedKey)))
	} else if bc.APIKey != "" {
		// APIKey is an env var reference ($ENV_VAR) but the env var is unset.
		lines = append(lines, bad.Render("  ✗  API key env var "+bc.APIKey+" is not set"))
	} else if bc.Type != "managed" {
		lines = append(lines, warn.Render("  ⚠  No API key — add api_key to ~/.huginn/config.json"))
	}

	// Managed backend model
	if bc.Type == "managed" && bc.BuiltinModel != "" {
		lines = append(lines, row("Built-in model", bc.BuiltinModel))
		lines = append(lines, status(true, "Managed (local) backend"))
	}

	// Ollama URL
	if s.cfg.OllamaBaseURL != "" {
		lines = append(lines, row("Ollama URL", s.cfg.OllamaBaseURL))
	}

	// ── Default model ─────────────────────────────────────────────────────────
	lines = append(lines, section("Default Model"))

	{
		modelStr := s.cfg.DefaultModel
		if modelStr == "" {
			modelStr = "(not set)"
		}
		isSet := s.cfg.DefaultModel != ""
		indicator := good.Render("●")
		if !isSet {
			indicator = warn.Render("○")
		}
		lines = append(lines, fmt.Sprintf("    %s  %s  %s",
			indicator,
			label.Render("default_model"),
			value.Render(modelStr),
		))
	}

	// ── Providers quick-ref ───────────────────────────────────────────────────
	lines = append(lines, section("Quick Setup"))
	lines = append(lines, dim.Render("    To switch providers, edit ~/.huginn/config.json:"))
	lines = append(lines, "")
	examples := []struct{ name, snippet string }{
		{"Anthropic", `"provider": "anthropic", "api_key": "$ANTHROPIC_API_KEY"`},
		{"OpenAI", `"provider": "openai", "api_key": "$OPENAI_API_KEY"`},
		{"Ollama", `"provider": "ollama"  (no API key needed)`},
		{"OpenRouter", `"provider": "openrouter", "api_key": "$OPENROUTER_API_KEY"`},
	}
	for _, ex := range examples {
		active := bc.Provider == strings.ToLower(ex.name)
		prefix := "    "
		if active {
			prefix = good.Render("  ► ")
		} else {
			prefix = dim.Render("    ")
		}
		lines = append(lines, prefix+value.Render(ex.name)+"  "+dim.Render(ex.snippet))
	}

	// Pad to fill height
	for len(lines) < height {
		lines = append(lines, "")
	}

	content := strings.Join(lines[:min(len(lines), height)], "\n")
	return lipgloss.NewStyle().MarginLeft(2).Render(content)
}
