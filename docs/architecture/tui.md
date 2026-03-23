# TUI Architecture

**Topic**: Terminal UI — how Huginn renders multi-agent output in the terminal

---

## 1. The Problem

When an AI coding assistant runs multiple agents concurrently, each agent streams
tokens, invokes tools, and reports status changes — all at the same time. Writing
that output to stdout naively produces a scrambled mess: tokens from three agents
interleaved with tool results and error messages, no clear separation, no ability
to scroll back independently.

Plain stdout also breaks other requirements:

- **Streaming**: the user needs to see model output token-by-token, not after the
  full response is buffered.
- **Simultaneous panels**: a diff review, a permission prompt, and a live chat
  stream cannot coexist on a raw terminal.
- **Stateful chrome**: a status footer, auto-run indicator, and input box must
  stay pinned while scrollable content above them updates.
- **Overlays**: the slash-command wizard, file picker, and session picker must
  appear inline without clearing the screen.

A structured TUI solves all of these by owning the entire terminal frame and
redrawing it on every state change.

---

## 2. Bubbletea's Elm Architecture

Huginn uses [Bubble Tea](https://github.com/charmbracelet/bubbletea), a Go TUI
framework built on the Elm architecture: Model / Update / View.

```
┌─────────────────────────────────────────────────────┐
│                    Bubble Tea runtime                │
│                                                     │
│   tea.Msg ──► Update(Model, Msg) ──► (Model, Cmd)  │
│                                            │         │
│                                            ▼         │
│                                    Cmd fires async  │
│                                    returns tea.Msg  │
│                                            │         │
│   View(Model) ◄── render ──────────────────┘         │
│        │                                             │
│        ▼                                             │
│   terminal frame                                     │
└─────────────────────────────────────────────────────┘
```

**Model** (`App` struct in `internal/tui/app.go`) holds all TUI state: chat
history, viewport scroll position, active streaming buffers, current `appState`
enum, sub-model instances for overlays, and the swarm view.

**Update** is the single entry point for all state mutations. Every incoming
`tea.Msg` (keypress, streamed token, tool event, swarm event, window resize) is
handled here. Update returns a new Model and an optional `tea.Cmd` — a function
that runs asynchronously and emits the next Msg.

**View** reads the Model and produces a string that Bubble Tea writes to the
terminal. View is pure: it never mutates state.

Why this fits Huginn: agent work is inherently asynchronous. An LLM streams
tokens on a goroutine; tools execute on goroutines; the swarm runs N agents in
parallel. The Elm architecture makes the update path single-threaded — there is
no mutex protecting the Model because Update is never called concurrently. All
async results are bridged into the single-threaded Update loop via `tea.Cmd`.

---

## 3. Message Flow Diagram

```
SwarmEvent channel          (goroutine: swarm.Swarm.Run)
  │
  └─► readSwarmEvent(ch) tea.Cmd
        │  (blocks on channel read, returns swarmEventMsg)
        ▼
      swarmEventMsg
        │
        └─► Update() ──► SwarmViewModel.SetStatus / AppendOutput / etc.
                  │
                  └─► readSwarmEvent(ch)  [chain: next event]


streaming.Runner / tokenCh  (goroutine: backend HTTP stream)
  │
  └─► waitForToken(ch, errCh) tea.Cmd
        │  (blocks on channel read)
        ▼
      tokenMsg(content)          ─► Update() ─► streaming.Builder.WriteString
      thinkingTokenMsg(content)  ─► Update() ─► thoughtStreaming.Builder.WriteString
      streamDoneMsg{}            ─► Update() ─► flush builder ─► addLine("assistant", ...)


agent event channel (agentic loop)
  │
  └─► waitForEvent(eventCh, errCh) tea.Cmd
        ▼
      toolCallMsg  ─► Update() ─► addLine("tool-call", preview)
      toolDoneMsg  ─► Update() ─► addLine("tool-done", output + timing)
      tokenMsg     ─► Update() ─► streaming.Builder.WriteString
      streamDoneMsg ─► Update() ─► flush ─► state = stateChat


permission gate  (goroutine: main.go forwarder)
  │
  └─► p.Send(PermissionPromptMsg{Req, RespCh})
        ▼
      PermissionPromptMsg ─► Update() ─► state = statePermAwait
                                           (user presses a/A/d)
                                         ─► RespCh <- decision


All paths end at:
  Update() ─► new Model state
               │
               └─► View() ─► rendered terminal frame
```

---

## 4. Key TUI Components

### 4.1 appState enum

`appState` (defined in `app.go`) is an iota enum that controls which overlays
render and which keyboard shortcuts are active:

| State            | Description                                              |
|------------------|----------------------------------------------------------|
| `stateChat`      | Normal chat input. Default idle state.                   |
| `stateWizard`    | Slash-command picker overlay (`/` prefix in input).      |
| `stateFilePicker`| `@` file attachment picker overlay.                      |
| `stateApproval`  | Plan approval prompt (approve / edit / cancel).          |
| `stateStreaming`  | Model is actively streaming tokens.                      |
| `statePermAwait` | Waiting for user Allow / Deny / AllowAll on a tool call. |
| `stateSessionPicker` | Session resume overlay.                              |
| `stateSwarm`     | Multi-agent swarm progress view.                         |
| `stateAgentWizard` | Agent creation wizard overlay.                         |

State transitions are the only mechanism by which overlays appear and disappear.
Each overlay's key handler is gated by checking `a.state` at the top of Update.

### 4.2 Chat viewport

A `bubbles/viewport.Model` holds the scrollable chat history. All rendered lines
are committed to `a.history []chatLine` and the viewport is refreshed with
`refreshViewport()` after each mutation. `recalcViewportHeight()` reserves lines
for all visible chrome (divider, input box, footer, overlays) and adjusts the
viewport height accordingly.

### 4.3 WizardModel

`wizard.go` — a filtered command list triggered by `/`. Accepts fuzzy filtering
as the user continues typing after the `/`. Emits `WizardSelectMsg` on Enter,
`WizardDismissMsg` on Escape.

### 4.4 FilePickerModel

`filepicker.go` — an `@`-triggered overlay that fuzzy-searches the indexed
workspace files. Supports multi-select (up to 10 attachments). Emits
`FilePickerConfirmMsg` or `FilePickerCancelMsg`.

### 4.5 DiffReviewModel

`diffview_model.go` — manages sequential per-file diff review with keyboard
shortcuts: `a`/`s` (approve), `r` (reject), `A` (approve all), `R` (reject all).
Delegates rendering to the `diffview` package.

### 4.6 SwarmViewModel

`swarmview.go` — maintains per-agent card state (status, tool name, output ring
buffer capped at 50 lines). Renders either an overview grid of agent cards or a
focused detail view for one agent. Driven by `swarmEventMsg` arriving from
`readSwarmEvent`.

### 4.7 AgentWizardModel / loaderModel

`agentwizard.go` — a multi-step form (name → model → backstory → confirm) for
creating named agents at runtime. Persists to `~/.huginn/agents.json` on
completion.

`loader.go` — a standalone progress-bar TUI program run before the main App
starts. Runs `repo.Build` in a goroutine, sends `progressMsg` events, and
terminates with the completed index.

---

## 5. Streaming Rendering

### Regular tokens

When the agent loop or backend runner emits a text token, it is sent on a
`chan string` (the runner's `TokenCh()`). `waitForToken` is a `tea.Cmd` that
blocks on that channel and returns a `tokenMsg`. Update appends the token to
`a.streaming` (a `strings.Builder`) and chains another `waitForToken`.

The in-progress streamed text is rendered inline in `refreshViewport` alongside
committed history lines — the user sees each token appear as it arrives.

On `streamDoneMsg`, the accumulated `a.streaming` content is committed to
`a.history` as a full `chatLine` and the builder is reset.

### Extended thinking tokens

Anthropic's extended thinking feature emits separate `StreamThought` events from
the backend. `streamEventToMsg` converts these to `thinkingTokenMsg`.

In Update, a `thinkingTokenMsg` is written to `a.thoughtStreaming` (a separate
builder) and rendered with `StyleThought` — a muted gray italic (`#6B7280`).
Thought content is display-only: it is never committed to `a.history` or sent
back to the model as context. On `streamDoneMsg`, `a.thoughtStreaming` is reset
alongside `a.streaming`.

This separation means the user sees the model's reasoning while it thinks, but
the final assistant message in history contains only the response text.

---

## 6. Diff Review

### diffview package

`internal/diffview/diffview.go` is a self-contained diff package with no
dependency on Bubble Tea. It provides:

- `FileDiff` — computed diff for one file: path, old/new content, added/deleted
  counts, unified diff string, and parsed `[]DiffHunk`.
- `DiffHunk` / `DiffLine` — hunk header and individual lines with op byte
  (`+`, `-`, ` `).
- `ComputeDiff(path, oldContent, newContent)` — LCS-based diff computation.
- `RenderDiff(d FileDiff, width int)` — ANSI-colored output using lipgloss:
  green for additions (`#3FB950`), red for deletions (`#F85149`), gray for
  context (`#6E7681`), purple-bold for file paths (`#BB86FC`).
- `RenderBatch(diffs []FileDiff, width int)` — renders all diffs separated by
  dividers with batch Accept/Reject controls appended.

### Integration into TUI flow

`DiffReviewModel` (in `diffview_model.go`) wraps a slice of `FileDiff` with
cursor and decision tracking. `HandleKey` processes the per-file or batch
keyboard shortcuts and advances the cursor. `View()` delegates to
`diffview.RenderDiff` for the current diff; `ViewBatch()` delegates to
`diffview.RenderBatch`.

The App renders the diff inside the viewport during `stateApproval`. Once
`DiffReviewModel.Done()` returns true, the decisions slice is inspected to
apply or reject each proposed file change.

---

## 7. Permission Prompts

When the agent loop invokes a tool that requires user approval (auto-run is off),
the permission gate in `main.go` calls a `promptFunc` that sends a
`PermissionPromptMsg` to the Bubble Tea program via `p.Send()`. The struct
carries the `PermissionRequest` and a `chan permissions.Decision` that must
receive exactly one value.

In Update, `PermissionPromptMsg` transitions the App to `statePermAwait` and
stores the pending prompt in `a.permPending`. View renders the prompt with
`renderPermissionPrompt()`.

The user presses:
- `a` / `y` — `permissions.Allow` (this call only)
- `A` — `permissions.AllowAll` (all future calls in session)
- `d` / `c` / `n` — `permissions.Deny`

`handlePermission(decision)` sends the decision on `RespCh`, clears
`a.permPending`, and transitions back to `stateStreaming`. The gate goroutine
unblocks and the agent loop continues.

The `autoRunAtom` (`*atomic.Bool`) is a shared value set by `SetAutoRunAtom`.
When auto-run is on, the gate reads the atom directly without going through the
TUI event loop — permission is granted without interruption.

---

## 8. Headless Mode

### Why it exists

Not every Huginn invocation is interactive. CI pipelines, editor integrations,
and scripting use cases need the same workspace analysis (index + radar) without
a human at a terminal. Headless mode provides a non-interactive path through the
same codebase tooling.

### What it does

`internal/headless/runner.go` implements `Run(HeadlessConfig)`. It runs the
full pipeline — workspace detection, incremental Pebble index, optional radar
scan — and returns a `RunResult` struct with all findings and metadata.

Headless mode does not start a Bubble Tea program. It has no viewport, no
streaming renderer, and no permission prompt. When `JSON: true` is set in
`HeadlessConfig`, the caller can serialize `RunResult` directly to JSON for
machine consumption.

The pipeline steps:
1. `repo.Detect` — workspace vs. single-repo vs. plain directory.
2. `storage.Open` — Pebble store in `~/.huginn/store/<path-hash>/`.
3. `repo.BuildIncrementalWithStats` — incremental file indexing.
4. `radar.Evaluate` — BFS-based change impact analysis (if `/radar run` is
   requested).

### What headless does not provide

Interactive permission prompts are impossible in headless mode. Any tool calls
requiring approval are not handled — headless is used for analysis, not for
running agent loops against live code.

---

## 9. Why This Design?

### Why Bubble Tea over termbox or raw ANSI?

Raw ANSI requires the author to manage cursor position, clear lines, and handle
terminal resize manually. Termbox is lower-level and puts the rendering loop on
the caller.

Bubble Tea's Elm architecture is the decisive advantage: because Update is the
only mutation path and View is pure, concurrent agent output never produces a
race condition on the TUI state. The goroutines that run the LLM backend, agent
loop, and swarm only communicate back to the TUI via channels read by `tea.Cmd`.
No goroutine touches the Model directly.

### Why is diffview separate from tui?

`internal/diffview` has zero imports from `internal/tui`. This means:

1. It is testable without a terminal: `RenderDiff` can be called in a unit test
   that just checks the returned string.
2. It is reusable in headless mode: a future headless diff-review step can call
   `ComputeDiff` / `RenderDiff` directly without any Bubble Tea dependency.
3. The tui package stays decoupled from diff logic; `DiffReviewModel` is a thin
   adapter.

### Why headless proves the architecture is sound?

If the TUI were doing load-bearing work — parsing tool output, managing agent
state, deciding what to index — then headless mode would need to duplicate that
logic or pull in TUI code. The fact that `headless.Run` can call `repo.Build`,
`storage.Open`, and `radar.Evaluate` without importing anything from `internal/tui`
proves that the TUI is a pure presentation layer. The agent loop, workspace
indexing, and diff computation all work independently of whether a terminal is
attached.

---

## 10. Thread Safety

**Single-threaded Update**: Bubble Tea guarantees that `Update` is never called
concurrently. The `App` struct has no mutexes protecting its fields because no
goroutine reads or writes them directly.

**Goroutine-to-TUI bridge via tea.Cmd**: Every goroutine (streaming runner,
agent loop, swarm executor, permission gate forwarder) communicates with the TUI
by sending values on a channel. A `tea.Cmd` function blocks on that channel and
returns the value as a `tea.Msg`. Bubble Tea dispatches the Msg to Update on its
own goroutine.

**SwarmEvent channel**: `swarm.Swarm` maintains an internal `chan SwarmEvent`
with a buffer of 512. The swarm goroutines emit events non-blocking (dropping
and incrementing `droppedEvents` if the buffer is full). On the TUI side,
`readSwarmEvent` is a `tea.Cmd` that reads one event and chains the next call —
forming an event bridge with no goroutine leak.

**autoRunAtom**: The permission gate's `promptFunc` runs on whatever goroutine
calls it (the agent loop goroutine). It reads `autoRunAtom` (an `*atomic.Bool`)
to check auto-run state without touching the App model. The App writes to the
atom in Update when the user toggles auto-run via Shift+Tab.

**eventCh nil-guard**: When a stream is cancelled (Ctrl+C), the App sets
`a.eventCh = nil` and drains the old channel on a background goroutine. Stale
`toolCallMsg` / `toolDoneMsg` events that arrive after cancellation are dropped
in Update via the guard `if a.eventCh == nil { return a, nil }`.

---

## 11. Limitations

**Terminal width/height constraints**: All layout math is based on the current
terminal dimensions reported by `tea.WindowSizeMsg`. Very narrow terminals (< 40
columns) degrade gracefully but are not explicitly handled — clipping may occur
in rendered cards and diff output.

**No mouse support**: The swarm overview uses number keys (1-9) to focus agents
instead of click targets. The diff review and file picker are fully keyboard-only.
Mouse events are not wired in any mode.

**Headless loses interactive prompts**: Any agent flow that requires user
permission approval is blocked in headless mode. The headless runner does not
implement a fallback (e.g., auto-deny or stdin prompt). Callers must not run
permission-gated agent loops headlessly.

**Thought tokens are ephemeral**: `thinkingTokenMsg` content is displayed while
streaming but is never stored in `a.history`. If the user scrolls up during
extended thinking, the thought text is gone once the stream completes. There is
no way to recall a model's reasoning after the fact.

**SwarmEvent buffer overflow**: If the TUI falls behind the swarm (e.g., a very
fast agent emitting thousands of token events), events are silently dropped.
`Swarm.DroppedEvents()` exposes the count, but the TUI does not currently surface
this to the user.

**Session auto-save is fire-and-forget**: After each `streamDoneMsg`, the session
is saved in a background goroutine (`go func() { _ = sessionStore.SaveManifest(...) }()`).
Write failures are silently discarded.

---

## 12. See Also

- `docs/architecture/swarm.md` — swarm event types, concurrency model,
  semaphore-based task scheduling.
- `docs/architecture/permissions-and-safety.md` — the Gate, permission
  decisions, AllowAll semantics, autoRun toggle.
- `docs/architecture/backend.md` — streaming event types (`StreamText`,
  `StreamThought`, `StreamDone`), how the backend HTTP stream is consumed by
  the Runner.
