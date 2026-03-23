package tui

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/config"
)

// ---------------------------------------------------------------------------
// renderFooter — various widths
// ---------------------------------------------------------------------------

func TestRenderFooter_Width80(t *testing.T) {
	a := newMinimalApp()
	a.width = 80
	a.cfg = &config.Config{DefaultModel: "claude-3"}
	a.version = "1.0.0"
	out := a.renderFooter()
	if out == "" {
		t.Error("expected non-empty footer at width 80")
	}
}

func TestRenderFooter_Width120(t *testing.T) {
	a := newMinimalApp()
	a.width = 120
	a.cfg = &config.Config{DefaultModel: "claude-3"}
	a.version = "1.0.0"
	out := a.renderFooter()
	if out == "" {
		t.Error("expected non-empty footer at width 120")
	}
	if !strings.Contains(out, "huginn") {
		t.Error("expected 'huginn' brand in footer")
	}
}

func TestRenderFooter_Width200(t *testing.T) {
	a := newMinimalApp()
	a.width = 200
	a.cfg = &config.Config{DefaultModel: "gpt-4"}
	a.version = "2.0.0"
	out := a.renderFooter()
	if out == "" {
		t.Error("expected non-empty footer at width 200")
	}
}

func TestRenderFooter_Width0_NoPanic(t *testing.T) {
	a := newMinimalApp()
	a.width = 0
	a.cfg = &config.Config{DefaultModel: "test"}
	// Should not panic.
	_ = a.renderFooter()
}

func TestRenderFooter_Width1_NoPanic(t *testing.T) {
	a := newMinimalApp()
	a.width = 1
	a.cfg = &config.Config{DefaultModel: "test"}
	_ = a.renderFooter()
}

// ---------------------------------------------------------------------------
// renderFooter — model info in footer
// ---------------------------------------------------------------------------

