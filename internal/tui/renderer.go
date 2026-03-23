package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// dotFrames are the three animation frames for the typing indicator.
var dotFrames = []string{"●  ◦  ◦", "◦  ●  ◦", "◦  ◦  ●"}

// muninnCallLabel returns a human-readable label for a muninn tool call in progress.
func muninnCallLabel(toolName string) string {
	switch {
	case toolName == "muninn_recall" || toolName == "muninn_recall_tree":
		return "recalling from memory"
	case toolName == "muninn_where_left_off":
		return "orienting from memory"
	case toolName == "muninn_remember" || toolName == "muninn_remember_batch" || toolName == "muninn_remember_tree":
		return "storing in memory"
	case toolName == "muninn_read":
		return "reading memory"
	case toolName == "muninn_find_by_entity" || toolName == "muninn_entities" || toolName == "muninn_similar_entities" || toolName == "muninn_entity_clusters":
		return "searching memory"
	case toolName == "muninn_link":
		return "linking memories"
	case toolName == "muninn_forget":
		return "forgetting"
	case toolName == "muninn_evolve" || toolName == "muninn_consolidate":
		return "evolving memory"
	case toolName == "muninn_session" || toolName == "muninn_state":
		return "checking memory state"
	default:
		return "memory operation"
	}
}

// muninnDoneLabel returns a human-readable label for a completed muninn tool call.
func muninnDoneLabel(toolName string) string {
	switch {
	case toolName == "muninn_recall" || toolName == "muninn_recall_tree":
		return "recalled from memory"
	case toolName == "muninn_where_left_off":
		return "oriented from memory"
	case toolName == "muninn_remember" || toolName == "muninn_remember_batch" || toolName == "muninn_remember_tree":
		return "stored in memory"
	case toolName == "muninn_read":
		return "read from memory"
	case toolName == "muninn_find_by_entity" || toolName == "muninn_entities" || toolName == "muninn_similar_entities" || toolName == "muninn_entity_clusters":
		return "searched memory"
	case toolName == "muninn_link":
		return "linked memories"
	case toolName == "muninn_forget":
		return "forgotten"
	case toolName == "muninn_evolve" || toolName == "muninn_consolidate":
		return "memory evolved"
	case toolName == "muninn_session" || toolName == "muninn_state":
		return "memory checked"
	default:
		return "memory updated"
	}
}

// muninnCallIcon returns a directional icon for an in-flight muninn operation.
// ← = retrieving from memory, → = writing to memory, ◎ = neutral operation.
func muninnCallIcon(toolName string) string {
	switch toolName {
	case "muninn_recall", "muninn_recall_tree", "muninn_read", "muninn_where_left_off",
		"muninn_find_by_entity", "muninn_entities", "muninn_similar_entities",
		"muninn_entity_clusters", "muninn_entity", "muninn_entity_state",
		"muninn_entity_timeline", "muninn_traverse", "muninn_provenance":
		return "←"
	case "muninn_remember", "muninn_remember_batch", "muninn_remember_tree",
		"muninn_evolve", "muninn_consolidate", "muninn_forget":
		return "→"
	default:
		return "◎"
	}
}

// isMuninnRecallOp returns true for tool names that retrieve data from memory
// (as opposed to writes or neutral ops). Used to detect "nothing found" state.
func isMuninnRecallOp(toolName string) bool {
	switch toolName {
	case "muninn_recall", "muninn_recall_tree", "muninn_read", "muninn_where_left_off",
		"muninn_find_by_entity", "muninn_entities", "muninn_similar_entities",
		"muninn_entity_clusters", "muninn_entity", "muninn_entity_state",
		"muninn_entity_timeline", "muninn_traverse":
		return true
	}
	return false
}

// isMuninnEmptyResult returns true when a muninn tool result contains no memories.
// Handles common MuninnDB response patterns: empty array, null, zero-count objects.
func isMuninnEmptyResult(output string) bool {
	s := strings.TrimSpace(output)
	if s == "" || s == "null" || s == "[]" || s == "{}" {
		return true
	}
	if strings.Contains(s, `"count": 0`) || strings.Contains(s, `"count":0`) {
		return true
	}
	// Short payloads that are just an empty array wrapper, e.g. {"memories":[]}
	if len(s) < 60 && strings.Contains(s, `[]`) {
		return true
	}
	return false
}

