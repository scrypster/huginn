package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/scrypster/huginn/internal/tools"
)

// ── User-configurable hooks (G10 extended) ─────────────────────────────────
//
// Design: internal/agent/hooks.go's HookRegistry is the substrate the
// harness already builds at startup (Orchestrator.EnableToolHooks). This
// file adds a *second*, user-authored layer on top of it: shell-command
// hooks a human (or, with sign-off, a model on the human's behalf) declares
// in .huginn/hooks.json (per-workspace) and/or ~/.huginn/hooks.json
// (global), loaded by UserHookRunner and registered into the SAME
// HookRegistry the built-in G1 syntax hook lives on — a PreToolUse denial
// from either source vetoes the tool identically.
//
// Trust model: these are shell commands the user (or an agent the user
// authorized) wrote to their own disk. Running them is exactly as
// privileged as anything else the user could type into a terminal on this
// machine — there is no sandbox and none is promised. What IS enforced:
//   - Off by default: no hooks.json anywhere → UserHookRunner.pre/post are
//     empty and every Pre/Post call is a no-op allow. Nothing runs shell
//     commands until the user (or an agent they've granted write_file, with
//     the normal write-permission confirmation) creates the file — see the
//     package doc for the smart-model-authoring design decision.
//   - Every execution — allowed or vetoing — is appended to a HookAuditLog
//     so "a hook silently sits in the loop forever" is not a supported
//     failure mode; internal/server exposes this over the API for the UI.
//   - A malformed hooks.json is a load error, never silently ignored (see
//     LoadUserHooksFile) — a typo in the file must not produce "the hook I
//     configured stopped running" with no diagnostic anywhere.

// UserHookEvent is the event a user hook attaches to. Mirrors the two
// built-in hook points HookRegistry already exposes.
type UserHookEvent string

const (
	UserHookPreToolUse  UserHookEvent = "PreToolUse"
	UserHookPostToolUse UserHookEvent = "PostToolUse"
)

// UserHookMatch selects which tool calls a hook applies to. Tools is a list
// of exact tool names and/or glob patterns (path.Match syntax — "*", "?",
// "[...]"); "*" alone matches every tool. Must be non-empty — an empty list
// is a load error rather than a hook that silently never fires.
type UserHookMatch struct {
	Tools []string `json:"tools"`
}

// UserHookAction is what the hook does. "command" is the only action.Type
// today: run Command in a shell, with the tool call handed to it (see
// runHookCommand for the exact stdin/env contract).
type UserHookAction struct {
	Type        string `json:"type"`
	Command     string `json:"command"`
	TimeoutSecs int    `json:"timeout_secs,omitempty"`
}

// UserHookDef is one entry in hooks.json.
type UserHookDef struct {
	ID      string         `json:"id"`
	Event   UserHookEvent  `json:"event"`
	Match   UserHookMatch  `json:"match"`
	Action  UserHookAction `json:"action"`
	Enabled bool           `json:"enabled"`
	// Source is set by the loader (not read from JSON) so the audit trail
	// and the UI can show where a hook came from.
	Source string `json:"-"`
}

// UserHooksFile is the top-level shape of hooks.json.
type UserHooksFile struct {
	Hooks []UserHookDef `json:"hooks"`
}

const (
	defaultHookTimeoutSecs = 30
	maxHookTimeoutSecs     = 300

	// hookOutputCapBytes bounds how much combined stdout+stderr a hook
	// execution can accumulate before it's truncated. A hook that prints
	// unbounded output (e.g. a runaway loop) must not OOM the process, and
	// the captured string is also what lands in the (bounded, but
	// per-entry-unbounded) HookAuditLog.
	hookOutputCapBytes = 64 * 1024

	// hookWaitDelay bounds how long runHookCommand waits, after the
	// command's context is canceled (timeout) or the process exits, for
	// its stdout/stderr pipes to actually close. Without this, a hook that
	// backgrounds a child (e.g. "sleep 30 &") keeps the pipe open past the
	// timeout and cmd.Run() blocks on the I/O copy regardless of ctx.
	hookWaitDelay = 2 * time.Second
)

// GlobalHooksPath returns ~/.huginn/hooks.json. huginnHome is the huginn
// home directory root (the same one EnableUserHooks callers already have —
// e.g. main.go's huginnHome) so tests can point it at a temp dir.
func GlobalHooksPath(huginnHome string) string {
	if strings.TrimSpace(huginnHome) == "" {
		return ""
	}
	return filepath.Join(huginnHome, "hooks.json")
}

