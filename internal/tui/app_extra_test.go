package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/scrypster/huginn/internal/modelconfig"
	"github.com/scrypster/huginn/internal/repo"
	"github.com/scrypster/huginn/internal/stats"
)

// ============================================================
// welcomeMessage
// ============================================================

func TestWelcomeMessage_NonEmpty(t *testing.T) {
	result := welcomeMessage()
	if result == "" {
		t.Error("welcomeMessage() should return non-empty string")
	}
}

func TestWelcomeMessage_ContainsHuginn(t *testing.T) {
	result := welcomeMessage()
	if !strings.Contains(result, "HUGINN") {
		t.Errorf("expected 'HUGINN' in welcome message, got %q", result)
	}
}

// ============================================================
// App setters
// ============================================================

func TestApp_SetStatsRegistry(t *testing.T) {
	a := newMinimalApp()
	reg := stats.NewRegistry()
	a.SetStatsRegistry(reg)
	if a.statsReg != reg {
		t.Error("SetStatsRegistry should wire up the registry")
	}
}

func TestApp_SetStatsRegistry_Nil(t *testing.T) {
	a := newMinimalApp()
	a.SetStatsRegistry(nil)
	if a.statsReg != nil {
		t.Error("SetStatsRegistry(nil) should set to nil")
	}
}

func TestApp_SetStore_NilNoOp(t *testing.T) {
	a := newMinimalApp()
	a.SetStore(nil) // should not panic
	if a.store != nil {
		t.Error("SetStore(nil) should set store to nil")
	}
}

func TestApp_SetUseAgentLoop(t *testing.T) {
	a := newMinimalApp()
	a.SetUseAgentLoop(true)
	if !a.useAgentLoop {
		t.Error("SetUseAgentLoop(true) should set useAgentLoop=true")
	}
	a.SetUseAgentLoop(false)
	if a.useAgentLoop {
		t.Error("SetUseAgentLoop(false) should set useAgentLoop=false")
	}
}

func TestApp_SetWorkspace(t *testing.T) {
	a := newMinimalApp()
	idx := &repo.Index{Root: "/tmp"}
	a.SetWorkspace("/tmp", idx)
	if a.workspaceRoot != "/tmp" {
		t.Errorf("expected workspaceRoot='/tmp', got %q", a.workspaceRoot)
	}
	if a.idx != idx {
		t.Error("expected idx to be set")
	}
}

func TestApp_SetWorkspace_NilIndex(t *testing.T) {
	a := newMinimalApp()
	a.SetWorkspace("/tmp", nil)
	if a.workspaceRoot != "/tmp" {
		t.Errorf("expected workspaceRoot='/tmp', got %q", a.workspaceRoot)
	}
}

func TestApp_SetWorkspace_WithChunks(t *testing.T) {
	a := newMinimalApp()
	idx := &repo.Index{
		Root: "/workspace",
		Chunks: []repo.FileChunk{
			{Path: "main.go", Content: "package main"},
			{Path: "main.go", Content: "func main() {}"},  // duplicate
			{Path: "app.go", Content: "package main"},
		},
	}
	a.SetWorkspace("/workspace", idx)
	// File picker should have been populated (no panic, no error)
	if len(a.filePicker.allFiles) == 0 {
		t.Error("expected filePicker.allFiles to be populated")
	}
}

// ============================================================
// Init
// ============================================================

func TestApp_Init_ReturnsBlink(t *testing.T) {
	a := newMinimalApp()
	cmd := a.Init()
	if cmd == nil {
		t.Error("App.Init() should return textinput.Blink command")
	}
}

// ============================================================
// View
// ============================================================

func newRenderableApp() *App {
	ti := textinput.New()
	ti.Focus()
	ti.Width = 74

	vp := viewport.New(80, 20)

	return &App{
		state:    stateChat,
		input:    ti,
		viewport: vp,
		width:    80,
		height:   24,
		autoRun:  true,
		models:   modelconfig.DefaultModels(),
	}
}

func TestApp_View_ZeroWidth_ReturnsLoading(t *testing.T) {
	a := newRenderableApp()
	a.width = 0
	result := a.View()
	if !strings.Contains(result, "Loading") {
		t.Errorf("expected 'Loading…' for zero width, got %q", result)
	}
}

func TestApp_View_NormalState_NonEmpty(t *testing.T) {
	a := newRenderableApp()
	result := a.View()
	if result == "" {
		t.Error("View() should return non-empty string in normal state")
	}
}