// newGlamourRenderer creates a glamour markdown renderer for the given terminal width.
// We use WithStandardStyle("dark") instead of WithAutoStyle() because AutoStyle sends
// an OSC 11 terminal background query whose response leaks into the textinput widget
// as raw escape sequences (e.g. "]11;rgb:158e/193a/1e75\[1;1R").
func newGlamourRenderer(width int) *glamour.TermRenderer {
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		// Fall back gracefully; renderMarkdown will handle nil.
		return nil
	}
	return r
}

// renderMarkdown renders markdown content through glamour, falling back to plain text.
func (a *App) renderMarkdown(content string) string {
	if a.glamourRenderer == nil {
		return content
	}
	rendered, err := a.glamourRenderer.Render(content)
	if err != nil {
		return content
	}
	// glamour adds a trailing newline; strip it so we control spacing uniformly.
	return strings.TrimRight(rendered, "\n")
}

func welcomeMessage() string {
	logo := "  HUGINN  —  your local AI coding assistant"
	return StyleAccent.Render(logo) + "\n\n" +
		StyleDim.Render("  No agent selected — run /agents new to create one") + "\n" +
		StyleDim.Render("  Type / for commands · ctrl+c to quit") + "\n"
}

// welcomeMessageWithAgent renders the initial welcome screen when an agent is active.
func welcomeMessageWithAgent(agent string) string {
	logo := "  HUGINN  —  your local AI coding assistant"
	agentLine := StyleDim.Render("  Agent: ") + StyleAccent.Render(agent) +
		StyleDim.Render("  ·  ctrl+p to switch  ·  /agents to manage")
	return StyleAccent.Render(logo) + "\n\n" +
		agentLine + "\n" +
		StyleDim.Render("  Type / for commands · ctrl+c to quit") + "\n"
}

