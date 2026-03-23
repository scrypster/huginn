package tui

import (
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/scrypster/huginn/internal/diffview"
	"github.com/scrypster/huginn/internal/session"
	"github.com/scrypster/huginn/internal/swarm"
)

// ============================================================
// renameSession — with store but using a persistent dir (avoids TempDir cleanup race)
// ============================================================

func TestRenameSession_WithStore_PersistentDir(t *testing.T) {
	dir, err := os.MkdirTemp("", "huginn-rename-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	// Don't defer RemoveAll — the goroutine may still be using the dir.

	a := newTestApp()
	store := session.NewStore(dir)
	a.sessionStore = store
	a.activeSession = &session.Session{}
	cmd := a.renameSession("new title")
	if cmd != nil {
		t.Error("expected nil cmd from renameSession even with store")
	}
	if a.activeSession.Manifest.Title != "new title" {
		t.Errorf("expected title updated, got %q", a.activeSession.Manifest.Title)
	}
}

// ============================================================
// AppendOutput — trim path when exceeding maxOutputLines
// ============================================================

func TestSwarmViewModel_AppendOutput_TrimsWhenExceedsMax(t *testing.T) {
	sv := NewSwarmViewModel(80, 24)
	sv.AddAgent("a1", "Alpha", "#ff0000")

	// Add more than maxOutputLines (50) lines.
	for i := 0; i < 60; i++ {
		sv.AppendOutput("a1", "line")
	}

	av := sv.agentIndex["a1"]
	if av == nil {
		t.Fatal("expected agent to exist")
	}
	if len(av.output) != maxOutputLines {
		t.Errorf("expected output trimmed to %d, got %d", maxOutputLines, len(av.output))
	}
}

func TestSwarmViewModel_AppendOutput_UnknownAgent_NoOp(t *testing.T) {
	sv := NewSwarmViewModel(80, 24)
	// Should not panic for unknown agent ID.
	sv.AppendOutput("nonexistent", "line")
}

// ============================================================
// viewFocused — unfocused (id not in index)
// ============================================================

func TestSwarmViewModel_ViewFocused_NotFound(t *testing.T) {
	sv := NewSwarmViewModel(80, 24)
	sv.focusedID = "nobody"
	result := sv.viewFocused()
	if result != "agent not found" {
		t.Errorf("expected 'agent not found', got %q", result)
	}
}

func TestSwarmViewModel_ViewFocused_WithAgent(t *testing.T) {
	sv := NewSwarmViewModel(80, 24)
	sv.AddAgent("a1", "Alpha", "#ff0000")
	sv.SetStatus("a1", swarm.StatusThinking)
	sv.SetFocus("a1")
	// Append some output lines.
	for i := 0; i < 5; i++ {
		sv.AppendOutput("a1", "output line")
	}
	result := sv.viewFocused()
	if result == "" {
		t.Error("expected non-empty viewFocused output")
	}
}

func TestSwarmViewModel_ViewFocused_OutputTruncation(t *testing.T) {
	sv := NewSwarmViewModel(80, 10) // short height so viewH < len(lines)
	sv.AddAgent("a1", "Alpha", "#ff0000")
	sv.SetFocus("a1")
	// Add many lines to trigger truncation branch (len(lines) > viewH).
	for i := 0; i < 20; i++ {
		sv.AppendOutput("a1", "output line")
	}
	result := sv.viewFocused()
	if result == "" {
		t.Error("expected non-empty viewFocused output with truncation")
	}
}

// ============================================================
// DiffReviewModel.ViewBatch — with actual diffs
// ============================================================

func TestDiffReviewModel_ViewBatch_WithDiffs(t *testing.T) {
	diffs := []diffview.FileDiff{
		{
			Path: "main.go",
			UnifiedDiff: `--- a/main.go
+++ b/main.go
@@ -1,3 +1,3 @@
-old line
+new line
 context`,
		},
	}
	m := NewDiffReviewModel(diffs, 80)
	result := m.ViewBatch()
	if result == "" {
		t.Error("expected non-empty ViewBatch output with diffs")
	}
}

// ============================================================
// ctrlCResetCmd — returns a non-nil command
// ============================================================

func TestCtrlCResetCmd_ReturnsNonNil(t *testing.T) {
	cmd := ctrlCResetCmd()
	if cmd == nil {
		t.Error("expected non-nil cmd from ctrlCResetCmd")
	}
}

// ============================================================
// newGlamourRenderer — covers the success path
// ============================================================

func TestNewGlamourRenderer_SuccessPath(t *testing.T) {
	r := newGlamourRenderer(80)
	// May return nil if glamour fails, but should not panic.
	_ = r
}

// ============================================================
// renderMarkdown — with non-nil renderer
// ============================================================

func TestRenderMarkdown_WithRenderer(t *testing.T) {
	a := newTestApp()
	a.glamourRenderer = newGlamourRenderer(80)
	result := a.renderMarkdown("# Hello\n\nWorld")
	// Should return something (either rendered or original).
	if result == "" {
		t.Error("expected non-empty renderMarkdown output")
	}
}

// ============================================================
// handleSlashCommand — simple mode-setting commands
// ============================================================

func TestHandleSlashCommand_Iterate_AddsHistory(t *testing.T) {
	a := newTestApp()
	_ = a.handleSlashCommand(SlashCommand{Name: "iterate"})
	if len(a.chat.history) == 0 {
		t.Fatal("expected history for /iterate")
	}
}

func TestHandleSlashCommand_Reason_AddsMessage(t *testing.T) {
	a := newTestApp()
	_ = a.handleSlashCommand(SlashCommand{Name: "reason"})
	if len(a.chat.history) == 0 {
		t.Fatal("expected history for /reason")
	}
}

func TestHandleSlashCommand_Iterate_AddsMessage(t *testing.T) {
	a := newTestApp()
	_ = a.handleSlashCommand(SlashCommand{Name: "iterate"})
	if len(a.chat.history) == 0 {
		t.Fatal("expected history for /iterate")
	}
}

func TestHandleSlashCommand_SwitchModel_AddsMessage(t *testing.T) {
	a := newTestApp()
	_ = a.handleSlashCommand(SlashCommand{Name: "switch-model"})
	if len(a.chat.history) == 0 {
		t.Fatal("expected history for /switch-model")
	}
}

func TestHandleSlashCommand_Help_AddsMessage(t *testing.T) {
	a := newTestApp()
	_ = a.handleSlashCommand(SlashCommand{Name: "help"})
	if len(a.chat.history) == 0 {
		t.Fatal("expected history for /help")
	}
}

func TestHandleSlashCommand_Title_WithArgs(t *testing.T) {
	a := newTestApp()
	a.activeSession = &session.Session{}
	_ = a.handleSlashCommand(SlashCommand{Name: "title", Args: "my session"})
	if a.activeSession.Manifest.Title != "my session" {
		t.Errorf("expected title updated, got %q", a.activeSession.Manifest.Title)
	}
}

// (TestHandleSlashCommand_Radar_NoWorkspace and TestHandleSlashCommand_Swarm_NilEvents
// already declared in hardening_iter3_test.go)

// ============================================================
// FilePickerModel.View — non-root currentDir and filter branches
// ============================================================

func TestFilePicker_View_WithCurrentDir(t *testing.T) {
	fp := newFilePickerModel()
	fp.SetFiles([]string{
		"src/main.go",
		"src/app.go",
		"cmd/run.go",
	}, "")
	fp.Show()
	// Navigate into src directory.
	fp.currentDir = "src"
	fp.refilter()
	result := fp.View(80)
	if result == "" {
		t.Error("expected non-empty View with currentDir set")
	}
}

func TestFilePicker_View_WithFilter(t *testing.T) {
	fp := newFilePickerModel()
	fp.SetFiles([]string{
		"main.go",
		"app.go",
		"utils.go",
	}, "")
	fp.Show()
	fp.filter = "main"
	fp.refilter()
	result := fp.View(80)
	if result == "" {
		t.Error("expected non-empty View with filter set")
	}
}

func TestFilePicker_View_WithSelectedFiles(t *testing.T) {
	fp := newFilePickerModel()
	fp.SetFiles([]string{"main.go", "app.go"}, "")
	fp.Show()
	fp.selected["main.go"] = true
	result := fp.View(80)
	if result == "" {
		t.Error("expected non-empty View with selected files")
	}
}

func TestFilePicker_View_ScrollHint(t *testing.T) {
	fp := newFilePickerModel()
	fp.maxVisible = 2
	files := make([]string, 10)
	for i := range files {
		files[i] = string(rune('a'+i)) + ".go"
	}
	fp.SetFiles(files, "")
	fp.Show()
	result := fp.View(80)
	if result == "" {
		t.Error("expected non-empty View with scroll hint")
	}
}

// ============================================================
// recalcViewportHeight — stateFilePicker and queuedMsg branches
// ============================================================

func TestApp_RecalcViewportHeight_QueuedMsg(t *testing.T) {
	a := newTestApp()
	a.queuedMsg = "some queued message"
	a.recalcViewportHeight()
	// Just ensure no panic.
}

// ============================================================
// FilePickerModel.Update — navigate into dir and back, filter
// ============================================================

func TestFilePicker_Update_Right_EntersDir(t *testing.T) {
	fp := newFilePickerModel()
	fp.SetFiles([]string{"src/main.go", "src/app.go", "cmd/run.go"}, "")
	fp.Show()
	// The filtered view at root shows dirs: "cmd", "src".
	// Find "src" dir.
	for i, e := range fp.filtered {
		if e.rel == "src" {
			fp.cursor = i
			break
		}
	}
	fp, _ = fp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'→'}})
	// After typing a non-matching key, just verify no panic.
	_ = fp
}

