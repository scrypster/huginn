# TUI Reference

The terminal interface launches with `huginn` (no arguments). This page covers the layout, keyboard shortcuts, and slash commands available in the TUI.

---

## Layout

```
┌─────────────────────────────────────────────────────────────────────┐
│  Sidebar                  │  Main viewport                          │
│  ─────────────────────    │  ─────────────────────────────────────  │
│  [Sessions / Threads]     │  Conversation output / streaming text   │
│                           │                                         │
│  ↑ j/k or pgup/pgdn       │  Scrollable. j/k/pgup/pgdn when input  │
│    to navigate            │  is empty. G = jump to bottom.          │
│                           │                                         │
│                           │  ┌───────────────────────────────────┐  │
│                           │  │  [Active agent]  [Model]          │  │
│                           │  │  > Chat input                     │  │
│                           │  │  [Attachment chips, if any]       │  │
│                           │  └───────────────────────────────────┘  │
│                           │  Status bar: mode / state / keyhint     │
└─────────────────────────────────────────────────────────────────────┘
```

- **Sidebar** — shows sessions and threads. Toggle focus with `ctrl+b`.
- **Viewport** — scrollable output area. In streaming state the view follows new output; press `G` to re-enable follow mode after scrolling up.
- **Chat input** — the prompt bar at the bottom. `Enter` sends. `#` opens the file picker to attach files.
- **Status bar** — reflects the current app state (chat, streaming, awaiting permission, etc.) and active agent.

---

## Keyboard shortcuts

### General

| Key | Action |
|-----|--------|
| `Enter` | Send message (when chat input is focused) |
| `ctrl+c` | Cancel current generation if streaming; clear input if not; press twice to exit |
| `ctrl+d` | Exit Huginn immediately |
| `?` | Show keyboard shortcuts help overlay (when input is unfocused) |
| `shift+tab` | Toggle auto-run mode on/off |
| `ctrl+p` | Cycle to the next primary agent |
| `ctrl+a` | Open agent creation wizard |
| `ctrl+b` | Toggle sidebar focus |

### Navigation (when input is empty)

These keys are active only when the chat input field is empty. Once you start typing, they revert to normal text input.

| Key | Action |
|-----|--------|
| `j` / `down` | Scroll down one line |
| `k` / `up` | Scroll up one line |
| `pgdown` | Scroll down by one page |
| `pgup` | Scroll up by one page |
| `G` | Jump to bottom and re-enable follow mode |
| `home` | Jump to top |
| `]` | Jump to next thread header |
| `[` | Jump to previous thread header |
| `enter` | Expand a collapsed thread header |
| `space` | Collapse an expanded thread header |
| `esc` | Cancel a queued message, or unfocus attachment chips |

### Permission prompts

Shown when a tool call requires approval (`statePermAwait`).

| Key | Action |
|-----|--------|
| `a` / `y` | Allow this tool call (once) |
| `A` | Allow all future calls to this tool for the rest of the session |
| `d` / `c` / `n` | Deny this call |

### Write approval

Shown when an agent wants to write or modify a file (`stateWriteAwait`).

| Key | Action |
|-----|--------|
| `a` / `y` | Approve the write |
| `d` | Deny the write |

### Diff / approval

Shown when reviewing a proposed change (`stateApproval`).

| Key | Action |
|-----|--------|
| `a` / `y` | Approve the change |
| `e` | Open for editing before applying |
| `c` / `n` | Cancel / reject |

### Artifact actions

When the cursor is on an artifact line in `stateChat`:

| Key | Action |
|-----|--------|
| `a` | Accept the artifact at the cursor |
| `r` | Reject the artifact at the cursor |

### Overlays and panels

| Key | Action |
|-----|--------|
| `ctrl+o` | Open artifact overlay if cursor is on an artifact line; otherwise toggle expand/collapse of the last tool output |
| `ctrl+t` | Open thread overlay |
| `ctrl+e` | Open observation deck |
| `ctrl+s` | Open swarm detail view |

**Inside the artifact overlay:**

| Key | Action |
|-----|--------|
| `esc` / `q` | Close the overlay |
| `up` / `k` | Scroll up |
| `down` / `j` | Scroll down |

**Inside the thread overlay:**

| Key | Action |
|-----|--------|
| `esc` | Close the overlay |
| `a` | Accept artifact at cursor (same as in main view) |
| `ctrl+o` | Open artifact overlay from within the thread overlay |

### File attachments

| Key | Action |
|-----|--------|
| `#` | Open the file picker |
| `backspace` (empty input, attachments present) | Focus the attachment chip row |
| `left` / `right` (chip focused) | Navigate between attachment chips |
| `backspace` (chip focused) | Remove the focused chip |
| `esc` (chip focused) | Unfocus chips, return to input |