// refreshViewport re-renders the full chat history into the viewport content.
// It also rebuilds a.chatLineOffsets so callers can map history indices to
// viewport line numbers for accurate thread jumping and collapse-delta math.
func (a *App) refreshViewport() {
	// Save scroll position before replacing content so we can restore it.
	savedOffset := a.viewport.YOffset
	oldTotal := a.viewport.TotalLineCount()

	// Rebuild per-history-entry line offsets alongside the content string.
	a.chatLineOffsets = make([]int, len(a.chat.history))
	currentLine := 0

	var sb strings.Builder
	for i := range a.chat.history {
		line := a.chat.history[i]

		// Record where this history entry starts in viewport coordinates.
		a.chatLineOffsets[i] = currentLine

		var lineSB strings.Builder
		switch line.role {
		case "user":
			// Cursor-style: user message in a muted background box.
			boxWidth := a.width - 4
			if boxWidth < 20 {
				boxWidth = 20
			}
			msg := StyleUserBox.Width(boxWidth).Render(line.content)
			lineSB.WriteString(msg)

		case "assistant":
			// Cache the full assistant block (header + markdown body) to avoid
			// expensive O(N) glamour re-renders on every streaming token.
			if line.renderedCache != "" && line.renderWidth == a.viewport.Width {
				lineSB.WriteString(line.renderedCache)
			} else {
				var blockSB strings.Builder
				// Render agent identity header when the agent name is known.
				if line.agentName != "" {
					icon := lipgloss.NewStyle().
						Background(lipgloss.Color(line.agentColor)).
						Foreground(lipgloss.Color("#000000")).
						Bold(true).
						Padding(0, 1).
						Render(line.agentIcon)
					nameLabel := StyleAgentLabel(line.agentColor).Render(line.agentName)
					// Fill a separator line to the right of the name.
					sepWidth := a.width - lipgloss.Width(icon) - lipgloss.Width(" "+line.agentName+" ") - 4
					if sepWidth < 2 {
						sepWidth = 2
					}
					sep := StyleDim.Render(" " + strings.Repeat("─", sepWidth))
					blockSB.WriteString(icon + " " + nameLabel + sep)
					blockSB.WriteString("\n")
				}
				// Render through glamour for rich markdown.
				rendered := a.renderMarkdown(line.content)
				blockSB.WriteString(rendered)
				// Store in cache for subsequent refreshes at the same width.
				a.chat.history[i].renderedCache = blockSB.String()
				a.chat.history[i].renderWidth = a.viewport.Width
				lineSB.WriteString(blockSB.String())
			}

		case "system":
			lineSB.WriteString(StyleThinking.Render("⟳ " + line.content))

		case "error":
			lineSB.WriteString(StyleError.Render("✗ " + line.content))

		case "tool-call":
			if strings.HasPrefix(line.toolName, "muninn_") {
				icon := muninnCallIcon(line.toolName)
				label := muninnCallLabel(line.toolName)
				lineSB.WriteString(StyleMuninn.Render("  " + icon + "  " + label + "…"))
			} else {
				// Cursor-style: "$ command args"
				lineSB.WriteString(StyleToolCommand.Render("$ "))
				lineSB.WriteString(StyleDim.Render(line.content))
			}

		case "tool-done":
			if strings.HasPrefix(line.toolName, "muninn_") {
				if isMuninnRecallOp(line.toolName) && isMuninnEmptyResult(line.fullOutput) {
					lineSB.WriteString(StyleMuninnEmpty.Render("  ○  nothing in memory yet"))
				} else {
					icon := muninnCallIcon(line.toolName)
					label := muninnDoneLabel(line.toolName)
					lineSB.WriteString(StyleMuninn.Render("  ◉  " + icon + "  " + label))
				}
				if line.duration != "" {
					lineSB.WriteString("  " + StyleToolTiming.Render(line.duration))
				}
			} else if line.expanded && line.fullOutput != "" {
				// Show full output.
				lineSB.WriteString(StyleToolDone.Render("  " + strings.ReplaceAll(line.fullOutput, "\n", "\n  ")))
				if line.duration != "" {
					lineSB.WriteString("  " + StyleToolTiming.Render(line.duration))
				}
				lineSB.WriteString("\n  " + StyleToolTruncate.Render("ctrl+o to collapse"))
			} else {
				lineSB.WriteString(StyleToolDone.Render("  " + line.content))
				if line.duration != "" {
					lineSB.WriteString("  " + StyleToolTiming.Render(line.duration))
				}
				if line.truncated > 0 {
					lineSB.WriteString("\n  " + StyleToolTruncate.Render(
						fmt.Sprintf("… truncated (%d more lines) · ctrl+o to expand", line.truncated)))
				}
			}

		case "tool-error":
			if strings.HasPrefix(line.toolName, "muninn_") {
				// line.content is "✗ toolName: actual error" — strip the prefix to show the reason
				errDetail := line.content
				if idx := strings.Index(errDetail, ": "); idx >= 0 {
					errDetail = errDetail[idx+2:]
				}
				if errDetail == "" {
					errDetail = "unavailable"
				}
				lineSB.WriteString(StyleMuninnEmpty.Render("  ◎  memory: " + errDetail))
			} else {
				lineSB.WriteString(StyleToolError.Render(line.content))
			}

		case "delegation-start":
			lineSB.WriteString(StyleDim.Render(line.content))

		case "delegation-done":
			color := a.delegationAgentColor
			if color == "" {
				color = string(colorAccent)
			}
			lineSB.WriteString(StyleDelegationBox(color).Render(line.content))

		case "thread-header":
			lineSB.WriteString(renderThreadLine(line, a.width))

		case "swarm-bar":
			lineSB.WriteString(renderSwarmBar(line, a.width))
		}

		// If this is a standalone artifact line, append its render.
		if line.isArtifactLine && line.role == "artifact" {
			lineSB.WriteString(renderArtifactLine(line))
		}

		block := lineSB.String()
		sb.WriteString(block)
		sb.WriteString("\n\n")
		currentLine += strings.Count(block, "\n") + 2
	}

	// chatLineOffsets is now current; clear the dirty flag.
	a.chatLineOffsetsDirty = false

	// Display thinking content (already styled) followed by streaming content.
	if a.chat.thoughtStreaming.Len() > 0 {
		sb.WriteString(a.chat.thoughtStreaming.String())
	}
	if a.chat.streaming.Len() > 0 {
		// Render in-progress streaming content through glamour.
		rendered := a.renderMarkdown(a.chat.streaming.String())
		sb.WriteString(rendered)
		sb.WriteString(StyleDim.Render("▌"))
	} else if a.state == stateStreaming {
		// No tokens yet — show "Agent is typing" indicator with animated dots.
		// Resolve agent name and color for a personalized typing bubble.
		agentLabel := "assistant"
		agentColor := string(colorAccent)
		if a.primaryAgent != "" {
			agentLabel = a.primaryAgent
			if a.agentReg != nil {
				if ag, ok := a.agentReg.ByName(a.primaryAgent); ok && ag.Color != "" {
					agentColor = ag.Color
				}
			}
		}
		label := StyleAgentLabel(agentColor).Render(agentLabel)
		dots := StyleGenerating.Render("Generating " + dotFrames[a.dotPhase])
		sb.WriteString(label + "  " + dots)
	}

	a.viewport.SetContent(sb.String())
	if !a.scrollMode {
		// Follow mode (default): always show the latest content.
		a.viewport.GotoBottom()
	} else {
		// Scroll mode: anchor the view from the top so the user's reading position
		// stays stable as new content appends at the bottom.
		a.viewport.SetYOffset(savedOffset)
		// Track how many new lines appeared while the user was scrolled up.
		newLines := a.viewport.TotalLineCount() - oldTotal
		if newLines > 0 {
			a.newLinesWhileScrolled += newLines
		}
	}
}

