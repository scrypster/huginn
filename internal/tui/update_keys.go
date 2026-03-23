package tui

// update_keys.go — keyboard input handler for the chat screen.
// Extracted from the monolithic Update() in app.go.

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/scrypster/huginn/internal/permissions"
)

// handleKeyMsg dispatches keyboard input for the main chat interface.
// Called from Update() when a tea.KeyMsg is received.
func (a *App) handleKeyMsg(msg tea.KeyMsg, cmds []tea.Cmd) (tea.Model, tea.Cmd) {
	// Artifact view overlay intercepts all keys.
	if a.state == stateArtifactView {
		switch msg.String() {
		case "esc", "q":
			a.state = stateChat
			a.artifactOverlay.viewport.Width = 0 // reset so it rebuilds on next open
		case "up", "k":
			a.artifactOverlay.viewport.LineUp(1)
		case "down", "j":
			a.artifactOverlay.viewport.LineDown(1)
		}
		return a, nil
	}

	// Thread overlay intercepts all keys.
	if a.state == stateThreadOverlay {
		switch msg.String() {
		case "esc":
			a.state = stateChat
			a.threadOverlay.viewport.Width = 0
		case "up", "k":
			a.threadOverlay.viewport.LineUp(1)
		case "down", "j":
			a.threadOverlay.viewport.LineDown(1)
		case "a":
			a.acceptArtifactAtCursor()
		case "ctrl+o":
			a.openArtifactOverlay()
		}
		return a, nil
	}

	// Observation deck overlay intercepts all keys.
	if a.state == stateObservationDeck {
		switch msg.String() {
		case "esc", "q":
			a.state = stateChat
			a.observationDeck.viewport.Width = 0
		case "up", "k":
			a.observationDeck.viewport.LineUp(1)
		case "down", "j":
			a.observationDeck.viewport.LineDown(1)
		}
		return a, nil
	}

	// Session picker intercepts all keys when visible.
	if a.state == stateSessionPicker {
		var spCmd tea.Cmd
		a.sessionPicker, spCmd = a.sessionPicker.Update(msg)
		if spCmd != nil {
			cmds = append(cmds, spCmd)
		}
		if !a.sessionPicker.visible {
			a.recalcViewportHeight()
		}
		return a, tea.Batch(cmds...)
	}

	// Agent creation wizard intercepts all keys when active.
	if a.state == stateAgentWizard {
		var awCmd tea.Cmd
		var awModel tea.Model
		awModel, awCmd = a.agentWizard.Update(msg)
		a.agentWizard = awModel.(agentWizardModel)
		if awCmd != nil {
			cmds = append(cmds, awCmd)
		}
		return a, tea.Batch(cmds...)
	}

	// File picker intercepts all keys when visible.
	if a.state == stateFilePicker {
		var fpCmd tea.Cmd
		a.filePicker, fpCmd = a.filePicker.Update(msg)
		if fpCmd != nil {
			cmds = append(cmds, fpCmd)
		}
		if !a.filePicker.Visible() {
			a.recalcViewportHeight()
		}
		return a, tea.Batch(cmds...)
	}

	// Wizard intercepts all keys when visible.
	if a.state == stateWizard {
		var wizCmd tea.Cmd
		a.wizard, wizCmd = a.wizard.Update(msg)
		cmds = append(cmds, wizCmd)
		if !a.wizard.Visible() {
			return a, tea.Batch(cmds...)
		}
		if msg.String() != "tab" {
			var inputCmd tea.Cmd
			a.input, inputCmd = a.input.Update(msg)
			cmds = append(cmds, inputCmd)
			val := a.input.Value()
			if !strings.HasPrefix(val, "/") {
				a.wizard.Hide()
				a.state = stateChat
				a.recalcViewportHeight()
			} else {
				a.wizard.UpdateFilter(strings.TrimPrefix(val, "/"))
			}
		}
		return a, tea.Batch(cmds...)
	}

	// Sidebar has priority for its navigation keys when focused.
	// Must be checked before the global switch so "enter"/"esc"/"up"/"down"
	// are not swallowed by the generic handlers below.
	if a.state == stateChat && a.sidebar.focused {
		switch msg.String() {
		case "up", "down", "k", "j", "enter", " ", "esc", "tab", "ctrl+b":
			var sbCmd tea.Cmd
			a.sidebar, sbCmd = a.sidebar.Update(msg)
			cmds = append(cmds, sbCmd)
			return a, tea.Batch(cmds...)
		}
	}

	switch msg.String() {
	case "?":
		if a.state == stateChat {
			if !a.input.Focused() {
				// Unfocused → show full keyboard shortcuts help.
				a.addLine("system", "Keyboard shortcuts:\n"+helpText())
				a.refreshViewport()
				return a, nil
			} else if a.input.Value() == "" {
				// Focused but empty — add a brief tip; let '?' reach the input too.
				a.addLine("system", "Tip: press '?' when the input is not active to view shortcuts.")
				a.refreshViewport()
			}
			// Focused with content: '?' is a literal character, fall through.
		}

	case "ctrl+c":
		if a.chat.cancelStream != nil {
			a.chat.cancelStream()
			a.chat.cancelStream = nil
			a.state = stateChat
			a.agentTurn = 0
			a.queuedMsg = ""
			a.chat.tokenCount = 0
			a.ctrlCPending = false
			if a.writePending != nil {
				select {
				case a.writePending.RespCh <- false:
				default:
				}
				a.writePending = nil
			}
			if a.chat.eventCh != nil {
				ch := a.chat.eventCh
				a.chat.eventCh = nil
				go drainWithTimeout(ch)
			} else if a.chat.runner != nil {
				ch := a.chat.runner.TokenCh()
				a.chat.runner = nil
				go drainWithTimeout(ch)
			}
			a.recalcViewportHeight()
			a.addLine("system", "Cancelled.")
			a.refreshViewport()
			return a, nil
		}
		if inputVal := a.input.Value(); inputVal != "" {
			a.input.SetValue("")
			a.ctrlCPending = false
			return a, nil
		}
		if a.ctrlCPending {
			return a, tea.Quit
		}
		a.ctrlCPending = true
		a.addLine("system", "Press ctrl+c again to exit.")
		a.refreshViewport()
		return a, ctrlCResetCmd()

	case "ctrl+o":
		// If cursor is on an artifact line, open full-screen artifact overlay.
		if a.state == stateChat {
			for i := len(a.chat.history) - 1; i >= 0; i-- {
				if a.chat.history[i].isArtifactLine {
					a.openArtifactOverlay()
					return a, nil
				}
			}
		}
		// Otherwise toggle tool-done expand/collapse.
		for i := len(a.chat.history) - 1; i >= 0; i-- {
			if a.chat.history[i].role == "tool-done" && a.chat.history[i].truncated > 0 {
				a.chat.history[i].renderedCache = "" // invalidate cached render
				a.chat.history[i].expanded = !a.chat.history[i].expanded
				a.enterFollowMode() // scroll to show the expanded/collapsed result
				a.refreshViewport()
				break
			}
		}
		return a, nil

	case "ctrl+t":
		// Open full-screen thread overlay for the most recent expanded thread.
		if a.state == stateChat {
			a.openThreadOverlay()
		}
		return a, nil

	case "ctrl+e":
		// Open observation deck for the current thread/context.
		if a.state == stateChat {
			a.openObservationDeck()
		}
		return a, nil

	case "ctrl+s":
		// Open swarm detail view when cursor is on a swarm bar, otherwise existing swarm overlay.
		if a.state == stateChat {
			for i := len(a.chat.history) - 1; i >= 0; i-- {
				if a.chat.history[i].role == "swarm-bar" {
					if a.swarmView != nil {
						a.state = stateSwarm
					}
					return a, nil
				}
			}
		}

	case "]":
		// Jump to next thread header — only when input is empty.
		if a.state == stateChat && a.input.Value() == "" {
			a.jumpToNextThread(1)
			return a, nil
		}

	case "[":
		// Jump to previous thread header — only when input is empty.
		if a.state == stateChat && a.input.Value() == "" {
			a.jumpToNextThread(-1)
			return a, nil
		}

	case "j":
		if (a.state == stateChat || a.state == stateStreaming) && a.input.Value() == "" {
			a.scrollDown(1)
			return a, nil
		}

	case "k":
		if (a.state == stateChat || a.state == stateStreaming) && a.input.Value() == "" {
			a.scrollUp(1)
			return a, nil
		}

	case "G":
		if (a.state == stateChat || a.state == stateStreaming) && a.input.Value() == "" {
			a.enterFollowMode()
			return a, nil
		}

	case "home":
		if (a.state == stateChat || a.state == stateStreaming) && a.input.Value() == "" {
			a.scrollMode = true
			a.viewport.SetYOffset(0)
			return a, nil
		}

	case "ctrl+b":
		if a.sidebar.IsVisible() {
			a.sidebar.focused = !a.sidebar.focused
			return a, nil
		}

	case "ctrl+a":
		if a.state == stateChat {
			a.agentWizard = newAgentWizardWithMemory(a.muninnEndpoint, a.muninnConnected)
			a.state = stateAgentWizard
			a.recalcViewportHeight()
			return a, nil
		}

	case "ctrl+p":
		if a.agentReg != nil {
			names := a.agentReg.Names()
			sort.Strings(names)
			if len(names) > 0 {
				current := strings.ToLower(a.primaryAgent)
				nextIdx := 0
				for i, n := range names {
					if n == current {
						nextIdx = (i + 1) % len(names)
						break
					}
				}
				chosen := names[nextIdx]
				if ag, ok := a.agentReg.ByName(chosen); ok {
					a.primaryAgent = ag.Name
				} else {
					a.primaryAgent = chosen
				}
				a.agentReg.SetDefault(a.primaryAgent)
				a.input.Placeholder = "Message " + a.primaryAgent + "..."
				a.addLine("system", fmt.Sprintf("Primary agent → %s", a.primaryAgent))
				a.refreshViewport()
			}
		}
		return a, nil

	case "shift+tab":
		a.autoRun = !a.autoRun
		if a.autoRunAtom != nil {
			a.autoRunAtom.Store(a.autoRun)
		}
		status := "on"
		if !a.autoRun {
			status = "off"
		}
		a.addLine("system", fmt.Sprintf("Auto-run %s.", status))
		a.refreshViewport()
		return a, nil

	case "backspace":
		if a.chipFocused {
			if len(a.attachments) > 0 {
				a.attachments = append(a.attachments[:a.chipCursor], a.attachments[a.chipCursor+1:]...)
				if a.chipCursor >= len(a.attachments) {
					a.chipCursor = max(0, len(a.attachments)-1)
				}
				if len(a.attachments) == 0 {
					a.chipFocused = false
				}
			} else {
				a.chipFocused = false
			}
			a.recalcViewportHeight()
			a.refreshViewport()
			return a, nil
		}
		if a.state == stateChat && a.input.Value() == "" && len(a.attachments) > 0 {
			a.chipFocused = true
			a.chipCursor = len(a.attachments) - 1
			return a, nil
		}

	case "left":
		if a.chipFocused {
			if a.chipCursor > 0 {
				a.chipCursor--
			}
			return a, nil
		}

	case "right":
		if a.chipFocused {
			if a.chipCursor < len(a.attachments)-1 {
				a.chipCursor++
			}
			return a, nil
		}

	case "pgup":
		if a.state == stateChat || a.state == stateStreaming {
			a.scrollUp(a.viewport.VisibleLineCount())
			return a, nil
		}

	case "pgdown":
		if a.state == stateChat || a.state == stateStreaming {
			a.scrollDown(a.viewport.VisibleLineCount())
			return a, nil
		}

	case "up":
		if a.queuedMsg != "" {
			a.input.SetValue(a.queuedMsg)
			a.input.CursorEnd()
			a.queuedMsg = ""
			a.recalcViewportHeight()
			a.refreshViewport()
			return a, nil
		}
		// When the input is empty, scroll up rather than moving the text cursor.
		if (a.state == stateChat || a.state == stateStreaming) && a.input.Value() == "" {
			a.scrollUp(3)
			return a, nil
		}

	case "esc":
		if a.queuedMsg != "" {
			a.queuedMsg = ""
			a.recalcViewportHeight()
			a.refreshViewport()
			return a, nil
		}
		if a.chipFocused {
			a.chipFocused = false
			return a, nil
		}

	case "enter":
		a.ctrlCPending = false
		if a.chipFocused {
			a.chipFocused = false
			return a, nil
		}
		// Expand a collapsed thread header when enter is pressed in idle chat state.
		if a.state == stateChat {
			if handled := a.toggleThreadAtCursor(); handled {
				return a, nil
			}
		}
		if a.state == stateStreaming {
			raw := strings.TrimSpace(a.input.Value())
			if raw != "" {
				a.queuedMsg = raw
				a.input.SetValue("")
				a.recalcViewportHeight()
				a.refreshViewport()
			}
			return a, nil
		}
		if a.state == statePermAwait || a.state == stateWriteAwait {
			return a, nil
		}
		raw := strings.TrimSpace(a.input.Value())
		if raw == "" {
			return a, nil
		}
		a.input.SetValue("")
		return a, a.submitMessage(raw)

	case "a", "y":
		if a.state == statePermAwait {
			return a, a.handlePermission(permissions.Allow)
		}
		if a.state == stateWriteAwait {
			return a, a.handleWriteApproval(true)
		}
		// Accept artifact when cursor is on an artifact line in chat.
		if a.state == stateChat && msg.String() == "a" {
			for i := len(a.chat.history) - 1; i >= 0; i-- {
				if a.chat.history[i].isArtifactLine {
					a.acceptArtifactAtCursor()
					return a, nil
				}
			}
		}
	case "r":
		// Reject artifact when cursor is on an artifact line in chat.
		if a.state == stateChat {
			for i := len(a.chat.history) - 1; i >= 0; i-- {
				if a.chat.history[i].isArtifactLine {
					a.rejectArtifactAtCursor()
					return a, nil
				}
			}
		}
	case "A":
		if a.state == statePermAwait {
			return a, a.handlePermission(permissions.AllowAll)
		}
	case "d":
		if a.state == statePermAwait {
			return a, a.handlePermission(permissions.Deny)
		}
		if a.state == stateWriteAwait {
			return a, a.handleWriteApproval(false)
		}
	case "c", "n":
		if a.state == statePermAwait {
			return a, a.handlePermission(permissions.Deny)
		}
		if a.state == stateWriteAwait {
			return a, a.handleWriteApproval(false)
		}

	case " ":
		// Collapse an expanded thread header.
		if a.state == stateChat {
			if handled := a.collapseThreadAtCursor(); handled {
				return a, nil
			}
		}
	}

	if a.state == stateChat {
		if a.dmPicker.Visible() {
			switch msg.String() {
			case "up", "ctrl+p", "down", "ctrl+n", "esc", "enter", "tab":
				var pkCmd tea.Cmd
				a.dmPicker, pkCmd = a.dmPicker.Update(msg)
				cmds = append(cmds, pkCmd)
				a.recalcViewportHeight()
				return a, tea.Batch(cmds...)
			}
		}
		if a.channelPicker.Visible() {
			switch msg.String() {
			case "up", "ctrl+p", "down", "ctrl+n", "esc", "enter", "tab":
				var pkCmd tea.Cmd
				a.channelPicker, pkCmd = a.channelPicker.Update(msg)
				cmds = append(cmds, pkCmd)
				a.recalcViewportHeight()
				return a, tea.Batch(cmds...)
			}
		}
		if a.atMention.Visible() {
			switch msg.String() {
			case "up", "ctrl+p", "down", "ctrl+n", "esc", "enter", "tab":
				var atCmd tea.Cmd
				a.atMention, atCmd = a.atMention.Update(msg)
				cmds = append(cmds, atCmd)
				return a, tea.Batch(cmds...)
			}
		}
		if msg.String() == "#" {
			a.state = stateFilePicker
			a.filePicker.maxVisible = max(6, a.height/3)
			a.filePicker.width = a.width
			a.filePicker.Show()
			a.recalcViewportHeight()
			return a, nil
		}
		var inputCmd tea.Cmd
		a.input, inputCmd = a.input.Update(msg)
		cmds = append(cmds, inputCmd)
		var vpCmd tea.Cmd
		a.viewport, vpCmd = a.viewport.Update(msg)
		cmds = append(cmds, vpCmd)
		if strings.HasPrefix(a.input.Value(), "/") {
			a.state = stateWizard
			a.wizard.Show(strings.TrimPrefix(a.input.Value(), "/"))
			a.recalcViewportHeight()
		} else {
			atPrefix := ExtractAtPrefix(a.input.Value())
			if atPrefix != "" {
				a.atMention.Show(atPrefix)
			} else {
				a.atMention.Hide()
			}
		}
		return a, tea.Batch(cmds...)
	}

	return a, tea.Batch(cmds...)
}