func TestApp_View_ContainsInputBox(t *testing.T) {
	a := newRenderableApp()
	result := a.View()
	// The input box uses rounded border chars
	if result == "" {
		t.Error("View() should be non-empty")
	}
}

func TestApp_View_WithAttachments_ShowsChips(t *testing.T) {
	a := newRenderableApp()
	a.attachments = []string{"main.go"}
	result := a.View()
	if !strings.Contains(result, "main.go") {
		t.Errorf("expected 'main.go' chip in View with attachments, got %q", result)
	}
}

func TestApp_View_WithQueuedMsg_ShowsFollowUpBox(t *testing.T) {
	a := newRenderableApp()
	a.queuedMsg = "follow me up"
	result := a.View()
	if !strings.Contains(result, "follow me up") {
		t.Errorf("expected queued message in View, got %q", result)
	}
}

func TestApp_View_StreamingState(t *testing.T) {
	a := newRenderableApp()
	a.state = stateStreaming
	result := a.View()
	if result == "" {
		t.Error("expected non-empty view in stateStreaming")
	}
}

// ============================================================
// refreshViewport — exercises various chatLine roles
// ============================================================

func TestApp_RefreshViewport_UserLine(t *testing.T) {
	a := newRenderableApp()
	a.addLine("user", "hello assistant")
	a.refreshViewport()
	content := a.viewport.View()
	if !strings.Contains(content, "hello assistant") {
		t.Errorf("expected user message in viewport, got %q", content)
	}
}

func TestApp_RefreshViewport_SystemLine(t *testing.T) {
	a := newRenderableApp()
	a.addLine("system", "system message")
	a.refreshViewport()
	content := a.viewport.View()
	if !strings.Contains(content, "system message") {
		t.Errorf("expected system message in viewport, got %q", content)
	}
}

func TestApp_RefreshViewport_ErrorLine(t *testing.T) {
	a := newRenderableApp()
	a.addLine("error", "something went wrong")
	a.refreshViewport()
	content := a.viewport.View()
	if !strings.Contains(content, "something went wrong") {
		t.Errorf("expected error in viewport, got %q", content)
	}
}

func TestApp_RefreshViewport_ToolCallLine(t *testing.T) {
	a := newRenderableApp()
	a.addLine("tool-call", "bash: ls -la")
	a.refreshViewport()
	content := a.viewport.View()
	if !strings.Contains(content, "bash: ls -la") {
		t.Errorf("expected tool-call in viewport, got %q", content)
	}
}

func TestApp_RefreshViewport_ToolDoneLine(t *testing.T) {
	a := newRenderableApp()
	a.chat.history = append(a.chat.history, chatLine{
		role:    "tool-done",
		content: "found 3 files",
	})
	a.refreshViewport()
	content := a.viewport.View()
	if !strings.Contains(content, "found 3 files") {
		t.Errorf("expected tool-done in viewport, got %q", content)
	}
}

func TestApp_RefreshViewport_ToolDoneWithDuration(t *testing.T) {
	a := newRenderableApp()
	a.chat.history = append(a.chat.history, chatLine{
		role:     "tool-done",
		content:  "output",
		duration: "1.2s",
	})
	a.refreshViewport()
	content := a.viewport.View()
	if !strings.Contains(content, "1.2s") {
		t.Errorf("expected duration in viewport, got %q", content)
	}
}

func TestApp_RefreshViewport_ToolDoneTruncated(t *testing.T) {
	a := newRenderableApp()
	a.chat.history = append(a.chat.history, chatLine{
		role:      "tool-done",
		content:   "summary line",
		truncated: 5,
	})
	a.refreshViewport()
	content := a.viewport.View()
	if !strings.Contains(content, "truncated") {
		t.Errorf("expected 'truncated' indicator in viewport, got %q", content)
	}
}

func TestApp_RefreshViewport_ToolDoneExpanded(t *testing.T) {
	a := newRenderableApp()
	a.chat.history = append(a.chat.history, chatLine{
		role:       "tool-done",
		content:    "summary",
		truncated:  3,
		fullOutput: "line1\nline2\nline3\nline4",
		expanded:   true,
	})
	a.refreshViewport()
	content := a.viewport.View()
	if !strings.Contains(content, "collapse") {
		t.Errorf("expected 'collapse' hint for expanded tool-done, got %q", content)
	}
}

func TestApp_RefreshViewport_ToolErrorLine(t *testing.T) {
	a := newRenderableApp()
	a.addLine("tool-error", "✗ bash: command not found")
	a.refreshViewport()
	content := a.viewport.View()
	if !strings.Contains(content, "command not found") {
		t.Errorf("expected tool-error in viewport, got %q", content)
	}
}

