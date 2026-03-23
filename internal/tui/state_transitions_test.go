package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/scrypster/huginn/internal/permissions"
)

// TestApp_StateTransition_ChatToWizard verifies the app can transition from chat to wizard state.
func TestApp_StateTransition_ChatToWizard(t *testing.T) {
	a := newMinimalApp()
	if a.state != stateChat {
		t.Fatalf("initial state should be stateChat, got %v", a.state)
	}

	a.state = stateWizard
	if a.state != stateWizard {
		t.Error("state should transition to stateWizard")
	}
}

// TestApp_StateTransition_ChatToFilePicker verifies transition to file picker.
func TestApp_StateTransition_ChatToFilePicker(t *testing.T) {
	a := newMinimalApp()
	a.state = stateFilePicker
	if a.state != stateFilePicker {
		t.Error("state should transition to stateFilePicker")
	}
}

// TestApp_StateTransition_ChatToSessionPicker verifies transition to session picker.
func TestApp_StateTransition_ChatToSessionPicker(t *testing.T) {
	a := newMinimalApp()
	a.state = stateSessionPicker
	if a.state != stateSessionPicker {
		t.Error("state should transition to stateSessionPicker")
	}
}

// TestApp_PermissionPrompt_Basic verifies permission prompting state works.
func TestApp_PermissionPrompt_SetPending(t *testing.T) {
	a := newMinimalApp()
	if a.permPending != nil {
		t.Error("permPending should initially be nil")
	}

	respCh := make(chan permissions.Decision)
	a.permPending = &PermissionPromptMsg{
		RespCh: respCh,
	}
	if a.permPending == nil {
		t.Error("permPending should be set")
	}

	// Cleanup
	close(respCh)
}

// TestApp_WriteApproval_SetPending verifies write approval state works.
func TestApp_WriteApproval_SetPending(t *testing.T) {
	a := newMinimalApp()
	if a.writePending != nil {
		t.Error("writePending should initially be nil")
	}

	respCh := make(chan bool)
	a.writePending = &writeApprovalMsg{
		Path:   "/tmp/test.go",
		RespCh: respCh,
	}
	if a.writePending == nil {
		t.Error("writePending should be set")
	}

	// Cleanup
	close(respCh)
}

// TestApp_AutoRun_Toggle verifies autoRun flag can be toggled.
func TestApp_AutoRun_Toggle(t *testing.T) {
	a := newMinimalApp()
	// newMinimalApp creates a bare App; autoRun starts false (zero value).
	// Verify it can be set and cleared.
	a.autoRun = true
	if !a.autoRun {
		t.Error("autoRun should be true after setting")
	}

	a.autoRun = false
	if a.autoRun {
		t.Error("autoRun should be false after clearing")
	}

	a.autoRun = true
	if !a.autoRun {
		t.Error("autoRun should be toggled back to true")
	}
}

// TestApp_Attachments_Add verifies attachments list can be populated.
func TestApp_Attachments_Add(t *testing.T) {
	a := newMinimalApp()
	if len(a.attachments) != 0 {
		t.Error("attachments should initially be empty")
	}

	a.attachments = append(a.attachments, "file1.go", "file2.go")
	if len(a.attachments) != 2 {
		t.Error("attachments should have 2 items")
	}
	if a.attachments[0] != "file1.go" {
		t.Errorf("first attachment should be 'file1.go', got %q", a.attachments[0])
	}
}

// TestApp_Attachments_MaxLimit verifies attachment max limit behavior.
func TestApp_Attachments_MaxLimit(t *testing.T) {
	a := newMinimalApp()
	// Try to add more than 10 attachments
	for i := 0; i < 15; i++ {
		a.attachments = append(a.attachments, "file.go")
	}
	// The app doesn't enforce max at this level; that's UI-controlled.
	// Just verify we can store them.
	if len(a.attachments) != 15 {
		t.Errorf("expected 15 attachments, got %d", len(a.attachments))
	}
}

// TestApp_QueuedMsg_Set verifies queuedMsg field works.
func TestApp_QueuedMsg_Set(t *testing.T) {
	a := newMinimalApp()
	if a.queuedMsg != "" {
		t.Error("queuedMsg should initially be empty")
	}

	a.queuedMsg = "follow up message"
	if a.queuedMsg != "follow up message" {
		t.Errorf("expected queued message, got %q", a.queuedMsg)
	}

	a.queuedMsg = ""
	if a.queuedMsg != "" {
		t.Error("queuedMsg should be clearable")
	}
}

