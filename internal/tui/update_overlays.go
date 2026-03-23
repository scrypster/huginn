package tui

// update_overlays.go — handlers for all overlay/picker/wizard/session messages.
// Extracted from the monolithic Update() in app.go.

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/scrypster/huginn/internal/agents"
)

// handleWizardSelectMsg executes the slash command chosen from the wizard picker.
func (a *App) handleWizardSelectMsg(msg WizardSelectMsg) (tea.Model, tea.Cmd) {
	a.state = stateChat
	a.recalcViewportHeight()
	return a, a.handleSlashCommand(msg.Command)
}

// handleWizardDismissMsg closes the slash-command wizard without executing anything.
func (a *App) handleWizardDismissMsg(_ WizardDismissMsg) (tea.Model, tea.Cmd) {
	a.state = stateChat
	a.input.SetValue("")
	a.recalcViewportHeight()
	return a, nil
}

// handleAtMentionSelectMsg inserts the selected agent name into the input and
// hides the @-mention autocomplete dropdown.
func (a *App) handleAtMentionSelectMsg(msg AtMentionSelectMsg) (tea.Model, tea.Cmd) {
	a.input.SetValue(replaceAtMention(a.input.Value(), msg.Name))
	a.input.CursorEnd()
	a.atMention.Hide()
	return a, nil
}

// handleAtMentionDismissMsg hides the @-mention autocomplete dropdown.
func (a *App) handleAtMentionDismissMsg(_ AtMentionDismissMsg) (tea.Model, tea.Cmd) {
	a.atMention.Hide()
	return a, nil
}

// handleDMSwitchMsg switches the active agent to the one chosen in the DM picker.
func (a *App) handleDMSwitchMsg(msg DMSwitchMsg) (tea.Model, tea.Cmd) {
	a.dmPicker.Hide()
	a.primaryAgent = msg.Agent
	a.activeChannel = "" // clear any active channel
	a.input.Placeholder = "Message " + msg.Agent + "…"
	a.addLine("system", fmt.Sprintf("Switched to @%s", msg.Agent))
	a.recalcViewportHeight()
	a.refreshViewport()
	return a, nil
}

// handleChannelSwitchMsg switches to the channel chosen in the channel picker.
func (a *App) handleChannelSwitchMsg(msg ChannelSwitchMsg) (tea.Model, tea.Cmd) {
	a.channelPicker.Hide()
	a.activeChannel = msg.Name
	a.sidebar.SetActive(msg.Name)
	a.input.Placeholder = a.channelPlaceholder(msg.Name)
	a.addLine("system", fmt.Sprintf("Switched to #%s", msg.Name))
	a.recalcViewportHeight()
	a.refreshViewport()
	return a, nil
}

// handlePickerDismissMsg hides both the DM and channel pickers without selection.
func (a *App) handlePickerDismissMsg(_ pickerDismissMsg) (tea.Model, tea.Cmd) {
	a.dmPicker.Hide()
	a.channelPicker.Hide()
	a.recalcViewportHeight()
	return a, nil
}

// handleSidebarSelectMsg processes a selection from the sidebar, switching the
// active DM agent or channel accordingly.
func (a *App) handleSidebarSelectMsg(msg SidebarSelectMsg) (tea.Model, tea.Cmd) {
	switch msg.Kind {
	case sidebarSectionDMs:
		a.primaryAgent = msg.Name
		a.activeChannel = "" // clear any active channel
		a.sidebar.SetActive(msg.Name)
		a.input.Placeholder = "Message " + msg.Name + "…"
		if a.agentReg != nil {
			a.agentReg.SetDefault(msg.Name)
		}
		a.addLine("system", fmt.Sprintf("Switched to @%s", msg.Name))
	case sidebarSectionChannels:
		a.activeChannel = msg.Name
		a.sidebar.SetActive(msg.Name)
		a.input.Placeholder = a.channelPlaceholder(msg.Name)
		a.addLine("system", fmt.Sprintf("Switched to #%s", msg.Name))
	}
	a.sidebar.focused = false
	a.refreshViewport()
	return a, nil
}

