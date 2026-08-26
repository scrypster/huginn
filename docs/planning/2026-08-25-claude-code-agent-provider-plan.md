# Claude Code Agent Provider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make a Huginn agent whose provider is `claude-code` BE a Claude Code session — you chat with it in Huginn, and Huginn drives that session with the agent's prompt, skills, tool restrictions and approval gates applied to every turn.

**Architecture:** A `ClaudeCodeBackend` implements the four-method `backend.Backend` interface and is constructed at the single agent-backend resolver in `main.go`. Each turn runs `claude --resume <session> -p <newest message>` with an assembled system prompt, an inline `--settings` JSON carrying `PreToolUse` approval hooks, and the agent's pre-authorised tools. Claude Code executes its own tools, so a new `ExecutedTools` field carries them back for persistence without the agent loop dispatching them a second time.

**Tech Stack:** Go 1.25, the existing `internal/claudecode` package (parser, mapper, tailer, ingester, watcher, delegate), `internal/backend`, `internal/agents`, `internal/server`.

**Spec:** `docs/planning/2026-08-25-claude-code-agent-provider-design.md`

## Global Constraints

- Go 1.25.0 (`go.mod`). Do not raise the floor. No new third-party dependencies.
- **NO TEST MAY INVOKE THE REAL `claude` CLI, TOUCH THE NETWORK, OR SPEND MONEY.** Use `internal/claudecode/testdata/fake-claude.sh` or a script written into `t.TempDir()`.
- Do NOT run `go test ./...` repo-wide. Other packages have non-hermetic tests. Run only the packages a task touches.
- Do NOT start a real Huginn server, and do NOT run `pnpm install` / `make build-frontend`.
- `git add` only files you edit, by explicit path. Never `git add -A`.
- No diff-added line may be unformatted (`gofmt -l`).
- **Approval fails closed.** Any path that cannot get an answer returns `deny`.
- Reuse `internal/claudecode/delegate.go`'s `BuildArgs` and `applyStreamLine`; do not write a second stream parser.

## Verified facts (established empirically — do not re-derive, do not "correct")

- `claude --resume <id> -p "…"` carries context across separate processes.
- `--permission-mode manual` does NOT prompt in `-p` mode. Per-call approval is only reachable via hooks.
- A `PreToolUse` hook returning `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"…"}}` on stdout, exit 0, genuinely blocks the tool call.
- The hook receives on stdin: `tool_name`, `tool_input`, `tool_use_id`, `session_id`, `cwd`, `transcript_path`, `permission_mode`, `prompt_id`.
- `--settings` accepts inline JSON, so hooks stay per-session. NEVER write to `~/.claude/settings.json`.
- `backend.Message` is `{Role, Content, Parts, ToolCalls, ToolName, ToolCallID}`.
- `backend.ToolCall` is `{ID, Function ToolCallFunction}`; `ToolCallFunction` is `{Name, Arguments map[string]any}`.
- `internal/agent/loop.go:441` persists `chatResult.ToolCalls` into history; `:468` ends the loop when it is empty; `:475` dispatches it. One field, two jobs.
- **A `PreToolUse` hook that TIMES OUT fails OPEN.** Verified against the real
  CLI: a hook entry accepts an explicit `"timeout"` (seconds) and Claude Code
  honours it, but when it fires the hook is killed and the tool runs anyway —
  the write succeeded and `permission_denials` was empty. Our fail-closed
  guarantee therefore rests on `claudeApproveTimeout` staying safely BELOW
  `claudecode.ClaudeHookTimeoutSecs` (20s vs 30s), so `huginn claude-approve`
  always prints an explicit `deny` first. Never tune either number alone.
- `main.go:3126` is the agent backend resolver: `serveCache.For(ag.Provider, ag.Endpoint, ag.APIKey, ag.GetModelID())`.

---

### Task 1: Agent fields for the bound session

**Files:**
- Modify: `internal/agents/config.go` (the `AgentConfig` struct, around line 18-40)
- Modify: `internal/agents/agent.go:36-58` (the `Agent` struct)
- Test: `internal/agents/claude_fields_test.go`

**Interfaces:**
- Produces: `AgentConfig.ClaudeSessionID string` (json `claude_session_id`), `AgentConfig.ClaudeCWD string` (json `claude_cwd`); the same two fields on `Agent`.

- [ ] **Step 1: Write the failing test**

Create `internal/agents/claude_fields_test.go`:

```go
package agents

import (
	"encoding/json"
	"testing"
)

func TestAgentConfigRoundTripsClaudeFields(t *testing.T) {
	in := AgentConfig{
		Name:            "Elena",
		Provider:        "claude-code",
		ClaudeSessionID: "11111111-2222-3333-4444-555555555555",
		ClaudeCWD:       "/Users/dev/project",
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out AgentConfig
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.ClaudeSessionID != in.ClaudeSessionID {
		t.Errorf("ClaudeSessionID = %q, want %q", out.ClaudeSessionID, in.ClaudeSessionID)
	}
	if out.ClaudeCWD != in.ClaudeCWD {
		t.Errorf("ClaudeCWD = %q, want %q", out.ClaudeCWD, in.ClaudeCWD)
	}
}

func TestClaudeFieldsAreOmittedWhenEmpty(t *testing.T) {
	b, err := json.Marshal(AgentConfig{Name: "Native", Provider: "anthropic"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, k := range []string{"claude_session_id", "claude_cwd"} {
		if contains(s, k) {
			t.Errorf("native agent JSON contains %q; both fields must be omitempty: %s", k, s)
		}
	}
}

func contains(h, n string) bool {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agents/ -run TestAgentConfig -v`
Expected: FAIL — `unknown field ClaudeSessionID`.

- [ ] **Step 3: Add the fields**

In `internal/agents/config.go`, in `AgentConfig`, after the `Endpoint` / `APIKey` lines:

```go
	// Claude Code provider binding. Set only when Provider == "claude-code".
	// One agent is bound to exactly one Claude Code session for its lifetime.
	ClaudeSessionID string `json:"claude_session_id,omitempty" yaml:"claude_session_id,omitempty"`
	ClaudeCWD       string `json:"claude_cwd,omitempty"        yaml:"claude_cwd,omitempty"`
```

In `internal/agents/agent.go`, in the `Agent` struct after `APIKey`:

```go
	ClaudeSessionID string
	ClaudeCWD       string
```