// renderScrollbar returns a 1-char-wide string column matching the viewport
// height that shows a proportional scrollbar (│ track, █ thumb). When all
// content fits on screen the track is shown without a thumb.
func (a *App) renderScrollbar() string {
	h := a.viewport.Height
	if h <= 0 {
		return ""
	}
	total := a.viewport.TotalLineCount()
	var out strings.Builder
	if total <= h {
		// All content visible — dim track, no thumb.
		for i := 0; i < h; i++ {
			if i > 0 {
				out.WriteString("\n")
			}
			out.WriteString(StyleScrollTrack.Render("│"))
		}
		return out.String()
	}
	// Calculate proportional thumb size and position.
	thumbSize := h * h / total
	if thumbSize < 1 {
		thumbSize = 1
	}
	if thumbSize > h {
		thumbSize = h
	}
	thumbStart := a.viewport.YOffset * (h - thumbSize) / (total - h)
	if thumbStart < 0 {
		thumbStart = 0
	}
	if thumbStart > h-thumbSize {
		thumbStart = h - thumbSize
	}
	for i := 0; i < h; i++ {
		if i > 0 {
			out.WriteString("\n")
		}
		if i >= thumbStart && i < thumbStart+thumbSize {
			out.WriteString(StyleScrollThumb.Render("█"))
		} else {
			out.WriteString(StyleScrollTrack.Render("│"))
		}
	}
	return out.String()
}

// renderChips renders the attachment chip row above the input box.
// Shows file attachment chips and a shell-context chip when present.
func (a *App) renderChips() string {
	var sb strings.Builder
	sb.WriteString("  ") // left padding

	// Shell context chip.
	if a.shellContext != "" {
		lines := strings.Count(a.shellContext, "\n")
		shellChip := StyleYellow.Render("⚡") + " " + StyleDim.Render(fmt.Sprintf("shell output (%d lines) will be sent", lines))
		sb.WriteString(shellChip)
		if len(a.attachments) > 0 {
			sb.WriteString("   ")
		}
	}

	// File attachment chips.
	for i, p := range a.attachments {
		name := filepath.Base(p)
		chip := StyleAccent.Render("@") + name + " " + StyleDim.Render("×")
		if a.chipFocused && i == a.chipCursor {
			chip = StyleGold.Render("▸") + StyleAccent.Render("@") + name + " " + StyleGold.Render("×") + StyleGold.Render("◂")
		}
		sb.WriteString(chip)
		if i < len(a.attachments)-1 {
			sb.WriteString("   ")
		}
	}

	if a.chipFocused {
		sb.WriteString(StyleDim.Render("  ←→ move · Backspace remove · Esc done"))
	} else {
		sb.WriteString(StyleDim.Render("  (Backspace to manage)"))
	}
	return sb.String()
}

// renderInputBox renders the bordered input box with optional streaming hints.
func (a *App) renderInputBox() string {
	inputContent := a.input.View()
	cw := a.chatWidth()

	if a.state == stateStreaming {
		// Show animated dots + token count on the left and "ctrl+c to stop" on the right.
		generatingText := StyleGenerating.Render(fmt.Sprintf("Generating %s  %d tokens", dotFrames[a.dotPhase], a.chat.tokenCount))
		stopHint := StyleDim.Render("ctrl+c to stop")

		// Build the top row with generating indicator.
		innerWidth := cw - 6 // account for box borders + padding
		if innerWidth < 10 {
			innerWidth = 10
		}
		gap := innerWidth - lipgloss.Width(generatingText) - lipgloss.Width(stopHint)
		if gap < 1 {
			gap = 1
		}
		statusRow := generatingText + strings.Repeat(" ", gap) + stopHint

		boxContent := statusRow + "\n" + inputContent
		return StyleInputBox.Width(cw - 2).Render(boxContent)
	}

	return StyleInputBox.Width(cw - 2).Render(inputContent)
}