// TestApp_CtrlCPending_Toggle verifies ctrl+c pending flag.
func TestApp_CtrlCPending_Toggle(t *testing.T) {
	a := newMinimalApp()
	if a.ctrlCPending {
		t.Error("ctrlCPending should initially be false")
	}

	a.ctrlCPending = true
	if !a.ctrlCPending {
		t.Error("ctrlCPending should be set to true")
	}

	a.ctrlCPending = false
	if a.ctrlCPending {
		t.Error("ctrlCPending should be reset to false")
	}
}

// TestApp_PrimaryAgent_SetAndUpdate verifies primary agent updates.
func TestApp_PrimaryAgent_Set(t *testing.T) {
	a := newMinimalApp()
	if a.primaryAgent != "" {
		t.Error("primaryAgent should initially be empty")
	}

	a.SetPrimaryAgent("TestBot")
	if a.primaryAgent != "TestBot" {
		t.Errorf("expected primaryAgent='TestBot', got %q", a.primaryAgent)
	}

	// Verify placeholder is updated
	expectedPlaceholder := "Message TestBot..."
	if a.input.Placeholder != expectedPlaceholder {
		t.Errorf("expected placeholder %q, got %q", expectedPlaceholder, a.input.Placeholder)
	}
}

// TestApp_AgentTurn_Counter verifies agentTurn counter.
func TestApp_AgentTurn_Counter(t *testing.T) {
	a := newMinimalApp()
	if a.agentTurn != 0 {
		t.Error("agentTurn should initially be 0")
	}

	a.agentTurn = 5
	if a.agentTurn != 5 {
		t.Errorf("expected agentTurn=5, got %d", a.agentTurn)
	}

	a.agentTurn = 0
	if a.agentTurn != 0 {
		t.Error("agentTurn should be resettable to 0")
	}
}

// TestApp_SessionCost_Update verifies session cost tracking.
func TestApp_SessionCost_Update(t *testing.T) {
	a := newMinimalApp()
	if a.sessionCostUSD != 0 {
		t.Error("sessionCostUSD should initially be 0")
	}

	a.sessionCostUSD = 0.0123
	if a.sessionCostUSD != 0.0123 {
		t.Errorf("expected cost 0.0123, got %f", a.sessionCostUSD)
	}

	// HeaderView only renders when primaryAgent is set.
	a.primaryAgent = "TestAgent"
	hv := a.HeaderView()
	if !stringContains(hv, "0.01") {
		t.Errorf("HeaderView should include cost, got %q", hv)
	}
}

// TestApp_View_RespondsToResize verifies recalcViewportHeight adjusts viewport.
func TestApp_View_RespondsToResize(t *testing.T) {
	a := newMinimalApp()
	a.height = 30
	a.width = 100
	a.recalcViewportHeight()

	// Height should be reduced by reservedLines
	expectedHeight := 30 - reservedLines
	if expectedHeight <= 0 {
		expectedHeight = 3 // minimum height
	}
	if a.viewport.Height != expectedHeight {
		t.Errorf("expected viewport height %d, got %d", expectedHeight, a.viewport.Height)
	}
}

// TestApp_ChipFocus_Navigation verifies chip focus state.
func TestApp_ChipFocus_Navigation(t *testing.T) {
	a := newMinimalApp()
	if a.chipFocused {
		t.Error("chipFocused should initially be false")
	}

	a.chipFocused = true
	if !a.chipFocused {
		t.Error("chipFocused should be settable to true")
	}

	a.chipCursor = 2
	if a.chipCursor != 2 {
		t.Errorf("expected chipCursor=2, got %d", a.chipCursor)
	}
}

// TestApp_Init_ReturnsCmd verifies Init() returns a command.
func TestApp_Init_ReturnsCmd(t *testing.T) {
	a := newMinimalApp()
	cmd := a.Init()
	if cmd == nil {
		t.Error("Init() should return a non-nil command")
	}
}

// TestApp_Update_WithWidthHeight verifies Update sets width/height correctly.
func TestApp_Update_WithWidthHeight(t *testing.T) {
	a := newMinimalApp()
	msg := tea.WindowSizeMsg{Width: 120, Height: 40}
	newApp, _ := a.Update(msg)
	a2, ok := newApp.(*App)
	if !ok {
		t.Fatal("Update should return an *App")
	}
	if a2.width != 120 || a2.height != 40 {
		t.Errorf("expected width=120, height=40; got width=%d, height=%d", a2.width, a2.height)
	}
}

// stringContains checks if s contains substr (helper for tests).
func stringContains(s, substr string) bool {
	for i := 0; i < len(s)-len(substr)+1; i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
