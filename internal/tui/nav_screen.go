package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/scrypster/huginn/internal/agents"
)

// appScreen is the top-level navigation — which major screen is visible.
// It is distinct from appState, which tracks modal/overlay state within the chat screen.
type appScreen int

const (
	screenChat        appScreen = iota // default: the main chat/conversation screen
	screenAgents                       // /agents — agent roster and management
	screenModels                       // /models — LLM provider configuration
	screenConnections                  // /connections — integration management
	screenSettings                     // /settings — workspace and display settings
	screenWorkflows                    // /workflows — workflow scheduler
	screenSkills                       // /skills — skills marketplace
	screenLogs                         // /logs — system log viewer
	screenInbox                        // /inbox — notifications and approvals
)

// backToChatMsg is dispatched when the user navigates back to the chat screen.
type backToChatMsg struct{}

// screenMeta holds display metadata for a navigation screen.
type screenMeta struct {
	title    string
	subtitle string
	body     string
	commands []string // slash commands or keybindings relevant to this screen
}

// screenMetaFor returns display metadata for the given screen.
func screenMetaFor(s appScreen) screenMeta {
	switch s {
	case screenAgents:
		return screenMeta{
			title:    "Agents",
			subtitle: "Manage your AI agents — create, configure, and switch between specialized agents.",
			body:     "Agents are specialized AI personas with custom system prompts, models, and tools.\nEach agent brings a unique focus to your workflow.",
			commands: []string{
				"Ctrl+A   — create a new agent",
				"/agents new   — launch the agent wizard",
				"/agents list  — list all agents",
				"/agents swap <name> <model>  — change an agent's model",
			},
		}
	case screenModels:
		return screenMeta{
			title:    "Models",
			subtitle: "Configure LLM providers — Anthropic, OpenAI, Ollama, OpenRouter, and more.",
			body:     "Connect to cloud providers or run local models via Ollama.\nEach provider can have multiple models assigned to different agent slots.",
			commands: []string{
				"Edit ~/.huginn/config.json  — configure providers and models",
				"huginn serve → web UI → Models  — full provider management UI",
			},
		}
	case screenConnections:
		return screenMeta{
			title:    "Connections",
			subtitle: "Manage integrations — GitHub, Slack, Jira, Datadog, Linear, and more.",
			body:     "Connections give agents access to external services via MCP tools.\nEach connection exposes a set of tools the agent can invoke during a conversation.",
			commands: []string{
				"Edit ~/.huginn/mcp.json  — configure MCP server connections",
				"huginn serve → web UI → Connections  — full integration management UI",
			},
		}
	case screenSettings:
		return screenMeta{
			title:    "Settings",
			subtitle: "Configure workspace, tools, and display preferences.",
			body:     "Settings control workspace root, indexing, tool permissions,\ndisplay themes, and CLI behavior.",
			commands: []string{
				"Edit ~/.huginn/config.json  — all workspace and tool settings",
				"huginn serve → web UI → Settings  — visual settings editor",
			},
		}
	case screenWorkflows:
		return screenMeta{
			title:    "Workflows",
			subtitle: "Create and schedule automated agent workflows.",
			body:     "Workflows run agent pipelines on a schedule or event trigger.\nChain multiple agents together to automate complex recurring tasks.",
			commands: []string{
				"Edit ~/.huginn/workflows.json  — define workflows in JSON",
				"huginn serve → web UI → Workflows  — visual workflow builder",
			},
		}
	case screenSkills:
		return screenMeta{
			title:    "Skills",
			subtitle: "Browse and install skills from the registry.",
			body:     "Skills extend agent capabilities with specialized instructions and tool bundles.\nInstall community skills or create your own.",
			commands: []string{
				"huginn serve → web UI → Skills  — browse and install from the skills marketplace",
			},
		}
	case screenLogs:
		return screenMeta{
			title:    "Logs",
			subtitle: "View system logs with level filtering.",
			body:     "System logs capture agent events, tool calls, errors, and startup diagnostics.",
			commands: []string{
				"huginn logs  — stream logs from the CLI",
				"huginn serve → web UI → Logs  — interactive log viewer with filtering",
			},
		}
	case screenInbox:
		return screenMeta{
			title:    "Inbox",
			subtitle: "View notifications and pending approval requests.",
			body:     "The inbox collects agent messages, pending tool approvals, and workflow alerts\nthat require your attention.",
			commands: []string{
				"huginn serve → web UI → Inbox  — full notification management",
			},
		}
	default:
		return screenMeta{title: "Unknown"}
	}
}