// renderFollowUpBox renders the yellow-bordered follow-ups queue box.
func (a *App) renderFollowUpBox() string {
	cw := a.chatWidth()
	innerWidth := cw - 6
	if innerWidth < 20 {
		innerWidth = 20
	}

	// Truncate queued message if too long.
	displayMsg := a.queuedMsg
	if len([]rune(displayMsg)) > innerWidth-4 {
		displayMsg = string([]rune(displayMsg)[:innerWidth-7]) + "…"
	}

	msgLine := StyleDim.Render("○ ") + StyleUserMsg.Render(displayMsg)
	hintLine := StyleDim.Render("enter send now · ↑ edit · esc cancel")

	content := msgLine + "\n" + hintLine

	// Render box with "follow-ups" title embedded in the top border.
	box := StyleFollowUpBox.Width(cw - 2).Render(content)

	// Inject the title into the top border by string replacement.
	// The rounded border top looks like: "╭────...────╮"
	// We replace the leading dashes after "╭" with " follow-ups ".
	title := " follow-ups "
	lines := strings.Split(box, "\n")
	if len(lines) > 0 {
		topBorder := lines[0]
		// Find insertion point after the opening corner character.
		if len([]rune(topBorder)) > 2 {
			runesBorder := []rune(topBorder)
			// runesBorder[0] is '╭', replace chars 1..len(title) with title runes.
			titleRunes := []rune(title)
			if len(runesBorder) > len(titleRunes)+1 {
				for i, r := range titleRunes {
					runesBorder[1+i] = r
				}
				lines[0] = string(runesBorder)
			}
		}
		box = strings.Join(lines, "\n")
	}

	return box
}

// renderFooter builds the two-row bottom status bar.
func (a *App) renderFooter() string {
	// Row 1: Auto-run status
	var autoRunText string
	if a.autoRun {
		autoRunText = StyleAutoRun.Render("► Auto-run all commands") +
			StyleDim.Render(" (shift+tab to turn off)")
	} else {
		autoRunText = StyleDim.Render("○ Auto-run off") +
			StyleDim.Render(" (shift+tab to turn on)")
	}
	row1 := StyleStatusBar.Width(a.width).Render(autoRunText)

	// Row 2: Left = "huginn vX.X.X" or streaming model, Right = model info + shortcuts
	left := fmt.Sprintf(" huginn v%s", a.version)
	if a.priceTracker != nil {
		if costText := a.priceTracker.StatusBarText(); costText != "" {
			left += "  " + costText
		}
	}
	if hv := a.HeaderView(); hv != "" {
		left += "  " + hv
	}

	var right string
	switch a.state {
	case stateStreaming:
		modelName := clipString(a.activeModel, 24)
		if a.agentTurn > 0 {
			right = fmt.Sprintf(" %s · turn %d  / commands · # files · ! shell ", modelName, a.agentTurn)
		} else {
			right = fmt.Sprintf(" %s  / commands · # files · ! shell ", modelName)
		}
	case statePermAwait:
		right = " ⚑ permission required  [a]llow · [A]lways · [d]eny "
	case stateWriteAwait:
		right = " ✎ file write pending  [y]es · [n]o "
	default:
		var modelName string
		if a.cfg != nil && a.cfg.DefaultModel != "" {
			modelName = a.cfg.DefaultModel
		}
		if a.primaryAgent != "" && a.agentReg != nil {
			if ag, ok := a.agentReg.ByName(a.primaryAgent); ok {
				if m := ag.GetModelID(); m != "" {
					modelName = m
				}
			}
		}
		planner := clipString(modelName, 20)
		if !a.useAgentLoop {
			right = fmt.Sprintf(" %s · chat only  / commands · # files · ! shell ", planner)
		} else {
			right = fmt.Sprintf(" %s  / commands · # files · ! shell ", planner)
		}
	}

	// In scroll mode, replace the right side with a position indicator so the
	// user knows they are reading history and how to return to live follow mode.
	if a.scrollMode {
		total := a.viewport.TotalLineCount()
		bottom := a.viewport.YOffset + a.viewport.VisibleLineCount()
		if bottom > total {
			bottom = total
		}
		if a.newLinesWhileScrolled > 0 {
			right = fmt.Sprintf(" ↓ %d new · L%d/%d · PgDn to follow ", a.newLinesWhileScrolled, bottom, total)
		} else {
			right = fmt.Sprintf(" L%d/%d · PgDn to follow ", bottom, total)
		}
	}

	// Fill the gap between left and right.
	gap := a.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		available := a.width - lipgloss.Width(left) - 1
		if available > 0 {
			right = clipString(right, available)
		} else {
			right = ""
		}
		gap = a.width - lipgloss.Width(left) - lipgloss.Width(right)
		if gap < 0 {
			gap = 0
		}
	}
	content2 := left + strings.Repeat(" ", gap) + right
	row2 := StyleStatusBar.Width(a.width).Render(content2)

	return lipgloss.JoinVertical(lipgloss.Left, row1, row2)
}

