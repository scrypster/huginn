package tui

import (
	"sync/atomic"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/scrypster/huginn/internal/permissions"
	"github.com/scrypster/huginn/internal/tools"
)

// newTestAppWithAutoRun returns a test app with autoRunAtom wired.
func newTestAppWithAutoRun(autoRun bool) *App {
	a := newTestApp()
	atom := &atomic.Bool{}
	atom.Store(autoRun)
	a.autoRunAtom = atom
	a.autoRun = autoRun
	return a
}

// TestPermissionPromptMsg_AutoRunOn verifies that when autoRun is true,
// PermissionPromptMsg immediately responds Allow without entering statePermAwait.
func TestPermissionPromptMsg_AutoRunOn(t *testing.T) {
	a := newTestAppWithAutoRun(true)
	respCh := make(chan permissions.Decision, 1)
	msg := PermissionPromptMsg{
		Req:    permissions.PermissionRequest{ToolName: "bash", Level: tools.PermExec},
		RespCh: respCh,
	}
	m, _ := a.Update(msg)
	got := m.(*App)

	if got.state != stateChat {
		t.Errorf("expected stateChat when autoRun=true, got %d", got.state)
	}
	if got.permPending != nil {
		t.Error("permPending should be nil when autoRun=true")
	}
	select {
	case decision := <-respCh:
		if decision != permissions.Allow {
			t.Errorf("expected Allow, got %d", decision)
		}
	default:
		t.Error("respCh should have received Allow immediately")
	}
}

// TestPermissionPromptMsg_AutoRunOff verifies that when autoRun is false,
// the TUI enters statePermAwait and stores the pending request.
func TestPermissionPromptMsg_AutoRunOff(t *testing.T) {
	a := newTestAppWithAutoRun(false)
	respCh := make(chan permissions.Decision, 1)
	msg := PermissionPromptMsg{
		Req:    permissions.PermissionRequest{ToolName: "write_file", Level: tools.PermWrite},
		RespCh: respCh,
	}
	m, _ := a.Update(msg)
	got := m.(*App)

	if got.state != statePermAwait {
		t.Errorf("expected statePermAwait when autoRun=false, got %d", got.state)
	}
	if got.permPending == nil {
		t.Fatal("permPending should be set when autoRun=false")
	}
	if got.permPending.Req.ToolName != "write_file" {
		t.Errorf("expected toolName 'write_file', got %q", got.permPending.Req.ToolName)
	}
}

// TestHandlePermission_Allow verifies that pressing 'a' in statePermAwait sends Allow.
func TestHandlePermission_Allow(t *testing.T) {
	a := newTestAppWithAutoRun(false)
	a.state = statePermAwait // TUI is in permission-prompt mode
	respCh := make(chan permissions.Decision, 1)
	a.permPending = &PermissionPromptMsg{
		Req:    permissions.PermissionRequest{ToolName: "bash"},
		RespCh: respCh,
	}

	m, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	got := m.(*App)

	if got.state != stateStreaming {
		t.Errorf("expected stateStreaming after allow, got %d", got.state)
	}
	if got.permPending != nil {
		t.Error("permPending should be nil after decision")
	}
	select {
	case decision := <-respCh:
		if decision != permissions.Allow {
			t.Errorf("expected Allow, got %d", decision)
		}
	default:
		t.Error("respCh should have received Allow")
	}
}

// TestHandlePermission_AllowAll verifies that pressing 'A' sends AllowAll.
func TestHandlePermission_AllowAll(t *testing.T) {
	a := newTestAppWithAutoRun(false)
	a.state = statePermAwait
	respCh := make(chan permissions.Decision, 1)
	a.permPending = &PermissionPromptMsg{
		Req:    permissions.PermissionRequest{ToolName: "edit_file"},
		RespCh: respCh,
	}

	m, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("A")})
	got := m.(*App)

	if got.state != stateStreaming {
		t.Errorf("expected stateStreaming after AllowAll, got %d", got.state)
	}
	select {
	case decision := <-respCh:
		if decision != permissions.AllowAll {
			t.Errorf("expected AllowAll, got %d", decision)
		}
	default:
		t.Error("respCh should have received AllowAll")
	}
}

// TestHandlePermission_Deny verifies that pressing 'd' sends Deny.
func TestHandlePermission_Deny(t *testing.T) {
	a := newTestAppWithAutoRun(false)
	a.state = statePermAwait
	respCh := make(chan permissions.Decision, 1)
	a.permPending = &PermissionPromptMsg{
		Req:    permissions.PermissionRequest{ToolName: "bash"},
		RespCh: respCh,
	}

	m, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	got := m.(*App)

	if got.state != stateStreaming {
		t.Errorf("expected stateStreaming after deny, got %d", got.state)
	}
	select {
	case decision := <-respCh:
		if decision != permissions.Deny {
			t.Errorf("expected Deny, got %d", decision)
		}
	default:
		t.Error("respCh should have received Deny")
	}
}

// TestShiftTab_SyncsAutoRunAtom verifies that toggling shift+tab updates autoRunAtom.
func TestShiftTab_SyncsAutoRunAtom(t *testing.T) {
	a := newTestAppWithAutoRun(true)

	// Toggle off
	a.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if a.autoRunAtom.Load() {
		t.Error("expected autoRunAtom=false after first shift+tab")
	}

	// Toggle back on
	a.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if !a.autoRunAtom.Load() {
		t.Error("expected autoRunAtom=true after second shift+tab")
	}
}

// TestRenderPermissionPrompt_WithPending verifies the prompt renders the tool name.
func TestRenderPermissionPrompt_WithPending(t *testing.T) {
	a := newTestApp()
	a.permPending = &PermissionPromptMsg{
		Req: permissions.PermissionRequest{
			ToolName: "run_tests",
			Level:    tools.PermExec,
		},
		RespCh: make(chan permissions.Decision, 1),
	}
	rendered := a.renderPermissionPrompt()
	if rendered == "" {
		t.Error("renderPermissionPrompt should return non-empty string")
	}
}