### Thread navigation

| Key | Action |
|-----|--------|
| `]` | Jump to next thread header in viewport |
| `[` | Jump to previous thread header in viewport |

---

## Slash commands

Type a `/` prefix in the chat input to activate slash command mode.

### Mode commands

| Command | Description |
|---------|-------------|
| `/plan` | Plan mode: runs a planner pass, then a coder pass |
| `/code` | Code mode: direct implementation request, no planning phase |
| `/reason` | Reasoning mode: deep analysis before responding |
| `/iterate N` | Refine the last response N times |
| `/help` | Show keyboard shortcuts in the viewport |

### Agent commands

| Command | Description |
|---------|-------------|
| `/agents` | List all agents and open the agents screen |
| `/agents new` | Open the interactive agent creation wizard |
| `/agents create <name> <model>` | Create a new agent inline with a given model |
| `/agents swap <name> <model>` | Change an agent's model and save to disk |
| `/agents rename <name> <new>` | Rename an agent and save to disk |
| `/agents persona <name>` | View an agent's system prompt |
| `/agents delete <name>` | Delete an agent |
| `/switch-model` | Change the model for the current session slot |

### Swarm and parallel

| Command | Description |
|---------|-------------|
| `/swarm agent1:prompt1 \| agent2:prompt2` | Run named agents in parallel; each gets its own prompt |
| `/parallel task1 \| task2` | Run tasks in parallel using the default agent |

### Analysis

| Command | Description |
|---------|-------------|
| `/impact <symbol>` | Impact analysis for a code symbol (function, type, etc.) |
| `/radar` | Run a change impact scan on the current commit |
| `/stats` | Show session statistics (tokens, turns, cost) |
| `/workspace` | Show the workspace root and indexed chunk count |

### Session management

| Command | Description |
|---------|-------------|
| `/resume` | Open the session picker to switch sessions |
| `/save` | Save the current session |
| `/title <name>` | Rename the current session |

### Space navigation

| Command | Description |
|---------|-------------|
| `/dm` | Open the DM picker |
| `/channel` | Open the channel picker |

### Notepad

| Command | Description |
|---------|-------------|
| `/notepad list` | List all notepads |
| `/notepad show <name>` | Show the content of a notepad |
| `/notepad delete <name>` | Delete a notepad |

### Screen navigation

| Command | Description |
|---------|-------------|
| `/models` | Models management screen |
| `/connections` | Connections management screen |
| `/skills` | Skills management screen |
| `/settings` | Settings screen |
| `/workflows` | Workflows management screen |
| `/logs` | Logs screen |
| `/inbox` | Inbox screen |

---

## App states

The TUI tracks a discrete state at all times. The status bar and available keybindings change with the state.

| State | What it means |
|-------|---------------|
| `stateChat` | Normal input mode. You can type and send messages. |
| `stateStreaming` | The agent is generating a response. `ctrl+c` cancels. |
| `stateWizard` | Slash command prefix is active; the command picker is visible. |
| `stateFilePicker` | The `#` file picker is open. Navigate and select files to attach. |
| `stateApproval` | A diff or proposed change is waiting for your review. |
| `statePermAwait` | A tool call is waiting for permission (allow / deny). |
| `stateWriteAwait` | An agent wants to write or overwrite a file. |
| `stateSessionPicker` | The session switcher overlay is open (`/resume`). |
| `stateSwarm` | A swarm run is active; `/swarm` or `/parallel` was invoked. |
| `stateAgentWizard` | The interactive agent creation wizard is open. |
| `stateArtifactView` | The artifact overlay is open (`ctrl+o`). |
| `stateThreadOverlay` | The thread overlay is open (`ctrl+t`). |
| `stateObservationDeck` | The observation deck is open (`ctrl+e`). |

---

## Tips

- **Use `G` to re-anchor to the bottom.** Scrolling up during streaming pauses auto-follow. Press `G` to jump back to the bottom and resume following output.
- **`ctrl+c` is not an exit key.** It cancels streaming, then clears input on a second press. To exit cleanly, use `ctrl+d`.
- **Vim-style navigation works in the viewport.** When your input is empty, `j`/`k` scroll line by line and `]`/`[` jump between thread boundaries — useful in long sessions.
- **`A` (capital) saves you repeated prompts.** In a permission prompt, `A` grants that tool permission for the entire session rather than just once.
- **Combine `/swarm` with named agents for parallelism.** If you have agents `Alice` and `Bob`, `/swarm Alice:write the tests | Bob:write the docs` runs both concurrently and merges results.