// renderThreadLine renders a collapsible/expandable thread summary line.
// When collapsed it shows a single summary pill; when expanded it shows a
// bordered box with the full thread content.
func renderThreadLine(line chatLine, width int) string {
	color := line.agentColor
	if color == "" {
		color = agentColorFromName(line.threadAgentName)
	}
	icon := agentIconFromName(line.threadAgentName)

	if line.threadCollapsed {
		// Collapsed: single pill line.
		// " T  Tom · auth refactor complete · 14s · [↵ expand]"
		iconBox := lipgloss.NewStyle().
			Background(lipgloss.Color(color)).
			Foreground(lipgloss.Color("#000000")).
			Bold(true).
			Padding(0, 1).
			Render(icon)
		namePart := StyleAgentLabel(color).Render(line.threadAgentName)
		elapsed := ""
		if line.threadElapsed != "" {
			elapsed = " · " + StyleDim.Render(line.threadElapsed)
		}
		hint := StyleDim.Render(" · [↵ expand]")
		summary := line.content
		if summary == "" {
			summary = "task complete"
		}
		return iconBox + " " + namePart + " · " + StyleDim.Render(summary) + elapsed + hint
	}

	// Expanded: bordered box.
	// ╭─ Tom ·  bash, read_file  · 14s ─────────╮
	// │  ...content...                            │
	// │  📄 auth-refactor.diff  14 files  +203/-87│
	// │     [a] accept  [r] reject  [ctrl+o] view │
	// │  [↵ collapse]                             │
	// ╰───────────────────────────────────────────╯
	var inner strings.Builder
	if line.content != "" {
		inner.WriteString(line.content)
		inner.WriteString("\n")
	}
	if line.threadToolsUsed != "" {
		inner.WriteString(StyleDim.Render("tools: " + line.threadToolsUsed))
		inner.WriteString("\n")
	}
	// Render inline artifact if present.
	if line.isArtifactLine && line.artifactTitle != "" {
		inner.WriteString(renderArtifactLine(line))
		inner.WriteString("\n")
	}
	inner.WriteString(StyleDim.Render("[↵ collapse]"))

	titleParts := line.threadAgentName
	if line.threadToolsUsed != "" {
		titleParts += " · " + line.threadToolsUsed
	}
	if line.threadElapsed != "" {
		titleParts += " · " + line.threadElapsed
	}

	boxWidth := width - 4
	if boxWidth < 20 {
		boxWidth = 20
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(color)).
		Padding(0, 1).
		Width(boxWidth).
		Render(inner.String())

	// Inject the title into the top border after the opening corner.
	title := " " + titleParts + " "
	lines := strings.Split(box, "\n")
	if len(lines) > 0 {
		topBorder := lines[0]
		titleRunes := []rune(title)
		runesBorder := []rune(topBorder)
		if len(runesBorder) > len(titleRunes)+1 {
			for i, r := range titleRunes {
				if 1+i < len(runesBorder) {
					runesBorder[1+i] = r
				}
			}
			lines[0] = string(runesBorder)
		}
		box = strings.Join(lines, "\n")
	}
	return box
}

// artifactKindIcon returns the emoji icon for a given artifact kind.
func artifactKindIcon(kind string) string {
	switch kind {
	case "code_patch":
		return "📄"
	case "document":
		return "📝"
	case "timeline":
		return "📊"
	case "structured_data":
		return "📊"
	case "file_bundle":
		return "🗂"
	}
	return "📄"
}