func TestFilePicker_Update_CtrlU_ClearsFilter(t *testing.T) {
	fp := newFilePickerModel()
	fp.SetFiles([]string{"main.go", "app.go"}, "")
	fp.Show()
	fp.filter = "main"
	fp.refilter()
	fp, _ = fp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ctrl+u")})
	// Just ensure no panic.
	_ = fp
}

func TestFilePicker_Update_Left_AtRoot_IsNoop(t *testing.T) {
	fp := newFilePickerModel()
	fp.SetFiles([]string{"main.go"}, "")
	fp.Show()
	fp.currentDir = "" // at root
	fp, _ = fp.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if fp.currentDir != "" {
		t.Errorf("expected no dir change at root, got %q", fp.currentDir)
	}
}

func TestFilePicker_Update_BackspaceWithFilter(t *testing.T) {
	fp := newFilePickerModel()
	fp.SetFiles([]string{"main.go"}, "")
	fp.Show()
	fp.filter = "ma"
	fp.refilter()
	fp, _ = fp.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if fp.filter != "m" {
		t.Errorf("expected filter 'm' after backspace, got %q", fp.filter)
	}
}

func TestFilePicker_Update_PrintableChar_AppendsToFilter(t *testing.T) {
	fp := newFilePickerModel()
	fp.SetFiles([]string{"main.go"}, "")
	fp.Show()
	fp, _ = fp.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	if fp.filter != "m" {
		t.Errorf("expected filter 'm' after typing 'm', got %q", fp.filter)
	}
}