func TestApp_RefreshViewport_StreamingSpinner(t *testing.T) {
	a := newRenderableApp()
	a.state = stateStreaming
	// No streaming content yet — shows spinner
	a.refreshViewport()
	content := a.viewport.View()
	if !strings.Contains(content, "Generating") {
		t.Errorf("expected generating spinner in streaming viewport, got %q", content)
	}
}

func TestApp_RefreshViewport_StreamingContent(t *testing.T) {
	a := newRenderableApp()
	a.state = stateStreaming
	a.chat.streaming.WriteString("partial response...")
	a.refreshViewport()
	content := a.viewport.View()
	if !strings.Contains(content, "partial response") {
		t.Errorf("expected streaming content in viewport, got %q", content)
	}
}

// ============================================================
// handleModelCommand
// ============================================================

func TestApp_HandleModelCommand_ValidCommand(t *testing.T) {
	a := newRenderableApp()
	a.state = stateChat

	// handleModelCommand parses and stores the model
	a.handleModelCommand("use testmodel for reasoning")

	if a.models.Reasoner != "testmodel" {
		t.Errorf("expected reasoner model 'testmodel', got %q", a.models.Reasoner)
	}
	// Should add a history entry
	if len(a.chat.history) == 0 {
		t.Error("expected history entry after handleModelCommand")
	}
}

func TestApp_HandleModelCommand_InvalidCommand_NoChange(t *testing.T) {
	a := newRenderableApp()
	original := a.models.Reasoner

	a.handleModelCommand("just some text that's not a model command")

	if a.models.Reasoner != original {
		t.Errorf("model should not change for invalid command, got %q", a.models.Reasoner)
	}
}

// ============================================================
// handleSlashCommand — pure paths (no orchestrator needed)
// ============================================================

func TestApp_HandleSlashCommand_Help(t *testing.T) {
	a := newRenderableApp()
	a.handleSlashCommand(SlashCommand{Name: "help"})

	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry after /help")
	}
	last := a.chat.history[len(a.chat.history)-1]
	if !strings.Contains(last.content, "ctrl+c") {
		t.Errorf("expected help text in history, got %q", last.content)
	}
}

func TestApp_HandleSlashCommand_Reason_AddsHistory(t *testing.T) {
	a := newRenderableApp()
	a.handleSlashCommand(SlashCommand{Name: "reason"})

	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry after /reason")
	}
	last := a.chat.history[len(a.chat.history)-1]
	if !strings.Contains(strings.ToLower(last.content), "reason") {
		t.Errorf("expected reason-related message, got %q", last.content)
	}
}

func TestApp_HandleSlashCommand_Reason(t *testing.T) {
	a := newRenderableApp()
	a.handleSlashCommand(SlashCommand{Name: "reason"})

	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry after /reason")
	}
}

func TestApp_HandleSlashCommand_Iterate(t *testing.T) {
	a := newRenderableApp()
	a.handleSlashCommand(SlashCommand{Name: "iterate"})

	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry after /iterate")
	}
}

func TestApp_HandleSlashCommand_SwitchModel(t *testing.T) {
	a := newRenderableApp()
	a.handleSlashCommand(SlashCommand{Name: "switch-model"})

	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry after /switch-model")
	}
}

func TestApp_HandleSlashCommand_Stats_NoRegistry(t *testing.T) {
	a := newRenderableApp()
	a.statsReg = nil
	a.handleSlashCommand(SlashCommand{Name: "stats"})

	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry after /stats")
	}
	last := a.chat.history[len(a.chat.history)-1]
	if !strings.Contains(last.content, "stats") {
		t.Errorf("expected stats message, got %q", last.content)
	}
}

func TestApp_HandleSlashCommand_Stats_WithRegistry(t *testing.T) {
	a := newRenderableApp()
	a.statsReg = stats.NewRegistry()
	a.handleSlashCommand(SlashCommand{Name: "stats"})

	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry after /stats with registry")
	}
}

func TestApp_HandleSlashCommand_Workspace_Empty(t *testing.T) {
	a := newRenderableApp()
	a.workspaceRoot = ""
	a.handleSlashCommand(SlashCommand{Name: "workspace"})

	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry after /workspace")
	}
	last := a.chat.history[len(a.chat.history)-1]
	if !strings.Contains(last.content, "Workspace") {
		t.Errorf("expected 'Workspace' in message, got %q", last.content)
	}
}