// handleSidebarBlurMsg returns keyboard focus from the sidebar to the chat input.
func (a *App) handleSidebarBlurMsg(_ SidebarBlurMsg) (tea.Model, tea.Cmd) {
	a.sidebar.focused = false
	return a, nil
}

// handleStartupHealthCheckMsg kicks off the asynchronous startup health check.
func (a *App) handleStartupHealthCheckMsg(_ startupHealthCheckMsg) (tea.Model, tea.Cmd) {
	return a, a.runStartupHealthCheck()
}

// handleHealthCheckResultMsg displays any issues found by the startup health check.
func (a *App) handleHealthCheckResultMsg(msg healthCheckResultMsg) (tea.Model, tea.Cmd) {
	return a, a.handleHealthCheckResult(msg.issues)
}

// handleAgentWizardDoneMsg saves the newly created agent and returns to chat.
func (a *App) handleAgentWizardDoneMsg(msg AgentWizardDoneMsg) (tea.Model, tea.Cmd) {
	a.state = stateChat
	if saveErr := agents.SaveAgentDefault(msg.Agent); saveErr != nil {
		a.addLine("error", fmt.Sprintf("Error saving agent: %v", saveErr))
	} else {
		a.addLine("system", fmt.Sprintf("Agent %q created and saved.", msg.Agent.Name))
	}
	a.recalcViewportHeight()
	a.refreshViewport()
	return a, nil
}

// handleAgentWizardCancelMsg closes the agent creation wizard without saving.
func (a *App) handleAgentWizardCancelMsg(_ AgentWizardCancelMsg) (tea.Model, tea.Cmd) {
	a.state = stateChat
	a.recalcViewportHeight()
	return a, nil
}

// handleWizardTabCompleteMsg applies the tab-completed slash command text to
// the input field and updates the wizard filter.
func (a *App) handleWizardTabCompleteMsg(msg WizardTabCompleteMsg) (tea.Model, tea.Cmd) {
	a.input.SetValue("/" + msg.Text)
	a.input.CursorEnd()
	a.wizard.UpdateFilter(msg.Text)
	return a, nil
}

// handleFilePickerConfirmMsg adds the selected files to the attachment list,
// deduplicating and enforcing the maximum attachment count.
func (a *App) handleFilePickerConfirmMsg(msg FilePickerConfirmMsg) (tea.Model, tea.Cmd) {
	a.state = stateChat
	const maxAttachments = 10
	for _, p := range msg.Paths {
		if len(a.attachments) >= maxAttachments {
			break
		}
		dup := false
		for _, existing := range a.attachments {
			if existing == p {
				dup = true
				break
			}
		}
		if !dup {
			a.attachments = append(a.attachments, p)
		}
	}
	a.recalcViewportHeight()
	return a, nil
}

// handleFilePickerCancelMsg closes the file picker without attaching anything.
func (a *App) handleFilePickerCancelMsg(_ FilePickerCancelMsg) (tea.Model, tea.Cmd) {
	a.state = stateChat
	a.recalcViewportHeight()
	return a, nil
}

// handleSessionPickerMsg resumes the session chosen in the session picker.
func (a *App) handleSessionPickerMsg(msg SessionPickerMsg) (tea.Model, tea.Cmd) {
	a.state = stateChat
	a.recalcViewportHeight()
	a.addLine("system", fmt.Sprintf("Resumed session: %s", msg.ID))
	a.refreshViewport()
	return a, a.resumeSession(msg.ID)
}

// handleSessionResumedMsg replays the loaded session's messages into the chat history.
func (a *App) handleSessionResumedMsg(msg sessionResumedMsg) (tea.Model, tea.Cmd) {
	a.activeSession = msg.sess
	a.state = stateChat
	a.chat.history = nil
	for _, m := range msg.messages {
		if m.Role == "user" || m.Role == "assistant" {
			a.addLine(m.Role, m.Content)
		}
	}
	a.recalcViewportHeight()
	a.refreshViewport()
	return a, nil
}

// handleSessionPickerDismissMsg closes the session picker without resuming.
func (a *App) handleSessionPickerDismissMsg(_ SessionPickerDismissMsg) (tea.Model, tea.Cmd) {
	a.state = stateChat
	a.recalcViewportHeight()
	return a, nil
}
