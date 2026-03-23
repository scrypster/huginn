package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/scrypster/huginn/internal/diffview"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/swarm"
)

// ============================================================
// agentwizard.View — all step branches
// ============================================================

func TestAgentWizardView_StepName(t *testing.T) {
	m := newAgentWizardModel()
	// default step is wizStepName
	result := m.View()
	if result == "" {
		t.Error("expected non-empty View for wizStepName")
	}
	if !strings.Contains(result, "Create new agent") {
		t.Errorf("expected 'Create new agent' in view, got: %q", result)
	}
}

func TestAgentWizardView_StepName_WithError(t *testing.T) {
	m := newAgentWizardModel()
	m.nameErr = "Name already taken"
	result := m.View()
	if !strings.Contains(result, "Name already taken") {
		t.Errorf("expected error message in view, got: %q", result)
	}
}

func TestAgentWizardView_StepModel(t *testing.T) {
	m := newAgentWizardModel()
	m.step = wizStepModel
	m.nameInput = "myagent"
	m.availModels = []string{"claude-3", "gpt-4", "llama3"}
	m.modelCursor = 1
	result := m.View()
	if result == "" {
		t.Error("expected non-empty View for wizStepModel")
	}
	if !strings.Contains(result, "claude-3") {
		t.Errorf("expected model names in view, got: %q", result)
	}
}

func TestAgentWizardView_StepBackstory(t *testing.T) {
	m := newAgentWizardModel()
	m.step = wizStepBackstory
	m.nameInput = "myagent"
	m.selectedModel = "gpt-4"
	result := m.View()
	if result == "" {
		t.Error("expected non-empty View for wizStepBackstory")
	}
	if !strings.Contains(result, "Backstory") {
		t.Errorf("expected 'Backstory' in view, got: %q", result)
	}
}

func TestAgentWizardView_StepConfirm(t *testing.T) {
	m := newAgentWizardModel()
	m.step = wizStepConfirm
	m.nameInput = "myagent"
	m.selectedModel = "gpt-4"
	m.backstory = "You are a helpful coding assistant."
	result := m.View()
	if result == "" {
		t.Error("expected non-empty View for wizStepConfirm")
	}
	if !strings.Contains(result, "myagent") {
		t.Errorf("expected agent name in confirm view, got: %q", result)
	}
}

func TestAgentWizardView_StepConfirm_LongBackstory(t *testing.T) {
	m := newAgentWizardModel()
	m.step = wizStepConfirm
	m.nameInput = "agent"
	m.selectedModel = "gpt-4"
	// Backstory longer than 80 chars should be truncated.
	m.backstory = strings.Repeat("x", 100)
	result := m.View()
	if !strings.Contains(result, "...") {
		t.Errorf("expected truncation indicator '...' for long backstory, got: %q", result)
	}
}

// ============================================================
// ctrlCResetCmd — executing the cmd to verify it returns correct type
// ============================================================

func TestCtrlCResetCmd_NonNilResult(t *testing.T) {
	cmd := ctrlCResetCmd()
	if cmd == nil {
		t.Error("ctrlCResetCmd() must return non-nil Cmd")
	}
}

// ============================================================
// readSwarmEvent — closed channel path
// ============================================================

func TestReadSwarmEvent_ClosedChannel_ReturnsDoneMsg(t *testing.T) {
	ch := make(chan swarm.SwarmEvent)
	close(ch)
	cmd := readSwarmEvent(ch)
	if cmd == nil {
		t.Fatal("expected non-nil cmd for closed channel")
	}
	msg := cmd()
	_, ok := msg.(swarmDoneMsg)
	if !ok {
		t.Errorf("expected swarmDoneMsg from closed channel, got %T", msg)
	}
}


// ============================================================
// handleParallelCommand — nil orch, empty args, no tasks paths
// ============================================================

func TestHandleParallelCommand_NilOrch_ReturnsNilAddsMessage(t *testing.T) {
	a := newTestApp()
	a.orch = nil
	cmd := a.handleParallelCommand("task1 | task2")
	if cmd != nil {
		t.Error("expected nil cmd when orch is nil")
	}
	if len(a.chat.history) == 0 {
		t.Error("expected system message when orch is nil")
	}
}

