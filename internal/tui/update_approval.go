package tui

// update_approval.go — handlers for permission prompts and write approvals.
// Extracted from the monolithic Update() in app.go.

import (
	"context"
	"log/slog"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/scrypster/huginn/internal/permissions"
)

// handlePermissionPromptMsg processes an incoming permission gate request.
// When auto-run is enabled the tool call is immediately allowed; otherwise
// the TUI enters the permission-await state for manual user approval.
func (a *App) handlePermissionPromptMsg(msg PermissionPromptMsg) (tea.Model, tea.Cmd) {
	if a.autoRun {
		msg.RespCh <- permissions.Allow
		return a, nil
	}
	slog.Debug("tui: permission prompt triggered", "tool", msg.Req.ToolName)
	a.permPending = &msg
	a.state = statePermAwait
	a.recalcViewportHeight()
	a.refreshViewport()
	return a, nil
}

// handleWriteApprovalMsg processes a file-write approval request. Auto-run
// mode immediately allows the write; otherwise the TUI enters the
// write-await state for manual confirmation.
func (a *App) handleWriteApprovalMsg(msg writeApprovalMsg) (tea.Model, tea.Cmd) {
	if a.autoRun {
		msg.RespCh <- true
		return a, nil
	}
	a.writePending = &msg
	a.state = stateWriteAwait
	a.recalcViewportHeight()
	a.refreshViewport()
	return a, nil
}

// handleAgentDispatchFallbackMsg handles the case where a named-agent
// dispatch was attempted but failed. It cancels the current stream and
// re-routes the input through the default streaming path.
func (a *App) handleAgentDispatchFallbackMsg(msg agentDispatchFallbackMsg) (tea.Model, tea.Cmd) {
	if a.chat.cancelStream != nil {
		a.chat.cancelStream()
	}
	a.chat.runner = nil
	a.chat.eventCh = nil
	a.chat.errCh = nil
	a.state = stateStreaming
	a.activeModel = a.cfg.DefaultModel
	slog.Debug("tui: streaming started", "agent_mode", a.useAgentLoop, "model", a.activeModel)
	ctx, cancel := context.WithCancel(context.Background())
	a.chat.cancelStream = cancel
	if a.useAgentLoop {
		a.addLine("system", "Agent mode — using tools")
		a.refreshViewport()
		return a, a.streamAgentChat(ctx, msg.input)
	}
	return a, a.streamChat(ctx, msg.input)
}
