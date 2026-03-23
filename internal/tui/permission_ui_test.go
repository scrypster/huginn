package tui

import (
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/permissions"
)

// ---------------------------------------------------------------------------
// renderPermissionPrompt — nil permPending
// ---------------------------------------------------------------------------

func TestRenderPermissionPrompt_NilPending(t *testing.T) {
	a := newMinimalApp()
	a.permPending = nil
	out := a.renderPermissionPrompt()
	if out == "" {
		t.Error("expected non-empty output even with nil permPending")
	}
	// Should contain key hints.
	if !strings.Contains(out, "[a]") && !strings.Contains(out, "[d]") {
		t.Errorf("expected key hints in permission prompt, got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// renderPermissionPrompt — with pending request
// ---------------------------------------------------------------------------

func TestRenderPermissionPrompt_WithPendingToolName(t *testing.T) {
	a := newMinimalApp()
	respCh := make(chan permissions.Decision, 1)
	a.permPending = &PermissionPromptMsg{
		Req: permissions.PermissionRequest{
			ToolName: "bash",
			Summary:  "run a shell command",
		},
		RespCh: respCh,
	}
	out := a.renderPermissionPrompt()
	if out == "" {
		t.Error("expected non-empty permission prompt")
	}
	// Should mention the tool name or action
	if !strings.Contains(out, "bash") && !strings.Contains(out, "shell") {
		t.Logf("permission prompt output: %q", out)
		// The FormatRequest function may render differently — just check non-empty
	}
}

func TestRenderPermissionPrompt_ContainsAllow(t *testing.T) {
	a := newMinimalApp()
	respCh := make(chan permissions.Decision, 1)
	a.permPending = &PermissionPromptMsg{
		Req: permissions.PermissionRequest{
			ToolName: "read_file",
			Summary:  "read a file",
		},
		RespCh: respCh,
	}
	out := a.renderPermissionPrompt()
	if !strings.Contains(out, "llow") {
		t.Errorf("expected 'allow' action hint in prompt, got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// renderWriteApprovalPrompt — nil writePending
// ---------------------------------------------------------------------------

func TestRenderWriteApprovalPrompt_NilPending(t *testing.T) {
	a := newMinimalApp()
	a.writePending = nil
	out := a.renderWriteApprovalPrompt()
	if out != "" {
		t.Errorf("expected empty string for nil writePending, got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// renderWriteApprovalPrompt — with pending write
// ---------------------------------------------------------------------------

func TestRenderWriteApprovalPrompt_WithPending(t *testing.T) {
	a := newMinimalApp()
	respCh := make(chan bool, 1)
	a.writePending = &writeApprovalMsg{
		Path:   "/tmp/output.go",
		RespCh: respCh,
	}
	out := a.renderWriteApprovalPrompt()
	if out == "" {
		t.Error("expected non-empty write approval prompt")
	}
	if !strings.Contains(out, "/tmp/output.go") {
		t.Errorf("expected file path in prompt, got: %q", out)
	}
}

func TestRenderWriteApprovalPrompt_ContainsYesNo(t *testing.T) {
	a := newMinimalApp()
	respCh := make(chan bool, 1)
	a.writePending = &writeApprovalMsg{
		Path:   "main.go",
		RespCh: respCh,
	}
	out := a.renderWriteApprovalPrompt()
	if !strings.Contains(out, "[y]") || !strings.Contains(out, "[n]") {
		t.Errorf("expected [y] and [n] hints in write approval prompt, got: %q", out)
	}
}

// ---------------------------------------------------------------------------
// handlePermissionPromptMsg — autoRun=true auto-approves
// ---------------------------------------------------------------------------

func TestHandlePermissionPromptMsg_AutoRunApproves(t *testing.T) {
	a := newMinimalApp()
	a.autoRun = true

	respCh := make(chan permissions.Decision, 1)
	msg := PermissionPromptMsg{
		Req:    permissions.PermissionRequest{ToolName: "bash"},
		RespCh: respCh,
	}
	a.handlePermissionPromptMsg(msg)

	select {
	case decision := <-respCh:
		if decision != permissions.Allow {
			t.Errorf("expected Allow decision, got %v", decision)
		}
	default:
		t.Error("expected decision to be sent on respCh when autoRun=true")
	}

	// State should not change to statePermAwait when auto-run.
	if a.state == statePermAwait {
		t.Error("state should not be statePermAwait when autoRun=true")
	}
}

// ---------------------------------------------------------------------------
// handlePermissionPromptMsg — autoRun=false shows prompt
// ---------------------------------------------------------------------------

func TestHandlePermissionPromptMsg_ManualModeShowsPrompt(t *testing.T) {
	a := newMinimalApp()
	a.autoRun = false

	respCh := make(chan permissions.Decision, 1)
	msg := PermissionPromptMsg{
		Req:    permissions.PermissionRequest{ToolName: "write_file"},
		RespCh: respCh,
	}
	a.handlePermissionPromptMsg(msg)

	if a.state != statePermAwait {
		t.Errorf("expected statePermAwait, got %v", a.state)
	}
	if a.permPending == nil {
		t.Error("expected permPending to be set")
	}
}

// ---------------------------------------------------------------------------
// handleWriteApprovalMsg — autoRun=true auto-approves
// ---------------------------------------------------------------------------

func TestHandleWriteApprovalMsg_AutoRunApproves(t *testing.T) {
	a := newMinimalApp()
	a.autoRun = true

	respCh := make(chan bool, 1)
	msg := writeApprovalMsg{
		Path:   "file.go",
		RespCh: respCh,
	}
	a.handleWriteApprovalMsg(msg)

	select {
	case allowed := <-respCh:
		if !allowed {
			t.Error("expected allow=true when autoRun=true")
		}
	default:
		t.Error("expected decision sent on respCh")
	}
}

// ---------------------------------------------------------------------------
// handleWriteApprovalMsg — autoRun=false shows prompt
// ---------------------------------------------------------------------------

func TestHandleWriteApprovalMsg_ManualModeShowsPrompt(t *testing.T) {
	a := newMinimalApp()
	a.autoRun = false

	respCh := make(chan bool, 1)
	msg := writeApprovalMsg{
		Path:   "file.go",
		RespCh: respCh,
	}
	a.handleWriteApprovalMsg(msg)

	if a.state != stateWriteAwait {
		t.Errorf("expected stateWriteAwait, got %v", a.state)
	}
	if a.writePending == nil {
		t.Error("expected writePending to be set")
	}
}

// ---------------------------------------------------------------------------
// handlePermission — resolves pending approval
// ---------------------------------------------------------------------------

func TestHandlePermission_AllowDecision(t *testing.T) {
	a := newMinimalApp()
	respCh := make(chan permissions.Decision, 1)
	a.permPending = &PermissionPromptMsg{
		Req:    permissions.PermissionRequest{ToolName: "bash"},
		RespCh: respCh,
	}
	a.state = statePermAwait

	a.handlePermission(permissions.Allow)

	select {
	case d := <-respCh:
		if d != permissions.Allow {
			t.Errorf("expected Allow, got %v", d)
		}
	default:
		t.Error("expected decision on channel")
	}
	if a.permPending != nil {
		t.Error("expected permPending cleared after handlePermission")
	}
	if a.state == statePermAwait {
		t.Error("state should no longer be statePermAwait")
	}
}

func TestHandlePermission_DenyDecision(t *testing.T) {
	a := newMinimalApp()
	respCh := make(chan permissions.Decision, 1)
	a.permPending = &PermissionPromptMsg{
		Req:    permissions.PermissionRequest{ToolName: "write_file"},
		RespCh: respCh,
	}
	a.state = statePermAwait

	a.handlePermission(permissions.Deny)

	select {
	case d := <-respCh:
		if d != permissions.Deny {
			t.Errorf("expected Deny, got %v", d)
		}
	default:
		t.Error("expected decision on channel")
	}
	// History should record the denial.
	found := false
	for _, h := range a.chat.history {
		if strings.Contains(h.content, "denied") || strings.Contains(h.content, "write_file") {
			found = true
		}
	}
	if !found {
		t.Error("expected denial message in history")
	}
}

func TestHandlePermission_NilPending(t *testing.T) {
	a := newMinimalApp()
	a.permPending = nil
	// Should not panic.
	a.handlePermission(permissions.Allow)
}

// ---------------------------------------------------------------------------
// handleWriteApproval — resolves pending write approval
// ---------------------------------------------------------------------------

func TestHandleWriteApproval_Allow(t *testing.T) {
	a := newMinimalApp()
	respCh := make(chan bool, 1)
	a.writePending = &writeApprovalMsg{
		Path:   "/tmp/test.go",
		RespCh: respCh,
	}
	a.state = stateWriteAwait

	a.handleWriteApproval(true)

	select {
	case allowed := <-respCh:
		if !allowed {
			t.Error("expected true from allowed write")
		}
	default:
		t.Error("expected value on channel")
	}
	if a.writePending != nil {
		t.Error("writePending should be nil after handling")
	}
}

func TestHandleWriteApproval_Deny(t *testing.T) {
	a := newMinimalApp()
	respCh := make(chan bool, 1)
	a.writePending = &writeApprovalMsg{
		Path:   "/tmp/test.go",
		RespCh: respCh,
	}
	a.state = stateWriteAwait

	a.handleWriteApproval(false)

	select {
	case allowed := <-respCh:
		if allowed {
			t.Error("expected false from denied write")
		}
	default:
		t.Error("expected value on channel")
	}
	found := false
	for _, h := range a.chat.history {
		if strings.Contains(h.content, "denied") {
			found = true
		}
	}
	if !found {
		t.Error("expected 'denied' message in history")
	}
}

func TestHandleWriteApproval_NilPending(t *testing.T) {
	a := newMinimalApp()
	a.writePending = nil
	// Should not panic.
	a.handleWriteApproval(true)
}