func TestHandleParallelCommand_EmptyArgs_ReturnsNil(t *testing.T) {
	a := newTestApp()
	a.orch = nil // prevents actual execution
	cmd := a.handleParallelCommand("")
	if cmd != nil {
		t.Error("expected nil cmd for empty args")
	}
	if len(a.chat.history) == 0 {
		t.Error("expected usage message for empty args")
	}
}

func TestHandleParallelCommand_OnlyPipes_NoTasks(t *testing.T) {
	a := newTestApp()
	a.orch = nil
	// Force nil orch path first, then test with whitespace-only tasks.
	cmd := a.handleParallelCommand("   |   |   ")
	if cmd != nil {
		t.Error("expected nil cmd when orch is nil")
	}
}

// ============================================================
// Update — streaming branches (token/thinking counts)
// ============================================================

func TestApp_Update_ThinkingTokenMsg_WithEventCh(t *testing.T) {
	a := newTestApp()
	ch := make(chan tea.Msg, 1)
	errCh := make(chan error, 1)
	a.chat.eventCh = ch
	a.chat.errCh = errCh
	ch <- tokenMsg("next-token")

	model, cmd := a.Update(thinkingTokenMsg("a thought"))
	updated := model.(*App)
	_ = updated
	if cmd == nil {
		t.Error("expected non-nil cmd from thinkingTokenMsg with eventCh")
	}
}

func TestApp_Update_ThinkingTokenMsg_WithRunner(t *testing.T) {
	a := newTestApp()
	// No eventCh, no runner — returns nil cmd.
	model, cmd := a.Update(thinkingTokenMsg("thought"))
	updated := model.(*App)
	_ = updated
	_ = cmd // may or may not be nil depending on runner state
}

// ============================================================
// Update — wsEventMsg handler branches
// ============================================================

func TestApp_Update_WsEventMsg_PrimaryAgentChanged(t *testing.T) {
	a := newTestApp()
	model, _ := a.Update(wsEventMsg{
		Type:    "primary_agent_changed",
		Payload: map[string]any{"agent": "CodeBot"},
	})
	updated := model.(*App)
	if updated.primaryAgent != "CodeBot" {
		t.Errorf("expected primaryAgent='CodeBot', got %q", updated.primaryAgent)
	}
}

func TestApp_Update_WsEventMsg_CostUpdate(t *testing.T) {
	a := newTestApp()
	model, _ := a.Update(wsEventMsg{
		Type:    "cost_update",
		Payload: map[string]any{"total": float64(1.23)},
	})
	updated := model.(*App)
	if updated.sessionCostUSD != 1.23 {
		t.Errorf("expected sessionCostUSD=1.23, got %f", updated.sessionCostUSD)
	}
}

func TestApp_Update_WsEventMsg_UnknownType_NoOp(t *testing.T) {
	a := newTestApp()
	model, cmd := a.Update(wsEventMsg{Type: "unknown_event", Payload: nil})
	updated := model.(*App)
	_ = updated
	if cmd != nil {
		t.Error("expected nil cmd for unknown wsEventMsg type")
	}
}

// ============================================================
// Update — delegation messages
// ============================================================

func TestApp_Update_DelegationStartMsg(t *testing.T) {
	a := newTestApp()
	model, _ := a.Update(delegationStartMsg{
		From:     "Alice",
		To:       "Bob",
		Question: "What is the plan?",
	})
	updated := model.(*App)
	if updated.delegationAgent != "Bob" {
		t.Errorf("expected delegationAgent='Bob', got %q", updated.delegationAgent)
	}
	if len(updated.chat.history) == 0 {
		t.Error("expected delegation-start history entry")
	}
	last := updated.chat.history[len(updated.chat.history)-1]
	if last.role != "delegation-start" {
		t.Errorf("expected role 'delegation-start', got %q", last.role)
	}
}

func TestApp_Update_DelegationTokenMsg_AccumulatesToken(t *testing.T) {
	a := newTestApp()
	a.delegationBuf = "hello"
	model, _ := a.Update(delegationTokenMsg{Agent: "Bob", Token: " world"})
	updated := model.(*App)
	if updated.delegationBuf != "hello world" {
		t.Errorf("expected delegationBuf='hello world', got %q", updated.delegationBuf)
	}
}

