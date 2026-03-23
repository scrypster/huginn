package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Palette — GitHub dark + Cursor-style accents
	colorBg      = lipgloss.Color("#0D1117")
	colorSurface = lipgloss.Color("#161B22")
	colorBorder  = lipgloss.Color("#30363D")
	colorGold    = lipgloss.Color("#D4A017") // follow-ups box border
	colorGreen   = lipgloss.Color("#3FB950") // generating dot, tool done
	colorBlue    = lipgloss.Color("#58A6FF")
	colorYellow  = lipgloss.Color("#D29922")
	colorRed     = lipgloss.Color("#F85149")
	colorDim     = lipgloss.Color("#6E7681")
	colorMuted   = lipgloss.Color("#8B949E")
	colorAccent  = lipgloss.Color("#BB86FC")
	colorWhite   = lipgloss.Color("#E6EDF3")
	colorTeal    = lipgloss.Color("#56D364") // auto-run status line

	// ── Chat messages ──────────────────────────────────────────────────────

	StyleUserLabel = lipgloss.NewStyle().
			Foreground(colorBlue).
			Bold(true)

	StyleUserMsg = lipgloss.NewStyle().
			Foreground(colorWhite)

	// StyleUserBox is the Cursor-style box for user messages: subtle dark bg, padding.
	StyleUserBox = lipgloss.NewStyle().
			Background(colorSurface).
			Foreground(colorWhite).
			Padding(0, 1)

	StyleAssistantLabel = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	StyleAssistantMsg = lipgloss.NewStyle().
				Foreground(colorWhite)

	StyleSystemMsg = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true)

	StyleThinking = lipgloss.NewStyle().
			Foreground(colorYellow).
			Italic(true)

	// StyleThought is used to render StreamThought (extended thinking) tokens
	// in the streaming display — muted gray italic.
	StyleThought = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280")).
			Italic(true)

	StyleError = lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true)

	// StyleWarning is used to render non-fatal warnings (e.g. MCP setup failures).
	StyleWarning = lipgloss.NewStyle().
			Foreground(colorYellow)

	// ── Tool output ────────────────────────────────────────────────────────

	StyleToolCall = lipgloss.NewStyle().
			Foreground(colorYellow)

	StyleToolDone = lipgloss.NewStyle().
			Foreground(colorGreen)

	StyleToolError = lipgloss.NewStyle().
			Foreground(colorRed)

	// StyleToolCommand is the `$ ` prefix on tool call lines (green).
	StyleToolCommand = lipgloss.NewStyle().
				Foreground(colorGreen).
				Bold(true)

	// StyleToolTiming is the dimmed timing shown on the right of tool-done lines.
	StyleToolTiming = lipgloss.NewStyle().
				Foreground(colorDim)

	// StyleToolTruncate is for "… truncated (N more lines) · ctrl+o to expand".
	StyleToolTruncate = lipgloss.NewStyle().
				Foreground(colorBlue).
				Italic(true)

	// StyleGenerating is "● Generating... N tokens" — green dot + bold text.
	StyleGenerating = lipgloss.NewStyle().
				Foreground(colorGreen).
				Bold(true)

	// StyleInputBox is the bordered input box style.
	StyleInputBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	// StyleFollowUpBox is the yellow/gold border box for queued messages.
	StyleFollowUpBox = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorGold).
				Padding(0, 1)

	// StyleAutoRun is "► Auto-run..." status line (teal/green).
	StyleAutoRun = lipgloss.NewStyle().
			Foreground(colorTeal).
			Bold(true)

	// ── Chrome ─────────────────────────────────────────────────────────────

	StyleDivider = lipgloss.NewStyle().
			Foreground(colorBorder)

	// StatusBar: NO padding — we handle spacing in content so Width() is exact.
	StyleStatusBar = lipgloss.NewStyle().
			Background(colorSurface).
			Foreground(colorMuted)

	StyleStatusAccent = lipgloss.NewStyle().
				Background(colorSurface).
				Foreground(colorAccent).
				Bold(true)

	StyleStatusGreen = lipgloss.NewStyle().
				Background(colorSurface).
				Foreground(colorGreen)

	StyleStatusYellow = lipgloss.NewStyle().
				Background(colorSurface).
				Foreground(colorYellow)

	StyleInputPrompt = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	StyleApprovalPrompt = lipgloss.NewStyle().
				Foreground(colorYellow).
				Bold(true)

	// ── Wizard ─────────────────────────────────────────────────────────────

	StyleWizardItem = lipgloss.NewStyle().
			PaddingLeft(2).
			Foreground(colorMuted)

	StyleWizardItemSelected = lipgloss.NewStyle().
				PaddingLeft(1).
				Foreground(colorAccent).
				Bold(true).
				Border(lipgloss.NormalBorder(), false, false, false, true).
				BorderForeground(colorAccent)

	StyleWizardBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	// ── Misc ───────────────────────────────────────────────────────────────

	StyleCode = lipgloss.NewStyle().
			Background(colorSurface).
			Foreground(colorWhite).
			Padding(0, 1)

	StyleFilePath = lipgloss.NewStyle().
			Foreground(colorBlue).
			Bold(true)

	StyleAccent = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	StyleDim = lipgloss.NewStyle().
			Foreground(colorDim)

	StyleGold = lipgloss.NewStyle().
			Foreground(colorGold)

	StyleYellow = lipgloss.NewStyle().
			Foreground(colorYellow)

	// ── Scrollbar ───────────────────────────────────────────────────────────

	StyleScrollTrack = lipgloss.NewStyle().
			Foreground(colorBorder)

	StyleScrollThumb = lipgloss.NewStyle().
			Foreground(colorMuted)

	// ── Muninn memory indicator ─────────────────────────────────────────────

	StyleMuninn = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	StyleMuninnEmpty = lipgloss.NewStyle().
				Foreground(colorDim)
)

// StyleDelegationBox renders a bordered box in the delegatee's agent color.
// Used to visually wrap a consulted agent's response inline in the chat stream.
func StyleDelegationBox(agentColor string) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(agentColor)).
		Padding(0, 1).
		MarginLeft(2)
}

// StyleAgentLabel renders the agent name+icon in the agent's color.
func StyleAgentLabel(agentColor string) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(agentColor)).
		Bold(true)
}

// WrapCode wraps content in a styled code block.
func WrapCode(content string) string {
	return StyleCode.Render(content)
}

// WrapFilePath renders a file path in accent color.
func WrapFilePath(path string) string {
	return StyleFilePath.Render(path)
}

// clipString trims s to at most maxRunes runes.
func clipString(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}