// renderArtifactLine renders an artifact summary inside a thread box.
func renderArtifactLine(line chatLine) string {
	icon := artifactKindIcon(line.artifactKind)
	title := line.artifactTitle
	if title == "" {
		title = line.artifactID
	}
	stats := ""
	if line.artifactStats != "" {
		stats = "  " + StyleDim.Render(line.artifactStats)
	}
	statusBadge := ""
	switch line.artifactStatus {
	case "accepted":
		statusBadge = "  " + StyleToolDone.Render("✓ accepted")
	case "rejected":
		statusBadge = "  " + StyleError.Render("✗ rejected")
	}
	row1 := icon + " " + StyleAccent.Render(title) + stats + statusBadge
	row2 := StyleDim.Render("     [a] accept  [r] reject  [ctrl+o] view full")
	return row1 + "\n" + row2
}

// renderArtifactOverlay renders the full-screen artifact view overlay.
func (a *App) renderArtifactOverlay() string {
	w := a.width
	if w < 20 {
		w = 20
	}

	// Title bar.
	icon := artifactKindIcon(a.artifactOverlay.kind)
	titleBar := StyleAccent.Render(icon+" "+a.artifactOverlay.title) +
		StyleDim.Render("  [esc/q] close  [↑/↓] scroll")
	titleLine := StyleStatusBar.Width(w).Render(titleBar)

	// Content viewport.
	content := a.artifactOverlay.content
	vp := a.artifactOverlay.viewport
	if vp.Width == 0 {
		vp = viewport.New(w, a.height-3)
		// Render content based on kind.
		switch a.artifactOverlay.kind {
		case "code_patch":
			content = renderDiffContent(content)
		case "document":
			content = a.renderMarkdown(content)
		}
		vp.SetContent(content)
		a.artifactOverlay.viewport = vp
	}
	footer := StyleDim.Width(w).Render("  [esc/q] close  [↑/↓] scroll  [k/j] line up/down")

	return lipgloss.JoinVertical(lipgloss.Left, titleLine, vp.View(), footer)
}