func TestApp_Update_DelegationDoneMsg_AddsHistoryLine(t *testing.T) {
	a := newTestApp()
	a.delegationBuf = "final answer"
	model, _ := a.Update(delegationDoneMsg{From: "Alice", To: "Bob", Answer: "ignored because buf has content"})
	updated := model.(*App)
	if len(updated.chat.history) == 0 {
		t.Fatal("expected delegation-done history entry")
	}
	last := updated.chat.history[len(updated.chat.history)-1]
	if last.role != "delegation-done" {
		t.Errorf("expected role 'delegation-done', got %q", last.role)
	}
	if updated.delegationBuf != "" {
		t.Error("expected delegationBuf cleared after done")
	}
}

func TestApp_Update_DelegationDoneMsg_UsesAnswerWhenBufEmpty(t *testing.T) {
	a := newTestApp()
	a.delegationBuf = "" // buf is empty
	model, _ := a.Update(delegationDoneMsg{From: "Alice", To: "Bob", Answer: "direct answer"})
	updated := model.(*App)
	if len(updated.chat.history) == 0 {
		t.Fatal("expected delegation-done history entry")
	}
	last := updated.chat.history[len(updated.chat.history)-1]
	if !strings.Contains(last.content, "direct answer") {
		t.Errorf("expected 'direct answer' in content, got %q", last.content)
	}
}

// ============================================================
// Update — swarmEventMsg branches
// ============================================================

func TestApp_Update_SwarmEventMsg_Ready(t *testing.T) {
	a := newTestApp()
	a.width = 80
	a.height = 24
	specs := []swarm.SwarmTaskSpec{
		{ID: "a1", Name: "Alpha", Color: "#ff0000"},
	}
	model, _ := a.Update(swarmEventMsg{event: swarm.SwarmEvent{
		Type:    swarm.EventSwarmReady,
		Payload: specs,
	}})
	updated := model.(*App)
	if updated.swarmView == nil {
		t.Error("expected swarmView initialized after EventSwarmReady")
	}
}

func TestApp_Update_SwarmEventMsg_Token(t *testing.T) {
	a := newTestApp()
	a.width = 80
	a.height = 24
	a.swarmView = NewSwarmViewModel(80, 24)
	a.swarmView.AddAgent("a1", "Alpha", "#ff0000")
	model, _ := a.Update(swarmEventMsg{event: swarm.SwarmEvent{
		Type:    swarm.EventToken,
		AgentID: "a1",
		Payload: "hello",
	}})
	updated := model.(*App)
	_ = updated
}

func TestApp_Update_SwarmEventMsg_ToolStart(t *testing.T) {
	a := newTestApp()
	a.width = 80
	a.swarmView = NewSwarmViewModel(80, 24)
	a.swarmView.AddAgent("a1", "Alpha", "#ff0000")
	model, _ := a.Update(swarmEventMsg{event: swarm.SwarmEvent{
		Type:    swarm.EventToolStart,
		AgentID: "a1",
		Payload: "bash_exec",
	}})
	updated := model.(*App)
	_ = updated
}

func TestApp_Update_SwarmEventMsg_StatusChange(t *testing.T) {
	a := newTestApp()
	a.width = 80
	a.swarmView = NewSwarmViewModel(80, 24)
	a.swarmView.AddAgent("a1", "Alpha", "#ff0000")
	model, _ := a.Update(swarmEventMsg{event: swarm.SwarmEvent{
		Type:    swarm.EventStatusChange,
		AgentID: "a1",
		Payload: swarm.StatusThinking,
	}})
	updated := model.(*App)
	_ = updated
}

func TestApp_Update_SwarmEventMsg_Complete(t *testing.T) {
	a := newTestApp()
	a.width = 80
	a.swarmView = NewSwarmViewModel(80, 24)
	a.swarmView.AddAgent("a1", "Alpha", "#ff0000")
	model, _ := a.Update(swarmEventMsg{event: swarm.SwarmEvent{
		Type:    swarm.EventComplete,
		AgentID: "a1",
	}})
	updated := model.(*App)
	_ = updated
}

func TestApp_Update_SwarmEventMsg_Error(t *testing.T) {
	a := newTestApp()
	a.width = 80
	a.swarmView = NewSwarmViewModel(80, 24)
	a.swarmView.AddAgent("a1", "Alpha", "#ff0000")
	model, _ := a.Update(swarmEventMsg{event: swarm.SwarmEvent{
		Type:    swarm.EventError,
		AgentID: "a1",
	}})
	updated := model.(*App)
	_ = updated
}

