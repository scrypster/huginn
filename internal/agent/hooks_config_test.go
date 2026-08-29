package agent

// hooks_config_test.go — TDD coverage for user-configurable hooks.json:
// loading, validation, glob matching, PreToolUse veto, PostToolUse
// observe-only, missing-file-is-a-noop, malformed-file-fails-loudly, and an
// audit entry per hook run.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/scrypster/huginn/internal/tools"
)

func writeHooksJSON(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func TestLoadUserHooksFile_MissingIsNotAnError(t *testing.T) {
	f, err := LoadUserHooksFile(filepath.Join(t.TempDir(), "hooks.json"))
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if f != nil {
		t.Fatalf("missing file should return nil, got %+v", f)
	}
}

func TestLoadUserHooksFile_Malformed(t *testing.T) {
	dir := t.TempDir()

	cases := map[string]string{
		"bad json":        `{not json`,
		"missing id":      `{"hooks":[{"event":"PreToolUse","match":{"tools":["*"]},"action":{"type":"command","command":"true"},"enabled":true}]}`,
		"bad event":       `{"hooks":[{"id":"a","event":"Bogus","match":{"tools":["*"]},"action":{"type":"command","command":"true"},"enabled":true}]}`,
		"empty tools":     `{"hooks":[{"id":"a","event":"PreToolUse","match":{"tools":[]},"action":{"type":"command","command":"true"},"enabled":true}]}`,
		"missing command": `{"hooks":[{"id":"a","event":"PreToolUse","match":{"tools":["*"]},"action":{"type":"command","command":""},"enabled":true}]}`,
		"bad action type": `{"hooks":[{"id":"a","event":"PreToolUse","match":{"tools":["*"]},"action":{"type":"http","command":"true"},"enabled":true}]}`,
		"duplicate id":    `{"hooks":[{"id":"a","event":"PreToolUse","match":{"tools":["*"]},"action":{"type":"command","command":"true"},"enabled":true},{"id":"a","event":"PostToolUse","match":{"tools":["*"]},"action":{"type":"command","command":"true"},"enabled":true}]}`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			p := writeHooksJSON(t, dir, "hooks-"+name+".json", content)
			if _, err := LoadUserHooksFile(p); err == nil {
				t.Fatalf("expected a loud error for %s, got nil", name)
			}
		})
	}
}

func TestLoadUserHooksFile_Valid(t *testing.T) {
	dir := t.TempDir()
	p := writeHooksJSON(t, dir, "hooks.json", `{"hooks":[
		{"id":"deny-rm","event":"PreToolUse","match":{"tools":["bash"]},"action":{"type":"command","command":"exit 1"},"enabled":true}
	]}`)
	f, err := LoadUserHooksFile(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f == nil || len(f.Hooks) != 1 || f.Hooks[0].ID != "deny-rm" {
		t.Fatalf("unexpected parse result: %+v", f)
	}
}

func TestMatchesTool_Glob(t *testing.T) {
	cases := []struct {
		patterns []string
		tool     string
		want     bool
	}{
		{[]string{"*"}, "bash", true},
		{[]string{"write_file"}, "write_file", true},
		{[]string{"write_file"}, "read_file", false},
		{[]string{"write_*"}, "write_file", true},
		{[]string{"write_*"}, "edit_file", false},
		{[]string{"read_file", "edit_file"}, "edit_file", true},
	}
	for _, c := range cases {
		if got := matchesTool(c.patterns, c.tool); got != c.want {
			t.Errorf("matchesTool(%v, %q) = %v, want %v", c.patterns, c.tool, got, c.want)
		}
	}
}

func TestUserHookRunner_NoFiles_IsNoop(t *testing.T) {
	dir := t.TempDir()
	r := NewUserHookRunner(nil)
	if err := r.Load(filepath.Join(dir, "global.json"), filepath.Join(dir, "ws.json")); err != nil {
		t.Fatalf("Load with no files present should not error: %v", err)
	}
	allow, reason := r.Pre(context.Background(), "bash", map[string]any{"command": "rm -rf /"})
	if !allow || reason != "" {
		t.Fatalf("no hooks configured should always allow, got allow=%v reason=%q", allow, reason)
	}
	if entries := r.AuditLog().Recent(0); len(entries) != 0 {
		t.Fatalf("expected no audit entries, got %d", len(entries))
	}
}

func TestUserHookRunner_PreToolUse_VetoesOnNonZeroExit(t *testing.T) {
	dir := t.TempDir()
	p := writeHooksJSON(t, dir, "hooks.json", `{"hooks":[
		{"id":"deny-bash","event":"PreToolUse","match":{"tools":["bash"]},"action":{"type":"command","command":"echo blocked by policy 1>&2; exit 1"},"enabled":true}
	]}`)
	r := NewUserHookRunner(nil)
	if err := r.Load(p); err != nil {
		t.Fatalf("Load: %v", err)
	}
	allow, reason := r.Pre(context.Background(), "bash", map[string]any{})
	if allow {
		t.Fatalf("expected veto, got allow=true")
	}
	if !strings.Contains(reason, "blocked by policy") {
		t.Fatalf("deny reason should surface hook output, got %q", reason)
	}
	// Unmatched tool passes through.
	allow2, _ := r.Pre(context.Background(), "read_file", map[string]any{})
	if !allow2 {
		t.Fatalf("hook matched only bash; read_file should be allowed")
	}

	// read_file never matched the hook's tool pattern, so it never ran and
	// left no audit entry — only the vetoed bash call did.
	entries := r.AuditLog().Recent(0)
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d: %+v", len(entries), entries)
	}
	if !entries[0].Vetoed || entries[0].Tool != "bash" {
		t.Fatalf("unexpected audit entry: %+v", entries[0])
	}
}

