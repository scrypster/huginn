package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/scrypster/huginn/internal/agents"
	"github.com/scrypster/huginn/internal/streaming"
)

func TestUpdate_ShellResultMsg_SuccessWithOutput(t *testing.T) {
	app := newTestApp()
	msg := shellResultMsg{cmd: "ls -la", output: "file1.txt\nfile2.txt\n"}
	m, _ := app.Update(msg)
	a := m.(*App)
	if a.state != stateChat {
		t.Errorf("expected stateChat after shell result, got %d", a.state)
	}
	if a.shellContext == "" {
		t.Error("expected shellContext to be populated")
	}
	if !strings.Contains(a.shellContext, "ls -la") {
		t.Error("expected shellContext to contain command")
	}
}

func TestUpdate_ShellResultMsg_ErrorOnly(t *testing.T) {
	app := newTestApp()
	msg := shellResultMsg{cmd: "bad-cmd", err: fmt.Errorf("command failed")}
	m, _ := app.Update(msg)
	a := m.(*App)
	if a.shellContext != "" {
		t.Error("expected empty shellContext on error")
	}
	found := false
	for _, line := range a.chat.history {
		if line.role == "error" && strings.Contains(line.content, "command failed") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected error line in history")
	}
}

func TestUpdate_ShellResultMsg_EmptyOutput(t *testing.T) {
	app := newTestApp()
	msg := shellResultMsg{cmd: "true", output: ""}
	m, _ := app.Update(msg)
	a := m.(*App)
	found := false
	for _, line := range a.chat.history {
		if strings.Contains(line.content, "(no output)") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected '(no output)' in history for empty shell output")
	}
}

func TestUpdate_DelegationStartMsg_WithRegistry(t *testing.T) {
	app := newTestApp()
	reg := agents.NewRegistry()
	reg.Register(&agents.Agent{Name: "Steve", Color: "#3FB950"})
	app.agentReg = reg

	msg := delegationStartMsg{From: "Chris", To: "Steve", Question: "How to refactor?"}
	m, _ := app.Update(msg)
	a := m.(*App)
	if a.delegationAgent != "Steve" {
		t.Errorf("expected delegationAgent=Steve, got %q", a.delegationAgent)
	}
	if a.delegationAgentColor != "#3FB950" {
		t.Errorf("expected color from registry, got %q", a.delegationAgentColor)
	}
}

func TestUpdate_DelegationStartMsg_NilRegistry(t *testing.T) {
	app := newTestApp()
	app.agentReg = nil
	msg := delegationStartMsg{From: "Chris", To: "Steve", Question: "Question?"}
	m, _ := app.Update(msg)
	a := m.(*App)
	if a.delegationAgent != "Steve" {
		t.Errorf("expected delegationAgent=Steve, got %q", a.delegationAgent)
	}
	// Should use default accent color
	if a.delegationAgentColor == "" {
		t.Error("expected non-empty default delegation color")
	}
}

func TestUpdate_DelegationStartMsg_LongQuestion(t *testing.T) {
	app := newTestApp()
	longQ := strings.Repeat("x", 100)
	msg := delegationStartMsg{From: "Chris", To: "Steve", Question: longQ}
	app.Update(msg)
	found := false
	for _, line := range app.chat.history {
		if line.role == "delegation-start" && strings.Contains(line.content, "…") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected truncated question in delegation-start history")
	}
}

func TestUpdate_DelegationTokenMsg_Accumulates(t *testing.T) {
	app := newTestApp()
	app.Update(delegationTokenMsg{Agent: "Steve", Token: "hello "})
	m, _ := app.Update(delegationTokenMsg{Agent: "Steve", Token: "world"})
	a := m.(*App)
	if a.delegationBuf != "hello world" {
		t.Errorf("expected 'hello world', got %q", a.delegationBuf)
	}
}

func TestUpdate_DelegationDoneMsg_UsesBuf(t *testing.T) {
	app := newTestApp()
	app.delegationBuf = "the answer"
	msg := delegationDoneMsg{From: "Chris", To: "Steve", Answer: "fallback"}
	m, _ := app.Update(msg)
	a := m.(*App)
	if a.delegationBuf != "" {
		t.Error("expected delegationBuf cleared")
	}
	if a.delegationAgent != "" {
		t.Error("expected delegationAgent cleared")
	}
	found := false
	for _, line := range a.chat.history {
		if line.role == "delegation-done" && strings.Contains(line.content, "the answer") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected delegation answer from buf in history")
	}
}

func TestUpdate_DelegationDoneMsg_EmptyBufUsesFallback(t *testing.T) {
	app := newTestApp()
	app.delegationBuf = ""
	msg := delegationDoneMsg{From: "Chris", To: "Steve", Answer: "fallback answer"}
	app.Update(msg)
	found := false
	for _, line := range app.chat.history {
		if line.role == "delegation-done" && strings.Contains(line.content, "fallback answer") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected fallback answer in history when buf is empty")
	}
}

