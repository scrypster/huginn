package tui

// state_machine_spec_test.go — Behavior specs for App state machine transitions.
//
// Run with: go test -race ./internal/tui/...
//
// These tests verify the state machine invariants documented in app_state.go:
// - Illegal state transitions don't panic (nil-guard coverage)
// - statePermAwait with nil permPending renders gracefully
// - stateWriteAwait with nil writePending doesn't panic in View()
// - handlePermission / handleWriteApproval with nil pending are no-ops
// - streamDoneMsg received in non-streaming state doesn't corrupt state
// - Entering stateStreaming and receiving streamDoneMsg returns to stateChat

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/scrypster/huginn/internal/config"
	"github.com/scrypster/huginn/internal/permissions"
)

func newStateMachineTestApp() *App {
	ti := textinput.New()
	ti.Width = 74
	vp := viewport.New(80, 20)
	return &App{
		state:    stateChat,
		input:    ti,
		viewport: vp,
		width:    80,
		height:   24,
		cfg:      &config.Config{DefaultModel: "test-model"},
	}
}

// TestAppState_PermAwait_NilPermPending_ViewDoesNotPanic verifies that View()
// does not panic when state is statePermAwait but permPending is nil.
//
// This closes the race where the permission message is consumed before View()
// renders — the nil guard in renderPermissionPrompt() must handle this case.
func TestAppState_PermAwait_NilPermPending_ViewDoesNotPanic(t *testing.T) {
	a := newStateMachineTestApp()
	a.state = statePermAwait
	a.permPending = nil // explicitly nil

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("View() panicked with nil permPending in statePermAwait: %v", r)
		}
	}()
	_ = a.View()
}

// TestAppState_WriteAwait_NilWritePending_ViewDoesNotPanic verifies that View()
// does not panic when state is stateWriteAwait but writePending is nil.
func TestAppState_WriteAwait_NilWritePending_ViewDoesNotPanic(t *testing.T) {
	a := newStateMachineTestApp()
	a.state = stateWriteAwait
	a.writePending = nil

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("View() panicked with nil writePending in stateWriteAwait: %v", r)
		}
	}()
	_ = a.View()
}

// TestAppState_HandlePermission_NilPending_ReturnsNil verifies that
// handlePermission is a safe no-op when permPending is nil.
//
// This matches the guard documented at permission_ui.go:56.
func TestAppState_HandlePermission_NilPending_ReturnsNil(t *testing.T) {
	a := newStateMachineTestApp()
	a.state = statePermAwait
	a.permPending = nil

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("handlePermission panicked with nil permPending: %v", r)
		}
	}()

	cmd := a.handlePermission(permissions.Allow)
	if cmd != nil {
		t.Error("handlePermission with nil permPending should return nil cmd")
	}
	// State must not have been corrupted.
	if a.state != statePermAwait {
		t.Errorf("state changed to %v, expected statePermAwait to be unchanged", a.state)
	}
}

// TestAppState_HandleWriteApproval_NilPending_ReturnsNil verifies that
// handleWriteApproval is a safe no-op when writePending is nil.
func TestAppState_HandleWriteApproval_NilPending_ReturnsNil(t *testing.T) {
	a := newStateMachineTestApp()
	a.state = stateWriteAwait
	a.writePending = nil

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("handleWriteApproval panicked with nil writePending: %v", r)
		}
	}()

	cmd := a.handleWriteApproval(true)
	if cmd != nil {
		t.Error("handleWriteApproval with nil writePending should return nil cmd")
	}
}

// TestAppState_StreamDone_AlwaysTransitionsToChat documents that streamDoneMsg
// always resets state to stateChat, regardless of the current state.
//
// This is the designed behavior: streamDoneMsg is a cleanup message that
// unconditionally resets the chat screen to idle. It passes through the
// non-chat-screen guard in Update() (line 403 in app.go) so it always fires.
func TestAppState_StreamDone_AlwaysTransitionsToChat(t *testing.T) {
	for _, initial := range []struct {
		name  string
		state appState
	}{
		{"stateWizard", stateWizard},
		{"stateStreaming", stateStreaming},
		{"stateChat", stateChat},
	} {
		t.Run(initial.name, func(t *testing.T) {
			a := newStateMachineTestApp()
			a.state = initial.state

			model, _ := a.Update(streamDoneMsg{})
			updated := model.(*App)

			if updated.state != stateChat {
				t.Errorf("streamDoneMsg from %v: got state %v, want stateChat", initial.name, updated.state)
			}
		})
	}
}


// TestAppState_AllStates_ViewDoesNotPanic verifies that View() does not panic
// for any valid appState value.
func TestAppState_AllStates_ViewDoesNotPanic(t *testing.T) {
	states := []struct {
		name  string
		state appState
	}{
		{"stateChat", stateChat},
		{"stateWizard", stateWizard},
		{"stateStreaming", stateStreaming},
		{"statePermAwait", statePermAwait},
		{"stateWriteAwait", stateWriteAwait},
		{"stateSessionPicker", stateSessionPicker},
		{"stateSwarm", stateSwarm},
		{"stateAgentWizard", stateAgentWizard},
	}

	for _, tc := range states {
		t.Run(tc.name, func(t *testing.T) {
			a := newStateMachineTestApp()
			a.state = tc.state
			// All pointer fields are nil; View must not dereference them without guards.
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("View() panicked in state %v: %v", tc.name, r)
				}
			}()
			_ = a.View()
		})
	}
}
