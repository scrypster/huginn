package tui

// scroll.go — viewport scroll helpers implementing the follow-mode state machine.
//
// Default (scrollMode=false): new content auto-scrolls to bottom (follow mode).
// Scroll mode (scrollMode=true): user has manually scrolled up; new content
// accumulates a counter shown in the footer until they return to the bottom.

// scrollUp enters scroll mode and moves the chat viewport up by n lines.
func (a *App) scrollUp(n int) {
	a.scrollMode = true
	a.viewport.ScrollUp(n)
}

// scrollDown moves the chat viewport down by n lines. Re-enters follow mode
// automatically when the viewport reaches the bottom.
func (a *App) scrollDown(n int) {
	a.viewport.ScrollDown(n)
	if a.viewport.AtBottom() {
		a.enterFollowMode()
	}
}

// enterFollowMode disables scroll mode, resets the new-lines counter, and
// jumps immediately to the bottom of the viewport.
func (a *App) enterFollowMode() {
	a.scrollMode = false
	a.newLinesWhileScrolled = 0
	a.viewport.GotoBottom()
}

// scrollToLine sets scroll mode and positions the viewport so that the given
// line number is at the top. Does NOT call refreshViewport — callers are
// responsible for ensuring offsets are current before calling.
func (a *App) scrollToLine(n int) {
	a.scrollMode = true
	a.viewport.SetYOffset(n)
}

// expandCollapseWithDelta expands (newCollapsed=false) or collapses
// (newCollapsed=true) the thread header at history index idx. If the thread
// header is above the current viewport, the YOffset is adjusted by the
// line-count delta so the user's reading position is preserved.
func (a *App) expandCollapseWithDelta(idx int, newCollapsed bool) {
	if idx < 0 || idx >= len(a.chat.history) {
		a.enterFollowMode()
		a.refreshViewport()
		return
	}
	if a.chat.history[idx].threadCollapsed == newCollapsed {
		a.enterFollowMode()
		a.refreshViewport()
		return
	}

	// Ensure offsets are current before reading them.
	if a.chatLineOffsetsDirty {
		a.refreshViewport()
	}

	oldOffset := a.viewport.YOffset
	oldThreadLine := 0
	if len(a.chatLineOffsets) > idx {
		oldThreadLine = a.chatLineOffsets[idx]
	}
	aboveViewport := oldThreadLine < oldOffset

	// Mutate the thread.
	a.chat.history[idx].renderedCache = ""
	a.chat.history[idx].threadCollapsed = newCollapsed
	a.chatLineOffsetsDirty = true

	// Rebuild content and offsets.
	a.refreshViewport()

	if aboveViewport && len(a.chatLineOffsets) > idx {
		// Thread is above the current view — shift YOffset by the size delta
		// so the content the user was reading stays in view.
		newThreadLine := a.chatLineOffsets[idx]
		delta := newThreadLine - oldThreadLine
		a.viewport.SetYOffset(oldOffset + delta)
		a.scrollMode = true
	} else {
		// Thread at or below view — jump to bottom to show it.
		a.enterFollowMode()
	}
}
