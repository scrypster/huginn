package tui

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// newTestAppForWriteApproval creates a minimal App for write-approval tests.
func newTestAppForWriteApproval(autoRun bool) *App {
	a := newTestApp()
	atom := &atomic.Bool{}
	atom.Store(autoRun)
	a.autoRunAtom = atom
	a.autoRun = autoRun
	return a
}

// TestWriteApprovalMsg_AutoRunAllows verifies that when autoRun=true,
// writeApprovalMsg immediately sends true on RespCh without entering stateWriteAwait.
func TestWriteApprovalMsg_AutoRunAllows(t *testing.T) {
	a := newTestAppForWriteApproval(true)
	respCh := make(chan bool, 1)
	msg := writeApprovalMsg{
		Path:       "/tmp/test.go",
		OldContent: []byte("old"),
		NewContent: []byte("new"),
		RespCh:     respCh,
	}

	m, _ := a.Update(msg)
	got := m.(*App)

	if got.state == stateWriteAwait {
		t.Errorf("expected state NOT to be stateWriteAwait when autoRun=true, got stateWriteAwait")
	}
	if got.writePending != nil {
		t.Error("writePending should be nil when autoRun=true")
	}
	select {
	case allow := <-respCh:
		if !allow {
			t.Errorf("expected true (allow) on RespCh when autoRun=true, got false")
		}
	default:
		t.Error("RespCh should have received true immediately when autoRun=true")
	}
}

// TestWriteApprovalMsg_ManualDenyBlocks verifies that when autoRun=false,
// writeApprovalMsg puts the App in stateWriteAwait and sets writePending.
func TestWriteApprovalMsg_ManualDenyBlocks(t *testing.T) {
	a := newTestAppForWriteApproval(false)
	respCh := make(chan bool, 1)
	msg := writeApprovalMsg{
		Path:       "/src/app.go",
		OldContent: []byte("package main"),
		NewContent: []byte("package main\n// edited"),
		RespCh:     respCh,
	}

	m, _ := a.Update(msg)
	got := m.(*App)

	if got.state != stateWriteAwait {
		t.Errorf("expected stateWriteAwait when autoRun=false, got %d", got.state)
	}
	if got.writePending == nil {
		t.Fatal("writePending should be non-nil when autoRun=false")
	}
	if got.writePending.Path != "/src/app.go" {
		t.Errorf("expected writePending.Path='/src/app.go', got %q", got.writePending.Path)
	}
	// RespCh should NOT have received anything yet.
	select {
	case <-respCh:
		t.Error("RespCh should not have received anything yet (waiting for user input)")
	default:
		// expected: nothing received yet
	}
}

// TestHandleWriteApproval_YesSendsTrue verifies that pressing 'y' in stateWriteAwait
// sends true on RespCh and transitions state to stateStreaming.
func TestHandleWriteApproval_YesSendsTrue(t *testing.T) {
	a := newTestAppForWriteApproval(false)
	a.state = stateWriteAwait
	respCh := make(chan bool, 1)
	a.writePending = &writeApprovalMsg{
		Path:       "/tmp/output.txt",
		OldContent: nil,
		NewContent: []byte("hello"),
		RespCh:     respCh,
	}

	m, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	got := m.(*App)

	if got.state != stateStreaming {
		t.Errorf("expected stateStreaming after 'y', got %d", got.state)
	}
	if got.writePending != nil {
		t.Error("writePending should be nil after approval")
	}
	select {
	case allow := <-respCh:
		if !allow {
			t.Errorf("expected true (allow) after pressing 'y', got false")
		}
	default:
		t.Error("RespCh should have received true after pressing 'y'")
	}
}

// TestHandleWriteApproval_NoSendsFalse verifies that pressing 'n' in stateWriteAwait
// sends false on RespCh and transitions state to stateStreaming.
func TestHandleWriteApproval_NoSendsFalse(t *testing.T) {
	a := newTestAppForWriteApproval(false)
	a.state = stateWriteAwait
	respCh := make(chan bool, 1)
	a.writePending = &writeApprovalMsg{
		Path:       "/etc/hosts",
		OldContent: []byte("127.0.0.1 localhost"),
		NewContent: []byte("127.0.0.1 localhost\n# added"),
		RespCh:     respCh,
	}

	m, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	got := m.(*App)

	if got.state != stateStreaming {
		t.Errorf("expected stateStreaming after 'n', got %d", got.state)
	}
	if got.writePending != nil {
		t.Error("writePending should be nil after denial")
	}
	select {
	case allow := <-respCh:
		if allow {
			t.Errorf("expected false (deny) after pressing 'n', got true")
		}
	default:
		t.Error("RespCh should have received false after pressing 'n'")
	}
}