Then find where `AgentConfig` is converted to `Agent` (`grep -n "APIKey:" internal/agents/*.go`) and copy both fields in every direction the conversion runs. Missing one produces an agent that loads from disk without its session.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/agents/ -v`
Expected: PASS, including every pre-existing agents test.

- [ ] **Step 5: Commit**

```bash
git add internal/agents/config.go internal/agents/agent.go internal/agents/claude_fields_test.go
git commit -m "feat(agents): bind an agent to a Claude Code session"
```

---

### Task 2: `ExecutedTools` — persist backend-run tools without dispatching them

This is the change that makes Claude Code's tool activity indistinguishable from native tool activity. Get it wrong in the other direction and Huginn runs every tool a second time.

**Files:**
- Modify: `internal/backend/backend.go` (add `ExecutedTool` type and the `ChatResponse` field)
- Modify: `internal/agent/loop.go:437-476`
- Test: `internal/agent/executed_tools_test.go`

**Interfaces:**
- Produces: `backend.ExecutedTool{Call ToolCall; Result string}`; `backend.ChatResponse.ExecutedTools []ExecutedTool`.

- [ ] **Step 1: Write the failing test**

Create `internal/agent/executed_tools_test.go`. Read `internal/agent/loop.go` first to find how `runLoop` (or its equivalent) is invoked in existing tests and mirror that setup — `grep -n "func Test" internal/agent/loop_test.go` will show the pattern.

```go
package agent

import (
	"context"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

// A backend that reports tools it already ran itself.
type executedToolsBackend struct{ calls int }

func (b *executedToolsBackend) ChatCompletion(_ context.Context, _ backend.ChatRequest) (*backend.ChatResponse, error) {
	b.calls++
	return &backend.ChatResponse{
		Content:    "I read the file.",
		DoneReason: "stop",
		ExecutedTools: []backend.ExecutedTool{{
			Call: backend.ToolCall{
				ID:       "tu1",
				Function: backend.ToolCallFunction{Name: "Read", Arguments: map[string]any{"file_path": "/tmp/x"}},
			},
			Result: "package main",
		}},
	}, nil
}
func (b *executedToolsBackend) Health(_ context.Context) error   { return nil }
func (b *executedToolsBackend) Shutdown(_ context.Context) error { return nil }
func (b *executedToolsBackend) ContextWindow() int               { return 200000 }

func TestExecutedToolsArePersistedButNotDispatched(t *testing.T) {
	var dispatched int
	// Wire the loop with a dispatcher that MUST NOT be called, following the
	// setup used by the existing tests in loop_test.go.
	result, err := runLoopForTest(t, &executedToolsBackend{}, func() { dispatched++ })
	if err != nil {
		t.Fatalf("loop: %v", err)
	}
	if dispatched != 0 {
		t.Fatalf("dispatched %d tool calls; ExecutedTools must NEVER be dispatched — they already ran", dispatched)
	}
	if result.StopReason != "stop" {
		t.Errorf("StopReason = %q, want stop — the loop must terminate, not iterate", result.StopReason)
	}

	var sawAssistantWithCall, sawToolResult bool
	for _, m := range result.Messages {
		if m.Role == "assistant" && len(m.ToolCalls) == 1 && m.ToolCalls[0].ID == "tu1" {
			sawAssistantWithCall = true
		}
		if m.Role == "tool" && m.ToolCallID == "tu1" && m.Content == "package main" {
			sawToolResult = true
		}
	}
	if !sawAssistantWithCall {
		t.Error("no assistant message carrying the executed tool call — it was not persisted into history")
	}
	if !sawToolResult {
		t.Error("no role=tool message carrying the result — Huginn's history is missing the tool output")
	}
}
```

`runLoopForTest` is a helper you write in this file wrapping whatever entry point `loop_test.go` already uses. If the existing tests call an unexported function directly, do the same.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run TestExecutedTools -v`
Expected: FAIL — `unknown field ExecutedTools`.

- [ ] **Step 3: Add the type and field**

In `internal/backend/backend.go`, next to `ToolCall`:

```go
// ExecutedTool is a tool call the BACKEND already ran. Claude Code executes its
// own tools, so these must be persisted into history but never dispatched — the
// agent loop keys dispatching off ChatResponse.ToolCalls, which stays empty.
type ExecutedTool struct {
	Call   ToolCall
	Result string
}
```

In `ChatResponse`, after `ToolCalls`:

```go
	// ExecutedTools carries tool calls the backend already executed. The loop
	// persists them into history and does NOT dispatch them.
	ExecutedTools []ExecutedTool
```

- [ ] **Step 4: Teach the loop to persist them**

In `internal/agent/loop.go`, replace the assistant-message construction at line 437-442:

```go
		// Append assistant response to history. A backend that ran its own
		// tools reports them in ExecutedTools; they belong in history exactly
		// like dispatched calls, but must never reach dispatchTools below.
		persistedCalls := chatResult.ToolCalls
		if len(persistedCalls) == 0 && len(chatResult.ExecutedTools) > 0 {
			persistedCalls = make([]backend.ToolCall, 0, len(chatResult.ExecutedTools))
			for _, et := range chatResult.ExecutedTools {
				persistedCalls = append(persistedCalls, et.Call)
			}
		}
		assistantMsg := backend.Message{
			Role:      "assistant",
			Content:   chatResult.Content,
			ToolCalls: persistedCalls,
		}
		messages = append(messages, assistantMsg)
		for _, et := range chatResult.ExecutedTools {
			messages = append(messages, backend.Message{
				Role:       "tool",
				ToolName:   et.Call.Function.Name,
				ToolCallID: et.Call.ID,
				Content:    et.Result,
			})
		}
		result.FinalContent = chatResult.Content
```

Leave lines 468 and 475 untouched: they must keep testing `chatResult.ToolCalls`, which a Claude Code backend never populates, so the loop still terminates.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/agent/ -v`
Expected: PASS, including every pre-existing loop test. A pre-existing test breaking here means the change altered native tool dispatch — stop and report rather than adjusting that test.

- [ ] **Step 6: Commit**

```bash
git add internal/backend/backend.go internal/agent/loop.go internal/agent/executed_tools_test.go
git commit -m "feat(backend): ExecutedTools — persist backend-run tools without dispatching"
```

---

### Task 3: System prompt assembly

**Files:**
- Create: `internal/claudecode/prompt.go`
- Test: `internal/claudecode/prompt_test.go`

**Interfaces:**
- Produces: `func AssembleSystemPrompt(agentPrompt string, skills []string, notepad string) string`

- [ ] **Step 1: Write the failing test**

Create `internal/claudecode/prompt_test.go`:

```go
package claudecode

import (
	"strings"
	"testing"
)

func TestAssembleSystemPromptIncludesEveryPart(t *testing.T) {
	got := AssembleSystemPrompt(
		"You are Elena. Be terse.",
		[]string{"## Skill: code-review\nAlways check error handling.", "## Skill: testing\nTDD."},
		"Project note: this repo uses Go 1.25.",
	)
	for _, want := range []string{
		"You are Elena. Be terse.",
		"code-review",
		"Always check error handling.",
		"TDD.",
		"Project note: this repo uses Go 1.25.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("assembled prompt missing %q:\n%s", want, got)
		}
	}
}

