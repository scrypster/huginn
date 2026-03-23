package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// newMinimalOnboarding creates an OnboardingModel without requiring a real
// runtime.Manager or models.Store. We set the fields we need directly.
func newMinimalOnboarding() *OnboardingModel {
	return &OnboardingModel{
		state:  onboardDownloadRuntime,
		width:  80,
		height: 24,
	}
}

// ============================================================
// IsDone / SelectedModel
// ============================================================

func TestOnboarding_IsDone_FalseWhenNotComplete(t *testing.T) {
	m := newMinimalOnboarding()
	m.state = onboardDownloadRuntime
	if m.IsDone() {
		t.Error("expected IsDone()=false when state is onboardDownloadRuntime")
	}
}

func TestOnboarding_IsDone_FalseModelSelect(t *testing.T) {
	m := newMinimalOnboarding()
	m.state = onboardModelSelect
	if m.IsDone() {
		t.Error("expected IsDone()=false when state is onboardModelSelect")
	}
}

func TestOnboarding_IsDone_FalseDownloadModel(t *testing.T) {
	m := newMinimalOnboarding()
	m.state = onboardDownloadModel
	if m.IsDone() {
		t.Error("expected IsDone()=false when state is onboardDownloadModel")
	}
}

func TestOnboarding_IsDone_TrueWhenDone(t *testing.T) {
	m := newMinimalOnboarding()
	m.state = onboardDone
	if !m.IsDone() {
		t.Error("expected IsDone()=true when state is onboardDone")
	}
}

func TestOnboarding_SelectedModel_EmptyInitially(t *testing.T) {
	m := newMinimalOnboarding()
	if m.SelectedModel() != "" {
		t.Errorf("expected empty SelectedModel() initially, got %q", m.SelectedModel())
	}
}

func TestOnboarding_SelectedModel_ReturnsSet(t *testing.T) {
	m := newMinimalOnboarding()
	m.selectedModel = "llama3.2:3b"
	if m.SelectedModel() != "llama3.2:3b" {
		t.Errorf("expected 'llama3.2:3b', got %q", m.SelectedModel())
	}
}

// ============================================================
// View
// ============================================================

func TestOnboarding_View_ZeroWidth_ReturnsSetupMessage(t *testing.T) {
	m := newMinimalOnboarding()
	m.width = 0
	result := m.View()
	if !strings.Contains(result, "Setting up Huginn") {
		t.Errorf("expected setup message for zero width, got %q", result)
	}
}

func TestOnboarding_View_DownloadRuntimeState(t *testing.T) {
	m := newMinimalOnboarding()
	m.state = onboardDownloadRuntime
	result := m.View()
	if !strings.Contains(result, "Downloading") {
		t.Errorf("expected 'Downloading' in runtime download view, got %q", result)
	}
}

func TestOnboarding_View_DownloadRuntimeWithProgress(t *testing.T) {
	m := newMinimalOnboarding()
	m.state = onboardDownloadRuntime
	m.runtimeDL = onboardProgressMsg{downloaded: 50, total: 100, phase: "runtime"}
	result := m.View()
	if !strings.Contains(result, "50%") {
		t.Errorf("expected percentage in download view, got %q", result)
	}
}

func TestOnboarding_View_ModelSelectState(t *testing.T) {
	m := newMinimalOnboarding()
	m.state = onboardModelSelect
	m.menuItems = []string{"llama3.2:3b", "qwen2.5:7b"}
	m.cursor = 0
	result := m.View()
	if !strings.Contains(result, "llama3.2:3b") {
		t.Errorf("expected model name in select view, got %q", result)
	}
}

func TestOnboarding_View_ModelSelectShowsAllItems(t *testing.T) {
	m := newMinimalOnboarding()
	m.state = onboardModelSelect
	m.menuItems = []string{"model-a", "model-b", "model-c"}
	m.cursor = 1
	result := m.View()
	if !strings.Contains(result, "model-a") {
		t.Errorf("expected 'model-a' in select view, got %q", result)
	}
	if !strings.Contains(result, "model-b") {
		t.Errorf("expected 'model-b' (active) in select view, got %q", result)
	}
	if !strings.Contains(result, "model-c") {
		t.Errorf("expected 'model-c' in select view, got %q", result)
	}
}

func TestOnboarding_View_DownloadModelState(t *testing.T) {
	m := newMinimalOnboarding()
	m.state = onboardDownloadModel
	m.selectedModel = "llama3.2:3b"
	result := m.View()
	if !strings.Contains(result, "llama3.2:3b") {
		t.Errorf("expected model name in download state, got %q", result)
	}
	if !strings.Contains(result, "Please wait") {
		t.Errorf("expected 'Please wait' in model download view, got %q", result)
	}
}

func TestOnboarding_View_DownloadModelWithProgress(t *testing.T) {
	m := newMinimalOnboarding()
	m.state = onboardDownloadModel
	m.selectedModel = "qwen:7b"
	m.modelDL = onboardProgressMsg{downloaded: 750, total: 1000, phase: "model"}
	result := m.View()
	if !strings.Contains(result, "75%") {
		t.Errorf("expected '75%%' in model download progress, got %q", result)
	}
}

func TestOnboarding_View_DoneState(t *testing.T) {
	m := newMinimalOnboarding()
	m.state = onboardDone
	result := m.View()
	if !strings.Contains(result, "Setup complete") {
		t.Errorf("expected 'Setup complete' in done state, got %q", result)
	}
}

func TestOnboarding_View_WithError(t *testing.T) {
	m := newMinimalOnboarding()
	m.state = onboardDownloadRuntime
	m.err = errors.New("network failure")
	result := m.View()
	if !strings.Contains(result, "Error") {
		t.Errorf("expected 'Error' when m.err is set, got %q", result)
	}
	if !strings.Contains(result, "network failure") {
		t.Errorf("expected error message in view, got %q", result)
	}
}