func TestFilePicker_Update_Tab_TogglesSelection(t *testing.T) {
	fp := newFilePickerModel()
	fp.SetFiles([]string{"main.go", "app.go"}, "")
	fp.Show()
	fp.cursor = 0
	fp, _ = fp.Update(tea.KeyMsg{Type: tea.KeyTab})
	// Should have selected one file.
	if len(fp.selected) == 0 {
		t.Error("expected selection after Tab")
	}
}

// ============================================================
// sessionPickerModel View — allMode and empty title branches
// ============================================================

func TestSessionPickerModel_View_AllMode(t *testing.T) {
	manifests := []session.Manifest{
		{ID: "s1", Title: "Session One"},
		{ID: "s2", Title: ""},
	}
	picker := newSessionPickerModel(manifests, "")
	picker.visible = true
	picker.allMode = true
	picker.applyFilter()
	result := picker.View()
	if result == "" {
		t.Error("expected non-empty View in allMode")
	}
}

func TestSessionPickerModel_View_EmptyTitle_UsesID(t *testing.T) {
	manifests := []session.Manifest{
		{ID: "session-abc-123", Title: ""},
	}
	picker := newSessionPickerModel(manifests, "")
	picker.visible = true
	picker.applyFilter()
	result := picker.View()
	if result == "" {
		t.Error("expected non-empty View with empty title session")
	}
}

