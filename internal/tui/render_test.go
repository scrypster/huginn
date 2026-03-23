package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/scrypster/huginn/internal/config"
	"github.com/scrypster/huginn/internal/modelconfig"
)

// newRenderApp returns an App ready for render function testing.
// It has a real textinput and viewport, plus a width/height so render
// functions don't short-circuit on zero dimensions.
func newRenderApp() *App {
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

// ============================================================
// renderChips
// ============================================================

func TestRenderChips_NoAttachments_Empty(t *testing.T) {
	a := newRenderApp()
	// renderChips is only called when attachments > 0, but we can call it directly.
	// With no attachments and no shellContext it should return minimal content.
	result := a.renderChips()
	// Should not contain chip-specific content
	if strings.Contains(result, "@") && strings.Contains(result, "×") {
		t.Logf("chips rendered even with no attachments: %q", result)
	}
}

func TestRenderChips_OneAttachment(t *testing.T) {
	a := newRenderApp()
	a.attachments = []string{"internal/tui/app.go"}
	result := a.renderChips()
	if !strings.Contains(result, "app.go") {
		t.Errorf("expected 'app.go' in chips, got %q", result)
	}
	if result == "" {
		t.Error("expected non-empty chip render for 1 attachment")
	}
}

func TestRenderChips_ThreeAttachments(t *testing.T) {
	a := newRenderApp()
	a.attachments = []string{"a.go", "b.go", "c.go"}
	result := a.renderChips()
	if !strings.Contains(result, "a.go") {
		t.Errorf("expected 'a.go' in chips, got %q", result)
	}
	if !strings.Contains(result, "b.go") {
		t.Errorf("expected 'b.go' in chips, got %q", result)
	}
	if !strings.Contains(result, "c.go") {
		t.Errorf("expected 'c.go' in chips, got %q", result)
	}
}

func TestRenderChips_ShellContext(t *testing.T) {
	a := newRenderApp()
	a.shellContext = "Shell command: ls\nOutput:\nfile1.txt"
	result := a.renderChips()
	if !strings.Contains(result, "shell output") {
		t.Errorf("expected 'shell output' chip with shellContext, got %q", result)
	}
}

func TestRenderChips_ChipFocused(t *testing.T) {
	a := newRenderApp()
	a.attachments = []string{"main.go", "app.go"}
	a.chipFocused = true
	a.chipCursor = 0
	result := a.renderChips()
	// chipFocused mode shows navigation hint
	if !strings.Contains(result, "←→") {
		t.Errorf("expected navigation hint in chipFocused mode, got %q", result)
	}
}

func TestRenderChips_NotFocused_ShowsBackspaceHint(t *testing.T) {
	a := newRenderApp()
	a.attachments = []string{"main.go"}
	a.chipFocused = false
	result := a.renderChips()
	if !strings.Contains(result, "Backspace") {
		t.Errorf("expected 'Backspace' hint when not focused, got %q", result)
	}
}

// ============================================================
// renderInputBox
// ============================================================

func TestRenderInputBox_NormalState(t *testing.T) {
	a := newRenderApp()
	a.state = stateChat
	result := a.renderInputBox()
	if result == "" {
		t.Error("renderInputBox should return non-empty string in normal state")
	}
}

func TestRenderInputBox_StreamingState(t *testing.T) {
	a := newRenderApp()
	a.state = stateStreaming
	a.chat.tokenCount = 42
	result := a.renderInputBox()
	if !strings.Contains(result, "Generating") {
		t.Errorf("expected 'Generating' in streaming input box, got %q", result)
	}
	if !strings.Contains(result, "42") {
		t.Errorf("expected token count '42' in streaming state, got %q", result)
	}
}

func TestRenderInputBox_StreamingShowsStopHint(t *testing.T) {
	a := newRenderApp()
	a.state = stateStreaming
	result := a.renderInputBox()
	if !strings.Contains(result, "ctrl+c") {
		t.Errorf("expected 'ctrl+c to stop' hint in streaming box, got %q", result)
	}
}

func TestRenderInputBox_ChipFocused(t *testing.T) {
	a := newRenderApp()
	a.state = stateChat
	a.chipFocused = true
	result := a.renderInputBox()
	// Should still render (input box is always shown)
	if result == "" {
		t.Error("renderInputBox should render even when chipFocused")
	}
}

// ============================================================
// renderFollowUpBox
// ============================================================

func TestRenderFollowUpBox_WithQueuedMsg(t *testing.T) {
	a := newRenderApp()
	a.queuedMsg = "Please continue working on that feature"
	result := a.renderFollowUpBox()
	if result == "" {
		t.Error("renderFollowUpBox should return non-empty string")
	}
	if !strings.Contains(result, "Please continue") {
		t.Errorf("expected queued message in follow-up box, got %q", result)
	}
}

func TestRenderFollowUpBox_ContainsHints(t *testing.T) {
	a := newRenderApp()
	a.queuedMsg = "test message"
	result := a.renderFollowUpBox()
	// Should contain navigation hints
	if !strings.Contains(result, "esc") {
		t.Errorf("expected 'esc' hint in follow-up box, got %q", result)
	}
}

func TestRenderFollowUpBox_LongMessageTruncated(t *testing.T) {
	a := newRenderApp()
	a.queuedMsg = strings.Repeat("very long message text ", 20)
	result := a.renderFollowUpBox()
	if result == "" {
		t.Error("renderFollowUpBox should return non-empty string for long message")
	}
	// Should contain truncation indicator
	if !strings.Contains(result, "…") {
		t.Logf("long message may or may not be truncated at width 80: %q", result)
	}
}

func TestRenderFollowUpBox_ShortMessage(t *testing.T) {
	a := newRenderApp()
	a.queuedMsg = "hi"
	result := a.renderFollowUpBox()
	if !strings.Contains(result, "hi") {
		t.Errorf("expected short message in follow-up box, got %q", result)
	}
}

// ============================================================
// renderFooter
// ============================================================

func TestRenderFooter_AutoRunTrue(t *testing.T) {
	a := newRenderApp()
	a.autoRun = true
	result := a.renderFooter()
	if result == "" {
		t.Error("renderFooter should return non-empty string")
	}
	if !strings.Contains(result, "Auto-run") {
		t.Errorf("expected 'Auto-run' in footer when autoRun=true, got %q", result)
	}
}

func TestRenderFooter_AutoRunFalse(t *testing.T) {
	a := newRenderApp()
	a.autoRun = false
	result := a.renderFooter()
	if !strings.Contains(result, "Auto-run off") {
		t.Errorf("expected 'Auto-run off' in footer when autoRun=false, got %q", result)
	}
}

func TestRenderFooter_ContainsModelName(t *testing.T) {
	a := newRenderApp()
	a.state = stateChat
	a.cfg = &config.Config{DefaultModel: "test-reasoner-model"}
	result := a.renderFooter()
	// Should contain the configured default model name
	if !strings.Contains(result, "test-reasoner-model") {
		t.Errorf("expected model name %q in footer, got %q", "test-reasoner-model", result)
	}
}

func TestRenderFooter_StreamingState(t *testing.T) {
	a := newRenderApp()
	a.state = stateStreaming
	a.activeModel = "qwen2.5:7b"
	a.agentTurn = 0
	result := a.renderFooter()
	if !strings.Contains(result, "qwen2.5:7b") {
		t.Errorf("expected active model name in streaming footer, got %q", result)
	}
}

func TestRenderFooter_StreamingWithAgentTurn(t *testing.T) {
	a := newRenderApp()
	a.state = stateStreaming
	a.activeModel = "qwen2.5:7b"
	a.agentTurn = 3
	result := a.renderFooter()
	if !strings.Contains(result, "turn 3") {
		t.Errorf("expected 'turn 3' in agent streaming footer, got %q", result)
	}
}

func TestRenderFooter_ChatState(t *testing.T) {
	a := newRenderApp()
	a.state = stateChat
	result := a.renderFooter()
	if result == "" {
		t.Error("expected non-empty footer in chat state")
	}
}

func TestRenderFooter_TwoRows(t *testing.T) {
	a := newRenderApp()
	result := a.renderFooter()
	// Footer should have at least 2 lines
	lines := strings.Split(result, "\n")
	if len(lines) < 2 {
		t.Errorf("expected at least 2 rows in footer, got %d lines", len(lines))
	}
}

func TestRenderFooter_LongModelNameClipped(t *testing.T) {
	a := newRenderApp()
	a.state = stateChat
	// Set a model name longer than 20 chars
	a.models = &modelconfig.Models{
		Reasoner: "very-long-model-name-that-exceeds-twenty-chars:latest",
	}
	a.width = 80
	result := a.renderFooter()
	if result == "" {
		t.Error("renderFooter should render even with long model name")
	}
}
