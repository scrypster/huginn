package tui

import (
	"fmt"
	"log/slog"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/scrypster/huginn/internal/permissions"
)

// renderPermissionPrompt renders the inline permission prompt for statePermAwait.
func (a *App) renderPermissionPrompt() string {
	if a.permPending == nil {
		return StyleApprovalPrompt.Render("[a]llow  [A]lways allow  [d]eny")
	}
	req := a.permPending.Req
	summary := permissions.FormatRequest(req)
	line1 := StyleDim.Render("Tool wants to: ") + summary
	line2 := StyleApprovalPrompt.Render("[a]llow once  [A]lways allow this session  [d]eny")
	return lipgloss.JoinVertical(lipgloss.Left, line1, line2)
}

// renderWriteApprovalPrompt renders the inline prompt for stateWriteAwait.
func (a *App) renderWriteApprovalPrompt() string {
	if a.writePending == nil {
		return ""
	}
	header := fmt.Sprintf("Write to: %s", a.writePending.Path)
	hint := StyleApprovalPrompt.Render("[y]es  [n]o")
	return lipgloss.JoinVertical(lipgloss.Left, StyleApprovalPrompt.Render(header), hint)
}

// handleWriteApproval responds to a file-write approval prompt (stateWriteAwait).
func (a *App) handleWriteApproval(allow bool) tea.Cmd {
	if a.writePending == nil {
		return nil
	}
	path := a.writePending.Path
	a.writePending.RespCh <- allow
	a.writePending = nil
	a.state = stateStreaming
	if allow {
		a.addLine("system", fmt.Sprintf("Write to %q allowed.", path))
	} else {
		a.addLine("system", fmt.Sprintf("Write to %q denied.", path))
	}
	a.recalcViewportHeight()
	a.refreshViewport()
	return nil
}

// handlePermission responds to a tool permission prompt (statePermAwait).
// Sends the decision on permPending.RespCh and resumes the streaming state.
func (a *App) handlePermission(decision permissions.Decision) tea.Cmd {
	if a.permPending == nil {
		return nil
	}
	req := a.permPending.Req

	label := "allowed"
	switch decision {
	case permissions.AllowAll:
		label = "always allowed this session"
	case permissions.Deny:
		label = "denied"
	}
	slog.Debug("tui: permission decision", "tool", req.ToolName, "decision", label)

	a.permPending.RespCh <- decision
	a.permPending = nil
	a.state = stateStreaming

	a.addLine("system", fmt.Sprintf("Tool %q %s.", req.ToolName, label))
	a.recalcViewportHeight()
	a.refreshViewport()
	return nil
}