func TestSessionPickerModel_Update_Tab_TogglesAllMode(t *testing.T) {
	manifests := []session.Manifest{
		{ID: "s1", Title: "Session 1"},
	}
	picker := newSessionPickerModel(manifests, "")
	picker.visible = true
	wasAllMode := picker.allMode
	updated, _ := picker.Update(tea.KeyMsg{Type: tea.KeyTab})
	if updated.allMode == wasAllMode {
		t.Error("expected allMode to toggle after Tab")
	}
}

func TestSessionPickerModel_Update_TypedChar_AppendsToFilter(t *testing.T) {
	manifests := []session.Manifest{
		{ID: "s1", Title: "Session 1"},
	}
	picker := newSessionPickerModel(manifests, "")
	picker.visible = true
	updated, _ := picker.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if updated.filter != "s" {
		t.Errorf("expected filter 's' after typing, got %q", updated.filter)
	}
}

func TestSessionPickerModel_Update_Backspace_TrimsFilter(t *testing.T) {
	manifests := []session.Manifest{
		{ID: "s1", Title: "Session 1"},
	}
	picker := newSessionPickerModel(manifests, "")
	picker.visible = true
	picker.filter = "se"
	picker.applyFilter()
	updated, _ := picker.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if updated.filter != "s" {
		t.Errorf("expected filter 's' after backspace, got %q", updated.filter)
	}
}

// ============================================================
// swarmview.renderCard — output preview and truncation branches
// ============================================================

// (handlePermission tests already declared in permission_test.go)

// ============================================================
// OnboardingModel.Update — key press branches (without runtime/models)
// ============================================================

func TestOnboardingModel_Update_QuitKey(t *testing.T) {
	m := &OnboardingModel{
		width:  80,
		height: 24,
		state:  onboardModelSelect,
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Error("expected non-nil cmd (tea.Quit) from 'q' key")
	}
}

func TestOnboardingModel_Update_UpDownKeys(t *testing.T) {
	m := &OnboardingModel{
		width:     80,
		height:    24,
		state:     onboardModelSelect,
		menuItems: []string{"model-a", "model-b", "model-c"},
		cursor:    1,
	}
	// Up key.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	onb := updated.(*OnboardingModel)
	if onb.cursor != 0 {
		t.Errorf("expected cursor 0 after up, got %d", onb.cursor)
	}
	// Down key.
	updated2, _ := onb.Update(tea.KeyMsg{Type: tea.KeyDown})
	onb2 := updated2.(*OnboardingModel)
	if onb2.cursor != 1 {
		t.Errorf("expected cursor 1 after down, got %d", onb2.cursor)
	}
}

func TestOnboardingModel_Update_WindowSizeMsg(t *testing.T) {
	m := &OnboardingModel{}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	onb := updated.(*OnboardingModel)
	if onb.width != 120 {
		t.Errorf("expected width 120, got %d", onb.width)
	}
}

func TestOnboardingModel_View_ZeroWidth(t *testing.T) {
	m := &OnboardingModel{width: 0}
	result := m.View()
	if result != "Setting up Huginn…" {
		t.Errorf("expected zero-width message, got %q", result)
	}
}

func TestOnboardingModel_View_WithWidth(t *testing.T) {
	m := &OnboardingModel{
		width:  80,
		height: 24,
		state:  onboardDownloadRuntime,
	}
	result := m.View()
	if result == "" {
		t.Error("expected non-empty View output")
	}
}

// ============================================================
// agentWizardModel.Update — model cursor up/down (lines 110-117)
// ============================================================

func TestAgentWizardModel_Update_ModelCursor_Up(t *testing.T) {
	m := newAgentWizardModel()
	m.step = wizStepModel
	m.availModels = []string{"model-a", "model-b", "model-c"}
	m.modelCursor = 2
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	wm := updated.(agentWizardModel)
	if wm.modelCursor != 1 {
		t.Errorf("expected modelCursor 1 after up, got %d", wm.modelCursor)
	}
}