// WorkspaceHooksPath returns <workspaceRoot>/.huginn/hooks.json.
func WorkspaceHooksPath(workspaceRoot string) string {
	if strings.TrimSpace(workspaceRoot) == "" {
		return ""
	}
	return filepath.Join(workspaceRoot, ".huginn", "hooks.json")
}

// LoadUserHooksFile reads and validates one hooks.json. A missing file
// returns (nil, nil) — "no hooks configured here" is not an error. Any
// other read failure or a malformed/invalid file (bad JSON, unknown event,
// empty match.tools, missing command, duplicate id within the file) is
// returned as an error — this loader never silently drops a hook.
func LoadUserHooksFile(path string) (*UserHooksFile, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var f UserHooksFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	seen := make(map[string]bool, len(f.Hooks))
	for i := range f.Hooks {
		h := &f.Hooks[i]
		if strings.TrimSpace(h.ID) == "" {
			return nil, fmt.Errorf("%s: hooks[%d] missing required \"id\"", path, i)
		}
		if seen[h.ID] {
			return nil, fmt.Errorf("%s: duplicate hook id %q", path, h.ID)
		}
		seen[h.ID] = true
		if h.Event != UserHookPreToolUse && h.Event != UserHookPostToolUse {
			return nil, fmt.Errorf("%s: hook %q: event must be %q or %q, got %q", path, h.ID, UserHookPreToolUse, UserHookPostToolUse, h.Event)
		}
		if len(h.Match.Tools) == 0 {
			return nil, fmt.Errorf(`%s: hook %q: match.tools must be non-empty (use ["*"] to match every tool)`, path, h.ID)
		}
		if h.Action.Type != "command" {
			return nil, fmt.Errorf("%s: hook %q: action.type must be \"command\", got %q", path, h.ID, h.Action.Type)
		}
		if strings.TrimSpace(h.Action.Command) == "" {
			return nil, fmt.Errorf("%s: hook %q: action.command is required", path, h.ID)
		}
		if h.Action.TimeoutSecs < 0 {
			return nil, fmt.Errorf("%s: hook %q: timeout_secs must not be negative", path, h.ID)
		}
	}
	return &f, nil
}

// mergeUserHooks concatenates hook files in priority order (lowest priority
// first) and dedupes by id, the LAST occurrence winning — so a workspace
// hooks.json entry with the same id as a global one overrides it.
func mergeUserHooks(files ...*UserHooksFile) []UserHookDef {
	byID := make(map[string]UserHookDef)
	var order []string
	for _, f := range files {
		if f == nil {
			continue
		}
		for _, h := range f.Hooks {
			if _, exists := byID[h.ID]; !exists {
				order = append(order, h.ID)
			}
			byID[h.ID] = h
		}
	}
	out := make([]UserHookDef, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	return out
}

// matchesTool reports whether toolName matches any pattern in patterns.
// "*" matches everything; other patterns are exact names or path.Match
// globs (e.g. "write_*", "*_file").
func matchesTool(patterns []string, toolName string) bool {
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "*" || p == toolName {
			return true
		}
		if ok, err := path.Match(p, toolName); err == nil && ok {
			return true
		}
	}
	return false
}

// HookAuditEntry is one recorded execution of a user hook.
type HookAuditEntry struct {
	Time     time.Time     `json:"time"`
	HookID   string        `json:"hook_id"`
	Event    UserHookEvent `json:"event"`
	Tool     string        `json:"tool"`
	Vetoed   bool          `json:"vetoed"` // PreToolUse only; always false for PostToolUse
	ExitCode int           `json:"exit_code"`
	Output   string        `json:"output"`
	Err      string        `json:"error,omitempty"`
	// TestRun is true for a dry-run against a sample tool call (POST
	// /api/v1/hooks/test — see RunHookForTest) and false for an execution
	// that gated or observed a real tool call (Pre/Post). Every run is
	// audited either way; this field is how the trail stays distinguishable
	// rather than test runs being silently omitted.
	TestRun bool `json:"test_run"`
}

// HookAuditLog is a bounded, concurrency-safe ring buffer of hook
// executions. Deliberately independent of internal/server's SQLite audit
// trail (internal/agent cannot import internal/server); the server surfaces
// this over the API instead (see internal/server/handlers_hooks.go).
type HookAuditLog struct {
	mu      sync.Mutex
	entries []HookAuditEntry
	cap     int
}

