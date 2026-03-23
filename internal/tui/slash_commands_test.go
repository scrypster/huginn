package tui

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/config"
)

// ---------------------------------------------------------------------------
// handleSlashCommand — /help
// ---------------------------------------------------------------------------

func TestHandleSlashCommand_Help(t *testing.T) {
	a := newMinimalApp()
	a.handleSlashCommand(SlashCommand{Name: "help"})
	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry for /help")
	}
	found := false
	for _, h := range a.chat.history {
		if strings.Contains(h.content, "ctrl+c") {
			found = true
		}
	}
	if !found {
		t.Error("expected keybindings in /help output")
	}
}

func TestHandleSlashCommand_HelpContainsReason(t *testing.T) {
	a := newMinimalApp()
	a.handleSlashCommand(SlashCommand{Name: "help"})
	for _, h := range a.chat.history {
		if strings.Contains(h.content, "/reason") {
			return
		}
	}
	t.Error("expected '/reason' in /help output")
}

// ---------------------------------------------------------------------------
// handleSlashCommand — /workspace
// ---------------------------------------------------------------------------

func TestHandleSlashCommand_WorkspaceEmpty(t *testing.T) {
	a := newMinimalApp()
	a.handleSlashCommand(SlashCommand{Name: "workspace"})
	found := false
	for _, h := range a.chat.history {
		if strings.Contains(h.content, "Workspace") {
			found = true
		}
	}
	if !found {
		t.Error("expected 'Workspace' in history")
	}
}

func TestHandleSlashCommand_WorkspaceNotSet(t *testing.T) {
	a := newMinimalApp()
	a.handleSlashCommand(SlashCommand{Name: "workspace"})
	for _, h := range a.chat.history {
		if strings.Contains(h.content, "(not set)") {
			return
		}
	}
	t.Error("expected '(not set)' when workspace root is empty")
}

// ---------------------------------------------------------------------------
// handleSlashCommand — /stats
// ---------------------------------------------------------------------------

func TestHandleSlashCommand_StatsNoRegistry(t *testing.T) {
	a := newMinimalApp()
	a.handleSlashCommand(SlashCommand{Name: "stats"})
	found := false
	for _, h := range a.chat.history {
		if strings.Contains(h.content, "stats") {
			found = true
		}
	}
	if !found {
		t.Error("expected stats-related message in history")
	}
}

// ---------------------------------------------------------------------------
// handleSlashCommand — /impact
// ---------------------------------------------------------------------------

func TestHandleSlashCommand_ImpactNoArgs(t *testing.T) {
	a := newMinimalApp()
	a.handleSlashCommand(SlashCommand{Name: "impact", Args: ""})
	found := false
	for _, h := range a.chat.history {
		if strings.Contains(h.content, "Usage") {
			found = true
		}
	}
	if !found {
		t.Error("expected 'Usage' message for /impact with no args")
	}
}

func TestHandleSlashCommand_ImpactNoStore(t *testing.T) {
	a := newMinimalApp()
	a.handleSlashCommand(SlashCommand{Name: "impact", Args: "SomeFunc"})
	found := false
	for _, h := range a.chat.history {
		if strings.Contains(h.content, "unavailable") || strings.Contains(h.content, "storage") {
			found = true
		}
	}
	if !found {
		t.Error("expected 'unavailable' message for /impact without store")
	}
}

// ---------------------------------------------------------------------------
// handleSlashCommand — /radar
// ---------------------------------------------------------------------------

func TestHandleSlashCommand_RadarNoStore(t *testing.T) {
	a := newMinimalApp()
	a.handleSlashCommand(SlashCommand{Name: "radar"})
	found := false
	for _, h := range a.chat.history {
		if strings.Contains(h.content, "unavailable") {
			found = true
		}
	}
	if !found {
		t.Error("expected 'unavailable' message for /radar without store")
	}
}

func TestHandleSlashCommand_RadarNoWorkspace(t *testing.T) {
	a := newMinimalApp()
	// Set a fake store that won't be nil but also won't work.
	// workspaceRoot is empty — should warn about needing a workspace.
	a.handleSlashCommand(SlashCommand{Name: "radar"})
	// Either "unavailable" (no store) or "workspace" (no root) should appear.
	found := false
	for _, h := range a.chat.history {
		if strings.Contains(h.content, "unavailable") || strings.Contains(h.content, "workspace") {
			found = true
		}
	}
	if !found {
		t.Error("expected diagnostic message for /radar without prerequisites")
	}
}

// ---------------------------------------------------------------------------
// handleSlashCommand — screen navigation commands
// ---------------------------------------------------------------------------

func TestHandleSlashCommand_Models(t *testing.T) {
	a := newMinimalApp()
	a.cfg = &config.Config{}
	a.handleSlashCommand(SlashCommand{Name: "models"})
	if a.activeScreen != screenModels {
		t.Errorf("expected screenModels, got %v", a.activeScreen)
	}
}

func TestHandleSlashCommand_Connections(t *testing.T) {
	a := newMinimalApp()
	a.handleSlashCommand(SlashCommand{Name: "connections"})
	if a.activeScreen != screenConnections {
		t.Errorf("expected screenConnections, got %v", a.activeScreen)
	}
}

func TestHandleSlashCommand_Skills(t *testing.T) {
	a := newMinimalApp()
	a.handleSlashCommand(SlashCommand{Name: "skills"})
	if a.activeScreen != screenSkills {
		t.Errorf("expected screenSkills, got %v", a.activeScreen)
	}
}