func TestUserHookRunner_PreToolUse_AllowsOnZeroExit(t *testing.T) {
	dir := t.TempDir()
	p := writeHooksJSON(t, dir, "hooks.json", `{"hooks":[
		{"id":"observe","event":"PreToolUse","match":{"tools":["*"]},"action":{"type":"command","command":"exit 0"},"enabled":true}
	]}`)
	r := NewUserHookRunner(nil)
	if err := r.Load(p); err != nil {
		t.Fatalf("Load: %v", err)
	}
	allow, reason := r.Pre(context.Background(), "bash", map[string]any{})
	if !allow || reason != "" {
		t.Fatalf("expected allow, got allow=%v reason=%q", allow, reason)
	}
}

func TestUserHookRunner_PostToolUse_ObserveOnly(t *testing.T) {
	dir := t.TempDir()
	p := writeHooksJSON(t, dir, "hooks.json", `{"hooks":[
		{"id":"post-check","event":"PostToolUse","match":{"tools":["*"]},"action":{"type":"command","command":"echo not fatal 1>&2; exit 3"},"enabled":true}
	]}`)
	r := NewUserHookRunner(nil)
	if err := r.Load(p); err != nil {
		t.Fatalf("Load: %v", err)
	}
	result := &tools.ToolResult{Output: "original output"}
	r.Post(context.Background(), "bash", map[string]any{}, result)
	if result.Output != "original output" {
		t.Fatalf("PostToolUse must be observe-only, result mutated to %q", result.Output)
	}
	entries := r.AuditLog().Recent(1)
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	if entries[0].ExitCode != 3 || entries[0].HookID != "post-check" {
		t.Fatalf("unexpected audit entry: %+v", entries[0])
	}
}

func TestUserHookRunner_Disabled_NeverRuns(t *testing.T) {
	dir := t.TempDir()
	p := writeHooksJSON(t, dir, "hooks.json", `{"hooks":[
		{"id":"off","event":"PreToolUse","match":{"tools":["*"]},"action":{"type":"command","command":"exit 1"},"enabled":false}
	]}`)
	r := NewUserHookRunner(nil)
	if err := r.Load(p); err != nil {
		t.Fatalf("Load: %v", err)
	}
	allow, _ := r.Pre(context.Background(), "bash", map[string]any{})
	if !allow {
		t.Fatalf("disabled hook must never run/veto")
	}
}

func TestUserHookRunner_WorkspaceOverridesGlobalByID(t *testing.T) {
	dir := t.TempDir()
	global := writeHooksJSON(t, dir, "global.json", `{"hooks":[
		{"id":"guard","event":"PreToolUse","match":{"tools":["*"]},"action":{"type":"command","command":"exit 1"},"enabled":true}
	]}`)
	workspace := writeHooksJSON(t, dir, "workspace.json", `{"hooks":[
		{"id":"guard","event":"PreToolUse","match":{"tools":["*"]},"action":{"type":"command","command":"exit 0"},"enabled":true}
	]}`)
	r := NewUserHookRunner(nil)
	if err := r.Load(global, workspace); err != nil {
		t.Fatalf("Load: %v", err)
	}
	allow, _ := r.Pre(context.Background(), "bash", map[string]any{})
	if !allow {
		t.Fatalf("workspace hooks.json entry should override global entry with same id")
	}
}