func TestUpdate_FilePickerConfirmMsg_NoDuplicates(t *testing.T) {
	app := newTestApp()
	app.attachments = []string{"main.go"}
	msg := FilePickerConfirmMsg{Paths: []string{"main.go", "new.go"}}
	m, _ := app.Update(msg)
	a := m.(*App)
	if len(a.attachments) != 2 {
		t.Errorf("expected 2 attachments (no dup), got %d: %v", len(a.attachments), a.attachments)
	}
}

func TestUpdate_FilePickerConfirmMsg_MaxAttachments(t *testing.T) {
	app := newTestApp()
	// Pre-fill with 9 attachments
	for i := 0; i < 9; i++ {
		app.attachments = append(app.attachments, fmt.Sprintf("file%d.go", i))
	}
	msg := FilePickerConfirmMsg{Paths: []string{"a.go", "b.go", "c.go"}}
	m, _ := app.Update(msg)
	a := m.(*App)
	if len(a.attachments) > 10 {
		t.Errorf("expected max 10 attachments, got %d", len(a.attachments))
	}
}

func TestUpdate_BackspaceEmptyInput_WithAttachments_EntersChipFocus(t *testing.T) {
	app := newTestApp()
	app.attachments = []string{"file.go", "util.go"}
	app.input.SetValue("")
	msg := tea.KeyMsg{Type: tea.KeyBackspace}
	m, _ := app.Update(msg)
	a := m.(*App)
	if !a.chipFocused {
		t.Error("expected chipFocused=true after backspace on empty input with attachments")
	}
	if a.chipCursor != 1 {
		t.Errorf("expected chipCursor at last attachment (1), got %d", a.chipCursor)
	}
}

func TestUpdate_BackspaceInChipArea_EmptyAttachments(t *testing.T) {
	app := newTestApp()
	app.chipFocused = true
	app.attachments = nil
	msg := tea.KeyMsg{Type: tea.KeyBackspace}
	m, _ := app.Update(msg)
	a := m.(*App)
	if a.chipFocused {
		t.Error("expected chipFocused=false when no attachments")
	}
}

func TestUpdate_CtrlC_WhileStreaming_Cancels(t *testing.T) {
	app := newTestApp()
	cancelled := false
	app.chat.cancelStream = func() { cancelled = true }
	app.state = stateStreaming
	ch := make(chan tea.Msg, 1)
	app.chat.eventCh = ch

	msg := tea.KeyMsg{Type: tea.KeyCtrlC}
	m, _ := app.Update(msg)
	a := m.(*App)
	if !cancelled {
		t.Error("expected cancelStream to be called")
	}
	if a.state != stateChat {
		t.Errorf("expected stateChat after cancel, got %d", a.state)
	}
	if a.chat.cancelStream != nil {
		t.Error("expected cancelStream to be nil after cancel")
	}
}

func TestUpdate_CtrlC_WhileStreaming_TokenCh(t *testing.T) {
	app := newTestApp()
	cancelled := false
	app.chat.cancelStream = func() { cancelled = true }
	app.state = stateStreaming
	r := streaming.NewRunner()
	app.chat.runner = r
	r.Start(context.Background(), func(emit func(string)) error {
		emit("token")
		return nil
	})
	app.chat.eventCh = nil

	msg := tea.KeyMsg{Type: tea.KeyCtrlC}
	m, _ := app.Update(msg)
	a := m.(*App)
	if !cancelled {
		t.Error("expected cancelStream to be called")
	}
	if a.chat.runner != nil {
		t.Error("expected runner to be nil after cancel")
	}
	_ = a
}

func TestUpdate_CtrlC_SecondPress_Quits(t *testing.T) {
	app := newTestApp()
	app.ctrlCPending = true
	app.input.SetValue("")
	msg := tea.KeyMsg{Type: tea.KeyCtrlC}
	_, cmd := app.Update(msg)
	if cmd == nil {
		t.Error("expected non-nil quit command on second ctrl+c")
	}
}

func TestUpdate_ToolDoneMsg_WithDuration(t *testing.T) {
	app := newTestApp()
	ch := make(chan tea.Msg, 10)
	app.chat.eventCh = ch

	msg := toolDoneMsg{
		name:       "bash",
		isError:    false,
		preview:    "ok",
		duration:   500_000_000, // 500ms
		fullOutput: "full output",
	}
	app.Update(msg)
	last := app.chat.history[len(app.chat.history)-1]
	if last.role != "tool-done" {
		t.Errorf("expected tool-done, got %q", last.role)
	}
	if last.duration == "" {
		t.Error("expected non-empty duration")
	}
}

func TestUpdate_ToolDoneMsg_ErrorPath(t *testing.T) {
	app := newTestApp()
	ch := make(chan tea.Msg, 10)
	app.chat.eventCh = ch

	msg := toolDoneMsg{
		name:    "bash",
		isError: true,
		preview: "permission denied",
	}
	app.Update(msg)
	last := app.chat.history[len(app.chat.history)-1]
	if last.role != "tool-error" {
		t.Errorf("expected tool-error, got %q", last.role)
	}
	if !strings.Contains(last.content, "permission denied") {
		t.Errorf("expected error content, got %q", last.content)
	}
}