func TestHandleSlashCommand_Settings(t *testing.T) {
	a := newMinimalApp()
	a.cfg = &config.Config{}
	a.handleSlashCommand(SlashCommand{Name: "settings"})
	if a.activeScreen != screenSettings {
		t.Errorf("expected screenSettings, got %v", a.activeScreen)
	}
}

func TestHandleSlashCommand_Logs(t *testing.T) {
	a := newMinimalApp()
	a.handleSlashCommand(SlashCommand{Name: "logs"})
	if a.activeScreen != screenLogs {
		t.Errorf("expected screenLogs, got %v", a.activeScreen)
	}
}

func TestHandleSlashCommand_Inbox(t *testing.T) {
	a := newMinimalApp()
	a.handleSlashCommand(SlashCommand{Name: "inbox"})
	if a.activeScreen != screenInbox {
		t.Errorf("expected screenInbox, got %v", a.activeScreen)
	}
}

func TestHandleSlashCommand_Workflows(t *testing.T) {
	a := newMinimalApp()
	a.handleSlashCommand(SlashCommand{Name: "workflows"})
	if a.activeScreen != screenWorkflows {
		t.Errorf("expected screenWorkflows, got %v", a.activeScreen)
	}
}

// ---------------------------------------------------------------------------
// handleSlashCommand — /dm and /channel pickers
// ---------------------------------------------------------------------------

func TestHandleSlashCommand_DM(t *testing.T) {
	a := newMinimalApp()
	a.handleSlashCommand(SlashCommand{Name: "dm", Args: ""})
	if !a.dmPicker.Visible() {
		t.Error("expected dmPicker to be visible after /dm")
	}
}

func TestHandleSlashCommand_Channel(t *testing.T) {
	a := newMinimalApp()
	a.handleSlashCommand(SlashCommand{Name: "channel", Args: ""})
	if !a.channelPicker.Visible() {
		t.Error("expected channelPicker to be visible after /channel")
	}
}

// ---------------------------------------------------------------------------
// handleSlashCommand — /agents new opens wizard
// ---------------------------------------------------------------------------

func TestHandleSlashCommand_AgentsNew(t *testing.T) {
	a := newMinimalApp()
	a.handleSlashCommand(SlashCommand{Name: "agents", Args: "new"})
	if a.state != stateAgentWizard {
		t.Errorf("expected stateAgentWizard, got %v", a.state)
	}
}

// ---------------------------------------------------------------------------
// handleSlashCommand — unknown command is a no-op (no panic)
// ---------------------------------------------------------------------------

func TestHandleSlashCommand_UnknownCommand(t *testing.T) {
	a := newMinimalApp()
	// Should not panic — unknown commands fall through with no side effect.
	a.handleSlashCommand(SlashCommand{Name: "not-a-real-command"})
}

// ---------------------------------------------------------------------------
// handleSlashCommand — /switch-model adds line to history
// ---------------------------------------------------------------------------

func TestHandleSlashCommand_SwitchModel(t *testing.T) {
	a := newMinimalApp()
	a.handleSlashCommand(SlashCommand{Name: "switch-model"})
	found := false
	for _, h := range a.chat.history {
		if strings.Contains(h.content, "Switch") {
			found = true
		}
	}
	if !found {
		t.Error("expected 'Switch' in history for /switch-model")
	}
}

// ---------------------------------------------------------------------------
// helpText function
// ---------------------------------------------------------------------------

func TestHelpText_ContainsReason(t *testing.T) {
	h := helpText()
	if !strings.Contains(h, "/reason") {
		t.Error("expected '/reason' in helpText()")
	}
}

func TestHelpText_ContainsCtrlC(t *testing.T) {
	h := helpText()
	if !strings.Contains(h, "ctrl+c") {
		t.Error("expected 'ctrl+c' in helpText()")
	}
}

// ---------------------------------------------------------------------------
// handleAgentsCommand — unknown sub-command returns helpful error
// ---------------------------------------------------------------------------

func TestHandleAgentsCommand_UnknownSubCommand(t *testing.T) {
	a := newAppWithAgents()
	msg := a.handleAgentsCommand("foobar")
	if !strings.Contains(msg, "Unknown") {
		t.Errorf("expected 'Unknown' in response for unknown sub-command, got: %s", msg)
	}
}

// ---------------------------------------------------------------------------
// handleParallelCommand — no args returns usage
// ---------------------------------------------------------------------------

func TestHandleParallelCommand_NoArgs(t *testing.T) {
	a := newMinimalApp()
	a.handleSlashCommand(SlashCommand{Name: "parallel", Args: ""})
	// With no orchestrator, should say "requires an orchestrator" (or similar).
	// With orchestrator but empty args, should show "Usage".
	if len(a.chat.history) == 0 {
		t.Error("expected at least one history entry for /parallel")
	}
}

// ---------------------------------------------------------------------------
// handleSwarmCommand — no args returns usage
// ---------------------------------------------------------------------------

func TestHandleSwarmCommand_NoArgs(t *testing.T) {
	a := newMinimalApp()
	a.handleSlashCommand(SlashCommand{Name: "swarm", Args: ""})
	found := false
	for _, h := range a.chat.history {
		if strings.Contains(h.content, "Usage") || strings.Contains(h.content, "swarm") {
			found = true
		}
	}
	if !found {
		t.Error("expected usage hint for /swarm with no args")
	}
}