func TestAssembleSystemPromptSkipsEmptySections(t *testing.T) {
	got := AssembleSystemPrompt("Just the prompt.", nil, "")
	if strings.Contains(got, "Skills") || strings.Contains(got, "Notepad") {
		t.Errorf("empty sections must not produce headings:\n%s", got)
	}
	if strings.TrimSpace(got) != "Just the prompt." {
		t.Errorf("got %q, want exactly the prompt with no decoration", got)
	}
}

func TestAssembleSystemPromptEmptyEverything(t *testing.T) {
	if got := AssembleSystemPrompt("", nil, ""); got != "" {
		t.Errorf("got %q, want empty string so --append-system-prompt can be omitted", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/claudecode/ -run TestAssembleSystemPrompt -v`
Expected: FAIL — `undefined: AssembleSystemPrompt`.

- [ ] **Step 3: Write the implementation**

Create `internal/claudecode/prompt.go`:

```go
package claudecode

import "strings"

// AssembleSystemPrompt builds the text passed to `claude --append-system-prompt`
// for a Claude Code agent.
//
// It is rebuilt on every turn rather than at session creation, so edits to an
// agent's prompt, skills or notepad take effect on the next message instead of
// requiring a new session.
//
// It APPENDS to Claude Code's own system prompt rather than replacing it, so
// the CLI's built-in behaviour stays intact.
func AssembleSystemPrompt(agentPrompt string, skills []string, notepad string) string {
	var b strings.Builder

	if s := strings.TrimSpace(agentPrompt); s != "" {
		b.WriteString(s)
	}

	var kept []string
	for _, sk := range skills {
		if s := strings.TrimSpace(sk); s != "" {
			kept = append(kept, s)
		}
	}
	if len(kept) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("# Skills\n\n")
		b.WriteString(strings.Join(kept, "\n\n"))
	}

	if s := strings.TrimSpace(notepad); s != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("# Notepad\n\n")
		b.WriteString(s)
	}

	return b.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/claudecode/ -run TestAssembleSystemPrompt -v`
Expected: PASS — all three.

- [ ] **Step 5: Commit**

```bash
git add internal/claudecode/prompt.go internal/claudecode/prompt_test.go
git commit -m "feat(claudecode): assemble the per-turn system prompt from agent config"
```

---

### Task 4: Build the per-session hook settings

**Files:**
- Create: `internal/claudecode/hooks.go`
- Test: `internal/claudecode/hooks_test.go`

**Interfaces:**
- Produces: `func BuildHookSettings(gatedTools []string, hookCommand string) (string, error)` returning inline JSON for `--settings`.

- [ ] **Step 1: Write the failing test**

Create `internal/claudecode/hooks_test.go`:

```go
package claudecode

import (
	"encoding/json"
	"testing"
)

func TestBuildHookSettingsOneEntryPerGatedTool(t *testing.T) {
	out, err := BuildHookSettings([]string{"Write", "Bash"}, "/usr/local/bin/huginn claude-approve")
	if err != nil {
		t.Fatalf("BuildHookSettings: %v", err)
	}
	var got struct {
		Hooks struct {
			PreToolUse []struct {
				Matcher string `json:"matcher"`
				Hooks   []struct {
					Type    string `json:"type"`
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(got.Hooks.PreToolUse) != 2 {
		t.Fatalf("got %d PreToolUse entries, want 2 — one per gated tool", len(got.Hooks.PreToolUse))
	}
	seen := map[string]bool{}
	for _, e := range got.Hooks.PreToolUse {
		seen[e.Matcher] = true
		if len(e.Hooks) != 1 || e.Hooks[0].Type != "command" {
			t.Errorf("entry %q malformed: %+v", e.Matcher, e)
		}
		if e.Hooks[0].Command != "/usr/local/bin/huginn claude-approve" {
			t.Errorf("command = %q", e.Hooks[0].Command)
		}
	}
	if !seen["Write"] || !seen["Bash"] {
		t.Errorf("matchers = %v, want Write and Bash", seen)
	}
}

func TestBuildHookSettingsNoGatedToolsMeansNoHooks(t *testing.T) {
	out, err := BuildHookSettings(nil, "irrelevant")
	if err != nil {
		t.Fatalf("BuildHookSettings: %v", err)
	}
	if out != "" {
		t.Errorf("got %q, want empty so --settings is omitted entirely when nothing is gated", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/claudecode/ -run TestBuildHookSettings -v`
Expected: FAIL — `undefined: BuildHookSettings`.

- [ ] **Step 3: Write the implementation**

Create `internal/claudecode/hooks.go`:

```go
package claudecode

import "encoding/json"

// BuildHookSettings produces the inline JSON passed to `claude --settings`,
// registering one PreToolUse hook per gated tool.
//
// ONE ENTRY PER TOOL, not a combined matcher: a single-tool matcher is the only
// form verified against the CLI, and per-tool entries need no assumption about
// regex support.
//
// Tools NOT listed here are pre-authorised via --allowedTools and never invoke
// the hook. That is what makes fail-closed safe: if Huginn is unreachable, only
// the tools that always required a human are blocked, and the agent degrades to
// its pre-authorised capability instead of stopping dead.
//
// Returns "" when nothing is gated, so the caller omits --settings entirely.
func BuildHookSettings(gatedTools []string, hookCommand string) (string, error) {
	if len(gatedTools) == 0 {
		return "", nil
	}

	type hookCmd struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	type entry struct {
		Matcher string    `json:"matcher"`
		Hooks   []hookCmd `json:"hooks"`
	}

	entries := make([]entry, 0, len(gatedTools))
	for _, tool := range gatedTools {
		if tool == "" {
			continue
		}
		entries = append(entries, entry{
			Matcher: tool,
			Hooks:   []hookCmd{{Type: "command", Command: hookCommand}},
		})
	}
	if len(entries) == 0 {
		return "", nil
	}

	payload := map[string]any{"hooks": map[string]any{"PreToolUse": entries}}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/claudecode/ -run TestBuildHookSettings -v`
Expected: PASS — both.

- [ ] **Step 5: Commit**

```bash
git add internal/claudecode/hooks.go internal/claudecode/hooks_test.go
git commit -m "feat(claudecode): build per-session PreToolUse hook settings"
```

---

### Task 5: The `huginn claude-approve` hook command

This runs as a subprocess of Claude Code, on the critical path of every gated tool call. It must never hang and must never fail open.

**Files:**
- Create: `cmd_claude_approve.go` (repo root, beside `cmd_skill.go`)
- Test: `cmd_claude_approve_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `func runClaudeApprove(in io.Reader, out io.Writer, endpoint string, timeout time.Duration) int` — returns the process exit code, always 0.

- [ ] **Step 1: Write the failing test**

Create `cmd_claude_approve_test.go`:

```go
package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const hookStdin = `{"hook_event_name":"PreToolUse","tool_name":"Write","tool_use_id":"tu1","session_id":"s1","cwd":"/tmp","tool_input":{"file_path":"/tmp/x","content":"hi"}}`

func decision(t *testing.T, out string) (string, string) {
	t.Helper()
	var d struct {
		HookSpecificOutput struct {
			HookEventName            string `json:"hookEventName"`
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &d); err != nil {
		t.Fatalf("hook output is not valid JSON: %v\n%s", err, out)
	}
	if d.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Errorf("hookEventName = %q, want PreToolUse", d.HookSpecificOutput.HookEventName)
	}
	return d.HookSpecificOutput.PermissionDecision, d.HookSpecificOutput.PermissionDecisionReason
}

func TestClaudeApproveAllows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"decision":"allow"}`))
	}))
	defer srv.Close()

	var out bytes.Buffer
	code := runClaudeApprove(strings.NewReader(hookStdin), &out, srv.URL, 5*time.Second)
	if code != 0 {
		t.Errorf("exit code = %d, want 0 — a non-zero exit is itself a block signal", code)
	}
	if d, _ := decision(t, out.String()); d != "allow" {
		t.Errorf("decision = %q, want allow", d)
	}
}

func TestClaudeApproveDenies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"decision":"deny","reason":"user declined"}`))
	}))
	defer srv.Close()

	var out bytes.Buffer
	runClaudeApprove(strings.NewReader(hookStdin), &out, srv.URL, 5*time.Second)
	d, reason := decision(t, out.String())
	if d != "deny" {
		t.Errorf("decision = %q, want deny", d)
	}
	if !strings.Contains(reason, "user declined") {
		t.Errorf("reason = %q, want it to carry the server's reason", reason)
	}
}

func TestClaudeApproveDeniesWhenHuginnUnreachable(t *testing.T) {
	var out bytes.Buffer
	// Port 1 is reserved and will refuse instantly.
	code := runClaudeApprove(strings.NewReader(hookStdin), &out, "http://127.0.0.1:1", 2*time.Second)
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	d, reason := decision(t, out.String())
	if d != "deny" {
		t.Fatalf("decision = %q, want deny — an unreachable Huginn must NEVER approve", d)
	}
	if !strings.Contains(reason, "Write") {
		t.Errorf("reason = %q, want it to name the tool so the user knows what was blocked", reason)
	}
}

func TestClaudeApproveDeniesOnTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.Write([]byte(`{"decision":"allow"}`))
	}))
	defer srv.Close()

	var out bytes.Buffer
	start := time.Now()
	runClaudeApprove(strings.NewReader(hookStdin), &out, srv.URL, 300*time.Millisecond)
	if el := time.Since(start); el > 1500*time.Millisecond {
		t.Errorf("took %v; the timeout was not honoured", el)
	}
	if d, _ := decision(t, out.String()); d != "deny" {
		t.Errorf("decision = %q, want deny on timeout", d)
	}
}

func TestClaudeApproveDeniesOnGarbageStdin(t *testing.T) {
	var out bytes.Buffer
	runClaudeApprove(strings.NewReader("not json"), &out, "http://127.0.0.1:1", time.Second)
	if d, _ := decision(t, out.String()); d != "deny" {
		t.Errorf("decision = %q, want deny — unparseable input must not approve", d)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run TestClaudeApprove -v`
Expected: FAIL — `undefined: runClaudeApprove`.

- [ ] **Step 3: Write the implementation**

Create `cmd_claude_approve.go`:

```go
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// claudeApproveTimeout bounds how long the hook waits for Huginn.
//
// IMPLEMENTATION NOTE: verify Claude Code's own PreToolUse hook timeout and
// keep this BELOW it. If Claude Code gives up first, our decision is lost
// rather than denied, which silently defeats fail-closed.
const claudeApproveTimeout = 60 * time.Second

// runClaudeApprove is the PreToolUse hook body. Claude Code writes the tool
// call to stdin and reads a decision from stdout.
//
// It ALWAYS exits 0 and ALWAYS emits a decision: a crashed or non-zero-exiting
// hook is itself interpreted as a block, but with no reason the user can read.
// Every failure path here emits an explicit deny naming the tool.
func runClaudeApprove(in io.Reader, out io.Writer, endpoint string, timeout time.Duration) int {
	raw, _ := io.ReadAll(in)

	var hook struct {
		ToolName  string          `json:"tool_name"`
		ToolUseID string          `json:"tool_use_id"`
		SessionID string          `json:"session_id"`
		CWD       string          `json:"cwd"`
		ToolInput json.RawMessage `json:"tool_input"`
	}
	if err := json.Unmarshal(raw, &hook); err != nil {
		emitDecision(out, "deny", "Huginn could not parse the tool call; denying")
		return 0
	}
	tool := hook.ToolName
	if tool == "" {
		tool = "tool"
	}

	body, err := json.Marshal(map[string]any{
		"tool_name":   hook.ToolName,
		"tool_use_id": hook.ToolUseID,
		"session_id":  hook.SessionID,
		"cwd":         hook.CWD,
		"tool_input":  hook.ToolInput,
	})
	if err != nil {
		emitDecision(out, "deny", fmt.Sprintf("Huginn could not encode the request; %s denied", tool))
		return 0
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		emitDecision(out, "deny", fmt.Sprintf("Huginn unreachable — %s requires approval", tool))
		return 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		emitDecision(out, "deny", fmt.Sprintf("Huginn returned %d — %s requires approval", resp.StatusCode, tool))
		return 0
	}

	var decided struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decided); err != nil {
		emitDecision(out, "deny", fmt.Sprintf("Huginn sent an unreadable decision — %s denied", tool))
		return 0
	}
	if decided.Decision != "allow" {
		reason := decided.Reason
		if reason == "" {
			reason = fmt.Sprintf("Huginn denied %s", tool)
		}
		emitDecision(out, "deny", reason)
		return 0
	}

	emitDecision(out, "allow", decided.Reason)
	return 0
}

func emitDecision(out io.Writer, decision, reason string) {
	payload := map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       decision,
			"permissionDecisionReason": reason,
		},
	}
	b, _ := json.Marshal(payload)
	fmt.Fprintln(out, string(b))
}
```

- [ ] **Step 4: Register the subcommand**

In `main.go`, find where subcommands are dispatched (`grep -n '"skill"' main.go` shows the pattern used by `cmd_skill.go`) and add a `claude-approve` case that calls:

```go
	os.Exit(runClaudeApprove(os.Stdin, os.Stdout,
		fmt.Sprintf("http://127.0.0.1:%d/api/v1/claude/approve", cfg.WebUI.Port),
		claudeApproveTimeout))
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test . -run TestClaudeApprove -v`
Expected: PASS — all five, including both fail-closed cases.

Run: `go build ./...`

- [ ] **Step 6: Commit**

```bash
git add cmd_claude_approve.go cmd_claude_approve_test.go main.go
git commit -m "feat: huginn claude-approve PreToolUse hook, fails closed"
```

---

### Task 6: The `/api/v1/claude/approve` endpoint

**Files:**
- Create: `internal/server/handlers_claude_approve.go`
- Create: `internal/server/handlers_claude_approve_test.go`
- Modify: `internal/server/server.go` (route registration, beside the existing claude routes)

**Interfaces:**
- Consumes: `Agent.ClaudeSessionID` (Task 1).
- Produces: `POST /api/v1/claude/approve` → `{"decision":"allow"|"deny","reason":string}`.

- [ ] **Step 1: Write the failing test**

Create `internal/server/handlers_claude_approve_test.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postApprove(t *testing.T, s *Server, body string) (int, string, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/claude/approve", strings.NewReader(body))
	s.handleClaudeApprove(rec, req)
	var out struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&out)
	return rec.Code, out.Decision, out.Reason
}

func TestApproveDeniesUnknownSession(t *testing.T) {
	s := &Server{}
	code, decision, reason := postApprove(t, s,
		`{"tool_name":"Write","session_id":"nobody","tool_use_id":"t1"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — the hook needs a parseable body even on refusal", code)
	}
	if decision != "deny" {
		t.Errorf("decision = %q, want deny for a session bound to no agent", decision)
	}
	if reason == "" {
		t.Error("reason must be populated so Claude Code can tell the user why")
	}
}

func TestApproveDeniesMalformedBody(t *testing.T) {
	s := &Server{}
	_, decision, _ := postApprove(t, s, `not json`)
	if decision != "deny" {
		t.Errorf("decision = %q, want deny", decision)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestApprove -v`
Expected: FAIL — `s.handleClaudeApprove undefined`.

- [ ] **Step 3: Write the implementation**

Create `internal/server/handlers_claude_approve.go`:

```go
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// handleClaudeApprove answers a PreToolUse hook from a Claude Code agent.
//
// It ALWAYS responds 200 with a decision object: the hook parses this body, and
// a non-200 would make it fall back to its own deny with a less useful reason.
// Refusal is expressed in the payload, not the status code.
func (s *Server) handleClaudeApprove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ToolName  string          `json:"tool_name"`
		ToolUseID string          `json:"tool_use_id"`
		SessionID string          `json:"session_id"`
		CWD       string          `json:"cwd"`
		ToolInput json.RawMessage `json:"tool_input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondApprove(w, "deny", "Huginn could not parse the approval request")
		return
	}

	agentName, ok := s.agentForClaudeSession(req.SessionID)
	if !ok {
		slog.Warn("claudecode: approval request for an unbound session",
			"session_id", req.SessionID, "tool", req.ToolName)
		respondApprove(w, "deny",
			"No Huginn agent is bound to this Claude Code session")
		return
	}

	// v1: any tool that reached the hook was NOT pre-authorised, so it needs a
	// human. Surfacing it in the approval UI is wired in a follow-up; until
	// then an un-preauthorised tool is denied with a legible reason rather than
	// silently allowed.
	slog.Info("claudecode: tool call requires approval",
		"agent", agentName, "tool", req.ToolName, "tool_use_id", req.ToolUseID)
	respondApprove(w, "deny",
		"Huginn: "+req.ToolName+" is not in this agent's allowed tools")
}

func respondApprove(w http.ResponseWriter, decision, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"decision": decision,
		"reason":   reason,
	})
}

// agentForClaudeSession maps a Claude Code session id to the agent bound to it.
func (s *Server) agentForClaudeSession(sessionID string) (string, bool) {
	if sessionID == "" || s.agentLoader == nil {
		return "", false
	}
	cfg, err := s.agentLoader()
	if err != nil || cfg == nil {
		return "", false
	}
	for _, a := range cfg.Agents {
		if a.ClaudeSessionID != "" && a.ClaudeSessionID == sessionID {
			return a.Name, true
		}
	}
	return "", false
}
```

Check `s.agentLoader`'s exact type first — `grep -n "agentLoader" internal/server/server.go` — and match the returned struct's field name for the agent slice.

- [ ] **Step 4: Register the route**

In `registerRoutes` (`internal/server/server.go:805`), beside the existing claude routes:

```go
	mux.HandleFunc("POST /api/v1/claude/approve", api(s.handleClaudeApprove))
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/server/ -run TestApprove -v`
Expected: PASS — both.

Run: `go test ./internal/server/` — the full package still passes.

- [ ] **Step 6: Commit**

```bash
git add internal/server/handlers_claude_approve.go internal/server/handlers_claude_approve_test.go internal/server/server.go
git commit -m "feat(server): approval endpoint for Claude Code agent tool calls"
```

---

### Task 7: `ClaudeCodeBackend`

**Files:**
- Create: `internal/claudecode/agent_backend.go`
- Test: `internal/claudecode/agent_backend_test.go`

**Interfaces:**
- Consumes: `AssembleSystemPrompt` (Task 3), `BuildHookSettings` (Task 4), `BuildArgs`/`applyStreamLine` (existing `delegate.go`), `backend.ExecutedTool` (Task 2).
- Produces: `type AgentBackendConfig struct{ Binary, SessionID, CWD, Model, SystemPrompt string; AllowedTools, GatedTools []string; HookCommand, MCPConfig string; FirstTurn bool }` and `func NewAgentBackend(cfg AgentBackendConfig) *AgentBackend` implementing `backend.Backend`.

- [ ] **Step 1: Write the failing test**

Create `internal/claudecode/agent_backend_test.go`:

```go
package claudecode

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/scrypster/huginn/internal/backend"
)

func agentBackendCfg(t *testing.T) AgentBackendConfig {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", "fake-claude.sh"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if err := os.Chmod(p, 0o755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	return AgentBackendConfig{
		Binary:       p,
		SessionID:    "11111111-2222-3333-4444-555555555555",
		CWD:          t.TempDir(),
		Model:        "opus",
		SystemPrompt: "You are Elena.",
		AllowedTools: []string{"Read"},
		GatedTools:   []string{"Write"},
		HookCommand:  "huginn claude-approve",
	}
}

func TestAgentBackendSendsOnlyTheNewestMessage(t *testing.T) {
	b := NewAgentBackend(agentBackendCfg(t))
	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Messages: []backend.Message{
			{Role: "user", Content: "old turn one"},
			{Role: "assistant", Content: "old reply"},
			{Role: "user", Content: "NEWEST TURN"},
		},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if !strings.Contains(resp.Content, "Done:") {
		t.Errorf("Content = %q, want the fake CLI's result text", resp.Content)
	}
	argv := b.lastArgs()
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "NEWEST TURN") {
		t.Errorf("newest message not passed to the CLI: %v", argv)
	}
	if strings.Contains(joined, "old turn one") {
		t.Error("older history was replayed — Claude Code owns the conversation, only the newest turn is sent")
	}
}

func TestAgentBackendNeverReturnsDispatchableToolCalls(t *testing.T) {
	b := NewAgentBackend(agentBackendCfg(t))
	resp, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Messages: []backend.Message{{Role: "user", Content: "go"}},
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if len(resp.ToolCalls) != 0 {
		t.Fatalf("ToolCalls = %d, want 0 — populating it would make the agent loop run Claude Code's tools a second time", len(resp.ToolCalls))
	}
	if len(resp.ExecutedTools) == 0 {
		t.Error("ExecutedTools empty; the fake CLI emits a tool_use and it must be reported for history")
	}
	if resp.DoneReason != "stop" {
		t.Errorf("DoneReason = %q, want stop", resp.DoneReason)
	}
}

func TestAgentBackendUsesResumeAfterFirstTurn(t *testing.T) {
	cfg := agentBackendCfg(t)
	cfg.FirstTurn = true
	first := NewAgentBackend(cfg)
	if _, err := first.ChatCompletion(context.Background(), backend.ChatRequest{
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("first turn: %v", err)
	}
	if j := strings.Join(first.lastArgs(), " "); !strings.Contains(j, "--session-id") || strings.Contains(j, "--resume") {
		t.Errorf("first turn must use --session-id, not --resume: %v", first.lastArgs())
	}

	cfg.FirstTurn = false
	later := NewAgentBackend(cfg)
	if _, err := later.ChatCompletion(context.Background(), backend.ChatRequest{
		Messages: []backend.Message{{Role: "user", Content: "hi again"}},
	}); err != nil {
		t.Fatalf("later turn: %v", err)
	}
	if j := strings.Join(later.lastArgs(), " "); !strings.Contains(j, "--resume") {
		t.Errorf("later turns must use --resume: %v", later.lastArgs())
	}
}

func TestAgentBackendPassesPromptAllowedToolsAndHooks(t *testing.T) {
	b := NewAgentBackend(agentBackendCfg(t))
	if _, err := b.ChatCompletion(context.Background(), backend.ChatRequest{
		Messages: []backend.Message{{Role: "user", Content: "go"}},
	}); err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	j := strings.Join(b.lastArgs(), " ")
	for _, want := range []string{"--append-system-prompt", "You are Elena.", "--allowedTools", "Read", "--settings"} {
		if !strings.Contains(j, want) {
			t.Errorf("args missing %q: %v", want, b.lastArgs())
		}
	}
	if strings.Contains(j, "dangerously-skip-permissions") {
		t.Error("an agent backend must never pass --dangerously-skip-permissions")
	}
}

func TestAgentBackendEmptyMessagesIsAnError(t *testing.T) {
	b := NewAgentBackend(agentBackendCfg(t))
	if _, err := b.ChatCompletion(context.Background(), backend.ChatRequest{}); err == nil {
		t.Error("expected an error when there is no message to send")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/claudecode/ -run TestAgentBackend -v`
Expected: FAIL — `undefined: NewAgentBackend`.

- [ ] **Step 3: Write the implementation**

Create `internal/claudecode/agent_backend.go`. Reuse `applyStreamLine` from `delegate.go`; do NOT write a second parser.

```go
package claudecode

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/scrypster/huginn/internal/backend"
)

// AgentBackendConfig is everything a Claude Code agent needs for one turn.
type AgentBackendConfig struct {
	Binary       string
	SessionID    string
	CWD          string
	Model        string
	SystemPrompt string   // assembled by AssembleSystemPrompt, rebuilt every turn
	AllowedTools []string // pre-authorised; never invoke the approval hook
	GatedTools   []string // require approval; one PreToolUse hook entry each
	HookCommand  string
	MCPConfig    string // empty in v1 — the seam for the toolbelt MCP server
	FirstTurn    bool   // true until the session exists
}

// AgentBackend drives one Claude Code session as a Huginn agent's backend.
//
// It implements backend.Backend, with two documented departures from the
// interface's usual semantics:
//
//   - req.Messages is ignored past the newest turn. Claude Code owns the
//     conversation; replaying history would duplicate it.
//   - ToolCalls is always empty. Claude Code executes its own tools, so they
//     are reported via ExecutedTools for persistence and must never be
//     dispatched by the agent loop.
type AgentBackend struct {
	cfg AgentBackendConfig

	mu   sync.Mutex // serialises turns: one writer per transcript
	args []string   // last argv, for tests
}

// NewAgentBackend returns a backend bound to one Claude Code session.
func NewAgentBackend(cfg AgentBackendConfig) *AgentBackend { return &AgentBackend{cfg: cfg} }

func (b *AgentBackend) lastArgs() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.args...)
}

func (b *AgentBackend) Health(context.Context) error   { return nil }
func (b *AgentBackend) Shutdown(context.Context) error { return nil }
func (b *AgentBackend) ContextWindow() int             { return 200000 }

func (b *AgentBackend) ChatCompletion(ctx context.Context, req backend.ChatRequest) (*backend.ChatResponse, error) {
	// One turn at a time per session: two concurrent writers would corrupt the
	// transcript, which is the session's only source of truth.
	b.mu.Lock()
	defer b.mu.Unlock()

	prompt, err := newestUserMessage(req.Messages)
	if err != nil {
		return nil, err
	}

	args, err := b.buildArgs(prompt)
	if err != nil {
		return nil, err
	}
	b.args = args

	cmd := exec.CommandContext(ctx, b.cfg.Binary, args...)
	if b.cfg.CWD != "" {
		cmd.Dir = b.cfg.CWD
	}
	setProcessGroup(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("claudecode agent: stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("claudecode agent: start %s: %w", b.cfg.Binary, err)
	}

	var (
		res   DelegateResult
		tools []backend.ExecutedTool
	)
	onEvent := func(e StreamEvent) {
		switch e.Type {
		case "text":
			if req.OnToken != nil {
				req.OnToken(e.Text)
			}
		case "tool_use":
			// Surfaced for live display; persistence happens via ExecutedTools.
		}
	}

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		applyStreamLine(line, &res, onEvent)
		tools = appendExecutedTools(tools, line)
	}

	if waitErr := cmd.Wait(); waitErr != nil {
		return nil, fmt.Errorf("claudecode agent: %s failed: %w", b.cfg.Binary, waitErr)
	}
	if res.IsError {
		return nil, fmt.Errorf("claudecode agent: %s", res.ErrorText)
	}

	return &backend.ChatResponse{
		Content:       res.Text,
		DoneReason:    "stop",
		ExecutedTools: tools,
	}, nil
}

func (b *AgentBackend) buildArgs(prompt string) ([]string, error) {
	args := []string{"-p", prompt, "--output-format", "stream-json", "--verbose"}
	if b.cfg.FirstTurn {
		args = append(args, "--session-id", b.cfg.SessionID)
	} else {
		args = append(args, "--resume", b.cfg.SessionID)
	}
	if b.cfg.Model != "" {
		args = append(args, "--model", b.cfg.Model)
	}
	if b.cfg.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", b.cfg.SystemPrompt)
	}
	if len(b.cfg.AllowedTools) > 0 {
		args = append(args, "--allowedTools")
		args = append(args, b.cfg.AllowedTools...)
	}
	settings, err := BuildHookSettings(b.cfg.GatedTools, b.cfg.HookCommand)
	if err != nil {
		return nil, fmt.Errorf("claudecode agent: build hook settings: %w", err)
	}
	if settings != "" {
		args = append(args, "--settings", settings)
	}
	if b.cfg.MCPConfig != "" {
		args = append(args, "--mcp-config", b.cfg.MCPConfig)
	}
	return args, nil
}

// newestUserMessage returns the last user turn. Claude Code owns the rest.
func newestUserMessage(msgs []backend.Message) (string, error) {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" && strings.TrimSpace(msgs[i].Content) != "" {
			return msgs[i].Content, nil
		}
	}
	return "", fmt.Errorf("claudecode agent: no user message to send")
}
```

Add `appendExecutedTools` to the same file. It reuses `streamLine` and `contentBlock` from `delegate.go` / `mapper.go`:

```go
// appendExecutedTools collects tool_use blocks from assistant lines and fills
// their results from the following user line's tool_result blocks.
func appendExecutedTools(acc []backend.ExecutedTool, raw []byte) []backend.ExecutedTool {
	var sl streamLine
	if err := json.Unmarshal(raw, &sl); err != nil {
		return acc
	}
	for _, blk := range sl.Message.Content {
		switch blk.Type {
		case "tool_use":
			acc = append(acc, backend.ExecutedTool{
				Call: backend.ToolCall{
					ID:       blk.ID,
					Function: backend.ToolCallFunction{Name: blk.Name, Arguments: blk.Input},
				},
			})
		case "tool_result":
			for i := range acc {
				if acc[i].Call.ID == blk.ToolUseID {
					acc[i].Result = rawToString(blk.Content)
				}
			}
		}
	}
	return acc
}
```

Import `encoding/json` in this file and call `json.Unmarshal(raw, &sl)` directly — there is no
`jsonUnmarshal` helper to define.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/claudecode/ -run TestAgentBackend -v`
Expected: PASS — all five.

Run: `go test ./internal/claudecode/ -v` and `go test -race ./internal/claudecode/ -v`
Expected: PASS; nothing from the bridge regressed.

- [ ] **Step 5: Commit**

```bash
git add internal/claudecode/agent_backend.go internal/claudecode/agent_backend_test.go
git commit -m "feat(claudecode): AgentBackend — drive a Claude Code session as a Huginn agent"
```

---

### Task 8: Wire it up, bind the session, and stop the double write

**Files:**
- Modify: `main.go:3120-3130` (the agent backend resolver)
- Modify: `internal/claudecode/ingest.go` (skip agent-owned transcripts)
- Test: `internal/claudecode/agent_owned_test.go`

**Interfaces:**
- Consumes: `NewAgentBackend` (Task 7), `Agent.ClaudeSessionID` (Task 1).
- Produces: `func (i *Ingester) SetAgentOwned(externalIDs []string)`.

- [ ] **Step 1: Write the failing test**

Create `internal/claudecode/agent_owned_test.go`:

```go
package claudecode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIngesterSkipsAgentOwnedTranscripts(t *testing.T) {
	ing, sink, _ := newTestIngester(t)

	const extID = "55555555-5555-4555-8555-555555555555"
	b, err := os.ReadFile(filepath.Join("testdata", "basic.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	p := filepath.Join(t.TempDir(), extID+".jsonl")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// An agent owns this session: its chat path already persists these turns,
	// so ingesting them would duplicate every message.
	ing.SetAgentOwned([]string{extID})

	n, err := ing.IngestFile(p)
	if err != nil {
		t.Fatalf("IngestFile: %v", err)
	}
	if n != 0 {
		t.Errorf("appended %d messages for an agent-owned transcript, want 0", n)
	}
	if sink.session(extID) != nil {
		t.Error("a session was created for an agent-owned transcript; the agent already has one")
	}
}

func TestIngesterStillIngestsUnownedTranscripts(t *testing.T) {
	ing, sink, _ := newTestIngester(t)
	ing.SetAgentOwned([]string{"somebody-else"})

	const extID = "11111111-2222-3333-4444-555555555555"
	b, _ := os.ReadFile(filepath.Join("testdata", "basic.jsonl"))
	p := filepath.Join(t.TempDir(), extID+".jsonl")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	n, err := ing.IngestFile(p)
	if err != nil {
		t.Fatalf("IngestFile: %v", err)
	}
	if n == 0 {
		t.Error("an unowned transcript must still be ingested normally")
	}
	if sink.session(extID) == nil {
		t.Error("no session created for an unowned transcript")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/claudecode/ -run TestIngesterSkipsAgentOwned -v`
Expected: FAIL — `ing.SetAgentOwned undefined`.

- [ ] **Step 3: Implement the skip**

In `internal/claudecode/ingest.go`, add to `Ingester`:

```go
	agentOwned map[string]bool
```

initialise it in `NewIngester`, and add:

```go
// SetAgentOwned marks Claude Code sessions that a Huginn agent drives. Their
// transcripts are skipped: the agent's chat path already persists those turns,
// and ingesting them too would duplicate every message.
//
// Replaces the whole set, so callers pass the full list on every change.
func (i *Ingester) SetAgentOwned(externalIDs []string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	m := make(map[string]bool, len(externalIDs))
	for _, id := range externalIDs {
		if id != "" {
			m[id] = true
		}
	}
	i.agentOwned = m
}

func (i *Ingester) isAgentOwned(externalID string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.agentOwned[externalID]
}
```

In `IngestFile`, immediately after `externalID` is derived and the empty check:

```go
	if i.isAgentOwned(externalID) {
		return 0, nil
	}
```

- [ ] **Step 4: Wire the backend at the resolver**

In `main.go`, in the agent backend resolver around line 3126, branch before the cache:

```go
			if ag.Provider == "claude-code" {
				return claudecode.NewAgentBackend(claudecode.AgentBackendConfig{
					Binary:       cfg.ClaudeCode.Binary,
					SessionID:    ag.ClaudeSessionID,
					CWD:          ag.ClaudeCWD,
					Model:        ag.GetModelID(),
					SystemPrompt: claudecode.AssembleSystemPrompt(ag.SystemPrompt, agentSkillTexts(ag), notepadText()),
					AllowedTools: ag.LocalTools,
					GatedTools:   gatedToolsFor(ag),
					HookCommand:  huginnExe + " claude-approve",
					FirstTurn:    !claudeSessionExists(ag.ClaudeSessionID),
				}), nil
			}
			return serveCache.For(ag.Provider, ag.Endpoint, ag.APIKey, ag.GetModelID())
```

Write the four small helpers next to the resolver:

- `agentSkillTexts(ag)` — the agent's skills as prompt text. Follow how native agents already load skills (`grep -rn "Skills" internal/agent/ | grep -i prompt`).
- `notepadText()` — the notepad body, via `internal/notepad`.
- `gatedToolsFor(ag)` — every Claude Code tool NOT in `ag.LocalTools`. Start from the fixed list `{"Bash","Write","Edit","NotebookEdit","WebFetch"}` and subtract the allowed ones. Read-only tools left ungated are pre-authorised and never call Huginn, which is what keeps a Huginn outage from blocking ordinary work.
- `claudeSessionExists(id)` — true when `~/.claude/projects/**/<id>.jsonl` exists. Reuse `claudecode.DefaultRoot()`.

Also call `ing.SetAgentOwned(...)` with every agent's `ClaudeSessionID` after the bridge starts, and again whenever agents change (`grep -n "reload live registry when agents change" -r internal/` shows the existing hook).

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/claudecode/ -v`
Expected: PASS, including both new tests and the whole bridge suite.

Run: `go build ./...` and `GOOS=windows go build ./...`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/claudecode/ingest.go internal/claudecode/agent_owned_test.go main.go
git commit -m "feat: resolve claude-code agents to AgentBackend and skip their transcripts"
```

---

### Task 9: Documentation

**Files:**
- Create: `docs/features/claude-code-agents.md`
- Modify: `docs/index.md`
- Modify: `docs/planning/2026-08-25-claude-code-agent-provider-design.md` (status line)

- [ ] **Step 1: Write the feature doc**

Create `docs/features/claude-code-agents.md` in the house style used by `docs/features/routines.md`: `# Title`, `## What it is` as prose, a `---` rule, `## How to use it` with `###` subsections.

It must cover: creating an agent with provider `claude-code`; that one agent is bound to one session with a fixed working directory; that the system prompt, skills and notepad are reassembled every turn; how approval works (pre-authorised tools via `--allowedTools`, gated tools via `PreToolUse` hooks); that approval **fails closed** and what the user sees when Huginn is unreachable; and the takeover command `claude --resume <session-id>`.

State plainly what is NOT there: **Huginn's toolbelt connections do not reach a Claude Code agent** — they are Huginn-native integrations running in Huginn's process, and bridging them needs the separate MCP-server subsystem. Do not imply otherwise.

- [ ] **Step 2: Add the index entry**

`docs/index.md` lists features as `→ [Name](features/x.md) — blurb`. Add:

```
→ [Claude Code Agents](features/claude-code-agents.md) — an agent that IS a Claude Code session, with Huginn's rules and approvals
```

- [ ] **Step 3: Update the design doc status**

Change its `**Status:**` line to name the implementing branch.

- [ ] **Step 4: Verify**

Run: `go build ./...`
Run: `go test ./internal/claudecode/ ./internal/agents/ ./internal/backend/ ./internal/agent/ ./internal/server/ -count=1`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add docs/
git commit -m "docs: Claude Code agents feature guide"
```

---

## Verification checklist

| Spec requirement | Task |
|---|---|
| Agent bound to one session + cwd | 1 |
| `ExecutedTools` persisted, never dispatched | 2 |
| Prompt reassembled every turn | 3 |
| One hook entry per gated tool | 4 |
| Pre-authorised tools never call the hook | 4, 8 |
| Hook fails closed (unreachable, timeout, garbage) | 5 |
| Approval endpoint correlates session → agent | 6 |
| Newest message only; Claude Code owns history | 7 |
| `--session-id` first turn, `--resume` after | 7 |
| Never `--dangerously-skip-permissions` | 7 |
| One turn at a time per session | 7 |
| `--mcp-config` seam, empty in v1 | 7 |
| Agent-owned transcripts skipped by the ingester | 8 |
| Backend resolved at the agent resolver | 8 |
| Feature documentation, toolbelt gap stated | 9 |

**Deliberately not in this plan** (spec's Out of scope): the toolbelt MCP server; attaching to a live terminal session; the takeover pause state and the adoption liveness heuristic — those are UI work best specced once the backend exists. `claudeSessionExists` in Task 8 covers first-turn detection; a full session picker is not part of v1.