func TestOnboarding_View_ContainsWelcome(t *testing.T) {
	m := newMinimalOnboarding()
	result := m.View()
	if !strings.Contains(result, "Welcome") {
		t.Errorf("expected 'Welcome' in onboarding view, got %q", result)
	}
}

// ============================================================
// Update — keyboard and progress messages
// ============================================================

func TestOnboarding_Update_WindowSize(t *testing.T) {
	m := newMinimalOnboarding()
	model, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
	updated := model.(*OnboardingModel)
	if updated.width != 100 {
		t.Errorf("expected width=100, got %d", updated.width)
	}
	if updated.height != 50 {
		t.Errorf("expected height=50, got %d", updated.height)
	}
}

func TestOnboarding_Update_CtrlC_Quits(t *testing.T) {
	m := newMinimalOnboarding()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Error("expected non-nil cmd from ctrl+c")
	}
	// The cmd should be tea.Quit
	msg := cmd()
	if msg != tea.Quit() {
		t.Errorf("expected tea.Quit message, got %T", msg)
	}
}

func TestOnboarding_Update_RuntimeProgressNotDone(t *testing.T) {
	m := newMinimalOnboarding()
	ch := make(chan onboardProgressMsg, 1)
	m.runtimeCh = ch

	msg := onboardProgressMsg{downloaded: 100, total: 1000, phase: "runtime", done: false}
	model, cmd := m.Update(msg)
	updated := model.(*OnboardingModel)

	if updated.runtimeDL.downloaded != 100 {
		t.Errorf("expected runtimeDL.downloaded=100, got %d", updated.runtimeDL.downloaded)
	}
	if cmd == nil {
		t.Error("expected re-arm cmd for in-progress runtime download")
	}
}

func TestOnboarding_Update_RuntimeProgressError(t *testing.T) {
	m := newMinimalOnboarding()

	msg := onboardProgressMsg{phase: "runtime", err: errors.New("download failed")}
	model, cmd := m.Update(msg)
	updated := model.(*OnboardingModel)

	if updated.err == nil {
		t.Error("expected error to be stored on runtime download error")
	}
	if cmd != nil {
		t.Error("expected nil cmd on error")
	}
}

func TestOnboarding_Update_ModelSelectNavUp(t *testing.T) {
	m := newMinimalOnboarding()
	m.state = onboardModelSelect
	m.menuItems = []string{"a", "b", "c"}
	m.cursor = 2

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	updated := model.(*OnboardingModel)
	if updated.cursor != 1 {
		t.Errorf("expected cursor=1 after Up, got %d", updated.cursor)
	}
}

func TestOnboarding_Update_ModelSelectNavDown(t *testing.T) {
	m := newMinimalOnboarding()
	m.state = onboardModelSelect
	m.menuItems = []string{"a", "b", "c"}
	m.cursor = 0

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated := model.(*OnboardingModel)
	if updated.cursor != 1 {
		t.Errorf("expected cursor=1 after Down, got %d", updated.cursor)
	}
}

func TestOnboarding_Update_ModelSelectNavUpAtTop(t *testing.T) {
	m := newMinimalOnboarding()
	m.state = onboardModelSelect
	m.menuItems = []string{"a", "b"}
	m.cursor = 0

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	updated := model.(*OnboardingModel)
	if updated.cursor != 0 {
		t.Errorf("expected cursor to stay at 0, got %d", updated.cursor)
	}
}

func TestOnboarding_Update_ModelSelectNavDownAtBottom(t *testing.T) {
	m := newMinimalOnboarding()
	m.state = onboardModelSelect
	m.menuItems = []string{"a", "b"}
	m.cursor = 1

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated := model.(*OnboardingModel)
	if updated.cursor != 1 {
		t.Errorf("expected cursor to stay at 1, got %d", updated.cursor)
	}
}

func TestOnboarding_Update_ModelProgressError(t *testing.T) {
	m := newMinimalOnboarding()
	m.state = onboardDownloadModel

	msg := onboardProgressMsg{phase: "model", err: errors.New("model download failed")}
	model, cmd := m.Update(msg)
	updated := model.(*OnboardingModel)

	if updated.err == nil {
		t.Error("expected error to be stored on model download error")
	}
	if cmd != nil {
		t.Error("expected nil cmd on model error")
	}
}

func TestOnboarding_Update_ModelProgressNotDone(t *testing.T) {
	m := newMinimalOnboarding()
	m.state = onboardDownloadModel
	ch := make(chan onboardProgressMsg, 1)
	m.modelCh = ch

	msg := onboardProgressMsg{downloaded: 500, total: 2000, phase: "model", done: false}
	model, cmd := m.Update(msg)
	updated := model.(*OnboardingModel)

	if updated.modelDL.downloaded != 500 {
		t.Errorf("expected modelDL.downloaded=500, got %d", updated.modelDL.downloaded)
	}
	if cmd == nil {
		t.Error("expected re-arm cmd for in-progress model download")
	}
}

// ============================================================
// pollProgressCmd
// ============================================================

func TestPollProgressCmd_ReadsFromChannel(t *testing.T) {
	ch := make(chan onboardProgressMsg, 1)
	ch <- onboardProgressMsg{downloaded: 42, phase: "runtime"}

	cmd := pollProgressCmd(ch)
	msg := cmd()

	progress, ok := msg.(onboardProgressMsg)
	if !ok {
		t.Fatalf("expected onboardProgressMsg, got %T", msg)
	}
	if progress.downloaded != 42 {
		t.Errorf("expected downloaded=42, got %d", progress.downloaded)
	}
}