// NewHookAuditLog returns a ring buffer holding at most capacity entries
// (oldest dropped first). capacity <= 0 defaults to 500.
func NewHookAuditLog(capacity int) *HookAuditLog {
	if capacity <= 0 {
		capacity = 500
	}
	return &HookAuditLog{cap: capacity}
}

func (l *HookAuditLog) append(e HookAuditEntry) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, e)
	if over := len(l.entries) - l.cap; over > 0 {
		l.entries = l.entries[over:]
	}
}

// Recent returns up to n most-recent entries, newest first. n <= 0 returns
// everything held.
func (l *HookAuditLog) Recent(n int) []HookAuditEntry {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	total := len(l.entries)
	if n <= 0 || n > total {
		n = total
	}
	out := make([]HookAuditEntry, n)
	for i := 0; i < n; i++ {
		out[i] = l.entries[total-1-i]
	}
	return out
}

// UserHookRunner is the live, reloadable set of compiled user hooks for one
// orchestrator. It registers exactly one PreToolUse and one PostToolUse
// delegating hook into a HookRegistry (see Attach); Reload/Load swap in a
// freshly-compiled hook list without touching that registration, so the
// registry never needs to support unregistering.
type UserHookRunner struct {
	audit *HookAuditLog

	mu    sync.RWMutex
	defs  []UserHookDef // enabled hooks only, in load order
	paths []string

	mtimeMu sync.Mutex
	mtimes  map[string]time.Time
	lastErr error
}

// NewUserHookRunner returns an empty runner (no hooks loaded — Pre/Post are
// no-ops until Load is called).
func NewUserHookRunner(audit *HookAuditLog) *UserHookRunner {
	if audit == nil {
		audit = NewHookAuditLog(0)
	}
	return &UserHookRunner{audit: audit, mtimes: make(map[string]time.Time)}
}

// AuditLog returns the runner's audit log (never nil).
func (r *UserHookRunner) AuditLog() *HookAuditLog { return r.audit }

// LastError returns the error from the most recent Load call, if any (nil
// on success). Used by the reload endpoint and boot logging to surface a
// malformed hooks.json loudly instead of swallowing it.
func (r *UserHookRunner) LastError() error {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lastErr
}

// Paths returns the hooks.json paths this runner was last told to load
// (global, workspace — in the priority order given to Load), for reload.
func (r *UserHookRunner) Paths() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, len(r.paths))
	copy(out, r.paths)
	return out
}

// Load reads, validates, and compiles every path (lowest priority first —
// pass global then workspace so workspace overrides by id). On success it
// atomically replaces the runner's active hook set and remembers paths for
// future auto-reload/manual Reload calls. On failure it leaves the
// previously-loaded hooks in place (a bad edit to hooks.json must not blow
// away a working hook set) and returns the error — callers MUST surface
// this, not swallow it.
func (r *UserHookRunner) Load(paths ...string) error {
	// Stat every path up front, independent of load success, so a failed
	// Load still advances r.mtimes below. Otherwise maybeAutoReload keeps
	// seeing the broken file's mtime as "changed" forever and re-reads +
	// re-parses the same bad bytes on every single tool call until the
	// user fixes it (F6).
	mtimes := make(map[string]time.Time, len(paths))
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if fi, statErr := os.Stat(p); statErr == nil {
			mtimes[p] = fi.ModTime()
		}
	}

	var files []*UserHooksFile
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		f, err := LoadUserHooksFile(p)
		if err != nil {
			r.mu.Lock()
			r.lastErr = err
			r.mu.Unlock()
			r.mtimeMu.Lock()
			r.mtimes = mtimes
			r.mtimeMu.Unlock()
			return err
		}
		files = append(files, f)
	}
	merged := mergeUserHooks(files...)
	enabled := make([]UserHookDef, 0, len(merged))
	for _, h := range merged {
		if h.Enabled {
			enabled = append(enabled, h)
		}
	}
	sort.SliceStable(enabled, func(i, j int) bool { return enabled[i].ID < enabled[j].ID })

	r.mu.Lock()
	r.defs = enabled
	r.paths = append([]string(nil), paths...)
	r.lastErr = nil
	r.mu.Unlock()

	r.mtimeMu.Lock()
	r.mtimes = mtimes
	r.mtimeMu.Unlock()
	return nil
}

// Reload re-reads the paths passed to the last successful/attempted Load.
// A no-op (returns nil) if Load has never been called.
func (r *UserHookRunner) Reload() error {
	paths := r.Paths()
	if len(paths) == 0 {
		return nil
	}
	return r.Load(paths...)
}