// updateScreen handles messages when a non-chat navigation screen is active.
// Returns (model, cmd) — or nil cmd and the backToChatMsg to navigate home.
func (a *App) updateScreen(msg tea.Msg) (tea.Model, tea.Cmd) {
	// backToChatMsg always returns to chat regardless of which screen sent it.
	if _, ok := msg.(backToChatMsg); ok {
		a.activeScreen = screenChat
		return a, nil
	}

	switch a.activeScreen {
	case screenAgents:
		return a.updateAgentsScreen(msg)
	case screenModels:
		var cmd tea.Cmd
		a.modelsScreen, cmd = a.modelsScreen.Update(msg)
		if wm, ok := msg.(tea.WindowSizeMsg); ok {
			a.width = wm.Width
			a.height = wm.Height
		}
		return a, cmd
	case screenSettings:
		var cmd tea.Cmd
		a.settingsScreen, cmd = a.settingsScreen.Update(msg)
		if wm, ok := msg.(tea.WindowSizeMsg); ok {
			a.width = wm.Width
			a.height = wm.Height
		}
		return a, cmd
	default:
		return a.updatePlaceholderScreen(msg)
	}
}

// updateAgentsScreen handles events on the real Agents screen.
func (a *App) updateAgentsScreen(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.agentsScreen.width = msg.Width
		a.agentsScreen.height = msg.Height
		return a, nil

	case agentsScreenSelectMsg:
		// Switch primary agent and return to chat.
		a.primaryAgent = msg.name
		a.input.Placeholder = "Message " + msg.name + "…"
		if a.agentReg != nil {
			a.agentReg.SetDefault(msg.name)
		}
		a.activeScreen = screenChat
		a.addLine("system", fmt.Sprintf("Primary agent → %s", msg.name))
		a.refreshViewport()
		return a, nil

	case agentsScreenCreateMsg:
		// Open the agent wizard overlay (back in chat context).
		a.activeScreen = screenChat
		a.agentWizard = newAgentWizardWithMemory(a.muninnEndpoint, a.muninnConnected)
		a.state = stateAgentWizard
		a.recalcViewportHeight()
		return a, nil

	case agentsScreenDeleteMsg:
		if delErr := agents.DeleteAgentDefault(msg.name); delErr != nil {
			a.agentsScreen.msgLine = fmt.Sprintf("Delete failed: %v", delErr)
		} else {
			if a.agentReg != nil {
				a.agentReg.Unregister(msg.name)
				a.agentsScreen.refresh(a.agentReg)
				// Update DM picker names.
				a.dmPicker.SetNames(a.agentReg.Names())
				a.atMention.SetNames(a.agentReg.Names())
			}
			a.agentsScreen.msgLine = fmt.Sprintf("Agent %q deleted.", msg.name)
			// If we deleted the primary agent, clear it.
			if a.primaryAgent == msg.name {
				a.primaryAgent = ""
				a.input.Placeholder = "→ Add a follow-up"
			}
		}
		return a, nil

	default:
		var cmd tea.Cmd
		a.agentsScreen, cmd = a.agentsScreen.Update(msg)
		return a, cmd
	}
}

// updatePlaceholderScreen handles events on placeholder (not-yet-implemented) screens.
func (a *App) updatePlaceholderScreen(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q", "ctrl+c":
			a.activeScreen = screenChat
		}
	}
	return a, nil
}

// viewScreen renders the current navigation screen.
func (a *App) viewScreen() string {
	// Delegate to real screens when they are fully implemented.
	switch a.activeScreen {
	case screenAgents:
		return a.agentsScreen.View(a.width, a.height)
	case screenModels:
		return a.modelsScreen.View(a.width, a.height)
	case screenSettings:
		return a.settingsScreen.View(a.width, a.height)
	}

	// All other screens get the placeholder renderer.
	meta := screenMetaFor(a.activeScreen)

	// Styles
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		MarginBottom(1)
	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("250")).
		MarginBottom(1)
	bodyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		MarginBottom(2)
	commandLabelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		MarginBottom(0)
	commandStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("39")).
		MarginLeft(2)
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		MarginTop(2)

	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(titleStyle.Render(meta.title))
	sb.WriteString("\n")
	sb.WriteString(subtitleStyle.Render(meta.subtitle))
	sb.WriteString("\n")
	sb.WriteString(bodyStyle.Render(meta.body))

	if len(meta.commands) > 0 {
		sb.WriteString(commandLabelStyle.Render("Available now:"))
		sb.WriteString("\n")
		for _, cmd := range meta.commands {
			sb.WriteString(commandStyle.Render("• "+cmd))
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n")
	sb.WriteString(footerStyle.Render("Full screen coming in the next release."))

	content := lipgloss.NewStyle().MarginLeft(4).Render(sb.String())

	// Render the footer
	footer := a.renderFooter()

	// Calculate available height for content area
	footerHeight := strings.Count(footer, "\n") + 1
	contentHeight := a.height - footerHeight

	if contentHeight < 1 {
		contentHeight = 1
	}

	// Build the nav bar (breadcrumb)
	navBar := StyleDim.Render(fmt.Sprintf("  %s  ← Esc to return to chat", meta.title))
	navBarLines := 1

	// Fill remaining content area
	contentLines := strings.Count(content, "\n") + 1
	padLines := contentHeight - navBarLines - contentLines - 1
	if padLines < 0 {
		padLines = 0
	}
	padding := strings.Repeat("\n", padLines)

	return lipgloss.JoinVertical(lipgloss.Left,
		navBar,
		content+padding,
		footer,
	)
}