func TestApp_Update_SwarmDoneMsg_WithOutput(t *testing.T) {
	a := newTestApp()
	a.state = stateSwarm
	model, _ := a.Update(swarmDoneMsg{output: "swarm complete"})
	updated := model.(*App)
	if updated.state != stateChat {
		t.Errorf("expected stateChat after swarmDoneMsg, got %v", updated.state)
	}
	if len(updated.chat.history) == 0 {
		t.Fatal("expected history entry after swarm done with output")
	}
}

func TestApp_Update_SwarmDoneMsg_NoOutput(t *testing.T) {
	a := newTestApp()
	a.state = stateSwarm
	model, _ := a.Update(swarmDoneMsg{output: ""})
	updated := model.(*App)
	if updated.state != stateChat {
		t.Errorf("expected stateChat after swarmDoneMsg, got %v", updated.state)
	}
}

// ============================================================
// Update — AgentWizardDoneMsg and AgentWizardCancelMsg
// ============================================================

func TestApp_Update_AgentWizardCancelMsg(t *testing.T) {
	a := newTestApp()
	a.state = stateAgentWizard
	model, _ := a.Update(AgentWizardCancelMsg{})
	updated := model.(*App)
	if updated.state != stateChat {
		t.Errorf("expected stateChat after AgentWizardCancelMsg, got %v", updated.state)
	}
}

// ============================================================
// Update — SessionPickerMsg
// ============================================================

func TestApp_Update_SessionPickerMsg_NoStore(t *testing.T) {
	a := newTestApp()
	a.sessionStore = nil
	// SessionPickerMsg triggers resumeSession which returns a nil msg when store is nil.
	_, cmd := a.Update(SessionPickerMsg{ID: "sess-123"})
	if cmd == nil {
		t.Error("expected non-nil cmd from SessionPickerMsg (resumeSession cmd)")
	}
}

func TestApp_Update_SessionPickerDismissMsg(t *testing.T) {
	a := newTestApp()
	a.state = stateSessionPicker
	model, _ := a.Update(SessionPickerDismissMsg{})
	updated := model.(*App)
	if updated.state != stateChat {
		t.Errorf("expected stateChat after SessionPickerDismissMsg, got %v", updated.state)
	}
}

// ============================================================
// Update — PermissionPromptMsg autoRun=true path
// ============================================================

func TestApp_Update_PermissionPromptMsg_AutoRun_SendsAllow(t *testing.T) {
	a := newTestApp()
	a.autoRun = true

	respCh := make(chan permissions.Decision, 1)
	_, _ = a.Update(PermissionPromptMsg{
		RespCh: respCh,
	})
	// When autoRun is true, the decision should be sent immediately.
	select {
	case d := <-respCh:
		if d != permissions.Allow {
			t.Errorf("expected permissions.Allow when autoRun=true, got %v", d)
		}
	default:
		t.Error("expected decision to be sent synchronously when autoRun=true")
	}
}

// ============================================================
// Update — WizardDismissMsg (variant — app_extra_test.go has the main test)
// ============================================================

func TestApp_Update_WizardDismissMsg_ClearsInput(t *testing.T) {
	a := newTestApp()
	a.state = stateWizard
	a.input.SetValue("/plan")
	model, _ := a.Update(WizardDismissMsg{})
	updated := model.(*App)
	if updated.state != stateChat {
		t.Errorf("expected stateChat after WizardDismissMsg, got %v", updated.state)
	}
}

// ============================================================
// View — additional branches
// ============================================================

func TestApp_View_SessionPicker(t *testing.T) {
	a := newTestApp()
	a.state = stateSessionPicker
	result := a.View()
	if result == "" {
		t.Error("View() should be non-empty in stateSessionPicker")
	}
}

func TestApp_View_Swarm(t *testing.T) {
	a := newTestApp()
	a.state = stateSwarm
	a.swarmView = NewSwarmViewModel(80, 24)
	result := a.View()
	if result == "" {
		t.Error("View() should be non-empty in stateSwarm")
	}
}

func TestApp_View_Approval(t *testing.T) {
	a := newTestApp()
	a.state = stateChat
	result := a.View()
	if result == "" {
		t.Error("View() should be non-empty in stateChat")
	}
}

func TestApp_View_PermAwait(t *testing.T) {
	a := newTestApp()
	a.state = statePermAwait
	result := a.View()
	if result == "" {
		t.Error("View() should be non-empty in statePermAwait")
	}
}