// drainWithTimeout reads from ch until it closes or 2 seconds elapse, whichever
// comes first. It prevents goroutine leaks when a stream is cancelled: without a
// timeout the goroutine would block indefinitely if the producer never closes ch.
func drainWithTimeout[T any](ch <-chan T) {
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-timer.C:
			return
		}
	}
}

// jumpToNextThread navigates the viewport to the next (dir=1) or previous
// (dir=-1) thread-header line in the chat history using exact line offsets.
func (a *App) jumpToNextThread(dir int) {
	history := a.chat.history
	if len(history) == 0 {
		return
	}
	// Ensure offsets are current before reading them.
	if a.chatLineOffsetsDirty {
		a.refreshViewport()
	}
	currentTop := a.viewport.YOffset
	if dir > 0 {
		// Find the first thread header whose start line is strictly below currentTop.
		for i, line := range history {
			if line.role == "thread-header" && i < len(a.chatLineOffsets) {
				if a.chatLineOffsets[i] > currentTop {
					a.scrollToLine(a.chatLineOffsets[i])
					return
				}
			}
		}
	} else {
		// Find the last thread header whose start line is strictly above currentTop.
		for i := len(history) - 1; i >= 0; i-- {
			line := history[i]
			if line.role == "thread-header" && i < len(a.chatLineOffsets) {
				if a.chatLineOffsets[i] < currentTop {
					a.scrollToLine(a.chatLineOffsets[i])
					return
				}
			}
		}
	}
}

// toggleThreadAtCursor expands a collapsed thread header (the most recently
// added collapsed one in history). Returns true if a thread was toggled.
func (a *App) toggleThreadAtCursor() bool {
	for i := len(a.chat.history) - 1; i >= 0; i-- {
		if a.chat.history[i].role == "thread-header" && a.chat.history[i].threadCollapsed {
			a.expandCollapseWithDelta(i, false)
			return true
		}
	}
	return false
}

// collapseThreadAtCursor collapses the most recently expanded thread header.
// Returns true if a thread was collapsed.
func (a *App) collapseThreadAtCursor() bool {
	for i := len(a.chat.history) - 1; i >= 0; i-- {
		if a.chat.history[i].role == "thread-header" && !a.chat.history[i].threadCollapsed {
			a.expandCollapseWithDelta(i, true)
			return true
		}
	}
	return false
}