// maybeAutoReload stats the configured paths and reloads if any changed
// (mtime differs, or a file appeared/disappeared) since the last Load. Best
// effort: a stat error just skips auto-reload for this call, it does not
// propagate — a transient FS hiccup must never veto or break a tool call.
// Called before every Pre/Post so hooks.json edits take effect without a
// restart, with no background goroutine required.
func (r *UserHookRunner) maybeAutoReload() {
	paths := r.Paths()
	if len(paths) == 0 {
		return
	}
	changed := false
	current := make(map[string]time.Time, len(paths))
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		fi, err := os.Stat(p)
		if err != nil {
			continue // missing file: nothing to compare, not a change signal here
		}
		current[p] = fi.ModTime()
	}
	r.mtimeMu.Lock()
	for p, mt := range current {
		if prev, ok := r.mtimes[p]; !ok || !prev.Equal(mt) {
			changed = true
			break
		}
	}
	if !changed && len(current) != len(r.mtimes) {
		changed = true
	}
	r.mtimeMu.Unlock()
	if changed {
		_ = r.Load(paths...) // errors already captured in lastErr for the reload endpoint/boot log
	}
}

func (r *UserHookRunner) snapshot() []UserHookDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]UserHookDef, len(r.defs))
	copy(out, r.defs)
	return out
}

// Pre is a PreToolUseHook: runs every enabled PreToolUse user hook matching
// toolName, in id order. The first one to exit non-zero vetoes the call.
func (r *UserHookRunner) Pre(ctx context.Context, toolName string, args map[string]any) (bool, string) {
	if r == nil {
		return true, ""
	}
	r.maybeAutoReload()
	for _, h := range r.snapshot() {
		if h.Event != UserHookPreToolUse || !matchesTool(h.Match.Tools, toolName) {
			continue
		}
		exitCode, output, runErr := runHookCommand(ctx, h, toolName, args)
		vetoed := exitCode != 0 || runErr != nil
		r.audit.append(HookAuditEntry{
			Time: time.Now(), HookID: h.ID, Event: h.Event, Tool: toolName,
			Vetoed: vetoed, ExitCode: exitCode, Output: output, Err: errString(runErr), TestRun: false,
		})
		if vetoed {
			reason := strings.TrimSpace(output)
			if reason == "" && runErr != nil {
				reason = runErr.Error()
			}
			if reason == "" {
				reason = fmt.Sprintf("hook %q exited %d", h.ID, exitCode)
			}
			return false, reason
		}
	}
	return true, ""
}

// Post is a PostToolUseHook: runs every enabled PostToolUse user hook
// matching toolName, in id order. Observe-only — a non-zero exit is
// recorded in the audit log (and, best-effort, in slog) but never mutates
// result or fails the call.
func (r *UserHookRunner) Post(ctx context.Context, toolName string, args map[string]any, result *tools.ToolResult) {
	if r == nil {
		return
	}
	r.maybeAutoReload()
	for _, h := range r.snapshot() {
		if h.Event != UserHookPostToolUse || !matchesTool(h.Match.Tools, toolName) {
			continue
		}
		exitCode, output, runErr := runHookCommand(ctx, h, toolName, args)
		r.audit.append(HookAuditEntry{
			Time: time.Now(), HookID: h.ID, Event: h.Event, Tool: toolName,
			Vetoed: false, ExitCode: exitCode, Output: output, Err: errString(runErr), TestRun: false,
		})
	}
}

