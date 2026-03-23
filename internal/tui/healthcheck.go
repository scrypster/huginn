package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// startupHealthCheckMsg is sent on app startup to trigger the health check.
type startupHealthCheckMsg struct{}

// runStartupHealthCheck inspects the app state after init and shows actionable
// guidance in the chat viewport for any common misconfigurations:
//
//  1. No agents configured → first-run welcome, open wizard prompt.
//  2. No LLM backend configured → warn and point to /models.
//  3. Primary agent set but model is empty → warn.
//
// It returns a tea.Cmd so the caller can trigger further UI updates.
func (a *App) runStartupHealthCheck() tea.Cmd {
	var issues []string

	// ── 1. No agents ─────────────────────────────────────────────────────────
	noAgents := a.agentReg == nil || len(a.agentReg.Names()) == 0
	if noAgents {
		issues = append(issues, "firstRun")
	}

	// ── 2. No backend model configured ───────────────────────────────────────
	noBackend := a.cfg == nil || a.cfg.DefaultModel == ""
	if noBackend && !noAgents {
		// Only show the model warning when there ARE agents (firstRun covers both).
		issues = append(issues, "noBackend")
	}

	if len(issues) == 0 {
		return nil // everything looks good
	}

	return func() tea.Msg {
		return healthCheckResultMsg{issues: issues}
	}
}

// healthCheckResultMsg carries the result of the startup health check.
type healthCheckResultMsg struct{ issues []string }

// handleHealthCheckResult processes the health check result and updates the viewport.
// Called from App.Update() when healthCheckResultMsg is received.
func (a *App) handleHealthCheckResult(issues []string) tea.Cmd {
	for _, issue := range issues {
		switch issue {
		case "firstRun":
			// Show a welcome message and let the user decide what to do next.
			// We do NOT auto-open the wizard — forcing a blocking modal on first
			// run is poor UX. The user can type /agents to create an agent.
			a.viewport.SetContent(firstRunWelcome())
			a.refreshViewport()
		case "noBackend":
			a.addLine("system", healthWarning(
				"No LLM provider configured",
				"Run /models or edit ~/.huginn/config.json to add an Anthropic, OpenAI, or Ollama provider.",
			))
			a.refreshViewport()
		}
	}
	return nil
}

// firstRunWelcome renders the welcome screen shown on first launch (no agents yet).
func firstRunWelcome() string {
	var sb strings.Builder
	sb.WriteString(StyleAccent.Render("  HUGINN  —  your local AI coding assistant") + "\n\n")
	sb.WriteString(StyleDim.Render("  Welcome! No agents are configured yet.") + "\n\n")
	sb.WriteString(StyleDim.Render("  Agents are AI personas — each with a custom system prompt,") + "\n")
	sb.WriteString(StyleDim.Render("  model, and set of tools tailored to a specific role.") + "\n\n")
	sb.WriteString(StyleDim.Render("  Type ") + StyleAccent.Render("/agents") + StyleDim.Render(" to create your first agent.") + "\n")
	sb.WriteString(StyleDim.Render("  Type ") + StyleAccent.Render("/models") + StyleDim.Render(" to configure your LLM provider.") + "\n")
	return sb.String()
}

// healthWarning formats a structured health warning for display in the chat viewport.
func healthWarning(title, detail string) string {
	return fmt.Sprintf("  ⚠  %s\n\n     %s", title, detail)
}