func TestUserHookRunner_Load_MalformedFile_KeepsPreviousGoodState(t *testing.T) {
	dir := t.TempDir()
	p := writeHooksJSON(t, dir, "hooks.json", `{"hooks":[
		{"id":"guard","event":"PreToolUse","match":{"tools":["*"]},"action":{"type":"command","command":"exit 1"},"enabled":true}
	]}`)
	r := NewUserHookRunner(nil)
	if err := r.Load(p); err != nil {
		t.Fatalf("Load: %v", err)
	}
	allow, _ := r.Pre(context.Background(), "bash", map[string]any{})
	if allow {
		t.Fatalf("sanity: guard hook should veto before the bad edit")
	}

	// Now corrupt the file and reload — the error must be surfaced, not
	// silently swallowed, and the previously-good hook must keep running.
	writeHooksJSON(t, dir, "hooks.json", `{not json`)
	if err := r.Load(p); err == nil {
		t.Fatalf("expected Load to surface the malformed-file error")
	}
	if r.LastError() == nil {
		t.Fatalf("LastError() should report the malformed-file error")
	}
	allow2, _ := r.Pre(context.Background(), "bash", map[string]any{})
	if allow2 {
		t.Fatalf("a failed reload must not silently disable the previously-loaded hook")
	}
}