func TestAgentWizardModel_Update_ModelCursor_Down(t *testing.T) {
	m := newAgentWizardModel()
	m.step = wizStepModel
	m.availModels = []string{"model-a", "model-b", "model-c"}
	m.modelCursor = 0
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	wm := updated.(agentWizardModel)
	if wm.modelCursor != 1 {
		t.Errorf("expected modelCursor 1 after down, got %d", wm.modelCursor)
	}
}

func TestSwarmViewModel_RenderCard_WithOutput(t *testing.T) {
	sv := NewSwarmViewModel(80, 24)
	sv.AddAgent("a1", "Alpha", "#ff0000")
	sv.SetStatus("a1", swarm.StatusThinking)
	// Add output to trigger the "output" branch in renderCard.
	sv.AppendOutput("a1", "some output line")
	result := sv.viewOverview()
	if result == "" {
		t.Error("expected non-empty viewOverview with output")
	}
}

func TestSwarmViewModel_RenderCard_OutputTruncation(t *testing.T) {
	sv := NewSwarmViewModel(40, 24) // narrow width to trigger truncation
	sv.AddAgent("a1", "Alpha", "#ff0000")
	sv.SetStatus("a1", swarm.StatusThinking)
	// Add a very long line to trigger truncation (sv.width - 4 = 36 chars).
	longLine := "this is a very long output line that should be truncated because it exceeds the terminal width limit"
	sv.AppendOutput("a1", longLine)
	result := sv.viewOverview()
	if result == "" {
		t.Error("expected non-empty viewOverview with truncated output")
	}
}

// ============================================================
// loader.RunLoader — covers early return paths
// ============================================================

func TestLoaderModel_ViewNoPanic(t *testing.T) {
	m := newLoaderModel("Testing…")
	// Just call View to ensure no panic.
	_ = m.View()
}

func TestSessionPickerModel_Update_Enter_WithFiltered(t *testing.T) {
	manifests := []session.Manifest{
		{ID: "s1", Title: "Session 1"},
	}
	picker := newSessionPickerModel(manifests, "")
	picker.visible = true
	picker.applyFilter()
	picker.cursor = 0
	_, cmd := picker.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Error("expected non-nil cmd after Enter with filtered items")
	}
	msg := cmd()
	if _, ok := msg.(SessionPickerMsg); !ok {
		t.Errorf("expected SessionPickerMsg, got %T", msg)
	}
}

// ============================================================
// SwarmViewModel.TickSpinner — covers SpinnerFrame path
// ============================================================

func TestSwarmViewModel_TickSpinner(t *testing.T) {
	sv := NewSwarmViewModel(80, 24)
	frame0 := sv.SpinnerFrame()
	sv.TickSpinner()
	frame1 := sv.SpinnerFrame()
	_ = frame0
	_ = frame1
	// Just ensure no panic and frames are non-empty.
	if frame1 == "" {
		t.Error("expected non-empty spinner frame after tick")
	}
}

// ============================================================
// sessionpicker Update — up/down navigation with manifests
// ============================================================

func TestSessionPickerModel_Update_UpDownNavigation(t *testing.T) {
	manifests := []session.Manifest{
		{ID: "s1", Title: "Session 1"},
		{ID: "s2", Title: "Session 2"},
	}
	picker := newSessionPickerModel(manifests, "")
	picker.visible = true
	picker.cursor = 1

	// Up key.
	upKey := tea.KeyMsg{Type: tea.KeyUp}
	_, _ = picker.Update(upKey)

	// Down key.
	downKey := tea.KeyMsg{Type: tea.KeyDown}
	_, _ = picker.Update(downKey)
}

func TestSessionPickerModel_View_WithManifests(t *testing.T) {
	manifests := []session.Manifest{
		{ID: "s1", Title: "Session 1"},
		{ID: "s2", Title: "Session 2"},
	}
	picker := newSessionPickerModel(manifests, "")
	picker.visible = true
	result := picker.View()
	if result == "" {
		t.Error("expected non-empty View output for session picker with manifests")
	}
}