// renderDiffContent colorizes unified diff content: + lines green, - lines red.
func renderDiffContent(content string) string {
	addStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7ee787"))
	delStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#f85149"))
	var sb strings.Builder
	for _, line := range strings.Split(content, "\n") {
		switch {
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			sb.WriteString(addStyle.Render(line))
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
			sb.WriteString(delStyle.Render(line))
		default:
			sb.WriteString(line)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// renderThreadOverlay renders the full-screen thread detail overlay.
func (a *App) renderThreadOverlay() string {
	w := a.width
	if w < 20 {
		w = 20
	}

	// Title bar.
	agentChainStr := strings.Join(a.threadOverlay.agentChain, " → ")
	titleText := "Thread: " + a.threadOverlay.title
	if agentChainStr != "" {
		titleText += " (" + agentChainStr + ")"
	}
	titleLine := StyleAccent.Width(w).Render(titleText)

	// Build content from thread lines.
	var contentSB strings.Builder
	for _, line := range a.threadOverlay.lines {
		switch line.role {
		case "assistant":
			if line.agentName != "" {
				contentSB.WriteString(StyleAgentLabel(line.agentColor).Render(line.agentName) + "\n")
			}
			contentSB.WriteString(line.content + "\n")
		case "tool-call":
			contentSB.WriteString(StyleToolCommand.Render("$ ") + StyleDim.Render(line.content) + "\n")
		case "tool-done":
			contentSB.WriteString(StyleToolDone.Render("  "+line.content) + "\n")
		case "delegation-start":
			// Show delegation divider.
			handoffAgent := ""
			if idx := strings.LastIndex(line.content, "→ "); idx >= 0 {
				handoffAgent = strings.TrimSpace(line.content[idx+2:])
				if ci := strings.Index(handoffAgent, ":"); ci >= 0 {
					handoffAgent = strings.TrimSpace(handoffAgent[:ci])
				}
			}
			contentSB.WriteString(StyleDim.Render("── handed off to "+handoffAgent+" ──") + "\n")
		case "system":
			contentSB.WriteString(StyleThinking.Render("⟳ "+line.content) + "\n")
		default:
			if line.isArtifactLine {
				contentSB.WriteString(renderArtifactLine(line) + "\n")
			}
		}
		contentSB.WriteByte('\n')
	}

	vp := a.threadOverlay.viewport
	if vp.Width == 0 {
		vp = viewport.New(w-4, a.height-4)
		a.threadOverlay.viewport = vp
	}
	a.threadOverlay.viewport.SetContent(contentSB.String())

	boxContent := a.threadOverlay.viewport.View()

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorAccent)).
		Width(w-2).
		Padding(0, 1).
		Render(boxContent)

	footer := StyleDim.Render("  [esc] close  [↑/↓] scroll  [a] accept  [ctrl+o] view diff")

	return lipgloss.JoinVertical(lipgloss.Left, titleLine, box, footer)
}

// renderObservationDeck renders the full-screen observation deck overlay.
func (a *App) renderObservationDeck() string {
	w := a.width
	if w < 20 {
		w = 20
	}

	titleText := "Observation Deck"
	if a.observationDeck.title != "" {
		titleText += ": " + a.observationDeck.title
	}

	boxContent := strings.Join(a.observationDeck.lines, "\n")
	if boxContent == "" {
		boxContent = StyleDim.Render("  (no steps recorded)")
	}

	vp := a.observationDeck.viewport
	if vp.Width == 0 {
		vp = viewport.New(w-4, a.height-4)
		vp.SetContent(boxContent)
		a.observationDeck.viewport = vp
	} else {
		a.observationDeck.viewport.SetContent(boxContent)
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(colorAccent)).
		Width(w-2).
		Padding(0, 1).
		Render(a.observationDeck.viewport.View())

	titleLine := StyleAccent.Width(w).Render(titleText)
	footer := StyleDim.Render("  [esc] close  [↑/↓] scroll")

	return lipgloss.JoinVertical(lipgloss.Left, titleLine, box, footer)
}

// renderSwarmBar renders an inline swarm status bar chat line.
func renderSwarmBar(line chatLine, width int) string {
	var sb strings.Builder
	blueBar := lipgloss.NewStyle().Foreground(lipgloss.Color("#58A6FF"))
	dimBar := StyleDim

	// Header row.
	if line.swarmDone {
		// Completed: show summary.
		agentCount := len(line.swarmAgents)
		header := fmt.Sprintf("⚡ Swarm complete: %d agents", agentCount)
		dividerWidth := width - lipgloss.Width(header) - 4
		if dividerWidth < 2 {
			dividerWidth = 2
		}
		sb.WriteString(StyleYellow.Render(header) + "  " + dimBar.Render(strings.Repeat("─", dividerWidth)) + "\n")
		for _, ag := range line.swarmAgents {
			icon := agentIconFromName(ag.name)
			row := fmt.Sprintf("   %s %-8s  %s", icon, ag.name, ag.output)
			sb.WriteString(StyleAgentLabel(agentColorFromName(ag.name)).Render(row) + "\n")
		}
		sb.WriteString(dimBar.Render("   [↵] view threads  [a] accept all"))
	} else {
		// In-progress: show per-agent progress bars.
		header := "⚡ Swarm: " + line.swarmTitle
		dividerWidth := width - lipgloss.Width(header) - 4
		if dividerWidth < 2 {
			dividerWidth = 2
		}
		sb.WriteString(StyleYellow.Render(header) + "  " + dimBar.Render(strings.Repeat("─", dividerWidth)) + "\n")
		for _, ag := range line.swarmAgents {
			icon := agentIconFromName(ag.name)
			bar := renderProgressBar(ag.pct, 12, blueBar)
			pctOrStatus := fmt.Sprintf("%3d%%", ag.pct)
			if ag.status == "done" {
				pctOrStatus = "done ✓"
			} else if ag.status == "waiting" {
				pctOrStatus = "waiting"
			}
			tool := ag.tool
			if tool == "" {
				tool = ag.status
			}
			elapsed := ag.elapsed
			if elapsed == "" {
				elapsed = "—"
			}
			row := fmt.Sprintf("   %s %-8s  %s  %-6s  %-18s  %s",
				icon, ag.name, bar, pctOrStatus, tool, elapsed)
			sb.WriteString(StyleAgentLabel(agentColorFromName(ag.name)).Render(row) + "\n")
		}
		sb.WriteString(dimBar.Render("   [ctrl+s] detail  [ctrl+c] cancel"))
	}
	return sb.String()
}

// renderProgressBar renders a progress bar of the given width using █ for filled and ░ for empty.
func renderProgressBar(pct, width int, filledStyle lipgloss.Style) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := width * pct / 100
	empty := width - filled
	bar := filledStyle.Render(strings.Repeat("█", filled)) + StyleDim.Render(strings.Repeat("░", empty))
	return bar
}