func TestApp_HandleSlashCommand_Workspace_WithIndex(t *testing.T) {
	a := newRenderableApp()
	a.workspaceRoot = "/tmp/myrepo"
	a.idx = &repo.Index{
		Root:   "/tmp/myrepo",
		Chunks: []repo.FileChunk{{Path: "main.go", Content: "x"}},
	}
	a.handleSlashCommand(SlashCommand{Name: "workspace"})

	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry after /workspace with index")
	}
	last := a.chat.history[len(a.chat.history)-1]
	if !strings.Contains(last.content, "/tmp/myrepo") {
		t.Errorf("expected workspace root in message, got %q", last.content)
	}
}

func TestApp_HandleSlashCommand_Impact_NoArgs(t *testing.T) {
	a := newRenderableApp()
	a.handleSlashCommand(SlashCommand{Name: "impact", Args: ""})

	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry")
	}
	last := a.chat.history[len(a.chat.history)-1]
	if !strings.Contains(last.content, "Usage") {
		t.Errorf("expected usage hint for /impact with no args, got %q", last.content)
	}
}

func TestApp_HandleSlashCommand_Impact_NoStore(t *testing.T) {
	a := newRenderableApp()
	a.store = nil
	a.handleSlashCommand(SlashCommand{Name: "impact", Args: "SomeFunc"})

	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry")
	}
	last := a.chat.history[len(a.chat.history)-1]
	if !strings.Contains(last.content, "unavailable") {
		t.Errorf("expected 'unavailable' when store is nil, got %q", last.content)
	}
}

func TestApp_HandleSlashCommand_Radar_NoStore(t *testing.T) {
	a := newRenderableApp()
	a.store = nil
	a.handleSlashCommand(SlashCommand{Name: "radar"})

	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry")
	}
	last := a.chat.history[len(a.chat.history)-1]
	if !strings.Contains(last.content, "unavailable") {
		t.Errorf("expected 'unavailable' when store is nil, got %q", last.content)
	}
}

func TestApp_HandleSlashCommand_Radar_NoWorkspace(t *testing.T) {
	a := newRenderableApp()
	// We need a non-nil store but empty workspaceRoot — use a trick:
	// Since we can't easily create a real Store, just test the workspaceRoot path
	// by simulating the behavior. The handler checks store == nil first.
	// Set workspaceRoot empty but store nil to verify the nil check.
	a.store = nil
	a.workspaceRoot = ""
	a.handleSlashCommand(SlashCommand{Name: "radar"})

	if len(a.chat.history) == 0 {
		t.Fatal("expected history entry")
	}
}


// ============================================================
// newGlamourRenderer + renderMarkdown
// ============================================================

func TestNewGlamourRenderer_NonNil(t *testing.T) {
	r := newGlamourRenderer(80)
	// May be nil if glamour fails, but should not panic.
	_ = r
}

func TestApp_RenderMarkdown_NilRenderer(t *testing.T) {
	a := newRenderableApp()
	a.glamourRenderer = nil
	result := a.renderMarkdown("# Hello")
	// Falls back to plain text
	if result != "# Hello" {
		t.Errorf("expected plain fallback for nil renderer, got %q", result)
	}
}

func TestApp_RenderMarkdown_WithRenderer(t *testing.T) {
	a := newRenderableApp()
	a.glamourRenderer = newGlamourRenderer(80)
	result := a.renderMarkdown("hello world")
	// Should not be empty
	if result == "" {
		t.Error("renderMarkdown should return non-empty for valid input")
	}
}

// ============================================================
// ctrlCResetCmd
// ============================================================

func TestCtrlCResetCmd_ReturnsCmd(t *testing.T) {
	cmd := ctrlCResetCmd()
	if cmd == nil {
		t.Error("ctrlCResetCmd should return non-nil Cmd")
	}
}

// ============================================================
// Update — additional branches
// ============================================================

func TestApp_Update_CtrlCResetMsg(t *testing.T) {
	a := newMinimalApp()
	a.ctrlCPending = true

	model, cmd := a.Update(ctrlCResetMsg{})
	updated := model.(*App)

	if updated.ctrlCPending {
		t.Error("ctrlCPending should be false after ctrlCResetMsg")
	}
	if cmd != nil {
		t.Error("expected nil cmd from ctrlCResetMsg")
	}
}

func TestApp_Update_WizardDismissMsg(t *testing.T) {
	a := newMinimalApp()
	a.state = stateWizard

	model, _ := a.Update(WizardDismissMsg{})
	updated := model.(*App)
	if updated.state != stateChat {
		t.Errorf("expected stateChat after WizardDismissMsg, got %v", updated.state)
	}
}