func TestApp_View_Streaming(t *testing.T) {
	a := newTestApp()
	a.state = stateStreaming
	a.chat.tokenCount = 42
	result := a.View()
	if result == "" {
		t.Error("View() should be non-empty in stateStreaming")
	}
}

func TestApp_View_WithAttachments(t *testing.T) {
	a := newTestApp()
	a.state = stateChat
	a.attachments = []string{"main.go", "app.go"}
	result := a.View()
	if result == "" {
		t.Error("View() should be non-empty with attachments")
	}
}

func TestApp_View_WithQueuedMsg(t *testing.T) {
	a := newTestApp()
	a.state = stateChat
	a.queuedMsg = "pending message"
	result := a.View()
	if result == "" {
		t.Error("View() should be non-empty with queued message")
	}
}

// ============================================================
// renderFooter — statePermAwait branch
// ============================================================

func TestApp_RenderFooter_StateStreaming(t *testing.T) {
	a := newTestApp()
	a.state = stateStreaming
	a.version = "1.0.0"
	result := a.renderFooter()
	if result == "" {
		t.Error("renderFooter in stateStreaming should be non-empty")
	}
}

func TestApp_RenderFooter_StatePermAwait(t *testing.T) {
	a := newTestApp()
	a.state = statePermAwait
	a.version = "1.0.0"
	result := a.renderFooter()
	if result == "" {
		t.Error("renderFooter in statePermAwait should be non-empty")
	}
}

func TestApp_RenderFooter_StateStreaming_WithAgentTurn(t *testing.T) {
	a := newTestApp()
	a.state = stateStreaming
	a.activeModel = "claude-3"
	a.agentTurn = 3
	a.version = "1.0.0"
	result := a.renderFooter()
	if result == "" {
		t.Error("renderFooter in stateStreaming with agentTurn should be non-empty")
	}
}

// ============================================================
// diffview_model.go — ViewBatch remaining branch
// ============================================================

func TestDiffReviewModel_ViewBatch_AllDecisions(t *testing.T) {
	diffs := []diffview.FileDiff{
		{Path: "a.go", UnifiedDiff: "--- a.go\n+++ b.go\n@@ -1 +1 @@\n-old\n+new"},
		{Path: "b.go", UnifiedDiff: "--- b.go\n+++ c.go\n@@ -1 +1 @@\n-a\n+b"},
	}
	m := NewDiffReviewModel(diffs, 80)
	// Accept a.go, skip b.go.
	m = m.HandleKey("a")
	m = m.HandleKey("s")

	result := m.ViewBatch()
	if result == "" {
		t.Error("expected non-empty ViewBatch after decisions")
	}
}

// ============================================================
// renderMarkdown — nil glamourRenderer path (variant for coverage)
// ============================================================

func TestRenderMarkdown_NilRenderer_PassesThrough(t *testing.T) {
	a := newTestApp()
	a.glamourRenderer = nil
	result := a.renderMarkdown("# Hello World")
	if result != "# Hello World" {
		t.Errorf("expected passthrough for nil renderer, got %q", result)
	}
}

// ============================================================
// handleSlashCommand — swarm branch
// ============================================================

func TestHandleSlashCommand_Swarm_WithNoSwarmEvents(t *testing.T) {
	a := newTestApp()
	a.swarmEvents = nil
	_ = a.handleSlashCommand(SlashCommand{Name: "swarm"})
	// Should not transition to stateSwarm when events are nil.
	if a.state == stateSwarm {
		t.Error("expected state not to transition to stateSwarm when swarmEvents is nil")
	}
}

func TestHandleSlashCommand_Swarm_WithSwarmEvents(t *testing.T) {
	a := newTestApp()
	ch := make(chan swarm.SwarmEvent)
	a.swarmEvents = ch
	_ = a.handleSlashCommand(SlashCommand{Name: "swarm"})
	if a.state != stateSwarm {
		t.Errorf("expected stateSwarm when swarmEvents is set, got %v", a.state)
	}
}

// ============================================================
// handleSlashCommand — save and title branches
// ============================================================

func TestHandleSlashCommand_Save_NilStore(t *testing.T) {
	a := newTestApp()
	a.sessionStore = nil
	_ = a.handleSlashCommand(SlashCommand{Name: "save"})
	// Should not panic, even with nil store.
}

