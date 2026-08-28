package agent

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"github.com/scrypster/huginn/internal/tools"
	"github.com/scrypster/huginn/internal/tools/validation"
)

// SyntaxValidationHook is G1 — edit-time syntax validation — wired as a
// PreToolUse/PostToolUse pair (dogfooding G10's hook chain, the pattern
// future groundings should follow).
//
// Design: validate the would-be content BEFORE the write happens (computed
// the same way OnBeforeWrite's diff preview is, via previewWrite), not
// write-then-revert. That makes "block" mode trivial to get right: PreToolUse
// simply denies, so tool.Execute never runs and the file is provably
// unchanged. "warn" mode needs the write to go through but still wants the
// model to see the failure, so the pre hook stashes the warning (keyed by a
// deterministic hash of the call's args) and the paired post hook attaches
// it to the real result once the write has actually happened.
type SyntaxValidationHook struct {
	// ModeFn returns the current syntax_validation mode (from
	// .huginn/workspace.json). Consulted on every call so a config change
	// mid-session takes effect without restarting the loop. Nil defaults to
	// ModeBlock (validation.NormalizeMode("")).
	ModeFn func() string

	pending sync.Map // argsKey -> warning string, written by Pre, drained by Post
}

// NewSyntaxValidationHook builds a G1 hook. modeFn is typically
// func() string { return workspaceManager.Config().SyntaxValidation } —
// pass nil to always use the default ("block").
func NewSyntaxValidationHook(modeFn func() string) *SyntaxValidationHook {
	return &SyntaxValidationHook{ModeFn: modeFn}
}

// RegisterSyntaxValidation attaches G1 to reg as a matched PreToolUse +
// PostToolUse pair. This is the intended call site for wiring the hook into
// a harness-owned HookRegistry (e.g. one built per session/workspace and
// threaded onto RunLoopConfig.Hooks) — the registration API future
// groundings should mirror.
func RegisterSyntaxValidation(reg *HookRegistry, modeFn func() string) {
	if reg == nil {
		return
	}
	h := NewSyntaxValidationHook(modeFn)
	reg.RegisterPreToolUse(h.Pre)
	reg.RegisterPostToolUse(h.Post)
}

// mode resolves the effective validation.Mode for this call.
func (h *SyntaxValidationHook) mode() validation.Mode {
	raw := ""
	if h != nil && h.ModeFn != nil {
		raw = h.ModeFn()
	}
	return validation.NormalizeMode(raw)
}

// Pre implements PreToolUseHook. Only write_file/edit_file are inspected;
// every other tool passes through untouched.
func (h *SyntaxValidationHook) Pre(_ context.Context, toolName string, args map[string]any) (bool, string) {
	if toolName != "write_file" && toolName != "edit_file" {
		return true, ""
	}
	mode := h.mode()
	if mode == validation.ModeOff {
		return true, ""
	}

	path, _, newContent := previewWrite(toolName, args)
	if path == "" {
		return true, "" // nothing to validate (malformed args) — let the tool itself reject it
	}

	// Empty/whitespace-only content is a legitimate scaffold, not a broken
	// file: agents routinely create an empty file with write_file and fill it
	// in a following step. Every language validator would reject "" as a
	// syntax error ("expected 'package'…" for Go, etc.), so without this
	// exemption block mode would trap that common two-step create-then-fill
	// pattern. An absent file is not a malformed one — pass it through.
	if len(bytes.TrimSpace(newContent)) == 0 {
		return true, ""
	}

	res := validation.Validate(path, newContent)
	if !res.Checked || res.OK {
		return true, "" // unsupported language, missing validator, or syntactically valid
	}

	// res.Checked && !res.OK: a real syntax failure.
	if mode == validation.ModeBlock {
		return false, fmt.Sprintf("syntax validation failed (%s): %s", res.Lang, res.Message)
	}

	// ModeWarn: allow the write through, hand the warning to Post so it can
	// annotate the real tool result once the write has happened.
	h.pending.Store(argsKey(toolName, args), fmt.Sprintf("[syntax warning: %s validator] %s", res.Lang, res.Message))
	return true, ""
}

// Post implements PostToolUseHook. Attaches any warning this hook's Pre
// stashed for the same call, then discards it (LoadAndDelete — a pending
// entry must never leak to a later, unrelated call with the same args).
func (h *SyntaxValidationHook) Post(_ context.Context, toolName string, args map[string]any, result *tools.ToolResult) {
	if result == nil || (toolName != "write_file" && toolName != "edit_file") {
		return
	}
	v, ok := h.pending.LoadAndDelete(argsKey(toolName, args))
	if !ok {
		return
	}
	warning, _ := v.(string)
	if warning == "" {
		return
	}
	if result.Output != "" {
		result.Output += "\n" + warning
	} else {
		result.Output = warning
	}
}