// TestHandleWriteApproval_NilSafe verifies that handleWriteApproval with nil
// writePending does not panic.
func TestHandleWriteApproval_NilSafe(t *testing.T) {
	a := newTestAppForWriteApproval(false)
	a.writePending = nil

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("handleWriteApproval panicked with nil writePending: %v", r)
		}
	}()

	cmd := a.handleWriteApproval(true)
	if cmd != nil {
		t.Error("expected nil cmd when writePending is nil")
	}
}

// TestRenderWriteApprovalPrompt_NonEmpty verifies the prompt renders a non-empty string.
func TestRenderWriteApprovalPrompt_NonEmpty(t *testing.T) {
	a := newTestApp()
	a.writePending = &writeApprovalMsg{
		Path:       "/some/file.go",
		OldContent: nil,
		NewContent: []byte("content"),
		RespCh:     make(chan bool, 1),
	}
	rendered := a.renderWriteApprovalPrompt()
	if rendered == "" {
		t.Error("renderWriteApprovalPrompt should return non-empty string")
	}
}

// TestRenderWriteApprovalPrompt_NilSafe verifies that renderWriteApprovalPrompt
// with nil writePending returns empty string without panicking.
func TestRenderWriteApprovalPrompt_NilSafe(t *testing.T) {
	a := newTestApp()
	a.writePending = nil

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("renderWriteApprovalPrompt panicked with nil writePending: %v", r)
		}
	}()

	result := a.renderWriteApprovalPrompt()
	if result != "" {
		t.Errorf("expected empty string with nil writePending, got %q", result)
	}
}

// TestCtrlC_ClearsWritePending verifies that pressing ctrl+c while a stream is
// active (cancelStream != nil) and writePending is set causes the ctrl+c handler
// to drain writePending.RespCh with false and set writePending to nil.
//
// Design note: The OnBeforeWrite closure in streamAgentChat is already unblocked
// by ctx.Done() (select { case <-respCh; case <-ctx.Done() }). This test covers
// the belt-and-suspenders TUI cleanup path: the ctrl+c handler non-blocking sends
// false on RespCh and nils writePending so the TUI state is consistent.
func TestCtrlC_ClearsWritePending(t *testing.T) {
	a := newTestAppForWriteApproval(false)
	a.state = stateWriteAwait

	respCh := make(chan bool, 1)
	a.writePending = &writeApprovalMsg{Path: "/tmp/x", RespCh: respCh}

	// Provide a real cancelStream so the ctrl+c handler takes the streaming branch.
	_, cancel := context.WithCancel(context.Background())
	a.chat.cancelStream = cancel

	m, _ := a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	got := m.(*App)

	if got.writePending != nil {
		t.Error("writePending should be nil after ctrl+c cancels the stream")
	}
	if got.state != stateChat {
		t.Errorf("expected stateChat after ctrl+c, got %d", got.state)
	}

	// RespCh should have received false within a short window.
	select {
	case v := <-respCh:
		if v {
			t.Error("expected false on RespCh after ctrl+c cancel, got true")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("RespCh did not receive false within 100ms after ctrl+c cancel")
	}
}

// TestWriteApproval_ContextCancel_UnblocksGoroutine verifies that the
// OnBeforeWrite closure design (select on respCh vs ctx.Done()) is correct:
// when the context is cancelled before a response is sent on RespCh, the
// closure returns false without blocking.
//
// This test directly exercises the select logic by simulating the two channels.
func TestWriteApproval_ContextCancel_UnblocksGoroutine(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	respCh := make(chan bool, 1)

	result := make(chan bool, 1)
	go func() {
		// Mirrors the select in the OnBeforeWrite closure.
		select {
		case allowed := <-respCh:
			result <- allowed
		case <-ctx.Done():
			result <- false
		}
	}()

	// Cancel the context — no one sends on respCh.
	cancel()

	select {
	case v := <-result:
		if v {
			t.Error("expected false when context is cancelled, got true")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("goroutine blocked for >100ms after context cancel — goroutine leak")
	}
}