func TestHandleSlashCommand_Title_EmptyArgs(t *testing.T) {
	a := newTestApp()
	a.activeSession = nil
	_ = a.handleSlashCommand(SlashCommand{Name: "title", Args: ""})
	// Should not panic.
}

// ============================================================
// handleSlashCommand — code, reason, plan-mode branches
// ============================================================

func TestHandleSlashCommand_Reason_AddsSystemRole(t *testing.T) {
	a := newTestApp()
	_ = a.handleSlashCommand(SlashCommand{Name: "reason"})
	if len(a.chat.history) == 0 {
		t.Fatal("expected system history entry for /reason")
	}
	last := a.chat.history[len(a.chat.history)-1]
	if last.role != "system" {
		t.Errorf("expected system role, got %q", last.role)
	}
}

func TestHandleSlashCommand_Reason_AddsSystemLine(t *testing.T) {
	a := newTestApp()
	_ = a.handleSlashCommand(SlashCommand{Name: "reason"})
	if len(a.chat.history) == 0 {
		t.Fatal("expected system history entry for /reason")
	}
}

func TestHandleSlashCommand_SwitchModel_AddsSystemLine(t *testing.T) {
	a := newTestApp()
	_ = a.handleSlashCommand(SlashCommand{Name: "switch-model"})
	if len(a.chat.history) == 0 {
		t.Fatal("expected system history entry for /switch-model")
	}
}

func TestHandleSlashCommand_Iterate_AddsSystemLine(t *testing.T) {
	a := newTestApp()
	_ = a.handleSlashCommand(SlashCommand{Name: "iterate"})
	if len(a.chat.history) == 0 {
		t.Fatal("expected system history entry for /iterate")
	}
}

// ============================================================
// Update — ctrlCPending second press when streaming path (ctrl+c cancels)
// (distinct from TestApp_Update_CtrlC_SecondPress_Quits in final_coverage_test.go)
// ============================================================

func TestApp_Update_CtrlC_DoublePress_EmitsQuit(t *testing.T) {
	a := newTestApp()
	a.ctrlCPending = true
	a.input.SetValue("") // empty input

	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	// Should return tea.Quit — cmd won't be nil.
	if cmd == nil {
		t.Error("expected non-nil cmd (tea.Quit) on second ctrl+c")
	}
}

// ============================================================
// Update — "?" key shows shortcuts in stateChat
// ============================================================

func TestApp_Update_QuestionMark_ShowsShortcuts(t *testing.T) {
	a := newTestApp()
	a.state = stateChat
	model, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	updated := model.(*App)
	if len(updated.chat.history) == 0 {
		t.Fatal("expected help shortcuts added to history after '?'")
	}
}

// ============================================================
// Update — ctrl+p cycles primary agent (no registry = no-op)
// ============================================================

func TestApp_Update_CtrlP_NoRegistry_NoOp(t *testing.T) {
	a := newTestApp()
	a.agentReg = nil
	model, cmd := a.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	updated := model.(*App)
	_ = updated
	if cmd != nil {
		t.Error("expected nil cmd when agentReg is nil")
	}
}

// ============================================================
// renderInputBox — streaming branch
// ============================================================

func TestRenderInputBox_Streaming(t *testing.T) {
	a := newTestApp()
	a.state = stateStreaming
	a.chat.tokenCount = 10
	result := a.renderInputBox()
	if result == "" {
		t.Error("expected non-empty renderInputBox in streaming state")
	}
	if !strings.Contains(result, "10") {
		t.Errorf("expected token count in streaming input box, got %q", result)
	}
}

// ============================================================
// AppendOutput on SwarmViewModel
// ============================================================

func TestSwarmViewModel_AppendOutput(t *testing.T) {
	sv := NewSwarmViewModel(80, 24)
	sv.AddAgent("a1", "Alpha", "#ff0000")
	sv.AppendOutput("a1", "hello output")
	// Should not panic.
}

// ============================================================
// formatDuration edge cases
// ============================================================

func TestFormatDuration_ExactBoundary(t *testing.T) {
	// Exactly 1 second should use "s" format.
	result := formatDuration(1 * time.Second)
	if !strings.Contains(result, "s") {
		t.Errorf("expected 's' in 1-second duration, got %q", result)
	}
	if strings.Contains(result, "ms") {
		t.Errorf("expected no 'ms' in 1-second duration, got %q", result)
	}
}