func TestRenderFooter_ContainsModelNameInFooter(t *testing.T) {
	a := newMinimalApp()
	a.width = 120
	a.cfg = &config.Config{DefaultModel: "claude-haiku-test"}
	a.version = "0.1.0"
	out := a.renderFooter()
	if !strings.Contains(out, "claude-haiku-test") {
		t.Errorf("expected model name in footer, got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// renderFooter — primary agent in header view
// ---------------------------------------------------------------------------

func TestHeaderView_Empty(t *testing.T) {
	a := newMinimalApp()
	a.primaryAgent = ""
	out := a.HeaderView()
	if out != "" {
		t.Errorf("expected empty HeaderView when no primary agent, got: %q", out)
	}
}

func TestHeaderView_WithAgent(t *testing.T) {
	a := newMinimalApp()
	a.primaryAgent = "Coder"
	out := a.HeaderView()
	if !strings.Contains(out, "Coder") {
		t.Errorf("expected agent name in HeaderView, got: %q", out)
	}
}

func TestHeaderView_WithCost(t *testing.T) {
	a := newMinimalApp()
	a.primaryAgent = "Planner"
	a.sessionCostUSD = 0.05
	out := a.HeaderView()
	if !strings.Contains(out, "$0.05") {
		t.Errorf("expected cost in HeaderView, got: %q", out)
	}
}

func TestHeaderView_ZeroCostNotShown(t *testing.T) {
	a := newMinimalApp()
	a.primaryAgent = "Agent"
	a.sessionCostUSD = 0
	out := a.HeaderView()
	if strings.Contains(out, "$") {
		t.Errorf("expected no cost when sessionCostUSD=0, got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// refreshViewport — empty history does not panic
// ---------------------------------------------------------------------------

func TestRefreshViewport_EmptyHistory(t *testing.T) {
	a := newMinimalApp()
	a.width = 80
	// No panic expected.
	a.refreshViewport()
}

// ---------------------------------------------------------------------------
// refreshViewport — various chat line roles
// ---------------------------------------------------------------------------

func TestRefreshViewport_UserLine(t *testing.T) {
	a := newMinimalApp()
	a.width = 80
	a.height = 40
	a.recalcViewportHeight()
	a.addLine("user", "hello there")
	// Should not panic.
	a.refreshViewport()
}

func TestRefreshViewport_AssistantLine(t *testing.T) {
	a := newMinimalApp()
	a.width = 80
	a.addLine("assistant", "Hi! How can I help?")
	a.refreshViewport()
}

func TestRefreshViewport_SystemLine(t *testing.T) {
	a := newMinimalApp()
	a.width = 80
	a.addLine("system", "Indexing workspace…")
	a.refreshViewport()
}

func TestRefreshViewport_ErrorLine(t *testing.T) {
	a := newMinimalApp()
	a.width = 80
	a.addLine("error", "something went wrong")
	a.refreshViewport()
}

func TestRefreshViewport_ToolCallLine(t *testing.T) {
	a := newMinimalApp()
	a.width = 80
	a.addLine("tool-call", "bash: ls -la")
	a.refreshViewport()
}

func TestRefreshViewport_ToolDoneLine(t *testing.T) {
	a := newMinimalApp()
	a.width = 80
	a.chat.history = append(a.chat.history, chatLine{
		role:    "tool-done",
		content: "output preview",
		duration: "1.2s",
	})
	a.refreshViewport()
}

// ---------------------------------------------------------------------------
// refreshViewport — unicode content does not panic
// ---------------------------------------------------------------------------

func TestRefreshViewport_UnicodeContent(t *testing.T) {
	a := newMinimalApp()
	a.width = 80
	a.addLine("user", "日本語テスト 🎉 αβγ")
	a.addLine("assistant", "こんにちは — 你好 — مرحبا")
	// Should not panic or cause index out of range.
	a.refreshViewport()
}

func TestRefreshViewport_UnicodeNarrowWidth(t *testing.T) {
	a := newMinimalApp()
	a.width = 20
	a.addLine("user", strings.Repeat("日", 200))
	a.refreshViewport()
}

// ---------------------------------------------------------------------------
// recalcViewportHeight — various states
// ---------------------------------------------------------------------------

func TestRecalcViewportHeight_DefaultState(t *testing.T) {
	a := newMinimalApp()
	a.width = 120
	a.height = 40
	a.recalcViewportHeight()
	if a.viewport.Height < 3 {
		t.Errorf("expected viewport height >= 3, got %d", a.viewport.Height)
	}
}

func TestRecalcViewportHeight_MinimumHeight(t *testing.T) {
	a := newMinimalApp()
	a.width = 80
	a.height = 1 // very small terminal
	a.recalcViewportHeight()
	if a.viewport.Height < 3 {
		t.Errorf("expected minimum 3 lines, got %d", a.viewport.Height)
	}
}

func TestRecalcViewportHeight_WithQueuedMsg(t *testing.T) {
	a := newMinimalApp()
	a.width = 80
	a.height = 30
	a.queuedMsg = "follow-up question"
	a.recalcViewportHeight()
	// Should subtract extra 3 lines for follow-up box.
	if a.viewport.Height < 3 {
		t.Errorf("expected viewport height >= 3, got %d", a.viewport.Height)
	}
}

func TestRecalcViewportHeight_WithAttachments(t *testing.T) {
	a := newMinimalApp()
	a.width = 80
	a.height = 30
	a.attachments = []string{"file1.go", "file2.go"}
	a.recalcViewportHeight()
	if a.viewport.Height < 3 {
		t.Errorf("expected viewport height >= 3, got %d", a.viewport.Height)
	}
}

// ---------------------------------------------------------------------------
// View — width=0 returns loading string
// ---------------------------------------------------------------------------

func TestView_ZeroWidthReturnsLoading(t *testing.T) {
	a := newMinimalApp()
	a.width = 0
	out := a.View()
	if !strings.Contains(out, "Loading") {
		t.Errorf("expected 'Loading' for width=0, got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// renderInputBox — various states
// ---------------------------------------------------------------------------

func TestRenderInputBox_Width120(t *testing.T) {
	a := newMinimalApp()
	a.width = 120
	out := a.renderInputBox()
	if out == "" {
		t.Error("expected non-empty input box at width 120")
	}
}

func TestRenderInputBox_NarrowWidth(t *testing.T) {
	a := newMinimalApp()
	a.width = 10
	// Should not panic.
	_ = a.renderInputBox()
}

// ---------------------------------------------------------------------------
// renderFollowUpBox — with queued message
// ---------------------------------------------------------------------------

func TestRenderFollowUpBox_WithQueuedMsgContent(t *testing.T) {
	a := newMinimalApp()
	a.width = 80
	a.queuedMsg = "my follow-up message"
	out := a.renderFollowUpBox()
	if !strings.Contains(out, "my follow-up message") {
		t.Errorf("expected queued message in follow-up box, got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// welcomeMessage — renders without panic
// ---------------------------------------------------------------------------

func TestWelcomeMessage_NonEmptyContent(t *testing.T) {
	out := welcomeMessage()
	if out == "" {
		t.Error("expected non-empty welcome message")
	}
	if !strings.Contains(out, "HUGINN") {
		t.Error("expected 'HUGINN' in welcome message")
	}
}

func TestWelcomeMessageWithAgent_ContainsAgent(t *testing.T) {
	out := welcomeMessageWithAgent("Coder")
	if !strings.Contains(out, "Coder") {
		t.Errorf("expected agent name in welcome message, got: %q", out)
	}
	if !strings.Contains(out, "HUGINN") {
		t.Error("expected 'HUGINN' in agent welcome message")
	}
}