// F6: a failed Load must still advance r.mtimes for the broken file, or
// maybeAutoReload will see its mtime as "changed" forever and re-read +
// re-parse the same bad bytes on every single Pre/Post call.
func TestUserHookRunner_Load_MalformedFile_AdvancesMtime_NoReloadThrash(t *testing.T) {
	dir := t.TempDir()
	p := writeHooksJSON(t, dir, "hooks.json", `{"hooks":[
		{"id":"guard","event":"PreToolUse","match":{"tools":["*"]},"action":{"type":"command","command":"exit 1"},"enabled":true}
	]}`)
	r := NewUserHookRunner(nil)
	if err := r.Load(p); err != nil {
		t.Fatalf("Load: %v", err)
	}

	writeHooksJSON(t, dir, "hooks.json", `{not json`)
	if err := r.Load(p); err == nil {
		t.Fatalf("expected Load to surface the malformed-file error")
	}

	r.mtimeMu.Lock()
	recorded, ok := r.mtimes[p]
	r.mtimeMu.Unlock()
	if !ok {
		t.Fatalf("failed Load did not record an mtime for %s — every subsequent Pre/Post call will re-parse the broken file", p)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !recorded.Equal(fi.ModTime()) {
		t.Fatalf("recorded mtime %v does not match the broken file's actual mtime %v", recorded, fi.ModTime())
	}

	// With the mtime recorded, further auto-reload checks against the same
	// (still-broken) file must be no-ops: LastError should remain the same
	// malformed-file error and not keep re-triggering Load.
	before := r.LastError()
	r.maybeAutoReload()
	after := r.LastError()
	if before == nil || after == nil || before.Error() != after.Error() {
		t.Fatalf("expected no reload thrash: before=%v after=%v", before, after)
	}
}

// F1: a hook that backgrounds a child must not defeat its own timeout. This
// is the exact repro from the vet: timeout=2s, command backgrounds a 30s
// sleep and returns immediately — runHookCommand must come back in roughly
// timeout+WaitDelay, not 30s (the backgrounded child would otherwise keep
// the stdout pipe open and block cmd.Run()'s I/O copy indefinitely).
func TestRunHookCommand_TimeoutNotDefeatedByBackgroundedChild(t *testing.T) {
	h := UserHookDef{
		ID:     "bg-sleep",
		Event:  UserHookPreToolUse,
		Action: UserHookAction{Type: "command", Command: "sleep 30 & echo started", TimeoutSecs: 2},
	}
	start := time.Now()
	exitCode, output, err := runHookCommand(context.Background(), h, "bash", nil)
	elapsed := time.Since(start)

	// Generous upper bound: timeout (2s) + hookWaitDelay (2s) + scheduling
	// slack. The bug produced ~30s; a correct fix comes back in a few
	// seconds either way, so 10s cleanly separates pass from the old bug.
	if elapsed >= 10*time.Second {
		t.Fatalf("runHookCommand took %v — timeout was defeated by the backgrounded child (bug F1)", elapsed)
	}
	// The exact error differs by platform: on macOS the process-group kill
	// closes the pipe first (context timeout error); on Linux Go's WaitDelay
	// path fires first (exec.ErrWaitDelay, "WaitDelay expired before I/O
	// complete"). Either way the safety property is the same — a non-nil
	// error, which the caller treats as a fail-closed veto.
	if err == nil {
		t.Fatalf("expected an error (timeout or WaitDelay), got exitCode=%d output=%q err=nil", exitCode, output)
	}
	t.Logf("runHookCommand returned in %v (exitCode=%d, output=%q)", elapsed, exitCode, output)
}

// F4: unbounded hook output must not be captured without limit — a hook
// printing gigabytes would otherwise OOM the process before its own
// timeout fires. Output past the cap is truncated, with the truncation
// marked rather than silently dropped.
func TestRunHookCommand_OutputCappedAndMarkedTruncated(t *testing.T) {
	h := UserHookDef{
		ID:    "firehose",
		Event: UserHookPostToolUse,
		Action: UserHookAction{
			Type:        "command",
			Command:     "yes | head -c 200000",
			TimeoutSecs: 5,
		},
	}
	_, output, err := runHookCommand(context.Background(), h, "bash", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(output) > hookOutputCapBytes+256 {
		t.Fatalf("captured output not capped: got %d bytes (cap %d)", len(output), hookOutputCapBytes)
	}
	if !strings.Contains(output, "truncated") {
		t.Fatalf("expected truncation to be marked in the output, got %d bytes with no marker", len(output))
	}
}

func TestUserHookRunner_AutoReloadsOnFileChange(t *testing.T) {
	dir := t.TempDir()
	p := writeHooksJSON(t, dir, "hooks.json", `{"hooks":[
		{"id":"guard","event":"PreToolUse","match":{"tools":["*"]},"action":{"type":"command","command":"exit 1"},"enabled":true}
	]}`)
	r := NewUserHookRunner(nil)
	if err := r.Load(p); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if allow, _ := r.Pre(context.Background(), "bash", map[string]any{}); allow {
		t.Fatalf("sanity: expected veto before edit")
	}

	// Bump mtime forward so the poll-on-call check reliably observes a change
	// even on filesystems with coarse mtime resolution.
	future := time.Now().Add(2 * time.Second)
	writeHooksJSON(t, dir, "hooks.json", `{"hooks":[
		{"id":"guard","event":"PreToolUse","match":{"tools":["*"]},"action":{"type":"command","command":"exit 0"},"enabled":true}
	]}`)
	if err := os.Chtimes(p, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	allow, _ := r.Pre(context.Background(), "bash", map[string]any{})
	if !allow {
		t.Fatalf("expected auto-reload to pick up the edited hooks.json without an explicit Reload() call")
	}
}

func TestUserHookRunner_ReloadOnDemand(t *testing.T) {
	dir := t.TempDir()
	p := writeHooksJSON(t, dir, "hooks.json", `{"hooks":[
		{"id":"guard","event":"PreToolUse","match":{"tools":["*"]},"action":{"type":"command","command":"exit 1"},"enabled":true}
	]}`)
	r := NewUserHookRunner(nil)
	if err := r.Load(p); err != nil {
		t.Fatalf("Load: %v", err)
	}
	writeHooksJSON(t, dir, "hooks.json", `{"hooks":[
		{"id":"guard","event":"PreToolUse","match":{"tools":["*"]},"action":{"type":"command","command":"exit 0"},"enabled":true}
	]}`)
	if err := r.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if allow, _ := r.Pre(context.Background(), "bash", map[string]any{}); !allow {
		t.Fatalf("Reload() should pick up the edited hooks.json")
	}
}

func TestUserHookRunner_RegistersIntoHookRegistry(t *testing.T) {
	dir := t.TempDir()
	p := writeHooksJSON(t, dir, "hooks.json", `{"hooks":[
		{"id":"guard","event":"PreToolUse","match":{"tools":["bash"]},"action":{"type":"command","command":"exit 1"},"enabled":true}
	]}`)
	reg := NewHookRegistry()
	r := NewUserHookRunner(nil)
	if err := r.Load(p); err != nil {
		t.Fatalf("Load: %v", err)
	}
	reg.RegisterPreToolUse(r.Pre)

	allow, reason := reg.runPre(context.Background(), "bash", map[string]any{})
	if allow || reason == "" {
		t.Fatalf("user hook registered into HookRegistry should veto bash, got allow=%v reason=%q", allow, reason)
	}
}

func TestUserHookRunner_CoexistsWithSyntaxHook(t *testing.T) {
	dir := t.TempDir()
	p := writeHooksJSON(t, dir, "hooks.json", `{"hooks":[
		{"id":"guard-write","event":"PreToolUse","match":{"tools":["write_file"]},"action":{"type":"command","command":"exit 1"},"enabled":true}
	]}`)
	reg := NewHookRegistry()
	RegisterSyntaxValidation(reg, func() string { return "block" })
	r := NewUserHookRunner(nil)
	if err := r.Load(p); err != nil {
		t.Fatalf("Load: %v", err)
	}
	reg.RegisterPreToolUse(r.Pre)

	// Syntactically valid Go content but the user hook still vetoes
	// write_file unconditionally — both deny sources must work.
	allow, reason := reg.runPre(context.Background(), "write_file", map[string]any{
		"path": "main.go", "content": "package main\nfunc main() {}\n",
	})
	if allow || reason == "" {
		t.Fatalf("expected the user hook to veto even though syntax validation passes, got allow=%v reason=%q", allow, reason)
	}
}