// HookTestResult is the outcome of a dry-run of one hook against a sample
// tool call — the payload behind POST /api/v1/hooks/test ("test-run a hook
// against a sample tool call" in the hooks UI).
type HookTestResult struct {
	Allowed  bool   `json:"allowed"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
}

// RunHookForTest runs one hook's command against a sample tool call for a
// UI/API preview. The trust story for this feature is "every run is
// audited" — so a test run IS recorded into audit (when audit is non-nil),
// tagged TestRun: true so the trail stays distinguishable from real
// executions rather than the run going unrecorded entirely. A nil audit
// log is a safe no-op (append() nil-guards), for callers that genuinely
// have none (e.g. before EnableUserHooks has run).
func RunHookForTest(ctx context.Context, audit *HookAuditLog, h UserHookDef, toolName string, args map[string]any) HookTestResult {
	exitCode, output, err := runHookCommand(ctx, h, toolName, args)
	allowed := true
	if h.Event == UserHookPreToolUse {
		allowed = exitCode == 0 && err == nil
	}
	audit.append(HookAuditEntry{
		Time: time.Now(), HookID: h.ID, Event: h.Event, Tool: toolName,
		Vetoed: h.Event == UserHookPreToolUse && !allowed, ExitCode: exitCode,
		Output: output, Err: errString(err), TestRun: true,
	})
	return HookTestResult{Allowed: allowed, ExitCode: exitCode, Output: output, Error: errString(err)}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// runHookCommand runs one user hook's command via "sh -c", giving it the
// tool call two ways for compatibility with whatever the hook author wrote:
//
//   - stdin: JSON {"event","tool","args"} (Claude-Code-style hooks read
//     their payload from stdin as JSON).
//   - env: HUGINN_EVENT, HUGINN_TOOL, HUGINN_TOOL_ARGS (args as JSON) — for
//     hooks that would rather not parse stdin (a one-line `[ "$HUGINN_TOOL"
//     = bash ] && exit 1` needs no JSON parser).
//
// See the package doc comment / schema doc for why this isn't a byte-exact
// clone of Claude Code's own hook env vars.
func runHookCommand(ctx context.Context, h UserHookDef, toolName string, args map[string]any) (exitCode int, output string, err error) {
	timeout := h.Action.TimeoutSecs
	if timeout <= 0 {
		timeout = defaultHookTimeoutSecs
	}
	if timeout > maxHookTimeoutSecs {
		timeout = maxHookTimeoutSecs
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	argsJSON, _ := json.Marshal(args)
	payload, _ := json.Marshal(map[string]any{
		"event": h.Event,
		"tool":  toolName,
		"args":  args,
	})

	cmd := exec.CommandContext(cctx, "sh", "-c", h.Action.Command)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Env = append(os.Environ(),
		"HUGINN_EVENT="+string(h.Event),
		"HUGINN_TOOL="+toolName,
		"HUGINN_TOOL_ARGS="+string(argsJSON),
		"HUGINN_HOOK_ID="+h.ID,
	)
	out := &limitedHookOutput{limit: hookOutputCapBytes}
	cmd.Stdout = out
	cmd.Stderr = out

	// Run the command in its own process group so a timeout can kill the
	// whole tree, not just "sh" itself — a hook that backgrounds a child
	// (e.g. "sleep 30 & echo started") would otherwise keep that child
	// alive holding the stdout pipe open, and cmd.Run() blocks on the I/O
	// copy indefinitely regardless of ctx (F1). WaitDelay is the backstop:
	// even if the process-group kill can't reach every descendant (or on
	// platforms where it's a no-op — see hooks_config_windows.go), Run()
	// is still bounded to timeout+hookWaitDelay because Go forcibly closes
	// the pipes once WaitDelay elapses after cancellation.
	setHookProcAttrs(cmd)
	cmd.WaitDelay = hookWaitDelay
	cmd.Cancel = func() error {
		killHookProcessGroup(cmd)
		return cmd.Process.Kill()
	}

	runErr := cmd.Run()

	if cctx.Err() == context.DeadlineExceeded {
		return -1, out.String(), fmt.Errorf("hook %q timed out after %ds", h.ID, timeout)
	}
	if runErr == nil {
		return 0, strings.TrimSpace(out.String()), nil
	}
	if exitErr, ok := runErr.(*exec.ExitError); ok {
		return exitErr.ExitCode(), strings.TrimSpace(out.String()), nil
	}
	return -1, strings.TrimSpace(out.String()), runErr
}

// limitedHookOutput caps how much combined stdout+stderr a hook execution
// can accumulate (F4) — a hook that prints unbounded output must not OOM
// the process or bloat the audit log. Writes beyond the cap are discarded
// (not erroring the write, which could otherwise abort the child process
// with a broken-pipe-style failure); the returned string marks that
// truncation happened instead of silently dropping data with no trace.
type limitedHookOutput struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (w *limitedHookOutput) Write(p []byte) (int, error) {
	if !w.truncated {
		remaining := w.limit - w.buf.Len()
		if remaining <= 0 {
			w.truncated = true
		} else if len(p) > remaining {
			w.buf.Write(p[:remaining])
			w.truncated = true
		} else {
			w.buf.Write(p)
		}
	}
	return len(p), nil // report the full write as consumed either way
}

func (w *limitedHookOutput) String() string {
	if w.truncated {
		return w.buf.String() + fmt.Sprintf("\n...[output truncated at %d bytes]", w.limit)
	}
	return w.buf.String()
}
